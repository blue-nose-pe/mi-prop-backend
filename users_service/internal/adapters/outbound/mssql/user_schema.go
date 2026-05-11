package mssqladapter

// Schema de búsqueda de la tabla `users`. Whitelist absoluta:
// solo las propiedades declaradas aquí pueden ser filtradas, ordenadas
// o devueltas. password_hash NO aparece → imposible exponerlo.
//
// Para añadir búsqueda a otra tabla:
//   1) Declarar su schema al estilo de este archivo.
//   2) Embed *SearchEngine en el repo + NewSearchEngine(db, schemaX).
var userSearchSchema = SearchSchema{
	Table:        "users",
	IDColumn:     "id",
	CreatedCol:   "created_at",
	UpdatedCol:   "updated_at",
	ArchivedExpr: "active = 0", // archivado = desactivado
	DefaultLimit: 50,
	MaxLimit:     200,
	Columns: map[string]SearchColumn{
		"email":           {DBName: "email", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"first_name":      {DBName: "first_name", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"last_name":       {DBName: "last_name", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"document_number": {DBName: "document_number", Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
		"phone":           {DBName: "phone", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
		"school_id":       {DBName: "school_id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: true},
		"active":          {DBName: "active", Type: SearchTypeBool, Filterable: true, Sortable: false, Selectable: true},
		"last_access_at":  {DBName: "last_access_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"created_at":      {DBName: "created_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
		"updated_at":      {DBName: "updated_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
	},
}
