// Package auditmw provee un interceptor gRPC que registra cada mutación
// (Create*/Update*/Delete*/Assign*/Revoke*) en una tabla audit_log local
// del microservicio.
//
// Por qué local y no centralizado: bounded context. Cada servicio
// auditando su dominio. Un dashboard cross-audit lo arma analytics-service
// agregando los logs vía gRPC.
//
// La tabla audit_log debe existir en la BD del servicio:
//
//	CREATE TABLE audit_log (
//	    id              UNIQUEIDENTIFIER PRIMARY KEY DEFAULT NEWID(),
//	    actor_user_id   UNIQUEIDENTIFIER NULL,
//	    action          NVARCHAR(255) NOT NULL,   -- ej "users.Create"
//	    entity_type     NVARCHAR(100) NULL,
//	    entity_id       NVARCHAR(255) NULL,
//	    payload_json    NVARCHAR(MAX) NULL,
//	    correlation_id  NVARCHAR(64)  NULL,
//	    ip              NVARCHAR(64)  NULL,
//	    success         BIT NOT NULL,
//	    error_message   NVARCHAR(MAX) NULL,
//	    created_at      DATETIME2(7)  NOT NULL DEFAULT SYSUTCDATETIME(),
//	);
package auditmw

import (
	"context"
	"encoding/json"
	"strings"

	"users_service/internal/shared/jwtmw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// Sink persiste un evento de auditoría. El microservicio implementa
// esta interfaz contra su tabla audit_log.
type Sink interface {
	Record(ctx context.Context, ev Event) error
}

// Event es la unidad de auditoría que llega al sink.
type Event struct {
	ActorUserID   string // sub del JWT, vacío si la request es anónima
	Action        string // FullMethod sin prefijo, ej "users.UserService/CreateUser"
	PayloadJSON   string // request serializada (sin campos sensibles, ver Sanitizer)
	CorrelationID string
	IP            string
	Success       bool
	ErrorMessage  string
}

// Sanitizer permite redactar campos sensibles del request antes de
// serializar (passwords, tokens, etc.). Si nil, no se aplica redacción.
type Sanitizer func(method string, req proto.Message) proto.Message

// IsMutation devuelve true para los métodos que el interceptor debe
// auditar. Por convención: Create*, Update*, Delete*, Assign*, Revoke*,
// Login, Refresh, Logout. Lectura puro (Get*, List*, Search*) no se audita.
func IsMutation(fullMethod string) bool {
	idx := strings.LastIndex(fullMethod, "/")
	if idx < 0 || idx == len(fullMethod)-1 {
		return false
	}
	name := fullMethod[idx+1:]
	for _, prefix := range mutationPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

var mutationPrefixes = []string{
	"Create", "Update", "Delete", "Deactivate", "Activate",
	"Assign", "Revoke", "Login", "Logout", "Refresh", "Reset",
	"Submit", "Publish", "Generate",
}

// UnaryServerInterceptor audita cada llamada que pase IsMutation. El
// resultado del handler (success/error) se registra junto con el actor
// del JWT (si presente) y el correlation_id.
func UnaryServerInterceptor(sink Sink, sanitize Sanitizer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)

		if !IsMutation(info.FullMethod) {
			return resp, err
		}

		ev := Event{
			Action:        info.FullMethod,
			Success:       err == nil,
			ActorUserID:   actorFromContext(ctx),
			CorrelationID: correlationFromContext(ctx),
			IP:            ipFromContext(ctx),
		}
		if err != nil {
			ev.ErrorMessage = err.Error()
		}
		if pm, ok := req.(proto.Message); ok {
			payload := pm
			if sanitize != nil {
				payload = sanitize(info.FullMethod, pm)
			}
			if b, mErr := protoToJSON(payload); mErr == nil {
				ev.PayloadJSON = string(b)
			}
		}
		_ = sink.Record(ctx, ev)
		return resp, err
	}
}

func actorFromContext(ctx context.Context) string {
	if c := jwtmw.FromContext(ctx); c != nil {
		return c.Subject
	}
	return ""
}

func correlationFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if vs := md.Get("x-correlation-id"); len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func ipFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if vs := md.Get("x-forwarded-for"); len(vs) > 0 {
		return vs[0]
	}
	if vs := md.Get("x-real-ip"); len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func protoToJSON(m proto.Message) ([]byte, error) {
	// proto.Marshal entrega binario; para legibilidad en BD usamos JSON.
	// Convertir vía json.Marshal directo funciona para mensajes sin
	// tipos exóticos (Any, Timestamp, etc). Para casos más ricos
	// podemos cambiar a protojson más adelante.
	return json.Marshal(m)
}
