package proxy

import (
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
//   /api/auth/*                     auth (login/refresh/logout)
//   /api/users/*                    user_service.UserService
//   /api/schools/*                  user_service (schools)
//   /api/exams/*                    exams_service.ExamService
//   /api/questions/*                exams_service.QuestionService
//   /api/keys/*                     keys_service.KeyService
//   /api/surveys/*                  satisfaction_service.SurveyService
//   /api/analytics/*                analytics_service
//   /api/hubspot/*                  hubspot_service (admin)
func (p *Proxy) RegisterAll(mux *http.ServeMux) {
	p.RegisterAuth(mux)
	p.RegisterHealth(mux)
	// Los demás grupos se registran cuando el frontend los necesite —
	// se siguen el mismo patrón que RegisterAuth (un archivo por dominio
	// con un método Register*). El usuario y el equipo pueden agregar
	// rutas incrementalmente sin tocar este archivo.
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
