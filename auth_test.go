package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// newMockDB sets the package-level conn to a sqlmock-backed DB.
// Returns the mock, and a cleanup that restores the previous conn.
func newMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	prev := conn
	conn = db
	return mock, func() {
		db.Close()
		conn = prev
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	tok, err := generateToken(42, "a@b.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	uid, email, err := validateToken(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if uid != 42 || email != "a@b.com" {
		t.Errorf("got uid=%d email=%s", uid, email)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	if _, _, err := validateToken("garbage"); err == nil {
		t.Error("expected error for garbage token")
	}

	// wrong signing method (none)
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"user_id": float64(1), "email": "x"})
	s, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, _, err := validateToken(s); err == nil {
		t.Error("expected error for 'none' algo token")
	}

	// expired
	exp := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(1), "email": "x", "exp": time.Now().Add(-time.Hour).Unix(),
	})
	es, _ := exp.SignedString(secret_key)
	if _, _, err := validateToken(es); err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateToken_UserIDString(t *testing.T) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "77", "email": "z@x", "exp": time.Now().Add(time.Hour).Unix(),
	})
	s, _ := tok.SignedString(secret_key)
	uid, _, err := validateToken(s)
	if err != nil || uid != 77 {
		t.Errorf("uid=%d err=%v", uid, err)
	}
}

func TestValidateToken_BadUserID(t *testing.T) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "abc", "email": "z@x", "exp": time.Now().Add(time.Hour).Unix(),
	})
	s, _ := tok.SignedString(secret_key)
	if _, _, err := validateToken(s); err == nil {
		t.Error("expected error parsing non-numeric user_id")
	}

	tok2 := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": true, "email": "z@x", "exp": time.Now().Add(time.Hour).Unix(),
	})
	s2, _ := tok2.SignedString(secret_key)
	if _, _, err := validateToken(s2); err == nil {
		t.Error("expected error for bool user_id")
	}

	tok3 := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(1), "exp": time.Now().Add(time.Hour).Unix(),
	})
	s3, _ := tok3.SignedString(secret_key)
	if _, _, err := validateToken(s3); err == nil {
		t.Error("expected error for missing email")
	}
}

func TestAuthMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := r.Context().Value(userIDKey).(int64)
		if uid == 0 {
			t.Error("user_id not propagated in context")
		}
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(okHandler)

	t.Run("missing header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("code = %d", rec.Code)
		}
	})

	t.Run("missing bearer", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Token abc")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("code = %d", rec.Code)
		}
	})

	t.Run("bad token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer garbage")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("code = %d", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		tok, _ := generateToken(99, "ok@x")
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("code = %d", rec.Code)
		}
	})
}

func jsonBody(v interface{}) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

func TestRegisterHandler_DBNotInitialized(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()

	req := httptest.NewRequest("POST", "/register", jsonBody(map[string]string{"username": "u", "email": "e", "password": "p"}))
	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRegisterHandler_BadPayload(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/register", bytes.NewBufferString("not-json"))
	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRegisterHandler_MissingFields(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/register", jsonBody(map[string]string{"username": "u"}))
	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRegisterHandler_UsernameExists(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()

	mock.ExpectQuery("SELECT user_id FROM users WHERE user_name").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(1))

	req := httptest.NewRequest("POST", "/register",
		jsonBody(map[string]string{"username": "alice", "email": "a@b", "password": "pw"}))
	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("code = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterHandler_EmailExists(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()

	mock.ExpectQuery("SELECT user_id FROM users WHERE user_name").
		WithArgs("alice").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id FROM users WHERE email").
		WithArgs("a@b").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(2))

	req := httptest.NewRequest("POST", "/register",
		jsonBody(map[string]string{"username": "alice", "email": "a@b", "password": "pw"}))
	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRegisterHandler_Success(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()

	mock.ExpectQuery("SELECT user_id FROM users WHERE user_name").
		WithArgs("alice").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id FROM users WHERE email").
		WithArgs("a@b").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO users").
		WithArgs("alice", "a@b", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(7)))

	req := httptest.NewRequest("POST", "/register",
		jsonBody(map[string]string{"username": "alice", "email": "a@b", "password": "pw"}))
	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["token"] == "" || out["user_id"] == nil {
		t.Errorf("missing fields: %v", out)
	}
}

func TestLoginHandler_DBNil(t *testing.T) {
	prev := conn
	conn = nil
	defer func() { conn = prev }()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"username": "x", "password": "y"}))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestLoginHandler_BadPayload(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/login", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	_, cleanup := newMockDB(t)
	defer cleanup()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"email": "u@b"}))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestLoginHandler_UserNotFound(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SELECT user_id, password_hash, email FROM users").
		WithArgs("nope@b").WillReturnError(sql.ErrNoRows)
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"email": "nope@b", "password": "pw"}))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	hashed, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	mock.ExpectQuery("SELECT user_id, password_hash, email FROM users").
		WithArgs("a@b").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "password_hash", "email"}).
			AddRow(int64(1), string(hashed), "a@b"))

	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"email": "a@b", "password": "wrong"}))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestLoginHandler_Success(t *testing.T) {
	mock, cleanup := newMockDB(t)
	defer cleanup()
	hashed, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	mock.ExpectQuery("SELECT user_id, password_hash, email FROM users").
		WithArgs("a@b").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "password_hash", "email"}).
			AddRow(int64(1), string(hashed), "a@b"))

	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"email": "a@b", "password": "correct"}))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["token"] == "" {
		t.Error("expected token")
	}
}


var _ = context.Background
