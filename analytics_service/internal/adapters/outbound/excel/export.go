// Package excelexport implementa ports.ExportCommands generando archivos
// .xlsx en memoria con github.com/xuri/excelize.
package excelexport

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/xuri/excelize/v2"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
)

// moduleRank: fila ordenable usada por ExportReporteEstudiante cuando arma
// la hoja "Puntajes por módulo" del simulacro/habitos.
type moduleRank struct {
	Code      string
	Points    int32
	MaxPoints int32
}

type Exporter struct {
	dashboards ports.DashboardQueries
}

var _ ports.ExportCommands = (*Exporter)(nil)

func NewExporter(d ports.DashboardQueries) *Exporter { return &Exporter{dashboards: d} }

// ExportAsesorDashboard genera un workbook con 3 hojas:
//   - "Asesor"   resumen + breakdown por tipo de examen (sin cambios).
//   - "Colegios" un row por colegio asignado (assignment SCD-2 vigente).
//                Sirve para que el asesor sepa CUALES son sus colegios
//                en vez de solo "Total colegios: N".
//   - "Keys"     un row por key creada/asociada con tipo, colegio (o
//                vacio si LAN) y aforo (current/max). Sirve para auditar
//                que keys siguen siendo utiles y cuales se agotaron.
func (e *Exporter) ExportAsesorDashboard(ctx context.Context, asesorID domain.UserID) ([]byte, error) {
	d, err := e.dashboards.GetAsesorDashboard(ctx, asesorID)
	if err != nil {
		return nil, err
	}

	resumen := [][]any{
		{"Asesor", d.AsesorName},
		{"Total colegios", d.TotalColegios},
		{"Total keys", d.TotalKeys},
		{"Total attempts", d.TotalAttempts},
		{"Visitas completadas", d.CompletedVisits},
		{"Visitas agendadas", d.ScheduledVisits},
		{},
		{"Tipo de examen", "Attempts", "Score promedio", "Score máximo prom."},
	}
	for code, s := range d.ByExamType {
		resumen = append(resumen, []any{code, s.Attempts, s.AvgScore, s.AvgMaxScore})
	}

	// Pestaña "Colegios": un row por colegio asignado. Orden alfabetico
	// por nombre para que el archivo abra en un orden util sin necesidad
	// de re-sortear en Excel.
	colegios := make([]domain.AsesorColegio, len(d.Colegios))
	copy(colegios, d.Colegios)
	sort.Slice(colegios, func(i, j int) bool {
		return colegios[i].Name < colegios[j].Name
	})
	colegiosRows := [][]any{
		{"Colegio", "Ciudad", "Categoría", "ID"},
	}
	for _, c := range colegios {
		colegiosRows = append(colegiosRows, []any{c.Name, c.City, c.Category, string(c.ID)})
	}

	// Pestaña "Keys": orden por uso (descendente) y luego por code, asi
	// las mas usadas aparecen primero.
	keys := make([]domain.AsesorKey, len(d.Keys))
	copy(keys, d.Keys)
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].CurrentUses != keys[j].CurrentUses {
			return keys[i].CurrentUses > keys[j].CurrentUses
		}
		return keys[i].Code < keys[j].Code
	})
	keysRows := [][]any{
		{"Código", "Tipo de examen", "Colegio", "Usos", "Aforo máximo"},
	}
	for _, k := range keys {
		school := k.SchoolName
		if school == "" {
			school = "— (LAN)"
		}
		keysRows = append(keysRows, []any{k.Code, k.ExamTypeCode, school, k.CurrentUses, k.MaxUses})
	}

	return writeMultiSheet([]xlsxSheet{
		{Name: "Asesor", Rows: resumen},
		{Name: "Colegios", Rows: colegiosRows},
		{Name: "Keys", Rows: keysRows},
	})
}

func (e *Exporter) ExportColegioDashboard(ctx context.Context, schoolID domain.SchoolID) ([]byte, error) {
	d, err := e.dashboards.GetColegioDashboard(ctx, schoolID, "") // export = todo el histórico
	if err != nil {
		return nil, err
	}
	resumen := [][]any{
		{"Colegio", d.SchoolName},
		{"Total estudiantes", d.TotalStudents},
		{"Total attempts", d.TotalAttempts},
		{},
		{"Tipo de examen", "Attempts", "Score promedio", "Score máximo prom."},
	}
	for code, s := range d.ByExamType {
		resumen = append(resumen, []any{code, s.Attempts, s.AvgScore, s.AvgMaxScore})
	}

	// Pestaña "Estudiantes": antes el export solo tenia rollup y el cliente
	// se quedaba sin la lista nominal. Ahora arma una segunda hoja con un
	// row por estudiante activo del colegio. Orden alfabetico por apellido,
	// nombre para que el archivo abra en un orden util sin necesidad de
	// re-sortear en Excel.
	students := make([]domain.ColegioStudent, len(d.Students))
	copy(students, d.Students)
	sort.Slice(students, func(i, j int) bool {
		if students[i].LastName != students[j].LastName {
			return students[i].LastName < students[j].LastName
		}
		return students[i].FirstName < students[j].FirstName
	})
	estudiantes := [][]any{
		{"Nombre", "Apellido", "DNI", "Correo", "Telefono", "ID"},
	}
	for _, s := range students {
		estudiantes = append(estudiantes, []any{
			s.FirstName,
			s.LastName,
			s.DocumentNumber,
			s.Email,
			s.Phone,
			string(s.ID),
		})
	}

	return writeMultiSheet([]xlsxSheet{
		{Name: "Colegio", Rows: resumen},
		{Name: "Estudiantes", Rows: estudiantes},
	})
}

// ExportColegioComparativo: Cliente (2026-06-04) — el xlsx ahora usa
// EXACTAMENTE la misma data que la tabla "Histórico de Colegios"
// (GetColegiosHistorico, period-aware + normalizado), para que tabla y export
// coincidan. Antes usaba GetColegioComparativo (otra query, otro promedio y
// otros attempts) → no cuadraban. Las columnas y el orden (alfabético por
// nombre) replican la tabla. En Estilos (habitos) se omite "Puntaje promedio"
// igual que en la UI.
func (e *Exporter) ExportColegioComparativo(ctx context.Context, examTypeCode string, period string) ([]byte, error) {
	listing, err := e.dashboards.GetColegiosHistorico(ctx, ports.ColegiosHistoricoInput{
		Period:       period,
		ExamTypeCode: examTypeCode,
	})
	if err != nil {
		return nil, err
	}
	showScore := examTypeCode != "habitos"
	header := []any{"Colegio", "Ciudad", "Segmento", "Periodo"}
	if showScore {
		header = append(header, "Puntaje promedio")
	}
	header = append(header, "Attempts", "Variación")
	rows := [][]any{header}
	for _, it := range listing.Items {
		variacion := "Sin previo"
		if it.HasPrevious {
			variacion = fmt.Sprintf("%+.1f%%", it.VariationPct)
		}
		row := []any{it.SchoolName, it.City, it.Category, listing.Period}
		if showScore {
			// Mismo redondeo que la tabla (1 decimal).
			row = append(row, fmt.Sprintf("%.1f", it.AvgScore))
		}
		row = append(row, it.Attempts, variacion)
		rows = append(rows, row)
	}
	return writeMultiSheet([]xlsxSheet{{
		Name:      "Histórico",
		Rows:      rows,
		ColWidths: map[string]float64{"A": 40, "B": 18, "C": 12, "D": 14, "E": 16, "F": 12, "G": 12},
	}})
}

// ExportReporteEstudiante construye un workbook multi-hoja con el reporte
// "Tour Vocacional UCSP". Reusa GetReporteEstudiante para que el contenido
// XLSX y el endpoint JSON queden alineados a la misma rúbrica.
//
// La forma del workbook se adapta al tipo de examen:
//   - vocacional: 4 hojas (Resumen + Áreas RIASEC + Top Áreas con carreras + Secciones)
//   - simulacro/habitos: 3 hojas (Resumen + Puntajes por módulo + Secciones)
//
// Esto evita que un simulacro muestre tabs vacías de "Áreas de Interés"
// (RIASEC no aplica) o tabs con código=área=etiqueta duplicados.
func (e *Exporter) ExportReporteEstudiante(ctx context.Context, in ports.ReporteEstudianteInput) ([]byte, error) {
	r, err := e.dashboards.GetReporteEstudiante(ctx, in)
	if err != nil {
		return nil, err
	}

	submitted := "—"
	if r.SubmittedAt != nil {
		submitted = r.SubmittedAt.Format("2006-01-02 15:04")
	}

	resumen := [][]any{
		{"Estudiante", r.StudentName},
		{"ID Estudiante", string(r.UserID)},
		{"Examen", r.ExamName},
		{"Tipo de examen", r.ExamTypeCode},
		{"ID Attempt", string(r.AttemptID)},
		{"Fecha de envío", submitted},
		{"Puntaje", r.Score},
		{"Puntaje máximo", r.MaxScore},
		{"Generado", r.GeneratedAt.Format("2006-01-02 15:04")},
	}

	secciones := [][]any{
		{"Sección", "Disponible", "Motivo"},
		{"Personalidad", boolEsp(r.Personalidad.Available), r.Personalidad.Reason},
		{"Apoyo familiar", boolEsp(r.ApoyoFamiliar.Available), r.ApoyoFamiliar.Reason},
		{"Proyecto de vida", boolEsp(r.ProyectoDeVida.Available), r.ProyectoDeVida.Reason},
	}

	sheets := []xlsxSheet{{Name: "Resumen", Rows: resumen}}

	// Cliente (doc observaciones): cada tipo de examen exporta SOLO las hojas
	// que le aplican.
	//   - vocacional: Resumen + Áreas RIASEC + Top Áreas + Secciones.
	//   - habitos:    Resumen + Puntajes por módulo + Secciones.
	//   - simulacro:  Resumen unicamente (es un puntaje; no lleva "Puntajes por
	//                 módulo" ni las "Secciones" psicométricas —Personalidad/
	//                 Apoyo familiar/Proyecto de vida— que solo aplican a las
	//                 evaluaciones psicométricas).
	switch r.ExamTypeCode {
	case "vocacional":
		// Vocacional: RIASEC completo + top con carreras sugeridas.
		areas := [][]any{
			{"Código", "Área", "Puntos", "Puntos máximos", "Porcentaje"},
		}
		if r.AreasInteres.Available {
			for _, code := range []string{"R", "I", "A", "S", "E", "C"} {
				s, ok := r.AreasInteres.Scores[code]
				if !ok {
					continue
				}
				pct := 0.0
				if s.MaxPoints > 0 {
					pct = float64(s.Points) / float64(s.MaxPoints) * 100
				}
				areas = append(areas, []any{s.Code, s.Label, s.Points, s.MaxPoints, fmt.Sprintf("%.1f%%", pct)})
			}
		} else {
			reason := r.AreasInteres.Reason
			if reason == "" {
				reason = "No hay respuestas categorizadas para calcular las áreas de interés."
			}
			areas = append(areas, []any{"—", reason, "", "", ""})
		}

		top := [][]any{
			{"Ranking", "Código", "Área", "Características", "Carreras sugeridas", "Puntos", "Puntos máximos"},
		}
		for i, t := range r.AreasInteres.Top {
			careers := ""
			for j, c := range t.Careers {
				if j > 0 {
					careers += ", "
				}
				careers += c
			}
			top = append(top, []any{i + 1, t.Code, t.AreaLabel, t.Characteristics, careers, t.Points, t.MaxPoints})
		}

		sheets = append(sheets,
			xlsxSheet{Name: "Áreas de Interés", Rows: areas},
			xlsxSheet{Name: "Top Áreas", Rows: top},
			xlsxSheet{Name: "Secciones", Rows: secciones},
		)
	case "simulacro":
		// Solo Resumen. El simulacro es un puntaje; no lleva "Puntajes por
		// módulo" ni "Secciones" psicométricas (no aplican).
	default:
		// habitos / otros: los "Code" agrupan módulos del examen (COM, MAT,
		// BIO, etc.). Se lista en una sola hoja "Puntajes por módulo" + Secciones.
		mod := [][]any{
			{"Módulo", "Puntos", "Puntos máximos", "Porcentaje"},
		}
		if r.AreasInteres.Available {
			ranks := make([]moduleRank, 0, len(r.AreasInteres.Scores))
			for code, s := range r.AreasInteres.Scores {
				ranks = append(ranks, moduleRank{Code: code, Points: s.Points, MaxPoints: s.MaxPoints})
			}
			// Mismo criterio que el back: por puntos desc, alfabético para desempate.
			sort.Slice(ranks, func(i, j int) bool {
				if ranks[i].Points != ranks[j].Points {
					return ranks[i].Points > ranks[j].Points
				}
				return ranks[i].Code < ranks[j].Code
			})
			for _, m := range ranks {
				pct := 0.0
				if m.MaxPoints > 0 {
					pct = float64(m.Points) / float64(m.MaxPoints) * 100
				}
				mod = append(mod, []any{m.Code, m.Points, m.MaxPoints, fmt.Sprintf("%.1f%%", pct)})
			}
		} else {
			reason := r.AreasInteres.Reason
			if reason == "" {
				reason = "Sin desglose por módulo disponible para este attempt."
			}
			mod = append(mod, []any{"—", "", "", reason})
		}
		sheets = append(sheets,
			xlsxSheet{Name: "Puntajes por módulo", Rows: mod},
			xlsxSheet{Name: "Secciones", Rows: secciones},
		)
	}

	return writeMultiSheet(sheets)
}

func boolEsp(b bool) string {
	if b {
		return "Sí"
	}
	return "No"
}


// writeSheet centraliza la generación del .xlsx para no repetir 3 veces
// el setup. Cada fila se escribe con SetSheetRow en A{n+1}.
func writeSheet(sheet string, rows [][]any) ([]byte, error) {
	return writeMultiSheet([]xlsxSheet{{Name: sheet, Rows: rows}})
}

// xlsxSheet describe una hoja a generar: nombre + filas. Lo usa
// writeMultiSheet para construir workbooks con varias pestañas en una sola
// pasada (reporte por estudiante, por ejemplo).
//
// ColWidths y ColNumFmt son opcionales y se aplican solo a las columnas
// listadas (key = letra de columna, ej "A", "B"). Las demás columnas
// quedan con el default de excelize.
type xlsxSheet struct {
	Name      string
	Rows      [][]any
	ColWidths map[string]float64
	ColNumFmt map[string]string
}

// writeMultiSheet genera un .xlsx en memoria con N hojas. La primera hoja
// queda activa. Si la lista está vacía devuelve un workbook con una hoja
// "Hoja1" vacía para no romper la convención de excelize.
func writeMultiSheet(sheets []xlsxSheet) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	for i, s := range sheets {
		idx, err := f.NewSheet(s.Name)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			f.SetActiveSheet(idx)
		}
		for j, row := range s.Rows {
			cell := fmt.Sprintf("A%d", j+1)
			if err := f.SetSheetRow(s.Name, cell, &row); err != nil {
				return nil, err
			}
		}
		for col, w := range s.ColWidths {
			if err := f.SetColWidth(s.Name, col, col, w); err != nil {
				return nil, err
			}
		}
		for col, fmtCode := range s.ColNumFmt {
			styleID, err := f.NewStyle(&excelize.Style{CustomNumFmt: &fmtCode})
			if err != nil {
				return nil, err
			}
			if err := f.SetColStyle(s.Name, col, styleID); err != nil {
				return nil, err
			}
		}
	}
	// excelize crea Sheet1 por defecto; lo borramos solo si añadimos otras.
	if len(sheets) > 0 {
		_ = f.DeleteSheet("Sheet1")
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
