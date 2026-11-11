// Package postgres owns remote catalog connections without pulling cabinet SQLite
// or migrations into the hosted API executable.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("NCADE_POSTGRES_DSN is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL configuration")
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot connect to PostgreSQL; check configuration, TLS and network access")
	}
	return db, nil
}

// CheckCatalog validates connectivity and the columns required by the API. It
// never runs DDL: schema changes are a separate deployment step.
func CheckCatalog(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT a.created_by_subject,e.game_key,r.manifest,o.step_key FROM catalog.artifacts a,catalog.game_editions e,catalog.emulator_releases r,catalog.onboarding_steps o WHERE false`)
	if err != nil {
		return fmt.Errorf("catalog is unavailable or requires migration")
	}
	return rows.Close()
}
