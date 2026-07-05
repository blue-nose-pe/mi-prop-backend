// Package query — read side de analytics_service. Componer dashboards
// llamando a clientes upstream (users/exams/keys) y agregando localmente.
package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
)

const cacheTTL = 5 * time.Minute

type DashboardHandler struct {
	users ports.UsersClient
	exams ports.ExamsClient
	keys  ports.KeysClient
	cache ports.Cache // puede ser nil (sin cache)
}

var _ ports.DashboardQueries = (*DashboardHandler)(nil)

func NewDashboardHandler(u ports.UsersClient, e ports.ExamsClient, k ports.KeysClient, c ports.Cache) *DashboardHandler {
	return &DashboardHandler{users: u, exams: e, keys: k, cache: c}
}

// ----- Asesor dashboard -----

func (h *DashboardHandler) GetAsesorDashboard(ctx context.Context, asesorID domain.UserID) (*domain.AsesorDashboard, error) {
	cacheKey := "asesor:" + string(asesorID)
	if h.cache != nil {
		var cached domain.AsesorDashboard
		if hit, _ := h.cache.Get(ctx, cacheKey, &cached); hit {
			return &cached, nil
		}
	}

	asesor, err := h.users.GetUser(ctx, asesorID)
	if err != nil {
		return nil, err
	}
	colegios, err := h.users.ListAssignedColegios(ctx, asesorID)
	if err != nil {
		return nil, err
	}
	keys, err := h.keys.ListKeysByAsesor(ctx, asesorID)
	if err != nil {
		return nil, err
	}
	// Colegio DESACTIVADO ("en reserva") no cuenta en el dashboard del asesor
	// (panel + tools del bot): se filtran el colegio, sus attempts, y sus llaves
	// (aforo/impactados). Solo sigue visible en la gestión de colegios.
	activeSchoolIDs := map[domain.SchoolID]struct{}{}
	colegiosAct := colegios[:0]
	for _, c := range colegios {
		if c.Active {
			colegiosAct = append(colegiosAct, c)
			activeSchoolIDs[c.ID] = struct{}{}
		}
	}
	colegios = colegiosAct
	keysAct := keys[:0]
	for _, k := range keys {
		if _, ok := activeSchoolIDs[k.SchoolID]; ok {
			keysAct = append(keysAct, k)
		}
	}
	keys = keysAct

	// Acumular attempts cruzando los colegios.
	stats := map[string]*domain.ExamTypeStats{}
	totalAttempts := int32(0)
	// Estudiantes DISTINTOS que rindieron (>=1 intento enviado), cross-colegio.
	// Un alumno con varios intentos cuenta UNA vez → "estudiantes impactados"
	// correcto (no la suma de intentos, que infla y "llena" el aforo).
	renderedStudents := map[string]struct{}{}
	for _, c := range colegios {
		atts, err := h.exams.ListAttemptsByColegio(ctx, c.ID)
		if err != nil {
			continue // un colegio sin attempts no debe romper el dashboard
		}
		for _, a := range atts {
			if a.SubmittedAt == nil {
				continue
			}
			if a.UserID != "" {
				renderedStudents[string(a.UserID)] = struct{}{}
			}
			ex, err := h.exams.GetExam(ctx, a.ExamID)
			if err != nil {
				continue
			}
			s := getOrInit(stats, ex.ExamTypeCode)
			s.Attempts++
			// Bug 7 fix: normalizar a porcentaje antes de promediar.
			// Antes se sumaban scores absolutos y max_scores absolutos
			// independientemente, lo que mezclaba exams de escalas distintas
			// (un simulacro 30pt + un simulacro 100pt daban un "avg" sin
			// sentido). Ahora cada attempt aporta su % al promedio.
			if a.Score != nil && a.MaxScore != nil && *a.MaxScore > 0 {
				s.AvgScore += (float64(*a.Score) / float64(*a.MaxScore)) * 100
				s.AvgMaxScore += 100
			}
			totalAttempts++
		}
	}
	finalize(stats)

	// Visitas: best-effort. Si users-service falla acá, el resto del
	// dashboard sigue siendo util — no rompemos el response.
	completedVisits, _ := h.users.CountVisitasByAsesor(ctx, asesorID, "completed")
	scheduledVisits, _ := h.users.CountVisitasByAsesor(ctx, asesorID, "scheduled")

	// Pendientes: reutilizamos GetAsesorPendientes para no duplicar
	// logica. Best-effort: si falla, total queda en 0.
	var pendingTotal, pendingAffected int32
	if pend, perr := h.GetAsesorPendientes(ctx, asesorID); perr == nil && pend != nil {
		pendingTotal = pend.TotalPending
		for _, p := range pend.Students {
			if len(p.PendingExams) > 0 {
				pendingAffected++
			}
		}
	}

	// Mapear colegios y keys al dominio para que el exporter XLSX los
	// rendere en hojas aparte ("Colegios" y "Keys"). schoolNameById
	// resuelve la columna `Colegio` en la hoja de keys sin tener que
	// volver a llamar a users-service.
	asesorColegios := make([]domain.AsesorColegio, 0, len(colegios))
	schoolNameByID := make(map[domain.SchoolID]string, len(colegios))
	for _, c := range colegios {
		asesorColegios = append(asesorColegios, domain.AsesorColegio{
			ID:       c.ID,
			Name:     c.Name,
			City:     c.City,
			Category: c.Category,
		})
		schoolNameByID[c.ID] = c.Name
	}
	asesorKeys := make([]domain.AsesorKey, 0, len(keys))
	// Cliente (2026-06-04): "X de Y impactados". Y = suma de aforo (max_uses);
	// X = suma de current_uses (estudiantes que ya usaron una key).
	var totalAforo, totalImpactados int32
	for _, k := range keys {
		totalAforo += k.MaxUses
		totalImpactados += k.CurrentUses
		asesorKeys = append(asesorKeys, domain.AsesorKey{
			ID:           k.ID,
			Code:         k.Code,
			ExamTypeCode: k.ExamTypeCode,
			SchoolName:   schoolNameByID[k.SchoolID],
			CurrentUses:  k.CurrentUses,
			MaxUses:      k.MaxUses,
		})
	}

	out := &domain.AsesorDashboard{
		AsesorID:         asesorID,
		AsesorName:       fullName(asesor),
		TotalColegios:    int32(len(colegios)),
		TotalKeys:        int32(len(keys)),
		TotalAttempts:    totalAttempts,
		CompletedVisits:  completedVisits,
		ScheduledVisits:  scheduledVisits,
		PendingTests:     pendingTotal,
		AffectedStudents: pendingAffected,
		TotalAforo:       totalAforo,
		TotalImpactados:  totalImpactados,
		TotalStudentsRendered: int32(len(renderedStudents)),
		ByExamType:       materialize(stats),
		Colegios:         asesorColegios,
		Keys:             asesorKeys,
		GeneratedAt:      time.Now().UTC(),
	}

	if h.cache != nil {
		_ = h.cache.Set(ctx, cacheKey, out, cacheTTL)
	}
	return out, nil
}

// ----- Colegio dashboard -----

// GetColegioDashboard agrega los attempts de un colegio. Si keyID != "",
// filtra SOLO los attempts rendidos con esa key (el portal del colegio
// permite analizar una key vocacional/estilos/simulacro concreta). keyID
// "" mantiene el comportamiento histórico (todos los attempts del periodo).
func (h *DashboardHandler) GetColegioDashboard(ctx context.Context, schoolID domain.SchoolID, period string, keyID string) (*domain.ColegioDashboard, error) {
	// Normalizamos el key_id a MAYÚSCULAS: exam_attempt.key_id (CONVERT
	// UNIQUEIDENTIFIER→NVARCHAR en SQL Server) siempre viene en MAYÚSCULAS, así
	// que un caller que mande el UUID en minúsculas matchearía 0 attempts y el
	// dashboard per-key saldría vacío en silencio. Normalizar aquí (y en el
	// cacheKey) colapsa ambas cajas. Fix audit 2026-07-02.
	keyID = strings.ToUpper(strings.TrimSpace(keyID))
	cacheKey := "colegio:" + string(schoolID) + ":" + period + ":" + keyID
	if h.cache != nil {
		var cached domain.ColegioDashboard
		if hit, _ := h.cache.Get(ctx, cacheKey, &cached); hit {
			return &cached, nil
		}
	}

	// Filtro de periodo: "" / "current" / "all" = todos los attempts (back-compat
	// con el portal de colegio); "YYYY" = año; "YYYY-QN" = quarter. Así el
	// doughnut/radar/detalle del comparativo respetan el periodo elegido.
	var pYear, pQuarter int32
	pAnnual, pAll := false, false
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "current", "all":
		pAll = true
	default:
		if y, q, ok := parsePeriodLabel(period); ok {
			pYear, pQuarter = y, q
		} else if y, ok := parseYearOnly(period); ok {
			pYear, pQuarter, pAnnual = y, 0, true
		} else {
			pAll = true
		}
	}
	inSelectedPeriod := func(t time.Time) bool {
		if pAll {
			return true
		}
		y, q := quarterOf(t)
		if pAnnual {
			return y == pYear
		}
		return y == pYear && q == pQuarter
	}

	school, err := h.users.GetSchool(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	students, err := h.users.ListEstudiantesEnColegio(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	atts, err := h.exams.ListAttemptsByColegio(ctx, schoolID)
	if err != nil {
		return nil, err
	}

	attemptsInPeriod := int32(0)
	// keyStudents: user_ids distintos con attempt que pasó el filtro (periodo +
	// key). En la vista per-key sirve como denominador coherente (estudiantes de
	// ESA key), evitando el shape engañoso de numerador-por-key / denominador-
	// todo-el-colegio. Fix audit 2026-07-02.
	keyStudents := map[string]struct{}{}
	stats := map[string]*domain.ExamTypeStats{}
	// Buckets de inclinaciones por exam_type_code (solo vocacional/estilos):
	// examTypeCode -> categoria -> {points, maxPoints} agregado sobre TODOS
	// los attempts del colegio de ese tipo. Marketing usa esto como barra
	// roja->verde por aptitud/estilo (vocacional/estilos no son promediables).
	areaBuckets := map[string]map[string]*areaAgg{}
	for _, a := range atts {
		if a.SubmittedAt == nil {
			continue
		}
		if !inSelectedPeriod(*a.SubmittedAt) {
			continue
		}
		// Filtro por key (portal del colegio): si se pidió una key concreta,
		// ignoramos los attempts de otras keys. keyID "" = todas. EqualFold por
		// robustez ante diferencias de caja (a.KeyID ya viene en MAYÚSCULAS).
		if keyID != "" && !strings.EqualFold(a.KeyID, keyID) {
			continue
		}
		if a.UserID != "" {
			keyStudents[string(a.UserID)] = struct{}{}
		}
		attemptsInPeriod++
		ex, err := h.exams.GetExam(ctx, a.ExamID)
		if err != nil {
			continue
		}
		s := getOrInit(stats, ex.ExamTypeCode)
		s.Attempts++
		// Bug fix (audit 2026-06-18): normalizar a porcentaje ANTES de promediar,
		// igual que GetAsesorDashboard (ln 78-80). Antes se sumaban scores
		// absolutos (ej. 265/450) y el dashboard de colegio mostraba 265 como si
		// fuera /100. Acumulamos (score/max)*100 por attempt; finalize divide /Attempts.
		if a.Score != nil && a.MaxScore != nil && *a.MaxScore > 0 {
			s.AvgScore += (float64(*a.Score) / float64(*a.MaxScore)) * 100
			s.AvgMaxScore += 100
		}
		// Vocacional/estilos: acumulamos las inclinaciones por categoria
		// (RIASEC / canales / estilos) leyendo las enriched answers del
		// attempt. Mismo bucketing que buildAreasInteres pero agregado.
		if ex.ExamTypeCode == "vocacional" || ex.ExamTypeCode == "habitos" {
			if answers, aerr := h.exams.ListEnrichedAnswers(ctx, a.ID); aerr == nil {
				bm := areaBuckets[ex.ExamTypeCode]
				if bm == nil {
					bm = map[string]*areaAgg{}
					areaBuckets[ex.ExamTypeCode] = bm
				}
				for _, ans := range answers {
					accumColegioArea(bm, ex.ExamTypeCode, ans.QuestionCategory, ans.OptionSortOrder)
				}
			}
		}
	}
	finalize(stats)
	// Adjuntamos las inclinaciones agregadas a su exam_type.
	for code, bm := range areaBuckets {
		if s := stats[code]; s != nil {
			s.Areas = buildColegioAreas(bm)
		}
	}

	studentRows := make([]domain.ColegioStudent, 0, len(students))
	for _, s := range students {
		studentRows = append(studentRows, domain.ColegioStudent{
			ID:             s.ID,
			FirstName:      s.FirstName,
			LastName:       s.LastName,
			DocumentNumber: s.DocumentNumber,
			Email:          s.Email,
			Phone:          s.Phone,
		})
	}

	// total_students: en la vista global = alumnos del colegio; en la vista
	// per-key = estudiantes distintos que rindieron con ESA key (numerador y
	// denominador en la misma escala). Fix audit 2026-07-02.
	totalStudents := int32(len(students))
	if keyID != "" {
		totalStudents = int32(len(keyStudents))
	}

	out := &domain.ColegioDashboard{
		SchoolID:      schoolID,
		SchoolName:    school.Name,
		TotalStudents: totalStudents,
		TotalAttempts: attemptsInPeriod,
		ByExamType:    materialize(stats),
		Students:      studentRows,
		GeneratedAt:   time.Now().UTC(),
	}
	if h.cache != nil {
		_ = h.cache.Set(ctx, cacheKey, out, cacheTTL)
	}
	return out, nil
}

// ----- Estudiante dashboard -----

func (h *DashboardHandler) GetEstudianteDashboard(ctx context.Context, userID domain.UserID) (*domain.EstudianteDashboard, error) {
	user, err := h.users.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	atts, err := h.exams.ListAttemptsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	tests := make([]domain.TestResult, 0, len(atts))
	for _, a := range atts {
		ex, err := h.exams.GetExam(ctx, a.ExamID)
		if err != nil {
			continue
		}
		tr := domain.TestResult{
			ExamTypeCode: ex.ExamTypeCode,
			ExamID:       a.ExamID,
			ExamName:     ex.Name,
			AttemptID:    a.ID,
		}
		if a.Score != nil {
			tr.Score = *a.Score
		}
		if a.MaxScore != nil {
			tr.MaxScore = *a.MaxScore
		}
		if a.SubmittedAt != nil {
			tr.SubmittedAt = *a.SubmittedAt
		}
		tests = append(tests, tr)
	}

	return &domain.EstudianteDashboard{
		UserID:      userID,
		StudentName: fullName(user),
		Tests:       tests,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// ----- Comparativo -----

func (h *DashboardHandler) GetColegioComparativo(ctx context.Context, examTypeCode string) (*domain.ColegioComparativo, error) {
	if !validExamType(examTypeCode) {
		return nil, domain.ErrInvalidExamType
	}
	schools, err := h.users.ListSchools(ctx, true)
	if err != nil {
		return nil, err
	}
	items := make([]domain.ColegioComparativoItem, 0, len(schools))
	for _, s := range schools {
		atts, err := h.exams.ListAttemptsByColegio(ctx, s.ID)
		if err != nil {
			continue
		}
		var sumScore float64
		var n int32      // participación: TODOS los attempts enviados del tipo
		var scored int32 // solo los que tienen puntaje (para el promedio)
		// Resolvemos exam_type_code via cache local: una sola consulta
		// GetExam por exam_id distinto encontrado entre los attempts del
		// colegio. Evita N+1 cuando los attempts comparten exam.
		typeByExam := map[domain.ExamID]string{}
		resolveType := func(examID domain.ExamID) string {
			if v, ok := typeByExam[examID]; ok {
				return v
			}
			ex, err := h.exams.GetExam(ctx, examID)
			if err != nil || ex == nil {
				typeByExam[examID] = ""
				return ""
			}
			typeByExam[examID] = ex.ExamTypeCode
			return ex.ExamTypeCode
		}
		// Fix audit 2026-07-02: contamos la PARTICIPACIÓN (n) de todo attempt
		// enviado del tipo pedido, y el promedio SOLO sobre los que tienen
		// puntaje. Antes se descartaba el attempt entero si no tenía score, así
		// que vocacional/estilos (sin puntaje) reportaban attempts=0 para todos
		// los colegios (inconsistente con GetColegioDashboard).
		for _, a := range atts {
			if a.SubmittedAt == nil {
				continue
			}
			if resolveType(a.ExamID) != examTypeCode {
				continue
			}
			n++
			if a.Score != nil && a.MaxScore != nil && *a.MaxScore > 0 {
				sumScore += float64(*a.Score) / float64(*a.MaxScore) * 100.0
				scored++
			}
		}
		avg := 0.0
		if scored > 0 {
			avg = sumScore / float64(scored)
		}
		items = append(items, domain.ColegioComparativoItem{
			SchoolID:   s.ID,
			SchoolName: s.Name,
			AvgScore:   avg,
			Attempts:   n,
		})
	}
	// Ranking descendente por avg_score (filas con 0 attempts al final).
	sort.Slice(items, func(i, j int) bool {
		if items[i].Attempts == 0 && items[j].Attempts > 0 {
			return false
		}
		if items[j].Attempts == 0 && items[i].Attempts > 0 {
			return true
		}
		return items[i].AvgScore > items[j].AvgScore
	})
	return &domain.ColegioComparativo{
		ExamTypeCode: examTypeCode,
		Items:        items,
		GeneratedAt:  time.Now().UTC(),
	}, nil
}

// ----- Histórico -----

func (h *DashboardHandler) GetHistoricoEstudiante(ctx context.Context, userID domain.UserID) (*domain.HistoricoEstudiante, error) {
	atts, err := h.exams.ListAttemptsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.TestResult, 0, len(atts))
	for _, a := range atts {
		ex, err := h.exams.GetExam(ctx, a.ExamID)
		if err != nil {
			continue
		}
		tr := domain.TestResult{
			ExamTypeCode: ex.ExamTypeCode,
			ExamID:       a.ExamID,
			ExamName:     ex.Name,
			AttemptID:    a.ID,
		}
		if a.Score != nil {
			tr.Score = *a.Score
		}
		if a.MaxScore != nil {
			tr.MaxScore = *a.MaxScore
		}
		if a.SubmittedAt != nil {
			tr.SubmittedAt = *a.SubmittedAt
		}
		items = append(items, tr)
	}
	return &domain.HistoricoEstudiante{
		UserID:      userID,
		Items:       items,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

// ----- helpers -----

// areaAgg acumula puntos por categoria (aptitud/estilo) a nivel colegio.
type areaAgg struct {
	points    int32
	maxPoints int32
}

// accumColegioArea acumula una respuesta en el bucket de inclinaciones del
// colegio, normalizando los tres encodings: TIV fiel "M{n}:{dim}" (solo M3 =
// areas de interes alimenta la barra; peso por modulo), RIASEC legacy "R".."C"
// (peso 3), y estilos "ACTIVO"/"VISUAL"/... (peso 1, binaria de prod).
func accumColegioArea(bm map[string]*areaAgg, examTypeCode, rawCat string, weight int32) {
	cat := strings.ToUpper(strings.TrimSpace(rawCat))
	if cat == "" {
		return
	}
	maxW := int32(3)
	if strings.HasPrefix(cat, "M") && strings.Contains(cat, ":") {
		mod, dim, ok := splitCat(cat)
		if !ok || mod != "M3" {
			return
		}
		cat = dim
		maxW = vocMaxWeight[mod]
	} else if examTypeCode == "habitos" {
		maxW = 1
	}
	ag := bm[cat]
	if ag == nil {
		ag = &areaAgg{}
		bm[cat] = ag
	}
	ag.points += weight
	ag.maxPoints += maxW
}

// colegioAreaLabel resuelve la etiqueta legible: TIV (SS/CL/...) via
// vocDimLabel, RIASEC legacy via riasecAreaLabel, estilos quedan tal cual.
func colegioAreaLabel(code string) string {
	if l, ok := vocDimLabel[code]; ok {
		return l
	}
	return labelOr(riasecAreaLabel, code, code)
}

// buildColegioAreas convierte los buckets agregados del colegio en una lista
// ordenada de menor a mayor inclinacion (para la barra roja->verde de marketing).
func buildColegioAreas(bm map[string]*areaAgg) []domain.ColegioAreaStat {
	out := make([]domain.ColegioAreaStat, 0, len(bm))
	for code, ag := range bm {
		ratio := 0.0
		if ag.maxPoints > 0 {
			ratio = float64(ag.points) / float64(ag.maxPoints)
		}
		out = append(out, domain.ColegioAreaStat{
			Code:      code,
			Label:     colegioAreaLabel(code),
			Points:    ag.points,
			MaxPoints: ag.maxPoints,
			Ratio:     ratio,
		})
	}
	// De menor a mayor (el cliente pidio la barra "de menor a mayor"); desempate
	// alfabetico para estabilidad.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ratio != out[j].Ratio {
			return out[i].Ratio < out[j].Ratio
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// topColegioArea agrega las inclinaciones de los attempts dados (vocacional/
// estilos) y devuelve el area/estilo MAS elegido del colegio (mayor ratio) como
// (code, label). "" si no hay respuestas categorizadas. Lo usa el historico de
// colegio para la columna "Top elegibles".
func (h *DashboardHandler) topColegioArea(ctx context.Context, atts []ports.UpstreamAttempt) (string, string) {
	bm := map[string]*areaAgg{}
	for _, a := range atts {
		answers, err := h.exams.ListEnrichedAnswers(ctx, a.ID)
		if err != nil {
			continue
		}
		// examTypeCode no se conoce acá sin un GetExam extra; el peso solo
		// afecta el ratio relativo (todas las dims del mismo examen comparten
		// escala), y el "top" es el de mayor ratio — robusto al factor comun.
		// Para el encoding TIV se normaliza la dimension igual.
		for _, ans := range answers {
			accumColegioArea(bm, "", ans.QuestionCategory, ans.OptionSortOrder)
		}
	}
	areas := buildColegioAreas(bm) // ascendente por ratio
	if len(areas) == 0 {
		return "", ""
	}
	top := areas[len(areas)-1] // el de mayor inclinacion
	return top.Code, top.Label
}

func fullName(u *ports.UpstreamUser) string {
	if u == nil {
		return ""
	}
	if u.FirstName == "" && u.LastName == "" {
		return u.Email
	}
	return fmt.Sprintf("%s %s", u.FirstName, u.LastName)
}

func getOrInit(m map[string]*domain.ExamTypeStats, k string) *domain.ExamTypeStats {
	s, ok := m[k]
	if !ok {
		s = &domain.ExamTypeStats{}
		m[k] = s
	}
	return s
}

// finalize convierte sumas en promedios.
func finalize(m map[string]*domain.ExamTypeStats) {
	for _, s := range m {
		if s.Attempts > 0 {
			s.AvgScore /= float64(s.Attempts)
			s.AvgMaxScore /= float64(s.Attempts)
		}
	}
}

func materialize(m map[string]*domain.ExamTypeStats) map[string]domain.ExamTypeStats {
	out := make(map[string]domain.ExamTypeStats, len(m))
	for k, v := range m {
		out[k] = *v
	}
	return out
}

func validExamType(s string) bool {
	switch s {
	case "vocacional", "simulacro", "habitos":
		return true
	}
	return false
}
