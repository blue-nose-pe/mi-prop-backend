package search

import (
	"fmt"
	"time"

	commonpb "users_service/proto/gen/common"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RequestFromProto convierte el mensaje gRPC al modelo de dominio (Go puro).
// Toda logica de SQL vive aguas abajo.
func RequestFromProto(p *commonpb.SearchRequest) Request {
	if p == nil {
		return Request{}
	}
	r := Request{
		Properties: p.GetProperties(),
		Limit:      int(p.GetLimit()),
		After:      int(p.GetAfter()),
	}
	for _, g := range p.GetFilterGroups() {
		group := FilterGroup{}
		for _, f := range g.GetFilters() {
			group.Filters = append(group.Filters, Filter{
				Property: f.GetPropertyName(),
				Operator: operatorFromPB(f.GetOperator()),
				Values:   f.GetValues(),
			})
		}
		r.FilterGroups = append(r.FilterGroups, group)
	}
	for _, s := range p.GetSorts() {
		dir := Asc
		if s.GetDirection() == commonpb.SortDirection_DESC {
			dir = Desc
		}
		r.Sorts = append(r.Sorts, Sort{
			Property:  s.GetPropertyName(),
			Direction: dir,
		})
	}
	return r
}

func operatorFromPB(op commonpb.FilterOperator) Operator {
	switch op {
	case commonpb.FilterOperator_EQ:
		return OpEQ
	case commonpb.FilterOperator_NEQ:
		return OpNEQ
	case commonpb.FilterOperator_LT:
		return OpLT
	case commonpb.FilterOperator_LTE:
		return OpLTE
	case commonpb.FilterOperator_GT:
		return OpGT
	case commonpb.FilterOperator_GTE:
		return OpGTE
	case commonpb.FilterOperator_IN:
		return OpIN
	case commonpb.FilterOperator_NOT_IN:
		return OpNotIN
	case commonpb.FilterOperator_CONTAINS:
		return OpContains
	case commonpb.FilterOperator_STARTS_WITH:
		return OpStartsWith
	case commonpb.FilterOperator_HAS_PROPERTY:
		return OpHasProperty
	case commonpb.FilterOperator_NOT_HAS_PROPERTY:
		return OpNotHasProperty
	case commonpb.FilterOperator_BETWEEN:
		return OpBetween
	}
	return ""
}

// ResponseToProto serializa al formato gRPC. Las properties (map dinamico)
// viajan como google.protobuf.Struct, que solo soporta valores JSON-ish:
// por eso time.Time se stringea a RFC3339Nano (igual que hace HubSpot).
func ResponseToProto(r *Response) (*commonpb.SearchResponse, error) {
	resp := &commonpb.SearchResponse{}
	if r == nil {
		return resp, nil
	}
	resp.Total = r.Total
	for _, res := range r.Results {
		props, err := structpb.NewStruct(toStructValues(res.Properties))
		if err != nil {
			return nil, fmt.Errorf("properties for id %s: %w", res.ID, err)
		}
		var updatedAt *timestamppb.Timestamp
		if !res.UpdatedAt.IsZero() {
			updatedAt = timestamppb.New(res.UpdatedAt)
		}
		resp.Results = append(resp.Results, &commonpb.SearchResult{
			Id:         res.ID,
			Properties: props,
			CreatedAt:  timestamppb.New(res.CreatedAt),
			UpdatedAt:  updatedAt,
			Archived:   res.Archived,
		})
	}
	if r.Paging != nil {
		resp.Paging = &commonpb.Paging{
			NextAfter: r.Paging.NextAfter,
			HasMore:   r.Paging.HasMore,
		}
	}
	return resp, nil
}

func toStructValues(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = toStructScalar(v)
	}
	return out
}

// toStructScalar normaliza un valor para que structpb pueda aceptarlo.
// structpb solo admite: nil, bool, string, float64, []any, map[string]any.
func toStructScalar(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string, bool, float64:
		return x
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case float32:
		return float64(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
