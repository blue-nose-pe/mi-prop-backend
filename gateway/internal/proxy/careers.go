// Catalogo estatico de carreras de pregrado UCSP.
//
// Este endpoint es publico (esta en la JWT skip-list) porque lo consume
// la pantalla de auto-registro publico del estudiante, que aun no tiene
// JWT. Lo expusimos como /api/careers para que el front se hidrate desde
// aqui y ops pueda actualizar el listado modificando este archivo sin
// tocar el front (un redeploy del gateway alcanza).
//
// El front trae un hardcoded como fallback para offline / 5xx.
package proxy

import "net/http"

// carrera matchea la shape que consume el front: {label, value}. value
// es lo que se guarda en sesion local (no se persiste en back), label
// es lo visible en el dropdown.
type carrera struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// careersCatalog es la fuente de verdad del listado. Cuando UCSP agregue
// o quite carreras, modificar aqui — no en el front.
var careersCatalog = []carrera{
	{"Administración de Negocios", "Administración de Negocios"},
	{"Arquitectura", "Arquitectura"},
	{"Ciencia Política y Gestión Pública", "Ciencia Política y Gestión Pública"},
	{"Ciencias de la Comunicación", "Ciencias de la Comunicación"},
	{"Ciencias de la Computación", "Ciencias de la Computación"},
	{"Contabilidad", "Contabilidad"},
	{"Derecho", "Derecho"},
	{"Economía", "Economía"},
	{"Educación", "Educación"},
	{"Ingeniería Ambiental", "Ingeniería Ambiental"},
	{"Ingeniería Civil", "Ingeniería Civil"},
	{"Ingeniería de Software", "Ingeniería de Software"},
	{"Ingeniería Electrónica y de Telecomunicaciones", "Ingeniería Electrónica y de Telecomunicaciones"},
	{"Ingeniería Industrial", "Ingeniería Industrial"},
	{"Ingeniería Mecatrónica", "Ingeniería Mecatrónica"},
	{"Marketing y Negocios Internacionales", "Marketing y Negocios Internacionales"},
	{"Medicina Humana", "Medicina Humana"},
	{"Psicología", "Psicología"},
	{"Otra", "Otra"},
}

// RegisterCareers — GET /api/careers (publico, sin auth).
func (p *Proxy) RegisterCareers(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/careers", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"items": careersCatalog,
			"total": len(careersCatalog),
		})
	})
}
