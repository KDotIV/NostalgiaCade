package pilot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/database"
	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"github.com/jackc/pgx/v5"
)

func TestCandidatesAndSourceScan(t *testing.T) {
	counts := map[string]int{}
	keys := map[string]bool{}
	for _, g := range Candidates() {
		if keys[g.Key] || g.Year < 1990 || g.Year > 2015 || g.Emulator == "" || g.PlayersMax < 1 {
			t.Fatalf("invalid candidate: %+v", g)
		}
		keys[g.Key] = true
		counts[g.Genre]++
	}
	if len(keys) != 100 {
		t.Fatal("expected 100 distinct candidates")
	}
	for _, genre := range GenreOrder {
		if counts[genre] != 20 {
			t.Fatal(genre, counts[genre])
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "Sly 2 - Band of Thieves (USA).iso")
	if err := os.WriteFile(path, []byte("synthetic test image"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Scan(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Games) != 1 || b.Games["sly-2"].Path != path {
		t.Fatal(b)
	}
	duplicate := filepath.Join(root, "Sly 2 - Band of Thieves (Europe).iso")
	os.WriteFile(duplicate, []byte("different"), 0600)
	if _, err = Scan(context.Background(), []string{root}); err == nil {
		t.Fatal("ambiguous sources accepted")
	}
}
func TestPostgresPilot(t *testing.T) {
	dsn := os.Getenv("NCADE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(config.Database, "ncade_test_") || (config.Host != "127.0.0.1" && config.Host != "localhost") {
		t.Fatal("pilot tests require a disposable loopback database")
	}
	ctx := context.Background()
	db, err := database.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = database.Migrate(ctx, db, "postgres"); err != nil {
		t.Fatal(err)
	}
	if err = emucatalog.Publish(ctx, db, emucatalog.Builtin(), "test-admin"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "Sly 2 - Band of Thieves (USA).iso")
	data := []byte("synthetic pilot image")
	var beforeArtifacts int
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifacts WHERE kind='game'`).Scan(&beforeArtifacts); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, data, 0600)
	b := Bindings{Version: 1, Games: map[string]Binding{"sly-2": {Path: path, Filename: filepath.Base(path), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Bytes: int64(len(data)), Format: "iso"}}}
	for i := 0; i < 2; i++ {
		if err = Seed(ctx, db, "test-admin", b); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM catalog.pilot_games`).Scan(&count); err != nil || count != 100 {
		t.Fatalf("games %d: %v", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.suite_games`).Scan(&count); err != nil || count != 100 {
		t.Fatalf("suite entries %d: %v", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifacts WHERE kind='game'`).Scan(&count); err != nil || count != beforeArtifacts+1 {
		t.Fatal("invented or duplicated game artifacts", count, err)
	}
	plan, err := Plan(ctx, db, "platform", b, false)
	if err == nil || len(plan.Missing) != 19 {
		t.Fatal("strict plan should reject missing sources", err)
	}
	plan, err = Plan(ctx, db, "platform", b, true)
	if err != nil || !plan.Partial || len(plan.Manifest.Games) != 1 {
		t.Fatal(plan, err)
	}
	if _, err = Plan(ctx, db, "racing", b, true); err == nil {
		t.Fatal("empty genre must not become installable")
	}
	observation := FileResearch{GameKey: "burnout-3", SourcePage: "https://archive.org/details/test-metadata", IndexPage: "https://r-roms.github.io/Sony/sony-playstation-2", Filename: "Burnout 3 (USA).zip", Bytes: 10, SHA1: strings.Repeat("a", 40), MD5: strings.Repeat("b", 32), Container: "ZIP", Region: "USA", ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	if err = PublishResearch(ctx, db, "test-admin", []FileResearch{observation}); err != nil {
		t.Fatal(err)
	}
	if _, err = Plan(ctx, db, "racing", b, true); err == nil {
		t.Fatal("publisher metadata must not authorize installation")
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifacts WHERE kind='game'`).Scan(&count); err != nil || count != beforeArtifacts+1 {
		t.Fatal("research manufactured an installable artifact", count, err)
	}
	if _, err = Report(ctx, db); err != nil {
		t.Fatal(err)
	}
	observation.SourcePage = "https://archive.org/details/test?token=secret"
	if err = observation.Validate(); err == nil {
		t.Fatal("token-bearing research reference accepted")
	}
	// The extension ledger must not manufacture foundation migration history.
	if err = db.QueryRow(`SELECT count(*) FROM public.schema_migrations`).Scan(&count); err != nil || count != 4 {
		t.Fatal("foundation history changed", count, err)
	}
	var hash string
	db.QueryRow(`SELECT sha256 FROM management.pilot_migrations`).Scan(&hash)
	db.Exec(`UPDATE management.pilot_migrations SET sha256='changed'`)
	if err = Migrate(ctx, db); err == nil {
		t.Fatal("modified migration accepted")
	}
	db.Exec(`UPDATE management.pilot_migrations SET sha256=$1`, hash)
}
