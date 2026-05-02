// Package usersclient implementa ports.UsersServiceCallback.
//
// Hoy: NoOp logueado. Cuando users_service tenga el RPC
// HubspotSync.SetUserHubspotRecordID expuesto, se reemplaza por
// GrpcClient (siguiente archivo en este paquete) que llama al servicio
// vía gRPC. La implementación NoOp permite arrancar dev local sin
// users_service corriendo.
package usersclient

import (
	"context"
	"log"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

type Noop struct{}

var _ ports.UsersServiceCallback = (*Noop)(nil)

func NewNoop() *Noop { return &Noop{} }

func (Noop) SetUserHubspotRecordID(_ context.Context, userID domain.UserID, recordID domain.RecordID) error {
	log.Printf("[users-callback noop] user=%s record=%s", userID, recordID)
	return nil
}
