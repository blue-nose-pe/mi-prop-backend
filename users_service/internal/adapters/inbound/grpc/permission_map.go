package grpchandler

// PermissionMap define qué permission code requiere cada RPC del servicio.
// Si un método no está acá, el interceptor permmw lo deja pasar (delega
// la decisión a la lógica de negocio del handler — útil para Login,
// Refresh, Me, ChangeMyPassword, etc.).
//
// Convención: `<service>.<table>.<action>` — ver migración 014 para el
// catálogo completo.
//
// Notas:
//   - AuthService.* y health/reflection NO están acá: jwtmw skip-list los
//     deja pasar antes de llegar a permmw.
//   - UserService.Me y ChangeMyPassword NO están acá: solo requieren que
//     el caller esté autenticado (no permisos extra). El interceptor
//     permmw deja pasar lo no mapeado.
//   - ResetPassword NO está acá: el handler chequea internamente
//     `roles[]` contains "superadmin" (defense-in-depth — el rol va en
//     el JWT y NO se cachea).
var PermissionMap = map[string]string{
	// users
	"/users.v1.UserService/CreateUser":             "db_users.users.write",
	"/users.v1.UserService/UpdateUser":             "db_users.users.write",
	"/users.v1.UserService/DeactivateUser":         "db_users.users.write",
	"/users.v1.UserService/ReactivateUser":         "db_users.users.write",
	"/users.v1.UserService/GetUser":                "db_users.users.read",
	"/users.v1.UserService/GetUserByEmail":         "db_users.users.read",
	"/users.v1.UserService/SearchUsers":            "db_users.users.read",

	// permissions / groups
	"/users.v1.UserService/AssignPermissionGroup":  "db_users.permission_group.write",
	"/users.v1.UserService/RevokePermissionGroup":  "db_users.permission_group.write",
	"/users.v1.UserService/ListUserPermissions":    "db_users.permission.read",
	"/users.v1.UserService/HasPermission":          "db_users.permission.read",

	// schools
	"/users.v1.SchoolService/GetSchool":             "db_users.school.read",
	"/users.v1.SchoolService/ListSchools":           "db_users.school.read",

	// permission_group service (CRUD a runtime)
	"/users.v1.PermissionGroupService/CreateGroup":      "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/UpdateGroup":      "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/DeleteGroup":      "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/AddPermission":    "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/RemovePermission": "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/GetGroup":         "db_users.permission_group.read",
	"/users.v1.PermissionGroupService/ListGroups":       "db_users.permission_group.read",
	"/users.v1.PermissionGroupService/ListPermissions":  "db_users.permission.read",
}
