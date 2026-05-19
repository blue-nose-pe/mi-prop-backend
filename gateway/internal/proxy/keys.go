// Keys handlers — proxy a keys-service.
//
// Rutas REST:
//   POST   /api/keys
//   GET    /api/keys/{id}
//   PATCH  /api/keys/{id}
//   GET    /api/keys/by-code/{code}
//   POST   /api/keys/{id}/deactivate
//   POST   /api/keys/validate
//   GET    /api/asesores/{id}/keys
//   GET    /api/colegios/{id}/keys
//   POST   /api/keys/search
//   GET    /api/attempts/by-key/{id} - attempts iniciados con esta key
//                                       (registrado aqui porque consulta
//                                        exams-service via key_id; bajo
//                                        /api/keys/{id}/attempts choca con
//                                        /api/keys/by-code/{code} en
//                                        Go ServeMux 1.22+)
package proxy

import (
	"net/http"

	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	keyscommonpb "keys_service/proto/gen/common"
)

func (p *Proxy) RegisterKeys(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/keys", p.generateKey)
	mux.HandleFunc("POST /api/keys/search", p.searchKeys)
	mux.HandleFunc("POST /api/keys/validate", p.validateKey)
	mux.HandleFunc("GET /api/keys/by-code/{code}", p.getKeyByCode)
	mux.HandleFunc("GET /api/keys/{id}", p.getKey)
	mux.HandleFunc("PATCH /api/keys/{id}", p.updateKey)
	mux.HandleFunc("POST /api/keys/{id}/deactivate", p.deactivateKey)
	mux.HandleFunc("GET /api/attempts/by-key/{id}", p.listAttemptsByKey)
	mux.HandleFunc("GET /api/asesores/{id}/keys", p.listKeysByAsesor)
	mux.HandleFunc("GET /api/colegios/{id}/keys", p.listKeysByColegio)
}

// listAttemptsByKey — GET /api/attempts/by-key/{id}
// Lista los attempts iniciados con la key indicada. Pega a exams-service
// (AttemptService.ListByKey) porque el join real esta en
// db_exams.exam_attempt.key_id. El path vive bajo /api/attempts en vez de
// /api/keys/{id}/attempts porque ese ultimo conflictaria con
// /api/keys/by-code/{code} en Go ServeMux 1.22+ (ambiguity rule).
func (p *Proxy) listAttemptsByKey(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Attempts.ListByKey(r.Context(), &examsgrpcpb.ListAttemptsByKeyRequest{
		KeyId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, a := range resp.GetItems() {
		items = append(items, map[string]any{
			"id":           a.GetId(),
			"exam_id":      a.GetExamId(),
			"user_id":      a.GetUserId(),
			"key_id":       a.GetKeyId(),
			"score":        a.GetScore(),
			"max_score":    a.GetMaxScore(),
			"started_at":   optionalTimestamp(a.GetStartedAt()),
			"submitted_at": optionalTimestamp(a.GetSubmittedAt()),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

type generateKeyRequest struct {
	Code         string `json:"code"`
	ExamTypeID   int32  `json:"exam_type_id"`
	SchoolID     string `json:"school_id"`
	AsesorUserID string `json:"asesor_user_id"`
	Mode         string `json:"mode"`
	Grade        string `json:"grade"`
	Section      string `json:"section"`
	ValidFrom    string `json:"valid_from"`
	ValidTo      string `json:"valid_to"`
	MaxUses      int32  `json:"max_uses"`
}

func (p *Proxy) generateKey(w http.ResponseWriter, r *http.Request) {
	var in generateKeyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	from, err := parseRFC3339(in.ValidFrom)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "valid_from: " + err.Error()})
		return
	}
	to, err := parseRFC3339(in.ValidTo)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "valid_to: " + err.Error()})
		return
	}
	resp, err := p.cli.Keys.GenerateKey(r.Context(), &keysgrpcpb.GenerateKeyRequest{
		Code:         in.Code,
		ExamTypeId:   in.ExamTypeID,
		SchoolId:     in.SchoolID,
		AsesorUserId: in.AsesorUserID,
		Mode:         in.Mode,
		Grade:        in.Grade,
		Section:      in.Section,
		ValidFrom:    from,
		ValidTo:      to,
		MaxUses:      in.MaxUses,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(resp.GetKey())})
}

func (p *Proxy) getKey(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Keys.GetKey(r.Context(), &keysgrpcpb.GetKeyRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	k := resp.GetKey()
	if k == nil {
		writeNotFound(w, "key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(k)})
}

type updateKeyRequest struct {
	Grade     string `json:"grade"`
	Section   string `json:"section"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
	MaxUses   int32  `json:"max_uses"`
}

func (p *Proxy) updateKey(w http.ResponseWriter, r *http.Request) {
	var in updateKeyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	from, err := parseRFC3339(in.ValidFrom)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "valid_from: " + err.Error()})
		return
	}
	to, err := parseRFC3339(in.ValidTo)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "valid_to: " + err.Error()})
		return
	}
	resp, err := p.cli.Keys.UpdateKey(r.Context(), &keysgrpcpb.UpdateKeyRequest{
		Id:        r.PathValue("id"),
		Grade:     in.Grade,
		Section:   in.Section,
		ValidFrom: from,
		ValidTo:   to,
		MaxUses:   in.MaxUses,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(resp.GetKey())})
}

func (p *Proxy) getKeyByCode(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Keys.GetByCode(r.Context(), &keysgrpcpb.GetByCodeRequest{Code: r.PathValue("code")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	k := resp.GetKey()
	if k == nil {
		writeNotFound(w, "key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(k)})
}

func (p *Proxy) deactivateKey(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Keys.DeactivateKey(r.Context(), &keysgrpcpb.DeactivateKeyRequest{Id: r.PathValue("id")}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type validateKeyRequest struct {
	Code string `json:"code"`
}

func (p *Proxy) validateKey(w http.ResponseWriter, r *http.Request) {
	var in validateKeyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Keys.ValidateKey(r.Context(), &keysgrpcpb.ValidateKeyRequest{Code: in.Code})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	k := resp.GetKey()
	if k == nil {
		writeNotFound(w, "key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(k)})
}

func (p *Proxy) listKeysByAsesor(w http.ResponseWriter, r *http.Request) {
	// Un asesor solo puede consultar sus propias keys; el path id se ignora
	// si no coincide con el caller. Superadmins pasan por aqui sin cambios.
	asesorID := enforceAsesorScope(r, r.PathValue("id"))
	resp, err := p.cli.Keys.ListByAsesor(r.Context(), &keysgrpcpb.ListByAsesorRequest{
		AsesorUserId: asesorID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, k := range resp.GetItems() {
		items = append(items, protoKeyToJSON(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (p *Proxy) listKeysByColegio(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Keys.ListByColegio(r.Context(), &keysgrpcpb.ListByColegioRequest{
		SchoolId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, k := range resp.GetItems() {
		items = append(items, protoKeyToJSON(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (p *Proxy) searchKeys(w http.ResponseWriter, r *http.Request) {
	req := &keyscommonpb.SearchRequest{}
	if err := decodeSearchRequest(r, req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Keys.SearchKeys(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*keyscommonpb.SearchResult, *keyscommonpb.Paging](
		resp.GetTotal(), resp.GetResults(), resp.GetPaging(),
	))
}

func protoKeyToJSON(k *keysgrpcpb.Key) map[string]any {
	if k == nil {
		return nil
	}
	return map[string]any{
		"id":             k.GetId(),
		"code":           k.GetCode(),
		"exam_type_id":   k.GetExamTypeId(),
		"school_id":      k.GetSchoolId(),
		"asesor_user_id": k.GetAsesorUserId(),
		"mode":           k.GetMode(),
		"grade":          k.GetGrade(),
		"section":        k.GetSection(),
		"valid_from":     optionalTimestamp(k.GetValidFrom()),
		"valid_to":       optionalTimestamp(k.GetValidTo()),
		"max_uses":       k.GetMaxUses(),
		"current_uses":   k.GetCurrentUses(),
		"active":         k.GetActive(),
		"created_at":     optionalTimestamp(k.GetCreatedAt()),
		"updated_at":     optionalTimestamp(k.GetUpdatedAt()),
	}
}
