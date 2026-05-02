package query

import (
	"context"

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
		case domain.KindSingle, domain.KindMulti:
			qm.Distribution = distribution(answers)
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

func distribution(answers []domain.Answer) map[string]int32 {
	out := map[string]int32{}
	for _, a := range answers {
		if a.ValueText == "" {
			continue
		}
		out[a.ValueText]++
	}
	return out
}
