package main

// mux - multiplexer is a request router that redirects the traffic to particular endpoint or handler function
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lib/pq"
)

// Create a db pool
var conn *sql.DB

type User struct {
	Name string `json:"name"`
}

var userCache = make(map[int]User)
var cacheMutex sync.RWMutex

func main() {
	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Connect to database
	if err := db(); err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return
	}
	slog.Info("Database connection established successfully")

	//  Routing and middleware setup
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("POST /user", createUser)
	mux.HandleFunc("GET /users/{id}", getUser)
	mux.HandleFunc("GET /stats/{code}", getStats)
	mux.HandleFunc("DELETE /delete/{id}", deleteUser)
	mux.HandleFunc("POST /shorten", shorten)
	mux.HandleFunc("GET /{code}", redirect)
	mux.HandleFunc("GET /health", healthHandler)

	handler := loggingMiddleware(mux)
	server := &http.Server{
		Addr:    ":3000",
		Handler: handler,
	}

	//  Set up signal listening context
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start HTTP Server in background
	go func() {
		slog.Info("Server listening on port 3000")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed to start", "error", err)
		}
	}()

	// Block until SIGINT or SIGTERM is received
	<-shutdownCtx.Done()
	slog.Info("Shutdown signal received, winding down HTTP server...")


	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	//  Shutdown HTTP Server first
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown failed", "error", err)
	} else {
		slog.Info("HTTP server stopped cleanly")
	}

	// close database connections last
	if conn != nil {
		slog.Info("Closing database connections...")
		if err := conn.Close(); err != nil {
			slog.Error("Failed to close database connection cleanly", "error", err)
		} else {
			slog.Info("Database connection closed successfully")
		}
	}

	slog.Info("Application fully shutdown.")
}


func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if conn == nil {
		slog.Error("health_check_failed", "component", "database", "error", "connection_not_initialized")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unhealthy","reason":"database_not_initialized"}`))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		slog.Error("health_check_failed", "component", "database", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unhealthy","reason":"database_down"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Welcome to the URL Shortener API",
	})
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var user User
	err := json.NewDecoder(r.Body).Decode(&user)

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)
		return
	}
	if user.Name == "" {
		http.Error(
			w,
			"name is required",
			http.StatusBadRequest,
		)
		return
	}
	cacheMutex.Lock()
	userCache[len(userCache)+1] = user
	cacheMutex.Unlock()

	slog.Info("User created", "user_id", len(userCache), "user", user.Name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id": len(userCache),
		"user":    user.Name,
	})

}

func getUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w,
			err.Error(),
			http.StatusBadRequest,
		)
		return
	}
	cacheMutex.RLock()
	user, ok := userCache[id]
	cacheMutex.RUnlock()

	if !ok {
		http.Error(
			w,
			"User not found",
			http.StatusNotFound,
		)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(user)
}

func deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)
		return
	}
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	if _, ok := userCache[id]; !ok {
		http.Error(
			w,
			"User not found",
			http.StatusNotFound,
		)
		return
	}
	delete(userCache, id)
	w.WriteHeader(http.StatusNoContent)
	slog.Info("User deleted", "user_id", id)

}

// Request/Response types
type ShortenRequest struct {
	OriginalURL string `json:"original_url"`
	URL         string `json:"url"`
}

type ShortenResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
}

type StatsResponse struct {
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
	Clicks      int    `json:"clicks"`
	CreatedAt   string `json:"created_at"`
}

func generateCode() string {
	charset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	code := make([]byte, 6)
	for i := range code {
		code[i] = charset[rand.Intn(len(charset))]
	}
	return string(code)
}

func shorten(w http.ResponseWriter, r *http.Request) {
	var req ShortenRequest

	if conn == nil {
		http.Error(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		slog.Error("Failed to decode request body", "error", err)
		return
	}
	if req.OriginalURL == "" {
		req.OriginalURL = req.URL
	}
	if req.OriginalURL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		slog.Error("URL is required")
		return
	}

	const maxCodeAttempts = 5
	var code string
	var err error
	for i := 0; i < maxCodeAttempts; i++ {
		code = generateCode()
		_, err = conn.Exec(`
INSERT INTO urls (short_code, original_url)
VALUES ($1, $2)
`, code, req.OriginalURL)
		if err == nil {
			break
		}
		if isUniqueViolation(err) {
			slog.Warn("Short code collision, retrying", "attempt", i+1, "error", err)
			continue
		}
		slog.Error("Failed to insert URL", "error", err)
		http.Error(w, "Failed to insert URL", http.StatusInternalServerError)
		return
	}
	if err != nil {
		http.Error(w, "Failed to generate unique short code", http.StatusInternalServerError)
		slog.Error("Failed to generate unique short code", "error", err)
		return
	}

	resp := ShortenResponse{
		ShortCode: code,
		ShortURL:  fmt.Sprintf("http://localhost:3000/%s", code),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)

}

func redirect(w http.ResponseWriter, r *http.Request) {
	if conn == nil {
		http.Error(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	code := r.PathValue("code")
	var originalURL string
	var Id int
	err := conn.QueryRow(`
SELECT id, original_url FROM urls WHERE short_code = $1
`, code).Scan(&Id, &originalURL)
	if err == sql.ErrNoRows {
		slog.Error("Short code not found for redirecting", "short_code", code)
		http.Error(w, "Short code not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("Failed to query URL", "error", err)
		http.Error(w, "Failed to query URL: Database error", http.StatusInternalServerError)
		return
	}

	ip := clientIP(r.RemoteAddr)
	go func(id int, ip, ua string) {
		_, err := conn.Exec(`
INSERT INTO clicks (url_id, ip_address, user_agent, clicked_at)
VALUES ($1, $2, $3, $4)
`, id, ip, ua, time.Now())
		if err != nil {
			slog.Error("Failed to record click", "error", err)
		}
	}(Id, ip, r.Header.Get("User-Agent"))

	http.Redirect(w, r, originalURL, http.StatusFound)
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remoteAddr, "[]")
}

func getStats(w http.ResponseWriter, r *http.Request) {
	if conn == nil {
		slog.Error("Database connection not initialized")
		http.Error(w, "Database not initialized", http.StatusServiceUnavailable)
		return
	}
	code := r.PathValue("code")
	var stats StatsResponse
	err := conn.QueryRow(`
SELECT u.short_code, u.original_url, COUNT(c.id) AS clicks, u.created_at
FROM urls u
LEFT JOIN clicks c ON u.id = c.url_id
WHERE u.short_code = $1
GROUP BY u.id
`, code).Scan(&stats.ShortCode, &stats.OriginalURL, &stats.Clicks, &stats.CreatedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "Short code not found", http.StatusNotFound)
		slog.Error("Short code not found", "short_code", code)
		return
	}
	if err != nil {
		http.Error(w, "Failed to query stats: Database error", http.StatusInternalServerError)
		slog.Error("Failed to query stats", "error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
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
