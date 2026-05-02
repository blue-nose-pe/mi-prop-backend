package domain

import "keys_service/internal/shared/apperr"

var (
	ErrKeyNotFound  = apperr.NewNotFound("KEY_NOT_FOUND", "key not found")
	ErrKeyCodeTaken = apperr.NewConflict("KEY_CODE_TAKEN", "key code already exists", "code")

	ErrInvalidMode      = apperr.NewValidation("INVALID_KEY_MODE", "mode must be 'school' or 'lan'", "mode")
	ErrSchoolRequired   = apperr.NewValidation("SCHOOL_ID_REQUIRED", "school_id is required when mode='school'", "school_id")
	ErrInvalidDateRange = apperr.NewValidation("INVALID_DATE_RANGE", "valid_from must be before valid_to", "valid_from")

	// Permission denied = el código existe pero no es usable ahora.
	ErrKeyNotUsable = apperr.NewPermissionDenied("KEY_NOT_USABLE", "key is not usable")
)
