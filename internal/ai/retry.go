package ai

import (
	"context"
	"time"
)

// Retry calls fn up to maxAttempts times with exponential backoff (delay, 2*delay, 4*delay...).
// Returns the last error if all attempts fail. Returns context error immediately if cancelled.
func Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	var err error
	delay := baseDelay
	for i := 0; i < maxAttempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = fn()
		if err == nil {
			return nil
		}
		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				delay *= 2
			}
		}
	}
	return err
}
