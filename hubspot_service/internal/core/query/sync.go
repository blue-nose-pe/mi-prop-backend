// Package query — read side de hubspot_service.
package query

import (
	"context"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

type SyncHandler struct {
	hs   ports.HubspotClient
	jobs ports.JobAdmin
}

var _ ports.SyncQueries = (*SyncHandler)(nil)

func NewSyncHandler(hs ports.HubspotClient, jobs ports.JobAdmin) *SyncHandler {
	return &SyncHandler{hs: hs, jobs: jobs}
}

func (h *SyncHandler) GetContactByDNI(ctx context.Context, dni string) (domain.RecordID, error) {
	return h.hs.FindContactByDNI(ctx, dni)
}

func (h *SyncHandler) ListFailed(ctx context.Context, limit, offset uint32) ([]domain.FailedSync, error) {
	return h.jobs.ListFailed(ctx, limit, offset)
}
