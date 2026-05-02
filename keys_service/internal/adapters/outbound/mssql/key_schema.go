package mssqladapter

// Whitelist de búsqueda para [key].
var keySearchSchema = SearchSchema{
	Table:        "[key]",
	IDColumn:     "id",
	CreatedCol:   "created_at",
	UpdatedCol:   "updated_at",
	ArchivedExpr: "active = 0",
	DefaultLimit: 50,
	MaxLimit:     200,
	Columns: map[string]SearchColumn{
		"code":           {DBName: "code", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"exam_type_id":   {DBName: "exam_type_id", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"school_id":      {DBName: "school_id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: true},
		"asesor_user_id": {DBName: "asesor_user_id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: true},
		"mode":           {DBName: "mode", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
		"grade":          {DBName: "grade", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"section":        {DBName: "section", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"valid_from":     {DBName: "valid_from", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"valid_to":       {DBName: "valid_to", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"max_uses":       {DBName: "max_uses", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"current_uses":   {DBName: "current_uses", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"active":         {DBName: "active", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"created_at":     {DBName: "created_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"updated_at":     {DBName: "updated_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
	},
}
