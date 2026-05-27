package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
)

var clickConn *sql.DB

func db() error {
	connStr := os.Getenv("DATABASE_URL")


	dbConn, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	dbConn.SetMaxOpenConns(25)
	dbConn.SetMaxIdleConns(5)
	dbConn.SetConnMaxLifetime(5 * time.Minute)

	if err := dbConn.Ping(); err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("ping database: %w", err)
	}
	conn = dbConn

	// Analytics pool
	clickPool, err := sql.Open("postgres", connStr)
	if err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("open analytics database: %w", err)
	}
	clickPool.SetMaxOpenConns(5)
	clickPool.SetMaxIdleConns(2)
	clickPool.SetConnMaxLifetime(5 * time.Minute)

	if err := clickPool.Ping(); err != nil {
		_ = clickPool.Close()
		_ = dbConn.Close()
		return fmt.Errorf("ping analytics database: %w", err)
	}
	clickConn = clickPool

	return nil
}
