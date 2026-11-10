package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/database"
)

func main() {
	dialect := flag.String("database", "sqlite", "sqlite or postgres")
	path := flag.String("path", "", "SQLite path (required for sqlite)")
	flag.Parse()
	if err := run(*dialect, *path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dialect, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var db *sql.DB
	var err error
	switch dialect {
	case "sqlite":
		if path == "" {
			return fmt.Errorf("--path is required for SQLite")
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		db, err = database.OpenSQLite(ctx, path)
	case "postgres":
		db, err = database.OpenPostgres(ctx, os.Getenv("NCADE_POSTGRES_DSN"))
	default:
		return fmt.Errorf("--database must be sqlite or postgres")
	}
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := database.Migrate(ctx, db, dialect)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
