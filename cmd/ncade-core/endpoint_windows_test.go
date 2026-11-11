package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func lockedEndpoint(t *testing.T) (string, string, *os.File) {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "core-endpoint.json")
	source := destination + ".tmp"
	for path, contents := range map[string]string{source: "new endpoint", destination: "stale endpoint"} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	// Confirm the fixture actually blocks Windows replacement.
	err = os.Rename(source, destination)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("expected a sharing conflict, got %v", err)
	}
	return source, destination, reader
}

func TestReplaceEndpointWaitsForReader(t *testing.T) {
	source, destination, reader := lockedEndpoint(t)
	result := make(chan error, 1)
	go func() { result <- replaceEndpoint(t.Context(), source, destination) }()
	select {
	case err := <-result:
		t.Fatalf("replacement finished while reader was open: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "new endpoint" {
		t.Fatalf("replacement = %q, %v", data, err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file remains: %v", err)
	}
}

func TestReplaceEndpointStopsAtDeadlineAndPreservesOldFile(t *testing.T) {
	for _, callerDeadline := range []bool{false, true} {
		name := "publication deadline"
		if callerDeadline {
			name = "caller deadline"
		}
		t.Run(name, func(t *testing.T) {
			source, destination, _ := lockedEndpoint(t)
			ctx := t.Context()
			if callerDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 75*time.Millisecond)
				defer cancel()
			}
			if err := replaceEndpoint(ctx, source, destination); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected publication deadline, got %v", err)
			}
			for path, want := range map[string]string{source: "new endpoint", destination: "stale endpoint"} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatalf("%s = %q, %v", path, data, err)
				}
			}
		})
	}
}

func TestReplaceEndpointReportsMissingSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "missing")
	if err := replaceEndpoint(t.Context(), source, source+".json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing source, got %v", err)
	}
}
