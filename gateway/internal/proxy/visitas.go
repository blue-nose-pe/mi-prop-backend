// Visitas — proxy CRUD a users_service.VisitaService.
//
// Rutas:
//   POST   /api/visitas
//   GET    /api/visitas
//   GET    /api/visitas/{id}
//   PATCH  /api/visitas/{id}
//
// Convención de PATCH:
//   - status, notes: "" significa "no tocar"
//   - completed_at:
//       * sin enviar     = no tocar
//       * "1900-01-01T00:00:00Z" = limpiar a NULL (sentinel)
//       * cualquier otro = setear ese valor
package proxy

import (
	"net/http"

	usersgrpcpb "users_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *Proxy) RegisterVisitas(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/visitas", p.createVisita)
	mux.HandleFunc("GET /api/visitas", p.listVisitas)
	mux.HandleFunc("GET /api/visitas/{id}", p.getVisita)
	mux.HandleFunc("PATCH /api/visitas/{id}", p.updateVisita)
}

type createVisitaRequest struct {
	AsesorUserID string `json:"asesor_user_id"`
	SchoolID     string `json:"school_id"`
	ScheduledAt  string `json:"scheduled_at"`
	CompletedAt  string `json:"completed_at"`
	Status       string `json:"status"`
	Notes        string `json:"notes"`
}

func (p *Proxy) createVisita(w http.ResponseWriter, r *http.Request) {
	var in createVisitaRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	scheduledAt, err := parseRFC3339(in.ScheduledAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "scheduled_at: " + err.Error()})
		return
	}
	completedAt, err := parseRFC3339(in.CompletedAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "completed_at: " + err.Error()})
		return
	}
	req := &usersgrpcpb.CreateVisitaRequest{
		AsesorUserId: in.AsesorUserID,
		SchoolId:     in.SchoolID,
		ScheduledAt:  scheduledAt,
		CompletedAt:  completedAt,
		Status:       in.Status,
		Notes:        in.Notes,
	}
	resp, err := p.cli.Visitas.CreateVisita(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"visita": protoVisitaToJSON(resp.GetVisita())})
}

type updateVisitaRequest struct {
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	ScheduledAt string `json:"scheduled_at"`
	CompletedAt string `json:"completed_at"`
}

func (p *Proxy) updateVisita(w http.ResponseWriter, r *http.Request) {
	var in updateVisitaRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	scheduledAt, err := parseRFC3339(in.ScheduledAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "scheduled_at: " + err.Error()})
		return
	}
	var completedAt *timestamppb.Timestamp
	if in.CompletedAt != "" {
		completedAt, err = parseRFC3339(in.CompletedAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "completed_at: " + err.Error()})
			return
		}
	}
	resp, err := p.cli.Visitas.UpdateVisita(r.Context(), &usersgrpcpb.UpdateVisitaRequest{
		Id:          r.PathValue("id"),
		Status:      in.Status,
		Notes:       in.Notes,
		ScheduledAt: scheduledAt,
		CompletedAt: completedAt,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"visita": protoVisitaToJSON(resp.GetVisita())})
}

func (p *Proxy) getVisita(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Visitas.GetVisita(r.Context(), &usersgrpcpb.GetVisitaRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	v := resp.GetVisita()
	if v == nil {
		writeNotFound(w, "visita")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"visita": protoVisitaToJSON(v)})
}

func (p *Proxy) listVisitas(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseRFC3339(q.Get("from"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "from: " + err.Error()})
		return
	}
	to, err := parseRFC3339(q.Get("to"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "to: " + err.Error()})
		return
	}
	// Mismo cierre que /api/asesores/{id}/keys: el filtro asesor_user_id
	// se fuerza al caller salvo que sea superadmin (que puede pasar
	// cualquier valor o "" para listar todo).
	asesorID := enforceAsesorScope(r, q.Get("asesor_user_id"))
	resp, err := p.cli.Visitas.ListVisitas(r.Context(), &usersgrpcpb.ListVisitasRequest{
		AsesorUserId: asesorID,
		SchoolId:     q.Get("school_id"),
		Status:       q.Get("status"),
		From:         from,
		To:           to,
		Limit:        parseUint32Query(q.Get("limit"), 100),
		Offset:       parseUint32Query(q.Get("offset"), 0),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, v := range resp.GetItems() {
		items = append(items, protoVisitaToJSON(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": resp.GetTotal(),
	})
}

func protoVisitaToJSON(v *usersgrpcpb.Visita) map[string]any {
	if v == nil {
		return nil
	}
	return map[string]any{
		"id":             v.GetId(),
		"asesor_user_id": v.GetAsesorUserId(),
		"school_id":      v.GetSchoolId(),
		"scheduled_at":   optionalTimestamp(v.GetScheduledAt()),
		"completed_at":   optionalTimestamp(v.GetCompletedAt()),
		"status":         v.GetStatus(),
		"notes":          v.GetNotes(),
		"created_at":     optionalTimestamp(v.GetCreatedAt()),
		"updated_at":     optionalTimestamp(v.GetUpdatedAt()),
	}
}
