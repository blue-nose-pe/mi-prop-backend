package apperr

import "context"

// Kind: taxonomia universal de errores. Independiente del transporte
// (gRPC, HTTP, CLI) — cada transporte mapea a sus propios codigos.
type Kind int

const (
	KindUnspecified Kind = iota
	KindValidation       // datos de entrada invalidos
	KindConflict         // choque con estado actual (duplicado, etc.)
	KindNotFound         // recurso no existe
	KindUnauthenticated  // credenciales faltantes / invalidas
	KindPermissionDenied // autenticado pero sin permiso
	KindInternal         // fallo inesperado
)

// Error: formato unico para errores que cruzan capas del sistema.
// Los services y adapters lo producen; la frontera (gRPC) lo traduce
// via ToGRPC sin necesidad de switches por caso.
//
// Agregar un error nuevo = declarar una var en domain/errors.go o
// llamar un factory. Nada mas se toca.
type Error struct {
	Kind    Kind
	Code    string // machine code estable: "EMAIL_TAKEN"
	Message string // humano: "email already registered"
	Field   string // opcional: nombre del campo asociado
	cause   error  // opcional: error original (solo para logs, NO se expone)
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }

// ---------- Constructores (una linea por error) ----------

func NewValidation(code, message, field string) *Error {
	return &Error{Kind: KindValidation, Code: code, Message: message, Field: field}
}

func NewConflict(code, message, field string) *Error {
	return &Error{Kind: KindConflict, Code: code, Message: message, Field: field}
}

func NewNotFound(code, message string) *Error {
	return &Error{Kind: KindNotFound, Code: code, Message: message}
}

func NewUnauthenticated(code, message string) *Error {
	return &Error{Kind: KindUnauthenticated, Code: code, Message: message}
}

func NewPermissionDenied(code, message string) *Error {
	return &Error{Kind: KindPermissionDenied, Code: code, Message: message}
}

func NewInternal(code, message string, cause error) *Error {
	return &Error{Kind: KindInternal, Code: code, Message: message, cause: cause}
}

// ---------- Correlation ID ----------

type ctxKey struct{}

var correlationIDKey = ctxKey{}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

func CorrelationIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(correlationIDKey).(string); ok {
		return v
	}
	return ""
}
