package domain

import "time"

// ReporteEstudiante: vista consolidada para imprimir/mostrar el "Tour
// Vocacional UCSP" del estudiante (la hoja con personalidad + apoyo
// familiar + areas + proyecto de vida + carreras).
//
// Secciones que dependen de rúbricas externas (personalidad, apoyo
// familiar, proyecto de vida) se devuelven con Available=false hasta
// que UCSP entregue sus reglas de scoring. El front puede maquetar la
// pantalla completa con placeholders y reemplazar valores sin cambiar
// el contrato.
type ReporteEstudiante struct {
	UserID       UserID
	StudentName  string
	AttemptID    AttemptID
	ExamID       ExamID
	ExamName     string
	ExamTypeCode string // "vocacional" en este caso, "" si no se pudo resolver
	SubmittedAt  *time.Time
	Score        float64
	MaxScore     float64

	AreasInteres   AreasInteresSection
	Personalidad   ReportSection // placeholder
	ApoyoFamiliar  ReportSection // placeholder
	ProyectoDeVida ReportSection // placeholder

	GeneratedAt time.Time
}

// AreasInteresSection: resultado real RIASEC. Computado de las respuestas
// del attempt usando question.category (R/I/A/S/E/C) y option.sort_order
// como peso Likert (Nada=0, Poco=1, Bastante=2, Mucho=3).
type AreasInteresSection struct {
	Available bool
	Reason    string                 // por que no esta disponible si Available=false
	Scores    map[string]CategoryStat // key = codigo RIASEC
	Top       []AreaInteresMatch     // top 1-3 areas por puntaje (ranking)
}

type CategoryStat struct {
	Code      string // R/I/A/S/E/C
	Label     string // "Realista", "Investigador", etc
	Points    int32  // suma de pesos Likert
	MaxPoints int32  // máximo posible para esa categoría en este exam
}

type AreaInteresMatch struct {
	Code            string   // R/I/A/S/E/C
	Label           string   // ej. "Investigador"
	AreaLabel       string   // ej. "CÁLCULO" (lo que se imprime grande)
	Characteristics string   // descripción larga ("Gusto de trabajar con conceptos...")
	Careers         []string // carreras sugeridas
	Points          int32
	MaxPoints       int32
}

// ReportSection es el sobre de una sección del reporte. Para Personalidad
// (carácter Le Senne), Apoyo Familiar (banda) y Proyecto de Vida
// (sub-dimensiones) lleva el resultado textual (ResultLabel/ResultDetail)
// y las barras por dimensión (Items). Available=false + Reason cuando la
// sección no se pudo computar (sin respuestas de ese módulo).
type ReportSection struct {
	Available    bool
	Reason       string
	ResultCode   string         // ej "SANGUINEO", banda familiar...
	ResultLabel  string         // título legible del resultado
	ResultDetail string         // descripción larga (HTML del modelo de prod)
	Items        []CategoryStat // barras por dimensión (puntos x/max)
	Blocks       []ReportBlock  // bloques de texto (4 columnas del carácter)
}

// ReportBlock: bloque de texto con título (Potencialidades / Oportunidades de
// Mejora / Perfil Académico / Recomendaciones del carácter Le Senne).
type ReportBlock struct {
	Title string
	Text  string
}
