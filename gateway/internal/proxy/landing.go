package proxy

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	examsgrpcpb "exams_service/proto/gen"
	hubspotgrpcpb "hubspot_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"

	"google.golang.org/grpc/metadata"
)

// RegisterLanding registra las rutas PUBLICAS de la landing "Preparate"
// (simulacro masivo). Van bajo /api/public/* → el middleware JWT las saltea
// (ver jwtSkip en cmd/server/main.go): el lead es un contacto anonimo que
// todavia no tiene cuenta.
func (p *Proxy) RegisterLanding(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/public/leads", p.createPublicLead)
	// Reporteria del simulacro masivo: lista los leads captados. NO es
	// publico (no va bajo /api/public/) → requiere JWT; users_service lo
	// gatea ademas con analytics.simulacro_masivo.read.
	mux.HandleFunc("GET /api/leads", p.listLeads)
	// Envio masivo del correo de acceso (magic-link) a los leads seleccionados.
	// Staff-only (gateado por permiso del masivo). NO publico.
	mux.HandleFunc("POST /api/leads/enviar-acceso", p.enviarAccesoLeads)
	// Key masiva activa de la campaña (publico): la landing y /ingresar la
	// resuelven para no hardcodear el codigo. El asesor crea la key LAN en el
	// admin y esta devuelve la mas reciente vigente.
	mux.HandleFunc("GET /api/public/active-lan-key", p.getActiveLanKey)
}

func (p *Proxy) getActiveLanKey(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Keys.GetActiveLanKey(r.Context(), &keysgrpcpb.GetActiveLanKeyRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	k := resp.GetKey()
	if k == nil {
		writeNotFound(w, "active-lan-key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(k)})
}

// listLeads - GET /api/leads?search=&key_code=&limit=&offset=
// Alimenta los KPIs del panel "Simulacro Masivo". Devuelve {items, total}.
func (p *Proxy) listLeads(w http.ResponseWriter, r *http.Request) {
	// Siempre fresco: el panel debe ver el estado actual al recargar (no cache).
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	resp, err := p.cli.Leads.ListLeads(r.Context(), &usersgrpcpb.ListLeadsRequest{
		Search:  q.Get("search"),
		KeyCode: q.Get("key_code"),
		Limit:   parseUint32Query(q.Get("limit"), 100),
		Offset:  parseUint32Query(q.Get("offset"), 0),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, l := range resp.GetItems() {
		items = append(items, protoLeadToJSON(l))
	}
	// El panel pide ?enrich=1 para saber quién YA rindió el examen (columna
	// "¿Presentó?"). Es un cruce cross-service, así que solo se hace a demanda.
	if q.Get("enrich") == "1" {
		p.enrichLeadsPresento(r.Context(), items)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": resp.GetTotal()})
}

// enrichLeadsPresento marca cada lead con "presento" (rindió el examen). Cruce:
// acceso_key_code (la LAN que se le envió) → attempts de esa key (exams.ListByKey)
// → user_ids que FINALIZARON → sus emails → match con el email del lead. Eficiente:
// 1 ListByKey por LAN distinta + 1 GetUser por presentante (acotado por presentantes,
// no por lead). Best-effort: si algo falla, el lead queda con presento=false.
func (p *Proxy) enrichLeadsPresento(ctx context.Context, items []map[string]any) {
	// 1. Códigos de key distintos que se enviaron.
	codes := map[string]bool{}
	for _, it := range items {
		if c, _ := it["acceso_key_code"].(string); c != "" {
			codes[c] = true
		}
	}
	if len(codes) == 0 {
		return
	}
	// 2. Por cada código: key_id → ListByKey → user_ids que finalizaron.
	submitted := map[string]bool{}
	for code := range codes {
		kr, err := p.cli.Keys.ValidateKey(ctx, &keysgrpcpb.ValidateKeyRequest{Code: code})
		if err != nil || kr.GetKey() == nil {
			continue
		}
		ar, err := p.cli.Attempts.ListByKey(ctx, &examsgrpcpb.ListAttemptsByKeyRequest{KeyId: kr.GetKey().GetId()})
		if err != nil {
			continue
		}
		for _, a := range ar.GetItems() {
			if a.GetSubmittedAt() != nil {
				submitted[a.GetUserId()] = true
			}
		}
	}
	// 3. user_ids que presentaron → emails (para matchear con el lead).
	presentEmails := map[string]bool{}
	for uid := range submitted {
		u, err := p.cli.Users.GetUser(ctx, &usersgrpcpb.GetUserRequest{Id: uid})
		if err != nil || u.GetUser() == nil {
			continue
		}
		if e := strings.ToLower(u.GetUser().GetEmail()); e != "" {
			presentEmails[e] = true
		}
	}
	// 4. Marcar cada lead.
	for _, it := range items {
		email, _ := it["email"].(string)
		it["presento"] = presentEmails[strings.ToLower(email)]
	}
}

type enviarAccesoLeadsRequest struct {
	KeyCode string   `json:"key_code"`
	LeadIDs []string `json:"lead_ids"`
}

// enviarAccesoLeads - POST /api/leads/enviar-acceso
// Envio masivo del correo de acceso (magic-link) a los leads seleccionados,
// con la key (LAN) indicada. ASINCRONO: valida + encola un goroutine y
// responde 202; el front ve el avance recargando GET /api/leads (los leads
// pasan a "enviado" via acceso_enviado_at). Idempotente: saltea los ya
// enviados. Rate-limit para no saturar SMTP/Resend (el "que no explote").
func (p *Proxy) enviarAccesoLeads(w http.ResponseWriter, r *http.Request) {
	if !hasPermission(r, "analytics.simulacro_masivo.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED",
			Message: "no tienes permiso para el simulacro masivo",
		})
		return
	}
	var in enviarAccesoLeadsRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if in.KeyCode == "" || len(in.LeadIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR",
			Message: "key_code y lead_ids son obligatorios",
		})
		return
	}
	const maxBatch = 2000
	if len(in.LeadIDs) > maxBatch {
		in.LeadIDs = in.LeadIDs[:maxBatch] // cap defensivo
	}

	// 1) Validar la key (LAN) una vez. Para masivo school_id="".
	keyResp, err := p.cli.Keys.ValidateKey(r.Context(), &keysgrpcpb.ValidateKeyRequest{Code: in.KeyCode})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	if keyResp.GetKey() == nil {
		writeJSON(w, http.StatusNotFound, errorBody{Status: "error", Code: "KEY_NOT_FOUND", Message: "La llave indicada no existe o no esta disponible."})
		return
	}
	schoolID := keyResp.GetKey().GetSchoolId()

	// 2) Resolver datos de los leads seleccionados.
	leadsResp, err := p.cli.Leads.GetLeadsByIDs(r.Context(), &usersgrpcpb.GetLeadsByIDsRequest{Ids: in.LeadIDs})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	leads := leadsResp.GetItems()

	// 3) Encolar el envio en background. Propagamos el JWT del caller para que
	//    MarkLeadAccessSent (que exige auth) no falle. RegisterStudentWithKey es
	//    publico (jwtSkip). El front ve el avance recargando GET /api/leads.
	authz := r.Header.Get("Authorization")
	keyCode := in.KeyCode
	go func() {
		sent := 0
		for _, l := range leads {
			if l.GetAccesoEnviadoAt() != nil {
				continue // idempotente: ya tiene acceso enviado
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			if authz != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", authz)
			}
			_, rerr := p.cli.Auth.RegisterStudentWithKey(ctx, &usersgrpcpb.RegisterStudentWithKeyRequest{
				Email:          l.GetEmail(),
				FirstName:      l.GetFirstName(),
				LastName:       l.GetLastName(),
				DocumentNumber: l.GetDni(),
				Phone:          l.GetPhone(),
				SchoolId:       schoolID,
			})
			if rerr != nil {
				log.Printf("[masivo-acceso] register FAIL lead=%s email=%s err=%v", l.GetId(), l.GetEmail(), rerr)
				cancel()
				continue
			}
			if _, merr := p.cli.Leads.MarkLeadAccessSent(ctx, &usersgrpcpb.MarkLeadAccessSentRequest{LeadId: l.GetId(), KeyCode: keyCode}); merr != nil {
				log.Printf("[masivo-acceso] mark FAIL lead=%s err=%v", l.GetId(), merr)
			}
			cancel()
			sent++
			time.Sleep(150 * time.Millisecond) // rate-limit SMTP/Resend
		}
		log.Printf("[masivo-acceso] envio terminado: %d/%d enviados con key=%s", sent, len(leads), keyCode)
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "accepted",
		"queued": len(leads),
	})
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
	// Consentimientos OBLIGATORIOS (Ley 29733 — protección de datos personales
	// del Perú). Sin ambos NO se persiste ni se sincroniza el lead a HubSpot.
	// El front ya los exige (checkboxes requiredTrue), pero este endpoint es
	// PÚBLICO y forjable → se valida también en el server (cumplimiento legal).
	if !in.TermsAccepted || !in.DataProcessing {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "CONSENT_REQUIRED",
			Message: "Debes aceptar los términos y el tratamiento de datos personales.",
		})
		return
	}
	// DNI peruano: exactamente 8 dígitos. Es la llave que une el lead con su
	// resultado del simulacro (lead↔resultado por DNI, fiel a prod).
	if !isNDigits(in.DNI, 8) {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR",
			Message: "El DNI debe tener 8 dígitos.",
		})
		return
	}
	// Celular: el front lo manda en E.164 (+51 + 9 dígitos). Validamos por
	// cantidad de dígitos para aceptar tanto el local (9) como el E.164 (11).
	if d := digitsOnly(in.Phone); d != "" && len(d) != 9 && len(d) != 11 {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR",
			Message: "El celular debe tener 9 dígitos.",
		})
		return
	}
	if !looksLikeEmail(in.Email) {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR",
			Message: "El correo no tiene un formato válido.",
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
		"data_processing":   l.GetDataProcessing(),
		"created_at":        optionalTimestamp(l.GetCreatedAt()),
		"acceso_enviado_at": optionalTimestamp(l.GetAccesoEnviadoAt()),
		"acceso_key_code":   l.GetAccesoKeyCode(),
	}
}

// isNDigits reporta si s tiene exactamente n caracteres y todos son dígitos.
func isNDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// digitsOnly devuelve solo los dígitos de s (descarta '+', espacios, guiones).
// Lo usa la validación del celular para aceptar tanto el local (9) como el
// E.164 que manda el front (+51 + 9 = 11 dígitos).
func digitsOnly(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// looksLikeEmail hace una validación de formato mínima: un solo '@' con texto
// a ambos lados y un '.' válido en el dominio. No pretende ser RFC 5322 — el
// front ya valida con Validators.email; esto es la red de seguridad del server
// para un endpoint público forjable.
func looksLikeEmail(s string) bool {
	at := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			if at != -1 {
				return false // más de un '@'
			}
			at = i
		}
	}
	if at <= 0 || at == len(s)-1 {
		return false
	}
	for i := at + 2; i < len(s)-1; i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
