package proxy

import (
	"context"
	"net/http"
	"strconv"
	"time"

	hubspotgrpcpb "hubspot_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"
)

// RegisterLanding registra las rutas PUBLICAS de la landing "Preparate"
// (simulacro masivo). Van bajo /api/public/* → el middleware JWT las saltea
// (ver jwtSkip en cmd/server/main.go): el lead es un contacto anonimo que
// todavia no tiene cuenta.
func (p *Proxy) RegisterLanding(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/public/leads", p.createPublicLead)
}

type createPublicLeadRequest struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	DNI            string `json:"dni"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	GraduationYear int32  `json:"graduation_year"`
	SchoolText     string `json:"school_text"`
	KeyCode        string `json:"key_code"`
	TermsAccepted  bool   `json:"terms_accepted"`
	DataProcessing bool   `json:"data_processing"`
}

// createPublicLead persiste un lead de la landing. Espejo del formulario de
// preparate.ucsp.edu.pe: nombres, apellido, dni, celular, correo, anio de
// egreso, colegio (texto libre) y los dos consentimientos.
//
// El sync del lead a HubSpot (portal UCSP 9013951) lo hace un paso posterior
// (hubspot_service.SyncLead) — aqui solo se persiste, fiel a prod donde el
// lead vive primero en la BD.
func (p *Proxy) createPublicLead(w http.ResponseWriter, r *http.Request) {
	var in createPublicLeadRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "BAD_BODY", Message: err.Error(),
		})
		return
	}
	if in.FirstName == "" || in.LastName == "" || in.Email == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR",
			Message: "first_name, last_name y email son obligatorios",
		})
		return
	}

	resp, err := p.cli.Leads.CreateLead(r.Context(), &usersgrpcpb.CreateLeadRequest{
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		Dni:            in.DNI,
		Phone:          in.Phone,
		Email:          in.Email,
		GraduationYear: in.GraduationYear,
		SchoolText:     in.SchoolText,
		KeyCode:        in.KeyCode,
		TermsAccepted:  in.TermsAccepted,
		DataProcessing: in.DataProcessing,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	// Sync del lead a HubSpot (portal UCSP 9013951) fire-and-forget: el lead
	// ya quedo persistido; si HubSpot esta lento/caido no bloqueamos la
	// respuesta al alumno. Contexto propio (el del request muere al responder).
	if in.Email != "" {
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			year := ""
			if in.GraduationYear > 0 {
				year = strconv.Itoa(int(in.GraduationYear))
			}
			_, _ = p.cli.Hubspot.SyncLead(bg, &hubspotgrpcpb.SyncLeadRequest{
				Email:          in.Email,
				FirstName:      in.FirstName,
				LastName:       in.LastName,
				DocumentNumber: in.DNI,
				Phone:          in.Phone,
				GraduationYear: year,
			})
		}()
	}

	writeJSON(w, http.StatusCreated, map[string]any{"lead": protoLeadToJSON(resp.GetLead())})
}

func protoLeadToJSON(l *usersgrpcpb.Lead) map[string]any {
	if l == nil {
		return nil
	}
	return map[string]any{
		"id":              l.GetId(),
		"first_name":      l.GetFirstName(),
		"last_name":       l.GetLastName(),
		"dni":             l.GetDni(),
		"phone":           l.GetPhone(),
		"email":           l.GetEmail(),
		"graduation_year": l.GetGraduationYear(),
		"school_text":     l.GetSchoolText(),
		"origen":          l.GetOrigen(),
		"key_code":        l.GetKeyCode(),
		"terms_accepted":  l.GetTermsAccepted(),
		"data_processing": l.GetDataProcessing(),
		"created_at":      optionalTimestamp(l.GetCreatedAt()),
	}
}
