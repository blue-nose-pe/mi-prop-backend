package grpchandler

import (
	"time"

	"users_service/internal/core/domain"
	pb "users_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Solo mapping domain -> proto. La traduccion de errores vive en
// apperr.ToGRPC — aqui no entra por SRP.

func toProtoUser(u *domain.User) *pb.User {
	if u == nil {
		return nil
	}
	return &pb.User{
		Id:                 string(u.ID),
		Email:              string(u.Email),
		FirstName:          u.FirstName,
		LastName:           u.LastName,
		DocumentNumber:     u.DocumentNumber,
		Phone:              u.Phone,
		SchoolId:           string(u.SchoolID),
		Active:             u.Active,
		LastAccessAt:       tsOrNil(u.LastAccessAt),
		CreatedAt:          timestamppb.New(u.CreatedAt),
		UpdatedAt:          tsOrNil(u.UpdatedAt),
		IsSuperadmin:       u.IsSuperadmin,
		MustChangePassword: u.MustChangePassword,
		IntId:              u.IntID,
	}
}

func tsOrNil(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
