package command

import (
	"context"

	"hubspot_service/internal/core/ports"
)

// AdminHandler implementa ports.AdminCommands. Solo expuesto a usuarios
// con permiso administrativo (lo controla el JWT interceptor + RBAC).
type AdminHandler struct {
	jobs ports.JobAdmin
}

var _ ports.AdminCommands = (*AdminHandler)(nil)

func NewAdminHandler(jobs ports.JobAdmin) *AdminHandler {
	return &AdminHandler{jobs: jobs}
}

func (h *AdminHandler) RetryFailed(ctx context.Context, queue, jobID string) error {
	return h.jobs.Retry(ctx, queue, jobID)
}

// TriggerFullSync se queda como hook reservado: dispara el reintento masivo
// de los failed que aún no fueron descartados. Por ahora delega al admin
// queue; cuando se implemente sync completo desde users_service se extiende.
func (h *AdminHandler) TriggerFullSync(_ context.Context) error {
	// TODO: cuando exista el caso de uso "re-emitir todos los users a HS",
	//       acá llamaríamos a users_service para iterar los users sin
	//       hubspot_record_id y encolar UpsertContact por cada uno.
	return nil
}
