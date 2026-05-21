package domain

import "exams_service/internal/shared/apperr"

// Errores tipados — la frontera (gRPC handler) los traduce vía apperr.ToGRPC.
var (
	// Not found
	ErrExamTypeNotFound = apperr.NewNotFound("EXAM_TYPE_NOT_FOUND", "exam type not found")
	ErrExamNotFound     = apperr.NewNotFound("EXAM_NOT_FOUND", "exam not found")
	ErrQuestionNotFound = apperr.NewNotFound("QUESTION_NOT_FOUND", "question not found")
	ErrOptionNotFound   = apperr.NewNotFound("QUESTION_OPTION_NOT_FOUND", "question option not found")
	ErrAttemptNotFound  = apperr.NewNotFound("ATTEMPT_NOT_FOUND", "exam attempt not found")

	// Conflict
	ErrExamTypeCodeTaken          = apperr.NewConflict("EXAM_TYPE_CODE_TAKEN", "exam type code already exists", "code")
	ErrExamCodeTaken              = apperr.NewConflict("EXAM_CODE_TAKEN", "exam code already exists", "code")
	ErrExamQuestionAlreadyLinked  = apperr.NewConflict("EXAM_QUESTION_ALREADY_LINKED", "question is already linked to this exam", "question_id")
	ErrAnswerOptionMismatch       = apperr.NewValidation("OPTION_NOT_IN_QUESTION", "selected option does not belong to the question", "option_id")

	// Validation
	ErrInvalidDateRange = apperr.NewValidation("INVALID_DATE_RANGE", "start_at must be before end_at", "start_at")
	ErrEmptyExamName    = apperr.NewValidation("EXAM_NAME_REQUIRED", "exam name is required", "name")
	ErrNoCorrectOption  = apperr.NewValidation("NO_CORRECT_OPTION", "question must have at least one correct option", "options")
	ErrInvalidReorder   = apperr.NewValidation("INVALID_REORDER", "reorder list must contain exactly the same question_ids that the exam has, with no duplicates", "question_ids")

	// Business
	ErrExamClosed         = apperr.NewPermissionDenied("EXAM_CLOSED", "exam is not currently open for attempts")
	ErrExamNotPublished   = apperr.NewPermissionDenied("EXAM_NOT_PUBLISHED", "exam has not been published")
	// ErrKeyExamTypeMismatch: la key es de otro tipo de prueba que el exam.
	// Ejemplo: key vocacional (VO-XXXX) usada para iniciar un simulacro.
	ErrKeyExamTypeMismatch = apperr.NewValidation("KEY_EXAM_TYPE_MISMATCH", "key is for a different exam type", "key_code")
	ErrAttemptAlreadyDone = apperr.NewConflict("ATTEMPT_ALREADY_SUBMITTED", "attempt was already submitted", "attempt_id")
	ErrCannotEditPublished = apperr.NewPermissionDenied("EXAM_PUBLISHED_LOCKED", "published exams cannot be edited; clone for a new version instead")
)
