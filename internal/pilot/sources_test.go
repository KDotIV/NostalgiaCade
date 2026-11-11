package pilot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanDoesNotFollowLinkedSources(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			name := "Sly 2 - Band of Thieves (USA).iso"
			image := filepath.Join(outside, name)
			if err := os.WriteFile(image, []byte("outside the selected root"), 0600); err != nil {
				t.Fatal(err)
			}
			target, link := image, filepath.Join(root, name)
			if kind == "directory" {
				target, link = outside, filepath.Join(root, "linked games")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}
			bindings, err := Scan(context.Background(), []string{root})
			if err != nil {
				t.Fatal(err)
			}
			if len(bindings.Games) != 0 {
				t.Fatalf("linked source was scanned: %+v", bindings)
			}
		})
	}
}
