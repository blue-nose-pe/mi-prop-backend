package asynqadapter

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

// Workers procesan los jobs encolados llamando al HubspotClient.
// Patrón: cada handler es un closure sobre los repos; idempotente
// (HubSpot upserts no duplican; resync de un mismo result solo
// sobre-escribe las mismas propiedades).
type WorkerDeps struct {
	HSClient        ports.HubspotClient
	CustomKeyTypeID string // type id del custom object Key
	CustomColegioID string // type id del custom object Colegio
	CustomAsesorID  string // type id del custom object Asesor
}

// NewMux construye un asynq.ServeMux con todos los handlers registrados.
// Lo llama cmd/worker/main.go al arrancar el server asynq.
func NewMux(deps WorkerDeps) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskSyncResult,    handleSyncResult(deps))
	mux.HandleFunc(TaskUpsertColegio, handleUpsertColegio(deps))
	mux.HandleFunc(TaskUpsertAsesor,  handleUpsertAsesor(deps))
	return mux
}

func handleSyncResult(deps WorkerDeps) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p SyncResultPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		// Si no llega contact_record_id, lo buscamos por DNI.
		recordID := domain.RecordID(p.ContactRecord)
		if recordID == "" {
			id, err := deps.HSClient.FindContactByDNI(ctx, p.DNI)
			if err != nil || id == "" {
				return err
			}
			recordID = id
		}
		props := map[string]string{
			"score_" + p.ExamTypeCode:     domain.FloatStr(p.Score),
			"max_score_" + p.ExamTypeCode: domain.FloatStr(p.MaxScore),
			"ultima_evaluacion":           p.SubmittedAt,
		}
		return deps.HSClient.UpdateContact(ctx, recordID, props)
	}
}

func handleUpsertColegio(deps WorkerDeps) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p UpsertColegioPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		props := map[string]string{
			"email":  p.Email,
			"nombre": p.Nombre,
		}
		if p.PersonalACargo != "" {
			props["personal_a_cargo"] = p.PersonalACargo
		}
		_, err := deps.HSClient.UpsertCustomObjectByProp(ctx, deps.CustomColegioID, "email", p.Email, props)
		return err
	}
}

func handleUpsertAsesor(deps WorkerDeps) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p UpsertAsesorPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		props := map[string]string{
			"email":  p.Email,
			"nombre": p.Nombre,
		}
		// NO enviamos `bio`: la propiedad NO existe en el custom object Asesor
		// del portal UCSP → HubSpot rechaza el upsert con PROPERTY_DOESNT_EXIST
		// (toda la tarea iba al DLQ). Si UCSP crea la prop "bio" en el portal,
		// reactivar: if p.Bio != "" { props["bio"] = p.Bio }.
		_, err := deps.HSClient.UpsertCustomObjectByProp(ctx, deps.CustomAsesorID, "email", p.Email, props)
		return err
	}
}

// ---------- helpers ----------

// int32Str: stringifica sin importar strconv, mantiene el archivo
// auto-contenido y rápido (los workers procesan miles de jobs/min).
func int32Str(n int32) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	const digits = "0123456789"
	buf := [11]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = digits[n%10]
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
