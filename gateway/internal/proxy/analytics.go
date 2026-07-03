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
	"context"

	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	"net/http"
	"sync"
	"time"

	analyticsgrpcpb "analytics_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (p *Proxy) RegisterAnalytics(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/analytics/dashboard", p.getGlobalDashboard)
	mux.HandleFunc("GET /api/analytics/asesores/keys-report", p.getAsesoresKeysReport)
	mux.HandleFunc("GET /api/analytics/satisfaccion/reporte", p.getSatisfaccionReporte)
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
	mux.HandleFunc("GET /api/analytics/estudiante/{id}/reporte.xlsx", p.exportReporteEstudianteXLSX)
}

// getAsesoresKeysReport — GET /api/analytics/asesores/keys-report
//
// Reporte de asesores POR LLAVE (pedido cliente 2026-07-03): "el corazón del
// sistema son los exámenes que presentan los alumnos por keys" — el reporte
// anterior mostraba números sueltos (impactados sin saber de qué llave, aforo
// solo de las llaves CREADAS por el asesor). Este endpoint expone, por asesor,
// TODAS las llaves de SUS COLEGIOS (sin importar quién las creó) con sus
// exámenes rendidos y alumnos distintos reales. El front decide la vista:
// por defecto solo llaves VIGENTES, filtro por tipo, e historial completo.
//
// Solo administración (mismo gate que el dashboard global). Cuentas y colegios
// QA quedan fuera (isQAUser/isQAColegio).
func (p *Proxy) getAsesoresKeysReport(w http.ResponseWriter, r *http.Request) {
	if !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso analytics.dashboard.read"})
		return
	}
	if unrestricted, _, _ := p.callerColegioScope(r); !unrestricted {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "el reporte de asesores es solo para administración"})
		return
	}
	out, err := p.collectAsesoresKeys(r.Context())
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asesores": out})
}

// collectAsesoresKeys compone asesor → colegios → llaves con rendidos/alumnos
// (total y por año) — la MISMA fuente para el Reporte de Asesores del panel y
// para la herramienta del Asistente IA (así el bot nunca contradice al panel).
func (p *Proxy) collectAsesoresKeys(ctx context.Context) ([]map[string]any, error) {
	asesores, err := p.cli.PermGroups.ListGroupUsers(ctx, &usersgrpcpb.ListGroupUsersRequest{GroupId: 3, Limit: 1000, ActiveOnly: true})
	if err != nil {
		return nil, err
	}
	// Cache por colegio: rendidos y alumnos DISTINTOS por llave (un colegio se
	// procesa una sola vez aunque lo compartan asesores).
	type schoolAtt struct {
		rendidos     map[string]int
		alumnos      map[string]map[string]struct{}
		rendidosAnio map[string]map[string]int                 // keyID → año → rendidos
		alumnosAnio  map[string]map[string]map[string]struct{} // keyID → año → alumnos distintos
	}
	attCache := map[string]*schoolAtt{}
	attOf := func(schoolID string) *schoolAtt {
		if sa, ok := attCache[schoolID]; ok {
			return sa
		}
		sa := &schoolAtt{
			rendidos:     map[string]int{},
			alumnos:      map[string]map[string]struct{}{},
			rendidosAnio: map[string]map[string]int{},
			alumnosAnio:  map[string]map[string]map[string]struct{}{},
		}
		if at, aerr := p.cli.Attempts.ListByColegio(ctx, &examsgrpcpb.ListAttemptsByColegioRequest{SchoolId: schoolID}); aerr == nil {
			for _, a := range at.GetItems() {
				if a.GetSubmittedAt() == nil || a.GetKeyId() == "" {
					continue
				}
				kid := a.GetKeyId()
				anio := a.GetSubmittedAt().AsTime().Format("2006")
				sa.rendidos[kid]++
				if sa.alumnos[kid] == nil {
					sa.alumnos[kid] = map[string]struct{}{}
				}
				sa.alumnos[kid][a.GetUserId()] = struct{}{}
				if sa.rendidosAnio[kid] == nil {
					sa.rendidosAnio[kid] = map[string]int{}
				}
				sa.rendidosAnio[kid][anio]++
				if sa.alumnosAnio[kid] == nil {
					sa.alumnosAnio[kid] = map[string]map[string]struct{}{}
				}
				if sa.alumnosAnio[kid][anio] == nil {
					sa.alumnosAnio[kid][anio] = map[string]struct{}{}
				}
				sa.alumnosAnio[kid][anio][a.GetUserId()] = struct{}{}
			}
		}
		attCache[schoolID] = sa
		return sa
	}
	now := time.Now()
	out := make([]map[string]any, 0, len(asesores.GetItems()))
	for _, u := range asesores.GetItems() {
		nombre := u.GetFirstName() + " " + u.GetLastName()
		if isQAUser(u.GetEmail(), nombre) {
			continue
		}
		schools, serr := p.cli.Schools.ListSchoolsByAsesor(ctx, &usersgrpcpb.ListSchoolsByAsesorRequest{AsesorId: u.GetId()})
		if serr != nil {
			continue
		}
		keys := []map[string]any{}
		totalColegios := 0
		for _, s := range schools.GetItems() {
			if isQAColegio(s.GetName()) {
				continue
			}
			totalColegios++
			kr, kerr := p.cli.Keys.ListByColegio(ctx, &keysgrpcpb.ListByColegioRequest{SchoolId: s.GetId()})
			if kerr != nil {
				continue
			}
			sa := attOf(s.GetId())
			for _, k := range kr.GetItems() {
				vigente := k.GetActive() && (k.GetValidTo() == nil || k.GetValidTo().AsTime().After(now))
				alumnosAnio := map[string]int{}
				for anio, set := range sa.alumnosAnio[k.GetId()] {
					alumnosAnio[anio] = len(set)
				}
				keys = append(keys, map[string]any{
					"code":              k.GetCode(),
					"colegio":           s.GetName(),
					"tipo":              toolRouteFromExamTypeId(k.GetExamTypeId()),
					"activa":            k.GetActive(),
					"vigente":           vigente,
					"aforo":             k.GetMaxUses(),
					"rendidos":          sa.rendidos[k.GetId()],
					"alumnos":           len(sa.alumnos[k.GetId()]),
					"rendidos_por_anio": sa.rendidosAnio[k.GetId()],
					"alumnos_por_anio":  alumnosAnio,
					"valid_from":        optionalTimestamp(k.GetValidFrom()),
					"valid_to":          optionalTimestamp(k.GetValidTo()),
					"creada_at":         optionalTimestamp(k.GetCreatedAt()),
				})
			}
		}
		out = append(out, map[string]any{
			"asesor_id":      u.GetId(),
			"asesor":         nombre,
			"email":          u.GetEmail(),
			"total_colegios": totalColegios,
			"keys":           keys,
		})
	}
	return out, nil
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
	// Permission gate: el dashboard global lista TODOS los asesores con
	// sus metricas. Cualquier user con analytics.dashboard.read puede
	// verlo (superadmin bypasea por hasPermission). UCSP define quien
	// tiene ese permiso via permission_group. No es un endpoint
	// "superadmin-only" — es un endpoint sensible cuyo acceso se
	// gestiona como cualquier otro recurso del sistema.
	if !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status:  "error",
			Code:    "PERMISSION_DENIED",
			Message: "no tienes permiso analytics.dashboard.read",
		})
		return
	}
	// SEGURIDAD (audit 2026-06-18): este dashboard agrega métricas de TODOS los
	// colegios/asesores. Solo admin/superadmin (unrestricted). Un rol scopeado
	// (asesor/coordinador) tenía analytics.dashboard.read y leía el agregado global
	// de colegios ajenos. El front NO llama este endpoint para roles scopeados
	// (admin usa colegios/historico, asesor usa asesor/{id}/dashboard).
	if unrestricted, _, _ := p.callerColegioScope(r); !unrestricted {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "el dashboard global es solo para administración",
		})
		return
	}
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
	// Scope check: el asesor dueno SIEMPRE puede ver su propio dashboard
	// sin necesidad de un permiso extra. Para ver el de OTRO asesor hace
	// falta analytics.dashboard.read (y superadmin bypasea por hasPermission).
	targetID := r.PathValue("id")
	callerID := userIDFromContext(r)
	if targetID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR", Message: "id de asesor es requerido",
		})
		return
	}
	if targetID != callerID && !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a este asesor",
		})
		return
	}
	resp, err := p.cli.Analytics.GetAsesorDashboard(r.Context(), &analyticsgrpcpb.GetAsesorDashboardRequest{
		AsesorId: targetID,
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
		"total_aforo":       resp.GetTotalAforo(),
		"total_impactados":  resp.GetTotalImpactados(),
		"total_students_rendered": resp.GetTotalStudentsRendered(),
		"by_exam_type":      examTypeStatsToJSON(resp.GetByExamType()),
		"generated_at":      optionalTimestamp(resp.GetGeneratedAt()),
	})
}

func (p *Proxy) getColegioDashboard(w http.ResponseWriter, r *http.Request) {
	schoolID := r.PathValue("id")
	if !p.enforceColegioScope(r, schoolID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this colegio",
		})
		return
	}
	resp, err := p.cli.Analytics.GetColegioDashboard(r.Context(), &analyticsgrpcpb.GetColegioDashboardRequest{
		SchoolId: schoolID,
		// period (opcional): el comparativo lo pasa (?period=YYYY) para que el
		// doughnut/radar/detalle respeten el periodo elegido. Sin él = todo el
		// histórico (el portal de colegio no lo manda).
		Period: r.URL.Query().Get("period"),
		// key_id (opcional): el portal del colegio lo pasa (?key_id=) para
		// analizar promedio/inclinaciones de UNA key concreta. Sin él = todas.
		KeyId: r.URL.Query().Get("key_id"),
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
	userID := r.PathValue("id")
	if !enforceUserScope(r, userID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this user's dashboard",
		})
		return
	}
	resp, err := p.cli.Analytics.GetEstudianteDashboard(r.Context(), &analyticsgrpcpb.GetEstudianteDashboardRequest{
		UserId: userID,
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
	// Permission gate: comparativo agrega data de TODOS los colegios. UCSP
	// asigna analytics.dashboard.read a los users que deban verlo
	// (default: solo superadmin; UCSP puede extender al equipo de
	// gerencia con un permission_group custom).
	if !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso analytics.dashboard.read",
		})
		return
	}
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
	// SEGURIDAD (audit 2026-06-18): el comparativo agrega TODOS los colegios. El
	// front lo usa también para el promedio del asesor, así que en vez de 403 lo
	// FILTRAMOS al alcance de colegios del caller (admin/superadmin = todos). Antes
	// un asesor/coordinador con analytics.dashboard.read veía el promedio de colegios
	// ajenos.
	unrestricted, allowed, _ := p.callerColegioScope(r)
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		if !unrestricted && !allowed[it.GetSchoolId()] {
			continue
		}
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
	userID := r.PathValue("id")
	// Self / admin (callerColegioScope.unrestricted) O un staff con permiso de
	// lectura de attempts. attempt.read habilita ver el histórico de otros (búsqueda
	// por DNI que pidió César), PERO ahora se scopea por colegio abajo.
	unrestricted, allowedColegios, caller := p.callerColegioScope(r)
	selfView := userID == caller
	if !selfView && !unrestricted && !hasPermission(r, "db_exams.exam_attempt.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this user's historial",
		})
		return
	}
	resp, err := p.cli.Analytics.GetHistoricoEstudiante(r.Context(), &analyticsgrpcpb.GetHistoricoEstudianteRequest{
		UserId: userID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := testResultsToJSON(resp.GetItems())
	// Enriquecemos cada item con el CODIGO de la key usada (el card mostraba "KEY —")
	// resolviendo attempt -> key_id -> GetKey. SEGURIDAD (audit 2026-06-18, IDOR):
	// además leemos el SCHOOL de la key para SCOPEAR: un staff scopeado (attempt.read
	// pero no admin) viendo el histórico de OTRO alumno solo ve los intentos de sus
	// colegios. self/admin (needScope=false) ven todo. Intento sin key resoluble
	// (LAN/masivo o key borrada) queda fuera del alcance de un rol scopeado.
	needScope := !selfView && !unrestricted
	keyIDByAttempt := map[string]string{}
	if attemptsResp, aerr := p.cli.Attempts.ListByUser(r.Context(), &examsgrpcpb.ListAttemptsByUserRequest{UserId: userID}); aerr == nil {
		for _, a := range attemptsResp.GetItems() {
			keyIDByAttempt[a.GetId()] = a.GetKeyId()
		}
	}
	type keyMeta struct{ code, school string }
	keyCache := map[string]keyMeta{}
	scoped := items[:0]
	for _, it := range items {
		attemptID, _ := it["attempt_id"].(string)
		var km keyMeta
		if keyID := keyIDByAttempt[attemptID]; keyID != "" {
			cached, ok := keyCache[keyID]
			if !ok {
				if kr, kerr := p.cli.Keys.GetKey(r.Context(), &keysgrpcpb.GetKeyRequest{Id: keyID}); kerr == nil && kr.GetKey() != nil {
					cached = keyMeta{code: kr.GetKey().GetCode(), school: kr.GetKey().GetSchoolId()}
				}
				keyCache[keyID] = cached
			}
			km = cached
		}
		if needScope && !allowedColegios[km.school] {
			continue
		}
		if km.code != "" {
			it["key_code"] = km.code
		}
		scoped = append(scoped, it)
	}
	items = scoped
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":      resp.GetUserId(),
		"items":        items,
		"generated_at": optionalTimestamp(resp.GetGeneratedAt()),
	})
}

// getColegioHistorico — GET /api/analytics/colegio/{id}/historico
// Query params:
//   - exam_type_code (opcional, "" agrega todos los tipos)
//   - periods (opcional, default 8 quarters)
func (p *Proxy) getColegioHistorico(w http.ResponseWriter, r *http.Request) {
	schoolID := r.PathValue("id")
	if !p.enforceColegioScope(r, schoolID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this colegio",
		})
		return
	}
	q := r.URL.Query()
	resp, err := p.cli.Analytics.GetHistoricoColegio(r.Context(), &analyticsgrpcpb.GetHistoricoColegioRequest{
		SchoolId:     schoolID,
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
	targetID := r.PathValue("id")
	callerID := userIDFromContext(r)
	if targetID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR", Message: "id de asesor es requerido",
		})
		return
	}
	if targetID != callerID && !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a este asesor",
		})
		return
	}
	resp, err := p.cli.Analytics.GetAsesorPendientes(r.Context(), &analyticsgrpcpb.GetAsesorPendientesRequest{
		AsesorId: targetID,
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
	userID := r.PathValue("id")
	if !enforceUserScope(r, userID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this user's reporte",
		})
		return
	}
	resp, err := p.cli.Analytics.GetReporteEstudiante(r.Context(), &analyticsgrpcpb.GetReporteEstudianteRequest{
		UserId:    userID,
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
		items := make([]map[string]any, 0, len(s.GetItems()))
		for _, it := range s.GetItems() {
			items = append(items, map[string]any{
				"code":       it.GetCode(),
				"label":      it.GetLabel(),
				"points":     it.GetPoints(),
				"max_points": it.GetMaxPoints(),
			})
		}
		blocks := make([]map[string]any, 0, len(s.GetBlocks()))
		for _, b := range s.GetBlocks() {
			blocks = append(blocks, map[string]any{
				"title": b.GetTitle(),
				"text":  b.GetText(),
			})
		}
		return map[string]any{
			"available":     s.GetAvailable(),
			"reason":        s.GetReason(),
			"result_code":   s.GetResultCode(),
			"result_label":  s.GetResultLabel(),
			"result_detail": s.GetResultDetail(),
			"items":         items,
			"blocks":        blocks,
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
	// Permission gate: vista historica global de TODOS los colegios.
	// Cualquier user con analytics.dashboard.read puede verlo.
	if !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso analytics.dashboard.read",
		})
		return
	}
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
			"school_id":      it.GetSchoolId(),
			"school_name":    it.GetSchoolName(),
			"city":           it.GetCity(),
			"category":       it.GetCategory(),
			"avg_score":      it.GetAvgScore(),
			"attempts":       it.GetAttempts(),
			"variation_pct":  it.GetVariationPct(),
			"has_previous":   it.GetHasPrevious(),
			"top_area_code":  it.GetTopAreaCode(),
			"top_area_label": it.GetTopAreaLabel(),
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
	// El asesor dueno puede exportar SU propio reporte sin necesidad de
	// analytics.export.write (uso self-service). Para exportar el de OTRO
	// asesor hace falta analytics.export.write (= "puede generar XLSX").
	targetID := r.PathValue("id")
	callerID := userIDFromContext(r)
	if targetID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR", Message: "id de asesor es requerido",
		})
		return
	}
	if targetID != callerID && !hasPermission(r, "analytics.export.write") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a este asesor",
		})
		return
	}
	resp, err := p.cli.Analytics.ExportAsesorXLSX(r.Context(), &analyticsgrpcpb.ExportAsesorXLSXRequest{
		AsesorId: targetID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeXLSX(w, resp.GetFilename(), resp.GetContent())
}

func (p *Proxy) exportColegioXLSX(w http.ResponseWriter, r *http.Request) {
	schoolID := r.PathValue("id")
	if !p.enforceColegioScope(r, schoolID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this colegio",
		})
		return
	}
	resp, err := p.cli.Analytics.ExportColegioXLSX(r.Context(), &analyticsgrpcpb.ExportColegioXLSXRequest{
		SchoolId: schoolID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeXLSX(w, resp.GetFilename(), resp.GetContent())
}

func (p *Proxy) exportComparativoXLSX(w http.ResponseWriter, r *http.Request) {
	// Export del comparativo global. Cualquier user con
	// analytics.export.write puede generarlo.
	if !hasPermission(r, "analytics.export.write") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso analytics.export.write",
		})
		return
	}
	// Cliente (2026-06-04): exam_type_code vacio = "Todos los tipos" (igual
	// que la tabla del historico). Ya no es obligatorio. period alinea el xlsx
	// con el periodo seleccionado en la tabla.
	examType := r.URL.Query().Get("exam_type_code")
	resp, err := p.cli.Analytics.ExportComparativoXLSX(r.Context(), &analyticsgrpcpb.ExportComparativoXLSXRequest{
		ExamTypeCode: examType,
		Period:       r.URL.Query().Get("period"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeXLSX(w, resp.GetFilename(), resp.GetContent())
}

// exportReporteEstudianteXLSX — GET /api/analytics/estudiante/{id}/reporte.xlsx
// Genera el reporte "Tour Vocacional UCSP" del estudiante en formato Excel
// (Resumen + RIASEC + Top areas + Secciones). attempt_id opcional via query.
func (p *Proxy) exportReporteEstudianteXLSX(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if !enforceUserScope(r, userID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this user's reporte",
		})
		return
	}
	resp, err := p.cli.Analytics.ExportReporteEstudianteXLSX(r.Context(), &analyticsgrpcpb.ExportReporteEstudianteXLSXRequest{
		UserId:    userID,
		AttemptId: r.URL.Query().Get("attempt_id"),
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
		// areas: inclinaciones agregadas (solo vocacional/estilos) para la
		// barra roja->verde del detalle por colegio.
		areas := make([]map[string]any, 0, len(v.GetAreas()))
		for _, a := range v.GetAreas() {
			areas = append(areas, map[string]any{
				"code":       a.GetCode(),
				"label":      a.GetLabel(),
				"points":     a.GetPoints(),
				"max_points": a.GetMaxPoints(),
				"ratio":      a.GetRatio(),
			})
		}
		out[k] = map[string]any{
			"attempts":      v.GetAttempts(),
			"avg_score":     v.GetAvgScore(),
			"avg_max_score": v.GetAvgMaxScore(),
			"areas":         areas,
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
			"attempt_id":     t.GetAttemptId(),
		})
	}
	return out
}
