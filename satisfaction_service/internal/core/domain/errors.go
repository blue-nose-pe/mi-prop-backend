package domain

import "satisfaction_service/internal/shared/apperr"

var (
	ErrSurveyNotFound   = apperr.NewNotFound("SURVEY_NOT_FOUND", "survey not found")
	ErrQuestionNotFound = apperr.NewNotFound("QUESTION_NOT_FOUND", "survey question not found")
	ErrResponseNotFound = apperr.NewNotFound("RESPONSE_NOT_FOUND", "response not found")

	ErrSurveyCodeTaken             = apperr.NewConflict("SURVEY_CODE_TAKEN", "survey code/version already exists", "code")
	ErrAlreadySubmittedThisAttempt = apperr.NewConflict("RESPONSE_ALREADY_SUBMITTED_FOR_ATTEMPT", "this user already submitted a response for this exam_attempt", "exam_attempt_id")
	ErrAlreadySubmittedThisSurvey  = apperr.NewConflict("RESPONSE_ALREADY_SUBMITTED_FOR_SURVEY", "this user already submitted a response for this survey", "survey_id")
	ErrMissingRequiredAnswer       = apperr.NewValidation("MISSING_REQUIRED_ANSWER", "missing answer for a required question", "answers")
	ErrQuestionNotInSurvey         = apperr.NewValidation("QUESTION_NOT_IN_SURVEY", "an answer references a question that does not belong to this survey", "answers")
	ErrInvalidAnswerValue          = apperr.NewValidation("INVALID_ANSWER_VALUE", "answer value out of range (scale 1-5, nps 0-10) or empty for a required question", "answers")
	ErrSurveyNoTarget              = apperr.NewValidation("SURVEY_NO_TARGET", "cannot publish a survey with no exam-type trigger and no key assigned; it would never be shown to any student", "trigger_kind")

	ErrInvalidQuestionKind = apperr.NewValidation("INVALID_QUESTION_KIND", "kind must be one of scale|nps|single|multi|open", "kind")
	ErrInvalidTarget       = apperr.NewValidation("INVALID_TARGET_ROLE", "target_role must be one of admin|asesor|coordinador|estudiante", "target_role")
	ErrInvalidTrigger      = apperr.NewValidation("INVALID_TRIGGER", "trigger must be one of post_test|on_demand|recurring", "trigger")
	ErrSurveyNotPublished  = apperr.NewPermissionDenied("SURVEY_NOT_PUBLISHED", "cannot answer an unpublished survey")
	ErrCannotEditPublished = apperr.NewPermissionDenied("SURVEY_LOCKED", "published surveys cannot be edited; clone for new version")
)
