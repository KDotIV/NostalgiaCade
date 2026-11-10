package emucatalog

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/KdotIV/NostalgiaCade/internal/database"
)

func TestPostgresCatalogPublication(t *testing.T) {
	dsn := os.Getenv("NCADE_TEST_POSTGRES_DSN")
	if dsn == "" || os.Getenv("NCADE_TEST_CATALOG") != "1" {
		t.Skip("isolated PostgreSQL test server required")
	}
	ctx := context.Background()
	db, err := database.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := Builtin()
	for i := 0; i < 2; i++ {
		if err = Publish(ctx, db, c, "test-admin"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Export(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != 5 {
		t.Fatal("catalog round-trip lost entries")
	}
	if err = got.Validate(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM catalog.emulator_releases`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("duplicate releases: %d %v", count, err)
	}
	c.Packages[0].SHA256 = strings.Repeat("0", 64)
	if err = Publish(ctx, db, c, "test-admin"); err == nil {
		t.Fatal("changed release was silently replaced")
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifacts WHERE kind='emulator'`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("failed publication leaked an artifact: %d %v", count, err)
	}
}
