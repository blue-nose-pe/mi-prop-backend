// Package query — read side de analytics_service. Componer dashboards
// llamando a clientes upstream (users/exams/keys) y agregando localmente.
package query

import (
	"context"
	"fmt"
	"sort"
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

	// Acumular attempts cruzando los colegios.
	stats := map[string]*domain.ExamTypeStats{}
	totalAttempts := int32(0)
	for _, c := range colegios {
		atts, err := h.exams.ListAttemptsByColegio(ctx, c.ID)
		if err != nil {
			continue // un colegio sin attempts no debe romper el dashboard
		}
		for _, a := range atts {
			if a.SubmittedAt == nil {
				continue
			}
			ex, err := h.exams.GetExam(ctx, a.ExamID)
			if err != nil {
				continue
			}
			s := getOrInit(stats, ex.ExamTypeCode)
			s.Attempts++
			if a.Score != nil && a.MaxScore != nil {
				s.AvgScore += float64(*a.Score)
				s.AvgMaxScore += float64(*a.MaxScore)
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
	for _, k := range keys {
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

func (h *DashboardHandler) GetColegioDashboard(ctx context.Context, schoolID domain.SchoolID) (*domain.ColegioDashboard, error) {
	cacheKey := "colegio:" + string(schoolID)
	if h.cache != nil {
		var cached domain.ColegioDashboard
		if hit, _ := h.cache.Get(ctx, cacheKey, &cached); hit {
			return &cached, nil
		}
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

	stats := map[string]*domain.ExamTypeStats{}
	for _, a := range atts {
		if a.SubmittedAt == nil {
			continue
		}
		ex, err := h.exams.GetExam(ctx, a.ExamID)
		if err != nil {
			continue
		}
		s := getOrInit(stats, ex.ExamTypeCode)
		s.Attempts++
		if a.Score != nil && a.MaxScore != nil {
			s.AvgScore += float64(*a.Score)
			s.AvgMaxScore += float64(*a.MaxScore)
		}
	}
	finalize(stats)

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

	out := &domain.ColegioDashboard{
		SchoolID:      schoolID,
		SchoolName:    school.Name,
		TotalStudents: int32(len(students)),
		TotalAttempts: int32(len(atts)),
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
		var n int32
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
		for _, a := range atts {
			if a.SubmittedAt == nil || a.Score == nil || a.MaxScore == nil || *a.MaxScore == 0 {
				continue
			}
			if resolveType(a.ExamID) != examTypeCode {
				continue
			}
			sumScore += float64(*a.Score) / float64(*a.MaxScore) * 100.0
			n++
		}
		avg := 0.0
		if n > 0 {
			avg = sumScore / float64(n)
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
