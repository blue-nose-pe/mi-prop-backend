// Surveys + Survey responses handlers — proxy a satisfaction-service.
//
// Rutas REST:
//   POST   /api/surveys
//   GET    /api/surveys/{id}
//   PATCH  /api/surveys/{id}
//   POST   /api/surveys/{id}/publish
//   POST   /api/surveys/{id}/deactivate
//   GET    /api/surveys/by-code
//   GET    /api/surveys/{id}/questions
//   POST   /api/surveys/{id}/questions
//   PATCH  /api/survey-questions/{id}
//   DELETE /api/survey-questions/{id}
//   POST   /api/surveys/search
//   POST   /api/survey-responses
//   GET    /api/survey-responses/{id}
//   GET    /api/surveys/{id}/metrics
package proxy

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	analyticsgrpcpb "analytics_service/proto/gen"
	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	satisfactiongrpcpb "satisfaction_service/proto/gen"
	satisfactioncommonpb "satisfaction_service/proto/gen/common"
)

func (p *Proxy) RegisterSurveys(mux *http.ServeMux) {
	// Surveys
	mux.HandleFunc("POST /api/surveys", p.createSurvey)
	mux.HandleFunc("POST /api/surveys/search", p.searchSurveys)
	mux.HandleFunc("GET /api/surveys/by-code", p.getSurveyByCode)
	mux.HandleFunc("GET /api/surveys/{id}", p.getSurvey)
	mux.HandleFunc("PATCH /api/surveys/{id}", p.updateSurvey)
	mux.HandleFunc("POST /api/surveys/{id}/publish", p.publishSurvey)
	mux.HandleFunc("POST /api/surveys/{id}/deactivate", p.deactivateSurvey)
	mux.HandleFunc("POST /api/surveys/{id}/reactivate", p.reactivateSurvey)
	mux.HandleFunc("POST /api/surveys/{id}/clone", p.cloneSurvey)
	mux.HandleFunc("GET /api/surveys/{id}/questions", p.listSurveyQuestions)
	mux.HandleFunc("POST /api/surveys/{id}/questions", p.addSurveyQuestion)
	mux.HandleFunc("GET /api/surveys/{id}/metrics", p.getSurveyMetrics)

	// Survey questions (recurso independiente, ID propio)
	mux.HandleFunc("PATCH /api/survey-questions/{id}", p.updateSurveyQuestion)
	mux.HandleFunc("DELETE /api/survey-questions/{id}", p.removeSurveyQuestion)

	// Survey responses
	mux.HandleFunc("POST /api/survey-responses", p.submitSurveyResponse)
	mux.HandleFunc("GET /api/survey-responses/{id}", p.getSurveyResponse)

	// ---- PUBLICO (sin JWT) — flujo post-examen del alumno anonimo ----
	// Cliente (doc observaciones): "No se muestra el test de satisfaccion".
	// El alumno que rinde el simulacro NO tiene JWT; estas rutas estan en la
	// skip-list del gateway (/api/public/) y solo exponen encuestas PUBLICADAS.
	mux.HandleFunc("GET /api/public/surveys/active", p.getPublicActiveSurvey)
	mux.HandleFunc("POST /api/public/survey-responses", p.submitSurveyResponse)
}

// getPublicActiveSurvey devuelve la encuesta publicada+activa que aplica a un
// tipo de examen (query `exam_type`) y, opcionalmente, a una key (`key_id`),
// junto con sus preguntas. Sin auth. Si no hay encuesta aplicable, responde
// 200 con survey=null (el front simplemente no muestra el prompt).
func (p *Proxy) getPublicActiveSurvey(w http.ResponseWriter, r *http.Request) {
	examType := r.URL.Query().Get("exam_type")
	keyID := r.URL.Query().Get("key_id")
	resp, err := p.cli.Surveys.GetActivePublished(r.Context(), &satisfactiongrpcpb.GetActivePublishedRequest{
		TriggerKind: examType,
		KeyId:       keyID,
	})
	if err != nil {
		// NotFound (no hay encuesta aplicable) no es un error para el cliente:
		// devolvemos survey=null y que el front decida no renderizar.
		if status.Code(err) == codes.NotFound {
			writeJSON(w, http.StatusOK, map[string]any{"survey": nil, "questions": []any{}})
			return
		}
		writeGRPCError(w, err)
		return
	}
	questions := make([]map[string]any, 0, len(resp.GetQuestions()))
	for _, q := range resp.GetQuestions() {
		questions = append(questions, protoSurveyQuestionToJSON(q))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"survey":    protoSurveyToJSON(resp.GetSurvey()),
		"questions": questions,
	})
}

// ====================== SURVEYS ======================

type createSurveyRequest struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	TargetRole  string `json:"target_role"`
	TriggerKind string `json:"trigger_kind"`
	KeyID       string `json:"key_id"` // opcional: dirige la encuesta a una key
}

// requireSurveyWrite gatea TODAS las rutas de escritura de encuestas. Solo
// db_satisfaction.survey.write (verificado en BD: solo admin_permissions lo
// tiene) puede crear/editar/publicar/desactivar/reactivar/clonar encuestas y
// gestionar sus preguntas. Sin esto, cualquier autenticado (asesor,
// coordinador, o incluso un alumno con su JWT) podía desactivar/editar la
// encuesta de satisfacción de PRODUCCIÓN y dejar a todos sin encuesta.
func (p *Proxy) requireSurveyWrite(w http.ResponseWriter, r *http.Request) bool {
	if hasPermission(r, "db_satisfaction.survey.write") {
		return true
	}
	writeJSON(w, http.StatusForbidden, errorBody{
		Status: "error", Code: "PERMISSION_DENIED",
		Message: "no tienes permiso para gestionar encuestas (db_satisfaction.survey.write)",
	})
	return false
}

func (p *Proxy) createSurvey(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	var in createSurveyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Surveys.CreateSurvey(r.Context(), &satisfactiongrpcpb.CreateSurveyRequest{
		Code:        in.Code,
		Title:       in.Title,
		Description: in.Description,
		TargetRole:  in.TargetRole,
		TriggerKind: in.TriggerKind,
		KeyId:       in.KeyID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(resp.GetSurvey())})
}

func (p *Proxy) getSurvey(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Surveys.GetSurvey(r.Context(), &satisfactiongrpcpb.GetSurveyRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	s := resp.GetSurvey()
	if s == nil {
		writeNotFound(w, "survey")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(s)})
}

type updateSurveyRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Cliente: trigger_kind editable post-creacion ("simulacro"/"vocacional"/
	// "estilos"). "" -> no tocar.
	TriggerKind string `json:"trigger_kind"`
	// Cliente (2026-06-04): key_id editable. "" -> no tocar; "-" -> limpiar.
	KeyID       string `json:"key_id"`
}

func (p *Proxy) updateSurvey(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	var in updateSurveyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Surveys.UpdateSurvey(r.Context(), &satisfactiongrpcpb.UpdateSurveyRequest{
		Id:          r.PathValue("id"),
		Title:       in.Title,
		Description: in.Description,
		TriggerKind: in.TriggerKind,
		KeyId:       in.KeyID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(resp.GetSurvey())})
}

func (p *Proxy) publishSurvey(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	if _, err := p.cli.Surveys.PublishSurvey(r.Context(), &satisfactiongrpcpb.PublishSurveyRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) deactivateSurvey(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	if _, err := p.cli.Surveys.DeactivateSurvey(r.Context(), &satisfactiongrpcpb.DeactivateSurveyRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// reactivateSurvey vuelve un survey paused a draft activo (active=true,
// published=false). El usuario debe pasar por publish despues si quiere
// volver a publicarlo.
func (p *Proxy) reactivateSurvey(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	resp, err := p.cli.Surveys.ReactivateSurvey(r.Context(), &satisfactiongrpcpb.ReactivateSurveyRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	s := resp.GetSurvey()
	if s == nil {
		writeNotFound(w, "survey")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(s)})
}

// cloneSurvey crea un draft nuevo del survey indicado, copiando preguntas.
// Util cuando el original esta publicado y no admite mas ediciones.
func (p *Proxy) cloneSurvey(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	resp, err := p.cli.Surveys.CloneSurvey(r.Context(), &satisfactiongrpcpb.CloneSurveyRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	s := resp.GetSurvey()
	if s == nil {
		writeNotFound(w, "survey")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(s)})
}

func (p *Proxy) getSurveyByCode(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	versionStr := r.URL.Query().Get("version")
	if code == "" || versionStr == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "code and version query params are required"})
		return
	}
	v, err := strconv.ParseInt(versionStr, 10, 32)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "version must be an integer"})
		return
	}
	resp, err := p.cli.Surveys.GetByCode(r.Context(), &satisfactiongrpcpb.GetByCodeRequest{
		Code:    code,
		Version: int32(v),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	s := resp.GetSurvey()
	if s == nil {
		writeNotFound(w, "survey")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(s)})
}

func (p *Proxy) searchSurveys(w http.ResponseWriter, r *http.Request) {
	req := &satisfactioncommonpb.SearchRequest{}
	if err := decodeSearchRequest(r, req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Surveys.Search(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*satisfactioncommonpb.SearchResult, *satisfactioncommonpb.Paging](
		resp.GetTotal(), resp.GetResults(), resp.GetPaging(),
	))
}

// ---------- Survey questions ----------

func (p *Proxy) listSurveyQuestions(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Surveys.ListQuestions(r.Context(), &satisfactiongrpcpb.ListQuestionsRequest{
		SurveyId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, q := range resp.GetItems() {
		items = append(items, protoSurveyQuestionToJSON(q))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type addSurveyQuestionRequest struct {
	Text        string `json:"text"`
	Kind        string `json:"kind"`
	SortOrder   int32  `json:"sort_order"`
	OptionsJSON string `json:"options_json"`
	Required    bool   `json:"required"`
}

func (p *Proxy) addSurveyQuestion(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	var in addSurveyQuestionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Surveys.AddQuestion(r.Context(), &satisfactiongrpcpb.AddQuestionRequest{
		SurveyId:    r.PathValue("id"),
		Text:        in.Text,
		Kind:        in.Kind,
		SortOrder:   in.SortOrder,
		OptionsJson: in.OptionsJSON,
		Required:    in.Required,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"question": protoSurveyQuestionToJSON(resp.GetQuestion())})
}

type updateSurveyQuestionRequest struct {
	Text        string `json:"text"`
	SortOrder   int32  `json:"sort_order"`
	OptionsJSON string `json:"options_json"`
	Required    bool   `json:"required"`
}

func (p *Proxy) updateSurveyQuestion(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	var in updateSurveyQuestionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Surveys.UpdateQuestion(r.Context(), &satisfactiongrpcpb.UpdateQuestionRequest{
		Id:          r.PathValue("id"),
		Text:        in.Text,
		SortOrder:   in.SortOrder,
		OptionsJson: in.OptionsJSON,
		Required:    in.Required,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) removeSurveyQuestion(w http.ResponseWriter, r *http.Request) {
	if !p.requireSurveyWrite(w, r) {
		return
	}
	if _, err := p.cli.Surveys.RemoveQuestion(r.Context(), &satisfactiongrpcpb.RemoveQuestionRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- Survey responses ----------

type submitSurveyResponseRequest struct {
	SurveyID      string                 `json:"survey_id"`
	UserID        string                 `json:"user_id"`
	ExamAttemptID string                 `json:"exam_attempt_id"`
	Answers       []surveyResponseAnswer `json:"answers"`
}

type surveyResponseAnswer struct {
	QuestionID  string `json:"question_id"`
	ValueText   string `json:"value_text"`
	ValueNumber int32  `json:"value_number"`
	HasNumber   bool   `json:"has_number"`
}

func (p *Proxy) submitSurveyResponse(w http.ResponseWriter, r *http.Request) {
	var in submitSurveyResponseRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	answers := make([]*satisfactiongrpcpb.Answer, 0, len(in.Answers))
	for _, a := range in.Answers {
		answers = append(answers, &satisfactiongrpcpb.Answer{
			QuestionId:  a.QuestionID,
			ValueText:   a.ValueText,
			ValueNumber: a.ValueNumber,
			HasNumber:   a.HasNumber,
		})
	}
	// Anti-suplantación (audit 2026-06-18): NUNCA confiamos en in.UserID del body
	// (era forjable — cualquiera, incluso anónimo en /api/public/, podía mandar
	// respuestas en nombre de otro alumno). El user_id se toma SOLO del JWT si el
	// request viene autenticado; el flujo público anónimo queda con user_id vacío
	// (la encuesta de satisfacción es anónima, user_id NULLABLE en la BD).
	userID := ""
	if caller := userIDFromContext(r); caller != "" {
		userID = caller
	}
	resp, err := p.cli.Responses.Submit(r.Context(), &satisfactiongrpcpb.SubmitResponseRequest{
		SurveyId:      in.SurveyID,
		UserId:        userID,
		ExamAttemptId: in.ExamAttemptID,
		Answers:       answers,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"response": protoSurveyResponseToJSON(resp.GetResponse())})
}

func (p *Proxy) getSurveyResponse(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Responses.GetResponse(r.Context(), &satisfactiongrpcpb.GetResponseRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	sr := resp.GetResponse()
	if sr == nil {
		writeNotFound(w, "survey response")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"response": protoSurveyResponseToJSON(sr)})
}

// getSatisfaccionReporte — GET /api/analytics/satisfaccion/reporte
//
// Reporte agregado de ENCUESTAS DE SATISFACCIÓN para el panel de Reportería.
// Por encuesta publicada+activa: respuestas, CSAT (promedio de las preguntas de
// escala 1-5), NPS (de las preguntas nps), tasa de respuesta con denominador
// CORRECTO (los intentos de examen que califican para esa encuesta: los de su
// llave si está atada a una, o los de su tipo de examen si es general) — NO el
// conteo de "usuarios rol estudiante" que dejaba el dashboard en 0%. Filtros:
// tipo (trigger_kind) y survey_code. Devuelve además el desglose por pregunta
// (para los gráficos). SOLO administración.
func (p *Proxy) getSatisfaccionReporte(w http.ResponseWriter, r *http.Request) {
	if unrestricted, _, _ := p.callerColegioScope(r); !unrestricted {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "el reporte de satisfacción es solo para administración"})
		return
	}
	tipoFiltro := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tipo")))
	codeFiltro := strings.TrimSpace(r.URL.Query().Get("survey_code"))
	resumen, encuestas, err := p.collectSatisfaccionReporte(r.Context(), tipoFiltro, codeFiltro)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resumen": resumen, "encuestas": encuestas})
}

// collectSatisfaccionReporte agrega las métricas de satisfacción por encuesta —
// MISMA fuente para el panel de Reportería y para el Asistente IA (así el bot
// nunca contradice la pantalla). Devuelve (resumen, encuestas).
func (p *Proxy) collectSatisfaccionReporte(ctx context.Context, tipoFiltro, codeFiltro string) (map[string]any, []map[string]any, error) {
	sresp, err := p.cli.Surveys.Search(ctx, &satisfactioncommonpb.SearchRequest{
		FilterGroups: []*satisfactioncommonpb.FilterGroup{{
			Filters: []*satisfactioncommonpb.Filter{{PropertyName: "active", Operator: satisfactioncommonpb.FilterOperator_EQ, Values: []string{"true"}}},
		}},
		Properties: []string{"id", "code", "title", "trigger_kind", "key_id", "published"},
		Limit:      200,
	})
	if err != nil {
		return nil, nil, err
	}

	// Denominador por TIPO (intentos submitted de ese tipo, sumando colegios):
	// se computa una vez por tipo con GetColegioComparativo. Cache.
	tipoAttempts := map[string]int32{}
	attemptsDeTipo := func(tipo string) int32 {
		if v, ok := tipoAttempts[tipo]; ok {
			return v
		}
		var total int32
		if resp, e := p.cli.Analytics.GetColegioComparativo(ctx, &analyticsgrpcpb.GetColegioComparativoRequest{ExamTypeCode: tipo}); e == nil {
			for _, it := range resp.GetItems() {
				if isQAColegio(it.GetSchoolName()) {
					continue
				}
				total += it.GetAttempts()
			}
		}
		tipoAttempts[tipo] = total
		return total
	}
	// Denominador por LLAVE (intentos submitted de esa key). Cache por colegio.
	keyAttempts := func(keyID string) int32 {
		if keyID == "" {
			return 0
		}
		kr, e := p.cli.Keys.GetKey(ctx, &keysgrpcpb.GetKeyRequest{Id: keyID})
		if e != nil || kr.GetKey() == nil {
			return 0
		}
		var n int32
		if at, e2 := p.cli.Attempts.ListByColegio(ctx, &examsgrpcpb.ListAttemptsByColegioRequest{SchoolId: kr.GetKey().GetSchoolId()}); e2 == nil {
			for _, a := range at.GetItems() {
				if a.GetSubmittedAt() != nil && strings.EqualFold(a.GetKeyId(), keyID) {
					n++
				}
			}
		}
		return n
	}

	tipoLabel := map[string]string{"simulacro": "Simulacro", "vocacional": "Vocacional", "estilos": "Estilos", "habitos": "Estilos"}
	out := make([]map[string]any, 0)
	var totResp, totCSATw, totCSATn float64
	npsSum, npsN := 0.0, 0.0
	for _, res := range sresp.GetResults() {
		props := map[string]any{}
		if res.GetProperties() != nil {
			props = res.GetProperties().AsMap()
		}
		published, _ := props["published"].(bool)
		if !published { // solo publicadas para el reporte
			continue
		}
		trigger := strings.ToLower(asString(props["trigger_kind"]))
		code := asString(props["code"])
		title := asString(props["title"])
		keyID := asString(props["key_id"])
		if tipoFiltro != "" && trigger != tipoFiltro {
			continue
		}
		if codeFiltro != "" && !strings.EqualFold(code, codeFiltro) {
			continue
		}
		m, merr := p.cli.Responses.GetMetrics(ctx, &satisfactiongrpcpb.GetMetricsRequest{SurveyId: res.GetId()})
		if merr != nil {
			continue
		}
		resp := int(m.GetTotalResponses())
		// CSAT = promedio de las preguntas de escala (1-5). NPS = promedio de nps.
		var scaleSum, scaleN float64
		var npsSurvey, npsSurveyN float64
		preguntas := []map[string]any{}
		for _, q := range m.GetPerQuestion() {
			pq := map[string]any{"kind": q.GetKind(), "count": q.GetCount()}
			if q.GetHasAverage() {
				pq["promedio"] = round1(float64(q.GetAverage()))
				if q.GetKind() == "scale" {
					scaleSum += float64(q.GetAverage())
					scaleN++
				}
			}
			if q.GetHasNps() {
				pq["nps"] = q.GetNps()
				npsSurvey += float64(q.GetNps())
				npsSurveyN++
			}
			if d := q.GetDistribution(); len(d) > 0 {
				pq["distribucion"] = d
			}
			preguntas = append(preguntas, pq)
		}
		row := map[string]any{
			"code": code, "titulo": title, "tipo": tipoLabel[trigger], "trigger": trigger,
			"atada_a_llave": keyID != "", "respuestas": resp, "preguntas": preguntas,
		}
		if scaleN > 0 {
			csat := round1(scaleSum / scaleN)
			row["csat_1a5"] = csat
			row["csat_pct"] = math.Round((csat - 1) / 4 * 100)
			totCSATw += csat * float64(resp)
			totCSATn += float64(resp)
		}
		if npsSurveyN > 0 {
			row["nps"] = math.Round(npsSurvey / npsSurveyN)
			npsSum += npsSurvey / npsSurveyN
			npsN++
		}
		// Tasa de respuesta con denominador correcto.
		var elegibles int32
		if keyID != "" {
			elegibles = keyAttempts(keyID)
		} else if trigger != "" {
			elegibles = attemptsDeTipo(trigger)
		}
		if elegibles > 0 {
			row["elegibles"] = elegibles
			row["tasa_respuesta"] = math.Round(math.Min(100, float64(resp)/float64(elegibles)*100))
		} else {
			row["tasa_respuesta"] = nil
		}
		totResp += float64(resp)
		out = append(out, row)
	}
	// Orden por respuestas desc.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			ri, _ := out[i]["respuestas"].(int)
			rj, _ := out[j]["respuestas"].(int)
			if rj > ri {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	resumen := map[string]any{
		"encuestas_publicadas": len(out),
		"total_respuestas":     int(totResp),
	}
	if totCSATn > 0 {
		resumen["csat_promedio_1a5"] = round1(totCSATw / totCSATn)
	}
	if npsN > 0 {
		resumen["nps_promedio"] = math.Round(npsSum / npsN)
	}
	return resumen, out, nil
}

func (p *Proxy) getSurveyMetrics(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Responses.GetMetrics(r.Context(), &satisfactiongrpcpb.GetMetricsRequest{
		SurveyId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	perQuestion := make([]map[string]any, 0, len(resp.GetPerQuestion()))
	for _, q := range resp.GetPerQuestion() {
		dist := q.GetDistribution()
		if dist == nil {
			dist = map[string]int32{}
		}
		var avg any
		if q.GetHasAverage() {
			avg = q.GetAverage()
		}
		var nps any
		if q.GetHasNps() {
			nps = q.GetNps()
		}
		perQuestion = append(perQuestion, map[string]any{
			"question_id":  q.GetQuestionId(),
			"kind":         q.GetKind(),
			"count":        q.GetCount(),
			"average":      avg,
			"has_average":  q.GetHasAverage(),
			"nps":          nps,
			"has_nps":      q.GetHasNps(),
			"distribution": dist,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"survey_id":       resp.GetSurveyId(),
		"total_responses": resp.GetTotalResponses(),
		"per_question":    perQuestion,
	})
}

// ====================== JSON mappers ======================

func protoSurveyToJSON(s *satisfactiongrpcpb.Survey) map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"id":           s.GetId(),
		"code":         s.GetCode(),
		"title":        s.GetTitle(),
		"description":  s.GetDescription(),
		"target_role":  s.GetTargetRole(),
		"trigger_kind": s.GetTriggerKind(),
		"key_id":       s.GetKeyId(),
		"version":      s.GetVersion(),
		"published":    s.GetPublished(),
		"active":       s.GetActive(),
		"created_at":   optionalTimestamp(s.GetCreatedAt()),
		"updated_at":   optionalTimestamp(s.GetUpdatedAt()),
	}
}

func protoSurveyQuestionToJSON(q *satisfactiongrpcpb.Question) map[string]any {
	if q == nil {
		return nil
	}
	return map[string]any{
		"id":           q.GetId(),
		"survey_id":    q.GetSurveyId(),
		"text":         q.GetText(),
		"kind":         q.GetKind(),
		"sort_order":   q.GetSortOrder(),
		"options_json": q.GetOptionsJson(),
		"required":     q.GetRequired(),
	}
}

func protoSurveyResponseToJSON(r *satisfactiongrpcpb.Response) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"id":              r.GetId(),
		"survey_id":       r.GetSurveyId(),
		"user_id":         r.GetUserId(),
		"exam_attempt_id": r.GetExamAttemptId(),
		"submitted_at":    optionalTimestamp(r.GetSubmittedAt()),
	}
}
