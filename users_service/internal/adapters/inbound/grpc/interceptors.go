package grpchandler

import (
	"context"
	"log"
	"runtime/debug"
	"time"

	"users_service/internal/shared/apperr"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// metadataHeaderCorrelationID es la key gRPC (minusculas por convencion)
// con la que viaja el correlation_id entre servicios.
const metadataHeaderCorrelationID = "x-correlation-id"

// CorrelationIDInterceptor garantiza que TODA request tenga un
// correlation_id:
//   - Si el caller ya envio uno en metadata, lo reusa (trazabilidad
//     end-to-end cuando gateway -> users_service -> otro servicio).
//   - Si no, genera un UUID v4 nuevo.
// Lo pone en ctx (apperr lo lee al construir errores) y lo copia al
// header de respuesta para que el cliente pueda loguearlo.
func CorrelationIDInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(metadataHeaderCorrelationID); len(v) > 0 && v[0] != "" {
			id = v[0]
		}
	}
	if id == "" {
		id = uuid.NewString()
	}
	ctx = apperr.WithCorrelationID(ctx, id)
	_ = grpc.SetHeader(ctx, metadata.Pairs(metadataHeaderCorrelationID, id))
	return handler(ctx, req)
}

// LoggingInterceptor: 1 linea por request con metodo, duracion, codigo
// y correlation_id (para cruzar con logs del gateway / otros servicios).
func LoggingInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	log.Printf("grpc method=%s code=%s dur=%s cid=%s",
		info.FullMethod,
		status.Code(err),
		time.Since(start),
		apperr.CorrelationIDFromContext(ctx),
	)
	return resp, err
}

// RecoveryInterceptor evita que un panic tire el proceso. Lo convierte
// en un Internal (pasa por el envelope estandar igualmente: el cliente
// ve el mismo formato que cualquier otro error).
func RecoveryInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in %s: %v\n%s", info.FullMethod, r, debug.Stack())
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}
