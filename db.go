package main

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

func db() error {
	connStr := "postgresql://postgres:Antriksh%2313@localhost:5432/urlshortener?sslmode=disable"

	dbConn, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if err := dbConn.Ping(); err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("ping database: %w", err)
	}
	conn = dbConn
	return nil
}
