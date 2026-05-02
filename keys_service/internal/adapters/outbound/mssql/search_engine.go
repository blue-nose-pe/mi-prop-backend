package mssqladapter

import (
	"context"
	"database/sql"
	"time"

	"keys_service/internal/shared/search"
)

// SearchEngine ejecuta búsquedas genéricas sobre cualquier tabla,
// parametrizado por un SearchSchema. Mismo patrón que el adapter Postgres
// pero sobre database/sql + go-mssqldb. Embebido por los repos concretos
// para ganar Search() por promoción de métodos.
type SearchEngine struct {
	db     *sql.DB
	schema SearchSchema
}

func NewSearchEngine(db *sql.DB, schema SearchSchema) *SearchEngine {
	return &SearchEngine{db: db, schema: schema}
}

// Search satisface ports.UserSearcher (y cualquier otra *Searcher con
// la misma firma).
func (e *SearchEngine) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	q, err := BuildSearch(e.schema, req)
	if err != nil {
		return nil, err
	}

	rows, err := e.db.QueryContext(ctx, q.SQL, q.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	resp := &search.Response{Results: make([]search.Result, 0, 16)}

	for rows.Next() {
		// scan dinámico: una variable interface{} por columna.
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		rowMap := make(map[string]any, len(cols))
		for i, name := range cols {
			rowMap[name] = vals[i]
		}

		if v, ok := rowMap["__total"].(int64); ok {
			resp.Total = uint32(v)
		}

		id, _ := rowMap["__id"].(string)
		archived, _ := asBool(rowMap["__archived"])

		var createdAt time.Time
		if v, ok := rowMap["__created_at"].(time.Time); ok {
			createdAt = v
		}
		var updatedAt time.Time
		if v, ok := rowMap["__updated_at"].(time.Time); ok {
			updatedAt = v
		}

		// Las claves __* son metadata; se sacan del bag de properties
		// para que el cliente solo vea las propiedades reales.
		delete(rowMap, "__id")
		delete(rowMap, "__created_at")
		delete(rowMap, "__updated_at")
		delete(rowMap, "__archived")
		delete(rowMap, "__total")

		resp.Results = append(resp.Results, search.Result{
			ID:         id,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
			Archived:   archived,
			Properties: rowMap,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	resp.Paging = &search.Paging{
		HasMore:   uint32(req.After)+uint32(len(resp.Results)) < resp.Total,
		NextAfter: uint32(req.After) + uint32(len(resp.Results)),
	}
	return resp, nil
}

// asBool acepta bool, int64 (T-SQL BIT llega como int64) y *bool.
func asBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int64:
		return x != 0, true
	case nil:
		return false, true
	default:
		return false, false
	}
}
