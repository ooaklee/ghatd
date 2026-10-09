package browsersecurity

import (
	"context"
	"sync"
	"time"
)

// Limiter is the injectable transport admission port. A positive duration asks
// the caller to refuse admission and return a bounded retry delay.
type Limiter interface {
	Allow(context.Context, string) (time.Duration, error)
}

// WindowLimiter bounds concurrent process-local windows to 10,000 identities.
// Hosts needing cross-instance admission must supply a distributed Limiter.
type WindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	buckets map[string]rateBucket
}
type rateBucket struct {
	count int
	reset time.Time
}

// NewWindowLimiter validates limits without starting timers or goroutines.
func NewWindowLimiter(limit int, window time.Duration, now func() time.Time) (*WindowLimiter, error) {
	if limit < 1 || limit > 10000 || window < time.Second || window > 24*time.Hour {
		return nil, ErrConfigurationInvalid
	}
	if now == nil {
		now = time.Now
	}
	return &WindowLimiter{limit: limit, window: window, now: now, buckets: map[string]rateBucket{}}, nil
}

func (l *WindowLimiter) Allow(ctx context.Context, key string) (time.Duration, error) {
	if l == nil || ctx == nil {
		return 0, ErrInvalidRequest
	}
	if l.now == nil || l.buckets == nil || l.limit < 1 || l.window < time.Second {
		return 0, ErrConfigurationInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if key == "" || len(key) > 512 {
		return 0, ErrInvalidRequest
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	bucket, exists := l.buckets[key]
	if !exists || !now.Before(bucket.reset) {
		if !exists && len(l.buckets) >= 10000 {
			for identity, old := range l.buckets {
				if !now.Before(old.reset) {
					delete(l.buckets, identity)
				}
			}
			if len(l.buckets) >= 10000 {
				return l.window, nil
			}
		}
		bucket = rateBucket{reset: now.Add(l.window)}
	}
	if bucket.count >= l.limit {
		return bucket.reset.Sub(now), nil
	}
	bucket.count++
	l.buckets[key] = bucket
	return 0, nil
}
