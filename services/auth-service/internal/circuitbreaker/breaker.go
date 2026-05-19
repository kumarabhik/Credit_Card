package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrOpen indicates the circuit breaker is currently open.
var ErrOpen = errors.New("circuit breaker open")

// Breaker is a small in-process circuit breaker for downstream calls.
type Breaker struct {
	mu        sync.Mutex
	name      string
	threshold int
	coolDown  time.Duration
	failures  int
	openUntil time.Time
}

// New returns a new breaker with the provided threshold and cooldown.
func New(name string, threshold int, coolDown time.Duration) *Breaker {
	return &Breaker{
		name:      name,
		threshold: threshold,
		coolDown:  coolDown,
	}
}

// Execute runs a protected downstream call.
func (b *Breaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	b.mu.Lock()
	if time.Now().Before(b.openUntil) {
		openUntil := b.openUntil
		b.mu.Unlock()
		return fmt.Errorf("%w: %s until %s", ErrOpen, b.name, openUntil.Format(time.RFC3339Nano))
	}
	b.mu.Unlock()

	err := fn(ctx)

	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		b.failures = 0
		b.openUntil = time.Time{}
		return nil
	}

	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = time.Now().Add(b.coolDown)
		b.failures = 0
	}
	return err
}
