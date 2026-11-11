package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/cli"
	"github.com/KdotIV/NostalgiaCade/internal/library"
	"github.com/google/uuid"
)

type InstallOptions struct{ DataDir, Root, Cabinet, Subject, Archives, FirmwareFrom string }
type RunJournal struct {
	ID        string `json:"id"`
	CabinetID string `json:"cabinetId"`
	ReleaseID string `json:"releaseId"`
	State     string `json:"state"`
	Genre     string `json:"genre"`
	Partial   bool   `json:"partial"`
	Steps     []Step `json:"steps"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}
type Step struct {
	Sequence  int               `json:"sequence"`
	Kind      string            `json:"kind"`
	Item      string            `json:"item"`
	Operation library.Operation `json:"operation"`
}

func Install(ctx context.Context, selection Selection, o InstallOptions, progress func(string)) (journal RunJournal, err error) {
	if o.Subject == "" || o.Cabinet == "" || len(o.Cabinet) > 100 || o.DataDir == "" {
		return journal, fmt.Errorf("cabinet, subject and data directory are required")
	}
	dataDir, e := filepath.Abs(o.DataDir)
	if e != nil {
		return journal, e
	}
	o.DataDir = dataDir
	call := func(method string, params, result any) error {
		return cli.CallLocal(ctx, o.DataDir, method, params, result)
	}
	var setup library.Setup
	if err = call("setup.status", nil, &setup); err != nil {
		return journal, err
	}
	if !setup.Configured {
		if o.Root == "" {
			return journal, fmt.Errorf("run setup or specify --root for this new cabinet")
		}
		if err = call("setup.configure", library.Setup{Root: o.Root}, &setup); err != nil {
			return journal, err
		}
	}
	var session library.Session
	if err = call("session.status", nil, &session); err != nil {
		return journal, err
	}
	if session.State != "idle" && session.State != "exited" {
		return journal, fmt.Errorf("close the active emulator before installation")
	}
	// Distinct local installations must not overwrite each other's inventory,
	// even if an operator reuses a display name. No filesystem path is uploaded.
	host, e := os.Hostname()
	if e != nil {
		return journal, e
	}
	cabinetID := ID("cabinet/" + host + "/" + strings.ToLower(dataDir) + "/" + o.Cabinet)
	journal = RunJournal{ID: uuid.NewString(), CabinetID: cabinetID, ReleaseID: selection.ReleaseID, State: "running", Genre: selection.Genre, Partial: selection.Partial, Steps: []Step{}}
	journalDir := filepath.Join(o.DataDir, "pilot-runs")
	if err = os.MkdirAll(journalDir, 0700); err != nil {
		return journal, err
	}
	journalPath := filepath.Join(journalDir, journal.ID+".json")
	persist := func() error {
		journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		b, e := json.MarshalIndent(journal, "", "  ")
		if e != nil {
			return e
		}
		temporary := journalPath + ".tmp"
		if e = os.WriteFile(temporary, b, 0600); e != nil {
			return e
		}
		return os.Rename(temporary, journalPath)
	}
	if err = persist(); err != nil {
		return journal, err
	}
	defer func() {
		if err != nil {
			journal.State = "failed"
			journal.Error = err.Error()
		} else {
			journal.State = "completed"
		}
		if e := persist(); err == nil && e != nil {
			err = e
		}
	}()
	submit := func(item string, request library.Request) error {
		index := len(journal.Steps)
		request.Key = "pilot-" + journal.ID + "-" + fmt.Sprint(index)
		journal.Steps = append(journal.Steps, Step{Sequence: index, Kind: request.Kind, Item: item, Operation: library.Operation{State: "planned"}})
		if e := persist(); e != nil {
			return e
		}
		var op library.Operation
		if e := call("operations.submit", request, &op); e != nil {
			return e
		}
		previous := ""
		for {
			journal.Steps[index].Operation = op
			if e := persist(); e != nil {
				return e
			}
			state := op.State + "/" + op.Phase
			if state != previous {
				progress(item + ": " + state)
				previous = state
			}
			if op.State == "completed" {
				return nil
			}
			if op.State == "failed" || op.State == "canceled" {
				return fmt.Errorf("%s: %s (local operation %s)", item, op.Error, op.ID)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("operator disconnected; local operation %s may continue", op.ID)
			case <-time.After(500 * time.Millisecond):
			}
			if e := call("operations.get", map[string]string{"id": op.ID}, &op); e != nil {
				return e
			}
		}
	}
	for _, p := range selection.Emulators.Packages {
		archive := ""
		if o.Archives != "" {
			archive = filepath.Join(o.Archives, p.Slug+"."+p.Format)
		}
		if err = submit(p.Slug, library.Request{Kind: "emulator_install", Install: &library.EmulatorInstall{Package: p, ArchivePath: archive}}); err != nil {
			return journal, err
		}
		if err = submit(p.Slug+" profile", library.Request{Kind: "emulator_prepare", Prepare: &library.ProfileRequest{ID: p.Slug}}); err != nil {
			return journal, err
		}
	}
	if o.FirmwareFrom != "" {
		for _, step := range selection.Onboarding {
			source := filepath.Join(o.FirmwareFrom, filepath.FromSlash(step.RelativePath))
			if err = submit(step.Emulator+" "+step.Kind, library.Request{Kind: "firmware_import", Firmware: &library.FirmwareRequest{ID: step.Emulator, Kind: step.Kind, Path: source}}); err != nil {
				return journal, err
			}
		}
	}
	for _, p := range selection.Emulators.Packages {
		var state library.ProfileStatus
		if err = call("emulators.profile", library.ProfileRequest{ID: p.Slug}, &state); err != nil {
			return journal, err
		}
		if !state.Ready {
			return journal, fmt.Errorf("%s prerequisites are missing; complete emulator onboarding before game installation", p.Slug)
		}
	}
	var plan library.Plan
	if err = call("library.plan", selection.Manifest, &plan); err != nil {
		return journal, err
	}
	progress(fmt.Sprintf("genre %s: remove %d managed-suite entries first, acquire %d bytes, retain saves/settings/emulators", selection.Genre, len(plan.RemoveGames), plan.AcquireBytes))
	if err = submit(selection.Genre, library.Request{Kind: "switch", Switch: &library.SwitchRequest{Manifest: selection.Manifest, ExpectedRevision: plan.Revision}}); err != nil {
		return journal, err
	}
	return journal, nil
}

// SyncJournal observes local operations and updates the local journal only.
func SyncJournal(ctx context.Context, dataDir, path string) error {
	var j RunJournal
	if err := ReadJSON(path, &j); err != nil {
		return err
	}
	if _, err := uuid.Parse(j.ID); err != nil {
		return fmt.Errorf("invalid run journal")
	}
	// Every operation must be resolved by this cabinet's core. Missing IDs fail.
	for i := range j.Steps {
		step := &j.Steps[i]
		if step.Operation.ID != "" {
			if err := cli.CallLocal(ctx, dataDir, "operations.get", map[string]string{"id": step.Operation.ID}, &step.Operation); err != nil {
				return err
			}
		}
	}
	j.State = "interrupted"
	if len(j.Steps) > 0 {
		last := j.Steps[len(j.Steps)-1]
		if last.Kind == "switch" && last.Operation.State == "completed" {
			j.State = "completed"
			j.Error = ""
		}
	}
	j.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	body, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", body, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
