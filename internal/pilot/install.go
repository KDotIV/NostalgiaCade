package pilot

import (
	"context"
	"database/sql"
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

func Install(ctx context.Context, db *sql.DB, selection Selection, o InstallOptions, progress func(string)) (journal RunJournal, err error) {
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
	if err = ensureCabinet(ctx, db, cabinetID, o.Cabinet); err != nil {
		return journal, err
	}
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
	// Only non-path selection metadata goes to the remote operation record.
	selected := []string{}
	for _, g := range selection.Manifest.Games {
		selected = append(selected, g.ID)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO management.installation_runs(id,cabinet_id,release_id,created_by_subject,state,selection) VALUES($1,$2,$3,$4,'running',$5)`, journal.ID, cabinetID, selection.ReleaseID, o.Subject, jsonText(map[string]any{"genre": selection.Genre, "partial": selection.Partial, "selectedKeys": selected, "missing": selection.Missing, "catalogEntries": selection.CatalogEntries}))
	if err != nil {
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
		finish, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		code := ""
		if err != nil {
			code = "operator_run_failed_check_local_journal"
		}
		if _, e := db.ExecContext(finish, `UPDATE management.installation_runs SET state=$2,error_code=$3,updated_at=now() WHERE id=$1`, journal.ID, journal.State, code); err == nil && e != nil {
			err = fmt.Errorf("local installation finished; remote report sync failed (journal %s)", journal.ID)
		}
	}()
	submit := func(item string, request library.Request) error {
		index := len(journal.Steps)
		request.Key = "pilot-" + journal.ID + "-" + fmt.Sprint(index)
		journal.Steps = append(journal.Steps, Step{Sequence: index, Kind: request.Kind, Item: item, Operation: library.Operation{State: "planned"}})
		if e := persist(); e != nil {
			return e
		}
		if e := writeStep(ctx, db, journal.ID, journal.Steps[index]); e != nil {
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
				if e := writeStep(ctx, db, journal.ID, journal.Steps[index]); e != nil {
					return fmt.Errorf("remote reporting unavailable; local operation %s may continue; inspect/sync the local journal", op.ID)
				}
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
		for _, step := range Prerequisites() {
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
	if err = Snapshot(ctx, db, o.DataDir, cabinetID, selection.Partial); err != nil {
		return journal, err
	}
	return journal, nil
}

func ensureCabinet(ctx context.Context, db *sql.DB, id, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	org, venue := ID("organization/local-pilot"), ID("venue/local-e2e")
	if _, err = tx.ExecContext(ctx, `INSERT INTO management.organizations(id,name) VALUES($1,'NCade local pilot') ON CONFLICT(id) DO NOTHING`, org); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO management.venues(id,organization_id,name) VALUES($1,$2,'Local E2E') ON CONFLICT(id) DO NOTHING`, venue, org); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO management.cabinets(id,organization_id,venue_id,name,last_seen_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(id) DO UPDATE SET last_seen_at=now()`, id, org, venue, name); err != nil {
		return err
	}
	return tx.Commit()
}
func writeStep(ctx context.Context, db *sql.DB, run string, s Step) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO management.installation_steps(run_id,sequence,kind,local_operation_id,state,phase,detail) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7) ON CONFLICT(run_id,sequence) DO UPDATE SET local_operation_id=excluded.local_operation_id,state=excluded.state,phase=excluded.phase,detail=excluded.detail,updated_at=now()`, run, s.Sequence, s.Kind, s.Operation.ID, s.Operation.State, s.Operation.Phase, jsonText(map[string]any{"item": s.Item, "hasLocalError": s.Operation.Error != ""}))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO management.installation_events(run_id,step_sequence,state,phase) VALUES($1,$2,$3,$4)`, run, s.Sequence, s.Operation.State, s.Operation.Phase)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func Snapshot(ctx context.Context, db *sql.DB, dataDir, cabinet string, partial bool) error {
	var games []library.Game
	var emulators []library.InstalledEmulator
	if err := cli.CallLocal(ctx, dataDir, "library.games", nil, &games); err != nil {
		return err
	}
	if err := cli.CallLocal(ctx, dataDir, "emulators.installed", nil, &emulators); err != nil {
		return err
	}
	profiles := map[string]library.ProfileStatus{}
	for _, e := range emulators {
		var status library.ProfileStatus
		if err := cli.CallLocal(ctx, dataDir, "emulators.profile", library.ProfileRequest{ID: e.Slug}, &status); err != nil {
			return err
		}
		profiles[e.Slug] = status
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Replace only this cabinet's observed inventory, never catalog/source data.
	if _, err = tx.ExecContext(ctx, `DELETE FROM management.cabinet_installations WHERE cabinet_id=$1`, cabinet); err != nil {
		return err
	}
	record := func(kind, key string, body any) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO management.cabinet_installations(cabinet_id,kind,item_key,detail) VALUES($1,$2,$3,$4)`, cabinet, kind, key, jsonText(body))
		return e
	}
	for _, e := range emulators {
		if err = record("emulator", e.Slug, map[string]any{"version": e.Version, "launchSupported": e.LaunchSupported, "deploymentReview": e.DeploymentReview}); err != nil {
			return err
		}
		p := profiles[e.Slug]
		if err = record("firmware", e.Slug, map[string]any{"ready": p.Ready, "prepared": p.Prepared, "missing": p.Missing}); err != nil {
			return err
		}
	}
	for _, g := range games {
		if err = record("game", g.ID, map[string]any{"title": g.Title, "genre": g.GenreID, "emulator": g.EmulatorID, "sha256": g.SHA256, "bytes": g.Bytes, "ownership": g.Ownership, "scope": g.Scope}); err != nil {
			return err
		}
	}
	if err = record("suite", "current", map[string]any{"partialPilotSelection": partial, "installedGameCount": len(games)}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE management.cabinets SET last_seen_at=now(),state_revision=state_revision+1 WHERE id=$1`, cabinet); err != nil {
		return err
	}
	return tx.Commit()
}

// SyncJournal reconciles observed local operations without resuming downloads or
// destructive work. A disconnected coordinator is never assumed to have finished.
func SyncJournal(ctx context.Context, db *sql.DB, dataDir, path string) error {
	var j RunJournal
	if err := ReadJSON(path, &j); err != nil {
		return err
	}
	if _, err := uuid.Parse(j.ID); err != nil {
		return fmt.Errorf("invalid run journal")
	}
	var cabinet string
	if err := db.QueryRowContext(ctx, `SELECT cabinet_id FROM management.installation_runs WHERE id=$1`, j.ID).Scan(&cabinet); err != nil {
		return err
	}
	if cabinet != j.CabinetID {
		return fmt.Errorf("journal cabinet mismatch")
	}
	for i := range j.Steps {
		s := &j.Steps[i]
		if s.Operation.ID != "" {
			if err := cli.CallLocal(ctx, dataDir, "operations.get", map[string]string{"id": s.Operation.ID}, &s.Operation); err != nil {
				return err
			}
		}
		if err := writeStep(ctx, db, j.ID, *s); err != nil {
			return err
		}
	}
	state := "interrupted"
	if len(j.Steps) > 0 {
		last := j.Steps[len(j.Steps)-1]
		if last.Kind == "switch" && last.Operation.State == "completed" {
			state = "completed"
		}
	}
	if err := Snapshot(ctx, db, dataDir, j.CabinetID, j.Partial); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE management.installation_runs SET state=$2,error_code=$3,updated_at=now() WHERE id=$1`, j.ID, state, strings.TrimSpace("reconciled_from_local_operations"))
	return err
}
