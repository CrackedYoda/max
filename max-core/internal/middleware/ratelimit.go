package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/shared"
)

// RateLimit is a per-IP fixed-window limiter. It is process-local, which is
// fine for a single instance; move it to Redis before running more than one.
func RateLimit(limit int, window time.Duration) gin.HandlerFunc {
	type bucket struct {
		count int
		reset time.Time
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*bucket{}
		sweep   = time.Now().Add(window)
	)

	return func(c *gin.Context) {
		now := time.Now()
		key := c.ClientIP()

		mu.Lock()
		if now.After(sweep) {
			for k, b := range buckets {
				if now.After(b.reset) {
					delete(buckets, k)
				}
			}
			sweep = now.Add(window)
		}
		b, ok := buckets[key]
		if !ok || now.After(b.reset) {
			b = &bucket{reset: now.Add(window)}
			buckets[key] = b
		}
		b.count++
		count, reset := b.count, b.reset
		mu.Unlock()

		if count > limit {
			c.Header("Retry-After", strconv.Itoa(int(time.Until(reset).Seconds())+1))
			shared.Fail(c, shared.ErrRateLimited)
			return
		}
		c.Next()
	}
}
