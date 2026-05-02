package mssqladapter

import (
	"fmt"
	"strings"

	"exams_service/internal/shared/search"
)

// Constructor genérico filtros → SQL parametrizado para Azure SQL.
// Reutilizable para cualquier tabla del microservicio (users, school, ...)
// y, copiándolo a otros servicios, para cualquier microservicio.
//
// Seguridad:
//   - Solo se aceptan propiedades declaradas en el Schema (whitelist).
//   - Todos los valores del usuario van por parámetros (@p1, @p2...), nunca
//     se concatenan al SQL → no hay riesgo de injection.
//   - Tipos se castean según el Schema (UNIQUEIDENTIFIER, BIGINT, BIT, DATETIME2).

// ---------- Schema ----------

type SearchSchema struct {
	Table        string
	IDColumn     string // columna PK (ej: "id")
	CreatedCol   string // columna createdAt
	UpdatedCol   string // columna updatedAt (puede ser NULL)
	ArchivedExpr string // expresión SQL bool para "archived" (ej: "active = 0"); "" => 0

	Columns      map[string]SearchColumn // propertyName -> definicion
	DefaultLimit int                     // si el request no trae limit (default 50)
	MaxLimit     int                     // cap (default 200)
	DefaultSort  []search.Sort           // si el request no trae sorts
}

type SearchColumn struct {
	DBName     string // columna real en la tabla (ej: "first_name")
	Type       SearchColType
	Filterable bool
	Sortable   bool
	Selectable bool
}

type SearchColType string

const (
	SearchTypeString    SearchColType = "string"
	SearchTypeInt       SearchColType = "int"
	SearchTypeUUID      SearchColType = "uuid"
	SearchTypeBool      SearchColType = "bool"
	SearchTypeTimestamp SearchColType = "timestamp"
)

// ---------- Build ----------

type BuiltQuery struct {
	SQL           string
	Args          []any
	SelectedProps []string // propiedades realmente incluidas en el SELECT
}

func BuildSearch(s SearchSchema, req search.Request) (*BuiltQuery, error) {
	if s.MaxLimit == 0 {
		s.MaxLimit = 200
	}
	if s.DefaultLimit == 0 {
		s.DefaultLimit = 50
	}

	limit := req.Limit
	if limit <= 0 {
		limit = s.DefaultLimit
	}
	if limit > s.MaxLimit {
		limit = s.MaxLimit
	}

	// ----- Propiedades a devolver -----
	wanted := req.Properties
	if len(wanted) == 0 {
		wanted = make([]string, 0, len(s.Columns))
		for name, c := range s.Columns {
			if c.Selectable {
				wanted = append(wanted, name)
			}
		}
	}
	validProps := make([]string, 0, len(wanted))
	for _, p := range wanted {
		if c, ok := s.Columns[p]; ok && c.Selectable {
			validProps = append(validProps, p)
		}
		// propiedades desconocidas se ignoran silenciosamente (HubSpot-style).
	}

	// ----- SELECT clause -----
	cols := []string{
		fmt.Sprintf("CONVERT(NVARCHAR(36), %s) AS __id", s.IDColumn),
		fmt.Sprintf("%s AS __created_at", s.CreatedCol),
		fmt.Sprintf("%s AS __updated_at", s.UpdatedCol),
	}
	if s.ArchivedExpr != "" {
		// CAST a BIT explícito para que el driver no devuelva int.
		cols = append(cols, fmt.Sprintf("CAST(IIF(%s, 1, 0) AS BIT) AS __archived", s.ArchivedExpr))
	} else {
		cols = append(cols, "CAST(0 AS BIT) AS __archived")
	}
	// total (antes del paging) via window function — soportado por SQL Server 2012+.
	cols = append(cols, "COUNT(*) OVER() AS __total")

	for _, p := range validProps {
		c := s.Columns[p]
		cols = append(cols, fmt.Sprintf(`%s AS %s`, selectExpr(c), quoteIdent(p)))
	}

	// ----- WHERE -----
	args := []any{}
	where, err := buildWhere(s, req.FilterGroups, &args)
	if err != nil {
		return nil, err
	}

	// ----- ORDER BY -----
	sorts := req.Sorts
	if len(sorts) == 0 {
		sorts = s.DefaultSort
	}
	orderBy := fmt.Sprintf("%s DESC, %s DESC", s.CreatedCol, s.IDColumn) // fallback estable
	if len(sorts) > 0 {
		parts := make([]string, 0, len(sorts))
		for _, st := range sorts {
			col, ok := s.Columns[st.Property]
			if !ok {
				return nil, search.UnknownProperty(st.Property)
			}
			if !col.Sortable {
				return nil, search.PropertyNotSortable(st.Property)
			}
			dir := "ASC"
			if st.Direction == search.Desc {
				dir = "DESC"
			}
			parts = append(parts, fmt.Sprintf("%s %s", col.DBName, dir))
		}
		orderBy = strings.Join(parts, ", ")
	}

	// ----- OFFSET / FETCH (T-SQL paging) -----
	args = append(args, req.After, limit)
	offsetPlaceholder := len(args) - 1
	limitPlaceholder := len(args)

	// ----- Compose -----
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(" FROM ")
	b.WriteString(s.Table)
	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}
	b.WriteString(" ORDER BY ")
	b.WriteString(orderBy)
	fmt.Fprintf(&b, " OFFSET @p%d ROWS FETCH NEXT @p%d ROWS ONLY", offsetPlaceholder, limitPlaceholder)

	return &BuiltQuery{SQL: b.String(), Args: args, SelectedProps: validProps}, nil
}

// selectExpr castea UUIDs a string para que el driver los devuelva como
// string en el map dinámico.
func selectExpr(c SearchColumn) string {
	if c.Type == SearchTypeUUID {
		return "CONVERT(NVARCHAR(36), " + c.DBName + ")"
	}
	return c.DBName
}

// quoteIdent encierra el alias en corchetes T-SQL para soportar nombres
// con caracteres especiales / palabras reservadas.
func quoteIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// ---------- WHERE builder ----------

func buildWhere(s SearchSchema, groups []search.FilterGroup, args *[]any) (string, error) {
	if len(groups) == 0 {
		return "", nil
	}
	orParts := make([]string, 0, len(groups))
	for _, g := range groups {
		if len(g.Filters) == 0 {
			continue
		}
		andParts := make([]string, 0, len(g.Filters))
		for _, f := range g.Filters {
			col, ok := s.Columns[f.Property]
			if !ok {
				return "", search.UnknownProperty(f.Property)
			}
			if !col.Filterable {
				return "", search.PropertyNotFilterable(f.Property)
			}
			clause, err := buildFilter(col, f, args)
			if err != nil {
				return "", err
			}
			andParts = append(andParts, clause)
		}
		if len(andParts) > 0 {
			orParts = append(orParts, "("+strings.Join(andParts, " AND ")+")")
		}
	}
	return strings.Join(orParts, " OR "), nil
}

func buildFilter(col SearchColumn, f search.Filter, args *[]any) (string, error) {
	need := func(n int) error {
		if len(f.Values) < n {
			return search.InvalidValues(string(f.Operator), n)
		}
		return nil
	}
	// ph appende un argumento y devuelve el placeholder con cast adecuado.
	ph := func(v string) string {
		*args = append(*args, v)
		return wrapCast(col.Type, fmt.Sprintf("@p%d", len(*args)))
	}

	switch f.Operator {
	case search.OpEQ:
		if err := need(1); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s = %s", col.DBName, ph(f.Values[0])), nil
	case search.OpNEQ:
		if err := need(1); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s <> %s", col.DBName, ph(f.Values[0])), nil
	case search.OpLT:
		if err := need(1); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s < %s", col.DBName, ph(f.Values[0])), nil
	case search.OpLTE:
		if err := need(1); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s <= %s", col.DBName, ph(f.Values[0])), nil
	case search.OpGT:
		if err := need(1); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s > %s", col.DBName, ph(f.Values[0])), nil
	case search.OpGTE:
		if err := need(1); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s >= %s", col.DBName, ph(f.Values[0])), nil
	case search.OpIN:
		if err := need(1); err != nil {
			return "", err
		}
		parts := make([]string, len(f.Values))
		for i, v := range f.Values {
			parts[i] = ph(v)
		}
		return fmt.Sprintf("%s IN (%s)", col.DBName, strings.Join(parts, ", ")), nil
	case search.OpNotIN:
		if err := need(1); err != nil {
			return "", err
		}
		parts := make([]string, len(f.Values))
		for i, v := range f.Values {
			parts[i] = ph(v)
		}
		return fmt.Sprintf("%s NOT IN (%s)", col.DBName, strings.Join(parts, ", ")), nil
	case search.OpContains:
		// LIKE en SQL Server respeta la collation de la columna; las DBs
		// del proyecto se crean con SQL_Latin1_General_CP1_CI_AS (CI = case-insensitive).
		if err := need(1); err != nil {
			return "", err
		}
		*args = append(*args, "%"+f.Values[0]+"%")
		return fmt.Sprintf("%s LIKE @p%d", col.DBName, len(*args)), nil
	case search.OpStartsWith:
		if err := need(1); err != nil {
			return "", err
		}
		*args = append(*args, f.Values[0]+"%")
		return fmt.Sprintf("%s LIKE @p%d", col.DBName, len(*args)), nil
	case search.OpHasProperty:
		return fmt.Sprintf("%s IS NOT NULL", col.DBName), nil
	case search.OpNotHasProperty:
		return fmt.Sprintf("%s IS NULL", col.DBName), nil
	case search.OpBetween:
		if err := need(2); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s BETWEEN %s AND %s", col.DBName, ph(f.Values[0]), ph(f.Values[1])), nil
	default:
		return "", search.InvalidOperator(string(f.Operator))
	}
}

// wrapCast envuelve un placeholder con el CAST adecuado al tipo de la columna.
// Los filtros siempre llegan como strings en el proto, por eso se castean en SQL.
func wrapCast(t SearchColType, placeholder string) string {
	switch t {
	case SearchTypeInt:
		return "CONVERT(BIGINT, " + placeholder + ")"
	case SearchTypeUUID:
		return "CONVERT(UNIQUEIDENTIFIER, " + placeholder + ")"
	case SearchTypeBool:
		return "CONVERT(BIT, " + placeholder + ")"
	case SearchTypeTimestamp:
		return "CONVERT(DATETIME2(7), " + placeholder + ")"
	default:
		return placeholder
	}
}
