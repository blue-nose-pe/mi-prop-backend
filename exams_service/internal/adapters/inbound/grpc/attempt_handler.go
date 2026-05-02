package grpchandler

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/apperr"

	pb "exams_service/proto/gen"
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
	var keyID domain.KeyID
	if req.GetKeyCode() != "" {
		validation, err := h.keys.Validate(ctx, req.GetKeyCode(), "" /* el handler no conoce el exam_type aún; keys_service lo deduce o no aplica */)
		if err != nil {
			return nil, apperr.ToGRPC(ctx, err)
		}
		if !validation.OK {
			return nil, apperr.ToGRPC(ctx, domain.ErrExamClosed)
		}
		keyID = validation.KeyID
	}

	att, err := h.cmds.Start(ctx, ports.StartAttemptInput{
		ExamID: domain.ExamID(req.GetExamId()),
		UserID: domain.UserID(req.GetUserId()),
		KeyID:  keyID,
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}

	if keyID != "" {
		// best-effort: si el contador falla, el attempt sigue siendo válido.
		_ = h.keys.IncrementUsage(ctx, keyID, att.ID, att.UserID)
	}

	return &pb.AttemptResponse{Attempt: toProtoAttempt(att)}, nil
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

func toAttemptsResponse(items []domain.ExamAttempt) *pb.ListAttemptsResponse {
	out := &pb.ListAttemptsResponse{Items: make([]*pb.Attempt, 0, len(items))}
	for i := range items {
		out.Items = append(out.Items, toProtoAttempt(&items[i]))
	}
	return out
}
