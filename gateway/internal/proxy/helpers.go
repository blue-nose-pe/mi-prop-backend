package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"gateway/internal/middleware"

	usersgrpcpb "users_service/proto/gen"

	keysgrpcpb "keys_service/proto/gen"

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

// schoolIDFromContext recupera el school_id del JWT del caller. Lo usa
// enforceColegioScope para reconocer cuentas del rol Colegio que acceden
// a su propio portal. Devuelve "" si el user no tiene school_id atado
// (asesores/admins/superadmin no lo necesitan).
func schoolIDFromContext(r *http.Request) string {
	c := middleware.ClaimsFromContext(r.Context())
	if c == nil {
		return ""
	}
	return c.SchoolID
}

// isSuperadminContext devuelve true si los claims del request incluyen
// el rol "superadmin". Casi nunca se debe usar como GATE directo —
// preferir hasPermission(r, "<code>") que ya bypasea para superadmin.
// Sigue util internamente para distinciones legitimas (ej. let admin
// actuar en nombre de otro user al iniciar un attempt).
func isSuperadminContext(r *http.Request) bool {
	c := middleware.ClaimsFromContext(r.Context())
	if c == nil {
		return false
	}
	for _, role := range c.Roles {
		if role == "superadmin" {
			return true
		}
	}
	return false
}

// hasPermission devuelve true si el caller tiene el permission code
// indicado en sus claims, O si es superadmin (bypass automatico).
//
// Esta es la forma CANONICA de gatear un endpoint. La filosofia del
// sistema es que NADA es exclusivo del superadmin: si UCSP quiere darle
// a un asesor de confianza la posibilidad de ver el comparativo global,
// crea un permission_group con `analytics.dashboard.read` y se lo asigna.
// El superadmin solo es un atajo operacional para no tener que crear el
// grupo al primer install.
//
// Las unicas excepciones son las distinciones operacionales internas
// (ej. "iniciar attempt en nombre de otro user") donde el comportamiento
// difiere entre superadmin y user comun — eso sigue usando
// isSuperadminContext directamente.
func hasPermission(r *http.Request, code string) bool {
	if isSuperadminContext(r) {
		return true
	}
	c := middleware.ClaimsFromContext(r.Context())
	if c == nil {
		return false
	}
	for _, p := range c.Permissions {
		if p == code {
			return true
		}
	}
	return false
}

// writeNotFound responde 404 con el envelope de error estandar cuando un
// resource lookup devuelve gRPC OK pero con payload vacio (upstream que
// no diferencia "ok+nil" de "not found"). Centraliza el shape del JSON.
func writeNotFound(w http.ResponseWriter, resource string) {
	writeJSON(w, http.StatusNotFound, errorBody{
		Status:  "error",
		Code:    "NOT_FOUND",
		Message: resource + " not found",
	})
}

// enforceAsesorScope devuelve el asesor_user_id efectivo para una request:
// los superadmins pueden pasar cualquier valor (incluido "" para listar
// todo); el resto queda forzado a su propio Subject. Devuelve "" si la
// request no esta autenticada (lo cual ya deberia haber bloqueado el
// middleware JWT, pero defendemos aqui igual).
func enforceAsesorScope(r *http.Request, requested string) string {
	if isSuperadminContext(r) {
		return requested
	}
	return userIDFromContext(r)
}

// enforceUserScope devuelve true si el caller puede acceder a recursos
// del user_id `target`: superadmin o self. Lo usan los endpoints que
// toman {id} de path y exponen data sensible del estudiante (attempts,
// dashboards individuales, historicos, reporte vocacional).
func enforceUserScope(r *http.Request, target string) bool {
	if isSuperadminContext(r) {
		return true
	}
	caller := userIDFromContext(r)
	return caller != "" && caller == target
}

// callerOwnsKeyOrSuperadmin: lookup la key y compara asesor_user_id con
// el caller. Tambien acepta asesor que tenga el colegio asignado al
// momento (porque alguien podria heredar keys que el asesor original
// genero). Best-effort: si la key no se puede leer, deniega.
func (p *Proxy) callerOwnsKeyOrSuperadmin(r *http.Request, keyID string) bool {
	if isSuperadminContext(r) {
		return true
	}
	caller := userIDFromContext(r)
	if caller == "" || keyID == "" {
		return false
	}
	resp, err := p.cli.Keys.GetKey(r.Context(), &keysgrpcpb.GetKeyRequest{Id: keyID})
	if err != nil || resp.GetKey() == nil {
		return false
	}
	k := resp.GetKey()
	if k.GetAsesorUserId() == caller {
		return true
	}
	// Fallback: si el caller tiene el school de la key asignado, tambien
	// puede ver attempts/uses (sirve para coordinadores y asesores nuevos
	// que heredaron una key generada por otro asesor del mismo colegio).
	return p.enforceColegioScope(r, k.GetSchoolId())
}

// enforceColegioScope devuelve true si el caller puede ver/modificar
// recursos del colegio target. Politica:
//   - superadmin: cualquier colegio
//   - user con school_id propio == target (cuenta del rol Colegio que
//     accede al portal de su propio colegio)
//   - cualquier user autenticado con assignment vigente al colegio (sea
//     asesor o coordinador). Llama users-service.ListSchoolsByAsesor del
//     caller y chequea inclusion.
//
// Costo: 1 RPC extra al users-service por request. Para optimizar, se
// puede cachear (school list rara vez cambia). Por ahora best-effort
// sincronico — si la consulta falla, devuelve false (cerrado por
// defecto).
func (p *Proxy) enforceColegioScope(r *http.Request, targetSchoolID string) bool {
	if isSuperadminContext(r) {
		return true
	}
	// Admin (admin_permissions): ve TODOS los colegios — no está atado a uno.
	// Se distingue por db_users.permission_group.write, que (verificado en BD:
	// solo admin_permissions lo tiene) ni el asesor ni el coordinador poseen.
	// Sin este caso, un admin no-superadmin (is_superadmin=0, sin school_id y
	// sin assignments) quedaba bloqueado de TODO endpoint scopeado por colegio
	// (dashboard, keys, students, student-key-info) con un 403.
	if hasPermission(r, "db_users.permission_group.write") {
		return true
	}
	caller := userIDFromContext(r)
	if caller == "" || targetSchoolID == "" {
		return false
	}
	// Cuenta del rol Colegio: el school_id del JWT == target.
	if callerSchoolID := schoolIDFromContext(r); callerSchoolID != "" && callerSchoolID == targetSchoolID {
		return true
	}
	resp, err := p.cli.Schools.ListSchoolsByAsesor(r.Context(), &usersgrpcpb.ListSchoolsByAsesorRequest{
		AsesorId: caller,
	})
	if err != nil {
		return false
	}
	for _, s := range resp.GetItems() {
		if s.GetId() == targetSchoolID {
			return true
		}
	}
	return false
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
