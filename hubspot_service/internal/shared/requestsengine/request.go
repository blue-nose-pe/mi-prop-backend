package requestsengine

import (
	"bytes"
	"context"
	"net/http"
)

// buildRequest arma el *http.Request con el token resuelto por el pool.
//
//   - Authorization: Bearer <token> (lo agrega siempre — sobrescribe
//     cualquier header Authorization que hubiera mandado el caller).
//   - Content-Type: application/json si hay body y el caller no lo seteó.
//   - Accept: application/json si el caller no lo seteó.
//
// Convenientemente, los Headers del caller se mantienen — si necesitás
// X-Hubspot-Custom o cualquier otro, pasalos en task.Headers.
func buildRequest(ctx context.Context, token string, t Task) (*http.Request, error) {
	var body *bytes.Reader
	if len(t.Body) > 0 {
		body = bytes.NewReader(t.Body)
	}

	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, t.Method, t.URL, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, t.Method, t.URL, nil)
	}
	if err != nil {
		return nil, err
	}

	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if len(t.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	return req, nil
}
