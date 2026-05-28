// Package ratelimit provides a small Limiter abstraction backed by
// golang.org/x/time/rate. We expose it as an interface so tests can plug in
// a deterministic fake and so a future Redis-backed limiter can drop in
// without touching call sites.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is the small interface that handlers depend on.
//
// Allow returns (true, 0) when the request can proceed. When (false, retryAfter)
// is returned, the handler should respond with HTTP 429 and a `Retry-After`
// header equal to retryAfter (rounded up to the nearest second).
type Limiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration)
}

// inMemoryLimiter is the default implementation: per-key token buckets kept
// in a map, with a background sweeper that drops entries idle longer than
// idleTTL. The sweeper guarantees we don't leak memory on attacker-style
// keys (e.g. one bucket per source IP).
type inMemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]*entry

	rate  rate.Limit
	burst int

	now func() time.Time

	stop chan struct{}
}

type entry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// idleTTL is how long an unused bucket survives before the sweeper drops it.
const idleTTL = time.Hour

// New returns an in-memory limiter with the given (rate, burst) parameters.
// rate is given as "events per minute" to match the .env configuration
// idiom (RATE_LIMIT_LOGIN_PER_MIN, etc.); convert internally to rate.Limit.
func New(perMinute float64, burst int) Limiter {
	if burst <= 0 {
		burst = 1
	}
	l := &inMemoryLimiter{
		buckets: make(map[string]*entry),
		rate:    rate.Limit(perMinute / 60.0),
		burst:   burst,
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go l.sweep()
	return l
}

// NewPerHour is a convenience for hour-scoped rates (register, password reset).
func NewPerHour(perHour float64, burst int) Limiter {
	if burst <= 0 {
		burst = 1
	}
	l := &inMemoryLimiter{
		buckets: make(map[string]*entry),
		rate:    rate.Limit(perHour / 3600.0),
		burst:   burst,
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go l.sweep()
	return l
}

// Allow takes a token from the per-key bucket. It is safe for concurrent use.
func (l *inMemoryLimiter) Allow(_ context.Context, key string) (bool, time.Duration) {
	l.mu.Lock()
	e, ok := l.buckets[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = e
	}
	e.lastUsed = l.now()
	r := e.limiter.ReserveN(l.now(), 1)
	l.mu.Unlock()

	if !r.OK() {
		// burst exceeds limiter capacity — should not happen with burst≥1.
		return false, time.Second
	}
	delay := r.DelayFrom(l.now())
	if delay <= 0 {
		return true, 0
	}
	// Roll the reservation back so the caller is not charged for a denied
	// attempt; otherwise repeated 429s would extend the cooldown indefinitely.
	r.Cancel()
	return false, delay
}

// Stop terminates the background sweeper. Tests should call this; production
// code typically lets it run for the process lifetime.
func (l *inMemoryLimiter) Stop() {
	close(l.stop)
}

func (l *inMemoryLimiter) sweep() {
	ticker := time.NewTicker(idleTTL / 2)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			cutoff := l.now().Add(-idleTTL)
			l.mu.Lock()
			for k, e := range l.buckets {
				if e.lastUsed.Before(cutoff) {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}
}
