package main

// Contains http handlers
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

// Create a db pool
var conn *sql.DB
var rdb *redis.Client
var ctx = context.Background()
var (
    registerLimiter *RateLimiter
    loginLimiter    *RateLimiter
    redirectLimiter *RateLimiter
    shortenLimiter  *RateLimiter
    deleteLimiter   *RateLimiter
)

type contextKey string

const userIDKey contextKey = "user_id"

func cacheKey(code string) string {
	return "short_url:" + code
}


func baseURL() string {
	return strings.TrimRight(getEnv("BASE_URL", "http://localhost:3000"), "/")
}

type User struct {
	Name string `json:"name"`
}

func main() {

	// Initialize Redis client.
	redisAddr := getEnv("REDIS_URL", "redis:6379")
	if strings.Contains(redisAddr, "://") {
		opts, err := redis.ParseURL(redisAddr)
		if err != nil {
			slog.Error("Failed to parse REDIS_URL", "error", err)
			return
		}
		rdb = redis.NewClient(opts)
	} else {
		rdb = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: "", // no password set
			DB:       0,  // use default DB
		})
	}
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Error("Redis ping failed", "error", err)
	} else {
		slog.Info("Redis connection established")
	}
	cancelPing()

	// rate limit config
	registerLimiter = NewRateLimiter(rdb, 5, time.Hour)
	loginLimiter    = NewRateLimiter(rdb, 10, time.Hour)
	redirectLimiter = NewRateLimiter(rdb, 60, time.Minute)
	shortenLimiter  = NewRateLimiter(rdb, 10, time.Minute)
	deleteLimiter   = NewRateLimiter(rdb, 20, time.Minute)

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
	// Public endpoints: IP rate limited
	mux.Handle("POST /register",
    IPRateLimit(registerLimiter)(http.HandlerFunc(RegisterHandler)))

	mux.Handle("POST /login",
    IPRateLimit(loginLimiter)(http.HandlerFunc(LoginHandler)))

	mux.Handle("GET /{code}",
    IPRateLimit(redirectLimiter)(http.HandlerFunc(redirect)))

// Authenticated endpoints: Auth first, then user rate limit
	mux.Handle("POST /shorten",
    AuthMiddleware(
        UserRateLimit(shortenLimiter)(http.HandlerFunc(shorten))))

	mux.Handle("DELETE /{code}",
    AuthMiddleware(
        UserRateLimit(deleteLimiter)(http.HandlerFunc(deleteURL))))

	// No rate limiting
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /stats/{code}", getStats)
	mux.HandleFunc("GET /stats/{code}/analytics", getAnalytics)
	mux.Handle("GET /my-urls", AuthMiddleware(http.HandlerFunc(getMyUrls)))

	handler := loggingMiddleware(corsMiddleware(mux))
	server := &http.Server{
		Addr:    ":" + getEnv("PORT", "3000"),
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

	// close redis connection
	if rdb != nil {
		slog.Info("Closing Redis connection...")
		if err := rdb.Close(); err != nil {
			slog.Error("Failed to close Redis connection cleanly", "error", err)
		} else {
			slog.Info("Redis connection closed successfully")
		}
	}

	// close analytics pool before the main pool
	if clickConn != nil {
		slog.Info("Closing analytics database connections...")
		if err := clickConn.Close(); err != nil {
			slog.Error("Failed to close analytics database connection cleanly", "error", err)
		} else {
			slog.Info("Analytics database connection closed successfully")
		}
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

	// check database connection health
	dbStatus := "ok"
	if conn == nil {
		slog.Error("health_check_failed", "component", "database", "error", "connection_not_initialized")
		dbStatus = "not_initialized"
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unhealthy","reason":"database_not_initialized"}`))
		return
	}

	// check redis connection health
	rdbStatus := "ok"
	if rdb != nil {
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := rdb.Ping(pingCtx).Err(); err != nil {
			slog.Error("health_check_failed", "component", "redis", "error", err)
			rdbStatus = "down"
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unhealthy","reason":"redis_down"}`))
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		slog.Error("health_check_failed", "component", "database", "error", err)
		dbStatus = "down"
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unhealthy","reason":"database_down"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy", "dbStatus": dbStatus, "redisStatus": rdbStatus})
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
	Alias       string `json:"alias"`
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

const codeCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func shorten(w http.ResponseWriter, r *http.Request) {
	var req ShortenRequest

	userID, ok := r.Context().Value(userIDKey).(int64)
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

	alias := strings.TrimSpace(req.Alias)
	var code string
	var err error

	if alias != "" {
		if err := validateAlias(alias); err != nil {
			WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, err := conn.Exec(`
INSERT INTO urls (user_id, short_code, original_url)
VALUES ($1, $2, $3)
`, userID, alias, req.OriginalURL)
		if err != nil {
			if isUniqueViolation(err) {
				WriteError(w, "Alias is already taken", http.StatusConflict)
				return
			}
			slog.Error("Failed to insert URL with alias", "error", err)
			WriteError(w, "Failed to insert URL", http.StatusInternalServerError)
			return
		}
		code = alias
	} else {
		const maxCodeAttempts = 5
		for i := 0; i < maxCodeAttempts; i++ {
			code, err = generateCode()
			if err != nil {
				slog.Error("Failed to generate random code", "error", err)
				WriteError(w, "Failed to generate short code", http.StatusInternalServerError)
				return
			}
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
	}

	resp := ShortenResponse{
		ShortCode: code,
		ShortURL:  fmt.Sprintf("%s/%s", baseURL(), code),
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

	key := cacheKey(code)
	if rdb != nil {
		cachedBytes, err := rdb.Get(r.Context(), key).Bytes()
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
			} else if err := rdb.Set(r.Context(), key, payload, 24*time.Hour).Err(); err != nil {
				slog.Warn("Redis cache set failed", "error", err)
			}
		}
	}

	ip := clientIP(r.RemoteAddr)
	go func(id int, ip, ua, ref string) {
		insertCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := clickConn.ExecContext(insertCtx, `
INSERT INTO clicks (url_id, ip_address, user_agent, referrer, clicked_at)
VALUES ($1, $2, $3, NULLIF($4, ''), $5)
`, id, ip, ua, ref, time.Now())
		if err != nil {
			slog.Error("Failed to record click", "error", err)
		}
	}(Id, ip, r.Header.Get("User-Agent"), r.Header.Get("Referer"))

	http.Redirect(w, r, originalURL, http.StatusFound)
}

func deleteURL(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	deleteResult, err := conn.Exec(`DELETE FROM urls WHERE short_code = $1`, code)
	if err != nil {
		slog.Error("Failed to delete URL", "error", err)
		WriteError(w, "Failed to delete URL", http.StatusInternalServerError)
		return
	}
	rowsAffected, err := deleteResult.RowsAffected()
	if err != nil {
		slog.Error("Failed to read delete result", "error", err)
		WriteError(w, "Failed to delete URL", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		WriteError(w, "URL not found", http.StatusNotFound)
		return
	}
	// Clear cache after deleting URL
	if rdb != nil {
		if err := rdb.Del(r.Context(), cacheKey(code)).Err(); err != nil && !errors.Is(err, redis.Nil) {
			slog.Warn("Failed to delete cache for short code", "short_code", code, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
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

type AnalyticsBucket struct {
	Bucket string `json:"bucket"`
	Clicks int    `json:"clicks"`
}

type AnalyticsBreakdown struct {
	Label  string `json:"label"`
	Clicks int    `json:"clicks"`
}

type AnalyticsResponse struct {
	ShortCode     string               `json:"short_code"`
	RangeDays     int                  `json:"range_days"`
	TotalClicks   int                  `json:"total_clicks"`
	Timeseries    []AnalyticsBucket    `json:"timeseries"`
	TopReferrers  []AnalyticsBreakdown `json:"top_referrers"`
	BrowserBreakdown []AnalyticsBreakdown `json:"browser_breakdown"`
}

func getAnalytics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		return
	}
	code := r.PathValue("code")

	// Clamp range to [1, 90] days, default 7.
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 90 {
			days = n
		}
	}

	var urlID int64
	if err := conn.QueryRow(`SELECT id FROM urls WHERE short_code = $1`, code).Scan(&urlID); err != nil {
		if err == sql.ErrNoRows {
			WriteError(w, "Short code not found", http.StatusNotFound)
			return
		}
		slog.Error("analytics: lookup failed", "error", err)
		WriteError(w, "Database error", http.StatusInternalServerError)
		return
	}

	since := time.Now().AddDate(0, 0, -days)
	resp := AnalyticsResponse{ShortCode: code, RangeDays: days}

	// Timeseries: one row per day. generate_series fills empty days with 0.
	tsRows, err := conn.Query(`
		WITH days AS (
			SELECT generate_series(
				date_trunc('day', $1::timestamp),
				date_trunc('day', NOW()),
				'1 day'
			) AS bucket
		)
		SELECT d.bucket, COALESCE(COUNT(c.id), 0)
		FROM days d
		LEFT JOIN clicks c
			ON c.url_id = $2
			AND date_trunc('day', c.clicked_at) = d.bucket
		GROUP BY d.bucket
		ORDER BY d.bucket
	`, since, urlID)
	if err != nil {
		slog.Error("analytics: timeseries query failed", "error", err)
		WriteError(w, "Database error", http.StatusInternalServerError)
		return
	}
	for tsRows.Next() {
		var b AnalyticsBucket
		var t time.Time
		if err := tsRows.Scan(&t, &b.Clicks); err != nil {
			tsRows.Close()
			slog.Error("analytics: timeseries scan failed", "error", err)
			WriteError(w, "Database error", http.StatusInternalServerError)
			return
		}
		b.Bucket = t.Format("2006-01-02")
		resp.Timeseries = append(resp.Timeseries, b)
		resp.TotalClicks += b.Clicks
	}
	tsRows.Close()

	// Top 5 referrers in window. NULL → "(direct)".
	refRows, err := conn.Query(`
		SELECT COALESCE(referrer, '(direct)') AS ref, COUNT(*) AS clicks
		FROM clicks
		WHERE url_id = $1 AND clicked_at >= $2
		GROUP BY ref
		ORDER BY clicks DESC
		LIMIT 5
	`, urlID, since)
	if err != nil {
		slog.Error("analytics: referrers query failed", "error", err)
		WriteError(w, "Database error", http.StatusInternalServerError)
		return
	}
	for refRows.Next() {
		var b AnalyticsBreakdown
		if err := refRows.Scan(&b.Label, &b.Clicks); err != nil {
			refRows.Close()
			slog.Error("analytics: referrers scan failed", "error", err)
			WriteError(w, "Database error", http.StatusInternalServerError)
			return
		}
		b.Label = shortenReferrer(b.Label)
		resp.TopReferrers = append(resp.TopReferrers, b)
	}
	refRows.Close()

	// Browser breakdown — bucket UAs into a handful of categories in Go.
	uaRows, err := conn.Query(`
		SELECT COALESCE(user_agent, ''), COUNT(*)
		FROM clicks
		WHERE url_id = $1 AND clicked_at >= $2
		GROUP BY user_agent
	`, urlID, since)
	if err != nil {
		slog.Error("analytics: ua query failed", "error", err)
		WriteError(w, "Database error", http.StatusInternalServerError)
		return
	}
	browsers := map[string]int{}
	for uaRows.Next() {
		var ua string
		var n int
		if err := uaRows.Scan(&ua, &n); err != nil {
			uaRows.Close()
			slog.Error("analytics: ua scan failed", "error", err)
			WriteError(w, "Database error", http.StatusInternalServerError)
			return
		}
		browsers[classifyBrowser(ua)] += n
	}
	uaRows.Close()
	for label, n := range browsers {
		resp.BrowserBreakdown = append(resp.BrowserBreakdown, AnalyticsBreakdown{Label: label, Clicks: n})
	}
	sort.Slice(resp.BrowserBreakdown, func(i, j int) bool {
		return resp.BrowserBreakdown[i].Clicks > resp.BrowserBreakdown[j].Clicks
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func getMyUrls(w http.ResponseWriter, r *http.Request) {
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		slog.Error("Database connection not initialized")
		return
	}
	userID, ok := r.Context().Value(userIDKey).(int64)
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

	urls := make([]map[string]interface{}, 0)
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
			"short_url":    fmt.Sprintf("%s/%s", baseURL(), shortCode),
		})
	}
	if err := rows.Err(); err != nil {
		WriteError(w, "Failed to read URL data: Database error", http.StatusInternalServerError)
		slog.Error("Row iteration error for user URLs", "user_id", userID, "error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(urls)
}
