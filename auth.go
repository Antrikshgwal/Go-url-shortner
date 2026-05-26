// User authentication

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var secret_key = []byte("your_secret_key")

func generateToken(username string, email string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.MapClaims{
			"username": username,
			"email":    email,
			"exp":      time.Now().Add(time.Hour * 24).Unix(),
		})

	tokenString, err := token.SignedString(secret_key)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func validateToken(tokenString string) (string, string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret_key, nil
	})

	if err != nil {
		return "", "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		username, ok := claims["username"].(string)
		if !ok {
			return "", "", fmt.Errorf("invalid username in token")
		}
		email, ok := claims["email"].(string)
		if !ok {
			return "", "", fmt.Errorf("invalid email in token")
		}
		return username, email, nil
	}

	return "", "", fmt.Errorf("invalid token")
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		return
	}

	var creds struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		WriteError(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	if creds.Username == "" || creds.Email == "" || creds.Password == "" {
		WriteError(w, "Username, email, and password are required", http.StatusBadRequest)
		return
	}
	// check if user_name or email already exists in database
	var id int

	err := conn.QueryRow("SELECT user_id FROM users WHERE user_name = $1", creds.Username).Scan(&id)
	if err == nil {
		WriteError(w, "Username already exists", http.StatusConflict)
		return
	}
	if err != sql.ErrNoRows {
		slog.Error("register username lookup failed", "error", err)
		WriteError(w, "Database error", http.StatusInternalServerError)
		return
	}

	err = conn.QueryRow("SELECT user_id FROM users WHERE email = $1", creds.Email).Scan(&id)
	if err == nil {
		WriteError(w, "Email already exists", http.StatusConflict)
		return
	}
	if err != sql.ErrNoRows {
		slog.Error("register email lookup failed", "error", err)
		WriteError(w, "Database error", http.StatusInternalServerError)
		return
	}
	// if new user then insert into database
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(creds.Password), bcrypt.DefaultCost)
	if err != nil {
		WriteError(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	_, err = conn.Exec("INSERT INTO users (user_name, email, password_hash) VALUES ($1, $2, $3)", creds.Username, creds.Email, hashedPassword)
	if err != nil {
		if isUniqueViolation(err) {
			WriteError(w, "Username or email already exists", http.StatusConflict)
			return
		}
		slog.Error("register insert failed", "error", err)
		WriteError(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	token, err := generateToken(creds.Username, creds.Email)
	if err != nil {
		WriteError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"username": creds.Username,
		"email":    creds.Email,
		"token":    token,
	})
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		return
	}
	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		WriteError(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	if creds.Username == "" || creds.Password == "" {
		WriteError(w, "Username and password are required", http.StatusBadRequest)
		return
	}
	var storedHashedPassword string
	var email string
	if err := conn.QueryRow("SELECT password_hash, email FROM users WHERE user_name = $1", creds.Username).Scan(&storedHashedPassword, &email); err != nil {
		if err == sql.ErrNoRows {
			WriteError(w, "Invalid username or password", http.StatusUnauthorized)
		} else {
			slog.Error("login lookup failed", "error", err)
			WriteError(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHashedPassword), []byte(creds.Password)); err != nil {
		WriteError(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	token, err := generateToken(creds.Username, email)
	if err != nil {
		WriteError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			WriteError(w, "Authorization header missing", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			WriteError(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		username, email, err := validateToken(tokenString)
		if err != nil {
			WriteError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), "username", username)
		ctx = context.WithValue(ctx, "email", email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
