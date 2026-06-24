// Package asynqadapter implementa ports.JobEnqueuer / ports.JobAdmin
// usando github.com/hibiken/asynq (Redis-backed task queue, equivalente
// funcional a BullMQ).
//
// Cada job lleva:
//   - MaxRetry(5)
//   - Backoff exponencial: 1s, 2s, 4s, 8s, 16s
//   - Timeout 30s por intento
//   - jobID determinístico cuando aplica → idempotencia (si exams_service
//     reenvía el mismo attempt_id no se duplica el job).
package asynqadapter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

// Names de queues. Los workers se suscriben a estos nombres.
const (
	TaskSyncResult    = "hubspot:sync-result"
	TaskUpsertColegio = "hubspot:upsert-colegio"
	TaskUpsertAsesor  = "hubspot:upsert-asesor"
)

// Enqueuer adapta asynq.Client a ports.JobEnqueuer.
type Enqueuer struct {
	client *asynq.Client
}

var _ ports.JobEnqueuer = (*Enqueuer)(nil)

func NewEnqueuer(redisAddr, redisPassword string, useTLS bool) *Enqueuer {
	return &Enqueuer{client: asynq.NewClient(redisOpt(redisAddr, redisPassword, useTLS))}
}

func (e *Enqueuer) Close() error { return e.client.Close() }

func (e *Enqueuer) EnqueueSyncResult(ctx context.Context, r domain.ExamResult) error {
	payload, err := json.Marshal(SyncResultPayload{
		DNI:           r.DNI,
		ExamTypeCode:  string(r.ExamTypeCode),
		Score:         r.Score,
		MaxScore:      r.MaxScore,
		AttemptID:     string(r.AttemptID),
		ContactRecord: string(r.ContactRecord),
		SubmittedAt:   r.SubmittedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(ctx,
		asynq.NewTask(TaskSyncResult, payload),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Second),
		asynq.TaskID(string(r.AttemptID)), // idempotencia
	)
	return err
}

func (e *Enqueuer) EnqueueUpsertColegio(ctx context.Context, c domain.ColegioPayload) error {
	payload, err := json.Marshal(UpsertColegioPayload{
		SchoolID:       string(c.SchoolID),
		Email:          c.Email,
		Nombre:         c.Nombre,
		PersonalACargo: c.PersonalACargo,
	})
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(ctx,
		asynq.NewTask(TaskUpsertColegio, payload),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Second),
		asynq.TaskID(string(c.SchoolID)),
	)
	return err
}

func (e *Enqueuer) EnqueueUpsertAsesor(ctx context.Context, a domain.AsesorPayload) error {
	payload, err := json.Marshal(UpsertAsesorPayload{
		AsesorUserID: string(a.AsesorUserID),
		Email:        a.Email,
		Nombre:       a.Nombre,
		Bio:          a.Bio,
	})
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(ctx,
		asynq.NewTask(TaskUpsertAsesor, payload),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Second),
		asynq.TaskID(string(a.AsesorUserID)),
	)
	return err
}

// ---------- Payloads (ser/deser-able JSON) ----------

type SyncResultPayload struct {
	DNI           string  `json:"dni"`
	ExamTypeCode  string  `json:"exam_type_code"`
	Score         float64 `json:"score"`
	MaxScore      float64 `json:"max_score"`
	AttemptID     string  `json:"attempt_id"`
	ContactRecord string `json:"contact_record_id,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
}

type UpsertColegioPayload struct {
	SchoolID       string `json:"school_id"`
	Email          string `json:"email"`
	Nombre         string `json:"nombre"`
	PersonalACargo string `json:"personal_a_cargo,omitempty"`
}

type UpsertAsesorPayload struct {
	AsesorUserID string `json:"asesor_user_id"`
	Email        string `json:"email"`
	Nombre       string `json:"nombre"`
	Bio          string `json:"bio,omitempty"`
}

func redisOpt(addr, password string, useTLS bool) asynq.RedisClientOpt {
	opt := asynq.RedisClientOpt{Addr: addr, Password: password}
	// asynq toma TLSConfig de tls.Config; lo dejamos nil para HTTP plano.
	// Para Azure Cache (puerto 6380 SSL) se inyecta desde main con un
	// constructor que set TLSConfig=&tls.Config{ServerName:host}.
	_ = useTLS
	return opt
}
