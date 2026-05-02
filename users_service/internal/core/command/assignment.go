package command

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// AssignmentHandler implementa ports.AssignmentCommands.
// Lógica de negocio: una reasignación cierra la vigente (preserva valid_from
// y setea valid_to = NOW) y crea una nueva fila. El histórico jamás se pisa.
type AssignmentHandler struct {
	users       ports.UserRepository
	assignments ports.AssignmentRepository
}

var _ ports.AssignmentCommands = (*AssignmentHandler)(nil)

func NewAssignmentHandler(
	users ports.UserRepository,
	assignments ports.AssignmentRepository,
) *AssignmentHandler {
	return &AssignmentHandler{users: users, assignments: assignments}
}

func (h *AssignmentHandler) Reassign(ctx context.Context, in ports.ReassignInput) error {
	// Validar que target existe siempre.
	if _, err := h.users.FindByID(ctx, in.Target); err != nil {
		return err
	}
	// Si source != "", validar que source también existe.
	if string(in.Source) != "" {
		if _, err := h.users.FindByID(ctx, in.Source); err != nil {
			return err
		}
	}
	// kind es validado por el CHECK constraint de la BD;
	// tipos inválidos se rechazan con error genérico del adapter.
	if !validKind(in.Kind) {
		return domain.ErrAssignmentNotFound // forzamos un 404 explícito por kind desconocido
	}
	return h.assignments.Reassign(ctx, ports.AssignmentKind(in.Kind), in.Source, in.Target, in.By)
}

func validKind(k ports.AssignmentKind) bool {
	switch k {
	case ports.AssignmentAsesorDeColegio,
		ports.AssignmentCoordinadorDeColegio,
		ports.AssignmentEstudianteEnColegio,
		ports.AssignmentAsesorDeEstudiante:
		return true
	}
	return false
}
