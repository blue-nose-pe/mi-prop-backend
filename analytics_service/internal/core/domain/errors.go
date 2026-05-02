package domain

import "analytics_service/internal/shared/apperr"

var (
	ErrAsesorNotFound      = apperr.NewNotFound("ASESOR_NOT_FOUND", "asesor not found")
	ErrColegioNotFound     = apperr.NewNotFound("COLEGIO_NOT_FOUND", "colegio not found")
	ErrEstudianteNotFound  = apperr.NewNotFound("ESTUDIANTE_NOT_FOUND", "estudiante not found")
	ErrInvalidExamType     = apperr.NewValidation("INVALID_EXAM_TYPE", "exam_type_code must be vocacional|simulacro|habitos", "exam_type_code")
	ErrUpstreamUnavailable = apperr.NewInternal("UPSTREAM_UNAVAILABLE", "upstream service is unavailable", nil)
)
