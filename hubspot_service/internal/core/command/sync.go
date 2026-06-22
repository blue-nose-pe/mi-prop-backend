// Package command — write side. Orquesta llamadas a HubSpot, encolado
// de jobs y callback al users_service.
package command

import (
	"context"
	"log"
	"strconv"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

// SyncHandler implementa ports.SyncCommands.
//
// Reglas:
//   - UpsertContact es síncrono: el caller necesita el record_id.
//     Si el cliente provee user_id, intentamos persistirlo en users_service
//     (best-effort: si users_service no responde, el record_id ya quedó
//     creado en HubSpot, así que devolvemos OK al caller).
//   - SendOTP es síncrono: el alumno está esperando el email.
//   - Los demás (results, asesores, colegios) se encolan con backoff
//     exponencial (5 intentos, 1s/2s/4s/8s/16s) → DLQ.
type SyncHandler struct {
	hs       ports.HubspotClient
	otp      ports.OTPWebhook
	queue    ports.JobEnqueuer
	usersCB  ports.UsersServiceCallback // nullable — best-effort
}

var _ ports.SyncCommands = (*SyncHandler)(nil)

func NewSyncHandler(
	hs ports.HubspotClient,
	otp ports.OTPWebhook,
	queue ports.JobEnqueuer,
	usersCB ports.UsersServiceCallback,
) *SyncHandler {
	return &SyncHandler{hs: hs, otp: otp, queue: queue, usersCB: usersCB}
}

func (h *SyncHandler) UpsertContact(ctx context.Context, c domain.Contact) (domain.RecordID, error) {
	if c.DNI == "" {
		return "", domain.ErrInvalidPayload
	}
	recordID, err := h.hs.UpsertContactByDNI(ctx, c.ToProperties(), c.DNI)
	if err != nil {
		return "", err
	}
	if c.UserID != "" && h.usersCB != nil {
		// best-effort: si falla, lo logueamos arriba pero no rompemos al caller.
		_ = h.usersCB.SetUserHubspotRecordID(ctx, c.UserID, recordID)
	}
	return recordID, nil
}

func (h *SyncHandler) SendOTP(ctx context.Context, in ports.SendOTPInput) error {
	// 1) Garantizamos que el contacto exista con otp_estudiante seteado.
	//    Antes solo escribiamos la prop SI el contacto ya existia: para
	//    estudiantes nuevos (registro publico via key) el contacto no
	//    estaba en HubSpot todavia, la prop quedaba vacia y la Automation
	//    no encontraba el OTP -> el email nunca salia. Upsert lo arregla
	//    creando el contacto on-the-fly con la prop seteada.
	//
	//    Cuando el caller (users_service) provee datos personales
	//    (register-with-key), los incluimos en la upsert: asi el contacto
	//    arranca con nombre/apellido/dni/phone en HubSpot en vez de vacio.
	//    En request-otp (login posterior) van vacios y solo refrescamos
	//    otp_estudiante — no pisamos lo que ya estaba en HubSpot.
	// Flags de origen siempre seteados: matchea exactamente lo que P1
	// hacia en utils/hubspot_sync/createHubspotContacto.js (lineas 29 y 33).
	// Los workflows del portal UCSP filtran por estas props para identificar
	// contactos creados desde Mi Proposito vs CRM manual / otros sistemas.
	props := map[string]string{
		"otp_estudiante":                 in.OTP,
		"origina_de_mi_proposito_":       "true",
		"sincronizado_por_mi_proposito_": "true",
	}
	if in.FirstName != "" {
		props["firstname"] = in.FirstName
	}
	if in.LastName != "" {
		props["lastname"] = in.LastName
	}
	if in.DocumentNumber != "" {
		// El custom field "Numero de Documento" del portal UCSP usa la
		// internal name "dni" (mismo seteo que UpsertContactByDNI).
		props["dni"] = in.DocumentNumber
	}
	// mi_proposito___id_colegio: prop INTEGER en HubSpot (heredada de P1
	// que usaba autoincr de MySQL). users_service ahora envia el int_id
	// resuelto desde la tabla school (migration 020). 0 = sin colegio
	// asignado al user, no setear la prop.
	if in.SchoolIntID > 0 {
		props["mi_proposito___id_colegio"] = strconv.Itoa(int(in.SchoolIntID))
	}
	if in.Phone != "" {
		props["phone"] = in.Phone
	}
	contactID, err := h.hs.UpsertContactByEmail(ctx, props, in.Email)
	if err != nil {
		// Bug 10 fix: si el upsert falla la prop `otp_estudiante` NO queda
		// seteada en HubSpot. El Workflow de Automation lee esa prop para
		// armar el cuerpo del email — sin ella, el email NO se manda. Antes
		// se loggeaba y seguia disparando el webhook (que en HubSpot va a
		// fallar silenciosamente o mandar email vacio). Ahora propagamos el
		// error para que users_service invalide el OTP persistido y muestre
		// error consistente al cliente (en su sender que llama esta RPC).
		log.Printf("[SendOTP] UpsertContactByEmail FAIL email=%s err=%v", in.Email, err)
		return err
	}
	// 1b) Asociar Contact <-> Company del colegio (object 0-2). P1 lo hacia
	//     en createHubspotContacto.js (associations.v4.basicApi.createDefault)
	//     para que el estudiante apareciera vinculado al Colegio en el
	//     sidebar de HubSpot. Sin esto, el contacto queda con la prop
	//     mi_proposito___id_colegio seteada pero "huerfano" — sin Company
	//     visible en la UI. Best-effort: si falla la asociacion no rompemos
	//     el flujo de OTP.
	if in.SchoolRecordID != "" && contactID != "" {
		if err := h.hs.AssociateContactToCompany(ctx, contactID, domain.RecordID(in.SchoolRecordID)); err != nil {
			log.Printf("[SendOTP] AssociateContactToCompany FAIL contact=%s company=%s err=%v", contactID, in.SchoolRecordID, err)
		}
	}
	// 2) Disparar webhook trigger (HubSpot Automation manda el email).
	return h.otp.Trigger(ctx, in.Email, in.OTP)
}

// SyncStudentContact — mismo upsert + asociacion que SendOTP, sin OTP.
// Pensado para el flujo "asesor crea estudiante": el contacto entra a
// HubSpot completo (firstname/lastname/dni/phone/colegio + flags de
// origen + Company association) pero no se dispara webhook OTP porque
// el estudiante no necesita verificar email — el asesor ya lo dio de
// alta manualmente.
func (h *SyncHandler) SyncStudentContact(ctx context.Context, in ports.SyncStudentContactInput) error {
	props := map[string]string{
		"origina_de_mi_proposito_":       "true",
		"sincronizado_por_mi_proposito_": "true",
	}
	if in.FirstName != "" {
		props["firstname"] = in.FirstName
	}
	if in.LastName != "" {
		props["lastname"] = in.LastName
	}
	if in.DocumentNumber != "" {
		props["dni"] = in.DocumentNumber
	}
	if in.Phone != "" {
		props["phone"] = in.Phone
	}
	if in.SchoolIntID > 0 {
		props["mi_proposito___id_colegio"] = strconv.Itoa(int(in.SchoolIntID))
	}
	contactID, err := h.hs.UpsertContactByEmail(ctx, props, in.Email)
	if err != nil {
		log.Printf("[SyncStudentContact] UpsertContactByEmail FAIL email=%s err=%v", in.Email, err)
		return err
	}
	if in.SchoolRecordID != "" && contactID != "" {
		if err := h.hs.AssociateContactToCompany(ctx, contactID, domain.RecordID(in.SchoolRecordID)); err != nil {
			log.Printf("[SyncStudentContact] AssociateContactToCompany FAIL contact=%s company=%s err=%v", contactID, in.SchoolRecordID, err)
		}
	}
	return nil
}

// SyncLead — upsert del contacto de un lead de la landing publica
// "Preparate" (simulacro masivo) al portal UCSP. Diferencias vs
// SyncStudentContact:
//   - identidad por EMAIL (el lead es anonimo, puede no tener DNI todavia).
//   - NO asocia Company: el lead masivo no tiene colegio real (en prod la
//     key LAN usa id_colegio=1 generico).
//   - setea origen_del_contacto="Examen Simulacro" (el valor interno del
//     enum en el portal UCSP coincide con el label) — igual que el flujo
//     de prod, para que admision lo trabaje en el mismo pipeline de leads.
//   - ano_de_egreso / ano_colegio_abc_fb (props texto) con el anio de egreso
//     que el alumno eligio en la landing.
// Best-effort: el caller (gateway) lo dispara fire-and-forget; el lead ya
// quedo persistido en la BD aunque HubSpot falle.
func (h *SyncHandler) SyncLead(ctx context.Context, in ports.SyncLeadInput) error {
	if in.Email == "" {
		return domain.ErrInvalidPayload
	}
	props := map[string]string{
		"origina_de_mi_proposito_":       "true",
		"sincronizado_por_mi_proposito_": "true",
		"origen_del_contacto":            "Examen Simulacro",
	}
	if in.FirstName != "" {
		props["firstname"] = in.FirstName
	}
	if in.LastName != "" {
		props["lastname"] = in.LastName
	}
	if in.DocumentNumber != "" {
		props["dni"] = in.DocumentNumber
	}
	if in.Phone != "" {
		props["phone"] = in.Phone
	}
	if in.GraduationYear != "" {
		props["ano_de_egreso"] = in.GraduationYear
		props["ano_colegio_abc_fb"] = in.GraduationYear
	}
	if _, err := h.hs.UpsertContactByEmail(ctx, props, in.Email); err != nil {
		log.Printf("[SyncLead] UpsertContactByEmail FAIL email=%s err=%v", in.Email, err)
		return err
	}
	return nil
}

// Object type IDs en HubSpot UCSP (produccion). Cambiar aca si UCSP
// migra a otro portal o redefine los custom objects.
const (
	HubspotTypeKey     = "2-32450705" // custom object Key
	HubspotTypeAsesor  = "2-32448565" // custom object Asesor
	HubspotTypeCompany = "0-2"        // Company estandar (colegios)
)

// SyncKey: upsert del custom object Key + asociacion best-effort a
// Asesor y Company. Paridad con P1 (createKey.js / createVisita.js).
func (h *SyncHandler) SyncKey(ctx context.Context, k domain.KeyPayload) error {
	if k.Code == "" {
		return domain.ErrInvalidPayload
	}
	// Upsert por la prop "codigo" (unique en el portal UCSP).
	keyID, err := h.hs.UpsertCustomObjectByProp(ctx, HubspotTypeKey, "codigo", k.Code, k.ToProperties())
	if err != nil {
		log.Printf("[SyncKey] UpsertCustomObjectByProp FAIL code=%s err=%v", k.Code, err)
		return err
	}

	// Asociacion Key <-> Asesor. Orden de resolucion:
	//   1) asesor_record_id explicito del caller (mejor caso).
	//   2) asesor_email (search por prop "email" — unique en el portal).
	//   3) skip (asociacion no se crea — record principal queda igual).
	// El intento de buscar por mi_proposito_asesor_id era v1 legacy
	// (INT en HubSpot) — v2 usa UUID, no matcheaba. Email es estable
	// entre v1 y v2.
	asesorID := k.AsesorRecordID
	if asesorID == "" && k.AsesorEmail != "" {
		found, err := h.hs.FindObjectByProp(ctx, HubspotTypeAsesor, "email", k.AsesorEmail)
		if err != nil {
			log.Printf("[SyncKey] FindAsesorByEmail FAIL email=%s err=%v", k.AsesorEmail, err)
		} else {
			asesorID = found
		}
	}
	if asesorID != "" {
		if err := h.hs.AssociateObjects(ctx, HubspotTypeKey, keyID, HubspotTypeAsesor, asesorID); err != nil {
			log.Printf("[SyncKey] Associate Key->Asesor FAIL key=%s asesor=%s err=%v", keyID, asesorID, err)
		}
	}

	// Asociacion Key <-> Company. Mismo patron.
	companyID := k.SchoolRecordID
	if companyID == "" && k.SchoolIntID > 0 {
		// Fix (audit 2026-06-18): mi_proposito___id_colegio es una prop INTEGER
		// (school.int_id), NO el UUID. Antes se buscaba la Company con
		// string(k.SchoolID) (un UUID) → nunca matcheaba y la Key quedaba sin
		// asociar a su colegio en HubSpot.
		found, err := h.hs.FindObjectByProp(ctx, HubspotTypeCompany, "mi_proposito___id_colegio", strconv.Itoa(int(k.SchoolIntID)))
		if err != nil {
			log.Printf("[SyncKey] FindCompany FAIL school_int=%d err=%v", k.SchoolIntID, err)
		} else {
			companyID = found
		}
	}
	if companyID != "" {
		if err := h.hs.AssociateObjects(ctx, HubspotTypeKey, keyID, HubspotTypeCompany, companyID); err != nil {
			log.Printf("[SyncKey] Associate Key->Company FAIL key=%s company=%s err=%v", keyID, companyID, err)
		}
	}
	log.Printf("[SyncKey] OK code=%s record_id=%s asesor=%s company=%s", k.Code, keyID, asesorID, companyID)
	return nil
}

func (h *SyncHandler) EnqueueExamResult(ctx context.Context, r domain.ExamResult) error {
	if !r.ExamTypeCode.Valid() {
		return domain.ErrInvalidExamType
	}
	if r.DNI == "" || r.AttemptID == "" {
		return domain.ErrInvalidPayload
	}
	return h.queue.EnqueueSyncResult(ctx, r)
}

func (h *SyncHandler) EnqueueAsesor(ctx context.Context, a domain.AsesorPayload) error {
	if a.Email == "" {
		return domain.ErrInvalidPayload
	}
	return h.queue.EnqueueUpsertAsesor(ctx, a)
}

func (h *SyncHandler) EnqueueColegio(ctx context.Context, c domain.ColegioPayload) error {
	if c.Email == "" {
		return domain.ErrInvalidPayload
	}
	return h.queue.EnqueueUpsertColegio(ctx, c)
}
