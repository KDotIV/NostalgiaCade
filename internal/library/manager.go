package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type Manager struct {
	db           *sql.DB
	dataDir      string
	mu           sync.Mutex
	ctx          context.Context
	stop         context.CancelFunc
	wg           sync.WaitGroup
	busy         string
	cancel       context.CancelFunc
	closing      bool
	session      Session
	process      runningProcess
	unresolved   bool
	httpClient   *http.Client // Test-only injection; production always uses secureHTTP.
	freeBytes    func(string) (int64, error)
	reserveBytes int64
}

func New(ctx context.Context, db *sql.DB, dataDir string) (*Manager, error) {
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{db: db, dataDir: filepath.Clean(dataDir), ctx: ctx, stop: cancel, freeBytes: availableBytes, reserveBytes: 1024 * 1024 * 1024}
	// Filesystem work is replayable. Never silently resume destructive work after restart.
	if _, err := db.ExecContext(ctx, "UPDATE operations SET state='failed',phase='interrupted',error='Core stopped during operation; retry explicitly to reconcile and continue.' WHERE state IN ('pending','running')"); err != nil {
		cancel()
		return nil, err
	}
	if err := m.recoverSession(ctx); err != nil {
		cancel()
		return nil, err
	}
	// Interrupted partials are never a second hidden game library. Clean only
	// names derived from durable operation IDs, through the owned ROM root.
	if setup, e := m.SetupStatus(ctx); e == nil && setup.Configured {
		_ = m.cleanupPartials(ctx, setup)
	}
	return m, nil
}

func (m *Manager) cleanupPartials(ctx context.Context, s Setup) error {
	root, err := m.openOwned(s.Paths.ROMs)
	if err != nil {
		return err
	}
	defer root.Close()
	rows, err := m.db.QueryContext(ctx, "SELECT id FROM operations WHERE state IN ('failed','canceled','completed')")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if len(id) == 32 && identifier.MatchString(id) {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = root.Remove("content/.partial-" + id); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	emulators, err := m.openOwned(s.Paths.Emulators)
	if err != nil {
		return err
	}
	defer emulators.Close()
	for _, id := range ids {
		for _, suffix := range []string{".archive", ".download"} {
			if err = emulators.Remove(".install-" + id + suffix); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err = emulators.RemoveAll(".install-" + id + ".stage"); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closing = true
	m.stop()
	m.mu.Unlock()
	m.wg.Wait()
	if m.process != nil {
		m.process.Release()
	}
}
func (m *Manager) idle() error {
	if m.closing {
		return fail("closing", "core is shutting down")
	}
	if m.busy != "" {
		return fail("busy", "operation %s is in progress", m.busy)
	}
	if m.process != nil || m.unresolved {
		return fail("game_running", "end or reconcile the current play session before changing the library")
	}
	return nil
}
func (m *Manager) PrepareShutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return err
	}
	m.closing = true
	return nil
}
func (m *Manager) Games(ctx context.Context) ([]Game, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,title,content_path,COALESCE(genre_id,''),COALESCE(emulator_id,''),ownership,scope,COALESCE(sha256,''),size_bytes,COALESCE(artifact_id,'') FROM local_games ORDER BY title,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Game{}
	for rows.Next() {
		var g Game
		if err = rows.Scan(&g.ID, &g.Title, &g.Path, &g.GenreID, &g.EmulatorID, &g.Ownership, &g.Scope, &g.SHA256, &g.Bytes, &g.ArtifactID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (m *Manager) Operation(ctx context.Context, id string) (Operation, error) {
	var op Operation
	err := m.db.QueryRowContext(ctx, "SELECT id,kind,state,phase,error,result_json FROM operations WHERE id=?", id).Scan(&op.ID, &op.Kind, &op.State, &op.Phase, &op.Error, &op.Result)
	if errors.Is(err, sql.ErrNoRows) {
		return op, fail("not_found", "operation does not exist")
	}
	return op, err
}
func (m *Manager) Submit(ctx context.Context, r Request) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Key == "" || len(r.Key) > 128 {
		return Operation{}, fail("invalid_request", "a nonempty idempotency key of at most 128 characters is required")
	}
	valid := (r.Kind == "import" && r.Import != nil && r.Switch == nil && r.Install == nil) ||
		(r.Kind == "switch" && r.Switch != nil && r.Import == nil && r.Install == nil) ||
		(r.Kind == "emulator_install" && r.Install != nil && r.Import == nil && r.Switch == nil)
	if !valid {
		return Operation{}, fail("invalid_request", "expected one import, switch or emulator_install request body")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return Operation{}, err
	}
	var oldID, oldBody string
	err = m.db.QueryRowContext(ctx, "SELECT id,request_json FROM operations WHERE idempotency_key=?", r.Key).Scan(&oldID, &oldBody)
	if err == nil {
		if oldBody != string(body) {
			return Operation{}, fail("idempotency_conflict", "key was already used for a different request")
		}
		return m.Operation(ctx, oldID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	if err = m.idle(); err != nil {
		return Operation{}, err
	}
	setup, err := m.configured(ctx)
	if err != nil {
		return Operation{}, err
	}
	if err = m.cleanupPartials(ctx, setup); err != nil {
		return Operation{}, err
	}
	id := newID()
	if _, err = m.db.ExecContext(ctx, "INSERT INTO operations(id,idempotency_key,kind,state,request_json) VALUES(?,?,?,'pending',?)", id, r.Key, r.Kind, string(body)); err != nil {
		return Operation{}, err
	}
	m.start(id, r)
	return m.Operation(ctx, id)
}
func (m *Manager) Retry(ctx context.Context, id string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return Operation{}, err
	}
	op, err := m.Operation(ctx, id)
	if err != nil {
		return op, err
	}
	if op.State != "failed" && op.State != "canceled" {
		return op, fail("invalid_state", "only failed or canceled operations may be retried")
	}
	var body string
	if err = m.db.QueryRowContext(ctx, "SELECT request_json FROM operations WHERE id=?", id).Scan(&body); err != nil {
		return op, err
	}
	var r Request
	if err = json.Unmarshal([]byte(body), &r); err != nil {
		return op, err
	}
	if _, err = m.db.ExecContext(ctx, "UPDATE operations SET state='pending',error='',phase='queued' WHERE id=?", id); err != nil {
		return op, err
	}
	m.start(id, r)
	return m.Operation(ctx, id)
}
func (m *Manager) Cancel(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy != id || m.cancel == nil {
		return fail("invalid_state", "operation is not running")
	}
	m.cancel()
	return nil
}
func (m *Manager) start(id string, r Request) {
	ctx, cancel := context.WithCancel(m.ctx)
	m.busy = id
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		var err error
		if r.Kind == "emulator_install" {
			err = m.runEmulatorInstall(ctx, id, *r.Install)
		} else if r.Kind == "switch" {
			err = m.runSwitch(ctx, id, *r.Switch)
		} else {
			err = m.runImport(ctx, id, *r.Import)
		}
		state := "completed"
		message := ""
		if err != nil {
			state = "failed"
			message = err.Error()
			if errors.Is(err, context.Canceled) {
				state = "canceled"
				message = "Canceled. Removed old content is not restored; retry to continue."
				if r.Kind == "emulator_install" {
					message = "Package installation canceled; retry to continue. Existing releases are preserved."
				}
			}
		}
		// Hold the admission lock until terminal state is durable.
		m.mu.Lock()
		defer m.mu.Unlock()
		_, persistErr := m.db.ExecContext(context.Background(), "UPDATE operations SET state=?,error=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?", state, message, id)
		if persistErr == nil {
			m.busy = ""
			m.cancel = nil
		} // Fail closed if journaling failed.
	}()
}
func (m *Manager) phase(ctx context.Context, id, phase string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE operations SET state='running',phase=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?", phase, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO operation_steps(operation_id,sequence,state,detail_json) SELECT ?,COALESCE(MAX(sequence),-1)+1,?,'{}' FROM operation_steps WHERE operation_id=?", id, phase, id); err != nil {
		return err
	}
	return tx.Commit()
}
