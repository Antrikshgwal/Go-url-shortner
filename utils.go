// logging, validation and error handling utilities

package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lib/pq"
)

// validate input URL
// 1. Must start with http:// or https://
// 2. Must be a valid URL format
// 3. Muts be less than 2048 characters
// 4. add logging to see common URL validation mistakes that user do
func validateURL(input string) error {
	parse, err := url.ParseRequestURI(input)

	if err != nil {
		slog.Error("Invalid URL format", "error", err)
		return err
	}
	if parse.Scheme != "http" && parse.Scheme != "https" {
		slog.Error("URL must start with http:// or https://")
		return fmt.Errorf("URL must start with http:// or https://")
	}
	if len(input) > 2048 {
		slog.Error("URL exceeds maximum length of 2048 characters")
		return fmt.Errorf("URL exceeds maximum length of 2048 characters")
	}
	return nil
}

// It can fail if the OS entropy source is unavailable, so it returns an error.
func generateCode() (string, error) {
	code := make([]byte, 6)
	max := big.NewInt(int64(len(codeCharset)))
	for i := range code {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate random code: %w", err)
		}
		code[i] = codeCharset[n.Int64()]
	}
	return string(code), nil
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remoteAddr, "[]")
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return false
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()

		next.ServeHTTP(sw, r)

		duration := time.Since(start)
		ip := clientIP(r.RemoteAddr)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", duration.Milliseconds(), "ip", ip)
	})
}

// parseAllowedOrigins turns a comma-separated env value into a lookup set.
func parseAllowedOrigins(raw string) map[string]bool {
	m := make(map[string]bool)
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			m[o] = true
		}
	}
	return m
}

// corsMiddleware sets CORS headers and answers preflight (OPTIONS) requests.
// todo: Allow all for now, tighten later
func corsMiddleware(next http.Handler) http.Handler {
	allowed := parseAllowedOrigins(getEnv("CORS_ALLOWED_ORIGINS", "*"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if allowed["*"] {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Preflight requests don't match the method-specific routes, so answer here.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type Errorresponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

func WriteError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(Errorresponse{Error: msg, Code: code})
}
