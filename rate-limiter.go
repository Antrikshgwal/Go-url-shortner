package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func NewRateLimiter(rdb *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{rdb: rdb, limit: limit, window: window}
}

func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, int, error) {
	now := time.Now().UnixMilli()
	windowStart := now - rl.window.Milliseconds()

	pipe := rl.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
	pipe.Expire(ctx, key, rl.window)

	if _, err := pipe.Exec(ctx); err != nil {
		// Redis failed: fail open (allow request)
		return true, 0, err
	}

	count := int(countCmd.Val())
	remaining := rl.limit - count - 1
	if remaining < 0 {
		remaining = 0
	}

	return count < rl.limit, remaining, nil
}

// IP based rate limiting middleware for unauthenticated endpoints
func IPRateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r.RemoteAddr)
			key := fmt.Sprintf("rate:ip:%s:%s", r.URL.Path, ip)

			allowed, remaining, err := limiter.Allow(r.Context(), key)
			if err != nil {
				slog.Warn("Rate limiter error, failing open", "error", err)
			}

			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

			if !allowed {
				w.Header().Set("Retry-After", "60")
				WriteError(w, "Too many requests", http.StatusTooManyRequests)
				slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// User-based rate limiting middleware for authenticated endpoints
func UserRateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            userID, ok := r.Context().Value("user_id").(int64)
            if !ok {
                // No user ID means auth failed upstream, let AuthMiddleware handle it
                next.ServeHTTP(w, r)
                return
            }

            key := fmt.Sprintf("rate:user:%d:%s", userID, r.URL.Path)

            allowed, remaining, err := limiter.Allow(r.Context(), key)
            if err != nil {
                slog.Warn("Rate limiter error, failing open", "error", err)
            }

            w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

            if !allowed {
                w.Header().Set("Retry-After", "60")
                WriteError(w, "Too many requests", http.StatusTooManyRequests)
                slog.Warn("Rate limit exceeded", "user_id", userID, "path", r.URL.Path)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
