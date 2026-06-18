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
	"context"
	"log"
	"net/http"
	"time"

	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	keyscommonpb "keys_service/proto/gen/common"
	usersgrpcpb "users_service/proto/gen"
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
	mux.HandleFunc("GET /api/colegios/{id}/student-key-info", p.studentKeyInfoByColegio)
	mux.HandleFunc("POST /api/admin/keys/resync-all", p.resyncAllKeys)
}

// studentKeyInfoByColegio — GET /api/colegios/{id}/student-key-info
// Devuelve, por cada alumno que usó alguna key del colegio, el grado /
// sección / herramienta de su key MÁS RECIENTE. Grado y sección viven en
// la key, no en el registro del user, así que el front enriquece con esto
// el listado "Ver estudiantes" (antes mostraba grado/sección = "—" fijos).
func (p *Proxy) studentKeyInfoByColegio(w http.ResponseWriter, r *http.Request) {
	schoolID := r.PathValue("id")
	// Scope por colegio (como sus hermanos listKeysByColegio/listStudentsByColegio):
	// un asesor/coordinador solo puede ver info de SUS colegios; superadmin todos.
	if !p.enforceColegioScope(r, schoolID) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a este colegio"})
		return
	}
	resp, err := p.cli.Keys.ListStudentKeyInfoByColegio(r.Context(), &keysgrpcpb.ListByColegioRequest{
		SchoolId: schoolID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	// Items vienen ordenados por used_at DESC → el primero por user_id es el
	// más reciente. Mapeamos exam_type_id → herramienta.
	examTool := map[int32]string{1: "vocacional", 2: "simulacro", 3: "habitos"}
	byUser := make(map[string]map[string]any)
	for _, it := range resp.GetItems() {
		uid := it.GetUserId()
		if _, seen := byUser[uid]; seen {
			continue // ya tenemos el más reciente
		}
		byUser[uid] = map[string]any{
			"grade":   it.GetGrade(),
			"section": it.GetSection(),
			"tool":    examTool[it.GetExamTypeId()],
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": byUser})
}

// listAttemptsByKey — GET /api/attempts/by-key/{id}
// Lista los attempts iniciados con la key indicada. Pega a exams-service
// (AttemptService.ListByKey) porque el join real esta en
// db_exams.exam_attempt.key_id. El path vive bajo /api/attempts en vez de
// /api/keys/{id}/attempts porque ese ultimo conflictaria con
// /api/keys/by-code/{code} en Go ServeMux 1.22+ (ambiguity rule).
func (p *Proxy) listAttemptsByKey(w http.ResponseWriter, r *http.Request) {
	// Bug 1 fix: solo superadmin o el asesor dueño de la key pueden ver
	// sus attempts. Lookup la key para conocer asesor_user_id y school_id.
	keyID := r.PathValue("id")
	if !p.callerOwnsKeyOrSuperadmin(r, keyID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this key's attempts",
		})
		return
	}
	resp, err := p.cli.Attempts.ListByKey(r.Context(), &examsgrpcpb.ListAttemptsByKeyRequest{
		KeyId: keyID,
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
	// ExamID opcional. Si !="" la key se ata a esa version puntual del
	// exam (deterministico). Si "" cae al fallback legacy (front busca
	// "primer exam publicado del exam_type_id"). Ver v0.16.
	ExamID string `json:"exam_id"`
	// MaxAttemptsPerUser opcional. 0 → server normaliza a 1 (un intento
	// por alumno). Si el asesor quiere multi-intento, lo declara explicito
	// (ej 2, 3...). El back enforza este limite en StartAttempt antes de
	// crear el attempt nuevo.
	MaxAttemptsPerUser int32 `json:"max_attempts_per_user"`
}

func (p *Proxy) generateKey(w http.ResponseWriter, r *http.Request) {
	// Crear key (colegio o LAN masiva) exige db_keys.key.write. Antes solo lo
	// gateaba el front (item "Crear" + route guard); el endpoint quedaba
	// abierto a cualquier JWT. Ahora el servidor también lo bloquea.
	if !hasPermission(r, "db_keys.key.write") {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso db_keys.key.write"})
		return
	}
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
	// Lookups pre-Generate: school.hubspot_record_id + name + int_id + asesor.email.
	// keys-service hace pass-through al hubspot.SyncKey en goroutine.
	hids := p.resolveHubspotIDsForKey(r.Context(), in.SchoolID, in.AsesorUserID)

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
		ExamId:       in.ExamID,
		MaxAttemptsPerUser: in.MaxAttemptsPerUser,
		SchoolRecordId:     hids.SchoolRecordID,
		SchoolName:         hids.SchoolName,
		SchoolIntId:        hids.SchoolIntID,
		AsesorEmail:        hids.AsesorEmail,
		AsesorIntId:        hids.AsesorIntID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": protoKeyToJSON(resp.GetKey())})
}

// hubspotKeyIDs agrupa los lookups que el gateway resuelve antes de
// llamar a keys.GenerateKey, para que hubspot-service tenga todo lo
// necesario para upsertear el custom object Key + asociaciones.
type hubspotKeyIDs struct {
	SchoolRecordID string
	SchoolName     string
	SchoolIntID    int32
	AsesorEmail    string
	AsesorIntID    int32
}

// resyncAllKeys — POST /api/admin/keys/resync-all (superadmin-only).
// Itera TODAS las keys de db_keys.key, resuelve los IDs HubSpot para
// cada una (school + asesor via users-service) y llama Keys.ResyncKey
// para que keys-service replay el hubspot.SyncKey sincrono. Pensado
// para backfillear keys creadas antes de paridad v1<->v2 (seccion /
// colegio_id / asesor_id / grado en HubSpot quedan vacios para keys
// viejas porque el upsert original no los seteo).
//
// Throttle de 200ms entre llamadas para no saturar el rate limit de
// HubSpot (100 req/10s = 10/s; nosotros vamos a ~5/s).
//
// Idempotente: hubspot.SyncKey hace UpsertCustomObjectByProp por
// codigo — re-correrlo en una key ya sincronizada solo refresca props,
// no crea duplicados.
func (p *Proxy) resyncAllKeys(w http.ResponseWriter, r *http.Request) {
	// Operacion de mantenimiento: re-sincroniza TODAS las keys hacia
	// HubSpot. Permission gate: db_keys.key.write (puede modificar keys).
	// UCSP asigna ese permiso al equipo que opera la sync.
	if !hasPermission(r, "db_keys.key.write") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso db_keys.key.write",
		})
		return
	}
	listResp, err := p.cli.Keys.ListAllKeys(r.Context(), &keysgrpcpb.ListAllKeysRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	keys := listResp.GetItems()
	total := len(keys)
	succeeded := 0
	failed := 0
	failures := make([]map[string]string, 0)
	for i, k := range keys {
		hids := p.resolveHubspotIDsForKey(r.Context(), k.GetSchoolId(), k.GetAsesorUserId())
		_, err := p.cli.Keys.ResyncKey(r.Context(), &keysgrpcpb.ResyncKeyRequest{
			Id:             k.GetId(),
			SchoolRecordId: hids.SchoolRecordID,
			SchoolName:     hids.SchoolName,
			SchoolIntId:    hids.SchoolIntID,
			AsesorEmail:    hids.AsesorEmail,
			AsesorIntId:    hids.AsesorIntID,
		})
		if err != nil {
			failed++
			failures = append(failures, map[string]string{
				"id":    k.GetId(),
				"code":  k.GetCode(),
				"error": err.Error(),
			})
			log.Printf("[resyncAllKeys] FAIL id=%s code=%s err=%v", k.GetId(), k.GetCode(), err)
		} else {
			succeeded++
		}
		// Throttle salvo la ultima iteracion.
		if i < total-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	log.Printf("[resyncAllKeys] DONE total=%d ok=%d fail=%d", total, succeeded, failed)
	writeJSON(w, http.StatusOK, map[string]any{
		"total":     total,
		"succeeded": succeeded,
		"failed":    failed,
		"failures":  failures,
	})
}

// resolveHubspotIDsForKey hace 2 lookups a users-service:
//   - Schools.GetSchool → hubspot_record_id + name + int_id
//   - Users.GetUser     → email (asesor.int_id depende de que el User
//     proto exponga int_id; cambio #4)
// Best-effort: si los lookups fallan, los campos quedan en zero-value y
// hubspot-service salta los seteos/asociaciones que dependen de ellos.
func (p *Proxy) resolveHubspotIDsForKey(ctx context.Context, schoolID, asesorUserID string) hubspotKeyIDs {
	out := hubspotKeyIDs{}
	if schoolID != "" {
		if sResp, err := p.cli.Schools.GetSchool(ctx, &usersgrpcpb.GetSchoolRequest{Id: schoolID}); err == nil && sResp.GetSchool() != nil {
			s := sResp.GetSchool()
			out.SchoolRecordID = s.GetHubspotRecordId()
			out.SchoolName = s.GetName()
			out.SchoolIntID = s.GetIntId()
		}
	}
	if asesorUserID != "" {
		if uResp, err := p.cli.Users.GetUser(ctx, &usersgrpcpb.GetUserRequest{Id: asesorUserID}); err == nil && uResp.GetUser() != nil {
			out.AsesorEmail = uResp.GetUser().GetEmail()
			out.AsesorIntID = uResp.GetUser().GetIntId()
		}
	}
	return out
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
	// MaxUses opcional: nil = no tocar; *0 = ilimitado; *N = N usos.
	// Antes era int32 y un PATCH sin max_uses lo seteaba a 0 silenciosamente.
	MaxUses *int32 `json:"max_uses"`
	// MaxAttemptsPerUser opcional: nil = no tocar; *N = nuevo valor.
	// 0 explicito = ilimitado (a diferencia de Generate que normaliza a 1).
	MaxAttemptsPerUser *int32 `json:"max_attempts_per_user"`
	// Active opcional: nil = no tocar; true = reactivar; false = desactivar.
	// El front lo usa para reactivar keys apagadas (DeactivateKey solo apaga).
	Active *bool `json:"active"`
}

func (p *Proxy) updateKey(w http.ResponseWriter, r *http.Request) {
	if !hasPermission(r, "db_keys.key.write") {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso db_keys.key.write"})
		return
	}
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
		MaxUses:   in.MaxUses, // *int32 → proto optional max_uses
		MaxAttemptsPerUser: in.MaxAttemptsPerUser,
		Active:    in.Active, // *bool → proto optional active
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
	if !hasPermission(r, "db_keys.key.write") {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso db_keys.key.write"})
		return
	}
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
	// Bug 1 fix: solo asesores/coordinadores del colegio + superadmin.
	schoolID := r.PathValue("id")
	if !p.enforceColegioScope(r, schoolID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this colegio's keys",
		})
		return
	}
	resp, err := p.cli.Keys.ListByColegio(r.Context(), &keysgrpcpb.ListByColegioRequest{
		SchoolId: schoolID,
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
	// SCOPE por colegio: el asesor/coordinador solo ve las keys de SUS colegios.
	// Antes /api/keys/search devolvía TODAS las keys (el código es el secreto de
	// acceso) de todos los colegios. Las keys LAN/masivo (school_id vacío) solo
	// las ve el admin. Forzamos school_id en la respuesta si se pidieron props.
	unrestricted, allowed, caller := p.callerColegioScope(r)
	if !unrestricted && len(req.GetProperties()) > 0 {
		req.Properties = append(req.Properties, "school_id")
	}
	resp, err := p.cli.Keys.SearchKeys(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	results := resp.GetResults()
	total := resp.GetTotal()
	if !unrestricted {
		results = scopeSearchResults(results, allowed, caller)
		total = uint32(len(results))
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*keyscommonpb.SearchResult, *keyscommonpb.Paging](
		total, results, resp.GetPaging(),
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
		"exam_id":        k.GetExamId(),
		"max_attempts_per_user": k.GetMaxAttemptsPerUser(),
	}
}
