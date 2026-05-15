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
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"

	examspb "exams_service/proto/gen"
	examspbcommon "exams_service/proto/gen/common"

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

func (g *GrpcExams) ListAttemptsByColegio(ctx context.Context, schoolID domain.SchoolID) ([]ports.UpstreamAttempt, error) {
	if schoolID == "" {
		return nil, nil
	}
	resp, err := g.attempts.ListByColegio(forwardAuth(ctx), &examspb.ListAttemptsByColegioRequest{SchoolId: string(schoolID)})
	if err != nil {
		return nil, err
	}
	return mapAttempts(resp.GetItems()), nil
}

func (g *GrpcExams) GetAttempt(ctx context.Context, id domain.AttemptID) (*ports.UpstreamAttempt, error) {
	resp, err := g.attempts.GetAttempt(forwardAuth(ctx), &examspb.GetAttemptRequest{AttemptId: string(id)})
	if err != nil {
		return nil, err
	}
	items := mapAttempts([]*examspb.Attempt{resp.GetAttempt()})
	if len(items) == 0 {
		return nil, fmt.Errorf("exams_service returned empty attempt for id=%s", id)
	}
	return &items[0], nil
}

func (g *GrpcExams) ListEnrichedAnswers(ctx context.Context, attemptID domain.AttemptID) ([]ports.UpstreamEnrichedAnswer, error) {
	resp, err := g.attempts.ListEnrichedAnswers(forwardAuth(ctx), &examspb.ListEnrichedAnswersRequest{AttemptId: string(attemptID)})
	if err != nil {
		return nil, err
	}
	out := make([]ports.UpstreamEnrichedAnswer, 0, len(resp.GetItems()))
	for _, a := range resp.GetItems() {
		if a == nil {
			continue
		}
		ea := ports.UpstreamEnrichedAnswer{
			QuestionID:       domain.QuestionID(a.GetQuestionId()),
			QuestionText:     a.GetQuestionText(),
			QuestionCategory: a.GetQuestionCategory(),
			OptionID:         a.GetOptionId(),
			OptionText:       a.GetOptionText(),
			OptionSortOrder:  a.GetOptionSortOrder(),
			OptionIsCorrect:  a.GetOptionIsCorrect(),
		}
		if t := a.GetAnsweredAt(); t != nil {
			ea.AnsweredAt = t.AsTime()
		}
		out = append(out, ea)
	}
	return out, nil
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
		ExamTypeCode: examTypeCodeFromID(e.GetExamTypeId()),
		Code:         e.GetCode(),
		Name:         e.GetName(),
		SchoolID:     domain.SchoolID(e.GetSchoolId()),
		Version:      e.GetVersion(),
	}, nil
}

// examTypeCodeFromID mapea el exam_type_id que devuelve exams_service a un
// code string. Sembramos 3 tipos via 008_seed_exam_types.sql con IDs
// IDENTITY (1, 2, 3), asi que el mapeo es estable mientras nadie altere
// el seed. Si el ID no coincide devolvemos "" para que el caller no asuma.
func examTypeCodeFromID(id int32) string {
	switch id {
	case 1:
		return "vocacional"
	case 2:
		return "simulacro"
	case 3:
		return "habitos"
	}
	return ""
}

// ListActivePublishedExams llama a SearchExams filtrando por active=true y
// published=true. Si schoolID != "", incluye exams cuyo school_id sea ese
// O exams sin school (school_id IS NULL, "exams abiertos").
func (g *GrpcExams) ListActivePublishedExams(ctx context.Context, schoolID domain.SchoolID) ([]ports.UpstreamExam, error) {
	filters := []*examspbcommon.Filter{
		{PropertyName: "active", Operator: examspbcommon.FilterOperator_EQ, Values: []string{"true"}},
		{PropertyName: "published", Operator: examspbcommon.FilterOperator_EQ, Values: []string{"true"}},
	}
	req := &examspbcommon.SearchRequest{
		FilterGroups: []*examspbcommon.FilterGroup{{Filters: filters}},
		Properties:   []string{"exam_type_id", "school_id", "name", "code", "version"},
		Limit:        500,
	}
	resp, err := g.exams.SearchExams(forwardAuth(ctx), req)
	if err != nil {
		return nil, err
	}
	out := make([]ports.UpstreamExam, 0, len(resp.GetResults()))
	for _, r := range resp.GetResults() {
		props := r.GetProperties().AsMap()
		sid := asString(props["school_id"])
		if schoolID != "" {
			// Filtra in-process: deja exams del colegio O exams sin colegio.
			if sid != "" && sid != string(schoolID) {
				continue
			}
		}
		var typeID int32
		if v, ok := props["exam_type_id"].(float64); ok {
			typeID = int32(v)
		}
		out = append(out, ports.UpstreamExam{
			ID:           domain.ExamID(r.GetId()),
			ExamTypeCode: examTypeCodeFromID(typeID),
			Code:         asString(props["code"]),
			Name:         asString(props["name"]),
			SchoolID:     domain.SchoolID(sid),
		})
	}
	return out, nil
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
