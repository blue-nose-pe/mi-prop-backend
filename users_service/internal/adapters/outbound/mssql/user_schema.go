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
		// `id` se expone como propiedad filtrable (no seleccionable: el id
		// siempre viaja en el sobre como __id, no como columna). Sirve para
		// hidrataciones por lote tipo `id IN (uuid1, uuid2, ...)` desde
		// otros servicios (p. ej. exams_service devuelve attempts con
		// user_id y el front pide los nombres en una sola request).
		"id":              {DBName: "id", Type: SearchTypeUUID, Filterable: true, Sortable: false, Selectable: false},
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
