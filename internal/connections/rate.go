// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"

	"golang.org/x/time/rate"
)

// semaphore bounds how many requests to one connection run at once (HTTPConfig.Concurrency).
// A buffered channel is enough for this - Go's standard counting-semaphore idiom - and avoids a
// dependency for something this small.
type semaphore chan struct{}

func newSemaphore(n int) semaphore {
	if n < 1 {
		n = 1
	}
	return make(semaphore, n)
}

func (s semaphore) acquire(ctx context.Context) error {
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s semaphore) release() { <-s }

// newLimiter builds a token-bucket limiter from an HTTPConfig's RateLimit, using
// golang.org/x/time/rate - the standard library has no rate limiter of its own, and this is the
// canonical one for exactly this job.
func newLimiter(rl RateLimit) *rate.Limiter {
	rps := rl.RPS
	if rps <= 0 {
		rps = 5
	}
	burst := rl.Burst
	if burst < 1 {
		burst = 10
	}
	return rate.NewLimiter(rate.Limit(rps), burst)
}
