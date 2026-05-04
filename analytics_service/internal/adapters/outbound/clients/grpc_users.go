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
	conn *grpc.ClientConn
	cli  userspb.UserServiceClient
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
	return &GrpcUsers{conn: conn, cli: userspb.NewUserServiceClient(conn)}, nil
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

// GetSchool — NoOp. users_service no expone SchoolService vía gRPC.
// Cuando se publique, reemplazar por el RPC correspondiente. Por ahora
// devolvemos un placeholder activo para no romper composiciones que
// solo necesitan el ID; el front puede resolver el nombre por otra vía.
func (g *GrpcUsers) GetSchool(_ context.Context, id domain.SchoolID) (*ports.UpstreamSchool, error) {
	log.Printf("[grpc_users] GetSchool: NoOp (SchoolService no publicado en users_service) school_id=%s", id)
	return &ports.UpstreamSchool{ID: id, Active: true}, nil
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
