// HubSpot handlers — proxy a hubspot-service.
//
// Rutas REST (admin-only en su mayoría):
//   POST /api/hubspot/contacts
//   GET  /api/hubspot/contacts/by-dni/{dni}
//   POST /api/hubspot/otp
//   POST /api/hubspot/results
//   POST /api/hubspot/schools
//   POST /api/hubspot/asesores
//   GET  /api/hubspot/failed
//   POST /api/hubspot/retry
package proxy

import (
	"net/http"
	"strconv"

	hubspotgrpcpb "hubspot_service/proto/gen"
)

func (p *Proxy) RegisterHubspot(mux *http.ServeMux) {
	// Fix auditoría 2026-07-02: TODAS las rutas /api/hubspot/* son herramientas de
	// ops/admin (el front NO las llama; el sync real post-examen/lead/alumno usa el
	// gRPC interno p.cli.Hubspot desde exams.go/landing.go/users.go, no estas rutas
	// HTTP). Antes NO tenían ningún gate de permiso y hubspot_service tampoco tiene
	// permission_map → cualquier autenticado, incluso un alumno con token OTP, podía
	// enviar OTPs arbitrarios (spam/phishing con identidad UCSP), leer syncs fallidos
	// con PII, forjar resultados de examen en el CRM o escribir contactos/colegios.
	// Se exige admin-marker en todas.
	mux.HandleFunc("POST /api/hubspot/contacts", p.hubspotAdminOnly(p.upsertHubspotContact))
	mux.HandleFunc("GET /api/hubspot/contacts/by-dni/{dni}", p.hubspotAdminOnly(p.getHubspotContactByDNI))
	mux.HandleFunc("POST /api/hubspot/otp", p.hubspotAdminOnly(p.sendHubspotOTP))
	mux.HandleFunc("POST /api/hubspot/results", p.hubspotAdminOnly(p.syncHubspotResult))
	mux.HandleFunc("POST /api/hubspot/schools", p.hubspotAdminOnly(p.upsertHubspotSchool))
	mux.HandleFunc("POST /api/hubspot/asesores", p.hubspotAdminOnly(p.upsertHubspotAsesor))
	mux.HandleFunc("GET /api/hubspot/failed", p.hubspotAdminOnly(p.listHubspotFailed))
	mux.HandleFunc("POST /api/hubspot/retry", p.hubspotAdminOnly(p.retryHubspotJob))
}

// hubspotAdminOnly gatea una ruta /api/hubspot/* a admin/superadmin (admin-marker
// db_users.permission_group.write). El JWT ya lo valida jwtmw; esto añade la
// autorización que faltaba por completo.
func (p *Proxy) hubspotAdminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isSuperadminContext(r) && !hasPermission(r, "db_users.permission_group.write") {
			writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "FORBIDDEN", Message: "solo administradores pueden operar HubSpot"})
			return
		}
		next(w, r)
	}
}

type upsertHubspotContactRequest struct {
	DNI        string            `json:"dni"`
	Email      string            `json:"email"`
	FirstName  string            `json:"first_name"`
	LastName   string            `json:"last_name"`
	Phone      string            `json:"phone"`
	SchoolName string            `json:"school_name"`
	Grade      string            `json:"grade"`
	UserID     string            `json:"user_id"`
	Extra      map[string]string `json:"extra"`
}

func (p *Proxy) upsertHubspotContact(w http.ResponseWriter, r *http.Request) {
	var in upsertHubspotContactRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Hubspot.UpsertContact(r.Context(), &hubspotgrpcpb.UpsertContactRequest{
		Dni:        in.DNI,
		Email:      in.Email,
		FirstName:  in.FirstName,
		LastName:   in.LastName,
		Phone:      in.Phone,
		SchoolName: in.SchoolName,
		Grade:      in.Grade,
		UserId:     in.UserID,
		Extra:      in.Extra,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"record_id": resp.GetRecordId()})
}

func (p *Proxy) getHubspotContactByDNI(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Hubspot.GetContactByDNI(r.Context(), &hubspotgrpcpb.GetContactByDNIRequest{
		Dni: r.PathValue("dni"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"record_id": resp.GetRecordId()})
}

type sendHubspotOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

func (p *Proxy) sendHubspotOTP(w http.ResponseWriter, r *http.Request) {
	var in sendHubspotOTPRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Hubspot.SendOTP(r.Context(), &hubspotgrpcpb.SendOTPRequest{
		Email: in.Email,
		Otp:   in.OTP,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type syncHubspotResultRequest struct {
	DNI             string  `json:"dni"`
	ExamTypeCode    string  `json:"exam_type_code"`
	Score           float64 `json:"score"`
	MaxScore        float64 `json:"max_score"`
	AttemptID       string  `json:"attempt_id"`
	SubmittedAt     string `json:"submitted_at"`
	ContactRecordID string `json:"contact_record_id"`
}

func (p *Proxy) syncHubspotResult(w http.ResponseWriter, r *http.Request) {
	var in syncHubspotResultRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Hubspot.SyncExamResult(r.Context(), &hubspotgrpcpb.SyncExamResultRequest{
		Dni:             in.DNI,
		ExamTypeCode:    in.ExamTypeCode,
		Score:           in.Score,
		MaxScore:        in.MaxScore,
		AttemptId:       in.AttemptID,
		SubmittedAt:     in.SubmittedAt,
		ContactRecordId: in.ContactRecordID,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type upsertHubspotSchoolRequest struct {
	SchoolID       string `json:"school_id"`
	Email          string `json:"email"`
	Nombre         string `json:"nombre"`
	PersonalACargo string `json:"personal_a_cargo"`
}

func (p *Proxy) upsertHubspotSchool(w http.ResponseWriter, r *http.Request) {
	var in upsertHubspotSchoolRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Hubspot.UpsertSchool(r.Context(), &hubspotgrpcpb.UpsertSchoolRequest{
		SchoolId:       in.SchoolID,
		Email:          in.Email,
		Nombre:         in.Nombre,
		PersonalACargo: in.PersonalACargo,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type upsertHubspotAsesorRequest struct {
	AsesorUserID string `json:"asesor_user_id"`
	Email        string `json:"email"`
	Nombre       string `json:"nombre"`
	Bio          string `json:"bio"`
}

func (p *Proxy) upsertHubspotAsesor(w http.ResponseWriter, r *http.Request) {
	var in upsertHubspotAsesorRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Hubspot.UpsertAsesor(r.Context(), &hubspotgrpcpb.UpsertAsesorRequest{
		AsesorUserId: in.AsesorUserID,
		Email:        in.Email,
		Nombre:       in.Nombre,
		Bio:          in.Bio,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) listHubspotFailed(w http.ResponseWriter, r *http.Request) {
	limit := uint32(50)
	offset := uint32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			limit = uint32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			offset = uint32(n)
		}
	}
	resp, err := p.cli.Hubspot.GetFailedSyncs(r.Context(), &hubspotgrpcpb.GetFailedSyncsRequest{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		items = append(items, map[string]any{
			"queue":         it.GetQueue(),
			"job_id":        it.GetJobId(),
			"failed_reason": it.GetFailedReason(),
			"payload":       it.GetPayload(),
			"failed_at":     it.GetFailedAt(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type retryHubspotJobRequest struct {
	Queue string `json:"queue"`
	JobID string `json:"job_id"`
}

func (p *Proxy) retryHubspotJob(w http.ResponseWriter, r *http.Request) {
	var in retryHubspotJobRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Hubspot.RetryFailed(r.Context(), &hubspotgrpcpb.RetryFailedRequest{
		Queue: in.Queue,
		JobId: in.JobID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": resp.GetOk()})
}
