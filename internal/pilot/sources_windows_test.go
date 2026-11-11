package pilot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestScanWindowsShortPathRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "source images with a long directory name")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	name := "Sly 2 - Band of Thieves (USA).iso"
	data := []byte("synthetic short-path test image")
	if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	longPath, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(longPath, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || n >= uint32(len(buffer)) {
		t.Fatalf("get short path: length=%d error=%v", n, err)
	}
	shortRoot := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(shortRoot, root) {
		t.Skip("filesystem does not generate 8.3 path aliases")
	}
	bindings, err := Scan(context.Background(), []string{shortRoot})
	if err != nil {
		t.Fatal(err)
	}
	game, found := bindings.Games["sly-2"]
	if !found || len(bindings.Games) != 1 || game.Path != filepath.Join(shortRoot, name) || game.Bytes != int64(len(data)) {
		t.Fatalf("short root %q lost its source: %+v", shortRoot, bindings)
	}
}
