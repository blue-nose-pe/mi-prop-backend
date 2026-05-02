package search

import (
	"fmt"

	"satisfaction_service/internal/shared/apperr"
)

// Factories para errores de validacion del motor de busqueda.
// Cada uno arma un *apperr.Error con el contexto real (nombre de la
// propiedad, operador, etc.) en Message y Field.
//
// El handler gRPC no las distingue individualmente: apperr.ToGRPC las
// serializa al envelope estandar segun el Kind.

func UnknownProperty(name string) *apperr.Error {
	return apperr.NewValidation("UNKNOWN_PROPERTY",
		fmt.Sprintf("unknown property: %s", name), name)
}

func PropertyNotFilterable(name string) *apperr.Error {
	return apperr.NewValidation("PROPERTY_NOT_FILTERABLE",
		fmt.Sprintf("property not filterable: %s", name), name)
}

func PropertyNotSortable(name string) *apperr.Error {
	return apperr.NewValidation("PROPERTY_NOT_SORTABLE",
		fmt.Sprintf("property not sortable: %s", name), name)
}

func InvalidOperator(op string) *apperr.Error {
	return apperr.NewValidation("INVALID_OPERATOR",
		fmt.Sprintf("invalid filter operator: %s", op), "")
}

func InvalidValues(op string, needCount int) *apperr.Error {
	return apperr.NewValidation("INVALID_VALUES",
		fmt.Sprintf("operator %s needs %d value(s)", op, needCount), "")
}
