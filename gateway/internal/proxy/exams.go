// Exams + Questions + Options + ExamQuestions + Attempts handlers.
//
// Rutas REST (todas proxeadas a exams_service):
//   POST   /api/exams
//   GET    /api/exams/{id}
//   PATCH  /api/exams/{id}
//   POST   /api/exams/{id}/publish
//   POST   /api/exams/{id}/deactivate
//   POST   /api/exams/{id}/clone
//   POST   /api/exams/search
//
//   POST   /api/questions
//   GET    /api/questions/{id}
//   PATCH  /api/questions/{id}
//   POST   /api/questions/{id}/deactivate
//   GET    /api/questions/{id}/options
//   POST   /api/questions/{id}/options
//   POST   /api/questions/search
//
//   PATCH  /api/options/{id}
//   DELETE /api/options/{id}
//
//   GET    /api/exams/{id}/questions
//   POST   /api/exams/{id}/questions
//   DELETE /api/exams/{id}/questions/{question_id}
//   POST   /api/exams/{id}/questions/reorder
//
//   POST   /api/attempts
//   GET    /api/attempts/{id}
//   POST   /api/attempts/{id}/answer
//   POST   /api/attempts/{id}/finish
//   GET    /api/users/{id}/attempts
//   GET    /api/exams/{id}/attempts
package proxy

import (
	"net/http"

	examsgrpcpb "exams_service/proto/gen"
	examscommonpb "exams_service/proto/gen/common"
)

func (p *Proxy) RegisterExams(mux *http.ServeMux) {
	// Exams
	mux.HandleFunc("POST /api/exams", p.createExam)
	mux.HandleFunc("POST /api/exams/search", p.searchExams)
	mux.HandleFunc("GET /api/exams/{id}", p.getExam)
	mux.HandleFunc("PATCH /api/exams/{id}", p.updateExam)
	mux.HandleFunc("POST /api/exams/{id}/publish", p.publishExam)
	mux.HandleFunc("POST /api/exams/{id}/deactivate", p.deactivateExam)
	mux.HandleFunc("POST /api/exams/{id}/reactivate", p.reactivateExam)
	mux.HandleFunc("POST /api/exams/{id}/clone", p.cloneExam)

	// Questions
	mux.HandleFunc("POST /api/questions", p.createQuestion)
	mux.HandleFunc("POST /api/questions/search", p.searchQuestions)
	mux.HandleFunc("GET /api/questions/{id}", p.getQuestion)
	mux.HandleFunc("PATCH /api/questions/{id}", p.updateQuestion)
	mux.HandleFunc("POST /api/questions/{id}/deactivate", p.deactivateQuestion)
	mux.HandleFunc("GET /api/questions/{id}/options", p.listQuestionOptions)
	mux.HandleFunc("POST /api/questions/{id}/options", p.addQuestionOption)

	// Options
	mux.HandleFunc("PATCH /api/options/{id}", p.updateOption)
	mux.HandleFunc("DELETE /api/options/{id}", p.removeOption)

	// ExamQuestions
	mux.HandleFunc("GET /api/exams/{id}/questions", p.listExamQuestions)
	mux.HandleFunc("POST /api/exams/{id}/questions", p.addExamQuestion)
	mux.HandleFunc("DELETE /api/exams/{id}/questions/{question_id}", p.removeExamQuestion)
	mux.HandleFunc("POST /api/exams/{id}/questions/reorder", p.reorderExamQuestions)

	// Attempts
	mux.HandleFunc("POST /api/attempts", p.startAttempt)
	mux.HandleFunc("GET /api/attempts/{id}", p.getAttempt)
	mux.HandleFunc("POST /api/attempts/{id}/answer", p.answerAttempt)
	mux.HandleFunc("POST /api/attempts/{id}/finish", p.finishAttempt)
	mux.HandleFunc("GET /api/users/{id}/attempts", p.listAttemptsByUser)
	mux.HandleFunc("GET /api/exams/{id}/attempts", p.listAttemptsByExam)
	// Detalle por-pregunta del intento: question_text + option_text +
	// option_is_correct. Lo usa el modal "Resultados del estudiante" para
	// computar correctas/incorrectas en cliente sin cargar el banco de
	// preguntas completo aparte.
	//
	// Vive bajo /api/answers/by-attempt/{id} y NO bajo
	// /api/attempts/{id}/answers porque ese ultimo entra en conflicto con
	// /api/attempts/by-key/{id} en el matching del ServeMux 1.22+ (ambos
	// matchean /api/attempts/by-key/answers, sin que uno sea mas
	// especifico que el otro).
	mux.HandleFunc("GET /api/answers/by-attempt/{id}", p.listAttemptAnswers)
	// `/api/attempts/by-key/{id}` se registra en RegisterKeys porque el
	// path /api/keys/{id}/attempts choca con /api/keys/by-code/{code} en
	// el matching del ServeMux 1.22+.
}

// ====================== EXAMS ======================

type createExamRequest struct {
	ExamTypeCode    string `json:"exam_type_code"`
	SchoolID        string `json:"school_id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	StartAt         string `json:"start_at"`
	EndAt           string `json:"end_at"`
	MaxParticipants int32  `json:"max_participants"`
}

func (p *Proxy) createExam(w http.ResponseWriter, r *http.Request) {
	var in createExamRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	startAt, err := parseRFC3339(in.StartAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "start_at: " + err.Error()})
		return
	}
	endAt, err := parseRFC3339(in.EndAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "end_at: " + err.Error()})
		return
	}
	resp, err := p.cli.Exams.CreateExam(r.Context(), &examsgrpcpb.CreateExamRequest{
		ExamTypeCode:    in.ExamTypeCode,
		SchoolId:        in.SchoolID,
		Code:            in.Code,
		Name:            in.Name,
		StartAt:         startAt,
		EndAt:           endAt,
		MaxParticipants: in.MaxParticipants,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exam": protoExamToJSON(resp.GetExam())})
}

func (p *Proxy) getExam(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Exams.GetExam(r.Context(), &examsgrpcpb.GetExamRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	e := resp.GetExam()
	if e == nil {
		writeNotFound(w, "exam")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exam": protoExamToJSON(e)})
}

type updateExamRequest struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	StartAt         string `json:"start_at"`
	EndAt           string `json:"end_at"`
	MaxParticipants int32  `json:"max_participants"`
}

func (p *Proxy) updateExam(w http.ResponseWriter, r *http.Request) {
	var in updateExamRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	startAt, err := parseRFC3339(in.StartAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "start_at: " + err.Error()})
		return
	}
	endAt, err := parseRFC3339(in.EndAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "end_at: " + err.Error()})
		return
	}
	resp, err := p.cli.Exams.UpdateExam(r.Context(), &examsgrpcpb.UpdateExamRequest{
		Id:              r.PathValue("id"),
		Code:            in.Code,
		Name:            in.Name,
		StartAt:         startAt,
		EndAt:           endAt,
		MaxParticipants: in.MaxParticipants,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exam": protoExamToJSON(resp.GetExam())})
}

func (p *Proxy) publishExam(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Exams.PublishExam(r.Context(), &examsgrpcpb.PublishExamRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) deactivateExam(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Exams.DeactivateExam(r.Context(), &examsgrpcpb.DeactivateExamRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) reactivateExam(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Exams.ReactivateExam(r.Context(), &examsgrpcpb.ReactivateExamRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) cloneExam(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Exams.CloneExam(r.Context(), &examsgrpcpb.CloneExamRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exam": protoExamToJSON(resp.GetExam())})
}

func (p *Proxy) searchExams(w http.ResponseWriter, r *http.Request) {
	req := &examscommonpb.SearchRequest{}
	if err := decodeSearchRequest(r, req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Exams.SearchExams(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*examscommonpb.SearchResult, *examscommonpb.Paging](
		resp.GetTotal(), resp.GetResults(), resp.GetPaging(),
	))
}

// ====================== QUESTIONS ======================

type createQuestionRequest struct {
	Text     string `json:"text"`
	Category string `json:"category"`
	Kind     string `json:"kind"`
}

func (p *Proxy) createQuestion(w http.ResponseWriter, r *http.Request) {
	var in createQuestionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Questions.CreateQuestion(r.Context(), &examsgrpcpb.CreateQuestionRequest{
		Text:     in.Text,
		Category: in.Category,
		Kind:     in.Kind,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"question": protoQuestionToJSON(resp.GetQuestion())})
}

func (p *Proxy) getQuestion(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Questions.GetQuestion(r.Context(), &examsgrpcpb.GetQuestionRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"question": protoQuestionToJSON(resp.GetQuestion())})
}

type updateQuestionRequest struct {
	Text     string `json:"text"`
	Category string `json:"category"`
	Kind     string `json:"kind"`
}

func (p *Proxy) updateQuestion(w http.ResponseWriter, r *http.Request) {
	var in updateQuestionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Questions.UpdateQuestion(r.Context(), &examsgrpcpb.UpdateQuestionRequest{
		Id:       r.PathValue("id"),
		Text:     in.Text,
		Category: in.Category,
		Kind:     in.Kind,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"question": protoQuestionToJSON(resp.GetQuestion())})
}

func (p *Proxy) deactivateQuestion(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Questions.DeactivateQuestion(r.Context(), &examsgrpcpb.DeactivateQuestionRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) listQuestionOptions(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Questions.ListOptions(r.Context(), &examsgrpcpb.ListOptionsRequest{
		QuestionId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(resp.GetOptions()))
	for _, o := range resp.GetOptions() {
		out = append(out, protoQuestionOptionToJSON(o))
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": out})
}

type addQuestionOptionRequest struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
	SortOrder int32  `json:"sort_order"`
}

func (p *Proxy) addQuestionOption(w http.ResponseWriter, r *http.Request) {
	var in addQuestionOptionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Questions.AddOption(r.Context(), &examsgrpcpb.AddOptionRequest{
		QuestionId: r.PathValue("id"),
		Text:       in.Text,
		IsCorrect:  in.IsCorrect,
		SortOrder:  in.SortOrder,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"option": protoQuestionOptionToJSON(resp.GetOption())})
}

func (p *Proxy) searchQuestions(w http.ResponseWriter, r *http.Request) {
	req := &examscommonpb.SearchRequest{}
	if err := decodeSearchRequest(r, req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Questions.SearchQuestions(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*examscommonpb.SearchResult, *examscommonpb.Paging](
		resp.GetTotal(), resp.GetResults(), resp.GetPaging(),
	))
}

// ====================== OPTIONS ======================

type updateOptionRequest struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
	SortOrder int32  `json:"sort_order"`
}

func (p *Proxy) updateOption(w http.ResponseWriter, r *http.Request) {
	var in updateOptionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Questions.UpdateOption(r.Context(), &examsgrpcpb.UpdateOptionRequest{
		Id:        r.PathValue("id"),
		Text:      in.Text,
		IsCorrect: in.IsCorrect,
		SortOrder: in.SortOrder,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) removeOption(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Questions.RemoveOption(r.Context(), &examsgrpcpb.RemoveOptionRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ====================== EXAM ↔ QUESTION ======================

func (p *Proxy) listExamQuestions(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.ExamQs.ListByExam(r.Context(), &examsgrpcpb.ListByExamRequest{ExamId: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		out = append(out, map[string]any{
			"exam_id":     it.GetExamId(),
			"question_id": it.GetQuestionId(),
			"points":      it.GetPoints(),
			"sort_order":  it.GetSortOrder(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type addExamQuestionRequest struct {
	QuestionID string `json:"question_id"`
	Points     int32  `json:"points"`
	SortOrder  int32  `json:"sort_order"`
}

func (p *Proxy) addExamQuestion(w http.ResponseWriter, r *http.Request) {
	var in addExamQuestionRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.ExamQs.AddQuestion(r.Context(), &examsgrpcpb.AddExamQuestionRequest{
		ExamId:     r.PathValue("id"),
		QuestionId: in.QuestionID,
		Points:     in.Points,
		SortOrder:  in.SortOrder,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) removeExamQuestion(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.ExamQs.RemoveQuestion(r.Context(), &examsgrpcpb.RemoveExamQuestionRequest{
		ExamId:     r.PathValue("id"),
		QuestionId: r.PathValue("question_id"),
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type reorderExamQuestionsRequest struct {
	QuestionIDs []string `json:"question_ids"`
}

func (p *Proxy) reorderExamQuestions(w http.ResponseWriter, r *http.Request) {
	var in reorderExamQuestionsRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.ExamQs.Reorder(r.Context(), &examsgrpcpb.ReorderRequest{
		ExamId:      r.PathValue("id"),
		QuestionIds: in.QuestionIDs,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ====================== ATTEMPTS ======================

type startAttemptRequest struct {
	ExamID  string `json:"exam_id"`
	UserID  string `json:"user_id"`
	KeyCode string `json:"key_code"`
}

func (p *Proxy) startAttempt(w http.ResponseWriter, r *http.Request) {
	var in startAttemptRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	// Bug 3 fix: el user_id viene SIEMPRE del JWT del caller, nunca del
	// body. Antes el body controlaba quien era el "owner" del attempt;
	// un estudiante autenticado podia mandar `user_id` de otra persona,
	// consumir su aforo de keys y rendir simulacro en su nombre. Para
	// roles staff que quieran arrancar un attempt en nombre de un alumno,
	// hay que crear un endpoint admin separado con permisos explicitos.
	callerID := userIDFromContext(r)
	if callerID == "" {
		writeJSON(w, http.StatusUnauthorized, errorBody{Status: "error", Code: "UNAUTHENTICATED", Message: "authenticated user required"})
		return
	}
	effectiveUserID := callerID
	if in.UserID != "" && in.UserID != callerID {
		// El caller esta intentando iniciar un attempt en nombre de OTRO
		// user. Hace falta db_exams.exam_attempt.write — caso: proctor o
		// asesor asistiendo a un estudiante en presencial. Superadmin
		// pasa por hasPermission.
		if !hasPermission(r, "db_exams.exam_attempt.write") {
			writeJSON(w, http.StatusForbidden, errorBody{
				Status:  "error",
				Code:    "PERMISSION_DENIED",
				Message: "no tienes permiso para iniciar attempts en nombre de otro user",
			})
			return
		}
		effectiveUserID = in.UserID
	}
	resp, err := p.cli.Attempts.StartAttempt(r.Context(), &examsgrpcpb.StartAttemptRequest{
		ExamId:  in.ExamID,
		UserId:  effectiveUserID,
		KeyCode: in.KeyCode,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempt": protoAttemptToJSON(resp.GetAttempt())})
}

func (p *Proxy) getAttempt(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Attempts.GetAttempt(r.Context(), &examsgrpcpb.GetAttemptRequest{
		AttemptId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	// Ownership del attempt: el dueno SIEMPRE puede verlo (self-service).
	// Para ver el attempt de OTRO user hace falta db_exams.exam_attempt.read
	// (superadmin bypasea automatico). UCSP asigna ese permiso a asesores
	// / coordinadores que necesiten revisar attempts de sus alumnos.
	att := resp.GetAttempt()
	if att != nil && att.GetUserId() != userIDFromContext(r) && !hasPermission(r, "db_exams.exam_attempt.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a este attempt",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempt": protoAttemptToJSON(att)})
}

type answerAttemptRequest struct {
	QuestionID string   `json:"question_id"`
	// SINGLE_CHOICE / TRUE_FALSE / SCALE: una sola opcion.
	OptionID   string   `json:"option_id"`
	// MULTIPLE_CHOICE: lista de opciones marcadas.
	OptionIDs  []string `json:"option_ids"`
	// OPEN_TEXT: respuesta libre.
	AnswerText string   `json:"answer_text"`
}

func (p *Proxy) answerAttempt(w http.ResponseWriter, r *http.Request) {
	var in answerAttemptRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	// Bug 1 fix: solo el dueño del attempt (o superadmin) puede responder.
	if !p.callerOwnsAttempt(r) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "attempt belongs to another user",
		})
		return
	}
	if _, err := p.cli.Attempts.Answer(r.Context(), &examsgrpcpb.AnswerRequest{
		AttemptId:  r.PathValue("id"),
		QuestionId: in.QuestionID,
		OptionId:   in.OptionID,
		OptionIds:  in.OptionIDs,
		AnswerText: in.AnswerText,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) finishAttempt(w http.ResponseWriter, r *http.Request) {
	// Bug 1 fix: solo el dueño del attempt (o superadmin) puede finalizar.
	if !p.callerOwnsAttempt(r) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "attempt belongs to another user",
		})
		return
	}
	resp, err := p.cli.Attempts.FinishAttempt(r.Context(), &examsgrpcpb.FinishAttemptRequest{
		AttemptId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempt": protoAttemptToJSON(resp.GetAttempt())})
}

// callerOwnsAttempt resuelve el ownership del attempt en el path.
// Lookup adicional al exams-service, costo aceptable porque attemptId
// no es enumerable. Devuelve true tambien para superadmin y para users
// con db_exams.exam_attempt.write (que permite operar sobre attempts ajenos
// — caso un proctor o asesor asistiendo a un estudiante en presencial).
func (p *Proxy) callerOwnsAttempt(r *http.Request) bool {
	if hasPermission(r, "db_exams.exam_attempt.write") {
		return true
	}
	caller := userIDFromContext(r)
	if caller == "" {
		return false
	}
	resp, err := p.cli.Attempts.GetAttempt(r.Context(), &examsgrpcpb.GetAttemptRequest{
		AttemptId: r.PathValue("id"),
	})
	if err != nil || resp.GetAttempt() == nil {
		return false
	}
	return resp.GetAttempt().GetUserId() == caller
}

func (p *Proxy) listAttemptsByUser(w http.ResponseWriter, r *http.Request) {
	// Self-scope por defecto: cualquier user ve sus propios attempts.
	// Para listar attempts de OTRO user hace falta db_exams.exam_attempt.read.
	targetID := r.PathValue("id")
	if targetID != userIDFromContext(r) && !hasPermission(r, "db_exams.exam_attempt.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a los attempts de este user",
		})
		return
	}
	resp, err := p.cli.Attempts.ListByUser(r.Context(), &examsgrpcpb.ListAttemptsByUserRequest{
		UserId: targetID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, a := range resp.GetItems() {
		items = append(items, protoAttemptToJSON(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// listAttemptAnswers → AttemptService.ListEnrichedAnswers. Cada item del
// array trae question_text, option_text y option_is_correct, suficiente
// para que el front compute correctas/incorrectas + render por modulo
// sin volver a tocar el banco de preguntas.
func (p *Proxy) listAttemptAnswers(w http.ResponseWriter, r *http.Request) {
	// Bug 1 fix: solo dueño del attempt (o superadmin) ve las respuestas.
	if !p.callerOwnsAttempt(r) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "attempt belongs to another user",
		})
		return
	}
	resp, err := p.cli.Attempts.ListEnrichedAnswers(r.Context(), &examsgrpcpb.ListEnrichedAnswersRequest{
		AttemptId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, a := range resp.GetItems() {
		items = append(items, map[string]any{
			"question_id":       a.GetQuestionId(),
			"question_text":     a.GetQuestionText(),
			"question_category": a.GetQuestionCategory(),
			"question_kind":     a.GetQuestionKind(),
			"option_id":         a.GetOptionId(),
			"option_text":       a.GetOptionText(),
			"option_sort_order": a.GetOptionSortOrder(),
			"option_is_correct": a.GetOptionIsCorrect(),
			"answered_at":       optionalTimestamp(a.GetAnsweredAt()),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (p *Proxy) listAttemptsByExam(w http.ResponseWriter, r *http.Request) {
	// Este endpoint lista TODOS los attempts de un exam, exponiendo
	// nombres y scores. Permission gate: db_exams.exam_attempt.read.
	// UCSP define quien tiene ese permiso via permission_group.
	if !hasPermission(r, "db_exams.exam_attempt.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso db_exams.exam_attempt.read",
		})
		return
	}
	resp, err := p.cli.Attempts.ListByExam(r.Context(), &examsgrpcpb.ListAttemptsByExamRequest{
		ExamId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, a := range resp.GetItems() {
		items = append(items, protoAttemptToJSON(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ====================== JSON mappers ======================

func protoExamToJSON(e *examsgrpcpb.Exam) map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"id":               e.GetId(),
		"exam_type_id":     e.GetExamTypeId(),
		"school_id":        e.GetSchoolId(),
		"parent_exam_id":   e.GetParentExamId(),
		"version":          e.GetVersion(),
		"code":             e.GetCode(),
		"name":             e.GetName(),
		"start_at":         optionalTimestamp(e.GetStartAt()),
		"end_at":           optionalTimestamp(e.GetEndAt()),
		"max_participants": e.GetMaxParticipants(),
		"published":        e.GetPublished(),
		"active":           e.GetActive(),
		"created_at":       optionalTimestamp(e.GetCreatedAt()),
		"updated_at":       optionalTimestamp(e.GetUpdatedAt()),
	}
}

func protoQuestionToJSON(q *examsgrpcpb.Question) map[string]any {
	if q == nil {
		return nil
	}
	return map[string]any{
		"id":         q.GetId(),
		"text":       q.GetText(),
		"category":   q.GetCategory(),
		"kind":       q.GetKind(),
		"active":     q.GetActive(),
		"created_at": optionalTimestamp(q.GetCreatedAt()),
		"updated_at": optionalTimestamp(q.GetUpdatedAt()),
	}
}

func protoQuestionOptionToJSON(o *examsgrpcpb.QuestionOption) map[string]any {
	if o == nil {
		return nil
	}
	return map[string]any{
		"id":          o.GetId(),
		"question_id": o.GetQuestionId(),
		"text":        o.GetText(),
		"is_correct":  o.GetIsCorrect(),
		"sort_order":  o.GetSortOrder(),
	}
}

func protoAttemptToJSON(a *examsgrpcpb.Attempt) map[string]any {
	if a == nil {
		return nil
	}
	return map[string]any{
		"id":           a.GetId(),
		"exam_id":      a.GetExamId(),
		"user_id":      a.GetUserId(),
		"key_id":       a.GetKeyId(),
		"score":        a.GetScore(),
		"max_score":    a.GetMaxScore(),
		"started_at":   optionalTimestamp(a.GetStartedAt()),
		"submitted_at": optionalTimestamp(a.GetSubmittedAt()),
	}
}
