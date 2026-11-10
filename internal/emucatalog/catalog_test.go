package emucatalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewedSnapshot(t *testing.T) {
	c := Builtin()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Packages) != 5 {
		t.Fatal("expected four console emulators plus one Switch emulator")
	}
	for _, p := range c.Packages {
		if p.Slug == "duckstation" && !strings.Contains(p.DeploymentReview, "Noncommercial") {
			t.Fatal("missing commercial review")
		}
	}
	b, _ := json.Marshal(c)
	if _, err := Read(strings.NewReader(string(b))); err != nil {
		t.Fatal(err)
	}
}
func TestArchivePaths(t *testing.T) {
	for _, name := range []string{"../escape.exe", "/absolute", "C:/escape", `a\..\escape`, "a:stream", "dir/../file", "dir//file", "NUL.txt", "com1.exe", "LPT9", "file.", "file ", "a/CON.cfg"} {
		if SafeName(name) {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"Dolphin-x64/Dolphin.exe", "QtPlugins/platforms/qwindows.dll", "LICENSE.txt"} {
		if !SafeName(name) {
			t.Errorf("rejected %s", name)
		}
	}
}
