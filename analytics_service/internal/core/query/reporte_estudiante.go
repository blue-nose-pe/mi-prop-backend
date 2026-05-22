// Reporte vocacional consolidado del estudiante ("Tour Vocacional UCSP").
//
// Computa solo la sección RIASEC (áreas de interés + carreras sugeridas),
// con la rúbrica clásica de Holland: cada respuesta aporta sort_order
// puntos a la categoría de su pregunta (R/I/A/S/E/C). Cuando UCSP entregue
// la rúbrica de personalidad, apoyo familiar y proyecto de vida, esas
// secciones se rellenan acá sin cambiar la firma del endpoint.
package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
	"analytics_service/internal/shared/apperr"
)

// riasecLabel: codigo -> nombre del eje.
var riasecLabel = map[string]string{
	"R": "Realista",
	"I": "Investigador",
	"A": "Artistico",
	"S": "Social",
	"E": "Emprendedor",
	"C": "Convencional",
}

// riasecAreaLabel: codigo -> etiqueta del area (lo que sale grande en la
// hoja del PDF, p.ej. "CÁLCULO" para Investigador).
var riasecAreaLabel = map[string]string{
	"R": "MECANICA",
	"I": "CALCULO",
	"A": "ARTE Y CREATIVIDAD",
	"S": "SERVICIO Y AYUDA",
	"E": "LIDERAZGO Y NEGOCIOS",
	"C": "ORGANIZACION Y SISTEMAS",
}

// riasecCharacteristics: caracterización corta del area para imprimir.
// Texto base alineado con el PDF de Flor para CALCULO; el resto son
// descripciones estándar de Holland adaptadas a Mi Propósito UCSP.
var riasecCharacteristics = map[string]string{
	"R": "Gusto por trabajar con herramientas, maquinas y tareas practicas que producen un resultado tangible.",
	"I": "Gusto de trabajar con conceptos abstractos, con numeros y problemas matematicos.",
	"A": "Gusto por expresarse mediante actividades creativas: arte, musica, escritura, diseno.",
	"S": "Gusto por ayudar, ensenar y acompanar a otros; trabajo centrado en personas.",
	"E": "Gusto por liderar, persuadir y emprender; orientacion a metas, ventas y negocios.",
	"C": "Gusto por organizar informacion, seguir procedimientos y trabajar con datos estructurados.",
}

// riasecCareers: carreras sugeridas por codigo. Lista alineada con la
// oferta de UCSP y con el ejemplo del PDF (CALCULO -> Matematica,
// Economia, Ing. Geologica, etc). Mantener acá como tabla simple
// permite ajustarla sin pegar a la DB.
var riasecCareers = map[string][]string{
	"R": {"Ingenieria Mecanica", "Ingenieria Industrial", "Ingenieria Mecatronica"},
	"I": {"Matematica", "Economia", "Ingenieria Geologica", "Telecomunicaciones", "Ingenieria de Minas", "Ingenieria Civil"},
	"A": {"Arquitectura", "Diseno Grafico", "Comunicacion"},
	"S": {"Educacion", "Psicologia", "Trabajo Social", "Enfermeria"},
	"E": {"Administracion", "Marketing", "Derecho", "Negocios Internacionales"},
	"C": {"Contabilidad", "Auditoria", "Administracion"},
}

// topAreasN: cuantas areas devolvemos en el "Top" del reporte. El PDF
// muestra solo el #1 pero exponer 2-3 le permite al front mostrar
// alternativas si quiere.
const topAreasN = 3

func (h *DashboardHandler) GetReporteEstudiante(ctx context.Context, in ports.ReporteEstudianteInput) (*domain.ReporteEstudiante, error) {
	if in.UserID == "" {
		return nil, apperr.NewValidation("MISSING_USER_ID", "user_id is required", "user_id")
	}

	// FASE 1 — paralelo: GetUser y la resolucion del attempt son
	// independientes. Antes corrian secuenciales sumando ~150-300ms
	// extras en el modal "Resultados del estudiante" (~640ms total).
	// Con goroutines bajamos a ~max(getUser, resolveAttempt) ≈ ~150ms.
	type userResult struct {
		user *ports.UpstreamUser
		err  error
	}
	type attemptResult struct {
		attempt *ports.UpstreamAttempt
		err     error
	}
	userCh := make(chan userResult, 1)
	attCh := make(chan attemptResult, 1)

	go func() {
		u, err := h.users.GetUser(ctx, in.UserID)
		userCh <- userResult{u, err}
	}()
	go func() {
		// Resolver attempt: si vino attempt_id explícito lo usamos; si no,
		// buscamos el ultimo attempt SUBMITTED del user (más reciente).
		if in.AttemptID != "" {
			att, err := h.exams.GetAttempt(ctx, in.AttemptID)
			if err != nil {
				attCh <- attemptResult{nil, err}
				return
			}
			if att.UserID != in.UserID {
				attCh <- attemptResult{nil, apperr.NewPermissionDenied("ATTEMPT_NOT_OWNED", "attempt does not belong to this user")}
				return
			}
			attCh <- attemptResult{att, nil}
			return
		}
		atts, err := h.exams.ListAttemptsByUser(ctx, in.UserID)
		if err != nil {
			attCh <- attemptResult{nil, err}
			return
		}
		a := pickLatestSubmitted(atts)
		if a == nil {
			attCh <- attemptResult{nil, apperr.NewNotFound("NO_SUBMITTED_ATTEMPT", "user has no submitted attempts to build a report from")}
			return
		}
		attCh <- attemptResult{a, nil}
	}()
	userRes := <-userCh
	if userRes.err != nil {
		return nil, userRes.err
	}
	attRes := <-attCh
	if attRes.err != nil {
		return nil, attRes.err
	}
	user := userRes.user
	attempt := attRes.attempt

	// FASE 2 — paralelo: GetExam y ListEnrichedAnswers solo dependen del
	// attempt resuelto, no entre si. Otro ~150-300ms ahorrados.
	type examResult struct {
		exam *ports.UpstreamExam
		err  error
	}
	type ansResult struct {
		answers []ports.UpstreamEnrichedAnswer
		err     error
	}
	examCh := make(chan examResult, 1)
	ansCh := make(chan ansResult, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e, err := h.exams.GetExam(ctx, attempt.ExamID)
		examCh <- examResult{e, err}
	}()
	go func() {
		defer wg.Done()
		a, err := h.exams.ListEnrichedAnswers(ctx, attempt.ID)
		ansCh <- ansResult{a, err}
	}()
	wg.Wait()
	close(examCh)
	close(ansCh)
	examRes := <-examCh
	if examRes.err != nil {
		return nil, examRes.err
	}
	ansResVal := <-ansCh
	if ansResVal.err != nil {
		return nil, ansResVal.err
	}
	exam := examRes.exam
	answers := ansResVal.answers

	rep := &domain.ReporteEstudiante{
		UserID:       in.UserID,
		StudentName:  fullName(user),
		AttemptID:    attempt.ID,
		ExamID:       attempt.ExamID,
		ExamName:     exam.Name,
		ExamTypeCode: exam.ExamTypeCode,
		SubmittedAt:  attempt.SubmittedAt,
		AreasInteres: buildAreasInteres(answers),
		Personalidad: domain.ReportSection{
			Available: false,
			Reason:    "Pendiente: requiere el cuestionario y rúbrica de personalidad (modelo de 4 temperamentos) que aportará UCSP.",
		},
		ApoyoFamiliar: domain.ReportSection{
			Available: false,
			Reason:    "Pendiente: requiere un cuestionario de relaciones familiares y la rúbrica 0..5 que aportará UCSP.",
		},
		ProyectoDeVida: domain.ReportSection{
			Available: false,
			Reason:    "Pendiente: requiere el cuestionario de Proyecto de Vida (autoconocimiento, información, preparación, presupuesto, vocación) y su rúbrica.",
		},
		GeneratedAt: time.Now().UTC(),
	}
	if attempt.Score != nil {
		rep.Score = *attempt.Score
	}
	if attempt.MaxScore != nil {
		rep.MaxScore = *attempt.MaxScore
	}
	return rep, nil
}

// buildAreasInteres acumula sort_order por categoria y arma el top N.
//
// El "max points" por categoria es N preguntas de esa categoria * peso
// maximo (asumimos 3 = "Mucho", consistente con los seeds del demo).
func buildAreasInteres(answers []ports.UpstreamEnrichedAnswer) domain.AreasInteresSection {
	if len(answers) == 0 {
		return domain.AreasInteresSection{
			Available: false,
			Reason:    "Attempt has no answers (was the test submitted?). Cannot compute RIASEC.",
		}
	}

	type bucket struct {
		points    int32
		maxPoints int32 // sumamos la max posible (sort_order del top option) por question
		questions int32
	}
	buckets := map[string]*bucket{}
	hasCategorized := false

	for _, a := range answers {
		cat := strings.ToUpper(strings.TrimSpace(a.QuestionCategory))
		if cat == "" {
			continue
		}
		hasCategorized = true
		b := buckets[cat]
		if b == nil {
			b = &bucket{}
			buckets[cat] = b
		}
		b.points += a.OptionSortOrder
		// Asumimos escala 0..3 (Nada/Poco/Bastante/Mucho). Si en el futuro
		// alguna pregunta tiene mas opciones, esto subestima maxPoints; el
		// porcentaje saldria > 100. Es preferible esa pequena imprecision
		// a hacer una query extra por opciones.
		b.maxPoints += 3
		b.questions++
	}

	if !hasCategorized {
		return domain.AreasInteresSection{
			Available: false,
			Reason:    "Questions in this attempt do not declare RIASEC categories (R/I/A/S/E/C). Cannot map to interest areas.",
		}
	}

	stats := make(map[string]domain.CategoryStat, len(buckets))
	for code, b := range buckets {
		stats[code] = domain.CategoryStat{
			Code:      code,
			Label:     labelOr(riasecLabel, code, code),
			Points:    b.points,
			MaxPoints: b.maxPoints,
		}
	}

	// Ranking por puntos desc, desempate alfabetico para estabilidad.
	type ranked struct {
		code   string
		points int32
		max    int32
	}
	ranks := make([]ranked, 0, len(buckets))
	for code, b := range buckets {
		ranks = append(ranks, ranked{code: code, points: b.points, max: b.maxPoints})
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].points != ranks[j].points {
			return ranks[i].points > ranks[j].points
		}
		return ranks[i].code < ranks[j].code
	})

	top := make([]domain.AreaInteresMatch, 0, topAreasN)
	for i := 0; i < len(ranks) && i < topAreasN; i++ {
		r := ranks[i]
		top = append(top, domain.AreaInteresMatch{
			Code:            r.code,
			Label:           labelOr(riasecLabel, r.code, r.code),
			AreaLabel:       labelOr(riasecAreaLabel, r.code, r.code),
			Characteristics: labelOr(riasecCharacteristics, r.code, ""),
			Careers:         riasecCareers[r.code],
			Points:          r.points,
			MaxPoints:       r.max,
		})
	}

	return domain.AreasInteresSection{
		Available: true,
		Scores:    stats,
		Top:       top,
	}
}

func labelOr(m map[string]string, k, fallback string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return fallback
}

func pickLatestSubmitted(atts []ports.UpstreamAttempt) *ports.UpstreamAttempt {
	var best *ports.UpstreamAttempt
	for i := range atts {
		a := &atts[i]
		if a.SubmittedAt == nil {
			continue
		}
		if best == nil || a.SubmittedAt.After(*best.SubmittedAt) {
			best = a
		}
	}
	return best
}

// formatRIASECScore se exporta porque otros endpoints podrian
// necesitar la misma normalizacion (por ahora, no se usa).
var _ = fmt.Sprintf
