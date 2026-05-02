package domain

import "time"

// Permission representa un permiso atomico del sistema (ej: users.create).
// code = scope.action (scope = nombre de microservicio/base de datos).
type Permission struct {
	ID          uint32
	Scope       string
	Code        string
	Name        string
	Description string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   *time.Time
}

// PermissionGroup agrupa permisos bajo un codigo reutilizable
// (ej: student_permissions).
type PermissionGroup struct {
	ID          uint32
	Code        string
	Name        string
	Description string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   *time.Time

	// Permissions se carga a demanda; puede estar vacio.
	Permissions []Permission
}
