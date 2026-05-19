// Analytics handlers — proxy a analytics-service.
//
// Rutas REST:
//   GET /api/analytics/dashboard                       - admin global (agregado)
//   GET /api/analytics/asesor/{id}/dashboard
//   GET /api/analytics/colegio/{id}/dashboard
//   GET /api/analytics/estudiante/{id}/dashboard
//   GET /api/analytics/comparativo
//   GET /api/analytics/estudiante/{id}/historico
//   GET /api/analytics/asesor/{id}/export.xlsx
//   GET /api/analytics/colegio/{id}/export.xlsx
//   GET /api/analytics/comparativo/export.xlsx
//
// Los exports devuelven binario XLSX con headers
// Content-Type / Content-Disposition apropiados — NO JSON.
package proxy

import (
	"net/http"
	"sync"
	"time"

	analyticsgrpcpb "analytics_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (p *Proxy) RegisterAnalytics(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/analytics/dashboard", p.getGlobalDashboard)
	mux.HandleFunc("GET /api/analytics/asesor/{id}/dashboard", p.getAsesorDashboard)
	mux.HandleFunc("GET /api/analytics/colegio/{id}/dashboard", p.getColegioDashboard)
	mux.HandleFunc("GET /api/analytics/estudiante/{id}/dashboard", p.getEstudianteDashboard)
	mux.HandleFunc("GET /api/analytics/comparativo", p.getComparativo)
	mux.HandleFunc("GET /api/analytics/estudiante/{id}/historico", p.getEstudianteHistorico)
	mux.HandleFunc("GET /api/analytics/estudiante/{id}/reporte", p.getReporteEstudiante)
	mux.HandleFunc("GET /api/analytics/asesor/{id}/pendientes", p.getAsesorPendientes)
	mux.HandleFunc("GET /api/analytics/colegio/{id}/historico", p.getColegioHistorico)
	mux.HandleFunc("GET /api/analytics/colegios/historico", p.getColegiosHistorico)
	mux.HandleFunc("GET /api/analytics/asesor/{id}/export.xlsx", p.exportAsesorXLSX)
	mux.HandleFunc("GET /api/analytics/colegio/{id}/export.xlsx", p.exportColegioXLSX)
	mux.HandleFunc("GET /api/analytics/comparativo/export.xlsx", p.exportComparativoXLSX)
}

// ---------- Global dashboard (admin) ----------

// getGlobalDashboard — GET /api/analytics/dashboard
//
// Agregador pensado para el rol superadmin: lista todos los asesores
// (permission_group_id=3) y compone un dashboard sumando los GetAsesorDashboard
// individuales en paralelo. No requiere RPC nuevo en analytics_service.
//
// Shape: misma estructura que AsesorDashboardResponse pero con totales
// globales, sin asesor_id/asesor_name (en su lugar lleva total_asesores).
func (p *Proxy) getGlobalDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// 1. Lista de asesores (grupo 3).
	asesores, err := p.cli.PermGroups.ListGroupUsers(ctx, &usersgrpcpb.ListGroupUsersRequest{
		GroupId:    3,
		Limit:      1000,
		ActiveOnly: true,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	// 2. Fan-out: GetAsesorDashboard por asesor, paralelo con cap.
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	results := make([]*analyticsgrpcpb.AsesorDashboardResponse, len(asesores.GetItems()))
	var wg sync.WaitGroup
	for i, u := range asesores.GetItems() {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			resp, err := p.cli.Analytics.GetAsesorDashboard(ctx, &analyticsgrpcpb.GetAsesorDashboardRequest{
				AsesorId: u.GetId(),
			})
			if err == nil {
				results[i] = resp
			}
		}()
	}
	wg.Wait()

	// 3. Sumar globales.
	var totalColegios, totalKeys, totalAttempts int32
	var completed, scheduled, pending, affected int32
	byType := map[string]*analyticsgrpcpb.ExamTypeStats{}
	for _, d := range results {
		if d == nil {
			continue
		}
		totalColegios += d.GetTotalColegios()
		totalKeys += d.GetTotalKeys()
		totalAttempts += d.GetTotalAttempts()
		completed += d.GetCompletedVisits()
		scheduled += d.GetScheduledVisits()
		pending += d.GetPendingTests()
		affected += d.GetAffectedStudents()
		for k, v := range d.GetByExamType() {
			if v == nil {
				continue
			}
			cur, ok := byType[k]
			if !ok {
				cur = &analyticsgrpcpb.ExamTypeStats{}
				byType[k] = cur
			}
			// Promedio ponderado por attempts.
			weight := float64(v.GetAttempts())
			cur.Attempts += v.GetAttempts()
			cur.AvgScore += v.GetAvgScore() * weight
			cur.AvgMaxScore += v.GetAvgMaxScore() * weight
		}
	}
	// Promedios finales (avg ponderado / total attempts).
	for _, v := range byType {
		if v.GetAttempts() > 0 {
			v.AvgScore = v.GetAvgScore() / float64(v.GetAttempts())
			v.AvgMaxScore = v.GetAvgMaxScore() / float64(v.GetAttempts())
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_asesores":    len(asesores.GetItems()),
		"total_colegios":    totalColegios,
		"total_keys":        totalKeys,
		"total_attempts":    totalAttempts,
		"completed_visits":  completed,
		"scheduled_visits":  scheduled,
		"pending_tests":     pending,
		"affected_students": affected,
		"by_exam_type":      examTypeStatsToJSON(byType),
		"generated_at":      time.Now().UTC().Format(time.RFC3339),
	})
}

// ---------- Dashboards ----------

func (p *Proxy) getAsesorDashboard(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.GetAsesorDashboard(r.Context(), &analyticsgrpcpb.GetAsesorDashboardRequest{
		AsesorId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asesor_id":         resp.GetAsesorId(),
		"asesor_name":       resp.GetAsesorName(),
		"total_colegios":    resp.GetTotalColegios(),
		"total_keys":        resp.GetTotalKeys(),
		"total_attempts":    resp.GetTotalAttempts(),
		"completed_visits":  resp.GetCompletedVisits(),
		"scheduled_visits":  resp.GetScheduledVisits(),
		"pending_tests":     resp.GetPendingTests(),
		"affected_students": resp.GetAffectedStudents(),
		"by_exam_type":      examTypeStatsToJSON(resp.GetByExamType()),
		"generated_at":      optionalTimestamp(resp.GetGeneratedAt()),
	})
}

func (p *Proxy) getColegioDashboard(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.GetColegioDashboard(r.Context(), &analyticsgrpcpb.GetColegioDashboardRequest{
		SchoolId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"school_id":      resp.GetSchoolId(),
		"school_name":    resp.GetSchoolName(),
		"total_students": resp.GetTotalStudents(),
		"total_attempts": resp.GetTotalAttempts(),
		"by_exam_type":   examTypeStatsToJSON(resp.GetByExamType()),
		"generated_at":   optionalTimestamp(resp.GetGeneratedAt()),
	})
}

func (p *Proxy) getEstudianteDashboard(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.GetEstudianteDashboard(r.Context(), &analyticsgrpcpb.GetEstudianteDashboardRequest{
		UserId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":      resp.GetUserId(),
		"student_name": resp.GetStudentName(),
		"tests":        testResultsToJSON(resp.GetTests()),
		"generated_at": optionalTimestamp(resp.GetGeneratedAt()),
	})
}

func (p *Proxy) getComparativo(w http.ResponseWriter, r *http.Request) {
	examType := r.URL.Query().Get("exam_type_code")
	if examType == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "exam_type_code query param is required"})
		return
	}
	resp, err := p.cli.Analytics.GetColegioComparativo(r.Context(), &analyticsgrpcpb.GetColegioComparativoRequest{
		ExamTypeCode: examType,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		items = append(items, map[string]any{
			"school_id":   it.GetSchoolId(),
			"school_name": it.GetSchoolName(),
			"avg_score":   it.GetAvgScore(),
			"attempts":    it.GetAttempts(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exam_type_code": resp.GetExamTypeCode(),
		"items":          items,
		"generated_at":   optionalTimestamp(resp.GetGeneratedAt()),
	})
}

func (p *Proxy) getEstudianteHistorico(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.GetHistoricoEstudiante(r.Context(), &analyticsgrpcpb.GetHistoricoEstudianteRequest{
		UserId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":      resp.GetUserId(),
		"items":        testResultsToJSON(resp.GetItems()),
		"generated_at": optionalTimestamp(resp.GetGeneratedAt()),
	})
}

// getColegioHistorico — GET /api/analytics/colegio/{id}/historico
// Query params:
//   - exam_type_code (opcional, "" agrega todos los tipos)
//   - periods (opcional, default 8 quarters)
func (p *Proxy) getColegioHistorico(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := p.cli.Analytics.GetHistoricoColegio(r.Context(), &analyticsgrpcpb.GetHistoricoColegioRequest{
		SchoolId:     r.PathValue("id"),
		ExamTypeCode: q.Get("exam_type_code"),
		Periods:      int32(parseUint32Query(q.Get("periods"), 0)),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		items = append(items, map[string]any{
			"period":        it.GetPeriod(),
			"year":          it.GetYear(),
			"quarter":       it.GetQuarter(),
			"avg_score":     it.GetAvgScore(),
			"attempts":      it.GetAttempts(),
			"variation_pct": it.GetVariationPct(),
			"has_previous":  it.GetHasPrevious(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"school_id":      resp.GetSchoolId(),
		"school_name":    resp.GetSchoolName(),
		"city":           resp.GetCity(),
		"category":       resp.GetCategory(),
		"exam_type_code": resp.GetExamTypeCode(),
		"items":          items,
		"generated_at":   optionalTimestamp(resp.GetGeneratedAt()),
	})
}

// getAsesorPendientes — GET /api/analytics/asesor/{id}/pendientes
// Lista tests publicados+activos que estudiantes de los colegios del
// asesor todavia no han rendido.
func (p *Proxy) getAsesorPendientes(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.GetAsesorPendientes(r.Context(), &analyticsgrpcpb.GetAsesorPendientesRequest{
		AsesorId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	byType := make([]map[string]any, 0, len(resp.GetByExamType()))
	for _, e := range resp.GetByExamType() {
		byType = append(byType, map[string]any{
			"exam_type_code":    e.GetExamTypeCode(),
			"pending_attempts":  e.GetPendingAttempts(),
			"affected_students": e.GetAffectedStudents(),
		})
	}
	students := make([]map[string]any, 0, len(resp.GetStudents()))
	for _, s := range resp.GetStudents() {
		exams := make([]map[string]any, 0, len(s.GetPendingExams()))
		for _, p := range s.GetPendingExams() {
			exams = append(exams, map[string]any{
				"exam_id":        p.GetExamId(),
				"exam_name":      p.GetExamName(),
				"exam_type_code": p.GetExamTypeCode(),
				"exam_code":      p.GetExamCode(),
			})
		}
		students = append(students, map[string]any{
			"user_id":       s.GetUserId(),
			"student_name":  s.GetStudentName(),
			"school_id":     s.GetSchoolId(),
			"school_name":   s.GetSchoolName(),
			"pending_exams": exams,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asesor_id":      resp.GetAsesorId(),
		"asesor_name":    resp.GetAsesorName(),
		"total_pending":  resp.GetTotalPending(),
		"total_students": resp.GetTotalStudents(),
		"by_exam_type":   byType,
		"students":       students,
		"generated_at":   optionalTimestamp(resp.GetGeneratedAt()),
	})
}

// getReporteEstudiante — GET /api/analytics/estudiante/{id}/reporte
// Query params:
//   - attempt_id (opcional; si vacio, usa el ultimo attempt submitted del user)
func (p *Proxy) getReporteEstudiante(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.GetReporteEstudiante(r.Context(), &analyticsgrpcpb.GetReporteEstudianteRequest{
		UserId:    r.PathValue("id"),
		AttemptId: r.URL.Query().Get("attempt_id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	areas := resp.GetAreasInteres()
	scores := map[string]any{}
	for k, v := range areas.GetScores() {
		scores[k] = map[string]any{
			"code":       v.GetCode(),
			"label":      v.GetLabel(),
			"points":     v.GetPoints(),
			"max_points": v.GetMaxPoints(),
		}
	}
	top := make([]map[string]any, 0, len(areas.GetTop()))
	for _, t := range areas.GetTop() {
		top = append(top, map[string]any{
			"code":            t.GetCode(),
			"label":           t.GetLabel(),
			"area_label":      t.GetAreaLabel(),
			"characteristics": t.GetCharacteristics(),
			"careers":         t.GetCareers(),
			"points":          t.GetPoints(),
			"max_points":      t.GetMaxPoints(),
		})
	}
	section := func(s *analyticsgrpcpb.ReportSection) map[string]any {
		return map[string]any{
			"available": s.GetAvailable(),
			"reason":    s.GetReason(),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        resp.GetUserId(),
		"student_name":   resp.GetStudentName(),
		"attempt_id":     resp.GetAttemptId(),
		"exam_id":        resp.GetExamId(),
		"exam_name":      resp.GetExamName(),
		"exam_type_code": resp.GetExamTypeCode(),
		"submitted_at":   optionalTimestamp(resp.GetSubmittedAt()),
		"score":          resp.GetScore(),
		"max_score":      resp.GetMaxScore(),
		"areas_interes": map[string]any{
			"available": areas.GetAvailable(),
			"reason":    areas.GetReason(),
			"scores":    scores,
			"top":       top,
		},
		"personalidad":     section(resp.GetPersonalidad()),
		"apoyo_familiar":   section(resp.GetApoyoFamiliar()),
		"proyecto_de_vida": section(resp.GetProyectoDeVida()),
		"generated_at":     optionalTimestamp(resp.GetGeneratedAt()),
	})
}

// getColegiosHistorico — GET /api/analytics/colegios/historico
// Query params:
//   - period (opcional, "" o "current" = quarter actual; sino "YYYY-QN")
//   - exam_type_code (opcional)
func (p *Proxy) getColegiosHistorico(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := p.cli.Analytics.GetColegiosHistorico(r.Context(), &analyticsgrpcpb.GetColegiosHistoricoRequest{
		Period:       q.Get("period"),
		ExamTypeCode: q.Get("exam_type_code"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		items = append(items, map[string]any{
			"school_id":     it.GetSchoolId(),
			"school_name":   it.GetSchoolName(),
			"city":          it.GetCity(),
			"category":      it.GetCategory(),
			"avg_score":     it.GetAvgScore(),
			"attempts":      it.GetAttempts(),
			"variation_pct": it.GetVariationPct(),
			"has_previous":  it.GetHasPrevious(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"period":         resp.GetPeriod(),
		"exam_type_code": resp.GetExamTypeCode(),
		"items":          items,
		"generated_at":   optionalTimestamp(resp.GetGeneratedAt()),
	})
}

// ---------- Exports XLSX (binarios) ----------

func (p *Proxy) exportAsesorXLSX(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.ExportAsesorXLSX(r.Context(), &analyticsgrpcpb.ExportAsesorXLSXRequest{
		AsesorId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeXLSX(w, resp.GetFilename(), resp.GetContent())
}

func (p *Proxy) exportColegioXLSX(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Analytics.ExportColegioXLSX(r.Context(), &analyticsgrpcpb.ExportColegioXLSXRequest{
		SchoolId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeXLSX(w, resp.GetFilename(), resp.GetContent())
}

func (p *Proxy) exportComparativoXLSX(w http.ResponseWriter, r *http.Request) {
	examType := r.URL.Query().Get("exam_type_code")
	if examType == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "exam_type_code query param is required"})
		return
	}
	resp, err := p.cli.Analytics.ExportComparativoXLSX(r.Context(), &analyticsgrpcpb.ExportComparativoXLSXRequest{
		ExamTypeCode: examType,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeXLSX(w, resp.GetFilename(), resp.GetContent())
}

// writeXLSX serializa un binario XLSX al ResponseWriter con los headers
// correctos para que el navegador lo descargue como adjunto.
func writeXLSX(w http.ResponseWriter, filename string, content []byte) {
	if filename == "" {
		filename = "export.xlsx"
	}
	w.Header().Set("Content-Type", xlsxContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// ---------- helpers ----------

func examTypeStatsToJSON(stats map[string]*analyticsgrpcpb.ExamTypeStats) map[string]any {
	out := make(map[string]any, len(stats))
	for k, v := range stats {
		if v == nil {
			out[k] = nil
			continue
		}
		out[k] = map[string]any{
			"attempts":      v.GetAttempts(),
			"avg_score":     v.GetAvgScore(),
			"avg_max_score": v.GetAvgMaxScore(),
		}
	}
	return out
}

func testResultsToJSON(items []*analyticsgrpcpb.TestResult) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, map[string]any{
			"exam_type_code": t.GetExamTypeCode(),
			"exam_id":        t.GetExamId(),
			"exam_name":      t.GetExamName(),
			"score":          t.GetScore(),
			"max_score":      t.GetMaxScore(),
			"submitted_at":   optionalTimestamp(t.GetSubmittedAt()),
		})
	}
	return out
}
