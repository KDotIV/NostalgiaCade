package database

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
)

func TestFoundationUpgradePreservesExistingLocalRows(t *testing.T) {
	db := localDB(t)
	ctx := context.Background()
	previous := fstest.MapFS{}
	for _, name := range []string{"001_local_state.sql", "002_genres.sql"} {
		path := "migrations/sqlite/" + name
		body, err := fs.ReadFile(migrations, path)
		if err != nil {
			t.Fatal(err)
		}
		previous[path] = &fstest.MapFile{Data: body}
	}
	if _, err := migrateFS(ctx, db, "sqlite", previous); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO local_games(id,title,content_path) VALUES('legacy','Keep me','C:/original.dol')"); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(ctx, db, "sqlite")
	if err != nil || len(result.Applied) != 2 {
		t.Fatalf("upgrade: %+v %v", result, err)
	}
	var title, path, scope string
	if err = db.QueryRow("SELECT title,content_path,scope FROM local_games WHERE id='legacy'").Scan(&title, &path, &scope); err != nil || title != "Keep me" || path != "C:/original.dol" || scope != "local" {
		t.Fatalf("legacy row lost: %s %s %s %v", title, path, scope, err)
	}
}

func localDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "cabinet space.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteMigrationsAndConstraints(t *testing.T) {
	db := localDB(t)
	ctx := context.Background()
	first, err := Migrate(ctx, db, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Applied) != 4 {
		t.Fatalf("applied %v", first.Applied)
	}
	second, err := Migrate(ctx, db, "sqlite")
	if err != nil || len(second.Applied) != 0 || second.Version != first.Version {
		t.Fatalf("rerun: %v %v", second, err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM genres").Scan(&count); err != nil || count != 5 {
		t.Fatalf("genres: %d %v", count, err)
	}
	if _, err := db.Exec("INSERT INTO local_games(id,title,content_path,ownership) VALUES('1','Game','C:/game','unknown')"); err == nil {
		t.Fatal("invalid ownership accepted")
	}
	if _, err := db.Exec("INSERT INTO operation_steps(operation_id,sequence,state,detail_json) VALUES('absent',0,'pending','{}')"); err == nil {
		t.Fatal("foreign keys disabled")
	}
	if _, err := db.Exec("INSERT INTO settings(key,value) VALUES('bad','not-json')"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := db.Exec("INSERT INTO local_games(title,content_path) VALUES('Missing ID','C:/missing')"); err == nil {
		t.Fatal("null primary key accepted")
	}
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version='001_local_state.sql'"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, db, "sqlite"); err == nil || !strings.Contains(err.Error(), "history has a gap") {
		t.Fatalf("missing historical migration should be rejected: %v", err)
	}
}

func TestMigrationRollbackAndHistoryProtection(t *testing.T) {
	ctx := context.Background()
	db := localDB(t)
	files := fstest.MapFS{
		"migrations/sqlite/001_first.sql":  {Data: []byte("CREATE TABLE example(id INTEGER PRIMARY KEY);")},
		"migrations/sqlite/002_broken.sql": {Data: []byte("THIS IS NOT SQL;")},
	}
	if _, err := migrateFS(ctx, db, "sqlite", files); err == nil {
		t.Fatal("broken migration passed")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'example'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("DDL not rolled back: %d %v", count, err)
	}
	delete(files, "migrations/sqlite/002_broken.sql")
	if _, err := migrateFS(ctx, db, "sqlite", files); err != nil {
		t.Fatal(err)
	}
	files["migrations/sqlite/001_first.sql"].Data = []byte("CREATE TABLE example(id TEXT PRIMARY KEY);")
	if _, err := migrateFS(ctx, db, "sqlite", files); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("checksum protection: %v", err)
	}
	delete(files, "migrations/sqlite/001_first.sql")
	files["migrations/sqlite/002_other.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;")}
	if _, err := migrateFS(ctx, db, "sqlite", files); err == nil || !strings.Contains(err.Error(), "unknown migration") {
		t.Fatalf("future schema protection: %v", err)
	}
}

func TestSQLiteConcurrentMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	first, err := OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, db := range []*sql.DB{first, second} {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := Migrate(context.Background(), db, "sqlite"); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresMigrations(t *testing.T) {
	dsn := os.Getenv("NCADE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set NCADE_TEST_POSTGRES_DSN to a disposable local ncade_test_* database")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if !strings.HasPrefix(config.Database, "ncade_test_") || (config.Host != "127.0.0.1" && config.Host != "localhost") {
		t.Fatal("PostgreSQL tests require a disposable loopback ncade_test_* database")
	}
	ctx := context.Background()
	db, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := Migrate(ctx, db, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Applied) != 4 {
		t.Fatalf("expected clean test database, applied %v", first.Applied)
	}
	second, err := Migrate(ctx, db, "postgres")
	if err != nil || len(second.Applied) != 0 {
		t.Fatalf("rerun: %v %v", second, err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM catalog.genres").Scan(&count); err != nil || count != 5 {
		t.Fatalf("genres: %d %v", count, err)
	}
	if _, err := db.Exec("INSERT INTO catalog.games(id,title,release_year,players_min,players_max,created_by_subject) VALUES('00000000-0000-0000-0000-000000000001','Bad',2000,2,1,'test')"); err == nil {
		t.Fatal("invalid player bounds accepted")
	}
	if _, err := db.Exec("INSERT INTO management.cabinets(id,organization_id,venue_id,name) VALUES('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000003','Cabinet')"); err == nil {
		t.Fatal("invalid tenant reference accepted")
	}
	if err := db.QueryRow("SELECT count(*) FROM management.memberships").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration must not create an administrator account")
	}
	// CHECK constraints must reject NULL, rather than accepting SQL's unknown.
	if _, err := db.Exec(`INSERT INTO catalog.artifacts(id,kind,sha256,download_bytes,installed_bytes,format)
		VALUES('00000000-0000-0000-0000-000000000010','game',repeat('a',64),1,1,'raw');
		INSERT INTO catalog.suites(id,genre_id,name)
		VALUES('00000000-0000-0000-0000-000000000011','racing','Test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO catalog.artifact_sources(id,artifact_id,mode)
		VALUES('00000000-0000-0000-0000-000000000012','00000000-0000-0000-0000-000000000010','authorized_url')`); err == nil {
		t.Fatal("authorized URL without a URL accepted")
	}
	if _, err := db.Exec(`INSERT INTO catalog.suite_releases(id,suite_id,version,status,signature,published_at,created_by_subject)
		VALUES('00000000-0000-0000-0000-000000000013','00000000-0000-0000-0000-000000000011',1,'published','test',now(),'test')`); err == nil {
		t.Fatal("published manifest without a hash accepted")
	}
}
