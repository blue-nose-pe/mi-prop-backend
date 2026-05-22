package grpchandler

import (
	"context"
	"time"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"
	"keys_service/internal/shared/apperr"
	"keys_service/internal/shared/search"
	pb "keys_service/proto/gen"
	commonpb "keys_service/proto/gen/common"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type KeyHandler struct {
	pb.UnimplementedKeyServiceServer
	cmds ports.KeyCommands
	qrys ports.KeyQueries
}

func NewKeyHandler(cmds ports.KeyCommands, qrys ports.KeyQueries) *KeyHandler {
	return &KeyHandler{cmds: cmds, qrys: qrys}
}

func (h *KeyHandler) GenerateKey(ctx context.Context, req *pb.GenerateKeyRequest) (*pb.KeyResponse, error) {
	in := ports.GenerateKeyInput{
		Code:         req.GetCode(),
		ExamTypeID:   req.GetExamTypeId(),
		SchoolID:     domain.SchoolID(req.GetSchoolId()),
		AsesorUserID: domain.UserID(req.GetAsesorUserId()),
		Mode:         domain.KeyMode(req.GetMode()),
		Grade:        req.GetGrade(),
		Section:      req.GetSection(),
		MaxUses:      req.GetMaxUses(),
		ExamID:       req.GetExamId(),
	}
	if t := req.GetValidFrom(); t != nil {
		v := t.AsTime()
		in.ValidFrom = &v
	}
	if t := req.GetValidTo(); t != nil {
		v := t.AsTime()
		in.ValidTo = &v
	}
	k, err := h.cmds.Generate(ctx, in)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.KeyResponse{Key: toProto(k)}, nil
}

func (h *KeyHandler) UpdateKey(ctx context.Context, req *pb.UpdateKeyRequest) (*pb.KeyResponse, error) {
	in := ports.UpdateKeyInput{
		ID:      domain.KeyID(req.GetId()),
		Grade:   req.GetGrade(),
		Section: req.GetSection(),
		MaxUses: req.MaxUses, // puntero: nil=no provisto, *=actualizar
	}
	if t := req.GetValidFrom(); t != nil {
		v := t.AsTime()
		in.ValidFrom = &v
	}
	if t := req.GetValidTo(); t != nil {
		v := t.AsTime()
		in.ValidTo = &v
	}
	k, err := h.cmds.Update(ctx, in)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.KeyResponse{Key: toProto(k)}, nil
}

func (h *KeyHandler) DeactivateKey(ctx context.Context, req *pb.DeactivateKeyRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Deactivate(ctx, domain.KeyID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *KeyHandler) ValidateKey(ctx context.Context, req *pb.ValidateKeyRequest) (*pb.KeyResponse, error) {
	k, err := h.cmds.Validate(ctx, req.GetCode())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.KeyResponse{Key: toProto(k)}, nil
}

func (h *KeyHandler) IncrementUsage(ctx context.Context, req *pb.IncrementUsageRequest) (*pb.EmptyResponse, error) {
	err := h.cmds.IncrementUsage(ctx, ports.IncrementUsageInput{
		KeyID:     domain.KeyID(req.GetKeyId()),
		UserID:    domain.UserID(req.GetUserId()),
		AttemptID: req.GetAttemptId(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *KeyHandler) GetKey(ctx context.Context, req *pb.GetKeyRequest) (*pb.KeyResponse, error) {
	k, err := h.qrys.Get(ctx, domain.KeyID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.KeyResponse{Key: toProto(k)}, nil
}

func (h *KeyHandler) GetByCode(ctx context.Context, req *pb.GetByCodeRequest) (*pb.KeyResponse, error) {
	k, err := h.qrys.GetByCode(ctx, req.GetCode())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.KeyResponse{Key: toProto(k)}, nil
}

func (h *KeyHandler) ListByAsesor(ctx context.Context, req *pb.ListByAsesorRequest) (*pb.ListKeysResponse, error) {
	items, err := h.qrys.ListByAsesor(ctx, domain.UserID(req.GetAsesorUserId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return toListResponse(items), nil
}

func (h *KeyHandler) ListByColegio(ctx context.Context, req *pb.ListByColegioRequest) (*pb.ListKeysResponse, error) {
	items, err := h.qrys.ListByColegio(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return toListResponse(items), nil
}

func (h *KeyHandler) ListUserIDsByColegio(ctx context.Context, req *pb.ListByColegioRequest) (*pb.ListUserIDsResponse, error) {
	ids, err := h.qrys.ListUserIDsByColegio(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return &pb.ListUserIDsResponse{UserIds: out}, nil
}

func (h *KeyHandler) SearchKeys(ctx context.Context, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
	resp, err := h.qrys.Search(ctx, search.RequestFromProto(req))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	pbResp, err := search.ResponseToProto(resp)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return pbResp, nil
}

// ----- mappers -----

func toProto(k *domain.Key) *pb.Key {
	if k == nil {
		return nil
	}
	out := &pb.Key{
		Id:           string(k.ID),
		Code:         k.Code,
		ExamTypeId:   k.ExamTypeID,
		SchoolId:     string(k.SchoolID),
		AsesorUserId: string(k.AsesorUserID),
		Mode:         string(k.Mode),
		Grade:        k.Grade,
		Section:      k.Section,
		MaxUses:      k.MaxUses,
		CurrentUses:  k.CurrentUses,
		Active:       k.Active,
		CreatedAt:    timestamppb.New(k.CreatedAt),
		ExamId:       k.ExamID,
	}
	if k.ValidFrom != nil {
		out.ValidFrom = timestamppb.New(*k.ValidFrom)
	}
	if k.ValidTo != nil {
		out.ValidTo = timestamppb.New(*k.ValidTo)
	}
	if k.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*k.UpdatedAt)
	}
	return out
}

func toListResponse(items []domain.Key) *pb.ListKeysResponse {
	out := &pb.ListKeysResponse{Items: make([]*pb.Key, 0, len(items))}
	for i := range items {
		out.Items = append(out.Items, toProto(&items[i]))
	}
	return out
}

// _ uses time so unused-import doesn't trigger if mappers shrink later.
var _ = time.Time{}
