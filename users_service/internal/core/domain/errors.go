package domain

import "users_service/internal/shared/apperr"

// Errores de dominio. Cada uno trae ya su Kind + Code + Message + Field.
// La frontera (adapter gRPC) los traduce automaticamente via apperr.ToGRPC.
//
// Agregar un error nuevo = una linea aqui. Nada mas.
var (
	// ----- Not found -----
	ErrUserNotFound       = apperr.NewNotFound("USER_NOT_FOUND", "user not found")
	ErrSchoolNotFound     = apperr.NewNotFound("SCHOOL_NOT_FOUND", "school not found")
	ErrPermissionNotFound = apperr.NewNotFound("PERMISSION_NOT_FOUND", "permission not found")
	ErrPermGroupNotFound  = apperr.NewNotFound("PERMISSION_GROUP_NOT_FOUND", "permission group not found")

	// ----- Conflict (choque de unicidad) -----
	ErrEmailTaken    = apperr.NewConflict("EMAIL_TAKEN", "email already registered", "email")
	ErrDocumentTaken = apperr.NewConflict("DOCUMENT_TAKEN", "document number already registered", "document_number")

	// ----- Validation -----
	ErrInvalidEmail = apperr.NewValidation("INVALID_EMAIL", "email format is invalid", "email")
	ErrWeakPassword = apperr.NewValidation("WEAK_PASSWORD", "password must have at least 8 characters", "password")

	// ----- Auth -----
	ErrInvalidPassword     = apperr.NewUnauthenticated("INVALID_CREDENTIALS", "email or password is incorrect")
	ErrUserInactive        = apperr.NewPermissionDenied("USER_INACTIVE", "user account is disabled")
	ErrForbidden           = apperr.NewPermissionDenied("FORBIDDEN", "you do not have permission to perform this action")
	ErrInvalidRefreshToken = apperr.NewUnauthenticated("INVALID_REFRESH_TOKEN", "refresh token is invalid, expired or revoked")
	ErrAssignmentNotFound  = apperr.NewNotFound("ASSIGNMENT_NOT_FOUND", "no current assignment for the given target")
)
