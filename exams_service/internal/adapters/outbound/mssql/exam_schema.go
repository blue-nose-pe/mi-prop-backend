package mssqladapter

// Whitelist de búsqueda para exam.
var examSearchSchema = SearchSchema{
	Table:        "exam",
	IDColumn:     "id",
	CreatedCol:   "created_at",
	UpdatedCol:   "updated_at",
	ArchivedExpr: "active = 0",
	DefaultLimit: 50,
	MaxLimit:     200,
	Columns: map[string]SearchColumn{
		"exam_type_id":     {DBName: "exam_type_id", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"school_id":        {DBName: "school_id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: true},
		"parent_exam_id":   {DBName: "parent_exam_id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: true},
		"version":          {DBName: "version", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"code":             {DBName: "code", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"name":             {DBName: "name", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"start_at":         {DBName: "start_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"end_at":           {DBName: "end_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"max_participants": {DBName: "max_participants", Type: SearchTypeInt, Filterable: true, Sortable: true, Selectable: true},
		"published":        {DBName: "published", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"active":           {DBName: "active", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"created_at":       {DBName: "created_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"updated_at":       {DBName: "updated_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
	},
}

// Whitelist de búsqueda para question.
var questionSearchSchema = SearchSchema{
	Table:        "question",
	IDColumn:     "id",
	CreatedCol:   "created_at",
	UpdatedCol:   "updated_at",
	ArchivedExpr: "active = 0",
	DefaultLimit: 50,
	MaxLimit:     200,
	Columns: map[string]SearchColumn{
		"text":       {DBName: "text", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
		"category":   {DBName: "category", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"kind":       {DBName: "kind", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
		"active":     {DBName: "active", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"created_at": {DBName: "created_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"updated_at": {DBName: "updated_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
	},
}
