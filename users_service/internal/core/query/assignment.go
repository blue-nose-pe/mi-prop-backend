package query

import (
	"context"

	"users_service/internal/core/ports"
)

// AssignmentHandler implementa ports.AssignmentQueries.
type AssignmentHandler struct {
	assignments ports.AssignmentRepository
}

var _ ports.AssignmentQueries = (*AssignmentHandler)(nil)

func NewAssignmentHandler(assignments ports.AssignmentRepository) *AssignmentHandler {
	return &AssignmentHandler{assignments: assignments}
}

func (h *AssignmentHandler) GetCurrent(ctx context.Context, in ports.GetCurrentAssignmentInput) (*ports.AssignmentSnapshot, error) {
	return h.assignments.FindCurrent(ctx, ports.AssignmentKind(in.Kind), in.Target)
}

func (h *AssignmentHandler) ListHistory(ctx context.Context, in ports.ListAssignmentHistoryInput) ([]ports.AssignmentSnapshot, error) {
	return h.assignments.ListHistory(ctx, ports.AssignmentKind(in.Kind), in.Target)
}
