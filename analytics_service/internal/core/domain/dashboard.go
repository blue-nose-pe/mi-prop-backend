// Package domain — entidades del dominio analytics_service.
//
// El servicio NO tiene BD propia. Modela vistas agregadas que se
// construyen en runtime consultando users/exams/keys/satisfaction vía
// gRPC. La cache (Redis) acelera dashboards repetidos.
package domain

import "time"

type (
	UserID    string
	SchoolID  string
	ExamID    string
	AttemptID string
)

// AsesorDashboard — vista agregada para un asesor.
type AsesorDashboard struct {
	AsesorID      UserID
	AsesorName    string
	TotalColegios int32
	TotalKeys     int32
	TotalAttempts int32
	ByExamType    map[string]ExamTypeStats // "vocacional" | "simulacro" | "habitos"
	GeneratedAt   time.Time
}

type ExamTypeStats struct {
	Attempts     int32
	AvgScore     float64
	AvgMaxScore  float64
}

// ColegioDashboard — vista agregada para un colegio.
type ColegioDashboard struct {
	SchoolID       SchoolID
	SchoolName     string
	TotalStudents  int32
	TotalAttempts  int32
	ByExamType     map[string]ExamTypeStats
	GeneratedAt    time.Time
}

// EstudianteDashboard — vista personal del estudiante.
type EstudianteDashboard struct {
	UserID      UserID
	StudentName string
	Tests       []TestResult
	GeneratedAt time.Time
}

type TestResult struct {
	ExamTypeCode string
	ExamID       ExamID
	ExamName     string
	Score        int32
	MaxScore     int32
	SubmittedAt  time.Time
}

// ColegioComparativo — agrega métricas de varios colegios para benchmark.
type ColegioComparativo struct {
	ExamTypeCode string
	Items        []ColegioComparativoItem
	GeneratedAt  time.Time
}

type ColegioComparativoItem struct {
	SchoolID   SchoolID
	SchoolName string
	AvgScore   float64
	Attempts   int32
}

// HistoricoEstudiante — todos los attempts ordenados temporalmente.
type HistoricoEstudiante struct {
	UserID    UserID
	Items     []TestResult
	GeneratedAt time.Time
}
