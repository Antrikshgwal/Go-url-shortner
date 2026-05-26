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
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

// Create a db pool
var conn *sql.DB
var rdb *redis.Client
var ctx = context.Background()

type User struct {
	Name string `json:"name"`
}

func main() {

	// Initialize Redis client
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 2*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Error("Redis ping failed", "error", err)
	} else {
		slog.Info("Redis connection established")
	}
	cancelPing()

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
	mux.HandleFunc("POST /register", RegisterHandler)
	mux.HandleFunc("POST /login", LoginHandler)
	mux.HandleFunc("GET /stats/{code}", getStats)


	mux.Handle("POST /shorten", AuthMiddleware(http.HandlerFunc(shorten)))
	mux.Handle("GET /myurls", AuthMiddleware(http.HandlerFunc(getMyUrls)))
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

	// check redis connection health
	if rdb != nil {
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := rdb.Ping(pingCtx).Err(); err != nil {
			slog.Error("health_check_failed", "component", "redis", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unhealthy","reason":"redis_down"}`))
			return
		}
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

	userID, ok := r.Context().Value("user_id").(int64)
	if !ok {
		WriteError(w, "Unauthorized: Missing or invalid token", http.StatusUnauthorized)
		return
	}



	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, "Invalid request payload", http.StatusBadRequest)
		slog.Error("Failed to decode request body", "error", err)
		return
	}
	if req.OriginalURL == "" {
		req.OriginalURL = req.URL
	}
	if req.OriginalURL == "" {
		WriteError(w, "URL is required", http.StatusBadRequest)
		slog.Error("URL is required")
		return
	}
	if err := validateURL(req.OriginalURL); err != nil {
		WriteError(w, "Invalid URL format", http.StatusBadRequest)
		slog.Error("URL validation failed", "error", err)
		return
	}

	const maxCodeAttempts = 5
	var code string
	var err error
	for i := 0; i < maxCodeAttempts; i++ {
		code = generateCode()
		_, err = conn.Exec(`
INSERT INTO urls (user_id, short_code, original_url)
VALUES ($1, $2, $3)
`, userID, code, req.OriginalURL)
		if err == nil {
			break
		}
		if isUniqueViolation(err) {
			slog.Warn("Short code collision, retrying", "attempt", i+1, "error", err)
			continue
		}
		slog.Error("Failed to insert URL", "error", err)
		WriteError(w, "Failed to insert URL", http.StatusInternalServerError)
		return
	}
	if err != nil {
		WriteError(w, "Failed to generate unique short code", http.StatusInternalServerError)
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
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	code := r.PathValue("code")
	var originalURL string
	var Id int

	cacheKey := "short_url:" + code
	if rdb != nil {
		cachedBytes, err := rdb.Get(r.Context(), cacheKey).Bytes()
		if err == nil {
			var cached struct {
				ID  int    `json:"id"`
				URL string `json:"url"`
			}
			if err := json.Unmarshal(cachedBytes, &cached); err == nil && cached.ID > 0 && cached.URL != "" {
				Id = cached.ID
				originalURL = cached.URL
			} else if err != nil {
				slog.Warn("Redis cache unmarshal failed", "error", err)
			}
		} else if !errors.Is(err, redis.Nil) {
			slog.Warn("Redis cache lookup failed", "error", err)
		}
	}

	if originalURL == "" {
		err := conn.QueryRow(`
SELECT id, original_url FROM urls WHERE short_code = $1
`, code).Scan(&Id, &originalURL)
		if err == sql.ErrNoRows {
			slog.Error("Short code not found for redirecting", "short_code", code)
			WriteError(w, "Short code not found", http.StatusNotFound)
			return
		}
		if err != nil {
			slog.Error("Failed to query URL", "error", err)
			WriteError(w, "Failed to query URL: Database error", http.StatusInternalServerError)
			return
		}

		if rdb != nil {
			payload, err := json.Marshal(map[string]interface{}{
				"id":  Id,
				"url": originalURL,
			})
			if err != nil {
				slog.Warn("Redis cache marshal failed", "error", err)
			} else if err := rdb.Set(r.Context(), cacheKey, payload, 24*time.Hour).Err(); err != nil {
				slog.Warn("Redis cache set failed", "error", err)
			}
		}
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
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
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
		WriteError(w, "Short code not found", http.StatusNotFound)
		slog.Error("Short code not found", "short_code", code)
		return
	}
	if err != nil {
		WriteError(w, "Failed to query stats: Database error", http.StatusInternalServerError)
		slog.Error("Failed to query stats", "error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
}

func getMyUrls(w http.ResponseWriter, r *http.Request) {
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	userID, ok := r.Context().Value("user_id").(int64)
	if !ok {
		WriteError(w, "Unauthorized: Missing or invalid token", http.StatusUnauthorized)
		return
	}
	 rows, err := conn.Query(`
          SELECT u.short_code, u.original_url, u.created_at, COUNT(c.id) as clicks
          FROM urls u
          LEFT JOIN clicks c ON u.id = c.url_id
          WHERE u.user_id = $1
          GROUP BY u.id
          ORDER BY u.created_at DESC
      `, userID)
	  if err != nil {
		WriteError(w, "Failed to query URLs: Database error", http.StatusInternalServerError)
		slog.Error("Failed to query URLs for user", "user_id", userID, "error", err)
		return
	}
	defer rows.Close()

	var urls []map[string]interface{}
	for rows.Next() {
		var shortCode, originalURL, createdAt string
		var clicks int
		if err := rows.Scan(&shortCode, &originalURL, &createdAt, &clicks); err != nil {
			WriteError(w, "Failed to read URL data: Database error", http.StatusInternalServerError)
			slog.Error("Failed to read URL data for user", "user_id", userID, "error", err)
			return
		}
		urls = append(urls, map[string]interface{}{
			"short_code":   shortCode,
			"original_url": originalURL,
			"created_at":   createdAt,
			"clicks":       clicks,
			"short_url":    fmt.Sprintf("http://localhost:3000/%s", shortCode),
		})
		w.Header()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(urls)
	}
	}