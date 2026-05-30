package main

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func setupRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	prev := rdb
	rdb = redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() {
		rdb.Close()
		rdb = prev
		s.Close()
	})
	return s
}

func setupClickConn(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	prev := clickConn
	clickConn = db
	t.Cleanup(func() {
		db.Close()
		clickConn = prev
	})
	return mock
}

// --- healthHandler ---

func TestHealthHandler_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	healthHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestHealthHandler_Ok(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectPing()
	prev := rdb
	rdb = nil
	defer func() { rdb = prev }()

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	healthHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleRoot(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handleRoot(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Welcome") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// --- shorten ---

func ctxWithUser(uid int64) context.Context {
	return context.WithValue(context.Background(), userIDKey, uid)
}

func TestShorten_Unauthorized(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{"url": "https://x.com"}))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestShorten_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{"url": "https://x.com"}))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestShorten_BadJSON(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/shorten", bytes.NewBufferString("{"))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestShorten_MissingURL(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{}))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestShorten_InvalidURL(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{"url": "not-a-url"}))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestShorten_Success(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectExec("INSERT INTO urls").
		WithArgs(int64(1), sqlmock.AnyArg(), "https://x.com").
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{"url": "https://x.com"}))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("code = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestShorten_InsertError(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectExec("INSERT INTO urls").
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{"original_url": "https://x.com"}))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestShorten_CollisionThenSuccess(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectExec("INSERT INTO urls").
		WillReturnError(&pq.Error{Code: "23505"})
	mock.ExpectExec("INSERT INTO urls").
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest("POST", "/shorten", jsonBody(map[string]string{"url": "https://x.com"}))
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	shorten(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("code = %d", rec.Code)
	}
}

// --- redirect ---

func TestRedirect_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("GET", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	redirect(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRedirect_NotFound(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	prev := rdb
	rdb = nil
	defer func() { rdb = prev }()

	mock.ExpectQuery("SELECT id, original_url FROM urls").
		WithArgs("abc").WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest("GET", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	redirect(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRedirect_DBError(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	prev := rdb
	rdb = nil
	defer func() { rdb = prev }()

	mock.ExpectQuery("SELECT id, original_url FROM urls").
		WithArgs("abc").WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest("GET", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	redirect(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRedirect_FromDB_RecordsClick(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	setupRedis(t)
	clickMock := setupClickConn(t)

	mock.ExpectQuery("SELECT id, original_url FROM urls").
		WithArgs("abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "original_url"}).
			AddRow(7, "https://example.com"))

	clickMock.ExpectExec("INSERT INTO clicks").
		WithArgs(7, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest("GET", "/abc", nil)
	req.SetPathValue("code", "abc")
	req.RemoteAddr = "1.2.3.4:5000"
	rec := httptest.NewRecorder()
	redirect(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("code = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com" {
		t.Errorf("Location = %q", loc)
	}
	// Give async click insert a chance to run.
	time.Sleep(50 * time.Millisecond)
}

func TestRedirect_FromCache(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	setupRedis(t)
	setupClickConn(t)

	if err := rdb.Set(context.Background(), cacheKey("zzz"), `{"id":5,"url":"https://cached.example"}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/zzz", nil)
	req.SetPathValue("code", "zzz")
	req.RemoteAddr = "1.1.1.1:1"
	rec := httptest.NewRecorder()
	redirect(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://cached.example" {
		t.Errorf("Location = %q", loc)
	}
	time.Sleep(20 * time.Millisecond)
}

// --- deleteURL ---

func TestDeleteURL_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("DELETE", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	deleteURL(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestDeleteURL_NotFound(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	prev := rdb
	rdb = nil
	defer func() { rdb = prev }()

	mock.ExpectExec("DELETE FROM urls").
		WithArgs("abc").
		WillReturnResult(sqlmock.NewResult(0, 0))

	req := httptest.NewRequest("DELETE", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	deleteURL(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestDeleteURL_Success(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	setupRedis(t)

	mock.ExpectExec("DELETE FROM urls").
		WithArgs("abc").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	deleteURL(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestDeleteURL_ExecError(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectExec("DELETE FROM urls").
		WithArgs("abc").
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest("DELETE", "/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	deleteURL(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d", rec.Code)
	}
}

// --- getStats ---

func TestGetStats_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("GET", "/stats/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	getStats(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestGetStats_NotFound(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SELECT u.short_code").
		WithArgs("abc").WillReturnError(sql.ErrNoRows)
	req := httptest.NewRequest("GET", "/stats/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	getStats(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestGetStats_DBError(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SELECT u.short_code").
		WithArgs("abc").WillReturnError(sql.ErrConnDone)
	req := httptest.NewRequest("GET", "/stats/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	getStats(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestGetStats_Success(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SELECT u.short_code").
		WithArgs("abc").
		WillReturnRows(sqlmock.NewRows([]string{"short_code", "original_url", "clicks", "created_at"}).
			AddRow("abc", "https://x.com", 42, "2024-01-01"))

	req := httptest.NewRequest("GET", "/stats/abc", nil)
	req.SetPathValue("code", "abc")
	rec := httptest.NewRecorder()
	getStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"clicks":42`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// --- getMyUrls ---

func TestGetMyUrls_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("GET", "/my-urls", nil)
	rec := httptest.NewRecorder()
	getMyUrls(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestGetMyUrls_Unauthorized(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("GET", "/my-urls", nil)
	rec := httptest.NewRecorder()
	getMyUrls(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestGetMyUrls_QueryError(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SELECT u.short_code").
		WithArgs(int64(1)).
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest("GET", "/my-urls", nil)
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	getMyUrls(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestGetMyUrls_Success(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SELECT u.short_code").
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"short_code", "original_url", "created_at", "clicks"}).
			AddRow("abc", "https://x.com", "2024-01-01", 5).
			AddRow("def", "https://y.com", "2024-01-02", 0))

	req := httptest.NewRequest("GET", "/my-urls", nil)
	req = req.WithContext(ctxWithUser(1))
	rec := httptest.NewRecorder()
	getMyUrls(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "abc") || !strings.Contains(body, "def") {
		t.Errorf("body = %s", body)
	}
}

func TestCacheKey(t *testing.T) {
	if got := cacheKey("xyz"); got != "short_url:xyz" {
		t.Errorf("cacheKey = %q", got)
	}
}
