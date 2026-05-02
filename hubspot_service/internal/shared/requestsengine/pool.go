package requestsengine

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// KeyPool centraliza el rate limiting en Redis. Todas las réplicas (server
// + workers, en cualquier nodo de AKS) coordinan via el mismo Redis.
//
// La atomicidad la garantiza redis_rate via scripts Lua: GET → eval →
// SET+EXPIRE en una sola operación, así dos procesos NUNCA consumen el
// mismo slot.
type KeyPool struct {
	limiter *redis_rate.Limiter
	tokens  []string
	rps     int
	prefix  string

	// rng — local al pool. Inicializado con seed por instancia para que
	// cada réplica tenga un orden distinto desde el primer Acquire.
	rng *rand.Rand
}

// NewKeyPool construye el pool. La conexión Redis ya debe estar abierta.
// Si len(tokens) == 0, devuelve error (sin tokens no hay engine).
func NewKeyPool(rdb *redis.Client, tokens []string, rps int, prefix string) (*KeyPool, error) {
	if len(tokens) == 0 {
		return nil, errors.New("requestsengine: KeyPool requires at least 1 token")
	}
	if rps <= 0 {
		rps = 10
	}
	if prefix == "" {
		prefix = "hs_ratelimit"
	}
	return &KeyPool{
		limiter: redis_rate.NewLimiter(rdb),
		tokens:  tokens,
		rps:     rps,
		prefix:  prefix,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

// Acquire devuelve un token con cupo disponible. Si todas las keys están
// saturadas, espera el menor RetryAfter y reintenta. Respeta el ctx —
// si cancela durante la espera, devuelve ctx.Err().
//
// Algoritmo:
//  1. Permutación aleatoria de los índices (distribuir carga uniforme).
//  2. Para cada índice, consulta redis_rate.Allow:
//       Allowed > 0  → devuelve ese token.
//       Allowed == 0 → guarda RetryAfter, sigue probando.
//  3. Si ninguna pasó, espera min(RetryAfter) y vuelve a 1.
func (p *KeyPool) Acquire(ctx context.Context) (string, error) {
	limit := redis_rate.PerSecond(p.rps)

	for {
		order := p.rng.Perm(len(p.tokens))

		var minWait time.Duration
		minWaitSet := false

		for _, idx := range order {
			tok := p.tokens[idx]
			res, err := p.limiter.Allow(ctx, p.prefix+":"+tok, limit)
			if err != nil {
				// Redis caído / red caída → fail-fast. El worker decide
				// si reintenta como otro fallo de red.
				return "", err
			}
			if res.Allowed > 0 {
				return tok, nil
			}
			// Sin cupo en esta key. Guardamos el RetryAfter más corto.
			if !minWaitSet || res.RetryAfter < minWait {
				minWait = res.RetryAfter
				minWaitSet = true
			}
		}

		// Ninguna key tuvo cupo. Esperar y reintentar.
		if minWait <= 0 {
			minWait = 50 * time.Millisecond // safety
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(minWait):
			// retry
		}
	}
}
