package library

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
)

func packageFixture(t *testing.T, f *testLibrary, name string, body []byte) EmulatorInstall {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(f.base, "package.zip")
	if err = os.WriteFile(archive, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	p := emucatalog.Builtin().Packages[0]
	p.Slug = "dolphin"
	p.Name = "Dolphin fixture"
	p.Version = "test-1"
	p.Format = "zip"
	p.Executable = "Dolphin.exe"
	p.SHA256 = fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))
	p.Bytes = int64(b.Len())
	return EmulatorInstall{Package: p, ArchivePath: archive}
}
func fakePE() []byte {
	b := make([]byte, 128)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[60:], 64)
	copy(b[64:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[68:], 0x8664)
	return b
}

func TestPackageInstallRetentionAndRetry(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	for _, dir := range []string{f.setup.Paths.Saves, f.setup.Paths.Settings, f.setup.Paths.Firmware} {
		write(t, filepath.Join(dir, "keep"), "preserve")
	}
	r := packageFixture(t, f, "Dolphin.exe", fakePE())
	op := submit(t, f, Request{Kind: "emulator_install", Install: &r})
	if op.State != "completed" {
		t.Fatal(op)
	}
	installed, err := f.m.InstalledEmulators(ctx)
	if err != nil || len(installed) != 1 || !installed[0].LaunchSupported {
		t.Fatalf("%+v %v", installed, err)
	}
	if _, err = os.Stat(r.ArchivePath); err != nil {
		t.Fatal("source archive changed", err)
	}
	// Reinstall verifies the existing receipt and preserves user data.
	op = submit(t, f, Request{Kind: "emulator_install", Install: &r})
	if op.State != "completed" {
		t.Fatal(op)
	}
	for _, dir := range []string{f.setup.Paths.Saves, f.setup.Paths.Settings, f.setup.Paths.Firmware} {
		if b, err := os.ReadFile(filepath.Join(dir, "keep")); err != nil || string(b) != "preserve" {
			t.Fatal("persistent data changed", err)
		}
	}
	write(t, installed[0].Executable, "changed executable")
	op = submit(t, f, Request{Kind: "emulator_install", Install: &r})
	if op.State != "failed" {
		t.Fatal("modified package accepted", op)
	}
}

func TestUnsafePackageDoesNotReplaceActiveProfile(t *testing.T) {
	for _, name := range []string{"../outside.exe", "C:/absolute.exe", "Dolphin.exe:stream", "NUL.txt", "Dolphin.exe"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			r := packageFixture(t, f, name, []byte("not a PE executable"))
			op := submit(t, f, Request{Kind: "emulator_install", Install: &r})
			if op.State != "failed" {
				t.Fatal(op)
			}
			var exe string
			if err := f.db.QueryRow("SELECT executable_path FROM emulator_profiles WHERE id='dolphin'").Scan(&exe); err != nil {
				t.Fatal(err)
			}
			if exe != filepath.Join(f.setup.Paths.Emulators, "Dolphin.exe") {
				t.Fatal("failed package changed active profile")
			}
			entries, _ := os.ReadDir(f.setup.Paths.Emulators)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".install-") {
					t.Fatal("failed staging was not cleaned")
				}
			}
		})
	}
}

func TestPackageHashFailureAndInterruptedCleanup(t *testing.T) {
	f := fixture(t)
	r := packageFixture(t, f, "Dolphin.exe", fakePE())
	r.Package.SHA256 = strings.Repeat("0", 64)
	op := submit(t, f, Request{Kind: "emulator_install", Install: &r})
	if op.State != "failed" || !strings.Contains(op.Error, "hash_mismatch") {
		t.Fatal(op)
	}
	stage := filepath.Join(f.setup.Paths.Emulators, ".install-"+op.ID+".stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stage, "interrupted"), "partial")
	if err := f.m.cleanupPartials(context.Background(), f.setup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("journaled staging not cleaned", err)
	}
}
