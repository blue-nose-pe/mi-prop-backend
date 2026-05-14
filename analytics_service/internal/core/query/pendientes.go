// Pendientes del asesor: tests publicados+activos que algun estudiante de
// los colegios del asesor todavia no rindio.
//
// Algoritmo:
//   1. Listar colegios del asesor (ListAssignedColegios)
//   2. Por colegio, listar estudiantes activos (ListEstudiantesEnColegio)
//   3. Por colegio, listar exams activos+publicados aplicables (los abiertos
//      o los que pertenecen al colegio)
//   4. Por estudiante, listar sus attempts (ListAttemptsByUser) y construir
//      el set de exam_ids que YA rindio (submitted_at != nil)
//   5. Pendientes = aplicables \ rendidos
//
// Concurrencia: las listas de attempts por estudiante se traen en paralelo
// con un semaforo de 8 para no saturar exams_service.
package query

import (
	"context"
	"sync"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
	"analytics_service/internal/shared/apperr"
)

const pendientesConcurrency = 8

func (h *DashboardHandler) GetAsesorPendientes(ctx context.Context, asesorID domain.UserID) (*domain.AsesorPendientes, error) {
	if asesorID == "" {
		return nil, apperr.NewValidation("MISSING_ASESOR_ID", "asesor_id is required", "asesor_id")
	}
	asesor, err := h.users.GetUser(ctx, asesorID)
	if err != nil {
		return nil, err
	}
	colegios, err := h.users.ListAssignedColegios(ctx, asesorID)
	if err != nil {
		return nil, err
	}

	out := &domain.AsesorPendientes{
		AsesorID:    asesorID,
		AsesorName:  fullName(asesor),
		GeneratedAt: time.Now().UTC(),
	}

	// Acumuladores cross-colegio.
	typeAgg := map[string]*domain.PendingExamTypeCount{}
	studentsWithPending := []domain.PendingStudent{}

	for _, c := range colegios {
		students, err := h.users.ListEstudiantesEnColegio(ctx, c.ID)
		if err != nil {
			continue // un colegio que falle no rompe el reporte
		}
		applicable, err := h.exams.ListActivePublishedExams(ctx, c.ID)
		if err != nil {
			continue
		}
		if len(applicable) == 0 || len(students) == 0 {
			out.TotalStudents += int32(len(students))
			continue
		}

		// Resolver attempts por estudiante en paralelo.
		type result struct {
			student      ports.UpstreamUser
			pendingExams []domain.PendingExam
			pendingByTyp map[string]int32
		}
		results := make([]result, len(students))
		sem := make(chan struct{}, pendientesConcurrency)
		var wg sync.WaitGroup

		for i := range students {
			idx := i
			s := students[i]
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				attempts, err := h.exams.ListAttemptsByUser(ctx, s.ID)
				if err != nil {
					return
				}
				submittedExams := map[domain.ExamID]struct{}{}
				for _, a := range attempts {
					if a.SubmittedAt != nil {
						submittedExams[a.ExamID] = struct{}{}
					}
				}
				var pe []domain.PendingExam
				byTyp := map[string]int32{}
				for _, ex := range applicable {
					if _, done := submittedExams[ex.ID]; done {
						continue
					}
					pe = append(pe, domain.PendingExam{
						ExamID:       ex.ID,
						ExamName:     ex.Name,
						ExamTypeCode: ex.ExamTypeCode,
						ExamCode:     ex.Code,
					})
					byTyp[ex.ExamTypeCode]++
				}
				results[idx] = result{student: s, pendingExams: pe, pendingByTyp: byTyp}
			}()
		}
		wg.Wait()

		out.TotalStudents += int32(len(students))
		for _, r := range results {
			if len(r.pendingExams) == 0 {
				continue
			}
			out.TotalPending += int32(len(r.pendingExams))
			studentsWithPending = append(studentsWithPending, domain.PendingStudent{
				UserID:       r.student.ID,
				StudentName:  fullName(&r.student),
				SchoolID:     c.ID,
				SchoolName:   c.Name,
				PendingExams: r.pendingExams,
			})
			for typ, count := range r.pendingByTyp {
				e := typeAgg[typ]
				if e == nil {
					e = &domain.PendingExamTypeCount{ExamTypeCode: typ}
					typeAgg[typ] = e
				}
				e.PendingAttempts += count
				e.AffectedStudents++
			}
		}
	}

	out.Students = studentsWithPending
	out.ByExamType = make([]domain.PendingExamTypeCount, 0, len(typeAgg))
	for _, v := range typeAgg {
		out.ByExamType = append(out.ByExamType, *v)
	}
	return out, nil
}
