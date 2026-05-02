package domain

import "time"

type (
	SurveyID   string
	QuestionID string
	ResponseID string
	UserID     string // ref lógica a db_users.users.id
	AttemptID  string // ref lógica a exams_service.exam_attempt.id
)

// Survey es la versión publicable de una encuesta.
type Survey struct {
	ID          SurveyID
	Code        string
	Title       string
	Description string
	TargetRole  string // admin | asesor | coordinador | estudiante
	Trigger     string // post_test | on_demand | recurring
	Version     int32
	Published   bool
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   *time.Time
}

// QuestionKind es uno de: scale, nps, single, multi, open.
type QuestionKind string

const (
	KindScale  QuestionKind = "scale"
	KindNPS    QuestionKind = "nps"
	KindSingle QuestionKind = "single"
	KindMulti  QuestionKind = "multi"
	KindOpen   QuestionKind = "open"
)

func (k QuestionKind) Valid() bool {
	switch k {
	case KindScale, KindNPS, KindSingle, KindMulti, KindOpen:
		return true
	}
	return false
}

type Question struct {
	ID          QuestionID
	SurveyID    SurveyID
	Text        string
	Kind        QuestionKind
	SortOrder   int32
	OptionsJSON string // JSON serializado del catálogo de opciones (si aplica)
	Required    bool
}

type Response struct {
	ID            ResponseID
	SurveyID      SurveyID
	UserID        UserID
	ExamAttemptID AttemptID
	SubmittedAt   time.Time
}

// Answer: PK compuesta (response_id, question_id).
type Answer struct {
	ResponseID  ResponseID
	QuestionID  QuestionID
	ValueText   string // para open / single (id de opción) / multi (csv de ids)
	ValueNumber *int32 // para scale / nps
}

// Metrics resume las respuestas de una pregunta. NPS y averages se
// calculan acá para no recargar el front.
type Metrics struct {
	SurveyID      SurveyID
	TotalResponses int32
	PerQuestion   []QuestionMetrics
}

type QuestionMetrics struct {
	QuestionID QuestionID
	Kind       QuestionKind
	Count      int32
	// Para scale/nps:
	Average *float64
	NPS     *int32 // -100..100
	// Para single/multi:
	Distribution map[string]int32 // option_value → count
}
