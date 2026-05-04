// Cliente gRPC contra exams_service. Reemplaza al NoopExams cuando
// EXAMS_SERVICE_ADDR está configurado.
//
// Cobertura del puerto ports.ExamsClient:
//   - ListAttemptsByUser    -> exams.v1.AttemptService/ListByUser   (RPC directo)
//   - ListAttemptsByExam    -> exams.v1.AttemptService/ListByExam   (RPC directo)
//   - ListAttemptsByColegio -> NoOp: AttemptService NO expone un RPC
//                              ListByColegio. Para implementarlo habría
//                              que: (a) listar exams del colegio y
//                              concatenar attempts, o (b) agregar el RPC
//                              en exams_service. NO se inventa: NoOp.
//   - GetExam               -> exams.v1.ExamService/GetExam         (RPC directo)
package clients

import (
	"context"
	"fmt"
	"log"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"

	examspb "exams_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type GrpcExams struct {
	conn     *grpc.ClientConn
	exams    examspb.ExamServiceClient
	attempts examspb.AttemptServiceClient
}

var _ ports.ExamsClient = (*GrpcExams)(nil)

func NewGrpcExams(addr string) (*GrpcExams, error) {
	if addr == "" {
		return nil, fmt.Errorf("exams_service address is empty")
	}
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial exams_service: %w", err)
	}
	return &GrpcExams{
		conn:     conn,
		exams:    examspb.NewExamServiceClient(conn),
		attempts: examspb.NewAttemptServiceClient(conn),
	}, nil
}

func (g *GrpcExams) Close() error {
	if g == nil || g.conn == nil {
		return nil
	}
	return g.conn.Close()
}

func (g *GrpcExams) ListAttemptsByUser(ctx context.Context, userID domain.UserID) ([]ports.UpstreamAttempt, error) {
	resp, err := g.attempts.ListByUser(forwardAuth(ctx), &examspb.ListAttemptsByUserRequest{UserId: string(userID)})
	if err != nil {
		return nil, err
	}
	return mapAttempts(resp.GetItems()), nil
}

func (g *GrpcExams) ListAttemptsByExam(ctx context.Context, examID domain.ExamID) ([]ports.UpstreamAttempt, error) {
	resp, err := g.attempts.ListByExam(forwardAuth(ctx), &examspb.ListAttemptsByExamRequest{ExamId: string(examID)})
	if err != nil {
		return nil, err
	}
	return mapAttempts(resp.GetItems()), nil
}

// ListAttemptsByColegio — NoOp. exams_service no expone un RPC para listar
// attempts por colegio. Se necesitaría agregarlo en el proto de exams.
func (g *GrpcExams) ListAttemptsByColegio(_ context.Context, schoolID domain.SchoolID) ([]ports.UpstreamAttempt, error) {
	log.Printf("[grpc_exams] ListAttemptsByColegio: NoOp (RPC no existe) school_id=%s", schoolID)
	return nil, nil
}

func (g *GrpcExams) GetExam(ctx context.Context, id domain.ExamID) (*ports.UpstreamExam, error) {
	resp, err := g.exams.GetExam(forwardAuth(ctx), &examspb.GetExamRequest{Id: string(id)})
	if err != nil {
		return nil, err
	}
	e := resp.GetExam()
	if e == nil {
		return nil, fmt.Errorf("exams_service returned empty exam for id=%s", id)
	}
	// El proto de exams expone exam_type_id (int IDENTITY), no el code.
	// Los IDs no son estables across deploys (los seeds usan IDENTITY,
	// no INT explícito), así que no podemos hardcodear 1->vocacional.
	// Para resolverlo correctamente habría que:
	//   - exponer un nuevo RPC ListExamTypes en exams_service, o
	//   - incluir exam_type_code en el message Exam.
	// Por ahora dejamos el code vacío; el core debe tolerarlo.
	return &ports.UpstreamExam{
		ID:           domain.ExamID(e.GetId()),
		ExamTypeCode: "", // best-effort: ver comentario arriba
		Name:         e.GetName(),
		SchoolID:     domain.SchoolID(e.GetSchoolId()),
		Version:      e.GetVersion(),
	}, nil
}

func mapAttempts(items []*examspb.Attempt) []ports.UpstreamAttempt {
	out := make([]ports.UpstreamAttempt, 0, len(items))
	for _, a := range items {
		if a == nil {
			continue
		}
		ua := ports.UpstreamAttempt{
			ID:     domain.AttemptID(a.GetId()),
			ExamID: domain.ExamID(a.GetExamId()),
			UserID: domain.UserID(a.GetUserId()),
		}
		// Score y MaxScore son escalares en el proto; los pasamos a *int32
		// solo si vinieron != 0 para distinguir "no enviado" de "0 puntos".
		// El proto no diferencia; best-effort: si el Attempt aún no
		// terminó (no hay submitted_at), dejamos los punteros nil.
		if t := a.GetSubmittedAt(); t != nil {
			submitted := t.AsTime()
			ua.SubmittedAt = &submitted
			score := a.GetScore()
			ua.Score = &score
			max := a.GetMaxScore()
			ua.MaxScore = &max
		}
		if t := a.GetStartedAt(); t != nil {
			ua.StartedAt = t.AsTime()
		}
		out = append(out, ua)
	}
	return out
}
