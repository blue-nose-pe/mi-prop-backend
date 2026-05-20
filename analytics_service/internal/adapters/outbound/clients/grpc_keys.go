// Cliente gRPC contra keys_service. Reemplaza al NoopKeys cuando
// KEYS_SERVICE_ADDR está configurado.
//
// Cobertura del puerto ports.KeysClient:
//   - ListKeysByAsesor  -> keys.v1.KeyService/ListByAsesor  (RPC directo)
//   - ListKeysByColegio -> keys.v1.KeyService/ListByColegio (RPC directo)
package clients

import (
	"context"
	"fmt"
	"time"

	"analytics_service/internal/core/domain"
	"analytics_service/internal/core/ports"

	keyspb "keys_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type GrpcKeys struct {
	conn *grpc.ClientConn
	cli  keyspb.KeyServiceClient
}

var _ ports.KeysClient = (*GrpcKeys)(nil)

func NewGrpcKeys(addr string) (*GrpcKeys, error) {
	if addr == "" {
		return nil, fmt.Errorf("keys_service address is empty")
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
		return nil, fmt.Errorf("dial keys_service: %w", err)
	}
	return &GrpcKeys{conn: conn, cli: keyspb.NewKeyServiceClient(conn)}, nil
}

func (g *GrpcKeys) Close() error {
	if g == nil || g.conn == nil {
		return nil
	}
	return g.conn.Close()
}

func (g *GrpcKeys) ListKeysByAsesor(ctx context.Context, asesorID domain.UserID) ([]ports.UpstreamKey, error) {
	resp, err := g.cli.ListByAsesor(forwardAuth(ctx), &keyspb.ListByAsesorRequest{AsesorUserId: string(asesorID)})
	if err != nil {
		return nil, err
	}
	return mapKeys(resp.GetItems()), nil
}

func (g *GrpcKeys) ListKeysByColegio(ctx context.Context, schoolID domain.SchoolID) ([]ports.UpstreamKey, error) {
	resp, err := g.cli.ListByColegio(forwardAuth(ctx), &keyspb.ListByColegioRequest{SchoolId: string(schoolID)})
	if err != nil {
		return nil, err
	}
	return mapKeys(resp.GetItems()), nil
}

func mapKeys(items []*keyspb.Key) []ports.UpstreamKey {
	out := make([]ports.UpstreamKey, 0, len(items))
	for _, k := range items {
		if k == nil {
			continue
		}
		// Mismo caveat que en grpc_exams.GetExam: el proto Key expone
		// exam_type_id (int IDENTITY), no el code estable. Lo mapeamos
		// con la misma tabla del seed 008 (1=vocacional, 2=simulacro,
		// 3=habitos) — sin esto el export XLSX del asesor mostraba la
		// columna "Tipo de examen" vacia para todas sus keys.
		out = append(out, ports.UpstreamKey{
			ID:           k.GetId(),
			Code:         k.GetCode(),
			ExamTypeCode: examTypeCodeFromID(k.GetExamTypeId()),
			SchoolID:     domain.SchoolID(k.GetSchoolId()),
			AsesorUserID: domain.UserID(k.GetAsesorUserId()),
			CurrentUses:  k.GetCurrentUses(),
			MaxUses:      k.GetMaxUses(),
		})
	}
	return out
}
