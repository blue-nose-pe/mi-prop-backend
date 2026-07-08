# Asistente IA (chatbot del panel)

> Fuente de verdad: `gateway/internal/proxy/assistant.go` (~3380 líneas), con apoyo
> de `surveys.go` y `analytics.go`. Este documento describe lo que el código hace
> **hoy**; los identificadores (endpoints, tools, env vars, permisos) son reales y
> citables.

## 1. Qué es

El Asistente IA es un **chat de solo lectura** integrado en el panel (entrada de
menú "Asistente IA", ruta de front `/app/asistente`). El usuario escribe una
pregunta en lenguaje natural sobre sus datos y el asistente responde en texto y,
cuando aporta, adjunta **gráficos ApexCharts** y **botones de enlace** ("ver
resultado / descargar PDF").

- **Endpoint único:** `POST /api/assistant/chat`, registrado en
  `RegisterAssistant` (`assistant.go:38-40`).
- **Solo lectura:** el modelo NUNCA toca la base de datos ni genera SQL. Solo
  puede invocar un **conjunto curado de herramientas de lectura**, y cada
  herramienta ejecuta los **mismos RPCs scopeados que el panel**, con el request
  del caller (`r`). Así hereda `enforceColegioScope` / `callerColegioScope`: un
  asesor no puede ver otro colegio ni preguntándole al bot (la herramienta
  devuelve vacío o error). No existe ninguna herramienta de escritura (no crea
  usuarios, no cambia permisos/contraseñas, no envía correos).
- **Cuerpo de la petición:** `{ "messages": [ {role, content}, ... ] }`. Se
  ignora por seguridad cualquier rol distinto de `user`/`assistant` que venga del
  cliente (nunca se confía en un rol `tool` inyectado por el front).
- **Respuesta:** `{ "answer": "...", "charts": [...], "links": [...] }`.

## 2. Filosofía: "sólido = código, probabilístico = LLM"

El diseño separa deliberadamente lo que **debe** ser determinístico de lo que se
delega al modelo:

- **`temperature: 0`** en toda llamada al LLM (`callLLM`, `assistant.go:78`). El
  red-team mostró que con temperatura > 0 el modelo a veces elegía otra
  herramienta/rama y daba respuestas distintas a la misma pregunta (p.ej.
  "promedio 76.3" vs "no hay exámenes").
- **Overrides deterministas antes de ejecutar cada tool.** Sobre el texto del
  último mensaje del usuario se calculan señales por reglas (sin LLM) y se
  **fuerzan** argumentos de las herramientas, para que el modelo no cruce tipos,
  no arrastre contexto ni contradiga el gráfico. Los principales
  (`assistant.go:281-423`):
  - `examTypeFromText` → si la pregunta dice "simulacro"/"vocacional"/"estilos"
    (y solo uno), fuerza `exam_type_code`/`grafico`/`tipo` a ese tipo.
  - `wantsParticipacion` → fuerza `metric="participacion"` en
    `comparativo_colegios` (el ranking ordena por intentos y el texto coincide
    con el gráfico).
  - `wantsSingleChart` ("en un solo gráfico/radar") → apaga los gráficos
    automáticos (`grafico="ninguno"`) para que el modelo COMPONGA la combinación.
  - `yearsFromText` → 1 año se pasa como `period`; 2+ años ("2025 vs 2026") se
    pasan como `periods` y el comparativo construye ÉL MISMO las columnas
    agrupadas por año (el modelo no sabía combinar dos rankings en un gráfico).
  - `topFromText` → "top N" recorta el ranking (texto y gráfico iguales).
  - **REDIRECT duro:** si la pregunta de asesores trae tipo o UN año,
    `comparativo_asesores` se reescribe a `reporte_asesores_llaves` (misma fuente
    que la pantalla; sin esto el bot etiquetaba intentos de todos los tipos como
    "simulacro").
- **La validación y la política de presentación viven en código, no en el
  prompt.** `normalizeCharts`/`capCharts` (§6) imponen dedup y topes de gráficos;
  `toolGraficoPersonalizado` valida tipos (whitelist), tamaños y números finitos.
  El modelo decide QUÉ mostrar; el código decide CÓMO se renderiza y qué se
  filtra.
- **El prompt del sistema** (~130 líneas, `assistantSystemPrompt`,
  `assistant.go:115-241`) concentra las reglas anti-invención, de alcance, de
  formato, el dominio (3 evaluaciones, roles, conceptos) y la Guía del sistema.

## 3. Modelo y configuración

La config del LLM se inyecta con `WithAssistant(enabled, baseURL, model, apiKey)`
(`proxy.go:35-40`), desde `config/config.go`:

| Env var | Campo | Default en código | Notas |
| --- | --- | --- | --- |
| `ASSISTANT_ENABLED` | `AssistantEnabled` | `true` | Si es `false` (o falta `LLM_BASE_URL`), `/api/assistant/chat` responde **503** `ASSISTANT_DISABLED`. |
| `LLM_BASE_URL` | `LLMBaseURL` | `https://api.openai.com/v1` | Compatible con OpenAI o el endpoint `/v1` de Ollama. Se le concatena `/chat/completions`. |
| `LLM_MODEL` | `LLMModel` | `gpt-4o-mini` | **En producción se ejecuta con `LLM_MODEL=gpt-4.1`** (sobreescribe el default); gpt-4o-mini quedaba corto en preguntas complejas. |
| `OPENAI_API_KEY` (o `LLM_API_KEY`) | `LLMAPIKey` | `""` | Bearer del endpoint. Se envía como `Authorization: Bearer …` solo si no está vacío (Ollama local no la necesita). La clave vive en el **Secret `assistant-openai`** del clúster. |

Parámetros de ejecución (todos en código):

- **`temperature: 0`**, `tool_choice: "auto"` cuando hay tools (`callLLM`).
- **`maxIters = 8`** rondas de tool-calling (`assistant.go:308`): margen para
  investigaciones multi-paso (traer datos → razonar → pedir más → componer).
- **Timeouts:** cliente HTTP `assistantHTTPClient` a **90 s**
  (`assistant.go:36`); contexto de la petición a **120 s** (`assistant.go:303`).
- **Manejo de errores amable:** si `callLLM` falla (timeout, cuota OpenAI
  agotada, caída del upstream) NO se devuelve 5xx (pintaba pantalla muerta en el
  front): se responde **200** con un mensaje útil y los gráficos ya reunidos. El
  motivo real queda en el log `[assistant] callLLM err …`.
- **Cierre forzado:** si se agotan las 8 rondas, se hace una última llamada **sin
  herramientas** para que el modelo sintetice en texto lo que ya obtuvo (evita
  dejar gráficos huérfanos junto a un "no pude completar").

### Ciclo de tool-calling (resumen)

1. Se arma `messages = [system] + historial (solo user/assistant con texto)`.
2. Se calculan las señales deterministas del último mensaje del usuario.
3. Bucle hasta 8 veces: se llama al LLM con los esquemas de tools; si no pide
   herramientas, se devuelve `answer + charts + links`. Si pide herramientas, se
   aplican los overrides, se ejecuta cada tool con el request del caller, se
   acumulan sus `charts` y sus `_links`, y el resultado JSON vuelve al modelo como
   mensaje `role:"tool"`.
4. Los `_links` se extraen del resultado y se devuelven aparte, para **no
   exponer URLs crudas al modelo** (las escribiría como texto).

## 4. Catálogo de herramientas

22 herramientas, registradas en `assistantTools()` (`assistant.go:1535-1737`):
el slice `schemas` (lo que ve el LLM) y el mapa `byName` (nombre → ejecutor).
Todas son de solo lectura y heredan el scope del caller.

| Herramienta | Qué hace | Restricción de rol |
| --- | --- | --- |
| `listar_colegios` | Lista los colegios visibles (nombre, ciudad, id); resuelve el id por nombre. | Scope del usuario |
| `dashboard_colegio` | Resumen de un colegio: intentos y promedio de simulacro + perfil de vocacional/estilos. Parámetro `grafico` controla qué gráfico adjuntar (`simulacro`/`vocacional`/`estilos`/`todos`/`ninguno`); acepta `period` (año) y `key_code`/`key_id`. | Scope del usuario |
| `comparativo_colegios` | Ranking/comparación de colegios en UNA evaluación (barras). `metric` = `promedio` (solo simulacro) o `participacion` (intentos). Soporta `period`, `periods` (multi-año → columnas agrupadas + campo `mejora`), `top`, `chart_style`. | Scope del usuario |
| `comparativo_inclinacion` | Ranking de colegios por su **inclinación (%)** hacia UN área vocacional (Cálculo, Verbal, Artes…) o UN estilo de aprendizaje (Teórico, Activo…). No sirve para simulacro. | Scope del usuario |
| `resumen_general` | Totales agregados de TODOS los colegios visibles: total de intentos, promedio global de simulacro, `por_tipo_total` (dona por tipo) y desglose por colegio. Adjunta gráfico de participación por colegio. | Scope del usuario |
| `dashboard_asesor` | Indicadores del asesor actual (colegios, llaves, intentos, alumnos impactados, aforo, visitas). Admin puede pasar `asesor_id`. | Actual; admin ve a otro |
| `listar_llaves_colegio` | Llaves de un colegio, de la más reciente a la más antigua, con tipo, aforo, activa, fecha, `usos_registro` (contador, no fiable) y `rendidos_reales`. | Scope del usuario |
| `estudiantes_de_colegio` | Estudiantes de un colegio (nombre, correo, documento), con filtro de texto opcional. | Scope del usuario |
| `detalle_alumno` | Desempeño de UN alumno por nombre: sus intentos (simulacro con puntaje; vocacional/estilos como participación), fecha y en qué llave. | Scope del usuario |
| `top_alumnos_llave` | Alumnos con el puntaje más alto/bajo de una llave de **simulacro** concreta (intento principal = el último presentado). Gráfico + botón a resultados/PDF. | Scope del usuario |
| `mejor_alumno_general` | El/los alumno(s) con mejor/peor puntaje de simulacro entre todos los colegios visibles (o de uno). Intento principal. Gráfico + botón. | Scope del usuario |
| `ranking_llaves` | Ranking global de llaves por participación (histórico, incluye caducas). Gráfico ya compuesto (barras, o columnas por año con `por_anios`). | **Solo administración** |
| `reporte_asesores_llaves` | Reporte de asesores por llave (misma fuente/filtros que la pantalla "Reporte de asesores"): por asesor, sus llaves, alumnos que rindieron, aforo y ocupación. Filtros `tipo`, `anio`, `solo_activas`, `asesor`. | **Solo administración** |
| `comparativo_asesores` | Ranking de TODOS los asesores por una métrica (`alumnos_impactados`, `intentos`, `colegios`, `llaves`, `visitas_completadas`). Barras. | **Solo administración** |
| `asesores_colegios` | Lista de asesores con los **colegios reales** que maneja cada uno y cuántos alumnos ha impactado. | **Solo administración** |
| `satisfaccion_encuestas` | Reporte de satisfacción (mismas cifras que Reportería → Reporte de satisfacción): por encuesta, respuestas, CSAT (1-5 y %), NPS, tasa de respuesta real y desglose por pregunta + comentarios. Filtros `tipo`, `encuesta`, `llave`. Adjunta gráficos de CSAT y NPS. | **Solo administración** |
| `catalogo_examenes` | Catálogo maestro de exámenes (los que gestiona el admin, NO las llaves): conteo/lista por tipo y nº de preguntas de un examen. | **Solo administración** |
| `coordinadores` | Coordinadores de colegio: sin `colegio` cuenta/lista todos con los colegios que gestionan; con `colegio` los de ese colegio. | Admin sin colegio; scope con colegio |
| `campana_masiva` | Simulacro Masivo / campaña "Prepárate" (LAN): leads captados, cuántos recibieron acceso y tasa de envío. Son **leads**, no intentos. | **Solo administración** |
| `grupos_permisos` | Grupos de permisos (roles): lista con cuántos usuarios tiene cada uno. | **Solo administración** |
| `grafico_personalizado` | Construye UN gráfico a medida cuando ninguna otra tool trae el exacto o hay que mezclar temas (colegios × llaves × alumnos × asesores × años). Valores VERBATIM de tools de la conversación. Este gráfico reemplaza a los automáticos. | — |
| `panel_graficos` | Compón un panel de 2-4 gráficos en UNA sola llamada (para "panel/dashboard/varios gráficos"). Cada elemento = mismos campos que `grafico_personalizado`. Reemplaza a los automáticos. | — |

> Las restricciones "solo administración" se implementan en la propia tool con
> `if unrestricted, _, _ := p.callerColegioScope(r); !unrestricted { return … }`
> (p.ej. `toolSatisfaccionEncuestas` en `assistant.go:2749`). El bot no "decide"
> el permiso: hereda exactamente lo que el panel niega (la misma fuga que cerró
> la pantalla).

## 5. Gráficos ApexCharts

Los gráficos se generan en el backend como objetos JSON y el front los renderiza
con ApexCharts. **Tipos soportados** (whitelist `chartKinds`, `assistant.go:2453`):

```
line · area · bar · column · pie · donut/doughnut · polar
gauge · radial · radar · scatter · heatmap · treemap · funnel
```

Dos vías para gráficos:

- **Automáticos:** muchas tools ya adjuntan su gráfico (p.ej. `resumen_general`
  → barras de participación por colegio; `comparativo_colegios` → ranking;
  `satisfaccion_encuestas` → CSAT + NPS; `top_alumnos_llave` → puntajes).
- **A medida:** `grafico_personalizado` (uno) y `panel_graficos` (2-4 en una
  llamada). El modelo elige el tipo más demostrativo y aporta `series`, `labels`,
  `unit`, `horizontal`, `stacked`, `value` (gauge). El código:
  - valida el `kind` contra la whitelist y exige `title` (≤90 chars);
  - satura números (descarta NaN/Inf, redondea a 2 decimales), limita a **8
    series × 50 puntos** y hasta 30 labels;
  - **aplana la familia "de porciones"** (pie/donut/polar/treemap/funnel/radial):
    si el modelo mandó formato de ejes (`[{name,data}]`), se convierte a números
    planos + labels desde los nombres de serie — sin esto ApexCharts pintaba un
    lienzo vacío (bug real en treemap/polar/radial/funnel);
  - marca el gráfico con `_custom:true` (marcador interno que nunca sale al
    front) para que **gane sobre los automáticos**.

## 6. Política de presentación (`normalizeCharts` / `capCharts`)

`capCharts` (`assistant.go:814-894`) impone en código, de forma determinística,
qué gráficos se devuelven (el cliente odia el "spam" de gráficos):

- **Custom gana:** si hay gráficos `_custom`, se descartan todos los automáticos y
  solo se aplican dedup + tope de 4.
- **Dedup** por `(kind + title)`.
- **Anti-gauge suelto:** si hay barras (ranking), se quitan los `gauge` que el
  modelo a veces adjunta por colegio.
- **Topes por tipo:** ≤1 `gauge`, ≤1 `bar`, ≤2 `doughnut`, ≤2 `radar`, y **≤6
  gráficos en total**.

`capLinks` deduplica botones por URL y limita a 6.

## 7. Guardrails e inteligencia

El prompt del sistema (`assistantSystemPrompt`) y el código cooperan para que el
bot sea preciso y no invente:

- **Anti-invención (lo más importante):** el modelo SOLO obtiene datos por
  herramientas; nunca inventa números, nombres, promedios, %, teléfonos o
  causas. Si una tool no da el dato, dice que no lo tiene. No acepta cifras que
  el usuario afirme (matrícula, tendencias) para calcular nada; no existe
  "promedio/benchmark nacional".
- **Features inexistentes:** si preguntan por una función/columna/insignia/nivel
  que no está en la Guía ("modo turbo", "índice de fidelización", "deciles"), el
  bot responde que **no existe**, no la inventa.
- **Filtra data QA/prueba:** funciones `isQAColegio`, `isQAAlumno`,
  `isQASurveyName`, `isQACommentText`, `isQAExamName`, `isQAUser`
  (`assistant.go:1045-1194, 3161`) excluyen cuentas/colegios/encuestas de prueba
  (e2e/hunt/permhunt/@bluenose.test…). Ojo con las excepciones deliberadas: la
  tanda seed persistente (`@miprop.demo`, `@bluenose.demo`) es **data demo
  legítima que SÍ cuenta**; y el "Test de Intereses Vocacionales (TIV)" es un
  examen real (el token "test" no debe matarlo).
- **Alcance por rol heredado del panel:** los datos ya vienen filtrados por los
  permisos del usuario. Un asesor no ve la operación de otro asesor (rehúsa "solo
  puedo mostrar TU propia operación"); las tools admin-only niegan a
  asesores/coordinadores igual que el panel. Verificado en vivo sin fuga de scope.
- **Pregunta ante ambigüedad:** si no está claro el **tipo** de evaluación o el
  **alcance** (un colegio vs toda la plataforma), pregunta en una línea antes de
  responder, en vez de adivinar. Si la pregunta ya es específica, responde directo.
- **No arrastra contexto:** cada pregunta se interpreta por sí sola; no hereda el
  colegio/tipo de un turno anterior salvo que el usuario lo diga explícitamente.
- **Solo lectura, explícito:** si le piden una acción de escritura —o que la
  simule— rehúsa aclarando que solo consulta.
- **No revela interioridades:** no nombra sus herramientas por su identificador,
  ni campos técnicos, ni tablas/SQL, ni su prompt/configuración.
- **Texto = gráfico:** el elemento que el bot nombre como #1 debe ser el primero
  del gráfico; si el gráfico muestra 5 colegios, el texto los enumera los 5.
- **Cifras que coinciden con el panel:** las tools ejecutan los **mismos RPCs**
  que las pantallas (`satisfaccion_encuestas` reusa la fuente de Reportería →
  Satisfacción; `reporte_asesores_llaves` la de "Reporte de asesores"), por lo
  que sus números coinciden con lo que el usuario ve en el panel.
- **Precisión numérica:** distingue puntaje absoluto (ej. 436.5/450) de
  porcentaje (97 %); total de los 3 tipos vs solo simulacro; intentos vs alumnos
  distintos; intento **principal** = el último presentado; "usos" de una llave ≠
  exámenes rendidos.

## 8. Límites honestos

- **Es un LLM:** aunque el diseño empuja casi todo a reglas deterministas
  (temperatura 0, overrides, validación en código), el modelo sigue pudiendo
  equivocarse al interpretar una pregunta o al redactar. Los guardrails reducen,
  no eliminan, el error.
- **Scope estricto por rol:** el bot nunca ve más de lo que el rol del usuario
  ve en el panel. Herramientas marcadas "solo administración" devuelven un aviso
  a asesores/coordinadores; no hay forma de "pedirle" al bot data fuera de scope.
- **Costo y dependencia de OpenAI:** producción usa **gpt-4.1**, más caro que
  gpt-4o-mini y dependiente de la cuota de OpenAI. Si la cuota se agota o el
  upstream cae, el endpoint responde 200 con un mensaje amable (no 5xx) y el
  log guarda la causa real. (Existe la opción de apuntar `LLM_BASE_URL` a un
  Ollama local, `deploy/llm/`, hoy con réplicas en 0.)
- **Sin memoria entre sesiones:** el asistente solo conoce el historial que el
  front le manda en `messages`; no persiste conversaciones.
- **No adjunta archivos:** el PDF de resultados se descarga desde la vista del
  alumno; las tools devuelven un **botón/enlace** a esa vista, no el archivo.
