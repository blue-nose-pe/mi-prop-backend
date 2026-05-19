package proxy

import (
	"log"
	"net/http"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Proxy agrupa los clientes gRPC y los handlers REST.
// El método Register* registra rutas en un *http.ServeMux.
type Proxy struct {
	cli *Clients
}

func New(c *Clients) *Proxy { return &Proxy{cli: c} }

// RegisterAll registra TODOS los grupos de rutas en el mux.
// Convención de paths del frontend Angular ya en producción:
//   /api/auth/*                     auth (login/refresh/logout/student-otp)
//   /api/users/*                    user_service.UserService + permission groups
//   /api/schools/{id}               user_service.SchoolService
//   /api/permission-groups/*        user_service.PermissionGroupService (CRUD roles)
//   /api/permissions                user_service.PermissionGroupService.ListPermissions
//   /api/exams/*                    exams_service (Exam + ExamQuestion + Attempt)
//   /api/questions/*                exams_service.QuestionService
//   /api/options/*                  exams_service.QuestionService (options)
//   /api/attempts/*                 exams_service.AttemptService
//   /api/keys/*                     keys_service.KeyService
//   /api/asesores/{id}/keys         keys_service.KeyService (ListByAsesor)
//   /api/colegios/{id}/keys         keys_service.KeyService (ListByColegio)
//   /api/surveys/*                  satisfaction_service.SurveyService
//   /api/survey-questions/*         satisfaction_service.SurveyService (questions)
//   /api/survey-responses/*         satisfaction_service.ResponseService
//   /api/analytics/*                analytics_service (dashboards + exports XLSX)
//   /api/hubspot/*                  hubspot_service (admin)
func (p *Proxy) RegisterAll(mux *http.ServeMux) {
	log.Printf("[REG] RegisterAuth start")
	p.RegisterAuth(mux)
	log.Printf("[REG] RegisterAuth done; RegisterHealth start")
	p.RegisterHealth(mux)
	log.Printf("[REG] RegisterHealth done; RegisterUsers start")
	p.RegisterUsers(mux)
	log.Printf("[REG] RegisterUsers done; RegisterVisitas start")
	p.RegisterVisitas(mux)
	log.Printf("[REG] RegisterVisitas done; RegisterPermissionGroups start")
	p.RegisterPermissionGroups(mux)
	log.Printf("[REG] RegisterPermissionGroups done; RegisterExams start")
	p.RegisterExams(mux)
	log.Printf("[REG] RegisterExams done; RegisterKeys start")
	p.RegisterKeys(mux)
	log.Printf("[REG] RegisterKeys done; RegisterSurveys start")
	p.RegisterSurveys(mux)
	log.Printf("[REG] RegisterSurveys done; RegisterAnalytics start")
	p.RegisterAnalytics(mux)
	log.Printf("[REG] RegisterAnalytics done; RegisterHubspot start")
	p.RegisterHubspot(mux)
	log.Printf("[REG] RegisterHubspot done; all routes registered")
}

// RegisterHealth expone /health para readiness/liveness del Ingress.
func (p *Proxy) RegisterHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// optionalTimestamp serializa un *timestamppb.Timestamp como ISO 8601
// o null cuando es nil. Lo usan todos los handlers que devuelven
// entidades con created_at/updated_at.
func optionalTimestamp(t *timestamppb.Timestamp) any {
	if t == nil {
		return nil
	}
	return t.AsTime().UTC().Format(time.RFC3339)
}
