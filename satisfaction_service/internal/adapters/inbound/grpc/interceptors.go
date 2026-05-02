// Package grpchandler — interceptores compartidos por el server.
package grpchandler

import (
	"context"
	"log"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/google/uuid"
)

// RecoveryInterceptor convierte panics en gRPC Internal sin tumbar el server.
func RecoveryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in %s: %v\n%s", info.FullMethod, r, debug.Stack())
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

// CorrelationIDInterceptor inyecta x-correlation-id si no viene del cliente.
func CorrelationIDInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if md == nil {
		md = metadata.New(nil)
	}
	if vs := md.Get("x-correlation-id"); len(vs) == 0 {
		md = md.Copy()
		md.Set("x-correlation-id", uuid.NewString())
	}
	ctx = metadata.NewIncomingContext(ctx, md)
	return handler(ctx, req)
}

// LoggingInterceptor: una línea por RPC con duración + status.
func LoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	cid := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vs := md.Get("x-correlation-id"); len(vs) > 0 {
			cid = vs[0]
		}
	}
	if err != nil {
		log.Printf("[%s] %s %v err=%v", cid, info.FullMethod, time.Since(start), err)
	} else {
		log.Printf("[%s] %s %v ok", cid, info.FullMethod, time.Since(start))
	}
	return resp, err
}
