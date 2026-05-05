// Package permsclient — cliente que cualquier microservicio (excepto
// users-service) usa para validar permisos contra users-service.
//
// Implementa permmw.Resolver para enchufarse directo al interceptor.
// Cachea la lista de codes del user en Redis con TTL corto (30s por
// defecto), de modo que las revocaciones del lado users-service se
// propagan como máximo en ~TTL. Si Redis falla pero users-service
// responde, sigue funcionando; si users-service no responde, devuelve
// error y permmw responde 503.
//
// Este archivo se COPIA en cada microservicio (autonomía total — ningún
// servicio importa código de otro). En cada copia, ajustar el import
// `userspb` al path local del servicio.
//
// Uso típico (en main.go del consumidor):
//
//   conn, _ := grpc.NewClient(cfg.UsersServiceAddr, ...)
//   client := permsclient.New(conn, redisClient, 30*time.Second)
//   grpc.NewServer(grpc.ChainUnaryInterceptor(
//       jwtmw.UnaryServerInterceptor(verifier, skipFn),
//       permmw.UnaryServerInterceptor(client, methodToCodeMap),
//   ))
package permsclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	userspb "users_service/proto/gen"
)

// Client implementa permmw.Resolver consultando users-service vía gRPC,
// con cache Redis intermedio.
type Client struct {
	rpc   userspb.UserServiceClient
	cache *redis.Client // opcional; si es nil, va siempre por gRPC
	ttl   time.Duration
}

// New: la conn ya debe estar abierta (pasarla desde main.go con keepalive).
// Si redisClient es nil, deshabilita el cache y va por gRPC en cada call.
func New(conn *grpc.ClientConn, redisClient *redis.Client, ttl time.Duration) *Client {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Client{
		rpc:   userspb.NewUserServiceClient(conn),
		cache: redisClient,
		ttl:   ttl,
	}
}

// Has implementa permmw.Resolver.
//
// Algoritmo:
//  1. Intenta leer la lista de codes del cache: clave "perms:<userID>".
//  2. Cache hit: chequea pertenencia local. Si la lista contiene "*"
//     (sentinel de superadmin) o el code, devuelve true.
//  3. Cache miss: llama a users-service ListUserPermissions(userID).
//     Cachea el resultado con TTL `c.ttl`. Chequea pertenencia.
func (c *Client) Has(ctx context.Context, userID, code string) (bool, error) {
	codes, err := c.codesForUser(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, x := range codes {
		if x == "*" || x == code {
			return true, nil
		}
	}
	return false, nil
}

// Invalidate borra la entry cacheada del user. Lo invocan los handlers
// de users-service cuando ese user pierde un permission_group, o cuando
// un admin lo desactiva. Como cada microservicio tiene su propio Redis
// db, la invalidación es local — para invalidar en TODOS los servicios
// se necesitaría un pub/sub adicional (acepto esa lag de hasta TTL en
// servicios "otros" hasta que se modele bien).
func (c *Client) Invalidate(ctx context.Context, userID string) error {
	if c.cache == nil {
		return nil
	}
	return c.cache.Del(ctx, cacheKey(userID)).Err()
}

func (c *Client) codesForUser(ctx context.Context, userID string) ([]string, error) {
	if c.cache != nil {
		key := cacheKey(userID)
		raw, err := c.cache.Get(ctx, key).Bytes()
		if err == nil {
			var codes []string
			if jerr := json.Unmarshal(raw, &codes); jerr == nil {
				return codes, nil
			}
			// Cache corrupto → invalidar y caer a gRPC.
			_ = c.cache.Del(ctx, key).Err()
		} else if !errors.Is(err, redis.Nil) {
			// Redis caído: NO fail-closed acá — preferimos ir por gRPC
			// directo en lugar de tirar todas las requests cuando Redis
			// se cae. users-service sigue siendo la autoridad.
			// (logueable, pero no bloqueante)
		}
	}
	// Cache miss o sin cache → gRPC.
	resp, err := c.rpc.ListUserPermissions(ctx, &userspb.ListPermsRequest{UserId: userID})
	if err != nil {
		return nil, err
	}
	codes := resp.GetCodes()
	if c.cache != nil {
		if buf, jerr := json.Marshal(codes); jerr == nil {
			_ = c.cache.Set(ctx, cacheKey(userID), buf, c.ttl).Err()
		}
	}
	return codes, nil
}

func cacheKey(userID string) string {
	return "perms:" + strings.TrimSpace(userID)
}
