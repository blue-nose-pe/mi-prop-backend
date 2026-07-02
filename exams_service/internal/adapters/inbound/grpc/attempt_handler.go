package grpchandler

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/apperr"

	pb "exams_service/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mapKeysClientError traduce errores gRPC que vienen de keys_service a
// errores de dominio de exams_service. Sin esto, un PermissionDenied de
// keys.ValidateKey (key exhausta/expirada) se propaga como INTERNAL_ERROR
// porque exams_service no lo reconoce como *apperr.Error y cae al
// fallback de ToGRPC.
func mapKeysClientError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.PermissionDenied:
		return domain.ErrKeyNotUsable
	case codes.NotFound:
		return domain.ErrKeyLookupNotFound
	default:
		return err
	}
}

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
	var keySchoolID domain.SchoolID
	var expectedExamTypeID int32
	var maxAttemptsPerUser int32
	if req.GetKeyCode() != "" {
		validation, err := h.keys.Validate(ctx, req.GetKeyCode(), "")
		if err != nil {
			return nil, apperr.ToGRPC(ctx, mapKeysClientError(err))
		}
		if !validation.OK {
			return nil, apperr.ToGRPC(ctx, domain.ErrExamClosed)
		}
		keyID = validation.KeyID
		keySchoolID = validation.SchoolID
		expectedExamTypeID = validation.ExamTypeID
		maxAttemptsPerUser = validation.MaxAttemptsPerUser
	}

	// Aforo (Bug #1): IncrementUsage es la única gate atómica contra el aforo
	// (UPDATE con guard current_uses<max_uses). Idempotencia (Bug #2): si el user
	// ya tiene un attempt activo, Start devuelve Reused y NO consume plaza.
	//
	// Fix audit 2026-07-02: el consumo de aforo se pasa a Start como callback
	// (ConsumeUsage) y Start lo invoca DESPUÉS de sus validaciones (IsOpen/
	// cross-type/masivo) y justo antes de crear el attempt. Antes se llamaba
	// IncrementUsage AQUÍ, ANTES de Start; si Start fallaba (examen cerrado/
	// despublicado, key de otro tipo, candado masivo) la plaza se perdía sin
	// crear attempt y sin compensación posible.
	//
	// Límite por usuario (key.MaxAttemptsPerUser): chequeo read-only ANTES de
	// consumir plaza; si llegó al máximo, rechazamos con MAX_ATTEMPTS_REACHED.
	var consumeUsage func(context.Context) error
	if keyID != "" {
		existing, err := h.qrys.GetActiveByExamUser(ctx, examID, userID)
		if err != nil {
			return nil, apperr.ToGRPC(ctx, err)
		}
		if existing == nil && maxAttemptsPerUser > 0 {
			// Conteo POR KEY — no global del exam. Una key vieja del mismo
			// alumno no debe bloquear keys nuevas que el asesor le entregue.
			submittedCount, err := h.qrys.CountSubmittedByKeyUser(ctx, keyID, userID)
			if err != nil {
				return nil, apperr.ToGRPC(ctx, err)
			}
			if submittedCount >= maxAttemptsPerUser {
				return nil, apperr.ToGRPC(ctx, domain.ErrMaxAttemptsReached)
			}
		}
		kid := keyID
		uid := userID
		consumeUsage = func(c context.Context) error {
			if err := h.keys.IncrementUsage(c, kid, "", uid); err != nil {
				// 0 filas afectadas → aforo lleno o key invalida.
				return mapKeysClientError(err)
			}
			return nil
		}
	}

	res, err := h.cmds.Start(ctx, ports.StartAttemptInput{
		ExamID:             examID,
		UserID:             userID,
		KeyID:              keyID,
		ExpectedExamTypeID: expectedExamTypeID,
		// Masivo = key LAN/masiva (con código pero sin colegio). Start usa
		// esto para bloquear el Examen Nacional (el masivo es solo UCSP).
		KeyIsMasivo:  keyID != "" && keySchoolID == "",
		ConsumeUsage: consumeUsage,
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}

	return &pb.AttemptResponse{Attempt: toProtoAttempt(res.Attempt)}, nil
}

func (h *AttemptHandler) Answer(ctx context.Context, req *pb.AnswerRequest) (*pb.EmptyResponse, error) {
	// Mapeo de option_ids[] del proto a []domain.OptionID. Cuando el
	// alumno usa el campo legacy option_id (single), el command lo
	// degrada a un []OptionID de 1 elemento.
	optIDs := make([]domain.OptionID, 0, len(req.GetOptionIds()))
	for _, id := range req.GetOptionIds() {
		optIDs = append(optIDs, domain.OptionID(id))
	}
	err := h.cmds.Answer(ctx, ports.AnswerInput{
		AttemptID:  domain.AttemptID(req.GetAttemptId()),
		QuestionID: domain.QuestionID(req.GetQuestionId()),
		OptionID:   domain.OptionID(req.GetOptionId()),
		OptionIDs:  optIDs,
		AnswerText: req.GetAnswerText(),
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

// CountSubmittedByKeyUser es RPC PUBLICO (jwtSkip en cmd/server/main.go).
// Lo invoca el gateway en /api/auth/student/lookup-by-key para chequear
// si el alumno ya consumio sus intentos contra esa key — antes de mandarle
// el OTP. No es info-leak grande: el caller necesita key_id (resuelto solo
// con un key_code valido previamente verificado por el gateway).
func (h *AttemptHandler) CountSubmittedByKeyUser(ctx context.Context, req *pb.CountSubmittedByKeyUserRequest) (*pb.CountSubmittedByKeyUserResponse, error) {
	keyID := domain.KeyID(req.GetKeyId())
	userID := domain.UserID(req.GetUserId())
	count, err := h.qrys.CountSubmittedByKeyUser(ctx, keyID, userID)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	lastID := ""
	if count > 0 {
		last, err := h.qrys.GetMostRecentSubmittedByKeyUser(ctx, keyID, userID)
		if err != nil {
			return nil, apperr.ToGRPC(ctx, err)
		}
		if last != nil {
			lastID = string(last.ID)
		}
	}
	return &pb.CountSubmittedByKeyUserResponse{Count: count, LastAttemptId: lastID}, nil
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
			QuestionKind:     string(domain.NormalizeKind(string(a.QuestionKind))),
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
