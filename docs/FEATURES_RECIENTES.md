# Features recientes — Mi Propósito 2.0

Resumen de las capacidades y semánticas que se **agregaron o cambiaron después de fines de mayo 2026**. El objetivo es que un dev nuevo (o el cliente) entienda cómo se comporta el sistema **HOY**, sin tener que reconstruirlo desde los commits. Cada punto está verificado contra el código actual del repo (rutas y nombres reales citados).

> Convención: cuando abajo se dice "gateway" es `gateway/internal/proxy/*.go` (HTTP/REST → gRPC); "keys_service", "users_service", "exams_service", "analytics" y "satisfaction_service" son los microservicios Go.

---

## 1. Asistente IA (bot) con gráficos y anti-invención

El panel incorpora un **chatbot solo-lectura** (`/app/asistente`, ruta REST `POST /api/assistant/chat`) que responde preguntas sobre reportería usando **tool-calling** de OpenAI (`gpt-4.1`) sobre herramientas ya scopeadas por rol — nunca SQL crudo, así que **hereda el mismo scope que el panel** (un asesor solo ve sus colegios). Implementación en `gateway/internal/proxy/assistant.go`.

**Por qué:** el cliente pedía "preguntarle a los datos" en lenguaje natural sin abrir cada dashboard. Para que sea confiable se blindó contra invención: contrato/glosario formal en el system prompt, políticas deterministas en código (rankings, comparativos multi-año, tipo de evaluación simulacro≠estilos) y herramientas que devuelven el resultado ya compuesto cuando el modelo tendería a fabricarlo. Renderiza gráficos embebidos (ApexCharts) con un catálogo completo de tipos y una tool `grafico_personalizado` cuyo código valida whitelist y límites.

Detalle completo (herramientas, catálogo de gráficos, límites) en **[ASISTENTE.md](ASISTENTE.md)**.

## 2. Aforo por ALUMNOS DISTINTOS (los reintentos no consumen cupo)

`key.max_uses` cuenta **usuarios distintos**, no intentos totales. El gate es atómico en `keys_service` → `KeyRepo.IncrementUses` (`keys_service/internal/adapters/outbound/mssql/key_repo.go`): un solo `UPDATE [key] SET current_uses = current_uses + 1` con guard `max_uses = 0 OR current_uses < max_uses`, ventana de validez, `active = 1` y `NOT EXISTS` de un `key_usage` previo del mismo `(key_id, user_id)`. Ese `NOT EXISTS` dentro del propio UPDATE evita la carrera entre dos requests concurrentes del mismo alumno.

**Por qué:** un alumno que reintenta no debe "gastar" una plaza extra ni robar cupo a otro. Consecuencia: `current_uses` = plazas ocupadas por alumnos únicos; los retries (gobernados por `max_attempts_per_user`) devuelven 0 filas afectadas y **no** incrementan el aforo. `max_uses = 0` significa **sin aforo** (llaves LAN masivas).

## 3. Intento PRINCIPAL = último presentado (no el mejor)

El intento que "vale" de un alumno es el **último submitted**, no el de mayor puntaje. Antes se usaba el mejor. Cambio en `assistant.go` (commit `15e53a5`): `top_alumnos_llave` y `mejor_alumno_general` rankean por el último intento por alumno; `detalle_alumno` ordena por fecha desc y marca `principal=true` el último de cada tipo (`principalPorTipo`).

**Por qué:** coherencia con el portal y los informes PDF, que abren el **último** intento. Así el número que ve el asesor en el bot coincide con el que abre el coordinador en el modal del alumno; los intentos anteriores quedan como registro histórico.

## 4. "Intentos por estudiante" configurable en la llave (`max_attempts_per_user`)

La llave tiene `max_attempts_per_user` re-expuesto en el formulario de creación/edición. `0` = server lo normaliza a un valor por defecto (ver `gateway/internal/proxy/keys.go`). El enforcement vive en el flujo de arranque: `gateway/internal/proxy/auth.go` consulta `Attempts.CountSubmittedByKeyUser` y, si el alumno ya alcanzó `max_attempts_per_user`, devuelve `can_attempt=false` con `block_reason="max_attempts"` (más `last_attempt_id`) **antes** del OTP.

**Por qué:** separa dos conceptos que se confundían — el **aforo** (cuántos alumnos distintos, punto 2) y **cuántas veces puede reintentar cada alumno**. Son límites independientes: un alumno con plaza puede reintentar hasta su cuota sin tocar el aforo.

## 5. Colegio inactivo (`active=0`) se RESTA de reportes / bot / dashboards

Al desactivar un colegio (`school.active=0`), deja de contar para su asesor/coordinador en TODAS las superficies de lectura (commit `bf0e7eb`). Gateway: `scopedColegios` / `resolveColegioID` / `listar_colegios` aplican `ActiveOnly` (rama admin) y `!GetActive()` (rama asesor), y `collectAsesoresKeys` excluye el colegio inactivo del Reporte de Asesores, `ranking_llaves` y `comparativo_asesores`. Analytics: `GetAsesorDashboard` filtra colegios inactivos **y sus llaves** (TotalKeys, aforo "X de Y", impactados, attempts) y `GetAsesorPendientes` no aporta pendientes. También bloquea registro de alumnos, acceso al portal y gestión de coordinadores del colegio inactivo (commit `d7c724e`).

Complemento (commit `8d8d43c`): los **intentos de usuarios desactivados** (`active=0`) tampoco cuentan. El fix está en un punto único — `exams_service` `ListByColegio` añade `AND active=1` en el subquery de usuarios — y cascadea a bot, analytics y panel (promedios/rankings dejaron de inflarse con cuentas QA).

**Por qué:** el cliente usa la desactivación como "reserva"; un colegio o una cuenta apagada no debe seguir sumando en promedios, aforos ni rankings.

## 6. Coordinadores muchos-a-muchos + permiso `db_users.coordinador.write`

Un colegio ya no tiene un único coordinador (`school.user_id`): ahora se manejan vía `assignment` con `kind='coordinador_de_colegio'` (`source`=coordinador, `target`=colegio), permitiendo **varios coordinadores por colegio y reutilizables**. Migración `036_backfill_coordinador_assignment.sql` respalda al coordinador existente en el nuevo modelo.

La gestión (asignar/quitar) exige el **permiso dedicado `db_users.coordinador.write`** (migración `037_coordinador_write_permission.sql`), que además **scopea**: un asesor con ese permiso solo toca coordinadores de **sus** colegios (`callerCanDeactivateCoordinador` en `gateway/internal/proxy/helpers.go`).

**Por qué:** un colegio real tiene varios coordinadores, y el cliente pidió que el asesor pudiera gestionarlos — pero sin abrir a todos los asesores lo que antes era solo-superadmin. La solución fue un permiso que el superadmin otorga a quien quiera.

## 7. Encuestas de satisfacción: tasa real, NPS 0-10, por llave o por tipo

`GET /api/analytics/satisfaccion/reporte` (solo admin) devuelve, por encuesta publicada: nº de respuestas, **CSAT** (promedio de las preguntas de escala 1-5, y su %), **NPS** y **tasa de respuesta con denominador correcto** = intentos de examen **elegibles** (los de su llave si está atada, o los de su tipo), en vez del viejo "usuarios rol estudiante" que siempre daba 0% (commit `fa91a22`, lógica en `gateway/internal/proxy/surveys.go` → `collectSatisfaccionReporte`, compartida por panel y bot).

- **NPS** se computa sobre preguntas de tipo `nps` en escala **0-10** (ponderado por nº de respuestas, `npsW/npsWn`), tras corregir la captura del alumno que venía en 1-5.
- Una encuesta puede estar **atada a una llave** (trae su `key_code`) o aplicar **por tipo** de examen (simulacro / vocacional / estilos); el reporte y la tool `satisfaccion_encuestas` filtran por tipo, por código/nombre de encuesta y por código de llave.

**Por qué:** el "0% de respuesta" era un denominador equivocado. Ahora el porcentaje refleja cuántos de los que **podían** responder lo hicieron, y las cifras del bot son idénticas a las de Reportería → Reporte de satisfacción.

## 8. Permiso de LAN masiva separado (`db_keys.lan.read` / `db_keys.lan.write`)

Ver/crear las **LAN masivas** (llaves sin colegio de la campaña "Prepárate", que captan leads) dejó de montar sobre el genérico `db_keys.key.write` y ahora depende de permisos propios (commit `374349d`, migración `039_lan_masiva_permissions.sql`): **crear/editar LAN → `db_keys.lan.write`**, **ver LAN → `db_keys.lan.read`**. En gateway, `generateKey` gatea por `mode` (`mode='lan'` exige `lan.write`; de colegio, `key.write`), `updateKey`/`deactivateKey` resuelven el modo de la llave objetivo (`gateKeyWriteByMode`) y `getKey`/`searchKeys` incluyen las LAN sin colegio para quien tenga `lan.read`.

**Por qué:** con el permiso único no se podía dar "crear llaves de colegio" sin dar también "crear LAN", y un asesor podía crear LANs sin querer. Ahora Marketing puede gestionar la campaña masiva sin tocar las llaves de colegio, y viceversa.

Catálogo completo de permisos y grupos en **[PERMISOS.md](PERMISOS.md)**.
