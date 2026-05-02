package proxy

import (
	"encoding/json"
	"errors"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// readJSON deserializa el body en `dst`. Reusa los errores comunes
// (formato malo → 400) para que cada handler no repita la lógica.
func readJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeGRPCError mapea un error gRPC del upstream a HTTP equivalente
// + JSON envelope estándar HubSpot-style. Códigos basados en
// google.golang.org/grpc/codes.
func writeGRPCError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorBody{
			Status: "error", Message: err.Error(), Code: "INTERNAL_ERROR",
		})
		return
	}
	httpCode, ourCode := mapCode(st.Code())
	writeJSON(w, httpCode, errorBody{
		Status:  "error",
		Message: st.Message(),
		Code:    ourCode,
	})
}

type errorBody struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func mapCode(c codes.Code) (int, string) {
	switch c {
	case codes.OK:
		return http.StatusOK, ""
	case codes.InvalidArgument, codes.FailedPrecondition:
		return http.StatusBadRequest, "VALIDATION_ERROR"
	case codes.Unauthenticated:
		return http.StatusUnauthorized, "UNAUTHENTICATED"
	case codes.PermissionDenied:
		return http.StatusForbidden, "PERMISSION_DENIED"
	case codes.NotFound:
		return http.StatusNotFound, "NOT_FOUND"
	case codes.AlreadyExists:
		return http.StatusConflict, "CONFLICT"
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests, "RATE_LIMIT"
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, "DEADLINE_EXCEEDED"
	case codes.Unavailable:
		return http.StatusServiceUnavailable, "UNAVAILABLE"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR"
	}
}
