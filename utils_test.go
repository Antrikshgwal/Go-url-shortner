package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid http", "http://example.com", false},
		{"valid https", "https://example.com/path?q=1", false},
		{"invalid scheme ftp", "ftp://example.com", true},
		{"missing scheme", "example.com", true},
		{"empty", "", true},
		{"too long", "https://example.com/" + strings.Repeat("a", 2048), true},
		{"garbage", "::::not a url", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateURL(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateURL(%q) err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestGenerateCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode failed: %v", err)
		}
		if len(code) != 6 {
			t.Fatalf("expected length 6, got %d (%q)", len(code), code)
		}
		for _, c := range code {
			if !strings.ContainsRune(codeCharset, c) {
				t.Fatalf("code contains invalid char %q", c)
			}
		}
		seen[code] = true
	}
	if len(seen) < 90 {
		t.Errorf("generateCode appears non-random: only %d unique out of 100", len(seen))
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"127.0.0.1:1234", "127.0.0.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := clientIP(tc.in); got != tc.want {
			t.Errorf("clientIP(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Error("nil should not be unique violation")
	}
	if isUniqueViolation(errors.New("random")) {
		t.Error("random error should not be unique violation")
	}
	pqErr := &pq.Error{Code: "23505"}
	if !isUniqueViolation(pqErr) {
		t.Error("23505 should be unique violation")
	}
	other := &pq.Error{Code: "23000"}
	if isUniqueViolation(other) {
		t.Error("non-23505 pq error should not be unique violation")
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, "boom", http.StatusBadRequest)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"error":"boom"`) {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestStatusWriter_DefaultsTo200OnWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	if _, err := sw.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if sw.status != http.StatusOK {
		t.Errorf("status = %d want 200", sw.status)
	}
}

func TestStatusWriter_CapturesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	sw.WriteHeader(http.StatusTeapot)
	sw.Write([]byte("x"))
	if sw.status != http.StatusTeapot {
		t.Errorf("status = %d want 418", sw.status)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("recorder code = %d", rec.Code)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	called := false
	h := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler not called")
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("code = %d", rec.Code)
	}
}
