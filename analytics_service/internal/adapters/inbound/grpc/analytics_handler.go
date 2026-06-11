package grpchandler

import (
	"context"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"
	"analytics_service/internal/shared/apperr"
	pb "analytics_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type AnalyticsHandler struct {
	pb.UnimplementedAnalyticsServiceServer
	qrys     ports.DashboardQueries
	exporter ports.ExportCommands
}

func NewAnalyticsHandler(q ports.DashboardQueries, e ports.ExportCommands) *AnalyticsHandler {
	return &AnalyticsHandler{qrys: q, exporter: e}
}

func (h *AnalyticsHandler) GetAsesorDashboard(ctx context.Context, req *pb.GetAsesorDashboardRequest) (*pb.AsesorDashboardResponse, error) {
	d, err := h.qrys.GetAsesorDashboard(ctx, domain.UserID(req.GetAsesorId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.AsesorDashboardResponse{
		AsesorId:         string(d.AsesorID),
		AsesorName:       d.AsesorName,
		TotalColegios:    d.TotalColegios,
		TotalKeys:        d.TotalKeys,
		TotalAttempts:    d.TotalAttempts,
		CompletedVisits:  d.CompletedVisits,
		ScheduledVisits:  d.ScheduledVisits,
		PendingTests:     d.PendingTests,
		AffectedStudents: d.AffectedStudents,
		TotalAforo:       d.TotalAforo,
		TotalImpactados:  d.TotalImpactados,
		ByExamType:       toExamTypeStats(d.ByExamType),
		GeneratedAt:      timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) GetColegioDashboard(ctx context.Context, req *pb.GetColegioDashboardRequest) (*pb.ColegioDashboardResponse, error) {
	d, err := h.qrys.GetColegioDashboard(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ColegioDashboardResponse{
		SchoolId:      string(d.SchoolID),
		SchoolName:    d.SchoolName,
		TotalStudents: d.TotalStudents,
		TotalAttempts: d.TotalAttempts,
		ByExamType:    toExamTypeStats(d.ByExamType),
		GeneratedAt:   timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) GetEstudianteDashboard(ctx context.Context, req *pb.GetEstudianteDashboardRequest) (*pb.EstudianteDashboardResponse, error) {
	d, err := h.qrys.GetEstudianteDashboard(ctx, domain.UserID(req.GetUserId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EstudianteDashboardResponse{
		UserId:      string(d.UserID),
		StudentName: d.StudentName,
		Tests:       toTestResults(d.Tests),
		GeneratedAt: timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) GetColegioComparativo(ctx context.Context, req *pb.GetColegioComparativoRequest) (*pb.ColegioComparativoResponse, error) {
	c, err := h.qrys.GetColegioComparativo(ctx, req.GetExamTypeCode())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ColegioComparativoResponse{
		ExamTypeCode: c.ExamTypeCode,
		Items:        make([]*pb.ColegioComparativoItem, 0, len(c.Items)),
		GeneratedAt:  timestamppb.New(c.GeneratedAt),
	}
	for _, it := range c.Items {
		out.Items = append(out.Items, &pb.ColegioComparativoItem{
			SchoolId:   string(it.SchoolID),
			SchoolName: it.SchoolName,
			AvgScore:   it.AvgScore,
			Attempts:   it.Attempts,
		})
	}
	return out, nil
}

func (h *AnalyticsHandler) GetHistoricoEstudiante(ctx context.Context, req *pb.GetHistoricoEstudianteRequest) (*pb.HistoricoEstudianteResponse, error) {
	d, err := h.qrys.GetHistoricoEstudiante(ctx, domain.UserID(req.GetUserId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.HistoricoEstudianteResponse{
		UserId:      string(d.UserID),
		Items:       toTestResults(d.Items),
		GeneratedAt: timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) GetHistoricoColegio(ctx context.Context, req *pb.GetHistoricoColegioRequest) (*pb.HistoricoColegioResponse, error) {
	d, err := h.qrys.GetHistoricoColegio(ctx, ports.HistoricoColegioInput{
		SchoolID:     domain.SchoolID(req.GetSchoolId()),
		ExamTypeCode: req.GetExamTypeCode(),
		Periods:      req.GetPeriods(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	items := make([]*pb.HistoricoColegioPoint, 0, len(d.Items))
	for _, p := range d.Items {
		items = append(items, &pb.HistoricoColegioPoint{
			Period:       p.Period,
			Year:         p.Year,
			Quarter:      p.Quarter,
			AvgScore:     p.AvgScore,
			Attempts:     p.Attempts,
			VariationPct: p.VariationPct,
			HasPrevious:  p.HasPrevious,
		})
	}
	return &pb.HistoricoColegioResponse{
		SchoolId:     string(d.SchoolID),
		SchoolName:   d.SchoolName,
		City:         d.City,
		Category:     d.Category,
		ExamTypeCode: d.ExamTypeCode,
		Items:        items,
		GeneratedAt:  timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) GetAsesorPendientes(ctx context.Context, req *pb.GetAsesorPendientesRequest) (*pb.AsesorPendientesResponse, error) {
	d, err := h.qrys.GetAsesorPendientes(ctx, domain.UserID(req.GetAsesorId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	byType := make([]*pb.PendingExamTypeCount, 0, len(d.ByExamType))
	for _, e := range d.ByExamType {
		byType = append(byType, &pb.PendingExamTypeCount{
			ExamTypeCode:     e.ExamTypeCode,
			PendingAttempts:  e.PendingAttempts,
			AffectedStudents: e.AffectedStudents,
		})
	}
	students := make([]*pb.PendingStudent, 0, len(d.Students))
	for _, s := range d.Students {
		pes := make([]*pb.PendingExam, 0, len(s.PendingExams))
		for _, p := range s.PendingExams {
			pes = append(pes, &pb.PendingExam{
				ExamId:       string(p.ExamID),
				ExamName:     p.ExamName,
				ExamTypeCode: p.ExamTypeCode,
				ExamCode:     p.ExamCode,
			})
		}
		students = append(students, &pb.PendingStudent{
			UserId:       string(s.UserID),
			StudentName:  s.StudentName,
			SchoolId:     string(s.SchoolID),
			SchoolName:   s.SchoolName,
			PendingExams: pes,
		})
	}
	return &pb.AsesorPendientesResponse{
		AsesorId:      string(d.AsesorID),
		AsesorName:    d.AsesorName,
		TotalPending:  d.TotalPending,
		TotalStudents: d.TotalStudents,
		ByExamType:    byType,
		Students:      students,
		GeneratedAt:   timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) GetReporteEstudiante(ctx context.Context, req *pb.GetReporteEstudianteRequest) (*pb.ReporteEstudianteResponse, error) {
	r, err := h.qrys.GetReporteEstudiante(ctx, ports.ReporteEstudianteInput{
		UserID:    domain.UserID(req.GetUserId()),
		AttemptID: domain.AttemptID(req.GetAttemptId()),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ReporteEstudianteResponse{
		UserId:       string(r.UserID),
		StudentName:  r.StudentName,
		AttemptId:    string(r.AttemptID),
		ExamId:       string(r.ExamID),
		ExamName:     r.ExamName,
		ExamTypeCode: r.ExamTypeCode,
		Score:        r.Score,
		MaxScore:     r.MaxScore,
		AreasInteres:   toAreasInteres(r.AreasInteres),
		Personalidad:   toReportSection(r.Personalidad),
		ApoyoFamiliar:  toReportSection(r.ApoyoFamiliar),
		ProyectoDeVida: toReportSection(r.ProyectoDeVida),
		GeneratedAt:    timestamppb.New(r.GeneratedAt),
	}
	if r.SubmittedAt != nil {
		out.SubmittedAt = timestamppb.New(*r.SubmittedAt)
	}
	return out, nil
}

func toReportSection(s domain.ReportSection) *pb.ReportSection {
	items := make([]*pb.CategoryStat, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, &pb.CategoryStat{
			Code:      it.Code,
			Label:     it.Label,
			Points:    it.Points,
			MaxPoints: it.MaxPoints,
		})
	}
	return &pb.ReportSection{
		Available:    s.Available,
		Reason:       s.Reason,
		ResultCode:   s.ResultCode,
		ResultLabel:  s.ResultLabel,
		ResultDetail: s.ResultDetail,
		Items:        items,
	}
}

func toAreasInteres(s domain.AreasInteresSection) *pb.AreasInteresSection {
	scores := make(map[string]*pb.CategoryStat, len(s.Scores))
	for k, v := range s.Scores {
		scores[k] = &pb.CategoryStat{
			Code:      v.Code,
			Label:     v.Label,
			Points:    v.Points,
			MaxPoints: v.MaxPoints,
		}
	}
	top := make([]*pb.AreaInteresMatch, 0, len(s.Top))
	for _, t := range s.Top {
		top = append(top, &pb.AreaInteresMatch{
			Code:            t.Code,
			Label:           t.Label,
			AreaLabel:       t.AreaLabel,
			Characteristics: t.Characteristics,
			Careers:         t.Careers,
			Points:          t.Points,
			MaxPoints:       t.MaxPoints,
		})
	}
	return &pb.AreasInteresSection{
		Available: s.Available,
		Reason:    s.Reason,
		Scores:    scores,
		Top:       top,
	}
}

func (h *AnalyticsHandler) GetColegiosHistorico(ctx context.Context, req *pb.GetColegiosHistoricoRequest) (*pb.ColegiosHistoricoResponse, error) {
	d, err := h.qrys.GetColegiosHistorico(ctx, ports.ColegiosHistoricoInput{
		Period:       req.GetPeriod(),
		ExamTypeCode: req.GetExamTypeCode(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	items := make([]*pb.ColegiosHistoricoRow, 0, len(d.Items))
	for _, r := range d.Items {
		items = append(items, &pb.ColegiosHistoricoRow{
			SchoolId:     string(r.SchoolID),
			SchoolName:   r.SchoolName,
			City:         r.City,
			Category:     r.Category,
			AvgScore:     r.AvgScore,
			Attempts:     r.Attempts,
			VariationPct: r.VariationPct,
			HasPrevious:  r.HasPrevious,
			TopAreaCode:  r.TopAreaCode,
			TopAreaLabel: r.TopAreaLabel,
		})
	}
	return &pb.ColegiosHistoricoResponse{
		Period:       d.Period,
		ExamTypeCode: d.ExamTypeCode,
		Items:        items,
		GeneratedAt:  timestamppb.New(d.GeneratedAt),
	}, nil
}

func (h *AnalyticsHandler) ExportAsesorXLSX(ctx context.Context, req *pb.ExportAsesorXLSXRequest) (*pb.ExportXLSXResponse, error) {
	bs, err := h.exporter.ExportAsesorDashboard(ctx, domain.UserID(req.GetAsesorId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExportXLSXResponse{Content: bs, Filename: "asesor-" + req.GetAsesorId() + ".xlsx"}, nil
}

func (h *AnalyticsHandler) ExportColegioXLSX(ctx context.Context, req *pb.ExportColegioXLSXRequest) (*pb.ExportXLSXResponse, error) {
	bs, err := h.exporter.ExportColegioDashboard(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExportXLSXResponse{Content: bs, Filename: "colegio-" + req.GetSchoolId() + ".xlsx"}, nil
}

func (h *AnalyticsHandler) ExportComparativoXLSX(ctx context.Context, req *pb.ExportComparativoXLSXRequest) (*pb.ExportXLSXResponse, error) {
	bs, err := h.exporter.ExportColegioComparativo(ctx, req.GetExamTypeCode(), req.GetPeriod())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExportXLSXResponse{Content: bs, Filename: "historico-colegios-" + req.GetExamTypeCode() + ".xlsx"}, nil
}

func (h *AnalyticsHandler) ExportReporteEstudianteXLSX(ctx context.Context, req *pb.ExportReporteEstudianteXLSXRequest) (*pb.ExportXLSXResponse, error) {
	bs, err := h.exporter.ExportReporteEstudiante(ctx, ports.ReporteEstudianteInput{
		UserID:    domain.UserID(req.GetUserId()),
		AttemptID: domain.AttemptID(req.GetAttemptId()),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExportXLSXResponse{Content: bs, Filename: "reporte-estudiante-" + req.GetUserId() + ".xlsx"}, nil
}

// ----- mappers -----

func toExamTypeStats(m map[string]domain.ExamTypeStats) map[string]*pb.ExamTypeStats {
	out := make(map[string]*pb.ExamTypeStats, len(m))
	for k, v := range m {
		areas := make([]*pb.ColegioAreaStat, 0, len(v.Areas))
		for _, a := range v.Areas {
			areas = append(areas, &pb.ColegioAreaStat{
				Code:      a.Code,
				Label:     a.Label,
				Points:    a.Points,
				MaxPoints: a.MaxPoints,
				Ratio:     a.Ratio,
			})
		}
		out[k] = &pb.ExamTypeStats{
			Attempts:    v.Attempts,
			AvgScore:    v.AvgScore,
			AvgMaxScore: v.AvgMaxScore,
			Areas:       areas,
		}
	}
	return out
}

func toTestResults(items []domain.TestResult) []*pb.TestResult {
	out := make([]*pb.TestResult, 0, len(items))
	for _, it := range items {
		out = append(out, &pb.TestResult{
			ExamTypeCode: it.ExamTypeCode,
			ExamId:       string(it.ExamID),
			ExamName:     it.ExamName,
			Score:        it.Score,
			MaxScore:     it.MaxScore,
			SubmittedAt:  timestamppb.New(it.SubmittedAt),
			AttemptId:    string(it.AttemptID),
		})
	}
	return out
}
