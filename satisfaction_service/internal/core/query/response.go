package query

import (
	"context"
	"strings"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

// ResponseHandler implementa ports.ResponseQueries.
//
// GetMetrics es el caso interesante: agrega los datos crudos del repo
// y calcula NPS / averages / distribuciones según el tipo de pregunta.
// La lógica de cómputo es del CORE, no del adapter — así un mismo dato
// puede mostrarse en distintos formatos sin tocar el SQL.
type ResponseHandler struct {
	responses ports.ResponseRepository
}

var _ ports.ResponseQueries = (*ResponseHandler)(nil)

func NewResponseHandler(r ports.ResponseRepository) *ResponseHandler {
	return &ResponseHandler{responses: r}
}

func (h *ResponseHandler) Get(ctx context.Context, id domain.ResponseID) (*domain.Response, error) {
	return h.responses.FindByID(ctx, id)
}

func (h *ResponseHandler) GetMetrics(ctx context.Context, surveyID domain.SurveyID) (*domain.Metrics, error) {
	raw, err := h.responses.GetMetricsRaw(ctx, surveyID)
	if err != nil {
		return nil, err
	}
	return computeMetrics(surveyID, raw), nil
}

func computeMetrics(surveyID domain.SurveyID, raw *ports.RawMetrics) *domain.Metrics {
	out := &domain.Metrics{
		SurveyID:       surveyID,
		TotalResponses: raw.TotalResponses,
		PerQuestion:    make([]domain.QuestionMetrics, 0, len(raw.Questions)),
	}

	// Agrupar respuestas por pregunta.
	byQ := map[domain.QuestionID][]domain.Answer{}
	for _, a := range raw.Answers {
		byQ[a.QuestionID] = append(byQ[a.QuestionID], a)
	}

	for _, q := range raw.Questions {
		answers := byQ[q.ID]
		qm := domain.QuestionMetrics{
			QuestionID: q.ID,
			Kind:       q.Kind,
			Count:      int32(len(answers)),
		}
		switch q.Kind {
		case domain.KindScale:
			if len(answers) > 0 {
				avg := averageNumber(answers)
				qm.Average = &avg
			}
		case domain.KindNPS:
			if len(answers) > 0 {
				avg := averageNumber(answers)
				qm.Average = &avg
				nps := calcNPS(answers)
				qm.NPS = &nps
			}
		case domain.KindSingle:
			qm.Distribution = distribution(answers, false)
		case domain.KindMulti:
			// multi: el value_text llega como lista separada; cada opción cuenta
			// por separado (antes el CSV entero se contaba como UN bucket).
			qm.Distribution = distribution(answers, true)
		case domain.KindOpen:
			// Texto libre: exponemos los comentarios como distribución (texto→1)
			// para que fluyan por el mismo canal (sin cambiar el proto) y el
			// reporte/bot puedan leerlos. computeMetrics no los tenía en cuenta.
			qm.Distribution = distribution(answers, false)
		}
		out.PerQuestion = append(out.PerQuestion, qm)
	}
	return out
}

func averageNumber(answers []domain.Answer) float64 {
	if len(answers) == 0 {
		return 0
	}
	var sum int64
	var n int64
	for _, a := range answers {
		if a.ValueNumber != nil {
			sum += int64(*a.ValueNumber)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}

func calcNPS(answers []domain.Answer) int32 {
	var promoters, detractors, total int32
	for _, a := range answers {
		if a.ValueNumber == nil {
			continue
		}
		v := *a.ValueNumber
		total++
		switch {
		case v >= 9:
			promoters++
		case v <= 6:
			detractors++
		}
	}
	if total == 0 {
		return 0
	}
	return (promoters - detractors) * 100 / total
}

// distribution cuenta ocurrencias de value_text. Si splitMulti es true (para
// preguntas de opción múltiple), separa el value_text por el delimitador '||'
// (o ',' legacy) y cuenta cada opción por separado — antes el CSV entero
// contaba como un único bucket, así que el conteo por opción salía mal en
// cuanto un alumno marcaba más de una.
func distribution(answers []domain.Answer, splitMulti bool) map[string]int32 {
	out := map[string]int32{}
	for _, a := range answers {
		text := strings.TrimSpace(a.ValueText)
		if text == "" {
			continue
		}
		if !splitMulti {
			out[text]++
			continue
		}
		sep := ","
		if strings.Contains(text, "||") {
			sep = "||"
		}
		for _, tok := range strings.Split(text, sep) {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				out[tok]++
			}
		}
	}
	return out
}
