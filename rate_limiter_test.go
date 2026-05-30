package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(s.Close)
	c := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { c.Close() })
	return s, c
}

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	_, c := newMiniRedis(t)
	rl := NewRateLimiter(c, 3, time.Minute)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, _, err := rl.Allow(ctx, "k1")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !allowed {
			t.Errorf("attempt %d should be allowed", i)
		}
		time.Sleep(2 * time.Millisecond) // avoid same-ms member dedup
	}
	allowed, remaining, _ := rl.Allow(ctx, "k1")
	if allowed {
		t.Error("4th request should be blocked")
	}
	if remaining != 0 {
		t.Errorf("remaining = %d", remaining)
	}
}

func TestRateLimiter_WindowExpires(t *testing.T) {
	_, c := newMiniRedis(t)
	// Window uses real wall clock (time.Now().UnixMilli) — must actually sleep.
	rl := NewRateLimiter(c, 1, 150*time.Millisecond)
	ctx := context.Background()

	allowed, _, _ := rl.Allow(ctx, "win")
	if !allowed {
		t.Fatal("first should be allowed")
	}
	allowed, _, _ = rl.Allow(ctx, "win")
	if allowed {
		t.Fatal("second should be blocked")
	}
	time.Sleep(200 * time.Millisecond)
	allowed, _, _ = rl.Allow(ctx, "win")
	if !allowed {
		t.Error("after window expiry should be allowed again")
	}
}

func TestRateLimiter_FailsOpenOnRedisError(t *testing.T) {
	s, c := newMiniRedis(t)
	s.Close() // force redis errors
	rl := NewRateLimiter(c, 1, time.Minute)
	allowed, _, err := rl.Allow(context.Background(), "x")
	if err == nil {
		t.Error("expected redis error")
	}
	if !allowed {
		t.Error("should fail open and allow")
	}
}

func TestIPRateLimit_BlocksWhenExceeded(t *testing.T) {
	_, c := newMiniRedis(t)
	rl := NewRateLimiter(c, 1, time.Minute)
	hits := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	})
	h := IPRateLimit(rl)(inner)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/path", nil)
		req.RemoteAddr = "1.1.1.1:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Errorf("first request blocked: %d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("second request not blocked: %d", rec.Code)
		}
	}
	if hits != 1 {
		t.Errorf("inner called %d times, want 1", hits)
	}
}

func TestUserRateLimit_BlocksWhenExceeded(t *testing.T) {
	_, c := newMiniRedis(t)
	rl := NewRateLimiter(c, 1, time.Minute)
	hits := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	})
	h := UserRateLimit(rl)(inner)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/p", nil)
		ctx := context.WithValue(req.Context(), userIDKey, int64(42))
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Errorf("first not allowed: %d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("second not blocked: %d", rec.Code)
		}
	}
	if hits != 1 {
		t.Errorf("hits=%d", hits)
	}
}

func TestUserRateLimit_PassesThroughWithoutUserID(t *testing.T) {
	_, c := newMiniRedis(t)
	rl := NewRateLimiter(c, 1, time.Minute)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := UserRateLimit(rl)(inner)

	req := httptest.NewRequest("GET", "/p", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Error("inner should be called when no user_id")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d", rec.Code)
	}
}
