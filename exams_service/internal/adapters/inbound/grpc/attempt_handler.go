package grpchandler

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/apperr"

	pb "exams_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// AttemptHandler también orquesta la validación de keys: si el request
// trae key_code, se delega a keys_service vía KeysClient antes de Start.
// Una vez creado el attempt, registra el uso de la key.
type AttemptHandler struct {
	pb.UnimplementedAttemptServiceServer
	cmds ports.AttemptCommands
	qrys ports.AttemptQueries
	keys ports.KeysClient
}

func NewAttemptHandler(
	cmds ports.AttemptCommands,
	qrys ports.AttemptQueries,
	keys ports.KeysClient,
) *AttemptHandler {
	return &AttemptHandler{cmds: cmds, qrys: qrys, keys: keys}
}

func (h *AttemptHandler) StartAttempt(ctx context.Context, req *pb.StartAttemptRequest) (*pb.AttemptResponse, error) {
	examID := domain.ExamID(req.GetExamId())
	userID := domain.UserID(req.GetUserId())

	var keyID domain.KeyID
	var expectedExamTypeID int32
	if req.GetKeyCode() != "" {
		validation, err := h.keys.Validate(ctx, req.GetKeyCode(), "")
		if err != nil {
			return nil, apperr.ToGRPC(ctx, err)
		}
		if !validation.OK {
			return nil, apperr.ToGRPC(ctx, domain.ErrExamClosed)
		}
		keyID = validation.KeyID
		expectedExamTypeID = validation.ExamTypeID
	}

	// Bug #1 fix: el IncrementUsage de keys_service es la unica gate
	// atomica contra el aforo (UPDATE con guard de current_uses<max_uses
	// en una sola operacion SQL). Para que sea efectiva, hay que llamarla
	// ANTES de crear el attempt y rechazar si devuelve error. El codigo
	// anterior la llamaba despues con `_ = ...`, lo que permitia que mas
	// alumnos de los del aforo crearan attempts validos (cada uno sin
	// contarse en current_uses).
	//
	// Idempotencia: si el user ya tiene un attempt activo para este exam,
	// el core Start devuelve Reused=true y NO consumimos un uso adicional
	// (el primer Start ya lo conto). Esto cierra el Bug #2 (refresh).
	if keyID != "" {
		// Checkeo pre-emptivo: si el user ya tenia un attempt activo de
		// este exam, no incrementar (ya contamos antes).
		// Reusamos FindActiveByExamUser via core para evitar duplicar query.
		// Para no hacer dos round-trips a la BD, simplemente delegamos al
		// core y, si dice Reused=true, no incrementamos. Como
		// IncrementUsage tiene que ser ANTES del Save, hacemos el check
		// de reuso aqui mismo.
		existing, err := h.qrys.GetActiveByExamUser(ctx, examID, userID)
		if err != nil {
			return nil, apperr.ToGRPC(ctx, err)
		}
		if existing == nil {
			// Attempt nuevo → consumir uso atomico de la key.
			if err := h.keys.IncrementUsage(ctx, keyID, "", userID); err != nil {
				// 0 filas afectadas → aforo lleno o key invalida.
				return nil, apperr.ToGRPC(ctx, err)
			}
		}
	}

	res, err := h.cmds.Start(ctx, ports.StartAttemptInput{
		ExamID:             examID,
		UserID:             userID,
		KeyID:              keyID,
		ExpectedExamTypeID: expectedExamTypeID,
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}

	return &pb.AttemptResponse{Attempt: toProtoAttempt(res.Attempt)}, nil
}

func (h *AttemptHandler) Answer(ctx context.Context, req *pb.AnswerRequest) (*pb.EmptyResponse, error) {
	err := h.cmds.Answer(ctx, ports.AnswerInput{
		AttemptID:  domain.AttemptID(req.GetAttemptId()),
		QuestionID: domain.QuestionID(req.GetQuestionId()),
		OptionID:   domain.OptionID(req.GetOptionId()),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *AttemptHandler) FinishAttempt(ctx context.Context, req *pb.FinishAttemptRequest) (*pb.AttemptResponse, error) {
	att, err := h.cmds.Finish(ctx, domain.AttemptID(req.GetAttemptId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.AttemptResponse{Attempt: toProtoAttempt(att)}, nil
}

func (h *AttemptHandler) GetAttempt(ctx context.Context, req *pb.GetAttemptRequest) (*pb.AttemptResponse, error) {
	att, err := h.qrys.Get(ctx, domain.AttemptID(req.GetAttemptId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.AttemptResponse{Attempt: toProtoAttempt(att)}, nil
}

func (h *AttemptHandler) ListByUser(ctx context.Context, req *pb.ListAttemptsByUserRequest) (*pb.ListAttemptsResponse, error) {
	items, err := h.qrys.ListByUser(ctx, domain.UserID(req.GetUserId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return toAttemptsResponse(items), nil
}

func (h *AttemptHandler) ListByExam(ctx context.Context, req *pb.ListAttemptsByExamRequest) (*pb.ListAttemptsResponse, error) {
	items, err := h.qrys.ListByExam(ctx, domain.ExamID(req.GetExamId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return toAttemptsResponse(items), nil
}

func (h *AttemptHandler) ListByColegio(ctx context.Context, req *pb.ListAttemptsByColegioRequest) (*pb.ListAttemptsResponse, error) {
	items, err := h.qrys.ListByColegio(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return toAttemptsResponse(items), nil
}

func (h *AttemptHandler) ListByKey(ctx context.Context, req *pb.ListAttemptsByKeyRequest) (*pb.ListAttemptsResponse, error) {
	items, err := h.qrys.ListByKey(ctx, domain.KeyID(req.GetKeyId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return toAttemptsResponse(items), nil
}

func (h *AttemptHandler) ListEnrichedAnswers(ctx context.Context, req *pb.ListEnrichedAnswersRequest) (*pb.ListEnrichedAnswersResponse, error) {
	items, err := h.qrys.ListEnrichedAnswers(ctx, domain.AttemptID(req.GetAttemptId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ListEnrichedAnswersResponse{Items: make([]*pb.EnrichedAnswer, 0, len(items))}
	for i := range items {
		a := &items[i]
		pa := &pb.EnrichedAnswer{
			QuestionId:       string(a.QuestionID),
			QuestionText:     a.QuestionText,
			QuestionCategory: a.QuestionCategory,
			OptionId:         string(a.OptionID),
			OptionText:       a.OptionText,
			OptionSortOrder:  a.OptionSortOrder,
			OptionIsCorrect:  a.OptionIsCorrect,
			AnsweredAt:       timestamppb.New(a.AnsweredAt),
		}
		out.Items = append(out.Items, pa)
	}
	return out, nil
}

func toAttemptsResponse(items []domain.ExamAttempt) *pb.ListAttemptsResponse {
	out := &pb.ListAttemptsResponse{Items: make([]*pb.Attempt, 0, len(items))}
	for i := range items {
		out.Items = append(out.Items, toProtoAttempt(&items[i]))
	}
	return out
}
