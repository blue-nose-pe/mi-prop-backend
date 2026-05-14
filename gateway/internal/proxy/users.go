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
package proxy

import (
	"net/http"
	"strconv"

	usersgrpcpb "users_service/proto/gen"
	userscommonpb "users_service/proto/gen/common"
)

func (p *Proxy) RegisterUsers(mux *http.ServeMux) {
	// CRUD básico
	mux.HandleFunc("POST /api/users", p.createUser)
	mux.HandleFunc("POST /api/users/bulk", p.bulkCreateUsers)
	mux.HandleFunc("GET /api/users/bulk/template.csv", p.bulkTemplateCSV)
	mux.HandleFunc("GET /api/users/bulk/sample.csv", p.bulkSampleCSV)
	mux.HandleFunc("GET /api/users/me", p.getMe)
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
	mux.HandleFunc("GET /api/users/{id}/permissions/check", p.checkUserPermission)

	// Schools
	mux.HandleFunc("POST /api/schools", p.createSchool)
	mux.HandleFunc("GET /api/schools", p.listSchools)
	mux.HandleFunc("GET /api/schools/{id}", p.getSchool)
	mux.HandleFunc("PATCH /api/schools/{id}", p.updateSchool)

	// Atajos semánticos (rolean al endpoint de grupo subyacente con
	// el id del grupo predefinido). Pensados para el front: más obvios
	// que llamar a /api/permission-groups/N/users.
	mux.HandleFunc("GET /api/students", p.listStudents)
	mux.HandleFunc("GET /api/asesores", p.listAsesores)
	mux.HandleFunc("GET /api/coordinadores", p.listCoordinadores)
}

// Atajo: GET /api/students = GET /api/permission-groups/1/users
func (p *Proxy) listStudents(w http.ResponseWriter, r *http.Request) {
	p.listUsersInGroup(w, r, 1)
}

// Atajo: GET /api/asesores = GET /api/permission-groups/3/users
func (p *Proxy) listAsesores(w http.ResponseWriter, r *http.Request) {
	p.listUsersInGroup(w, r, 3)
}

// Atajo: GET /api/coordinadores = GET /api/permission-groups/4/users
func (p *Proxy) listCoordinadores(w http.ResponseWriter, r *http.Request) {
	p.listUsersInGroup(w, r, 4)
}

// listUsersInGroup reusa el handler de listGroupUsers pero con el group_id
// fijado por código (no por path param).
func (p *Proxy) listUsersInGroup(w http.ResponseWriter, r *http.Request, groupID uint32) {
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
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, u := range resp.GetItems() {
		items = append(items, protoUserToJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": resp.GetTotal(),
	})
}

type createSchoolRequest struct {
	Name            string `json:"name"`
	UserID          string `json:"user_id"`
	HubspotRecordID string `json:"hubspot_record_id"`
	City            string `json:"city"`
	Category        string `json:"category"`
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
	City            string `json:"city"`     // "" no toca; "-" limpia
	Category        string `json:"category"` // "" no toca; "-" limpia
}

func (p *Proxy) updateSchool(w http.ResponseWriter, r *http.Request) {
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
	items := make([]map[string]any, 0, len(resp.GetItems()))
	for _, s := range resp.GetItems() {
		items = append(items, protoSchoolToJSON(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": resp.GetTotal(),
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
		"active":            s.GetActive(),
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
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(resp.GetUser())})
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
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(resp.GetUser())})
}

func (p *Proxy) deactivateUser(w http.ResponseWriter, r *http.Request) {
	if _, err := p.cli.Users.DeactivateUser(r.Context(), &usersgrpcpb.DeactivateUserRequest{
		Id: r.PathValue("id"),
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) reactivateUser(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.Users.ReactivateUser(r.Context(), &usersgrpcpb.ReactivateUserRequest{
		Id: r.PathValue("id"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(resp.GetUser())})
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
	writeJSON(w, http.StatusOK, map[string]any{"user": protoUserToJSON(resp.GetUser())})
}

func (p *Proxy) searchUsers(w http.ResponseWriter, r *http.Request) {
	req := &userscommonpb.SearchRequest{}
	if err := decodeSearchRequest(r, req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Users.SearchUsers(r.Context(), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, searchResponseToJSON[*userscommonpb.SearchResult, *userscommonpb.Paging](
		resp.GetTotal(), resp.GetResults(), resp.GetPaging(),
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
