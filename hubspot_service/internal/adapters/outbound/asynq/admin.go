package asynqadapter

import (
	"context"

	"github.com/hibiken/asynq"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

// Admin implementa ports.JobAdmin: listar fallidos + retry. Usa el
// inspector de asynq (lee directamente las colas de Redis).
type Admin struct {
	insp *asynq.Inspector
}

var _ ports.JobAdmin = (*Admin)(nil)

func NewAdmin(redisAddr, redisPassword string) *Admin {
	return &Admin{insp: asynq.NewInspector(asynq.RedisClientOpt{
		Addr: redisAddr, Password: redisPassword,
	})}
}

func (a *Admin) ListFailed(_ context.Context, limit, offset uint32) ([]domain.FailedSync, error) {
	queues, err := a.insp.Queues()
	if err != nil {
		return nil, err
	}
	out := make([]domain.FailedSync, 0, limit)
	for _, q := range queues {
		tasks, err := a.insp.ListArchivedTasks(q,
			asynq.PageSize(int(limit)), asynq.Page(int(offset/limit)+1))
		if err != nil {
			continue
		}
		for _, t := range tasks {
			out = append(out, domain.FailedSync{
				Queue:        q,
				JobID:        t.ID,
				FailedReason: t.LastErr,
				Payload:      string(t.Payload),
				FailedAt:     t.LastFailedAt,
			})
			if uint32(len(out)) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (a *Admin) Retry(_ context.Context, queue, jobID string) error {
	return a.insp.RunTask(queue, jobID)
}
