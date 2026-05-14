// Package domain — entidades del dominio analytics_service.
//
// El servicio NO tiene BD propia. Modela vistas agregadas que se
// construyen en runtime consultando users/exams/keys/satisfaction vía
// gRPC. La cache (Redis) acelera dashboards repetidos.
package domain

import "time"

type (
	UserID     string
	SchoolID   string
	ExamID     string
	AttemptID  string
	QuestionID string
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

// HistoricoColegio — serie temporal trimestral de un colegio. Cada item
// es un bucket "{YYYY}-Q{1..4}" con score promedio y variacion vs el
// quarter anterior.
type HistoricoColegio struct {
	SchoolID     SchoolID
	SchoolName   string
	City         string
	Category     string
	ExamTypeCode string // "" si agregado de todos los tipos
	Items        []HistoricoColegioPoint
	GeneratedAt  time.Time
}

type HistoricoColegioPoint struct {
	Period       string  // ej "2026-Q1"
	Year         int32
	Quarter      int32
	AvgScore     float64 // 0..100 (porcentaje sobre max_score)
	Attempts     int32
	VariationPct float64 // contra el punto anterior; ignorar si HasPrevious=false
	HasPrevious  bool    // true => el punto previo existia y tenia attempts validos
}

// ColegiosHistoricoListing — fila por colegio con la metrica del periodo
// actual + variacion vs anterior. Es lo que la grilla del front muestra.
type ColegiosHistoricoListing struct {
	Period       string // ej "2026-Q1"
	ExamTypeCode string
	Items        []ColegiosHistoricoRow
	GeneratedAt  time.Time
}

type ColegiosHistoricoRow struct {
	SchoolID     SchoolID
	SchoolName   string
	City         string
	Category     string
	AvgScore     float64 // del periodo. 0..100
	Attempts     int32
	VariationPct float64 // vs periodo anterior; ignorar si HasPrevious=false
	HasPrevious  bool
}
