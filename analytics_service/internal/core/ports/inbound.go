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
	// GetHistoricoColegio agrupa attempts por quarter y calcula variacion.
	// ExamTypeCode "" agrega todos los tipos. Periods <= 0 default a 8.
	GetHistoricoColegio(ctx context.Context, in HistoricoColegioInput) (*domain.HistoricoColegio, error)
	// GetColegiosHistorico devuelve una fila por colegio con la metrica del
	// periodo solicitado + variacion vs anterior. Period "" o "current" toma
	// el quarter actual; sino "YYYY-QN".
	GetColegiosHistorico(ctx context.Context, in ColegiosHistoricoInput) (*domain.ColegiosHistoricoListing, error)
	// GetReporteEstudiante consolida el "Tour Vocacional UCSP" del PDF.
	// Si AttemptID == "", usa el ultimo attempt submitted del user.
	GetReporteEstudiante(ctx context.Context, in ReporteEstudianteInput) (*domain.ReporteEstudiante, error)
}

type ReporteEstudianteInput struct {
	UserID    domain.UserID
	AttemptID domain.AttemptID
}

type HistoricoColegioInput struct {
	SchoolID     domain.SchoolID
	ExamTypeCode string
	Periods      int32
}

type ColegiosHistoricoInput struct {
	Period       string
	ExamTypeCode string
}

type ExportCommands interface {
	// Genera un Excel en memoria (bytes) con el reporte solicitado.
	// Lo persiste el caller (típicamente: lo manda como response gRPC).
	ExportAsesorDashboard(ctx context.Context, asesorID domain.UserID) ([]byte, error)
	ExportColegioDashboard(ctx context.Context, schoolID domain.SchoolID) ([]byte, error)
	ExportColegioComparativo(ctx context.Context, examTypeCode string) ([]byte, error)
}
