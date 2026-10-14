// Package pace keeps a polling loop inside a request budget and computes
// retry delays, so a viewer that refreshes often never turns into a burst and
// a throttled endpoint is respected instead of hammered.
package pace

import (
	"context"
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// Pacer enforces a minimum spacing between outgoing requests derived from a
// requests-per-minute budget.
type Pacer struct {
	mu      sync.Mutex
	spacing time.Duration
	next    time.Time
}

// NewPacer returns a pacer allowing perMinute requests per minute. A
// non-positive perMinute disables pacing.
func NewPacer(perMinute int) *Pacer {
	p := &Pacer{}
	p.SetPerMinute(perMinute)
	return p
}

// SetPerMinute changes the request budget.
func (p *Pacer) SetPerMinute(perMinute int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if perMinute <= 0 {
		p.spacing = 0
		return
	}
	p.spacing = time.Minute / time.Duration(perMinute)
}

// PerMinute reports the configured budget, or 0 when pacing is disabled.
func (p *Pacer) PerMinute() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.spacing <= 0 {
		return 0
	}
	return int(time.Minute / p.spacing)
}

// Spacing reports the minimum gap enforced between requests.
func (p *Pacer) Spacing() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spacing
}

// Wait blocks until the next request slot is due, or until ctx is done.
func (p *Pacer) Wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	if p.next.Before(now) {
		p.next = now
	}
	wait := p.next.Sub(now)
	p.next = p.next.Add(p.spacing)
	p.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Defer pushes the next request slot at least d into the future, which is how
// a 429 or a server-provided Retry-After is honored.
func (p *Pacer) Defer(d time.Duration) {
	if d <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if until := time.Now().Add(d); until.After(p.next) {
		p.next = until
	}
}

// Backoff derives increasing retry delays from consecutive failures,
// preferring a server-provided Retry-After when one exists.
type Backoff struct {
	Base time.Duration
	Max  time.Duration

	attempt int
}

// NewBackoff returns a backoff starting at base and capped at max.
func NewBackoff(base, max time.Duration) *Backoff {
	if base <= 0 {
		base = time.Second
	}
	if max < base {
		max = base
	}
	return &Backoff{Base: base, Max: max}
}

// Attempt reports how many consecutive failures have been observed.
func (b *Backoff) Attempt() int { return b.attempt }

// Reset clears the failure streak after a success.
func (b *Backoff) Reset() { b.attempt = 0 }

// Delay records one failure and returns how long to wait before retrying. A
// Retry-After hint wins over the computed delay, and a zero hint still counts
// as the server's answer.
func (b *Backoff) Delay(retryAfter time.Duration, hasRetryAfter bool) time.Duration {
	b.attempt++
	if hasRetryAfter {
		if retryAfter < 0 {
			retryAfter = 0
		}
		if retryAfter > b.Max {
			return b.Max
		}
		return retryAfter
	}
	delay := float64(b.Base) * math.Pow(2, float64(b.attempt-1))
	jitter := 0.85 + 0.3*rand.Float64()
	out := time.Duration(delay * jitter)
	if out > b.Max {
		return b.Max
	}
	return out
}
