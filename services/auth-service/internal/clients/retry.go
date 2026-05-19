package clients

import (
	"context"
	"fmt"
	"time"
)

func retry(ctx context.Context, attempts int, backoff time.Duration, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := fn(ctx); err != nil {
			lastErr = err
			if attempt == attempts-1 {
				break
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("downstream call failed after %d attempts: %w", attempts, lastErr)
}
