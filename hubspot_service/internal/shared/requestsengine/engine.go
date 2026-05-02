package requestsengine

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Engine es el objeto principal. Lo construye main.go una vez, lo
// consumen handlers y workers (asynq, gRPC handlers) llamando Do().
//
//   - Workers internos atienden la cola tasks de forma round-robin.
//   - El KeyPool coordina el rate limit GLOBAL via Redis.
//   - Close() es idempotente y graceful (espera a los workers en vuelo).
type Engine struct {
	cfg      Config
	client   *http.Client
	pool     *KeyPool
	tasks    chan Task
	wg       sync.WaitGroup
	stopOnce sync.Once

	// rng — para jitter del backoff (independiente del rng del pool).
	jitterMu  sync.Mutex
	jitterRng *rand.Rand
}

// New construye el engine y lanza los workers. Devuelve error si:
//   - Tokens está vacío.
//   - No puede conectar a Redis (ping falla).
//
// El caller HACE defer engine.Close() para drenar workers en vuelo.
func New(cfg Config) (*Engine, error) {
	cfg.applyDefaults()
	if len(cfg.Tokens) == 0 {
		return nil, errors.New("requestsengine: at least 1 token required")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}

	pool, err := NewKeyPool(rdb, cfg.Tokens, cfg.RPS, cfg.KeyPrefix)
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}

	e := &Engine{
		cfg:       cfg,
		client:    &http.Client{Timeout: cfg.HTTPTimeout},
		pool:      pool,
		tasks:     make(chan Task, cfg.QueueSize),
		jitterRng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for i := 0; i < cfg.Workers; i++ {
		e.wg.Add(1)
		go e.worker()
	}
	return e, nil
}

// Do envía una request a través del engine y devuelve la respuesta.
// Es síncrono — el caller bloquea hasta recibir Result.
//
// Si la cola está llena y ctx expira, devuelve ctx.Err() sin encolar.
// Si ctx expira mientras un worker procesa, el worker cancela el HTTP
// request via NewRequestWithContext (no se bloquea).
func (e *Engine) Do(
	ctx context.Context,
	id, method, url string,
	body []byte,
	headers map[string]string,
) (int, []byte, http.Header, error) {
	t := Task{
		ID:      id,
		Method:  method,
		URL:     url,
		Body:    body,
		Headers: headers,
		respC:   make(chan Result, 1),
	}

	// Encolar respetando ctx (si la cola está llena, espera).
	select {
	case e.tasks <- t:
	case <-ctx.Done():
		return 0, nil, nil, ctx.Err()
	}

	// Esperar respuesta del worker.
	select {
	case res := <-t.respC:
		return res.StatusCode, res.Body, res.Header, res.Err
	case <-ctx.Done():
		return 0, nil, nil, ctx.Err()
	}
}

// Close drena los workers en vuelo. Idempotente.
//
//   - sync.Once: cerrar un canal ya cerrado es panic. Once garantiza
//     que aunque Close se llame N veces concurrentes, el cuerpo corre
//     una sola.
//   - sync.WaitGroup: sin Wait el proceso podría terminar a la mitad
//     de un request.
func (e *Engine) Close() {
	e.stopOnce.Do(func() {
		close(e.tasks)
		e.wg.Wait()
	})
}

// worker — loop principal. Sale cuando el canal `tasks` se cierra.
func (e *Engine) worker() {
	defer e.wg.Done()
	for t := range e.tasks {
		t.respC <- e.process(t)
	}
}

// process intenta `MaxRetries+1` veces. Para cada intento:
//  1. Acquire un token con cupo (espera en Redis si saturado).
//  2. Construye y envía la request HTTP.
//  3. Si 2xx → devuelve.
//     Si 429 o 5xx → respeta Retry-After header o backoff exponencial,
//     reintenta.
//     Si otro 4xx (validación, auth, not found) → devuelve sin reintentar
//     (errores de cliente no se arreglan con tiempo).
//     Si error de red → backoff y reintenta.
func (e *Engine) process(t Task) Result {
	ctx := context.Background()

	var lastErr error
	for attempt := 0; attempt <= e.cfg.MaxRetries; attempt++ {
		token, err := e.pool.Acquire(ctx)
		if err != nil {
			lastErr = err
			e.sleepWithBackoff(attempt)
			continue
		}

		req, err := buildRequest(ctx, token, t)
		if err != nil {
			// Error de construcción (URL inválida, etc.) — no reintenta.
			return Result{ID: t.ID, Err: err}
		}

		resp, err := e.client.Do(req)
		if err != nil {
			lastErr = err
			e.sleepWithBackoff(attempt)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		// 2xx → éxito.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return Result{ID: t.ID, StatusCode: resp.StatusCode, Body: body, Header: resp.Header}
		}

		// 429 o 5xx → reintentar.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = errStatus(resp.StatusCode, body)
			e.retryAfterOrBackoff(resp, attempt)
			continue
		}

		// Otro 4xx → devolver tal cual (no reintentamos auth/validación).
		return Result{ID: t.ID, StatusCode: resp.StatusCode, Body: body, Header: resp.Header}
	}

	// Se agotaron los intentos.
	return Result{ID: t.ID, Err: lastErr}
}

// errStatus envuelve un código no-2xx como error. El caller que mira
// .Err puede no querer parsearlo — para ese caso le dejamos también
// StatusCode y Body en Result. Acá solo se usa para `lastErr` interno.
func errStatus(code int, body []byte) error {
	return &statusError{code: code, body: string(body)}
}

type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	return "upstream returned " + strconv.Itoa(e.code) + ": " + e.body
}

// sleepWithBackoff: 2^attempt * BaseBackoff con ±20% jitter.
func (e *Engine) sleepWithBackoff(attempt int) {
	base := e.cfg.BaseBackoff << attempt // attempt 0 → base, 1 → 2*base, 2 → 4*base
	if base <= 0 {
		base = e.cfg.BaseBackoff
	}
	e.jitterMu.Lock()
	jitter := time.Duration(e.jitterRng.Float64()*0.4*float64(base)) - time.Duration(0.2*float64(base))
	e.jitterMu.Unlock()
	time.Sleep(base + jitter)
}

// retryAfterOrBackoff: si HubSpot envía Retry-After (segundos o fecha
// HTTP), lo respeta. Sino, backoff exponencial con jitter.
func (e *Engine) retryAfterOrBackoff(resp *http.Response, attempt int) {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			time.Sleep(time.Duration(secs) * time.Second)
			return
		}
		if t, err := http.ParseTime(v); err == nil {
			d := time.Until(t)
			if d > 0 {
				time.Sleep(d)
				return
			}
		}
	}
	e.sleepWithBackoff(attempt)
}
