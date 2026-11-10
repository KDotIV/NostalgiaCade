package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KdotIV/NostalgiaCade/internal/database"
)

func TestRootLayoutResolvesWithoutCreatingFolders(t *testing.T) {
	for _, suffix := range []string{".ncade", filepath.Join("ROMS.bk", "ncade")} {
		t.Run(suffix, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), suffix)
			setup, err := NormalizeSetup(Setup{Root: root}, root)
			if err != nil {
				t.Fatal(err)
			}
			if setup.Paths.ROMs != filepath.Join(root, "ROMs") || setup.Paths.Saves != filepath.Join(root, "Saves") {
				t.Fatalf("unexpected layout: %+v", setup)
			}
			if _, err = os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("validation created the root")
			}
			if _, err = NormalizeSetup(setup, root); err != nil {
				t.Fatal("resolved setup did not round-trip", err)
			}
		})
	}
}

func TestSetupConflictsNameThePathsAndProtectApplicationState(t *testing.T) {
	base := t.TempDir()
	good, err := NormalizeSetup(Setup{Root: filepath.Join(base, "library")}, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.Root = ""
	bad.Paths.Saves = bad.Paths.ROMs
	_, err = NormalizeSetup(bad, filepath.Join(base, "state"))
	if err == nil || !strings.Contains(err.Error(), "ROMs folder") || !strings.Contains(err.Error(), "Saves folder") || !strings.Contains(err.Error(), good.Paths.ROMs) {
		t.Fatalf("unhelpful conflict: %v", err)
	}
	bad.Paths.Saves = filepath.Join(bad.Paths.ROMs, "Saves")
	if _, err = NormalizeSetup(bad, filepath.Join(base, "state")); err == nil {
		t.Fatal("nested content folders accepted")
	}
	for _, data := range []string{good.Paths.ROMs, filepath.Join(good.Paths.ROMs, "state")} {
		if _, err = NormalizeSetup(good, data); err == nil || !strings.Contains(err.Error(), "application-data folder") {
			t.Fatalf("state inside content accepted: %v", err)
		}
	}
	bad = good
	bad.Paths.Saves = filepath.Join(base, "other-saves")
	if _, err = NormalizeSetup(bad, base); err == nil || !strings.Contains(err.Error(), "invalid_layout") {
		t.Fatalf("conflicting root and individual path accepted: %v", err)
	}
}

func TestRootLayoutCanShareTheApplicationDirectory(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), ".ncade")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := database.OpenSQLite(ctx, filepath.Join(root, "cabinet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = database.Migrate(ctx, db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	m, err := New(ctx, db, root)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	setup, err := m.Configure(ctx, Setup{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range setup.Paths.all() {
		if _, err = os.Stat(filepath.Join(path, ".ncade-owner")); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := m.SetupStatus(ctx)
	if err != nil || stored.Root != root || !stored.Configured {
		t.Fatalf("root not persisted: %+v %v", stored, err)
	}
	if _, err = m.Configure(ctx, stored); err != nil {
		t.Fatal("reapplying root setup failed", err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM genres").Scan(&count); err != nil || count != 5 {
		t.Fatalf("state database was changed: %d %v", count, err)
	}
}
