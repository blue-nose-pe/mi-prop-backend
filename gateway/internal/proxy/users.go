// Users + Permission Groups + Schools handlers — proxy a users-service.
//
// Rutas REST:
//   POST   /api/users
//   GET    /api/users/{id}
//   PATCH  /api/users/{id}
//   POST   /api/users/{id}/deactivate
//   POST   /api/users/{id}/reactivate
//   GET    /api/users/me
//   POST   /api/users/me/change-password
//   POST   /api/users/{id}/reset-password
//   GET    /api/users/by-email
//   POST   /api/users/search
//   POST   /api/users/{id}/permissions/groups
//   DELETE /api/users/{id}/permissions/groups/{group_id}
//   GET    /api/users/{id}/permissions
//   GET    /api/users/{id}/permissions/check
//   GET    /api/schools/{id}
//   GET    /api/asesores/{id}/colegios     - colegios asignados al asesor (SCD-2 vigente)
//   POST   /api/asesores/students          - asesor crea estudiante en uno de sus colegios
//   GET    /api/colegios/{id}/students     - estudiantes activos del colegio
package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	hubspotpb "hubspot_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"
	userscommonpb "users_service/proto/gen/common"

	keysgrpcpb "keys_service/proto/gen"

	"google.golang.org/protobuf/types/known/structpb"

	"google.golang.org/grpc/metadata"
)

func (p *Proxy) RegisterUsers(mux *http.ServeMux) {
	// CRUD básico
	mux.HandleFunc("POST /api/users", p.createUser)
	mux.HandleFunc("POST /api/users/bulk", p.bulkCreateUsers)
	mux.HandleFunc("GET /api/users/bulk/template.csv", p.bulkTemplateCSV)
	mux.HandleFunc("GET /api/users/bulk/sample.csv", p.bulkSampleCSV)
	mux.HandleFunc("GET /api/users/me", p.getMe)
	mux.HandleFunc("PATCH /api/users/me", p.updateMe)
	mux.HandleFunc("POST /api/users/me/change-password", p.changeMyPassword)
	mux.HandleFunc("GET /api/users/by-email", p.getUserByEmail)
	mux.HandleFunc("POST /api/users/search", p.searchUsers)
	mux.HandleFunc("GET /api/users/{id}", p.getUser)
	mux.HandleFunc("PATCH /api/users/{id}", p.updateUser)
	mux.HandleFunc("POST /api/users/{id}/deactivate", p.deactivateUser)
	mux.HandleFunc("POST /api/users/{id}/reactivate", p.reactivateUser)
	mux.HandleFunc("POST /api/users/{id}/reset-password", p.resetUserPassword)

	// Permission groups
	mux.HandleFunc("POST /api/users/{id}/permissions/groups", p.assignPermissionGroup)
	mux.HandleFunc("DELETE /api/users/{id}/permissions/groups/{group_id}", p.revokePermissionGroup)
	mux.HandleFunc("GET /api/users/{id}/permissions", p.listUserPermissions)
	mux.HandleFunc("GET /api/users/{id}/groups", p.listUserGroups)
	mux.HandleFunc("GET /api/users/{id}/permissions/check", p.checkUserPermission)

	// Schools
	mux.HandleFunc("POST /api/schools", p.createSchool)
	mux.HandleFunc("GET /api/schools", p.listSchools)
	mux.HandleFunc("GET /api/schools/{id}", p.getSchool)
	mux.HandleFunc("PATCH /api/schools/{id}", p.updateSchool)
	mux.HandleFunc("POST /api/schools/{id}/asesores", p.assignAsesorToSchool)
	// Coordinadores many-to-many (varios por colegio).
	mux.HandleFunc("GET /api/schools/{id}/coordinadores", p.listCoordinadoresBySchool)
	mux.HandleFunc("POST /api/schools/{id}/coordinadores", p.assignCoordinadorToSchool)
	mux.HandleFunc("DELETE /api/schools/{id}/coordinadores/{userId}", p.revokeCoordinadorFromSchool)

	// Atajos semánticos (rolean al endpoint de grupo subyacente con
	// el id del grupo predefinido). Pensados para el front: más obvios
	// que llamar a /api/permission-groups/N/users.
	mux.HandleFunc("GET /api/students", p.listStudents)
	mux.HandleFunc("GET /api/asesores", p.listAsesores)
	mux.HandleFunc("GET /api/coordinadores", p.listCoordinadores)

	// Sub-recursos por id de asesor / colegio.
	mux.HandleFunc("GET /api/asesores/{id}/colegios", p.listColegiosByAsesor)
	mux.HandleFunc("POST /api/asesores/students", p.createStudentByAsesor)
	mux.HandleFunc("GET /api/colegios/{id}/students", p.listStudentsByColegio)

	// Cliente (doc observaciones): historico de grados del alumno por DNI.
	mux.HandleFunc("GET /api/students/grade-history", p.studentGradeHistory)
}

// Atajo: GET /api/students = GET /api/permission-groups/1/users
func (p *Proxy) listStudents(w http.ResponseWriter, r *http.Request) {
	p.listUsersInGroup(w, r, 1, true) // alumnos → scope por colegio
}

// Atajo: GET /api/asesores = GET /api/permission-groups/3/users
func (p *Proxy) listAsesores(w http.ResponseWriter, r *http.Request) {
	p.listUsersInGroup(w, r, 3, false) // staff: no atado a un colegio (reportería)
}

// Atajo: GET /api/coordinadores = GET /api/permission-groups/4/users
func (p *Proxy) listCoordinadores(w http.ResponseWriter, r *http.Request) {
	p.listUsersInGroup(w, r, 4, false)
}

// listUsersInGroup reusa el handler de listGroupUsers pero con el group_id
// fijado por código (no por path param).
//
// Gate: estos listados exponen PII (DNI/email/teléfono). Antes eran accesibles
// a CUALQUIER autenticado — un alumno (que tenía db_users.users.read) podía
// enumerar el directorio entero de alumnos/asesores/coordinadores (PII de
// menores, Ley 29733). Ahora exigen analytics.dashboard.read (lo tienen
// admin/asesor/coordinador/marketing; el alumno NO). Además, el listado de
// ALUMNOS se filtra al colegio del caller (scopeByColegio) para que un
// asesor/coordinador solo vea SUS alumnos, no los de todos los colegios.
func (p *Proxy) listUsersInGroup(w http.ResponseWriter, r *http.Request, groupID uint32, scopeByColegio bool) {
	if !hasPermission(r, "analytics.dashboard.read") {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "PERMISSION_DENIED",
			Message: "no tienes permiso para listar usuarios (analytics.dashboard.read)",
		})
		return
	}
	q := r.URL.Query()
	resp, err := p.cli.PermGroups.ListGroupUsers(r.Context(), &usersgrpcpb.ListGroupUsersRequest{
		GroupId:    groupID,
		Search:     q.Get("search"),
		Limit:      parseUint32Query(q.Get("limit"), 100),
		Offset:     parseUint32Query(q.Get("offset"), 0),
		ActiveOnly: q.Get("active_only") == "true" || q.Get("active_only") == "1",
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	users := resp.GetItems()
	total := resp.GetTotal()
	if scopeByColegio {
		if unrestricted, allowed, caller := p.callerColegioScope(r); !unrestricted {
			filtered := make([]*usersgrpcpb.User, 0, len(users))
			for _, u := range users {
				if (caller != "" && u.GetId() == caller) || (u.GetSchoolId() != "" && allowed[u.GetSchoolId()]) {
					filtered = append(filtered, u)
				}
			}
			users = filtered
			total = uint32(len(filtered))
		}
	}
	items := make([]map[string]any, 0, len(users))
	for _, u := range users {
		items = append(items, protoUserToJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

type createSchoolRequest struct {
	Name            string `json:"name"`
	UserID          string `json:"user_id"`
	HubspotRecordID string `json:"hubspot_record_id"`
	City            string `json:"city"`
	Category        string `json:"category"`
	Code            string `json:"code"`        // codigo modular MINEDU
	Penetration     string `json:"penetration"` // Alta/Media/Baja
	Email           string `json:"email"`       // contacto del colegio, opcional al crear
	Phone           string `json:"phone"`       // contacto del colegio, opcional al crear
	Ruc             string `json:"ruc"`         // RUC del colegio, opcional al crear
	Poblacion       string `json:"poblacion"`   // poblacion estudiantil, opcional al crear
	PersonalACargo  string `json:"personal_a_cargo"` // persona/contacto a cargo, opcional
}

func (p *Proxy) createSchool(w http.ResponseWriter, r *http.Request) {
	var in createSchoolRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Schools.CreateSchool(r.Context(), &usersgrpcpb.CreateSchoolRequest{
		Name:            in.Name,
		UserId:          in.UserID,
		HubspotRecordId: in.HubspotRecordID,
		City:            in.City,
		Category:        in.Category,
		Code:            in.Code,
		Penetration:     in.Penetration,
		Email:           in.Email,
		Phone:           in.Phone,
		Ruc:             in.Ruc,
		Poblacion:       in.Poblacion,
		PersonalACargo:  in.PersonalACargo,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"school": protoSchoolToJSON(resp.GetSchool())})
}

type updateSchoolRequest struct {
	Name            string `json:"name"`
	UserID          string `json:"user_id"`
	HubspotRecordID string `json:"hubspot_record_id"`
	City            string `json:"city"`        // "" no toca; "-" limpia
	Category        string `json:"category"`    // "" no toca; "-" limpia
	Code            string `json:"code"`        // "" no toca; "-" limpia
	Penetration     string `json:"penetration"` // "" no toca; "-" limpia
	Email           string `json:"email"`       // "" no toca; "-" limpia
	Phone           string `json:"phone"`       // "" no toca; "-" limpia
	Ruc             string `json:"ruc"`         // "" no toca; "-" limpia
	Poblacion       string `json:"poblacion"`   // "" no toca; "-" limpia
	PersonalACargo  string `json:"personal_a_cargo"` // "" no toca; "-" limpia
	Active          string `json:"active"`      // C1: "" no toca; "true"/"false"
}

func (p *Proxy) updateSchool(w http.ResponseWriter, r *http.Request) {
	// Fix auditoría 2026-07-02 (BOLA/IDOR de escritura): igual que getSchool ya
	// scopea la LECTURA, la ESCRITURA también debe hacerlo. Sin esto, cualquier
	// grupo con db_users.school.write podía PATCH-ear CUALQUIER colegio ajeno
	// (phone/email/RUC/hubspot_record_id/user_id/active). admin/superadmin pasan.
	if !p.enforceColegioScope(r, r.PathValue("id")) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "COLEGIO_SCOPE", Message: "no tienes acceso a este colegio"})
		return
	}
	var in updateSchoolRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Schools.UpdateSchool(r.Context(), &usersgrpcpb.UpdateSchoolRequest{
		Id:              r.PathValue("id"),
		Name:            in.Name,
		UserId:          in.UserID,
		HubspotRecordId: in.HubspotRecordID,
		City:            in.City,
		Category:        in.Category,
		Code:            in.Code,
		Penetration:     in.Penetration,
		Email:           in.Email,
		Phone:           in.Phone,
		Ruc:             in.Ruc,
		Poblacion:       in.Poblacion,
		PersonalACargo:  in.PersonalACargo,
		Active:          in.Active,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"school": protoSchoolToJSON(resp.GetSchool())})
}

func (p *Proxy) getSchool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "MISSING_ID", Message: "id is required"})
		return
	}
	resp, err := p.cli.Schools.GetSchool(r.Context(), &usersgrpcpb.GetSchoolRequest{Id: id})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	s := resp.GetSchool()
	if s == nil {
		writeJSON(w, http.StatusNotFound, errorBody{Status: "error", Code: "SCHOOL_NOT_FOUND", Message: "school not found"})
		return
	}
	// SCOPE por colegio (audit permisos 2026-07-02, fuga PII): el LISTADO ya
	// scopeaba, pero el registro individual GET /api/schools/{id} devolvía a
	// cualquier asesor/coordinador el registro COMPLETO (RUC, población,
	// hubspot_record_id, user_id) de un colegio AJENO. Ahora un no-admin solo
	// puede leer sus propios colegios; superadmin/admin (unrestricted) ven todo.
	if !p.enforceColegioScope(r, s.GetId()) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "COLEGIO_SCOPE", Message: "no tienes acceso a este colegio"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"school": protoSchoolToJSON(s)})
}

// listSchools - GET /api/schools?search=&limit=&offset=&active_only=
// Devuelve {items: [...], total: N}. Pensado para llenar dropdowns y tablas.
func (p *Proxy) listSchools(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseUint32Query(q.Get("limit"), 100)
	offset := parseUint32Query(q.Get("offset"), 0)
	activeOnly := q.Get("active_only") == "true" || q.Get("active_only") == "1"

	resp, err := p.cli.Schools.ListSchools(r.Context(), &usersgrpcpb.ListSchoolsRequest{
		Search:     q.Get("search"),
		Limit:      limit,
		Offset:     offset,
		ActiveOnly: activeOnly,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	// SCOPE por colegio (IDOR fix): un asesor/coordinador NO debe ver el
	// directorio completo de colegios. Superadmin/admin (unrestricted) ven todo;
	// el resto solo sus colegios (asesor_de_colegio + coordinador_de_colegio via
	// callerColegioScope). Antes /api/schools devolvía TODOS a cualquier caller,
	// filtrándose la lista de colegios en reportería (ej. reporte-colegios).
	unrestricted, allowed, _ := p.callerColegioScope(r)
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, s := range resp.GetItems() {
		if !unrestricted && !allowed[s.GetId()] {
			continue
		}
		items = append(items, protoSchoolToJSON(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": int32(len(items)),
	})
}

func protoSchoolToJSON(s *usersgrpcpb.School) map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"id":                s.GetId(),
		"user_id":           s.GetUserId(),
		"name":              s.GetName(),
		"city":              s.GetCity(),
		"category":          s.GetCategory(),
		"code":              s.GetCode(),
		"penetration":       s.GetPenetration(),
		"email":             s.GetEmail(),
		"phone":             s.GetPhone(),
		"ruc":               s.GetRuc(),
		"poblacion":         s.GetPoblacion(),
		"personal_a_cargo":  s.GetPersonalACargo(),
		"asesor_user_id":    s.GetAsesorUserId(),
		"asesor_name":       s.GetAsesorName(),
		// Coordinadores many-to-many (nombres "A, B, C"). `coordinador` es el
		// campo que lee el card del listado.
		"coordinador":           s.GetCoordinadoresNombres(),
		"coordinadores_nombres": s.GetCoordinadoresNombres(),
		"active":                s.GetActive(),
		"hubspot_record_id": s.GetHubspotRecordId(),
		"created_at":        optionalTimestamp(s.GetCreatedAt()),
		"updated_at":        optionalTimestamp(s.GetUpdatedAt()),
	}
}

// parseUint32Query devuelve el valor del query string como uint32, o el
// default si está vacío o es inválido.
func parseUint32Query(s string, defVal uint32) uint32 {
	if s == "" {
		return defVal
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return defVal
	}
	return uint32(n)
}

// ---------- CRUD ----------

type createUserRequest struct {
	Email             string `json:"email"`
	Password          string `json:"password"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	DocumentNumber    string `json:"document_number"`
	Phone             string `json:"phone"`
	SchoolID          string `json:"school_id"`
	PermissionGroupID uint32 `json:"permission_group_id"`
}

func (p *Proxy) createUser(w http.ResponseWriter, r *http.Request) {
	var in createUserRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	// ANTI-ESCALADA (audit permisos 2026-07-02, CRÍTICO): un caller no-admin con
	// db_users.users.write (el coordinador, que lo tiene para gestionar ESTUDIANTES)
	// podía POST /api/users con permission_group_id=asesor y fabricar cuentas de
	// staff. Ahora un caller no-admin SOLO puede crear ESTUDIANTES, y dentro de su
	// alcance de colegio. El admin (permission_group.write) sigue creando cualquiera.
	if !callerIsUserAdmin(r) {
		if !p.isStudentGroupID(r, in.PermissionGroupID) {
			writeJSON(w, http.StatusForbidden, errorBody{
				Status:  "error",
				Code:    "CANNOT_CREATE_PRIVILEGED_USER",
				Message: "solo puedes crear usuarios de tipo estudiante",
			})
			return
		}
		if in.SchoolID == "" || !p.enforceColegioScope(r, in.SchoolID) {
			writeJSON(w, http.StatusForbidden, errorBody{
				Status:  "error",
				Code:    "COLEGIO_SCOPE",
				Message: "solo puedes crear estudiantes en tus colegios",
			})
			return
		}
	}
	resp, err := p.cli.Users.CreateUser(r.Context(), &usersgrpcpb.CreateUserRequest{
		Email:             in.Email,
		Password:          in.Password,
		FirstName:         in.FirstName,
		LastName:          in.LastName,
		DocumentNumber:    in.DocumentNumber,
		Phone:             in.Phone,
		SchoolId:          in.SchoolID,
		PermissionGroupId: in.PermissionGroupID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(resp.GetUser())})
}

func (p *Proxy) getUser(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Users.GetUser(r.Context(), &usersgrpcpb.GetUserRequest{Id: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	u := resp.GetUser()
	if u == nil {
		writeNotFound(w, "user")
		return
	}
	// SEGURIDAD (audit 2026-06-18, IDOR): antes GET /api/users/{id} devolvía la PII
	// (DNI/email/tel) de CUALQUIER usuario a cualquier asesor/coordinador forjando
	// el id — fuga de datos de menores cross-colegio (Ley 29733), la contraparte
	// del scoping que ya tenía /api/users/search. Scopeamos los usuarios CON
	// colegio (alumnos) al alcance del caller. Pasan: el propio usuario (flujos
	// públicos del examen hidratan su registro con su token) y admin/superadmin
	// (bypass dentro de enforceColegioScope). Staff sin school_id no se scopea
	// para no romper lookups internos de la UI.
	if u.GetId() != userIDFromContext(r) && u.GetSchoolId() != "" && !p.enforceColegioScope(r, u.GetSchoolId()) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "PERMISSION_DENIED", Message: "no tienes acceso a este usuario"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(u)})
}

type updateUserRequest struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	DocumentNumber string `json:"document_number"`
	Phone          string `json:"phone"`
	SchoolID       string `json:"school_id"`
}

func (p *Proxy) updateUser(w http.ResponseWriter, r *http.Request) {
	var in updateUserRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if !p.callerCanManageTargetUser(r, r.PathValue("id")) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "CANNOT_MODIFY_STAFF", Message: "solo puedes editar estudiantes de tus colegios"})
		return
	}
	resp, err := p.cli.Users.UpdateUser(r.Context(), &usersgrpcpb.UpdateUserRequest{
		Id:             r.PathValue("id"),
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		DocumentNumber: in.DocumentNumber,
		Phone:          in.Phone,
		SchoolId:       in.SchoolID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	u := resp.GetUser()
	if u == nil {
		writeNotFound(w, "user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(u)})
}

func (p *Proxy) deactivateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Permitido: admin cualquiera; no-admin solo estudiantes de sus colegios O
	// coordinadores de sus colegios (asesor con coordinador.write). Desactivar
	// bloquea el login (users.active=false).
	if !p.callerCanManageTargetUser(r, id) && !p.callerCanDeactivateCoordinador(r, id) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "CANNOT_MODIFY_STAFF", Message: "no tienes permiso para desactivar a este usuario"})
		return
	}
	if _, err := p.cli.Users.DeactivateUser(r.Context(), &usersgrpcpb.DeactivateUserRequest{
		Id: r.PathValue("id"),
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) reactivateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !p.callerCanManageTargetUser(r, id) && !p.callerCanDeactivateCoordinador(r, id) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "CANNOT_MODIFY_STAFF", Message: "no tienes permiso para reactivar a este usuario"})
		return
	}
	resp, err := p.cli.Users.ReactivateUser(r.Context(), &usersgrpcpb.ReactivateUserRequest{
		Id: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	u := resp.GetUser()
	if u == nil {
		writeNotFound(w, "user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(u)})
}

// ---------- Self ("me") ----------

func (p *Proxy) getMe(w http.ResponseWriter, r *http.Request) {
	// users-service usa el `Subject` del JWT inyectado por el server-side
	// interceptor; el gateway le pasa un EmptyRequest y users-service
	// extrae al caller del context (forwarded via metadata interna).
	resp, err := p.cli.Users.Me(r.Context(), &usersgrpcpb.EmptyRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	perms := resp.GetPermissions()
	if perms == nil {
		perms = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        protoUserToJSON(resp.GetUser()),
		"permissions": perms,
	})
}

// updateMeRequest — el alumno actualiza sus PROPIOS datos de contacto. No lleva
// id: users-service usa el Subject del JWT del caller. school_id no editable.
type updateMeRequest struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	DocumentNumber string `json:"document_number"`
	Phone          string `json:"phone"`
}

// updateMe — PATCH /api/users/me. Cualquier autenticado (incl. alumno post-OTP)
// edita su propio perfil. La autorización real la hace users-service atándolo
// al Subject del JWT, así que aquí NO pasamos ningún id.
func (p *Proxy) updateMe(w http.ResponseWriter, r *http.Request) {
	var in updateMeRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Users.UpdateMe(r.Context(), &usersgrpcpb.UpdateMeRequest{
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		DocumentNumber: in.DocumentNumber,
		Phone:          in.Phone,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	u := resp.GetUser()
	if u == nil {
		writeNotFound(w, "user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(u)})
}

type changeMyPasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (p *Proxy) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	var in changeMyPasswordRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Users.ChangeMyPassword(r.Context(), &usersgrpcpb.ChangeMyPasswordRequest{
		OldPassword: in.OldPassword,
		NewPassword: in.NewPassword,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Users.ResetPassword(r.Context(), &usersgrpcpb.ResetPasswordRequest{
		TargetUserId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"temp_password": resp.GetTempPassword()})
}

// ---------- Búsquedas ----------

func (p *Proxy) getUserByEmail(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "email query param is required"})
		return
	}
	resp, err := p.cli.Users.GetUserByEmail(r.Context(), &usersgrpcpb.GetUserByEmailRequest{Email: email})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	u := resp.GetUser()
	if u == nil {
		writeNotFound(w, "user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(u)})
}

func (p *Proxy) searchUsers(w http.ResponseWriter, r *http.Request) {
	req := &userscommonpb.SearchRequest{}
	if err := decodeSearchRequest(r, req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	// SCOPE por colegio: el asesor/coordinador (y cualquier no-admin) solo puede
	// ver alumnos/staff de SUS colegios (o a sí mismo). Antes /api/users/search
	// devolvía DNI/email/teléfono/school_id de TODOS los colegios a cualquier
	// asesor → fuga de PII de menores (Ley 29733). Si el caller pidió props
	// específicas, forzamos school_id para poder filtrar; si pidió todas
	// (properties vacío) ya viene incluida.
	unrestricted, allowed, caller := p.callerColegioScope(r)
	// Un estudiante solo puede verse a SÍ MISMO en la búsqueda (no al roster de
	// su colegio): allowed vacío → scopeSearchResults solo conserva id==caller.
	// El auto-lookup del flujo de examen busca su propio email y lo encuentra.
	if callerIsStudentLike(r) {
		unrestricted = false
		allowed = map[string]bool{}
	}
	if !unrestricted && len(req.GetProperties()) > 0 {
		req.Properties = append(req.Properties, "school_id")
	}
	resp, err := p.cli.Users.SearchUsers(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	results := resp.GetResults()
	total := resp.GetTotal()
	if !unrestricted {
		results = scopeSearchResults(results, allowed, caller, false) // usuarios: sin LAN
		total = uint32(len(results))
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*userscommonpb.SearchResult, *userscommonpb.Paging](
		total, results, resp.GetPaging(),
	))
}

// ---------- Permission Groups ----------

type assignPermissionGroupRequest struct {
	PermissionGroupID uint32 `json:"permission_group_id"`
}

func (p *Proxy) assignPermissionGroup(w http.ResponseWriter, r *http.Request) {
	var in assignPermissionGroupRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Users.AssignPermissionGroup(r.Context(), &usersgrpcpb.AssignGroupRequest{
		UserId:            r.PathValue("id"),
		PermissionGroupId: in.PermissionGroupID,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) revokePermissionGroup(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.PathValue("group_id")
	gid, err := strconv.ParseUint(groupIDStr, 10, 32)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status: "error", Code: "VALIDATION_ERROR", Message: "group_id must be a positive integer",
		})
		return
	}
	if _, err := p.cli.Users.RevokePermissionGroup(r.Context(), &usersgrpcpb.RevokeGroupRequest{
		UserId:            r.PathValue("id"),
		PermissionGroupId: uint32(gid),
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) listUserPermissions(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Users.ListUserPermissions(r.Context(), &usersgrpcpb.ListPermsRequest{
		UserId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	codes := resp.GetCodes()
	if codes == nil {
		codes = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

// listUserGroups — GET /api/users/{id}/groups. Grupos (perfiles de acceso) a
// los que pertenece el usuario. Lo consume la ficha del asesor para mostrar el
// grupo actual y permitir cambiarlo (assign/revoke).
func (p *Proxy) listUserGroups(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Users.ListUserGroups(r.Context(), &usersgrpcpb.ListPermsRequest{
		UserId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(resp.GetItems()))
	for _, g := range resp.GetItems() {
		out = append(out, permissionGroupToJSON(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (p *Proxy) checkUserPermission(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "VALIDATION_ERROR", Message: "code query param is required"})
		return
	}
	resp, err := p.cli.Users.HasPermission(r.Context(), &usersgrpcpb.HasPermissionRequest{
		UserId: r.PathValue("id"),
		Code:   code,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"allowed": resp.GetAllowed()})
}

// ---------- Sub-recursos por asesor / colegio ----------

// listColegiosByAsesor — GET /api/asesores/{id}/colegios
// Lista los colegios donde el user es el asesor vigente (assignment SCD-2
// con valid_to IS NULL). Vacío si el asesor no tiene asignaciones.
func (p *Proxy) listColegiosByAsesor(w http.ResponseWriter, r *http.Request) {
	asesorID := r.PathValue("id")
	resp, err := p.cli.Schools.ListSchoolsByAsesor(r.Context(), &usersgrpcpb.ListSchoolsByAsesorRequest{
		AsesorId: asesorID,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	// Estos colegios son, por definicion del endpoint, los del asesor `asesorID`.
	// El listado no denormaliza el NOMBRE del asesor (asesor_name viene vacio),
	// asi que la card mostraba el generico "Asignado" en vez del nombre. Como
	// TODOS los items comparten el mismo asesor, resolvemos su nombre una sola
	// vez (self-access si el caller es el propio asesor; users.read si es admin)
	// y lo estampamos. Best-effort: si falla, se deja como estaba.
	asesorName := ""
	if uresp, uerr := p.cli.Users.GetUser(r.Context(), &usersgrpcpb.GetUserRequest{Id: asesorID}); uerr == nil {
		if u := uresp.GetUser(); u != nil {
			asesorName = strings.TrimSpace(u.GetFirstName() + " " + u.GetLastName())
		}
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, s := range resp.GetItems() {
		m := protoSchoolToJSON(s)
		if asesorName != "" {
			if cur, _ := m["asesor_name"].(string); strings.TrimSpace(cur) == "" {
				m["asesor_name"] = asesorName
			}
		}
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": resp.GetTotal(),
	})
}

// assignAsesorToSchool — POST /api/schools/{id}/asesores
// Body: {"user_id": "<asesor-uuid>"}.
// Crea una assignment SCD-2 kind=asesor_de_colegio. La SchoolService la
// implementa cerrando la vigente y abriendo una nueva en una sola tx.
func (p *Proxy) assignAsesorToSchool(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if in.UserID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "MISSING_USER_ID", Message: "user_id is required"})
		return
	}
	// Fix auditoría 2026-07-02: escritura sobre un colegio exige tener ese colegio
	// en scope (admin/superadmin pasan). Antes cualquier school.write reasignaba
	// el asesor de CUALQUIER colegio.
	if !p.enforceColegioScope(r, r.PathValue("id")) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "COLEGIO_SCOPE", Message: "no tienes acceso a este colegio"})
		return
	}
	if _, err := p.cli.Schools.AssignAsesor(r.Context(), &usersgrpcpb.AssignAsesorRequest{
		SchoolId: r.PathValue("id"),
		UserId:   in.UserID,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// listCoordinadoresBySchool — GET /api/schools/{id}/coordinadores
func (p *Proxy) listCoordinadoresBySchool(w http.ResponseWriter, r *http.Request) {
	// Fix auditoría 2026-07-02 (IDOR/PII): sin este gate CUALQUIER autenticado
	// (incluso un alumno) iteraba UUIDs de colegio y cosechaba nombre+email de los
	// coordinadores de todos los colegios (ListCoordinadoresBySchool tampoco está
	// en el permission_map → whitelist-by-default PASS). Igual que sus hermanos
	// listStudentsByColegio / studentKeyInfoByColegio, exigimos scope por colegio.
	if !p.enforceColegioScope(r, r.PathValue("id")) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "COLEGIO_SCOPE", Message: "no tienes acceso a este colegio"})
		return
	}
	resp, err := p.cli.Schools.ListCoordinadoresBySchool(r.Context(), &usersgrpcpb.ListCoordinadoresBySchoolRequest{
		SchoolId: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, c := range resp.GetItems() {
		nombre := strings.TrimSpace(c.GetFirstName() + " " + c.GetLastName())
		items = append(items, map[string]any{
			"id":                 c.GetId(),
			"coordinador_id":     c.GetId(),
			"coordinador_nombre": nombre,
			"nombre":             nombre,
			"email":              c.GetEmail(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// assignCoordinadorToSchool — POST /api/schools/{id}/coordinadores  Body: {user_id}
func (p *Proxy) assignCoordinadorToSchool(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if in.UserID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "MISSING_USER_ID", Message: "user_id is required"})
		return
	}
	// Fix auditoría 2026-07-02: el asesor (coordinador.write) solo asigna
	// coordinadores a SUS colegios; admin/superadmin, a cualquiera.
	if !p.enforceColegioScope(r, r.PathValue("id")) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "COLEGIO_SCOPE", Message: "no tienes acceso a este colegio"})
		return
	}
	// Validar que el coordinador exista ANTES de intentar el assignment: sin esto
	// un user_id inexistente reventaba el AddSource con FK violation → 500
	// INTERNAL_ERROR (audit permisos 2026-07-02). Ahora devuelve 404 limpio.
	if ur, err := p.cli.Users.GetUser(r.Context(), &usersgrpcpb.GetUserRequest{Id: in.UserID}); err != nil || ur.GetUser() == nil {
		writeJSON(w, http.StatusNotFound, errorBody{Status: "error", Code: "USER_NOT_FOUND", Message: "el coordinador indicado no existe"})
		return
	}
	if _, err := p.cli.Schools.AssignCoordinador(r.Context(), &usersgrpcpb.AssignCoordinadorRequest{
		SchoolId: r.PathValue("id"),
		UserId:   in.UserID,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// revokeCoordinadorFromSchool — DELETE /api/schools/{id}/coordinadores/{userId}
func (p *Proxy) revokeCoordinadorFromSchool(w http.ResponseWriter, r *http.Request) {
	// Fix auditoría 2026-07-02: revocar un coordinador exige tener el colegio en
	// scope (admin/superadmin pasan).
	if !p.enforceColegioScope(r, r.PathValue("id")) {
		writeJSON(w, http.StatusForbidden, errorBody{Status: "error", Code: "COLEGIO_SCOPE", Message: "no tienes acceso a este colegio"})
		return
	}
	if _, err := p.cli.Schools.RevokeCoordinador(r.Context(), &usersgrpcpb.RevokeCoordinadorRequest{
		SchoolId: r.PathValue("id"),
		UserId:   r.PathValue("userId"),
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// createStudentByAsesor — POST /api/asesores/students
//
// Crea un usuario con permission_group_id=1 (student) dentro de uno de los
// colegios asignados al asesor que hace la request. Pensado para que el
// asesor de un colegio pueda dar de alta estudiantes manualmente sin
// depender del flujo de auto-registro con key publica.
//
// Body: {"school_id", "email", "first_name", "last_name", "document_number"?, "phone"?}
//
// Autorizacion:
//   - JWT obligatorio (no esta en la skip-list).
//   - Si el caller NO es superadmin: school_id DEBE estar entre los
//     colegios asignados al asesor (ListSchoolsByAsesor). Si no, 403.
//   - Superadmin puede crear estudiantes en cualquier colegio (uso ops).
//
// Password: se genera una random de 48 chars hex y se descarta. El
// estudiante hace login solo via OTP (request-otp + verify-otp), nunca
// con password. Esto matchea el comportamiento de RegisterStudentWithKey.
func (p *Proxy) createStudentByAsesor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SchoolID       string `json:"school_id"`
		Email          string `json:"email"`
		FirstName      string `json:"first_name"`
		LastName       string `json:"last_name"`
		DocumentNumber string `json:"document_number"`
		Phone          string `json:"phone"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if in.SchoolID == "" || in.Email == "" || in.FirstName == "" || in.LastName == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status:  "error",
			Code:    "VALIDATION_ERROR",
			Message: "school_id, email, first_name y last_name son obligatorios",
		})
		return
	}

	asesorID := userIDFromContext(r)
	if asesorID == "" {
		writeJSON(w, http.StatusUnauthorized, errorBody{Status: "error", Code: "UNAUTHORIZED", Message: "no auth"})
		return
	}

	// Autorizacion por colegio: si el caller NO es superadmin, validamos
	// que el school_id este en su lista de asignaciones SCD-2 vigentes.
	// Esto bloquea que un asesor cree estudiantes en colegios ajenos.
	if !isSuperadminContext(r) {
		sresp, err := p.cli.Schools.ListSchoolsByAsesor(r.Context(), &usersgrpcpb.ListSchoolsByAsesorRequest{
			AsesorId: asesorID,
		})
		if err != nil {
			writeGRPCError(w, err)
			return
		}
		found := false
		for _, s := range sresp.GetItems() {
			if s.GetId() == in.SchoolID {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusForbidden, errorBody{
				Status:  "error",
				Code:    "FORBIDDEN_SCHOOL",
				Message: "no podes crear estudiantes en un colegio que no te esta asignado",
			})
			return
		}
	}

	// Password descartable (24 bytes -> 48 chars hex). El estudiante no
	// la usa nunca; su login es OTP-only.
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody{Status: "error", Code: "INTERNAL", Message: "rand"})
		return
	}
	randomPwd := hex.EncodeToString(buf)

	resp, err := p.cli.Users.CreateUser(r.Context(), &usersgrpcpb.CreateUserRequest{
		Email:             in.Email,
		Password:          randomPwd,
		FirstName:         in.FirstName,
		LastName:          in.LastName,
		DocumentNumber:    in.DocumentNumber,
		Phone:             in.Phone,
		SchoolId:          in.SchoolID,
		PermissionGroupId: 1, // student_permissions (= /api/students shortcut)
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	// Hidratar el contacto en HubSpot con TODOS los props + asociacion al
	// colegio. users_service.CreateUser dispara un upsert minimo
	// async (email+firstname+lastname+dni) que NO incluye phone, ni los
	// flags `origina_de_mi_proposito_`/`sincronizado_por_mi_proposito_`,
	// ni el id del colegio, ni la asociacion Contact <-> Company. Sin esto
	// el contacto queda incompleto en el CRM y el cliente UCSP no lo ve
	// vinculado a su colegio. Best-effort: si HubSpot falla, el user
	// ya esta creado y devolvemos 201 igual.
	// La goroutine corre DESPUES de devolver el 201, asi que NO podemos
	// reusar r.Context() (se cancela al cerrar la response). Usamos un
	// context.Background() con timeout propio. 15s alcanza para 2 RPCs
	// (GetSchool + SyncStudentContact) que llaman a HubSpot.
	//
	// Capturamos el Bearer del request antes de lanzar la goroutine y lo
	// re-adjuntamos a la outgoing metadata, porque users_service/hubspot
	// requieren JWT del caller (middleware.JWT lo agrega solo cuando hay
	// un r.Context() vivo — en background no existe).
	authz := r.Header.Get("Authorization")
	go func(schoolID, email, firstName, lastName, docNumber, phone, authzHeader string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if authzHeader != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", authzHeader)
		}
		schoolResp, err := p.cli.Schools.GetSchool(ctx, &usersgrpcpb.GetSchoolRequest{Id: schoolID})
		if err != nil || schoolResp == nil || schoolResp.GetSchool() == nil {
			log.Printf("[asesor-create-student] GetSchool FAIL id=%s err=%v", schoolID, err)
			return
		}
		s := schoolResp.GetSchool()
		if _, err := p.cli.Hubspot.SyncStudentContact(ctx, &hubspotpb.SyncStudentContactRequest{
			Email:          email,
			FirstName:      firstName,
			LastName:       lastName,
			DocumentNumber: docNumber,
			Phone:          phone,
			SchoolIntId:    s.GetIntId(),
			SchoolRecordId: s.GetHubspotRecordId(),
		}); err != nil {
			log.Printf("[asesor-create-student] SyncStudentContact FAIL email=%s err=%v", email, err)
		}
	}(in.SchoolID, in.Email, in.FirstName, in.LastName, in.DocumentNumber, in.Phone, authz)

	writeJSON(w, http.StatusCreated, map[string]any{"user": protoUserToJSON(resp.GetUser())})
}

// listStudentsByColegio — GET /api/colegios/{id}/students
// Atajo HubSpot-style: bajo el capó arma una SearchUsers con filtro
// school_id={id}, properties básicas y limit alto. Devuelve la misma shape
// que /api/users/search ({total, results[], paging}).
func (p *Proxy) listStudentsByColegio(w http.ResponseWriter, r *http.Request) {
	schoolID := r.PathValue("id")
	if schoolID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "MISSING_ID", Message: "id is required"})
		return
	}
	// Bug 1 fix: solo staff con jurisdiccion sobre este colegio.
	if !p.enforceColegioScope(r, schoolID) {
		writeJSON(w, http.StatusForbidden, errorBody{
			Status: "error", Code: "FORBIDDEN", Message: "no access to this colegio",
		})
		return
	}
	q := r.URL.Query()
	limit := parseUint32Query(q.Get("limit"), 200)
	if limit > 1000 {
		limit = 1000
	}
	req := &userscommonpb.SearchRequest{
		FilterGroups: []*userscommonpb.FilterGroup{{
			Filters: []*userscommonpb.Filter{
				{
					PropertyName: "school_id",
					Operator:     userscommonpb.FilterOperator_EQ,
					Values:       []string{schoolID},
				},
				{
					PropertyName: "active",
					Operator:     userscommonpb.FilterOperator_EQ,
					Values:       []string{"true"},
				},
			},
		}},
		Properties: []string{"email", "first_name", "last_name", "document_number", "phone", "school_id", "active"},
		Limit:      limit,
		After:      parseUint32Query(q.Get("after"), 0),
	}
	resp, err := p.cli.Users.SearchUsers(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	// Bug A fix: listado aditivo. Ademas de users.school_id=X, incluimos a
	// estudiantes que rindieron tests con keys de este colegio aunque su
	// users.school_id apunte a otro (caso: alumno se registro con una key
	// de otro colegio antes, pero este asesor le dio su key local y la uso).
	extra := p.fetchStudentsByKeyAttempts(r.Context(), schoolID, resp.GetResults())
	merged := append(resp.GetResults(), extra...)
	writeJSON(w, http.StatusOK, searchResponseToJSON[*userscommonpb.SearchResult, *userscommonpb.Paging](
		resp.GetTotal()+uint32(len(extra)), merged, resp.GetPaging(),
	))
}

// fetchStudentsByKeyAttempts trae users que usaron keys de este colegio
// pero cuyo users.school_id apunta a otro lado. Best-effort: si falla
// alguna llamada, devuelve vacio (no rompe el listado principal).
func (p *Proxy) fetchStudentsByKeyAttempts(ctx context.Context, schoolID string, alreadyIn []*userscommonpb.SearchResult) []*userscommonpb.SearchResult {
	uidsResp, err := p.cli.Keys.ListUserIDsByColegio(ctx, &keysgrpcpb.ListByColegioRequest{SchoolId: schoolID})
	if err != nil || uidsResp == nil {
		return nil
	}
	seen := make(map[string]bool, len(alreadyIn))
	for _, u := range alreadyIn {
		seen[u.GetId()] = true
	}
	var out []*userscommonpb.SearchResult
	for _, uid := range uidsResp.GetUserIds() {
		if seen[uid] {
			continue
		}
		uresp, err := p.cli.Users.GetUser(ctx, &usersgrpcpb.GetUserRequest{Id: uid})
		if err != nil || uresp.GetUser() == nil {
			continue
		}
		u := uresp.GetUser()
		props, _ := structpb.NewStruct(map[string]interface{}{
			"email":            u.GetEmail(),
			"first_name":       u.GetFirstName(),
			"last_name":        u.GetLastName(),
			"document_number":  u.GetDocumentNumber(),
			"phone":            u.GetPhone(),
			"school_id":        u.GetSchoolId(),
			"active":           u.GetActive(),
			"via_key_attempts": true, // hint para el front: viene via attempts
		})
		out = append(out, &userscommonpb.SearchResult{
			Id:         u.GetId(),
			Properties: props,
		})
		seen[uid] = true
	}
	return out
}

