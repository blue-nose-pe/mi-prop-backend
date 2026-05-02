package ports

import (
	"context"

	"analytics_service/internal/core/domain"
)

// analytics_service es READ-ONLY: SOLO queries. No hay commands.
//
// Los dashboards se calculan en runtime cruzando datos de users/exams/keys.
// La caché Redis es opcional (TTL 5 min) — si no se inyecta, cada request
// genera el dashboard desde cero.

type DashboardQueries interface {
	GetAsesorDashboard(ctx context.Context, asesorID domain.UserID) (*domain.AsesorDashboard, error)
	GetColegioDashboard(ctx context.Context, schoolID domain.SchoolID) (*domain.ColegioDashboard, error)
	GetEstudianteDashboard(ctx context.Context, userID domain.UserID) (*domain.EstudianteDashboard, error)
	GetColegioComparativo(ctx context.Context, examTypeCode string) (*domain.ColegioComparativo, error)
	GetHistoricoEstudiante(ctx context.Context, userID domain.UserID) (*domain.HistoricoEstudiante, error)
}

type ExportCommands interface {
	// Genera un Excel en memoria (bytes) con el reporte solicitado.
	// Lo persiste el caller (típicamente: lo manda como response gRPC).
	ExportAsesorDashboard(ctx context.Context, asesorID domain.UserID) ([]byte, error)
	ExportColegioDashboard(ctx context.Context, schoolID domain.SchoolID) ([]byte, error)
	ExportColegioComparativo(ctx context.Context, examTypeCode string) ([]byte, error)
}
