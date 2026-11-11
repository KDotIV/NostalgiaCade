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
	cleanup := flag.Bool("cleanup-catalog", false, "Consolidate the existing PostgreSQL catalog, including a manually provisioned foundation")
	flag.Parse()
	if *cleanup {
		if *dialect != "postgres" {
			fmt.Fprintln(os.Stderr, "--cleanup-catalog requires --database postgres")
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		db, err := database.OpenPostgres(ctx, os.Getenv("NCADE_POSTGRES_DSN"))
		if err == nil {
			defer db.Close()
			var result database.MigrationResult
			result, err = database.CleanupCatalog(ctx, db)
			if err == nil {
				err = json.NewEncoder(os.Stdout).Encode(result)
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Catalog cleanup failed; no changes committed. Check schema, privileges and migration history.")
			os.Exit(1)
		}
		return
	}
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
