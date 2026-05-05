// Permission groups + catálogo de permisos individuales — proxy a
// users-service.PermissionGroupService.
//
// Rutas REST:
//   POST   /api/permission-groups
//   GET    /api/permission-groups
//   GET    /api/permission-groups/{id}
//   PATCH  /api/permission-groups/{id}
//   DELETE /api/permission-groups/{id}
//   POST   /api/permission-groups/{id}/permissions/{permission_id}
//   DELETE /api/permission-groups/{id}/permissions/{permission_id}
//   GET    /api/permissions
package proxy

import (
	"net/http"
	"strconv"

	usersgrpcpb "users_service/proto/gen"
)

func (p *Proxy) RegisterPermissionGroups(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/permission-groups", p.createPermissionGroup)
	mux.HandleFunc("GET /api/permission-groups", p.listPermissionGroups)
	mux.HandleFunc("GET /api/permission-groups/{id}", p.getPermissionGroup)
	mux.HandleFunc("PATCH /api/permission-groups/{id}", p.updatePermissionGroup)
	mux.HandleFunc("DELETE /api/permission-groups/{id}", p.deletePermissionGroup)
	mux.HandleFunc("POST /api/permission-groups/{id}/permissions/{permission_id}", p.addPermissionToGroup)
	mux.HandleFunc("DELETE /api/permission-groups/{id}/permissions/{permission_id}", p.removePermissionFromGroup)
	mux.HandleFunc("GET /api/permissions", p.listPermissions)
}

// ---------- DTOs ----------

type createPermissionGroupRequest struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	PermissionIDs []uint32 `json:"permission_ids,omitempty"`
}

type updatePermissionGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ---------- Handlers ----------

func (p *Proxy) createPermissionGroup(w http.ResponseWriter, r *http.Request) {
	var in createPermissionGroupRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.PermGroups.CreateGroup(r.Context(), &usersgrpcpb.CreateGroupRequest{
		Code:          in.Code,
		Name:          in.Name,
		Description:   in.Description,
		PermissionIds: in.PermissionIDs,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": permissionGroupToJSON(resp.GetGroup())})
}

func (p *Proxy) listPermissionGroups(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.PermGroups.ListGroups(r.Context(), &usersgrpcpb.EmptyRequest{})
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

func (p *Proxy) getPermissionGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUint32Path(w, r, "id")
	if !ok {
		return
	}
	resp, err := p.cli.PermGroups.GetGroup(r.Context(), &usersgrpcpb.GetGroupRequest{Id: id})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": permissionGroupToJSON(resp.GetGroup())})
}

func (p *Proxy) updatePermissionGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUint32Path(w, r, "id")
	if !ok {
		return
	}
	var in updatePermissionGroupRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.PermGroups.UpdateGroup(r.Context(), &usersgrpcpb.UpdateGroupRequest{
		Id:          id,
		Name:        in.Name,
		Description: in.Description,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": permissionGroupToJSON(resp.GetGroup())})
}

func (p *Proxy) deletePermissionGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUint32Path(w, r, "id")
	if !ok {
		return
	}
	if _, err := p.cli.PermGroups.DeleteGroup(r.Context(), &usersgrpcpb.DeleteGroupRequest{Id: id}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) addPermissionToGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseUint32Path(w, r, "id")
	if !ok {
		return
	}
	permID, ok := parseUint32Path(w, r, "permission_id")
	if !ok {
		return
	}
	if _, err := p.cli.PermGroups.AddPermission(r.Context(), &usersgrpcpb.AddPermissionRequest{
		GroupId:      groupID,
		PermissionId: permID,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) removePermissionFromGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseUint32Path(w, r, "id")
	if !ok {
		return
	}
	permID, ok := parseUint32Path(w, r, "permission_id")
	if !ok {
		return
	}
	if _, err := p.cli.PermGroups.RemovePermission(r.Context(), &usersgrpcpb.RemovePermissionRequest{
		GroupId:      groupID,
		PermissionId: permID,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) listPermissions(w http.ResponseWriter, r *http.Request) {
	resp, err := p.cli.PermGroups.ListPermissions(r.Context(), &usersgrpcpb.EmptyRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(resp.GetItems()))
	for _, p := range resp.GetItems() {
		out = append(out, permissionToJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ---------- Helpers ----------

func permissionToJSON(p *usersgrpcpb.Permission) map[string]any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"id":          p.GetId(),
		"code":        p.GetCode(),
		"scope":       p.GetScope(),
		"name":        p.GetName(),
		"description": p.GetDescription(),
	}
}

func permissionGroupToJSON(g *usersgrpcpb.PermissionGroup) map[string]any {
	if g == nil {
		return nil
	}
	perms := make([]map[string]any, 0, len(g.GetPermissions()))
	for _, p := range g.GetPermissions() {
		perms = append(perms, permissionToJSON(p))
	}
	return map[string]any{
		"id":          g.GetId(),
		"code":        g.GetCode(),
		"name":        g.GetName(),
		"description": g.GetDescription(),
		"active":      g.GetActive(),
		"created_at":  optionalTimestamp(g.GetCreatedAt()),
		"updated_at":  optionalTimestamp(g.GetUpdatedAt()),
		"permissions": perms,
	}
}

// parseUint32Path lee un path param y devuelve el uint32. Si está vacío
// o no es numérico, escribe 400 y devuelve ok=false.
func parseUint32Path(w http.ResponseWriter, r *http.Request, name string) (uint32, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "MISSING_ID", Message: name + " is required"})
		return 0, false
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "INVALID_ID", Message: name + " must be a positive integer"})
		return 0, false
	}
	return uint32(n), true
}
