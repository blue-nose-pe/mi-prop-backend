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
	"net/http"
	"strconv"

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
	mux.HandleFunc("GET /api/surveys/{id}/questions", p.listSurveyQuestions)
	mux.HandleFunc("POST /api/surveys/{id}/questions", p.addSurveyQuestion)
	mux.HandleFunc("GET /api/surveys/{id}/metrics", p.getSurveyMetrics)

	// Survey questions (recurso independiente, ID propio)
	mux.HandleFunc("PATCH /api/survey-questions/{id}", p.updateSurveyQuestion)
	mux.HandleFunc("DELETE /api/survey-questions/{id}", p.removeSurveyQuestion)

	// Survey responses
	mux.HandleFunc("POST /api/survey-responses", p.submitSurveyResponse)
	mux.HandleFunc("GET /api/survey-responses/{id}", p.getSurveyResponse)
}

// ====================== SURVEYS ======================

type createSurveyRequest struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	TargetRole  string `json:"target_role"`
	TriggerKind string `json:"trigger_kind"`
}

func (p *Proxy) createSurvey(w http.ResponseWriter, r *http.Request) {
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
}

func (p *Proxy) updateSurvey(w http.ResponseWriter, r *http.Request) {
	var in updateSurveyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Surveys.UpdateSurvey(r.Context(), &satisfactiongrpcpb.UpdateSurveyRequest{
		Id:          r.PathValue("id"),
		Title:       in.Title,
		Description: in.Description,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"survey": protoSurveyToJSON(resp.GetSurvey())})
}

func (p *Proxy) publishSurvey(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Surveys.PublishSurvey(r.Context(), &satisfactiongrpcpb.PublishSurveyRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) deactivateSurvey(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Surveys.DeactivateSurvey(r.Context(), &satisfactiongrpcpb.DeactivateSurveyRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	resp, err := p.cli.Responses.Submit(r.Context(), &satisfactiongrpcpb.SubmitResponseRequest{
		SurveyId:      in.SurveyID,
		UserId:        in.UserID,
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
