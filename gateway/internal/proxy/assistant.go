// Asistente IA (chatbot del panel) — POST /api/assistant/chat.
//
// Un agente de SOLO LECTURA que responde preguntas del cliente sobre sus datos
// y adjunta gráficos. Usa la API estilo OpenAI (chat-completions con function
// calling); funciona con OpenAI (gpt-4o-mini) o con el endpoint /v1 de Ollama.
//
// SEGURIDAD (clave): el modelo NUNCA toca la BD ni genera SQL. Solo puede llamar
// a un conjunto curado de "herramientas" de lectura, y cada herramienta ejecuta
// los MISMOS RPCs scopeados que el panel, CON el request del caller (r) — por lo
// que heredan enforceColegioScope / callerColegioScope. Un asesor no puede ver
// otro colegio ni preguntándole al bot: la herramienta devuelve vacío/403. No hay
// herramientas de escritura (no crea usuarios, no cambia permisos/contraseñas).
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	analyticsgrpcpb "analytics_service/proto/gen"
	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"
)

var assistantHTTPClient = &http.Client{Timeout: 90 * time.Second}

func (p *Proxy) RegisterAssistant(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/assistant/chat", p.assistantChat)
}

// ---------- Protocolo OpenAI chat-completions ----------

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Name       string       `json:"name,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

type oaResp struct {
	Choices []struct {
		Message      oaMessage `json:"message"`
		FinishReason string    `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func (p *Proxy) callLLM(ctx context.Context, msgs []oaMessage, tools []any) (*oaResp, error) {
	body := map[string]any{
		"model":       p.llmModel,
		"messages":    msgs,
		"temperature": 0.2,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.llmBaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.llmAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.llmAPIKey)
	}
	resp, err := assistantHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var out oaResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("respuesta LLM ilegible (http %d)", resp.StatusCode)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("LLM error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("LLM sin choices (http %d)", resp.StatusCode)
	}
	return &out, nil
}

// ---------- Handler ----------

const assistantSystemPrompt = `Eres el asistente de análisis de "Mi Propósito" (UCSP), un programa de orientación vocacional para colegios. Ayudas al usuario a consultar sus datos rápidamente, en español, claro y conciso.

REGLAS ESTRICTAS:
- SOLO obtienes datos mediante las herramientas. NUNCA inventes números, nombres, promedios ni porcentajes. Si no tienes un dato por una herramienta, dilo claramente.
- Los datos que devuelven las herramientas YA vienen filtrados por los permisos del usuario (su rol y sus colegios). No puedes ver nada fuera de su alcance; si una herramienta devuelve vacío o "sin acceso", explícalo sin inventar y sin sugerir que existe data oculta.
- Eres de SOLO LECTURA: no puedes crear ni editar usuarios, colegios, permisos ni contraseñas. Si te lo piden, aclara que solo consultas información.
- Tipos de evaluación: "simulacro" tiene puntaje de 0 a 100 (promediable). "vocacional" y "estilos de aprendizaje" (habitos) son PERFILES de inclinación por área (RIASEC / estilos): NO son promediables, se leen como distribución/porcentaje por área.
- Ojo con las llaves (keys): "usos_registro" es un contador de accesos que puede estar inflado; para saber si una llave tiene datos usa "rendidos_reales". Si te piden un gráfico "de una key" y esa key tiene rendidos_reales=0, NO afirmes que no hay datos del colegio: primero busca con listar_llaves_colegio una key con rendidos_reales>0, o usa dashboard_colegio SIN key_id (resumen de todo el colegio), y explica que esa llave puntual todavía no tiene exámenes rendidos.
- Para "la última key creada": llama listar_llaves_colegio y toma la primera (vienen ordenadas de más reciente a más antigua); su campo es key_id.
- Cuando muestres promedios, rankings o distribuciones, el panel adjunta gráficos automáticamente; puedes referirte a ellos.
- Responde breve y directo. Menciona los colegios por su NOMBRE, nunca por su ID.

Primero decide qué herramienta(s) necesitas, llámalas, y recién con los datos reales responde.`

type assistantRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func (p *Proxy) assistantChat(w http.ResponseWriter, r *http.Request) {
	if !p.assistantEnabled || p.llmBaseURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Status: "error", Code: "ASSISTANT_DISABLED", Message: "el asistente no está disponible"})
		return
	}
	var in assistantRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if len(in.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "EMPTY", Message: "faltan mensajes"})
		return
	}

	// Construimos la conversación: system + historial del cliente (solo user/assistant
	// con texto; se ignora cualquier rol tool que venga del cliente por seguridad).
	msgs := []oaMessage{{Role: "system", Content: assistantSystemPrompt}}
	for _, m := range in.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Content == "" {
			continue
		}
		msgs = append(msgs, oaMessage{Role: m.Role, Content: m.Content})
	}

	toolSchemas, toolByName := assistantTools()
	var charts []any

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	const maxIters = 5
	for iter := 0; iter < maxIters; iter++ {
		resp, err := p.callLLM(ctx, msgs, toolSchemas)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, errorBody{Status: "error", Code: "ASSISTANT_UPSTREAM", Message: "el asistente no pudo responder en este momento"})
			return
		}
		m := resp.Choices[0].Message
		if len(m.ToolCalls) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"answer": m.Content, "charts": charts})
			return
		}
		// Adjuntamos el mensaje del assistant que pidió las herramientas...
		msgs = append(msgs, m)
		// ...y ejecutamos cada herramienta con el request del caller (scope heredado).
		for _, tc := range m.ToolCalls {
			var args map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			if args == nil {
				args = map[string]any{}
			}
			var resultJSON string
			if tool, ok := toolByName[tc.Function.Name]; ok {
				res, ch, terr := tool(p, r, args)
				if terr != nil {
					res = map[string]any{"error": terr.Error()}
				}
				charts = append(charts, ch...)
				b, _ := json.Marshal(res)
				resultJSON = string(b)
			} else {
				resultJSON = `{"error":"herramienta desconocida"}`
			}
			msgs = append(msgs, oaMessage{Role: "tool", ToolCallID: tc.ID, Content: resultJSON})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"answer": "No pude completar la consulta (demasiados pasos). ¿Puedes reformular la pregunta de forma más simple?",
		"charts": charts,
	})
}

// ---------- Herramientas (solo lectura, scopeadas) ----------

type assistantToolFn func(p *Proxy, r *http.Request, args map[string]any) (result any, charts []any, err error)

func argStr(args map[string]any, k string) string {
	if v, ok := args[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func assistantExamTypeName(id int32) string {
	switch id {
	case 1:
		return "vocacional"
	case 2:
		return "simulacro"
	case 3:
		return "estilos"
	}
	return "otro"
}

// assistantTools devuelve (schemas para el LLM, mapa nombre→ejecutor).
func assistantTools() ([]any, map[string]assistantToolFn) {
	schemas := []any{
		toolSchema("listar_colegios", "Lista los colegios que el usuario puede ver (nombre, ciudad, id). Úsala para saber qué colegios hay o para obtener el id de un colegio por su nombre.", map[string]any{
			"type": "object", "properties": map[string]any{}, "required": []string{},
		}),
		toolSchema("dashboard_colegio", "Resumen de un colegio: intentos y promedio de simulacro, y perfil de áreas de vocacional/estilos. Adjunta gráficos (gauge de promedio, doughnut y radar de áreas). Acepta filtrar por periodo (año 'YYYY') y por key (key_id).", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"school_id": map[string]any{"type": "string", "description": "id del colegio (usa listar_colegios para obtenerlo por nombre)"},
				"period":    map[string]any{"type": "string", "description": "opcional, año 'YYYY'; vacío = todo el histórico"},
				"key_id":    map[string]any{"type": "string", "description": "opcional, id de una llave para analizar solo ese grupo"},
			},
			"required": []string{"school_id"},
		}),
		toolSchema("comparativo_colegios", "Ranking/comparación de los colegios del usuario en una evaluación. Adjunta un gráfico de barras. exam_type_code: 'simulacro' (por promedio) | 'vocacional' | 'habitos' (por participación).", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"exam_type_code": map[string]any{"type": "string", "enum": []string{"simulacro", "vocacional", "habitos"}, "description": "tipo de evaluación"},
			},
			"required": []string{"exam_type_code"},
		}),
		toolSchema("dashboard_asesor", "Indicadores del asesor actual (o de un asesor si el usuario es admin y pasa asesor_id): total de colegios, llaves, intentos, alumnos impactados, aforo, visitas completadas/agendadas.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"asesor_id": map[string]any{"type": "string", "description": "opcional; solo admin puede consultar a otro asesor"},
			},
			"required": []string{},
		}),
		toolSchema("listar_llaves_colegio", "Lista las llaves (keys) de un colegio, ordenadas de la MÁS RECIENTE a la más antigua (la primera es 'la última key creada'). Cada llave trae: key_id, codigo, tipo, aforo, activa, creada (fecha), usos_registro (contador de accesos, NO confiable) y rendidos_reales (exámenes realmente rendidos con resultados). Usa esta herramienta para elegir una key con datos: si necesitas graficar, prefiere una con rendidos_reales > 0.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"school_id": map[string]any{"type": "string", "description": "id del colegio"},
			},
			"required": []string{"school_id"},
		}),
	}
	byName := map[string]assistantToolFn{
		"listar_colegios":       toolListarColegios,
		"dashboard_colegio":     toolDashboardColegio,
		"comparativo_colegios":  toolComparativoColegios,
		"dashboard_asesor":      toolDashboardAsesor,
		"listar_llaves_colegio": toolListarLlavesColegio,
	}
	return schemas, byName
}

func toolSchema(name, desc string, params map[string]any) any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}

func toolListarColegios(p *Proxy, r *http.Request, _ map[string]any) (any, []any, error) {
	ctx := r.Context()
	unrestricted, _, _ := p.callerColegioScope(r)
	out := []map[string]any{}
	if unrestricted {
		resp, err := p.cli.Schools.ListSchools(ctx, &usersgrpcpb.ListSchoolsRequest{Limit: 1000})
		if err != nil {
			return nil, nil, err
		}
		for _, s := range resp.GetItems() {
			out = append(out, map[string]any{"id": s.GetId(), "nombre": s.GetName(), "ciudad": s.GetCity()})
		}
	} else {
		resp, err := p.cli.Schools.ListSchoolsByAsesor(ctx, &usersgrpcpb.ListSchoolsByAsesorRequest{AsesorId: userIDFromContext(r)})
		if err != nil {
			return nil, nil, err
		}
		for _, s := range resp.GetItems() {
			out = append(out, map[string]any{"id": s.GetId(), "nombre": s.GetName(), "ciudad": s.GetCity()})
		}
	}
	return map[string]any{"colegios": out, "total": len(out)}, nil, nil
}

func toolDashboardColegio(p *Proxy, r *http.Request, args map[string]any) (any, []any, error) {
	sid := argStr(args, "school_id")
	if sid == "" {
		return map[string]any{"error": "falta school_id"}, nil, nil
	}
	if !p.enforceColegioScope(r, sid) {
		return map[string]any{"error": "no tienes acceso a este colegio"}, nil, nil
	}
	resp, err := p.cli.Analytics.GetColegioDashboard(r.Context(), &analyticsgrpcpb.GetColegioDashboardRequest{
		SchoolId: sid,
		Period:   argStr(args, "period"),
		KeyId:    argStr(args, "key_id"),
	})
	if err != nil {
		return nil, nil, err
	}
	name := resp.GetSchoolName()
	stats := resp.GetByExamType()
	porTipo := map[string]any{}
	var charts []any
	labelFor := map[string]string{"simulacro": "Simulacro", "vocacional": "Vocacional", "habitos": "Estilos de aprendizaje"}
	for code, s := range stats {
		if s == nil {
			continue
		}
		entry := map[string]any{"intentos": s.GetAttempts()}
		if code == "simulacro" {
			entry["promedio"] = round1(s.GetAvgScore())
			if s.GetAttempts() > 0 {
				charts = append(charts, map[string]any{"kind": "gauge", "title": "Promedio simulacro — " + name, "value": round1(s.GetAvgScore())})
			}
		}
		areas := s.GetAreas()
		if len(areas) > 0 {
			labels := make([]string, 0, len(areas))
			pts := make([]float64, 0, len(areas))
			ratios := make([]float64, 0, len(areas))
			topAreas := []map[string]any{}
			for _, a := range areas {
				labels = append(labels, a.GetLabel())
				pts = append(pts, round1(float64(a.GetPoints())))
				ratios = append(ratios, math.Round(a.GetRatio()*100))
				topAreas = append(topAreas, map[string]any{"area": a.GetLabel(), "inclinacion_pct": math.Round(a.GetRatio() * 100)})
			}
			entry["areas"] = topAreas
			if code == "vocacional" || code == "habitos" {
				charts = append(charts, map[string]any{"kind": "doughnut", "title": labelFor[code] + " — " + name, "labels": labels, "series": pts})
				charts = append(charts, map[string]any{"kind": "radar", "title": "Perfil " + labelFor[code] + " — " + name, "labels": labels, "series": []map[string]any{{"name": labelFor[code], "data": ratios}}})
			}
		}
		porTipo[code] = entry
	}
	data := map[string]any{
		"colegio":        name,
		"total_alumnos":  resp.GetTotalStudents(),
		"total_intentos": resp.GetTotalAttempts(),
		"por_tipo":       porTipo,
	}
	return data, charts, nil
}

func toolComparativoColegios(p *Proxy, r *http.Request, args map[string]any) (any, []any, error) {
	exam := argStr(args, "exam_type_code")
	if exam == "" {
		exam = "simulacro"
	}
	resp, err := p.cli.Analytics.GetColegioComparativo(r.Context(), &analyticsgrpcpb.GetColegioComparativoRequest{ExamTypeCode: exam})
	if err != nil {
		return nil, nil, err
	}
	unrestricted, allowed, _ := p.callerColegioScope(r)
	type row struct {
		name     string
		avg      float64
		attempts int32
	}
	rows := []row{}
	for _, it := range resp.GetItems() {
		if !unrestricted && !allowed[it.GetSchoolId()] {
			continue
		}
		rows = append(rows, row{it.GetSchoolName(), it.GetAvgScore(), it.GetAttempts()})
	}
	scored := exam == "simulacro"
	// Orden: simulacro por promedio; voca/estilos por participación.
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			less := false
			if scored {
				less = rows[j].avg > rows[i].avg
			} else {
				less = rows[j].attempts > rows[i].attempts
			}
			if less {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	labels := make([]string, 0, len(rows))
	data := make([]float64, 0, len(rows))
	items := make([]map[string]any, 0, len(rows))
	for _, rw := range rows {
		labels = append(labels, rw.name)
		if scored {
			data = append(data, round1(rw.avg))
			items = append(items, map[string]any{"colegio": rw.name, "promedio": round1(rw.avg), "intentos": rw.attempts})
		} else {
			data = append(data, float64(rw.attempts))
			items = append(items, map[string]any{"colegio": rw.name, "intentos": rw.attempts})
		}
	}
	var charts []any
	if len(rows) > 0 {
		serieName := "Promedio (%)"
		title := "Ranking de simulacro por colegio"
		if !scored {
			serieName = "Participación (intentos)"
			title = "Participación por colegio (" + exam + ")"
		}
		charts = append(charts, map[string]any{"kind": "bar", "horizontal": true, "title": title, "labels": labels, "series": []map[string]any{{"name": serieName, "data": data}}})
	}
	return map[string]any{"evaluacion": exam, "promediable": scored, "items": items}, charts, nil
}

func toolDashboardAsesor(p *Proxy, r *http.Request, args map[string]any) (any, []any, error) {
	aid := userIDFromContext(r)
	if req := argStr(args, "asesor_id"); req != "" && callerIsUserAdmin(r) {
		aid = req
	}
	resp, err := p.cli.Analytics.GetAsesorDashboard(r.Context(), &analyticsgrpcpb.GetAsesorDashboardRequest{AsesorId: aid})
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"colegios":            resp.GetTotalColegios(),
		"llaves":              resp.GetTotalKeys(),
		"intentos":            resp.GetTotalAttempts(),
		"alumnos_impactados":  resp.GetTotalStudentsRendered(),
		"aforo_total":         resp.GetTotalAforo(),
		"visitas_completadas": resp.GetCompletedVisits(),
		"visitas_agendadas":   resp.GetScheduledVisits(),
		"pruebas_pendientes":  resp.GetPendingTests(),
	}, nil, nil
}

func toolListarLlavesColegio(p *Proxy, r *http.Request, args map[string]any) (any, []any, error) {
	sid := argStr(args, "school_id")
	if sid == "" {
		return map[string]any{"error": "falta school_id"}, nil, nil
	}
	if !p.enforceColegioScope(r, sid) {
		return map[string]any{"error": "no tienes acceso a este colegio"}, nil, nil
	}
	resp, err := p.cli.Keys.ListByColegio(r.Context(), &keysgrpcpb.ListByColegioRequest{SchoolId: sid})
	if err != nil {
		return nil, nil, err
	}
	// Exámenes REALMENTE rendidos (submitted) por key. El contador current_uses de
	// la key es de accesos/registros y puede estar inflado (o poblado por seed sin
	// exámenes); "rendidos_reales" es lo que sí tiene resultados y grafica el panel.
	rendidosByKey := map[string]int{}
	if att, aerr := p.cli.Attempts.ListByColegio(r.Context(), &examsgrpcpb.ListAttemptsByColegioRequest{SchoolId: sid}); aerr == nil {
		for _, a := range att.GetItems() {
			if a.GetSubmittedAt() != nil && a.GetKeyId() != "" {
				rendidosByKey[a.GetKeyId()]++
			}
		}
	}
	out := make([]map[string]any, 0, len(resp.GetItems()))
	for _, k := range resp.GetItems() {
		out = append(out, map[string]any{
			"key_id":          k.GetId(),
			"codigo":          k.GetCode(),
			"tipo":            assistantExamTypeName(k.GetExamTypeId()),
			"usos_registro":   k.GetCurrentUses(), // contador de accesos (NO confiable para desempeño)
			"rendidos_reales": rendidosByKey[k.GetId()],
			"aforo":           k.GetMaxUses(),
			"activa":          k.GetActive(),
			"creada":          optionalTimestamp(k.GetCreatedAt()),
		})
	}
	// Orden: más reciente primero (para "la última key creada").
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			ci, _ := out[i]["creada"].(string)
			cj, _ := out[j]["creada"].(string)
			if cj > ci {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return map[string]any{"llaves": out, "total": len(out), "nota": "usos_registro es el contador de accesos (puede estar inflado); usa rendidos_reales para saber qué llaves tienen exámenes con resultados."}, nil, nil
}
