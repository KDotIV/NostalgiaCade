package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scriptedPrompt(t *testing.T, answers []string, out *bytes.Buffer) func(string) (string, error) {
	t.Helper()
	i := 0
	return func(label string) (string, error) {
		fmt.Fprintln(out, label)
		if i >= len(answers) {
			return "", fmt.Errorf("unexpected extra prompt: %s", label)
		}
		answer := answers[i]
		i++
		return answer, nil
	}
}

func TestSetupGuideRepairsOverlapBeforeConfirmation(t *testing.T) {
	base := t.TempDir()
	repeated := filepath.Join(base, "same")
	root := filepath.Join(base, ".ncade")
	answers := []string{"2", repeated, repeated, repeated, repeated, repeated, repeated, "1", root, "", "yes"}
	var output bytes.Buffer
	setup, err := setupGuide(filepath.Join(base, "state"), scriptedPrompt(t, answers, &output), &output)
	if err != nil {
		t.Fatal(err)
	}
	if setup.Root != root || setup.Paths.ROMs != filepath.Join(root, "ROMs") {
		t.Fatal(setup)
	}
	text := output.String()
	conflict, confirmation := strings.Index(text, "overlapping_paths"), strings.Index(text, "Create/use these folders?")
	if conflict < 0 || confirmation < conflict || strings.Count(text, "Create/use these folders?") != 1 {
		t.Fatal("confirmation preceded validation", text)
	}
	for _, path := range []string{root, repeated} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("guide created files before core configuration")
		}
	}
}

func TestSetupGuideSupportsIndividualPathsAndCancellation(t *testing.T) {
	base := t.TempDir()
	answers := []string{"2"}
	for _, name := range []string{"Emulators", "ROMs", "Firmware", "Saves", "Settings", "Cache"} {
		answers = append(answers, filepath.Join(base, name))
	}
	answers = append(answers, "downloads.example.org", "yes")
	var output bytes.Buffer
	setup, err := setupGuide(filepath.Join(base, "state"), scriptedPrompt(t, answers, &output), &output)
	if err != nil || setup.Root != "" || setup.Paths.Saves != filepath.Join(base, "Saves") {
		t.Fatalf("%+v %v", setup, err)
	}
	output.Reset()
	if _, err = setupGuide(base, scriptedPrompt(t, []string{"q"}, &output), &output); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancel: %v", err)
	}
}
