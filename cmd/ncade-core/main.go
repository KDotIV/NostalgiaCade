package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/core"
	"github.com/KdotIV/NostalgiaCade/internal/database"
	"github.com/KdotIV/NostalgiaCade/internal/ipc"
	"github.com/KdotIV/NostalgiaCade/internal/library"
	"github.com/gofrs/flock"
)

func main() {
	dataDir := flag.String("data-dir", "", "Local cabinet data directory (required)")
	flag.Parse()
	if err := run(*dataDir); err != nil {
		slog.Error("core stopped", "error", err)
		os.Exit(1)
	}
}

func run(dataDir string) error {
	if dataDir == "" {
		return fmt.Errorf("--data-dir is required")
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(abs, "core.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("a core already owns this data directory")
	}
	defer lock.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	db, err := database.OpenSQLite(ctx, filepath.Join(abs, "cabinet.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := database.Migrate(ctx, db, "sqlite")
	if err != nil {
		return err
	}
	manager, err := library.New(ctx, db, abs)
	if err != nil {
		return err
	}
	defer manager.Close()
	name := fmt.Sprintf("NostalgiaCade.%x", randomID())
	listener, err := ipc.Listen(name)
	if err != nil {
		return err
	}
	defer listener.Close()
	endpoint := struct {
		PipeName        string `json:"pipeName"`
		ProcessID       int    `json:"processId"`
		ProtocolVersion int    `json:"protocolVersion"`
	}{name, os.Getpid(), core.ProtocolVersion}
	payload, _ := json.Marshal(endpoint)
	endpointPath := filepath.Join(abs, "core-endpoint.json")
	tempPath := endpointPath + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0600); err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := replaceEndpoint(ctx, tempPath, endpointPath); err != nil {
		return err
	}
	defer os.Remove(endpointPath)
	service := &core.Service{DB: db, DatabaseVersion: result.Version, StartedAt: time.Now(), Library: manager}
	slog.Info("core ready", "protocol", core.ProtocolVersion, "schema", result.Version)
	return (&ipc.Server{Service: service, Shutdown: cancel}).Serve(ctx, listener)
}

func randomID() []byte {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return id
}
