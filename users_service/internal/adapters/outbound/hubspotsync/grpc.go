// Package hubspotsync implementa ports.HubspotSyncer dialeando
// hubspot_service.HubspotService/UpsertContact.
package hubspotsync

import (
	"context"
	"fmt"
	"time"

	"users_service/internal/core/ports"

	hubspotpb "hubspot_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

type Grpc struct {
	conn *grpc.ClientConn
	cli  hubspotpb.HubspotServiceClient
}

var _ ports.HubspotSyncer = (*Grpc)(nil)

// NewGrpc dialea hubspot_service en el address dado
// (ej: "miproposito-hubspot-service:50054").
func NewGrpc(addr string) (*Grpc, error) {
	if addr == "" {
		return nil, fmt.Errorf("hubspot_service address is empty")
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
		return nil, fmt.Errorf("dial hubspot_service: %w", err)
	}
	return &Grpc{conn: conn, cli: hubspotpb.NewHubspotServiceClient(conn)}, nil
}

func (g *Grpc) Close() error {
	if g == nil || g.conn == nil {
		return nil
	}
	return g.conn.Close()
}

// UpsertContact propaga x-correlation-id (no Authorization). El RPC está
// en la skip-list del JWT interceptor del hubspot_service.
func (g *Grpc) UpsertContact(ctx context.Context, c ports.HubspotContact) error {
	out := ctx
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("x-correlation-id"); len(v) > 0 {
			out = metadata.AppendToOutgoingContext(out, "x-correlation-id", v[0])
		}
	}
	_, err := g.cli.UpsertContact(out, &hubspotpb.UpsertContactRequest{
		UserId:    string(c.UserID),
		Email:     c.Email,
		Dni:       c.DNI,
		FirstName: c.FirstName,
		LastName:  c.LastName,
	})
	return err
}
