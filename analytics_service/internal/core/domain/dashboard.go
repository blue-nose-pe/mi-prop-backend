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
	AsesorID         UserID
	AsesorName       string
	TotalColegios    int32
	TotalKeys        int32
	TotalAttempts    int32
	CompletedVisits  int32 // count(visitas) del asesor con status=completed
	ScheduledVisits  int32 // count(visitas) con status=scheduled (proximas)
	PendingTests     int32 // suma cross-estudiantes de tests pendientes por rendir
	AffectedStudents int32 // estudiantes con al menos un test pendiente
	// Cliente (2026-06-04): "X de Y impactados". TotalAforo (Y) = suma de
	// aforo (max_uses) de las keys del asesor. TotalImpactados (X) = suma de
	// current_uses (estudiantes que ya usaron una key).
	TotalAforo       int32
	TotalImpactados  int32
	// Estudiantes DISTINTOS que rindieron (>=1 intento enviado). NO es la suma
	// de intentos (un alumno con 3 intentos cuenta 1). Es el "estudiantes
	// impactados" correcto para asesores sin aforo finito.
	TotalStudentsRendered int32
	ByExamType       map[string]ExamTypeStats // "vocacional" | "simulacro" | "habitos"
	// Colegios asignados al asesor (assignment SCD-2 vigente). El export
	// XLSX los renderiza en una hoja aparte "Colegios" — sin esta lista
	// nominal el asesor solo veia "Total colegios: N" sin saber cuales.
	Colegios []AsesorColegio
	// Keys creadas por el asesor (activas + inactivas). El export XLSX
	// las renderiza en una hoja aparte "Keys" para que el asesor sepa
	// cuales fueron, su vigencia y su uso.
	Keys        []AsesorKey
	GeneratedAt time.Time
}

// AsesorColegio — fila por colegio asignado al asesor. Subset de
// UpstreamSchool mantenido en el dominio para no acoplar el exporter
// al adapter outbound.
type AsesorColegio struct {
	ID       SchoolID
	Name     string
	City     string
	Category string
}

// AsesorKey — fila por key creada/asociada al asesor para el export.
// Subset minimo de UpstreamKey + SchoolName resuelto desde la lista de
// colegios del asesor. Si UCSP pide ver mode/grade/section/active/
// vigencia, ampliar UpstreamKey + el cliente gRPC keys.
type AsesorKey struct {
	ID           string
	Code         string
	ExamTypeCode string // "vocacional" | "simulacro" | "habitos"
	SchoolName   string // vacio si la key esta en modo LAN (sin school_id)
	CurrentUses  int32
	MaxUses      int32
}

type ExamTypeStats struct {
	Attempts    int32
	AvgScore    float64
	AvgMaxScore float64
	// Areas — SOLO vocacional/estilos. Inclinaciones agregadas del colegio
	// (areas RIASEC en vocacional; canales/estilos en habitos). Vocacional y
	// estilos NO son promediables por puntaje: marketing necesita ver "en que
	// se inclina el colegio". Ratio = sum(points)/sum(maxPoints) sobre todos
	// los attempts del colegio de ese tipo (0..1). El front lo pinta como una
	// barra roja->verde de menor a mayor. Vacio en simulacro.
	Areas []ColegioAreaStat
}

// ColegioAreaStat — una aptitud/estilo agregada a nivel colegio.
type ColegioAreaStat struct {
	Code      string  // "R".."C" (vocacional) | "AUDITIVO"/"ACTIVO"/... (estilos)
	Label     string  // etiqueta legible
	Points    int32   // suma de sort_order sobre todos los attempts del colegio
	MaxPoints int32   // suma del maximo posible (3 por pregunta)
	Ratio     float64 // Points/MaxPoints, 0..1
}

// ColegioDashboard — vista agregada para un colegio.
type ColegioDashboard struct {
	SchoolID      SchoolID
	SchoolName    string
	TotalStudents int32
	TotalAttempts int32
	ByExamType    map[string]ExamTypeStats
	// Students lista plana de estudiantes activos del colegio. La poblamos
	// igual que TotalStudents (de hecho TotalStudents = len(Students)). La
	// usa el export XLSX para incluir una pestaña "Estudiantes" con los
	// datos personales — el front lo descarga desde
	// /api/analytics/colegio/{id}/export.xlsx.
	Students    []ColegioStudent
	GeneratedAt time.Time
}

// ColegioStudent — fila por estudiante para hidratar la pestaña
// "Estudiantes" del export XLSX. Es un subset de UpstreamUser; se mantiene
// en el dominio para no acoplar el exporter al adapter outbound.
type ColegioStudent struct {
	ID             UserID
	FirstName      string
	LastName       string
	DocumentNumber string
	Email          string
	Phone          string
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
	// AttemptID — necesario para que el front (dashboard del estudiante) pueda
	// renderizar el result-card por intento: simulacro carga sus answers y
	// vocacional/estilos su reporte por attempt_id.
	AttemptID   AttemptID
	Score       float64
	MaxScore    float64
	SubmittedAt time.Time
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

// AsesorPendientes — tests pendientes por rendir agregados a nivel asesor.
// "Pendiente" = exam publicado+activo que un estudiante de algun colegio
// del asesor no ha rendido (no tiene attempt submitted).
type AsesorPendientes struct {
	AsesorID        UserID
	AsesorName      string
	TotalPending    int32
	TotalStudents   int32 // evaluados (la suma de todos los students de los colegios)
	ByExamType      []PendingExamTypeCount
	Students        []PendingStudent // solo incluye los que tienen >=1 pendiente
	GeneratedAt     time.Time
}

type PendingExamTypeCount struct {
	ExamTypeCode     string
	PendingAttempts  int32 // suma cross-estudiantes
	AffectedStudents int32 // estudiantes con >=1 pendiente del tipo
}

type PendingStudent struct {
	UserID       UserID
	StudentName  string
	SchoolID     SchoolID
	SchoolName   string
	PendingExams []PendingExam
}

type PendingExam struct {
	ExamID       ExamID
	ExamName     string
	ExamTypeCode string
	ExamCode     string
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
	// TopArea — SOLO vocacional/estilos. La inclinacion/area MAS elegida del
	// colegio en el periodo (vocacional/estilos no son promediables por
	// puntaje; el cliente pide ver "el top de elegibles"). Vacio en simulacro.
	TopAreaCode  string
	TopAreaLabel string
}
