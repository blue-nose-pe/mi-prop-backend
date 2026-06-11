package proxy

import (
	"net/http"
	"sort"
	"strings"
	"time"

	analyticsgrpcpb "analytics_service/proto/gen"
	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	userscommonpb "users_service/proto/gen/common"
)

// examTypeName mapea el exam_type_id (1/2/3) a un nombre legible.
func examTypeName(id int32) string {
	switch id {
	case 1:
		return "Vocacional"
	case 2:
		return "Simulacro"
	case 3:
		return "Estilos de aprendizaje"
	default:
		return "—"
	}
}

// studentGradeHistory — GET /api/students/grade-history?dni=<dni>
//
// Cliente (doc observaciones): el DNI es el identificador ESTABLE del alumno;
// un estudiante puede registrarse en 4to y luego en 5to con correos distintos
// pero el mismo DNI, y es el mismo alumno. Por eso el historico se arma por
// DNI (no por user_id, que puede duplicarse entre años).
//
// Composicion: users(by document_number) -> attempts(by user) -> key(by id),
// para resolver grado / año / codigo de key / tipo de examen por cada intento.
func (p *Proxy) studentGradeHistory(w http.ResponseWriter, r *http.Request) {
	dni := strings.TrimSpace(r.URL.Query().Get("dni"))
	if dni == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "MISSING_DNI", Message: "dni is required"})
		return
	}
	// Ver attempts de otros alumnos requiere este permiso (mismo gate que
	// /api/exams/{id}/attempts y /api/users/{id}/attempts cross-user).
	if !hasPermission(r, "db_exams.exam_attempt.read") {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes permiso db_exams.exam_attempt.read"})
		return
	}

	// 1) Usuarios con ese DNI (pueden ser varios: distintos años/correos).
	usersResp, err := p.cli.Users.SearchUsers(r.Context(), &userscommonpb.SearchRequest{
		FilterGroups: []*userscommonpb.FilterGroup{{
			Filters: []*userscommonpb.Filter{{
				PropertyName: "document_number",
				Operator:     userscommonpb.FilterOperator_EQ,
				Values:       []string{dni},
			}},
		}},
		Properties: []string{"first_name", "last_name", "email", "document_number"},
		Limit:      100,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	userIDs := make([]string, 0)
	studentName := ""
	for _, u := range usersResp.GetResults() {
		userIDs = append(userIDs, u.GetId())
		if studentName == "" && u.GetProperties() != nil {
			props := u.GetProperties().AsMap()
			fn, _ := props["first_name"].(string)
			ln, _ := props["last_name"].(string)
			if n := strings.TrimSpace(fn + " " + ln); n != "" {
				studentName = n
			}
		}
	}

	type histItem struct {
		Year     int    `json:"year"`
		Grade    string `json:"grade"`
		ExamType string `json:"exam_type"`
		KeyCode  string `json:"key_code"`
		// Score/MaxScore solo tienen sentido en SIMULACRO (es la unica prueba
		// medible por puntaje). Para vocacional/estilos el front no los muestra.
		Score    float64 `json:"score"`
		MaxScore float64 `json:"max_score"`
		// IsScored marca si la prueba se mide por puntaje (solo simulacro).
		IsScored bool `json:"is_scored"`
		// Highlight: para vocacional/estilos, el area/inclinacion principal del
		// alumno (p. ej. "CÁLCULO") en vez de un puntaje. "" si no aplica/no se
		// pudo calcular.
		Highlight   string `json:"highlight"`
		SubmittedAt string `json:"submitted_at"`
	}

	// Cache de keys por id — un alumno suele compartir la misma key entre
	// varios intentos; evitamos GetKey repetidos.
	keyCache := map[string]*keysgrpcpb.Key{}
	items := make([]histItem, 0)
	for _, uid := range userIDs {
		attemptsResp, aerr := p.cli.Attempts.ListByUser(r.Context(), &examsgrpcpb.ListAttemptsByUserRequest{UserId: uid})
		if aerr != nil {
			continue // best-effort: un user que falle no rompe el historico
		}
		for _, a := range attemptsResp.GetItems() {
			if a.GetSubmittedAt() == nil {
				continue // solo intentos finalizados cuentan en el historico
			}
			keyID := a.GetKeyId()
			key, ok := keyCache[keyID]
			if !ok && keyID != "" {
				if kr, kerr := p.cli.Keys.GetKey(r.Context(), &keysgrpcpb.GetKeyRequest{Id: keyID}); kerr == nil {
					key = kr.GetKey()
				}
				keyCache[keyID] = key // cachea incluso nil para no reintentar
			}
			year := a.GetSubmittedAt().AsTime().Year()
			grade, code, examType := "—", "—", "—"
			var examTypeID int32
			if key != nil {
				if key.GetGrade() != "" {
					grade = key.GetGrade()
				}
				if key.GetCode() != "" {
					code = key.GetCode()
				}
				examTypeID = key.GetExamTypeId()
				examType = examTypeName(examTypeID)
				if key.GetValidFrom() != nil {
					year = key.GetValidFrom().AsTime().Year()
				}
			}
			// Solo el simulacro (exam_type 2) se mide por puntaje. Para
			// vocacional (1) y estilos (3) traemos la INCLINACION del alumno (su
			// area principal) en vez de un numero, via el reporte del attempt.
			isScored := examTypeID == 2
			highlight := ""
			if examTypeID == 1 || examTypeID == 3 {
				if rep, rerr := p.cli.Analytics.GetReporteEstudiante(r.Context(), &analyticsgrpcpb.GetReporteEstudianteRequest{
					UserId:    uid,
					AttemptId: a.GetId(),
				}); rerr == nil {
					if top := rep.GetAreasInteres().GetTop(); len(top) > 0 {
						highlight = top[0].GetAreaLabel()
						if highlight == "" {
							highlight = top[0].GetLabel()
						}
					}
				}
			}
			items = append(items, histItem{
				Year:        year,
				Grade:       grade,
				ExamType:    examType,
				KeyCode:     code,
				Score:       a.GetScore(),
				MaxScore:    a.GetMaxScore(),
				IsScored:    isScored,
				Highlight:   highlight,
				SubmittedAt: a.GetSubmittedAt().AsTime().Format(time.RFC3339),
			})
		}
	}

	// Orden cronologico: por año asc, luego grado.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Year != items[j].Year {
			return items[i].Year < items[j].Year
		}
		return items[i].Grade < items[j].Grade
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"dni":      dni,
		"name":     studentName,
		"user_ids": userIDs,
		"items":    items,
	})
}
