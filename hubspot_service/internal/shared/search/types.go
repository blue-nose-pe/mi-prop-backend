// Package search: contrato de busqueda (estilo HubSpot) + tipos Go puros.
// Esta pensado para ser extraido luego a un modulo compartido entre
// microservicios. NO depende de SQL ni de gRPC; solo del proto common.
package search

import "time"

// ---------- Request ----------

type Request struct {
	FilterGroups []FilterGroup
	Properties   []string
	Limit        int
	After        int
	Sorts        []Sort
}

type FilterGroup struct {
	Filters []Filter
}

type Filter struct {
	Property string
	Operator Operator
	Values   []string
}

type Operator string

const (
	OpEQ             Operator = "EQ"
	OpNEQ            Operator = "NEQ"
	OpLT             Operator = "LT"
	OpLTE            Operator = "LTE"
	OpGT             Operator = "GT"
	OpGTE            Operator = "GTE"
	OpIN             Operator = "IN"
	OpNotIN          Operator = "NOT_IN"
	OpContains       Operator = "CONTAINS"
	OpStartsWith     Operator = "STARTS_WITH"
	OpHasProperty    Operator = "HAS_PROPERTY"
	OpNotHasProperty Operator = "NOT_HAS_PROPERTY"
	OpBetween        Operator = "BETWEEN"
)

type Sort struct {
	Property  string
	Direction Direction
}

type Direction string

const (
	Asc  Direction = "ASC"
	Desc Direction = "DESC"
)

// ---------- Response ----------

type Response struct {
	Total   uint32
	Results []Result
	Paging  *Paging
}

type Result struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Archived   bool
	Properties map[string]any
}

type Paging struct {
	NextAfter uint32
	HasMore   bool
}
