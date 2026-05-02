package command

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// HubspotSyncHandler implementa ports.HubspotSyncCommands.
// Caso de uso simple por diseño: el hubspot-service ya hizo todo el
// trabajo (creó el contact en HS), y users_service solo persiste el
// record_id devuelto. Toda la lógica de sync vive en hubspot-service.
type HubspotSyncHandler struct {
	users   ports.UserRepository
	schools ports.SchoolRepository
}

var _ ports.HubspotSyncCommands = (*HubspotSyncHandler)(nil)

func NewHubspotSyncHandler(
	users ports.UserRepository,
	schools ports.SchoolRepository,
) *HubspotSyncHandler {
	return &HubspotSyncHandler{users: users, schools: schools}
}

func (h *HubspotSyncHandler) SetUserHubspotRecordID(ctx context.Context, userID domain.UserID, recordID string) error {
	return h.users.SetHubspotRecordID(ctx, userID, recordID)
}

func (h *HubspotSyncHandler) SetSchoolHubspotRecordID(ctx context.Context, schoolID domain.SchoolID, recordID string) error {
	return h.schools.SetHubspotRecordID(ctx, schoolID, recordID)
}
