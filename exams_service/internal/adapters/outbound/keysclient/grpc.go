// Cliente gRPC contra keys_service. Reemplaza al Noop cuando
// KEYS_SERVICE_ADDR está configurado.
package keysclient

import (
	"context"
	"fmt"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"

	keyspb "keys_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"time"
)

type Grpc struct {
	conn *grpc.ClientConn
	cli  keyspb.KeyServiceClient
}

var _ ports.KeysClient = (*Grpc)(nil)

func NewGrpc(addr string) (*Grpc, error) {
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
	return &Grpc{conn: conn, cli: keyspb.NewKeyServiceClient(conn)}, nil
}

func (g *Grpc) Close() error {
	if g == nil || g.conn == nil {
		return nil
	}
	return g.conn.Close()
}

func (g *Grpc) Validate(ctx context.Context, code, _ string) (*ports.KeyValidation, error) {
	resp, err := g.cli.ValidateKey(forwardCorrelation(ctx), &keyspb.ValidateKeyRequest{Code: code})
	if err != nil {
		return nil, err
	}
	k := resp.GetKey()
	if k == nil {
		return &ports.KeyValidation{OK: false, Reason: "key not found"}, nil
	}
	v := &ports.KeyValidation{
		KeyID:    domain.KeyID(k.GetId()),
		SchoolID: domain.SchoolID(k.GetSchoolId()),
		OK:       k.GetActive(),
	}
	if !k.GetActive() {
		v.Reason = "key inactive"
	}
	if t := k.GetValidTo(); t != nil {
		expires := t.AsTime()
		v.ExpiresAt = &expires
	}
	return v, nil
}

func (g *Grpc) IncrementUsage(ctx context.Context, keyID domain.KeyID, attemptID domain.AttemptID, userID domain.UserID) error {
	_, err := g.cli.IncrementUsage(forwardCorrelation(ctx), &keyspb.IncrementUsageRequest{
		KeyId:     string(keyID),
		AttemptId: string(attemptID),
		UserId:    string(userID),
	})
	return err
}

// Propaga del context inbound al outbound:
//   - x-correlation-id (trazabilidad)
//   - authorization (Bearer JWT) — keys_service tiene jwtmw obligatorio
//     fuera de health/reflection, así que sin reenviar el header el
//     ValidateKey/IncrementUsage muere con Unauthenticated.
func forwardCorrelation(ctx context.Context) context.Context {
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
