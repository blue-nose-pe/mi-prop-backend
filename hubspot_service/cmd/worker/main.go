// Composition Root del WORKER de hubspot_service.
//
// Imagen Docker separada del server: server (gRPC + webhook inbound) y
// worker (asynq) escalan distinto. KEDA puede levantar más worker pods
// cuando crece la cola sin tocar el server. El server NO procesa jobs.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	"hubspot_service/config"
	asynqadapter "hubspot_service/internal/adapters/outbound/asynq"
	hubspotclient "hubspot_service/internal/adapters/outbound/hubspot"
	"hubspot_service/internal/shared/requestsengine"
)

func main() {
	cfg := config.Load()
	tokens := cfg.EffectiveTokens()
	if len(tokens) == 0 {
		log.Fatalf("HUBSPOT_API_TOKEN(S) is required")
	}

	// Engine: rate limit distribuido en Redis. CADA réplica del worker
	// (y del server) contribuye al MISMO contador global por API key.
	// Eso evita que N workers + M servers saturen la misma key con
	// rps/key * (N+M) y reciban 429 todo el día.
	engine, err := requestsengine.New(requestsengine.Config{
		Tokens:        tokens,
		RPS:           cfg.HubspotRPS,
		Workers:       cfg.HubspotWorkers,
		QueueSize:     cfg.HubspotQueueSize,
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDBRateLimit,
		KeyPrefix:     "hs_ratelimit",
	})
	if err != nil {
		log.Fatalf("requestsengine: %v", err)
	}
	defer engine.Close()

	hs := hubspotclient.New(engine)

	mux := asynqadapter.NewMux(asynqadapter.WorkerDeps{
		HSClient:        hs,
		CustomKeyTypeID: cfg.CustomKeyTypeID,
		CustomColegioID: cfg.CustomColegioTypeID,
		CustomAsesorID:  cfg.CustomAsesorTypeID,
	})

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword},
		asynq.Config{
			Concurrency: cfg.WorkerConcurrency,
			// Backoff exponencial: 1s, 2s, 4s, 8s, 16s, ...
			RetryDelayFunc: asynq.DefaultRetryDelayFunc,
			Logger: stdLogger{},
		},
	)

	// ---------- Run + graceful shutdown ----------
	go func() {
		if err := srv.Run(mux); err != nil {
			log.Fatalf("asynq server: %v", err)
		}
	}()
	log.Printf("hubspot_service worker started (concurrency=%d)", cfg.WorkerConcurrency)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Printf("shutdown signal received, draining workers...")
	done := make(chan struct{})
	go func() { srv.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(cfg.ShutdownWait):
		log.Printf("worker graceful timeout, forcing")
	}
	_ = context.Background() // explicit: si en el futuro queremos pasar ctx
}

// stdLogger adapta log.Printf a la interfaz asynq.Logger.
type stdLogger struct{}

func (stdLogger) Debug(args ...any) { log.Printf("DEBUG %v", args) }
func (stdLogger) Info(args ...any)  { log.Printf("INFO  %v", args) }
func (stdLogger) Warn(args ...any)  { log.Printf("WARN  %v", args) }
func (stdLogger) Error(args ...any) { log.Printf("ERROR %v", args) }
func (stdLogger) Fatal(args ...any) { log.Fatalf("FATAL %v", args) }
