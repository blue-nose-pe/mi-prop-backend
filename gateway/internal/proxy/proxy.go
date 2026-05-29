package proxy

import (
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Proxy agrupa los clientes gRPC y los handlers REST.
// El método Register* registra rutas en un *http.ServeMux.
//
// rdb (opcional, puede ser nil) habilita rate-limits per-endpoint mas
// estrictos que el global de main.go. Se usa para endpoints publicos
// sensibles a enumeration (Bug #27: lookup-by-key).
type Proxy struct {
	cli *Clients
	rdb *redis.Client
}

func New(c *Clients) *Proxy { return &Proxy{cli: c} }

// NewWithRedis crea un Proxy con rate-limit per-endpoint habilitado.
// Si rdb es nil cae a noop (igual que New).
func NewWithRedis(c *Clients, rdb *redis.Client) *Proxy {
	return &Proxy{cli: c, rdb: rdb}
}

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
	p.RegisterAuth(mux)
	p.RegisterHealth(mux)
	p.RegisterCareers(mux)
	p.RegisterUsers(mux)
	p.RegisterVisitas(mux)
	p.RegisterPermissionGroups(mux)
	p.RegisterExams(mux)
	p.RegisterKeys(mux)
	p.RegisterSurveys(mux)
	p.RegisterAnalytics(mux)
	p.RegisterHubspot(mux)
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
