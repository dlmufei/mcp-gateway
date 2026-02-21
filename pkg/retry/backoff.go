// Package retry provides retry and backoff utilities.
package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Backoff represents an exponential backoff strategy
type Backoff struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	Jitter     float64 // 0-1, percentage of jitter to add
	attempt    int
}

// NewBackoff creates a new Backoff with default values
func NewBackoff(initial, max time.Duration, multiplier float64) *Backoff {
	return &Backoff{
		Initial:    initial,
		Max:        max,
		Multiplier: multiplier,
		Jitter:     0.1, // 10% jitter by default
	}
}

// Next returns the next backoff duration and increments the attempt counter
func (b *Backoff) Next() time.Duration {
	if b.attempt == 0 {
		b.attempt++
		return b.Initial
	}

	// Calculate exponential backoff
	backoff := float64(b.Initial) * math.Pow(b.Multiplier, float64(b.attempt))
	b.attempt++

	// Apply maximum limit
	if backoff > float64(b.Max) {
		backoff = float64(b.Max)
	}

	// Add jitter
	if b.Jitter > 0 {
		jitter := backoff * b.Jitter * (rand.Float64()*2 - 1) // -jitter to +jitter
		backoff += jitter
	}

	return time.Duration(backoff)
}

// Reset resets the backoff to initial state
func (b *Backoff) Reset() {
	b.attempt = 0
}

// Attempt returns the current attempt number
func (b *Backoff) Attempt() int {
	return b.attempt
}

// Wait waits for the next backoff duration or until context is cancelled
func (b *Backoff) Wait(ctx context.Context) error {
	duration := b.Next()
	select {
	case <-time.After(duration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RetryFunc is a function that can be retried
type RetryFunc func(ctx context.Context) error

// Retry executes fn with exponential backoff until it succeeds or context is cancelled
func Retry(ctx context.Context, b *Backoff, fn RetryFunc) error {
	for {
		err := fn(ctx)
		if err == nil {
			b.Reset()
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if waitErr := b.Wait(ctx); waitErr != nil {
			return waitErr
		}
	}
}
