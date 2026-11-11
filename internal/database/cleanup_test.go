package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresCleanupMigration(t *testing.T) {
	dsn := os.Getenv("NCADE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(config.Database, "ncade_test_") || (config.Host != "127.0.0.1" && config.Host != "localhost") {
		t.Fatal("disposable loopback PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("ncade_test_cleanup_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
	// Build a new DSN from the already validated loopback configuration.
	testDSN := fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=disable", config.Host, config.Port, config.User, name)
	if config.Password != "" {
		testDSN += " password='" + strings.ReplaceAll(strings.ReplaceAll(config.Password, `\`, `\\`), `'`, `\'`) + "'"
	}
	db, err := OpenPostgres(ctx, testDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, file := range []string{"001_catalog.sql", "002_management.sql", "003_genres.sql", "004_emulator_packages.sql"} {
		body, err := migrations.ReadFile("migrations/postgres/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := os.ReadFile("testdata/legacy_pilot.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, string(legacy)); err != nil {
		t.Fatal(err)
	}
	fixture := `
    CREATE TABLE management.pilot_migrations(version TEXT PRIMARY KEY,sha256 TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now());
    INSERT INTO management.pilot_migrations VALUES('legacy',repeat('b',64),now());
    INSERT INTO catalog.emulators VALUES('00000000-0000-0000-0000-000000000001','test','Test','https://example.org');
    INSERT INTO catalog.artifacts(id,kind,sha256,download_bytes,installed_bytes,format) VALUES('00000000-0000-0000-0000-000000000002','emulator',repeat('a',64),123,456,'zip');
    INSERT INTO catalog.emulator_releases(id,emulator_id,artifact_id,upstream_version,os,architecture,adapter_key) VALUES('00000000-0000-0000-0000-000000000003','00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002','1','windows','x64','test');
    INSERT INTO catalog.emulator_packages VALUES('00000000-0000-0000-0000-000000000003','{"url":"https://example.org/emulator.zip"}',now(),'original-admin');
    INSERT INTO catalog.games(id,title,release_year,created_by_subject) VALUES('00000000-0000-0000-0000-000000000004','Game',2000,'admin');
    INSERT INTO catalog.platforms VALUES('00000000-0000-0000-0000-000000000005','ps2','PS2','console');
    INSERT INTO catalog.game_editions(id,game_id,platform_id,region,revision) VALUES('00000000-0000-0000-0000-000000000006','00000000-0000-0000-0000-000000000004','00000000-0000-0000-0000-000000000005','USA','1');
    INSERT INTO catalog.game_genres VALUES('00000000-0000-0000-0000-000000000004','racing');
    INSERT INTO catalog.suites VALUES('00000000-0000-0000-0000-000000000007','racing','Racing');
    INSERT INTO catalog.suite_releases(id,suite_id,version,created_by_subject) VALUES('00000000-0000-0000-0000-000000000008','00000000-0000-0000-0000-000000000007',1,'admin');
    INSERT INTO catalog.pilot_games VALUES('game','00000000-0000-0000-0000-000000000006','test','{"review":"preserve"}');
    INSERT INTO catalog.pilot_genres VALUES('racing','00000000-0000-0000-0000-000000000008');
    INSERT INTO catalog.artifacts(id,kind,sha256,download_bytes,installed_bytes,format) VALUES('00000000-0000-0000-0000-000000000009','game',repeat('c',64),123,123,'iso');
    INSERT INTO catalog.game_edition_artifacts VALUES('00000000-0000-0000-0000-000000000006','00000000-0000-0000-0000-000000000009','operator-import');
    INSERT INTO catalog.pilot_sources(game_key,artifact_id,filename,reference_only) VALUES('game','00000000-0000-0000-0000-000000000009','game.iso',false);
    INSERT INTO catalog.onboarding_steps VALUES('test','bios','{"note":"preserve"}');
    CREATE VIEW catalog.cleanup_blocker AS SELECT * FROM catalog.pilot_games;`
	if _, err = db.ExecContext(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	if _, err = CleanupCatalog(ctx, db); err == nil {
		t.Fatal("unexpected dependency was deleted")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM catalog.pilot_games`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed cleanup did not roll back", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema='catalog' AND table_name='emulator_releases' AND column_name='manifest'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed DDL leaked", err)
	}
	if _, err = db.Exec(`DROP VIEW catalog.cleanup_blocker`); err != nil {
		t.Fatal(err)
	}
	result, err := CleanupCatalog(ctx, db)
	if err != nil || len(result.Applied) != 1 {
		t.Fatal(result, err)
	}
	result, err = Migrate(ctx, db, "postgres")
	if err != nil || len(result.Applied) != 0 {
		t.Fatal("rerun", result, err)
	}
	var metadata, filename, subject, url string
	if err = db.QueryRow(`SELECT metadata->>'review' FROM catalog.game_editions WHERE game_key='game'`).Scan(&metadata); err != nil || metadata != "preserve" {
		t.Fatal("metadata lost", err)
	}
	if err = db.QueryRow(`SELECT filename FROM catalog.game_edition_artifacts`).Scan(&filename); err != nil || filename != "game.iso" {
		t.Fatal("source lost", err)
	}
	if err = db.QueryRow(`SELECT published_by_subject,manifest->>'url' FROM catalog.emulator_releases`).Scan(&subject, &url); err != nil || subject != "original-admin" || url != "https://example.org/emulator.zip" {
		t.Fatal("package lost", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.suites WHERE selected_release_id IS NOT NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatal("selected release lost", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.legacy_pilot_migrations WHERE version='legacy'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("history lost", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='management' OR (table_schema='public' AND table_name='schema_migrations')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("unwanted schema or fabricated history", err)
	}
	if _, err = db.Exec(`UPDATE catalog.cleanup_migrations SET sha256='changed'`); err != nil {
		t.Fatal(err)
	}
	if _, err = CleanupCatalog(ctx, db); err == nil {
		t.Fatal("modified checksum accepted")
	}
}
