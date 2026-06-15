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
	ErrLeadNotFound       = apperr.NewNotFound("LEAD_NOT_FOUND", "lead not found")
	ErrVisitaNotFound     = apperr.NewNotFound("VISITA_NOT_FOUND", "visita not found")
	ErrPermissionNotFound = apperr.NewNotFound("PERMISSION_NOT_FOUND", "permission not found")
	ErrPermGroupNotFound  = apperr.NewNotFound("PERMISSION_GROUP_NOT_FOUND", "permission group not found")

	// ----- Conflict (choque de unicidad) -----
	ErrEmailTaken         = apperr.NewConflict("EMAIL_TAKEN", "email already registered", "email")
	ErrDocumentTaken      = apperr.NewConflict("DOCUMENT_TAKEN", "document number already registered", "document_number")
	ErrPermGroupCodeTaken = apperr.NewConflict("PERMISSION_GROUP_CODE_TAKEN", "permission group code already exists", "code")
	ErrPermGroupHasUsers  = apperr.NewConflict("PERMISSION_GROUP_HAS_USERS", "cannot delete: group has users assigned", "id")

	// ----- Validation -----
	ErrInvalidEmail        = apperr.NewValidation("INVALID_EMAIL", "email format is invalid", "email")
	ErrWeakPassword        = apperr.NewValidation("WEAK_PASSWORD", "password must have at least 8 characters", "password")
	ErrInvalidVisitaStatus = apperr.NewValidation("INVALID_VISITA_STATUS", "status must be one of: scheduled, completed, cancelled, no_show", "status")

	// ----- Auth -----
	ErrInvalidPassword     = apperr.NewUnauthenticated("INVALID_CREDENTIALS", "email or password is incorrect")
	ErrUserInactive        = apperr.NewPermissionDenied("USER_INACTIVE", "user account is disabled")
	ErrForbidden           = apperr.NewPermissionDenied("FORBIDDEN", "you do not have permission to perform this action")
	ErrInvalidRefreshToken = apperr.NewUnauthenticated("INVALID_REFRESH_TOKEN", "refresh token is invalid, expired or revoked")
	ErrAssignmentNotFound  = apperr.NewNotFound("ASSIGNMENT_NOT_FOUND", "no current assignment for the given target")

	// ----- OTP (login estudiante) -----
	ErrOTPInvalid       = apperr.NewUnauthenticated("OTP_INVALID", "otp is invalid, expired or already used")
	ErrOTPNotStudent    = apperr.NewPermissionDenied("OTP_NOT_STUDENT", "this user does not log in via OTP")
	ErrOTPDeliveryFail  = apperr.NewInternal("OTP_DELIVERY_FAIL", "could not deliver OTP", nil)

	// PasswordChangeRequired: el user tiene must_change_password=true.
	// Todas las rutas (excepto /api/users/me y /change-password) devuelven
	// este error hasta que cambie su password.
	ErrPasswordChangeRequired = apperr.NewPermissionDenied(
		"PASSWORD_CHANGE_REQUIRED",
		"you must change your temporary password before continuing",
	)
	// SuperadminRequired: el caller no es superadmin y la ruta lo exige.
	// Aplica a operaciones críticas tipo edición del catálogo permission.
	ErrSuperadminRequired = apperr.NewPermissionDenied(
		"SUPERADMIN_REQUIRED",
		"this action is restricted to superadmins",
	)
)
