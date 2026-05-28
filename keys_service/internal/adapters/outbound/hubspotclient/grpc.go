// Package hubspotclient implementa ports.HubspotSyncer via gRPC contra
// hubspot_service. Lo usa el core.command.KeyHandler para replicar cada
// key creada/editada al CRM (paridad con P1: createKey.js etc).
//
// Si HUBSPOT_SERVICE_ADDR esta vacio, el cmd/server inyecta NoopSyncer
// (dev local) para que la app no necesite hubspot_service para arrancar.
package hubspotclient

import (
	"context"
	"fmt"
	"time"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"

	hubspotpb "hubspot_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type Grpc struct {
	conn *grpc.ClientConn
	cli  hubspotpb.HubspotServiceClient
}

var _ ports.HubspotSyncer = (*Grpc)(nil)

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

func (g *Grpc) SyncKey(ctx context.Context, in ports.HubspotSyncKeyInput) error {
	_, err := g.cli.SyncKey(ctx, &hubspotpb.SyncKeyRequest{
		Code:           in.Code,
		ExamTypeCode:   in.ExamTypeCode,
		AsesorUserId:   string(in.AsesorUserID),
		SchoolId:       string(in.SchoolID),
		SchoolName:     in.SchoolName,
		Grade:          in.Grade,
		Section:        in.Section,
		ValidFrom:      in.ValidFrom,
		ValidTo:        in.ValidTo,
		MaxUses:        in.MaxUses,
		AsesorRecordId: in.AsesorRecordID,
		SchoolRecordId: in.SchoolRecordID,
		AsesorEmail:    in.AsesorEmail,
		SchoolIntId:    in.SchoolIntID,
		AsesorIntId:    in.AsesorIntID,
	})
	return err
}

// NoopSyncer — implementacion para dev local cuando hubspot_service no
// esta wireado. Loguea + ignora. cmd/server lo usa cuando
// HUBSPOT_SERVICE_ADDR esta vacio para que keys_service arranque solo.
type NoopSyncer struct{}

var _ ports.HubspotSyncer = (*NoopSyncer)(nil)

func (NoopSyncer) SyncKey(_ context.Context, in ports.HubspotSyncKeyInput) error {
	_ = in
	return nil
}

// Asegurarnos que el cliente compila contra el tipo Key del domain
// (linker check — el cliente no consume Key directamente, lo recibe el
// caller, pero esta linea evita imports "unused" si refactorizamos).
var _ = (*domain.Key)(nil)
