package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*/*.sql
var migrations embed.FS

type MigrationResult struct {
	Version string   `json:"version"`
	Applied []string `json:"applied"`
}

// Migrate runs immutable, ordered migrations in one transaction. The lock spans
// both the history check and DDL. Unknown or edited applied versions fail closed.
func Migrate(ctx context.Context, db *sql.DB, dialect string) (MigrationResult, error) {
	return migrateFS(ctx, db, dialect, migrations)
}

func migrateFS(ctx context.Context, db *sql.DB, dialect string, files fs.FS) (result MigrationResult, err error) {
	result.Applied = []string{}
	if dialect != "sqlite" && dialect != "postgres" {
		return result, fmt.Errorf("unsupported migration dialect")
	}
	paths, err := fs.Glob(files, "migrations/"+dialect+"/*.sql")
	if err != nil || len(paths) == 0 {
		return result, fmt.Errorf("no migrations for %s", dialect)
	}
	sort.Strings(paths)
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	begin := "BEGIN"
	if dialect == "sqlite" {
		begin = "BEGIN IMMEDIATE"
	}
	if _, err = conn.ExecContext(ctx, begin); err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if dialect == "postgres" {
		if _, err = conn.ExecContext(ctx, "SELECT pg_advisory_xact_lock(72643001)"); err != nil {
			return result, err
		}
	}
	_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT NOT NULL PRIMARY KEY, sha256 TEXT NOT NULL, applied_at TEXT NOT NULL)`)
	if err != nil {
		return result, err
	}
	rows, err := conn.QueryContext(ctx, "SELECT version, sha256 FROM schema_migrations")
	if err != nil {
		return result, err
	}
	known := map[string]string{}
	for rows.Next() {
		var version, hash string
		if err = rows.Scan(&version, &hash); err != nil {
			rows.Close()
			return result, err
		}
		known[version] = hash
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	available := map[string]bool{}
	for _, path := range paths {
		available[strings.TrimPrefix(path, "migrations/"+dialect+"/")] = true
	}
	for version := range known {
		if !available[version] {
			return result, fmt.Errorf("database contains unknown migration %s; a newer application may be required", version)
		}
	}
	missingEarlier := false
	for _, path := range paths {
		version := strings.TrimPrefix(path, "migrations/"+dialect+"/")
		if _, exists := known[version]; !exists {
			missingEarlier = true
		} else if missingEarlier {
			return result, fmt.Errorf("migration history has a gap before %s; append new migrations after existing versions", version)
		}
	}
	for _, path := range paths {
		version := strings.TrimPrefix(path, "migrations/"+dialect+"/")
		body, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			return result, readErr
		}
		// Git may check out SQL files with CRLF; normalize before checksumming.
		body = []byte(strings.ReplaceAll(string(body), "\r\n", "\n"))
		hash := fmt.Sprintf("%x", sha256.Sum256(body))
		if previous, exists := known[version]; exists {
			if previous != hash {
				return result, fmt.Errorf("applied migration %s was modified", version)
			}
		} else {
			if _, err = conn.ExecContext(ctx, string(body)); err != nil {
				return result, fmt.Errorf("migration %s failed: %w", version, err)
			}
			insert := "INSERT INTO schema_migrations(version, sha256, applied_at) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))"
			if dialect == "postgres" {
				insert = "INSERT INTO schema_migrations(version, sha256, applied_at) VALUES ($1, $2, CURRENT_TIMESTAMP::text)"
			}
			if _, err = conn.ExecContext(ctx, insert, version, hash); err != nil {
				return result, err
			}
			result.Applied = append(result.Applied, version)
		}
		result.Version = version
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return result, err
}
