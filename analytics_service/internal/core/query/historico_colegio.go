// Histórico trimestral por colegio + listing cross-colegio.
//
// Bucketing: agrupamos attempts por (year, quarter) calculado de
// submitted_at en UTC. avg_score es el promedio del porcentaje
// `score / max_score * 100` por attempt — asi colegios con max_score
// distinto siguen siendo comparables.
//
// variation_pct: (current - previous) / previous * 100. Si previous == 0
// (o no hay periodo previo) se marca has_previous=false y el campo no
// es interpretable.
package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
	"analytics_service/internal/shared/apperr"
)

const defaultHistoricoPeriods = 8

func quarterOf(t time.Time) (int32, int32) {
	t = t.UTC()
	return int32(t.Year()), int32((int(t.Month())-1)/3 + 1)
}

func periodLabel(year, quarter int32) string {
	return fmt.Sprintf("%d-Q%d", year, quarter)
}

// parsePeriodLabel acepta "YYYY-QN". Devuelve (year, quarter, ok).
func parsePeriodLabel(s string) (int32, int32, bool) {
	var y, q int32
	if n, _ := fmt.Sscanf(strings.TrimSpace(s), "%d-Q%d", &y, &q); n == 2 && q >= 1 && q <= 4 {
		return y, q, true
	}
	return 0, 0, false
}

// parseYearOnly acepta "YYYY" (sin -QN) para el modo ANUAL pedido por el
// cliente (2026-06-04): el periodo principal es anual, el quarter es un
// desglose secundario. Devuelve (year, ok).
func parseYearOnly(s string) (int32, bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(strings.ToUpper(s), "-Q") {
		return 0, false
	}
	var y int32
	if n, _ := fmt.Sscanf(s, "%d", &y); n == 1 && y >= 2000 && y <= 2100 {
		return y, true
	}
	return 0, false
}

// currentPeriod calcula el quarter actual en UTC.
func currentPeriod() (int32, int32) {
	return quarterOf(time.Now().UTC())
}

// previousPeriod devuelve el quarter inmediatamente anterior.
func previousPeriod(y, q int32) (int32, int32) {
	if q == 1 {
		return y - 1, 4
	}
	return y, q - 1
}

// computeAvgScore promedia los porcentajes score/max_score*100 de cada
// attempt completado (submitted_at != nil, max_score > 0). Si no hay
// attempts validos devuelve 0.
func computeAvgScore(atts []ports.UpstreamAttempt) (float64, int32) {
	var sum float64
	var n int32
	for _, a := range atts {
		if a.SubmittedAt == nil || a.Score == nil || a.MaxScore == nil || *a.MaxScore == 0 {
			continue
		}
		sum += float64(*a.Score) / float64(*a.MaxScore) * 100.0
		n++
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// GetHistoricoColegio implementa el endpoint per-colegio.
func (h *DashboardHandler) GetHistoricoColegio(ctx context.Context, in ports.HistoricoColegioInput) (*domain.HistoricoColegio, error) {
	if in.SchoolID == "" {
		return nil, apperr.NewValidation("MISSING_SCHOOL_ID", "school_id is required", "school_id")
	}
	if in.Periods <= 0 {
		in.Periods = defaultHistoricoPeriods
	}
	if in.ExamTypeCode != "" && !validExamType(in.ExamTypeCode) {
		return nil, apperr.NewValidation("INVALID_EXAM_TYPE", fmt.Sprintf("invalid exam_type_code: %s", in.ExamTypeCode), "exam_type_code")
	}

	school, err := h.users.GetSchool(ctx, in.SchoolID)
	if err != nil {
		return nil, err
	}
	atts, err := h.exams.ListAttemptsByColegio(ctx, in.SchoolID)
	if err != nil {
		return nil, err
	}

	// Si filtramos por tipo, necesitamos saber el ExamTypeCode de cada
	// attempt -> resolverlo via GetExam (cacheado por id para no repetir).
	examCache := map[domain.ExamID]string{}
	resolveType := func(examID domain.ExamID) string {
		if v, ok := examCache[examID]; ok {
			return v
		}
		ex, err := h.exams.GetExam(ctx, examID)
		if err != nil || ex == nil {
			examCache[examID] = ""
			return ""
		}
		examCache[examID] = ex.ExamTypeCode
		return ex.ExamTypeCode
	}

	// Bucket: key = year*10 + quarter; value = lista de attempts del bucket.
	buckets := map[int32][]ports.UpstreamAttempt{}
	for _, a := range atts {
		if a.SubmittedAt == nil {
			continue
		}
		if in.ExamTypeCode != "" && resolveType(a.ExamID) != in.ExamTypeCode {
			continue
		}
		y, q := quarterOf(*a.SubmittedAt)
		key := y*10 + q
		buckets[key] = append(buckets[key], a)
	}

	// Ordenar buckets ascendente para calcular variacion contra el previo.
	keys := make([]int32, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	points := make([]domain.HistoricoColegioPoint, 0, len(keys))
	var prevAvg float64
	hadPrev := false
	for _, k := range keys {
		y := k / 10
		q := k % 10
		avg, n := computeAvgScore(buckets[k])
		p := domain.HistoricoColegioPoint{
			Period:   periodLabel(y, q),
			Year:     y,
			Quarter:  q,
			AvgScore: avg,
			Attempts: n,
		}
		if hadPrev && prevAvg > 0 {
			p.VariationPct = (avg - prevAvg) / prevAvg * 100.0
			p.HasPrevious = true
		}
		points = append(points, p)
		prevAvg = avg
		hadPrev = true
	}

	// Limitar a los ultimos N quarters si hay mas.
	if int32(len(points)) > in.Periods {
		points = points[int32(len(points))-in.Periods:]
	}

	return &domain.HistoricoColegio{
		SchoolID:     in.SchoolID,
		SchoolName:   school.Name,
		City:         school.City,
		Category:     school.Category,
		ExamTypeCode: in.ExamTypeCode,
		Items:        points,
		GeneratedAt:  time.Now().UTC(),
	}, nil
}

// GetColegiosHistorico implementa el listing cross-colegio para la grilla.
//
// Por simplicidad y consistencia: por cada colegio activo, computamos su
// avg_score del periodo solicitado y del anterior. Si no hay attempts en
// el periodo, se incluye igual con avg_score=0 y has_previous=false (para
// que la grilla del front pueda mostrar "—").
func (h *DashboardHandler) GetColegiosHistorico(ctx context.Context, in ports.ColegiosHistoricoInput) (*domain.ColegiosHistoricoListing, error) {
	if in.ExamTypeCode != "" && !validExamType(in.ExamTypeCode) {
		return nil, apperr.NewValidation("INVALID_EXAM_TYPE", fmt.Sprintf("invalid exam_type_code: %s", in.ExamTypeCode), "exam_type_code")
	}

	// Resolver el periodo objetivo + previo. Soporta 3 formas:
	//   "" / "current" -> quarter actual (back-compat).
	//   "YYYY-QN"       -> quarter especifico.
	//   "YYYY"          -> ANUAL (todo el año, compara vs el año anterior).
	var year, quarter int32
	annual := false
	switch strings.ToLower(strings.TrimSpace(in.Period)) {
	case "", "current":
		year, quarter = currentPeriod()
	default:
		if y, q, ok := parsePeriodLabel(in.Period); ok {
			year, quarter = y, q
		} else if y, ok := parseYearOnly(in.Period); ok {
			year, quarter, annual = y, 0, true
		} else {
			return nil, apperr.NewValidation("INVALID_PERIOD", fmt.Sprintf("invalid period (expected current, YYYY-QN o YYYY): %s", in.Period), "period")
		}
	}
	var prevYear, prevQuarter int32
	if annual {
		prevYear, prevQuarter = year-1, 0
	} else {
		prevYear, prevQuarter = previousPeriod(year, quarter)
	}

	schools, err := h.users.ListSchools(ctx, true)
	if err != nil {
		return nil, err
	}

	// Concurrencia: traer attempts por colegio en paralelo. Bounded a 8
	// para no saturar al exams_service.
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	rows := make([]domain.ColegiosHistoricoRow, len(schools))
	var wg sync.WaitGroup

	for i := range schools {
		s := schools[i]
		idx := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			row := domain.ColegiosHistoricoRow{
				SchoolID:   s.ID,
				SchoolName: s.Name,
				City:       s.City,
				Category:   s.Category,
			}
			atts, err := h.exams.ListAttemptsByColegio(ctx, s.ID)
			if err != nil {
				rows[idx] = row
				return
			}
			examCache := map[domain.ExamID]string{}
			resolveType := func(examID domain.ExamID) string {
				if v, ok := examCache[examID]; ok {
					return v
				}
				ex, err := h.exams.GetExam(ctx, examID)
				if err != nil || ex == nil {
					examCache[examID] = ""
					return ""
				}
				examCache[examID] = ex.ExamTypeCode
				return ex.ExamTypeCode
			}
			var current, previous []ports.UpstreamAttempt
			for _, a := range atts {
				if a.SubmittedAt == nil {
					continue
				}
				if in.ExamTypeCode != "" && resolveType(a.ExamID) != in.ExamTypeCode {
					continue
				}
				y, q := quarterOf(*a.SubmittedAt)
				if annual {
					// Modo anual: todo el año actual vs todo el año anterior.
					switch y {
					case year:
						current = append(current, a)
					case prevYear:
						previous = append(previous, a)
					}
				} else {
					switch {
					case y == year && q == quarter:
						current = append(current, a)
					case y == prevYear && q == prevQuarter:
						previous = append(previous, a)
					}
				}
			}
			avgCur, nCur := computeAvgScore(current)
			avgPrev, nPrev := computeAvgScore(previous)
			row.AvgScore = avgCur
			row.Attempts = nCur
			if nPrev > 0 && avgPrev > 0 {
				row.VariationPct = (avgCur - avgPrev) / avgPrev * 100.0
				row.HasPrevious = true
				// Sin attempts validos en el quarter anterior dejamos
				// HasPrevious=false y el front no muestra variacion.
			}
			rows[idx] = row
		}()
	}
	wg.Wait()

	// Compactar: dejar fuera filas sin attempts ni en periodo actual ni
	// en previo? Mejor incluirlas con 0 para que la grilla las muestre y
	// el front decida si filtra o no. Las dejamos como vinieron.

	// Limpiar NaN/Inf por si computeAvgScore produce esos (no deberia).
	for i := range rows {
		if math.IsNaN(rows[i].VariationPct) || math.IsInf(rows[i].VariationPct, 0) {
			rows[i].VariationPct = 0
		}
	}

	// Ordenar alfa por nombre — mejor que un orden indeterminado.
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].SchoolName) < strings.ToLower(rows[j].SchoolName) })

	label := periodLabel(year, quarter)
	if annual {
		label = fmt.Sprintf("%d (anual)", year)
	}
	return &domain.ColegiosHistoricoListing{
		Period:       label,
		ExamTypeCode: in.ExamTypeCode,
		Items:        rows,
		GeneratedAt:  time.Now().UTC(),
	}, nil
}
