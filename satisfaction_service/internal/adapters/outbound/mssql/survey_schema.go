package mssqladapter

var surveySearchSchema = SearchSchema{
	Table:        "survey",
	IDColumn:     "id",
	CreatedCol:   "created_at",
	UpdatedCol:   "updated_at",
	ArchivedExpr: "active = 0",
	DefaultLimit: 50,
	MaxLimit:     200,
	Columns: map[string]SearchColumn{
		"code":         {DBName: "code", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"title":        {DBName: "title", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"target_role":  {DBName: "target_role", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
		"trigger_kind": {DBName: "trigger_kind", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
		// Cliente (2026-06-04): permite buscar la encuesta dirigida a una key.
		"key_id":       {DBName: "key_id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: true},
		"version":      {DBName: "version", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"published":    {DBName: "published", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"active":       {DBName: "active", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"created_at":   {DBName: "created_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
	},
}
