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
		AsesorId:      string(d.AsesorID),
		AsesorName:    d.AsesorName,
		TotalColegios: d.TotalColegios,
		TotalKeys:     d.TotalKeys,
		TotalAttempts: d.TotalAttempts,
		ByExamType:    toExamTypeStats(d.ByExamType),
		GeneratedAt:   timestamppb.New(d.GeneratedAt),
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
	bs, err := h.exporter.ExportColegioComparativo(ctx, req.GetExamTypeCode())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExportXLSXResponse{Content: bs, Filename: "comparativo-" + req.GetExamTypeCode() + ".xlsx"}, nil
}

// ----- mappers -----

func toExamTypeStats(m map[string]domain.ExamTypeStats) map[string]*pb.ExamTypeStats {
	out := make(map[string]*pb.ExamTypeStats, len(m))
	for k, v := range m {
		out[k] = &pb.ExamTypeStats{
			Attempts:    v.Attempts,
			AvgScore:    v.AvgScore,
			AvgMaxScore: v.AvgMaxScore,
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
		})
	}
	return out
}
