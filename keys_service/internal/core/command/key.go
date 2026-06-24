// Package command — write side de keys_service.
package command

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log"
	"strings"
	"time"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"

	"google.golang.org/grpc/metadata"
)

// KeyHandler implementa ports.KeyCommands.
//
// Reglas:
//   - Mode school requiere school_id no vacío.
//   - Si Code llega vacío, generamos uno random (8 chars).
//   - Validate solo lee + chequea ventana/aforo, NO muta.
//   - IncrementUsage es atómico (repo.IncrementUses) y agrega un row
//     en key_usage para auditar.
//   - Generate/Update disparan SyncKey al CRM (HubSpot) en goroutine
//     separada best-effort para no bloquear la respuesta del front.
type KeyHandler struct {
	keys      ports.KeyRepository
	keyUsages ports.KeyUsageRepository
	hubspot   ports.HubspotSyncer
}

var _ ports.KeyCommands = (*KeyHandler)(nil)

func NewKeyHandler(keys ports.KeyRepository, usages ports.KeyUsageRepository, hubspot ports.HubspotSyncer) *KeyHandler {
	return &KeyHandler{keys: keys, keyUsages: usages, hubspot: hubspot}
}

// syncToHubspot — fire-and-forget en goroutine separada. El context del
// caller puede haberse cancelado al devolver la respuesta del front; usamos
// uno fresco con timeout independiente. Propagamos el authorization header
// del incoming ctx (gRPC metadata) para que hubspot_service pueda autenticar
// la llamada — sin esto, el RPC se rechaza con Unauthenticated.
//
// recordIDs es opcional — si el gateway resolvio asesor.hubspot_record_id
// y school.hubspot_record_id antes de llamar Generate/Update, los pasa
// aca para que hubspot-service haga la asociacion directo sin search.
type recordIDs struct {
	AsesorRecordID string
	SchoolRecordID string
	SchoolName     string
	AsesorEmail    string
	SchoolIntID    int32
	AsesorIntID    int32
}

func (h *KeyHandler) syncToHubspot(parent context.Context, k *domain.Key, rids recordIDs) {
	if h.hubspot == nil || k == nil {
		return
	}
	in := ports.HubspotSyncKeyInput{
		Code:           k.Code,
		ExamTypeCode:   examTypeCodeForHubspot(k.ExamTypeID),
		AsesorUserID:   k.AsesorUserID,
		SchoolID:       k.SchoolID,
		SchoolName:     rids.SchoolName,
		AsesorRecordID: rids.AsesorRecordID,
		SchoolRecordID: rids.SchoolRecordID,
		AsesorEmail:    rids.AsesorEmail,
		SchoolIntID:    rids.SchoolIntID,
		AsesorIntID:    rids.AsesorIntID,
		Grade:          k.Grade,
		Section:        k.Section,
		MaxUses:        k.MaxUses,
	}
	if k.ValidFrom != nil {
		in.ValidFrom = k.ValidFrom.UTC().Format(time.RFC3339)
	}
	if k.ValidTo != nil {
		in.ValidTo = k.ValidTo.UTC().Format(time.RFC3339)
	}
	// Capturamos los headers gRPC del incoming ctx (authorization +
	// x-correlation-id) ANTES de lanzar la goroutine, porque el caller
	// puede haber devuelto y dropeado el ctx para cuando la goroutine corra.
	auth, cid := extractAuthAndCID(parent)
	go func(in ports.HubspotSyncKeyInput, auth, cid string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if auth != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", auth)
		}
		if cid != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-correlation-id", cid)
		}
		if err := h.hubspot.SyncKey(ctx, in); err != nil {
			log.Printf("[hubspot] SyncKey FAIL code=%s err=%v", in.Code, err)
		}
	}(in, auth, cid)
}

// extractAuthAndCID lee los headers que necesita propagar al goroutine
// que llama hubspot_service. authorization es requerido (sino el RPC se
// rechaza); x-correlation-id es para trazabilidad.
func extractAuthAndCID(ctx context.Context) (string, string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	auth := ""
	cid := ""
	if v := md.Get("authorization"); len(v) > 0 {
		auth = v[0]
	}
	if v := md.Get("x-correlation-id"); len(v) > 0 {
		cid = v[0]
	}
	return auth, cid
}

// examTypeCodeForHubspot mapea el INT IDENTITY de exam_type al code
// string que hubspot_service espera. Mismo orden que el seed 008 del
// exams_service y mismo que examTypeCodeFromID del analytics_service.
func examTypeCodeForHubspot(id int32) string {
	switch id {
	case 1:
		return "vocacional"
	case 2:
		return "simulacro"
	case 3:
		return "habitos"
	}
	return ""
}

func (h *KeyHandler) Generate(ctx context.Context, in ports.GenerateKeyInput) (*domain.Key, error) {
	if !in.Mode.Valid() {
		return nil, domain.ErrInvalidMode
	}
	if in.Mode == domain.ModeSchool && string(in.SchoolID) == "" {
		return nil, domain.ErrSchoolRequired
	}
	if in.ValidFrom != nil && in.ValidTo != nil && !in.ValidFrom.Before(*in.ValidTo) {
		return nil, domain.ErrInvalidDateRange
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		if in.Mode == domain.ModeLAN {
			// Cliente C14: las keys masivas se nombran consecutivas "LAN N"
			// (igual que producción), no con acrónimos aleatorios.
			n, nerr := h.keys.NextLanNumber(ctx)
			if nerr != nil {
				return nil, nerr
			}
			code = fmt.Sprintf("LAN %d", n)
		} else {
			code = autogenCode(in.ExamTypeID)
		}
	}

	k := &domain.Key{
		Code:         code,
		ExamTypeID:   in.ExamTypeID,
		SchoolID:     in.SchoolID,
		AsesorUserID: in.AsesorUserID,
		Mode:         in.Mode,
		Grade:        strings.TrimSpace(in.Grade),
		Section:      strings.TrimSpace(in.Section),
		ValidFrom:    in.ValidFrom,
		ValidTo:      in.ValidTo,
		MaxUses:      in.MaxUses,
		Active:       true,
		ExamID:       strings.TrimSpace(in.ExamID),
		// Normalizamos 0 → 1 (default). Si el asesor quiere ilimitado debe
		// pasar un negativo via Update — el Generate publico fuerza 1 min.
		MaxAttemptsPerUser: maxAttemptsDefault(in.MaxAttemptsPerUser),
		LandingConfig:      in.LandingConfig,
	}
	id, err := h.keys.Save(ctx, k)
	if err != nil {
		return nil, err
	}
	saved, err := h.keys.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// Invariante "1 LAN activa a la vez": toda LAN nace activa (Active:true
	// arriba); al crearla, desactivamos cualquier otra LAN activa. Validado
	// con UCSP: hay un solo simulacro masivo por proceso de admision.
	if in.Mode == domain.ModeLAN {
		if err := h.keys.DeactivateOtherLans(ctx, id); err != nil {
			return nil, err
		}
	}
	h.syncToHubspot(ctx, saved, recordIDs{
		AsesorRecordID: in.AsesorRecordID,
		SchoolRecordID: in.SchoolRecordID,
		SchoolName:     in.SchoolName,
		AsesorEmail:    in.AsesorEmail,
		SchoolIntID:    in.SchoolIntID,
		AsesorIntID:    in.AsesorIntID,
	})
	return saved, nil
}

func (h *KeyHandler) Update(ctx context.Context, in ports.UpdateKeyInput) (*domain.Key, error) {
	k, err := h.keys.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(in.Grade); v != "" {
		k.Grade = v
	}
	if v := strings.TrimSpace(in.Section); v != "" {
		k.Section = v
	}
	if in.ValidFrom != nil {
		k.ValidFrom = in.ValidFrom
	}
	if in.ValidTo != nil {
		k.ValidTo = in.ValidTo
	}
	if in.MaxUses != nil {
		k.MaxUses = *in.MaxUses
	}
	if in.MaxAttemptsPerUser != nil {
		// Update si permite poner cualquier valor (incluido 0 = ilimitado)
		// porque el asesor lo esta modificando deliberadamente. Solo
		// Generate fuerza min=1.
		k.MaxAttemptsPerUser = *in.MaxAttemptsPerUser
	}
	if in.Active != nil {
		k.Active = *in.Active
	}
	if in.LandingConfig != nil {
		k.LandingConfig = *in.LandingConfig
	}
	if k.ValidFrom != nil && k.ValidTo != nil && !k.ValidFrom.Before(*k.ValidTo) {
		return nil, domain.ErrInvalidDateRange
	}
	if err := h.keys.Update(ctx, k); err != nil {
		return nil, err
	}
	// Invariante "1 LAN activa a la vez": si esta Update ACTIVO una LAN,
	// desactivamos las demas LAN activas.
	if in.Active != nil && *in.Active && k.Mode == domain.ModeLAN {
		if err := h.keys.DeactivateOtherLans(ctx, k.ID); err != nil {
			return nil, err
		}
	}
	// Update no recibe record_ids hoy (UpdateKeyInput no los expone). El
	// hubspot-service hara intento de lookup via mi_proposito_* — si falla,
	// el record principal se upsertea igual y las asociaciones existentes
	// se preservan (PUT es idempotente). Si en el futuro el front quiere
	// re-asociar, agregar campos al UpdateKeyInput similar a Generate.
	h.syncToHubspot(ctx, k, recordIDs{})
	return k, nil
}

// Resync — re-dispara el sync a HubSpot para una key existente.
// Synchronous: el caller (gateway admin endpoint) espera el resultado
// para reportar succeeded/failed por key. NO usa la goroutine fire-
// and-forget de syncToHubspot — pero arma el mismo HubspotSyncKeyInput
// para mantener paridad con Generate/Update.
func (h *KeyHandler) Resync(ctx context.Context, in ports.ResyncKeyInput) error {
	k, err := h.keys.FindByID(ctx, in.ID)
	if err != nil {
		return err
	}
	if h.hubspot == nil {
		return nil
	}
	payload := ports.HubspotSyncKeyInput{
		Code:           k.Code,
		ExamTypeCode:   examTypeCodeForHubspot(k.ExamTypeID),
		AsesorUserID:   k.AsesorUserID,
		SchoolID:       k.SchoolID,
		SchoolName:     in.SchoolName,
		AsesorRecordID: in.AsesorRecordID,
		SchoolRecordID: in.SchoolRecordID,
		AsesorEmail:    in.AsesorEmail,
		SchoolIntID:    in.SchoolIntID,
		AsesorIntID:    in.AsesorIntID,
		Grade:          k.Grade,
		Section:        k.Section,
		MaxUses:        k.MaxUses,
	}
	if k.ValidFrom != nil {
		payload.ValidFrom = k.ValidFrom.UTC().Format(time.RFC3339)
	}
	if k.ValidTo != nil {
		payload.ValidTo = k.ValidTo.UTC().Format(time.RFC3339)
	}
	// Propagamos los headers gRPC (authorization para JWT, x-correlation-id
	// para trazabilidad). Mismo patron que syncToHubspot pero sync.
	auth, cid := extractAuthAndCID(ctx)
	outCtx := ctx
	if auth != "" {
		outCtx = metadata.AppendToOutgoingContext(outCtx, "authorization", auth)
	}
	if cid != "" {
		outCtx = metadata.AppendToOutgoingContext(outCtx, "x-correlation-id", cid)
	}
	return h.hubspot.SyncKey(outCtx, payload)
}

func (h *KeyHandler) Deactivate(ctx context.Context, id domain.KeyID) error {
	if err := h.keys.SetActive(ctx, id, false); err != nil {
		return err
	}
	// Sync el active=false a HubSpot via UpsertCustomObjectByProp (mismo
	// upsert que SyncKey hace). Si HubSpot decide reflejar el active en
	// alguna prop custom, lo veria; por defecto el upsert solo refresca
	// las props que mandamos en ToProperties — Active no esta entre ellas
	// porque el portal UCSP no tiene esa prop. Por ahora, el deactivate
	// local NO necesita propagar al CRM.
	return nil
}

func (h *KeyHandler) Validate(ctx context.Context, code string) (*domain.Key, error) {
	k, err := h.keys.FindByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	ok, _ := k.IsUsable(time.Now().UTC())
	if !ok {
		return nil, domain.ErrKeyNotUsable
	}
	return k, nil
}

func (h *KeyHandler) IncrementUsage(ctx context.Context, in ports.IncrementUsageInput) error {
	// IncrementUses incrementa solo si (key, user) NO tiene key_usage previo
	// (semantica: max_uses cuenta usuarios distintos). 0 filas significa o
	// "es un retry del mismo user" o "key no usable" — desambiguamos.
	rows, err := h.keys.IncrementUses(ctx, in.KeyID, in.UserID)
	if err != nil {
		return err
	}
	if rows == 0 {
		existed, err := h.keyUsages.ExistsByKeyUser(ctx, in.KeyID, in.UserID)
		if err != nil {
			return err
		}
		if !existed {
			// User nuevo y la UPDATE no afecto filas → aforo lleno, ventana
			// fuera de rango o key inactiva.
			return domain.ErrKeyNotUsable
		}
		// Retry permitido: cae al Save de key_usage para auditar el attempt.
	}
	return h.keyUsages.Save(ctx, &domain.KeyUsage{
		KeyID:     in.KeyID,
		UserID:    in.UserID,
		AttemptID: in.AttemptID,
		UsedAt:    time.Now().UTC(),
	})
}

// randomCode genera un código alfanumérico (base32 sin padding).
func randomCode(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback determinístico extremadamente improbable; en prod
		// el rand.Read jamás falla.
		return strings.Repeat("X", n)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	if len(enc) > n {
		enc = enc[:n]
	}
	return strings.ToUpper(enc)
}

// maxAttemptsDefault normaliza el valor recibido en Generate:
//   - <= 0  → 1 (default razonable: un intento por alumno)
//   - >  0  → tal cual lo paso el asesor
//
// El valor 0 en proto indica "no provisto" porque proto int32 zero-value es
// ambiguo. Si UCSP quiere permitir multi-intento, lo declara explicito en
// el form (ej 2, 3...). Permitir 0 al crear seria un footgun (un alumno
// se come todo el aforo). Para reset a ilimitado en futuro, usar Update
// con un valor negativo o un endpoint dedicado.
func maxAttemptsDefault(v int32) int32 {
	if v <= 0 {
		return 1
	}
	return v
}

// autogenCode arma un código tipo `VO-XXXXXX`, `SI-XXXXXX`, `ES-XXXXXX`
// según el exam_type_id (1=vocacional, 2=simulacro, 3=hábitos). Para
// tipos desconocidos cae al fallback alfanumérico de 8 chars sin prefijo.
func autogenCode(examTypeID int32) string {
	var prefix string
	switch examTypeID {
	case 1:
		prefix = "VO"
	case 2:
		prefix = "SI"
	case 3:
		prefix = "ES"
	default:
		return randomCode(8)
	}
	return prefix + "-" + randomCode(6)
}
