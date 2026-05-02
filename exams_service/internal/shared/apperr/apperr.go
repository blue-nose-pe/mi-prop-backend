// Package apperr: errores tipados + traductor unico a la frontera gRPC.
//
// Uso desde el core / adapters:
//     return domain.ErrEmailTaken          // singleton tipado
//     return search.UnknownProperty(name)  // factory con contexto
//
// Uso desde el handler gRPC:
//     return nil, apperr.ToGRPC(ctx, err)
//
// Agregar un error nuevo: UNA linea en el paquete correspondiente.
// El mapper NO se toca: ToGRPC usa el Kind del Error para elegir
// codes.Code + ErrorCategory automaticamente.
package apperr

import (
	"context"
	"errors"

	commonpb "exams_service/proto/gen/common"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToGRPC traduce CUALQUIER error al envelope estandar (HubSpot-style).
// Si err no es *apperr.Error, produce un Internal generico para no
// filtrar detalles internos al cliente. El error original queda
// accesible via errors.Unwrap para logging upstream.
func ToGRPC(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	var ae *Error
	if !errors.As(err, &ae) {
		ae = NewInternal("INTERNAL_ERROR", "an unexpected error occurred", err)
	}

	code, category := transportFor(ae.Kind)

	detail := &commonpb.ErrorDetail{Code: ae.Code, Message: ae.Message}
	if ae.Field != "" {
		detail.Context = map[string]*commonpb.StringList{
			"propertyName": {Values: []string{ae.Field}},
		}
	}

	payload := &commonpb.ErrorResponse{
		Status:        "error",
		Message:       ae.Message,
		Errors:        []*commonpb.ErrorDetail{detail},
		Category:      category,
		CorrelationId: CorrelationIDFromContext(ctx),
	}

	st := status.New(code, ae.Message)
	if withDetails, derr := st.WithDetails(payload); derr == nil {
		return withDetails.Err()
	}
	return st.Err()
}

// transportFor mapea Kind (taxonomia de dominio) a codes.Code (gRPC) +
// ErrorCategory (envelope). UNICO lugar que hace este mapping.
func transportFor(k Kind) (codes.Code, commonpb.ErrorCategory) {
	switch k {
	case KindValidation:
		return codes.InvalidArgument, commonpb.ErrorCategory_VALIDATION_ERROR
	case KindConflict:
		return codes.AlreadyExists, commonpb.ErrorCategory_CONFLICT
	case KindNotFound:
		return codes.NotFound, commonpb.ErrorCategory_NOT_FOUND
	case KindUnauthenticated:
		return codes.Unauthenticated, commonpb.ErrorCategory_UNAUTHENTICATED
	case KindPermissionDenied:
		return codes.PermissionDenied, commonpb.ErrorCategory_PERMISSION_DENIED
	default:
		return codes.Internal, commonpb.ErrorCategory_INTERNAL_ERROR
	}
}
