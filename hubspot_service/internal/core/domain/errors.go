package domain

import "hubspot_service/internal/shared/apperr"

var (
	ErrContactNotFound  = apperr.NewNotFound("CONTACT_NOT_FOUND", "contact not found in HubSpot")
	ErrInvalidExamType  = apperr.NewValidation("INVALID_EXAM_TYPE", "exam_type_code must be vocacional|simulacro|habitos", "exam_type_code")
	ErrInvalidPayload   = apperr.NewValidation("INVALID_PAYLOAD", "payload is invalid", "")
	ErrHubspotUpstream  = apperr.NewInternal("HUBSPOT_UPSTREAM", "HubSpot upstream returned an error", nil)
	ErrOTPDeliveryFail  = apperr.NewInternal("OTP_DELIVERY_FAIL", "OTP webhook trigger did not return 202", nil)
)
