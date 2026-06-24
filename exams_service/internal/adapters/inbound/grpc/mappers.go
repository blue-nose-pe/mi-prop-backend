package grpchandler

import (
	"exams_service/internal/core/domain"
	pb "exams_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoExam(e *domain.Exam) *pb.Exam {
	if e == nil {
		return nil
	}
	out := &pb.Exam{
		Id:                     string(e.ID),
		ExamTypeId:             e.ExamTypeID,
		SchoolId:               string(e.SchoolID),
		ParentExamId:           string(e.ParentExamID),
		Version:                e.Version,
		Code:                   e.Code,
		Name:                   e.Name,
		StartAt:                timestamppb.New(e.StartAt),
		EndAt:                  timestamppb.New(e.EndAt),
		MaxParticipants:        e.MaxParticipants,
		DefaultPoints:          e.DefaultPoints,
		DefaultPointsIncorrect: e.DefaultPointsIncorrect,
		DefaultPointsBlank:     e.DefaultPointsBlank,
		Published:              e.Published,
		Active:                 e.Active,
		CreatedAt:              timestamppb.New(e.CreatedAt),
	}
	if e.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*e.UpdatedAt)
	}
	return out
}

func toProtoQuestion(q *domain.Question) *pb.Question {
	if q == nil {
		return nil
	}
	out := &pb.Question{
		Id:        string(q.ID),
		Text:      q.Text,
		Category:  q.Category,
		Kind:      string(domain.NormalizeKind(string(q.Kind))),
		Active:    q.Active,
		CreatedAt: timestamppb.New(q.CreatedAt),
	}
	if q.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*q.UpdatedAt)
	}
	return out
}

func toProtoOption(o *domain.QuestionOption) *pb.QuestionOption {
	if o == nil {
		return nil
	}
	return &pb.QuestionOption{
		Id:         string(o.ID),
		QuestionId: string(o.QuestionID),
		Text:       o.Text,
		IsCorrect:  o.IsCorrect,
		SortOrder:  o.SortOrder,
	}
}

func toProtoAttempt(a *domain.ExamAttempt) *pb.Attempt {
	if a == nil {
		return nil
	}
	out := &pb.Attempt{
		Id:        string(a.ID),
		ExamId:    string(a.ExamID),
		UserId:    string(a.UserID),
		KeyId:     string(a.KeyID),
		StartedAt: timestamppb.New(a.StartedAt),
		DurationMinutes: a.DurationMinutes,
	}
	if a.Score != nil {
		out.Score = *a.Score
	}
	if a.MaxScore != nil {
		out.MaxScore = *a.MaxScore
	}
	if a.SubmittedAt != nil {
		out.SubmittedAt = timestamppb.New(*a.SubmittedAt)
	}
	return out
}
