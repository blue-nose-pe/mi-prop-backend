package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"gateway/internal/middleware"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
// + JSON envelope estándar HubSpot-style. Si el upstream adjunta su
// commonpb.ErrorResponse en los Details (lo hace apperr.ToGRPC), se
// prefiere el code especifico de la app (p.ej. EXAM_CODE_TAKEN) sobre
// la categoria generica (CONFLICT).
func writeGRPCError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorBody{
			Status: "error", Message: err.Error(), Code: "INTERNAL_ERROR",
		})
		return
	}
	httpCode, ourCode := mapCode(st.Code())
	if appCode := appCodeFromDetails(st); appCode != "" {
		ourCode = appCode
	}
	writeJSON(w, httpCode, errorBody{
		Status:  "error",
		Message: st.Message(),
		Code:    ourCode,
	})
}

// appCodeFromDetails extrae el primer code de application-level desde el
// commonpb.ErrorResponse que apperr.ToGRPC adjunta como detail. Como cada
// servicio define su propio paquete commonpb (mismo schema, distinto type),
// usamos protojson para parsear sin acoplarnos a un servicio concreto.
func appCodeFromDetails(st *status.Status) string {
	for _, d := range st.Details() {
		msg, ok := d.(proto.Message)
		if !ok {
			continue
		}
		raw, err := protojson.Marshal(msg)
		if err != nil {
			continue
		}
		var payload struct {
			Errors []struct {
				Code string `json:"code"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		if len(payload.Errors) > 0 && payload.Errors[0].Code != "" {
			return payload.Errors[0].Code
		}
	}
	return ""
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

// userIDFromContext recupera el subject del JWT (= user_id) desde el
// context inyectado por el middleware JWT. Devuelve "" si no hay claims
// (ruta pública o token no presente).
func userIDFromContext(r *http.Request) string {
	c := middleware.ClaimsFromContext(r.Context())
	if c == nil {
		return ""
	}
	return c.Subject
}

// decodeSearchRequest deserializa el body JSON estilo HubSpot
// (filter_groups / properties / sorts / limit / after) directamente al
// `commonpb.SearchRequest` de cualquier servicio. Los seis servicios
// generan el mismo schema en sus respectivos paquetes `commonpb`, así
// que con protojson + proto.Message logramos mapear genéricamente.
//
// Nota: protojson respeta los nombres `snake_case` del .proto, que es
// lo que el openapi expone, así que no hace falta traducción extra.
func decodeSearchRequest(r *http.Request, dst proto.Message) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		// SearchRequest con valores default es válido (devuelve los primeros 50).
		return nil
	}
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	return opts.Unmarshal(body, dst)
}

// searchResultLike y searchPagingLike son interfaces que cumplen las
// versiones por servicio de `*commonpb.SearchResult` y `*commonpb.Paging`.
// Permiten un único helper para serializar SearchResponses sin importar
// el paquete exacto.
type searchResultLike interface {
	GetId() string
	GetProperties() *structpb.Struct
	GetCreatedAt() *timestamppb.Timestamp
	GetUpdatedAt() *timestamppb.Timestamp
	GetArchived() bool
}

type searchPagingLike interface {
	GetNextAfter() uint32
	GetHasMore() bool
}

// searchResponseToJSON construye el JSON envelope estándar
// `{ total, results, paging }` a partir de cualquier SearchResponse
// gRPC. Resuelve la conversión `structpb.Struct → map[string]any` para
// que el front lo reciba como objeto JSON normal.
func searchResponseToJSON[R searchResultLike, P searchPagingLike](
	total uint32, results []R, paging P,
) map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		var props map[string]any
		if r.GetProperties() != nil {
			props = r.GetProperties().AsMap()
		} else {
			props = map[string]any{}
		}
		out = append(out, map[string]any{
			"id":         r.GetId(),
			"created_at": optionalTimestamp(r.GetCreatedAt()),
			"updated_at": optionalTimestamp(r.GetUpdatedAt()),
			"archived":   r.GetArchived(),
			"properties": props,
		})
	}
	pagingObj := map[string]any{
		"next_after": paging.GetNextAfter(),
		"has_more":   paging.GetHasMore(),
	}
	return map[string]any{
		"total":   total,
		"results": out,
		"paging":  pagingObj,
	}
}

// parseRFC3339 convierte un string ISO 8601 a *timestamppb.Timestamp.
// Vacío → nil. Inválido → nil + err para que el handler responda 400.
func parseRFC3339(s string) (*timestamppb.Timestamp, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return timestamppb.New(t), nil
}
