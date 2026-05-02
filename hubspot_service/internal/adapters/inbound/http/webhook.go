// Package httpinbound recibe webhooks ENTRANTES de HubSpot.
// Política bidireccional conservadora (P2): solo reflejamos cambios en
// firstname, lastname, phone, lifecyclestage. Cambios sensibles (email,
// dni) requieren intervención manual — el webhook los loguea pero no
// los aplica.
package httpinbound

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// NewMux devuelve un http.Handler con las rutas:
//   POST /webhooks/hubspot/contact
//   GET  /health
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /webhooks/hubspot/contact", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Acuse de recibo inmediato (HubSpot espera 200/202 rápido).
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"received":true}`))

		// Parsing best-effort para logging estructurado.
		var payload any
		_ = json.Unmarshal(body, &payload)
		log.Printf("[hubspot-webhook] payload=%v", payload)

		// TODO: cuando UCSP active el webhook real, parsear los eventos y
		//       propagar via gRPC al users_service los campos permitidos.
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	return mux
}
