// Package otpsender implementa ports.OTPSender dialeando
// hubspot_service.HubspotService/SendOTP. El servicio HubSpot dispara
// el webhook al portal HubSpot que envía el email con el código.
package otpsender

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

var _ ports.OTPSender = (*Grpc)(nil)

// NewGrpc dialea hubspot_service.HubspotService/SendOTP en el address dado
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

// Client expone el cliente gRPC subyacente para reutilizar la conexion
// desde otro adapter (el SMTP, que dispara SyncStudentContact best-effort
// tras enviar el OTP sin crear una segunda conexion al hubspot_service).
func (g *Grpc) Client() hubspotpb.HubspotServiceClient {
	if g == nil {
		return nil
	}
	return g.cli
}

// Send dispara el SendOTP gRPC. Reenvía x-correlation-id si está en el
// context inbound. NO reenvía Authorization: el users_service llama al
// hubspot_service con el bearer del propio caller (estudiante intentando
// loguear NO TIENE bearer, así que esto solo funcionaría si hubspot_service
// dejara SendOTP en su skip-list — alternativamente se inyecta un service
// token. Por ahora se confía en el skip-list/network policy).
func (g *Grpc) Send(ctx context.Context, email, plainOTP string) error {
	return g.SendWithContact(ctx, ports.OTPSendInput{Email: email, PlainOTP: plainOTP})
}

// SendWithContact es la version completa: forwardea nombre/apellido/dni/
// phone para que hubspot_service upsertee el contacto con info completa.
// Cuando vienen vacios, el lado hubspot_service ignora la prop.
func (g *Grpc) SendWithContact(ctx context.Context, in ports.OTPSendInput) error {
	out := ctx
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("x-correlation-id"); len(v) > 0 {
			out = metadata.AppendToOutgoingContext(out, "x-correlation-id", v[0])
		}
	}
	_, err := g.cli.SendOTP(out, &hubspotpb.SendOTPRequest{
		Email:          in.Email,
		Otp:            in.PlainOTP,
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		DocumentNumber: in.DocumentNumber,
		Phone:          in.Phone,
		SchoolId:       in.SchoolID,
		SchoolIntId:    in.SchoolIntID,
		SchoolRecordId: in.SchoolRecordID,
	})
	return err
}
