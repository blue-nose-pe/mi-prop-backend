// Cliente gRPC contra users_service. Reemplaza al NoopUsers cuando
// USERS_SERVICE_ADDR está configurado.
//
// Cobertura del puerto ports.UsersClient:
//   - GetUser                  -> users.v1.UserService/GetUser              (RPC directo)
//   - GetSchool                -> NoOp: users_service NO expone SchoolService
//                                 vía gRPC. Lo hay a nivel core/repo, no
//                                 publicado. Devolver lista/objeto vacío
//                                 hasta que exista el RPC.
//   - ListAssignedColegios     -> NoOp: AssignmentService existe en core
//                                 pero no se expuso como RPC. Idem.
//   - ListEstudiantesEnColegio -> users.v1.UserService/SearchUsers con
//                                 filtro school_id = X (HubSpot-style;
//                                 school_id es Filterable en user_schema).
package clients

import (
	"context"
	"fmt"
	"log"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"

	commonpb "users_service/proto/gen/common"
	userspb "users_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

type GrpcUsers struct {
	conn    *grpc.ClientConn
	cli     userspb.UserServiceClient
	schools userspb.SchoolServiceClient
}

var _ ports.UsersClient = (*GrpcUsers)(nil)

func NewGrpcUsers(addr string) (*GrpcUsers, error) {
	if addr == "" {
		return nil, fmt.Errorf("users_service address is empty")
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
		return nil, fmt.Errorf("dial users_service: %w", err)
	}
	return &GrpcUsers{
		conn:    conn,
		cli:     userspb.NewUserServiceClient(conn),
		schools: userspb.NewSchoolServiceClient(conn),
	}, nil
}

func (g *GrpcUsers) Close() error {
	if g == nil || g.conn == nil {
		return nil
	}
	return g.conn.Close()
}

func (g *GrpcUsers) GetUser(ctx context.Context, id domain.UserID) (*ports.UpstreamUser, error) {
	resp, err := g.cli.GetUser(forwardAuth(ctx), &userspb.GetUserRequest{Id: string(id)})
	if err != nil {
		return nil, err
	}
	u := resp.GetUser()
	if u == nil {
		return nil, fmt.Errorf("users_service returned empty user for id=%s", id)
	}
	return &ports.UpstreamUser{
		ID:             domain.UserID(u.GetId()),
		Email:          u.GetEmail(),
		FirstName:      u.GetFirstName(),
		LastName:       u.GetLastName(),
		DocumentNumber: u.GetDocumentNumber(),
		SchoolID:       domain.SchoolID(u.GetSchoolId()),
		Active:         u.GetActive(),
	}, nil
}

func (g *GrpcUsers) GetSchool(ctx context.Context, id domain.SchoolID) (*ports.UpstreamSchool, error) {
	if id == "" {
		return nil, fmt.Errorf("school id is empty")
	}
	resp, err := g.schools.GetSchool(forwardAuth(ctx), &userspb.GetSchoolRequest{Id: string(id)})
	if err != nil {
		return nil, err
	}
	s := resp.GetSchool()
	if s == nil {
		return nil, fmt.Errorf("users_service returned empty school for id=%s", id)
	}
	return &ports.UpstreamSchool{
		ID:       domain.SchoolID(s.GetId()),
		Name:     s.GetName(),
		City:     s.GetCity(),
		Category: s.GetCategory(),
		Active:   s.GetActive(),
	}, nil
}

// ListSchools devuelve todos los colegios (limit=1000). Si en el futuro
// hay mas de 1000 colegios, paginar aca.
func (g *GrpcUsers) ListSchools(ctx context.Context, activeOnly bool) ([]ports.UpstreamSchool, error) {
	resp, err := g.schools.ListSchools(forwardAuth(ctx), &userspb.ListSchoolsRequest{
		Limit:      1000,
		ActiveOnly: activeOnly,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ports.UpstreamSchool, 0, len(resp.GetItems()))
	for _, s := range resp.GetItems() {
		out = append(out, ports.UpstreamSchool{
			ID:       domain.SchoolID(s.GetId()),
			Name:     s.GetName(),
			City:     s.GetCity(),
			Category: s.GetCategory(),
			Active:   s.GetActive(),
		})
	}
	return out, nil
}

// ListAssignedColegios — NoOp. AssignmentService existe en el core de
// users_service pero no se expuso como RPC. Devolver vacío.
func (g *GrpcUsers) ListAssignedColegios(_ context.Context, asesorID domain.UserID) ([]ports.UpstreamSchool, error) {
	log.Printf("[grpc_users] ListAssignedColegios: NoOp (AssignmentService no publicado) asesor=%s", asesorID)
	return nil, nil
}

// ListEstudiantesEnColegio usa SearchUsers (HubSpot-style) filtrando por
// school_id. user_schema declara school_id como Filterable + Selectable.
//
// Limit: 200 (cap del schema). Si un colegio supera 200 estudiantes habría
// que paginar; para los dashboards actuales sobra.
func (g *GrpcUsers) ListEstudiantesEnColegio(ctx context.Context, schoolID domain.SchoolID) ([]ports.UpstreamUser, error) {
	if schoolID == "" {
		return nil, nil
	}
	req := &commonpb.SearchRequest{
		FilterGroups: []*commonpb.FilterGroup{
			{
				Filters: []*commonpb.Filter{
					{
						PropertyName: "school_id",
						Operator:     commonpb.FilterOperator_EQ,
						Values:       []string{string(schoolID)},
					},
					{
						PropertyName: "active",
						Operator:     commonpb.FilterOperator_EQ,
						Values:       []string{"true"},
					},
				},
			},
		},
		Properties: []string{"email", "first_name", "last_name", "document_number", "school_id", "active"},
		Limit:      200,
	}
	resp, err := g.cli.SearchUsers(forwardAuth(ctx), req)
	if err != nil {
		return nil, err
	}
	out := make([]ports.UpstreamUser, 0, len(resp.GetResults()))
	for _, r := range resp.GetResults() {
		props := r.GetProperties().AsMap()
		out = append(out, ports.UpstreamUser{
			ID:             domain.UserID(r.GetId()),
			Email:          asString(props["email"]),
			FirstName:      asString(props["first_name"]),
			LastName:       asString(props["last_name"]),
			DocumentNumber: asString(props["document_number"]),
			SchoolID:       domain.SchoolID(asString(props["school_id"])),
			Active:         asBool(props["active"]),
		})
	}
	return out, nil
}

// asString cast tolerante: structpb solo carga string|float64|bool|nil|map|list.
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// forwardAuth propaga del context inbound al outbound:
//   - x-correlation-id (trazabilidad)
//   - authorization (Bearer JWT) — los servicios destino tienen jwtmw
//     obligatorio fuera de health/reflection. Sin reenviar el header los
//     RPCs mueren con Unauthenticated.
func forwardAuth(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	out := ctx
	if v := md.Get("x-correlation-id"); len(v) > 0 {
		out = metadata.AppendToOutgoingContext(out, "x-correlation-id", v[0])
	}
	if v := md.Get("authorization"); len(v) > 0 {
		out = metadata.AppendToOutgoingContext(out, "authorization", v[0])
	}
	return out
}
