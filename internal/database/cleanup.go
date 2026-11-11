package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
)

const cleanupVersion = "005_catalog_cleanup.sql"

// CleanupCatalog upgrades a manually provisioned foundation without fabricating
// its migration history. Normal, tracked installations use Migrate instead.
func CleanupCatalog(ctx context.Context, db *sql.DB) (MigrationResult, error) {
	var tracked bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&tracked); err != nil {
		return MigrationResult{}, err
	}
	if tracked {
		return Migrate(ctx, db, "postgres")
	}
	result := MigrationResult{Version: cleanupVersion, Applied: []string{}}
	body, err := migrations.ReadFile("migrations/postgres/" + cleanupVersion)
	if err != nil {
		return result, err
	}
	body = []byte(strings.ReplaceAll(string(body), "\r\n", "\n"))
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(72643001)`); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS catalog.cleanup_migrations(version TEXT PRIMARY KEY,sha256 TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now()); REVOKE ALL ON catalog.cleanup_migrations FROM PUBLIC`); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT version,sha256 FROM catalog.cleanup_migrations`)
	if err != nil {
		return result, err
	}
	applied := false
	for rows.Next() {
		var version, previous string
		if err = rows.Scan(&version, &previous); err != nil {
			rows.Close()
			return result, err
		}
		if version != cleanupVersion || previous != hash {
			rows.Close()
			return result, fmt.Errorf("unknown or modified catalog cleanup migration")
		}
		applied = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if !applied {
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.cleanup_migrations(version,sha256) VALUES($1,$2)`, cleanupVersion, hash); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, cleanupVersion)
	}
	return result, tx.Commit()
}
