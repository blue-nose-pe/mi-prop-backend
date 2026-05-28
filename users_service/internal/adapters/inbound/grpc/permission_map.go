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
	// ListUserPermissions y HasPermission NO van en este map: los students
	// necesitan poder leer SUS PROPIOS permisos (el front los consulta
	// despues del login OTP para saber que UI mostrar). El check de
	// "tu propio user_id o tenes db_users.permission.read" se hace dentro
	// del handler (user_handler.go:ListUserPermissions/HasPermission).

	// schools
	"/users.v1.SchoolService/GetSchool":             "db_users.school.read",
	"/users.v1.SchoolService/ListSchools":           "db_users.school.read",
	"/users.v1.SchoolService/ListSchoolsByAsesor":   "db_users.school.read",
	"/users.v1.SchoolService/CreateSchool":          "db_users.school.write",
	"/users.v1.SchoolService/UpdateSchool":          "db_users.school.write",
	// AssignAsesor: re-asigna el asesor responsable de un colegio (SCD-2).
	// SIN este map era publica a cualquier authenticated user (permmw es
	// whitelist-by-default): un student podia robar colegios a otros
	// asesores. Bug encontrado en auditoria 2026-05-28. La misma permission
	// que CreateSchool/UpdateSchool — la mutacion es del mismo nivel.
	"/users.v1.SchoolService/AssignAsesor":          "db_users.school.write",

	// visitas (operacional, asesor)
	"/users.v1.VisitaService/CreateVisita": "db_users.school.write",
	"/users.v1.VisitaService/UpdateVisita": "db_users.school.write",
	"/users.v1.VisitaService/GetVisita":    "db_users.school.read",
	"/users.v1.VisitaService/ListVisitas":  "db_users.school.read",

	// permission_group service (CRUD a runtime)
	"/users.v1.PermissionGroupService/CreateGroup":      "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/UpdateGroup":      "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/DeleteGroup":      "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/AddPermission":    "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/RemovePermission": "db_users.permission_group.write",
	"/users.v1.PermissionGroupService/GetGroup":         "db_users.permission_group.read",
	"/users.v1.PermissionGroupService/ListGroups":       "db_users.permission_group.read",
	"/users.v1.PermissionGroupService/ListPermissions":  "db_users.permission.read",
	"/users.v1.PermissionGroupService/ListGroupUsers":   "db_users.users.read",
}
