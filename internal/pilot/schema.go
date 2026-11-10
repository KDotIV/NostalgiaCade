package pilot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schema string

// Migrate owns an independent extension ledger because the supplied Azure
// foundation was manually created. It does not invent historical checksums,
// replay foundation DDL or adopt unrelated public-schema cabinet tables.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(72643001)`); err != nil {
		return err
	}
	for _, table := range []string{"catalog.game_editions", "catalog.emulator_packages", "management.cabinets"} {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("foundation table %s is missing; apply foundation migrations on a new database first", table)
		}
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS management.pilot_migrations(version TEXT PRIMARY KEY,sha256 TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now()); REVOKE ALL ON management.pilot_migrations FROM PUBLIC;`); err != nil {
		return err
	}
	const version = "001_installation_catalog"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(schema, "\r\n", "\n"))))
	rows, err := tx.QueryContext(ctx, `SELECT version,sha256 FROM management.pilot_migrations`)
	if err != nil {
		return err
	}
	applied := false
	for rows.Next() {
		var v, h string
		if err = rows.Scan(&v, &h); err != nil {
			rows.Close()
			return err
		}
		if v != version || h != hash {
			rows.Close()
			return fmt.Errorf("unknown or modified pilot migration")
		}
		applied = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !applied {
		// Plain CREATE statements fail transactionally if an untracked table exists.
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO management.pilot_migrations(version,sha256) VALUES($1,$2)`, version, hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}
