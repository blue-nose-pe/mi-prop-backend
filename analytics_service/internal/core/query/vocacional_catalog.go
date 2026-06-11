// Catálogo de scoring del Test de Intereses Vocacionales (TIV), replicado
// fiel del modelo de producción (prod_tiv: type_tests, calculo_test, levels,
// profiles, carreras). El examen tiene 4 módulos; cada pregunta declara su
// category como "M{módulo}:{dimensión}" (ej "M1:EMOTIVIDAD", "M3:SS") y sus
// opciones SCALE usan sort_order como peso (escala distinta por módulo).
//
//	M1 CONOCIÉNDOME            -> Personalidad (Le Senne, 8 caracteres)
//	M2 MI APOYO FAMILIAR       -> Apoyo Familiar (3 bandas)
//	M3 NO ME GUSTA/ME ENCANTA  -> Áreas de Interés (10 áreas + carreras)
//	M4 MI PROYECTO DE VIDA     -> Proyecto de Vida (5 sub-dimensiones)
package query

// vocMaxWeight: peso máximo de una opción según el módulo (define el max
// por pregunta para los porcentajes). M1/M3 = 0..2, M2 = 0..1, M4 = 0..5.
var vocMaxWeight = map[string]int32{"M1": 2, "M2": 1, "M3": 2, "M4": 5}

// vocDimLabel: código de dimensión -> etiqueta legible.
var vocDimLabel = map[string]string{
	"EMOTIVIDAD": "Emotividad", "ACTIVIDAD": "Actividad", "RESONANCIA": "Resonancia",
	"FAMILIAR":         "Comunicación y dinámica familiar",
	"SS":               "Sensibilidad Social",
	"EP":               "Gestión y Comunicación",
	"V":                "Verbal",
	"AP":               "Artes",
	"MS":               "Musical",
	"OG":               "Organización",
	"CT":               "Investigación",
	"CL":               "Cálculo",
	"MC":               "Trabajo Manual",
	"AL":               "Naturaleza",
	"AUTOCONOCIMIENTO": "Autoconocimiento",
	"VOCACION":         "Vocación",
	"PRESUPUESTO":      "Presupuesto",
	"INFORMACION":      "Información",
	"PREPARACION":      "Preparación",
}

// Orden estable de dimensiones por módulo (para barras y ranking).
var vocPersonalidadDims = []string{"EMOTIVIDAD", "ACTIVIDAD", "RESONANCIA"}
var vocInteresDims = []string{"SS", "EP", "V", "AP", "MS", "OG", "CT", "CL", "MC", "AL"}
var vocProyectoDims = []string{"AUTOCONOCIMIENTO", "VOCACION", "PRESUPUESTO", "INFORMACION", "PREPARACION"}

// interesInfo: características + carreras sugeridas por área de interés.
// Carreras tomadas del catálogo real de UCSP (prod_tiv.carreras) por afinidad
// con el ambiente Holland de cada área.
type interesInfo struct {
	characteristics string
	careers         []string
}

var vocInteres = map[string]interesInfo{
	"SS": {"Gusto por ayudar, acompañar y servir a otras personas; orientación al bienestar de los demás.",
		[]string{"Psicología", "Educación", "Trabajo Social", "Enfermería", "Ciencias Médicas"}},
	"EP": {"Gusto por liderar, gestionar, persuadir y comunicar; orientación a metas, negocios y personas.",
		[]string{"Administración", "Marketing", "Negocios Internacionales", "Relaciones Públicas", "Comunicación"}},
	"V": {"Gusto por la expresión a través de la palabra, la lectura, la escritura y la argumentación.",
		[]string{"Derecho", "Comunicación", "Educación", "Literatura", "Filosofía"}},
	"AP": {"Gusto por expresarse y crear a través del arte, el diseño y lo estético.",
		[]string{"Arquitectura", "Publicidad", "Diseño Gráfico", "Artes"}},
	"MS": {"Sensibilidad e interés por la música, el ritmo y la expresión sonora.",
		[]string{"Artes", "Educación", "Comunicación"}},
	"OG": {"Gusto por organizar, planificar, ordenar información y seguir procedimientos.",
		[]string{"Contabilidad", "Administración", "Ingeniería Industrial", "Finanzas", "Banca y Seguros"}},
	"CT": {"Gusto por investigar, analizar, experimentar y comprender el porqué de las cosas.",
		[]string{"Ciencia de la Computación", "Inteligencia Artificial", "Ciencia de Datos", "Biología", "Física", "Química"}},
	"CL": {"Gusto por trabajar con números, problemas matemáticos y razonamiento lógico.",
		[]string{"Matemática", "Economía", "Ingeniería Civil", "Contabilidad", "Ingeniería Industrial"}},
	"MC": {"Gusto por construir, manipular, reparar y trabajar con herramientas y máquinas.",
		[]string{"Ingeniería Mecánica", "Ingeniería Mecatrónica", "Ingeniería Civil", "Arquitectura"}},
	"AL": {"Gusto por la naturaleza, los seres vivos, el medio ambiente y el trabajo al aire libre.",
		[]string{"Ingeniería Ambiental", "Biología", "Agronomía", "Ciencias de la Nutrición"}},
}

// leSenneChar: uno de los 8 caracteres de la caracterología de Le Senne,
// resultado de combinar Emotividad × Actividad × Resonancia (cada eje binario).
type leSenneChar struct {
	code   string
	label  string
	detail string
}

// leSenne mapea (emotivo, activo, secundario) -> carácter, según la
// caracterología clásica de Le Senne (alineado con prod_tiv.profiles).
func leSenne(emotivo, activo, secundario bool) leSenneChar {
	switch {
	case emotivo && activo && secundario:
		return leSenneChar{"APASIONADO", "Apasionado / Entregado", "Transformador del mundo, buscador de ideales. Combina la pasión con la constancia para concretar grandes proyectos."}
	case emotivo && activo && !secundario:
		return leSenneChar{"COLERICO", "Colérico / Impetuoso", "Entregado a la vida, osado, ganador. Energía desbordante y reacción inmediata ante los retos."}
	case emotivo && !activo && secundario:
		return leSenneChar{"SENTIMENTAL", "Sentimental / Reflexivo", "Soñador, con alta sensibilidad. Vive intensamente sus emociones y reflexiona profundamente."}
	case emotivo && !activo && !secundario:
		return leSenneChar{"NERVIOSO", "Nervioso / Artístico", "Creativo, busca la novedad. Sensible y cambiante, con gran imaginación."}
	case !emotivo && activo && secundario:
		return leSenneChar{"FLEMATICO", "Flemático / Conservador", "Tradicional, con inclinación por la rutina. Metódico, sereno y perseverante."}
	case !emotivo && activo && !secundario:
		return leSenneChar{"SANGUINEO", "Sanguíneo / Conquistador", "Amante de la vida, práctico. Sociable, adaptable y orientado a resultados concretos."}
	case !emotivo && !activo && secundario:
		return leSenneChar{"APATICO", "Apático / Disciplinado", "Pragmático y sobrio. Constante y reservado, fiel a sus hábitos."}
	default: // !emotivo && !activo && !primario
		return leSenneChar{"AMORFO", "Amorfo / Despreocupado", "Paciente, va a su propio ritmo por la vida. Tranquilo, tolerante y poco impulsivo."}
	}
}

// familiaBand clasifica el puntaje de apoyo familiar (M2, max ~10) en una
// de las 3 bandas del modelo (prod_tiv.profiles type_test 2).
func familiaBand(points, max int32) (code, label, detail string) {
	// Umbrales del modelo: alto (>=80%), medio (50-79%), bajo (<50%).
	pct := 0.0
	if max > 0 {
		pct = float64(points) / float64(max)
	}
	textOr := func(code, fallback string) string {
		if t := familiaText[code]; t != "" {
			return t
		}
		return fallback
	}
	switch {
	case pct >= 0.8:
		return "FELICIDADES", "¡Felicidades! Vas por buen camino",
			textOr("FELICIDADES", "Tu entorno familiar muestra una comunicación y un acompañamiento saludables respecto a tus intereses vocacionales.")
	case pct >= 0.5:
		return "ALERTA", "¡Vaya! Parece que algo no anda bien",
			textOr("ALERTA", "Hay aspectos de la comunicación familiar sobre tu vocación que conviene reforzar y conversar con más apertura.")
	default:
		return "PELIGRO", "¡Peligro! Hay aspectos que trabajar",
			textOr("PELIGRO", "La comunicación familiar respecto a tus intereses vocacionales necesita atención. Busca espacios de diálogo y apoyo.")
	}
}
