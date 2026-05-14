// Package clients contiene implementaciones de UsersClient/ExamsClient/
// KeysClient.
//
// HOY: NoOp que devuelve listas vacías. Permite arrancar dev local
// sin los servicios upstream corriendo.
//
// MAÑANA (cuando todos estén desplegados): se reemplaza por GrpcClients
// reales que llamen a users-service, exams-service y keys-service vía
// gRPC stubs generados desde sus .proto. La interfaz queda igual — el
// core no se entera.
package clients

import (
	"context"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
)

// NoopUsers / NoopExams / NoopKeys son placeholders no funcionales que
// retornan estructuras vacías. Útil para tests y bootstrap.

type NoopUsers struct{}

var _ ports.UsersClient = (*NoopUsers)(nil)

func (NoopUsers) GetUser(_ context.Context, id domain.UserID) (*ports.UpstreamUser, error) {
	return &ports.UpstreamUser{ID: id, Active: true}, nil
}
func (NoopUsers) GetSchool(_ context.Context, id domain.SchoolID) (*ports.UpstreamSchool, error) {
	return &ports.UpstreamSchool{ID: id, Active: true}, nil
}
func (NoopUsers) ListSchools(_ context.Context, _ bool) ([]ports.UpstreamSchool, error) {
	return nil, nil
}
func (NoopUsers) ListAssignedColegios(_ context.Context, _ domain.UserID) ([]ports.UpstreamSchool, error) {
	return nil, nil
}
func (NoopUsers) ListEstudiantesEnColegio(_ context.Context, _ domain.SchoolID) ([]ports.UpstreamUser, error) {
	return nil, nil
}

type NoopExams struct{}

var _ ports.ExamsClient = (*NoopExams)(nil)

func (NoopExams) ListAttemptsByUser(_ context.Context, _ domain.UserID) ([]ports.UpstreamAttempt, error) {
	return nil, nil
}
func (NoopExams) ListAttemptsByExam(_ context.Context, _ domain.ExamID) ([]ports.UpstreamAttempt, error) {
	return nil, nil
}
func (NoopExams) ListAttemptsByColegio(_ context.Context, _ domain.SchoolID) ([]ports.UpstreamAttempt, error) {
	return nil, nil
}
func (NoopExams) GetExam(_ context.Context, id domain.ExamID) (*ports.UpstreamExam, error) {
	return &ports.UpstreamExam{ID: id, ExamTypeCode: "vocacional"}, nil
}

type NoopKeys struct{}

var _ ports.KeysClient = (*NoopKeys)(nil)

func (NoopKeys) ListKeysByAsesor(_ context.Context, _ domain.UserID) ([]ports.UpstreamKey, error) {
	return nil, nil
}
func (NoopKeys) ListKeysByColegio(_ context.Context, _ domain.SchoolID) ([]ports.UpstreamKey, error) {
	return nil, nil
}
