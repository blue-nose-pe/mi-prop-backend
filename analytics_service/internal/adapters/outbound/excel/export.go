// Package excelexport implementa ports.ExportCommands generando archivos
// .xlsx en memoria con github.com/xuri/excelize.
package excelexport

import (
	"bytes"
	"context"
	"fmt"

	"github.com/xuri/excelize/v2"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
)

type Exporter struct {
	dashboards ports.DashboardQueries
}

var _ ports.ExportCommands = (*Exporter)(nil)

func NewExporter(d ports.DashboardQueries) *Exporter { return &Exporter{dashboards: d} }

func (e *Exporter) ExportAsesorDashboard(ctx context.Context, asesorID domain.UserID) ([]byte, error) {
	d, err := e.dashboards.GetAsesorDashboard(ctx, asesorID)
	if err != nil {
		return nil, err
	}
	rows := [][]any{
		{"Asesor", d.AsesorName},
		{"Total colegios", d.TotalColegios},
		{"Total keys", d.TotalKeys},
		{"Total attempts", d.TotalAttempts},
		{},
		{"Tipo de examen", "Attempts", "Score promedio", "Score máximo prom."},
	}
	for code, s := range d.ByExamType {
		rows = append(rows, []any{code, s.Attempts, s.AvgScore, s.AvgMaxScore})
	}
	return writeSheet("Asesor", rows)
}

func (e *Exporter) ExportColegioDashboard(ctx context.Context, schoolID domain.SchoolID) ([]byte, error) {
	d, err := e.dashboards.GetColegioDashboard(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	rows := [][]any{
		{"Colegio", d.SchoolName},
		{"Total estudiantes", d.TotalStudents},
		{"Total attempts", d.TotalAttempts},
		{},
		{"Tipo de examen", "Attempts", "Score promedio", "Score máximo prom."},
	}
	for code, s := range d.ByExamType {
		rows = append(rows, []any{code, s.Attempts, s.AvgScore, s.AvgMaxScore})
	}
	return writeSheet("Colegio", rows)
}

func (e *Exporter) ExportColegioComparativo(ctx context.Context, examTypeCode string) ([]byte, error) {
	c, err := e.dashboards.GetColegioComparativo(ctx, examTypeCode)
	if err != nil {
		return nil, err
	}
	rows := [][]any{
		{"Colegio", "Score promedio", "Attempts"},
	}
	for _, it := range c.Items {
		rows = append(rows, []any{it.SchoolName, it.AvgScore, it.Attempts})
	}
	return writeSheet("Comparativo", rows)
}

// writeSheet centraliza la generación del .xlsx para no repetir 3 veces
// el setup. Cada fila se escribe con SetSheetRow en A{n+1}.
func writeSheet(sheet string, rows [][]any) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	for i, row := range rows {
		cell := fmt.Sprintf("A%d", i+1)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
