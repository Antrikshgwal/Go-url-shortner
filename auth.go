// User authentication

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var secret_key = []byte(os.Getenv("JWT_SECRET"))

func generateToken(userID int64, email string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.MapClaims{
			"user_id": userID,
			"email":   email,
			"exp":     time.Now().Add(time.Hour * 24).Unix(),
		})

	tokenString, err := token.SignedString(secret_key)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func validateToken(tokenString string) (int64, string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret_key, nil
	})

	if err != nil {
		return 0, "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		var userID int64
		switch v := claims["user_id"].(type) {
		case float64:
			userID = int64(v)
		case string:
			parsed, parseErr := strconv.ParseInt(v, 10, 64)
			if parseErr != nil {
				return 0, "", fmt.Errorf("invalid user_id in token")
			}
			userID = parsed
		default:
			return 0, "", fmt.Errorf("invalid user_id in token")
		}
		email, ok := claims["email"].(string)
		if !ok {
			return 0, "", fmt.Errorf("invalid email in token")
		}
		return userID, email, nil
	}

	return 0, "", fmt.Errorf("invalid token")
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
	var id int64

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

	err = conn.QueryRow("INSERT INTO users (user_name, email, password_hash) VALUES ($1, $2, $3) RETURNING user_id", creds.Username, creds.Email, hashedPassword).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			WriteError(w, "Username or email already exists", http.StatusConflict)
			return
		}
		slog.Error("register insert failed", "error", err)
		WriteError(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	token, err := generateToken(id, creds.Email)
	if err != nil {
		WriteError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id": id,
		"email":   creds.Email,
		"token":   token,
	})
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if conn == nil {
		WriteError(w, "Database not initialized", http.StatusServiceUnavailable)
		return
	}
	var creds struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		WriteError(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	if creds.Email == "" || creds.Password == "" {
		WriteError(w, "Email and password are required", http.StatusBadRequest)
		return
	}
	var storedHashedPassword string
	var email string
	var userID int64
	if err := conn.QueryRow("SELECT user_id, password_hash, email FROM users WHERE email = $1", creds.Email).Scan(&userID, &storedHashedPassword, &email); err != nil {
		if err == sql.ErrNoRows {
			WriteError(w, "Invalid email or password", http.StatusUnauthorized)
		} else {
			slog.Error("login lookup failed", "error", err)
			WriteError(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHashedPassword), []byte(creds.Password)); err != nil {
		WriteError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	token, err := generateToken(userID, email)
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
			WriteError(w, "Authorization needed", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			WriteError(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		userID, email, err := validateToken(tokenString)
		if err != nil {
			WriteError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		ctx = context.WithValue(ctx, "email", email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
