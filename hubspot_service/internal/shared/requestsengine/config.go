// Package requestsengine — rate-limited HTTP client distribuido para
// llamadas a APIs externas con cuota por API key (HubSpot, Sendgrid,
// HubSpot Automation Webhooks, etc.).
//
// Problema que resuelve:
//   - HubSpot acepta máx 10 rps POR API key.
//   - El hubspot-service corre con N réplicas en AKS (server + worker).
//   - Sin coordinación, las N réplicas saturan la misma key
//     simultáneamente y reciben 429.
//
// Solución: un KeyPool centralizado en Redis. Antes de cada HTTP request,
// el worker pregunta a Redis qué API key tiene cupo (script Lua atómico).
// Si todas saturadas, espera el RetryAfter mínimo y reintenta.
package requestsengine

import "time"

// Config — parametriza el engine. Todos los campos tienen defaults
// razonables (aplicados en New).
type Config struct {
	// Tokens — lista de API keys disponibles. El engine balancea entre
	// ellas con orden aleatorio (rand.Perm) en cada Acquire para
	// distribuir la carga uniformemente.
	Tokens []string

	// RPS — rate limit POR token. HubSpot = 10. Si tenés 5 tokens y
	// RPS=10, capacidad total = 50 rps coordinados entre todos los
	// procesos que comparten el mismo Redis.
	RPS int

	// Workers — goroutines concurrentes por instancia del engine.
	// Demasiados → saturás el rate limit y todos esperan en Redis;
	// pocos → no aprovechás la concurrencia. Default 10.
	Workers int

	// RedisAddr — host:port. En AKS típicamente "redis:6379" o el
	// hostname de Azure Cache for Redis (`<name>.redis.cache.windows.net:6380`).
	RedisAddr string
	// RedisPassword — opcional. En Azure Cache es obligatorio.
	RedisPassword string
	// RedisDB — opcional. Default 0. Si compartís Redis con asynq,
	// usar otra DB para no mezclar keyspaces.
	RedisDB int

	// QueueSize — buffer del canal interno tasks. Backpressure: si el
	// caller llama Do() y la cola está llena, el ctx decide si espera
	// o devuelve error. Default 100.
	QueueSize int

	// MaxRetries — reintentos ante 429/5xx/errores de red. Default 3.
	MaxRetries int

	// BaseBackoff — base del backoff exponencial. Default 500ms.
	// attempt 0 → ~500ms, 1 → ~1s, 2 → ~2s (con ±20% jitter).
	BaseBackoff time.Duration

	// HTTPTimeout — timeout del cliente HTTP. Default 30s.
	HTTPTimeout time.Duration

	// KeyPrefix — prefijo de la clave en Redis. Default "hs_ratelimit".
	// Cambiar si comparten Redis con otro engine.
	KeyPrefix string
}

func (c *Config) applyDefaults() {
	if c.RPS <= 0 {
		c.RPS = 10
	}
	if c.Workers <= 0 {
		c.Workers = 10
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 100
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 500 * time.Millisecond
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = 30 * time.Second
	}
	if c.KeyPrefix == "" {
		c.KeyPrefix = "hs_ratelimit"
	}
}
