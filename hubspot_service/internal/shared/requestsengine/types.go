package requestsengine

import "net/http"

// Task es un request pendiente que el caller mete en la cola interna
// del engine. Lo construye Do(); el caller no instancia Task directo.
type Task struct {
	ID      string
	Method  string
	URL     string
	Body    []byte
	Headers map[string]string

	// respC — canal por donde el worker devuelve el Result al caller.
	// Privado: el caller bloquea en Do() hasta recibir.
	respC chan Result
}

// Result — la respuesta que un worker entrega de vuelta al caller.
// Si Err != nil, los demás campos pueden estar vacíos (error de red,
// ctx cancelado o se agotaron los reintentos).
type Result struct {
	ID         string
	StatusCode int
	Body       []byte
	Header     http.Header
	Err        error
}
