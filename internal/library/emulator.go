package library

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type runningProcess interface {
	PID() int
	Identity() string
	Wait(context.Context) (int, error)
	Stop(bool) error
	Release()
}

func (m *Manager) RegisterEmulator(ctx context.Context, e Emulator) (Emulator, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return e, err
	}
	s, err := m.configured(ctx)
	if err != nil {
		return e, err
	}
	if !identifier.MatchString(e.ID) || !supportedAdapter(e.Adapter) || strings.TrimSpace(e.Version) == "" || len(e.Version) > 100 {
		return e, fail("invalid_emulator", "specify a profile ID, supported adapter and version label")
	}
	e.Executable, err = cleanPath(e.Executable)
	if err != nil {
		return e, err
	}
	if !contained(s.Paths.Emulators, e.Executable) || !strings.EqualFold(filepath.Ext(e.Executable), ".exe") {
		return e, fail("invalid_emulator", "select the actual emulator .exe inside the chosen emulators folder")
	}
	f, err := os.Open(e.Executable)
	if err != nil {
		return e, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return e, err
	}
	if !st.Mode().IsRegular() {
		return e, fail("invalid_emulator", "executable must be a regular file")
	}
	hash, _, err := hashStream(ctx, f, nil, 1024*1024*1024, nil)
	if err != nil {
		return e, err
	}
	var existing string
	if queryErr := m.db.QueryRowContext(ctx, "SELECT adapter FROM emulator_profiles WHERE id=?", e.ID).Scan(&existing); queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
		return e, queryErr
	}
	if existing != "" && existing != e.Adapter {
		return e, fail("adapter_conflict", "a profile's adapter cannot be changed")
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO emulator_profiles(id,adapter,executable_path,executable_sha256,version_label) VALUES(?,?,?,?,?)
      ON CONFLICT(id) DO UPDATE SET executable_path=excluded.executable_path,executable_sha256=excluded.executable_sha256,version_label=excluded.version_label`, e.ID, e.Adapter, e.Executable, hash, e.Version)
	return e, err
}

// Dolphin's main user folder is persistent. Command-line config is an ephemeral
// layer, so user/controller settings are not rewritten on each launch.
func dolphinArgs(p Paths, profile, game string) []string {
	user := filepath.Join(p.Settings, profile)
	save := filepath.Join(p.Saves, profile)
	args := []string{"--batch", "--exec", game, "--user", user}
	for _, setting := range []string{
		"Dolphin.Display.Fullscreen=True", "Dolphin.Interface.ConfirmStop=False",
		"Dolphin.Core.SkipIPL=True", "Dolphin.Core.DSPHLE=True",
		"Dolphin.Core.MemcardAPath=" + filepath.Join(save, "MemoryCardA.USA.raw"),
		"Dolphin.Core.MemcardBPath=" + filepath.Join(save, "MemoryCardB.USA.raw"),
		"Dolphin.Core.GCIFolderAPath=" + filepath.Join(save, "CardA"),
		"Dolphin.Core.GCIFolderBPath=" + filepath.Join(save, "CardB"),
		"Dolphin.General.NANDRootPath=" + filepath.Join(save, "Wii"),
		"Dolphin.General.WiiSDCardPath=" + filepath.Join(save, "sd.raw"),
	} {
		args = append(args, "--config", setting)
	}
	return args
}
func (m *Manager) Launch(ctx context.Context, gameID string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return Session{}, err
	}
	_, err := m.configured(ctx)
	if err != nil {
		return Session{}, err
	}
	var g Game
	err = m.db.QueryRowContext(ctx, "SELECT id,content_path,COALESCE(emulator_id,''),scope,size_bytes FROM local_games WHERE id=?", gameID).Scan(&g.ID, &g.Path, &g.EmulatorID, &g.Scope, &g.Bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fail("not_found", "game is not installed")
	}
	if err != nil {
		return Session{}, err
	}
	if g.Scope == "suite" {
		var readiness string
		if err = m.db.QueryRowContext(ctx, "SELECT readiness FROM active_suite WHERE singleton=1").Scan(&readiness); err != nil {
			return Session{}, err
		}
		if readiness != "ready" {
			return Session{}, fail("not_ready", "genre collection is in maintenance")
		}
	}
	if err = noLinks(g.Path); err != nil {
		return Session{}, err
	}
	st, err := os.Stat(g.Path)
	if err != nil {
		return Session{}, err
	}
	if !st.Mode().IsRegular() || st.Size() != g.Bytes {
		return Session{}, fail("content_changed", "game file changed; reimport it")
	}
	p, err := m.profile(ctx, g.EmulatorID)
	if err != nil {
		return Session{}, err
	}
	if p.e.Adapter != "dolphin" {
		status := p.status()
		if !status.Ready && !(p.e.Adapter == "eden" && strings.EqualFold(filepath.Ext(g.Path), ".nro") && status.Prepared) {
			return Session{}, fail("prerequisites_missing", "%s", strings.Join(status.Missing, " "))
		}
	}
	args, env := p.command(g.Path)
	return m.startProfileSession(ctx, p, g.ID, "game", args, env)
}

func (m *Manager) startProfileSession(ctx context.Context, p profileLayout, gameID, purpose string, args, env []string) (Session, error) {
	if err := p.verifySource(ctx); err != nil {
		return Session{}, err
	}
	if p.e.Adapter != "dolphin" {
		if p.prepared != p.hash {
			return Session{}, fail("profile_not_prepared", "run emulator prepare --id %s --apply first", p.e.ID)
		}
		if err := p.validateRouting(); err != nil {
			return Session{}, err
		}
		if err := noLinks(p.exe); err != nil {
			return Session{}, err
		}
		st, err := os.Stat(p.exe)
		if err != nil {
			return Session{}, err
		}
		if err = verifyFile(ctx, p.exe, p.hash, st.Size()); err != nil {
			return Session{}, fail("emulator_changed", "profile runtime changed; existing files preserved")
		}
		if p.e.Adapter == "duckstation" {
			if st, err := os.Stat(filepath.Join(p.runtime, "portable.txt")); err != nil || !st.Mode().IsRegular() {
				return Session{}, fail("profile_conflict", "portable marker is missing")
			}
		}
	}
	for _, base := range []string{p.paths.Settings, p.paths.Saves} {
		root, e2 := m.openOwned(base)
		if e2 != nil {
			return Session{}, e2
		}
		e2 = root.MkdirAll(p.e.ID, 0700)
		root.Close()
		if e2 != nil {
			return Session{}, e2
		}
		if e2 = noLinks(filepath.Join(base, p.e.ID)); e2 != nil {
			return Session{}, e2
		}
	}
	session := Session{ID: newID(), GameID: gameID, State: "starting", Purpose: purpose, EmulatorID: p.e.ID}
	if _, err := m.db.ExecContext(ctx, "INSERT INTO play_sessions(id,game_id,state,purpose,emulator_id) VALUES(?,?,'starting',?,?)", session.ID, gameID, purpose, p.e.ID); err != nil {
		return Session{}, err
	}
	proc, err := startNative(p.exe, args, filepath.Dir(p.exe), env...)
	if err != nil {
		_, _ = m.db.ExecContext(context.Background(), "UPDATE play_sessions SET state='exited',ended_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?", session.ID)
		return Session{}, err
	}
	session.PID = proc.PID()
	session.State = "running"
	if _, err = m.db.ExecContext(ctx, "UPDATE play_sessions SET state='running',process_id=?,process_identity=? WHERE id=?", proc.PID(), proc.Identity(), session.ID); err != nil {
		_ = proc.Stop(true)
		_, _ = proc.Wait(context.Background())
		proc.Release()
		return Session{}, err
	}
	m.session = session
	m.process = proc
	m.watch(proc, session.ID)
	return session, nil
}
func (m *Manager) watch(proc runningProcess, id string) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		code, err := proc.Wait(m.ctx)
		if m.ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		state := "exited"
		if err != nil {
			state = "interrupted"
			m.unresolved = true
		}
		_, persistErr := m.db.ExecContext(context.Background(), "UPDATE play_sessions SET state=?,exit_code=?,ended_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?", state, code, id)
		if persistErr != nil {
			m.unresolved = true
		}
		m.session.State = state
		m.session.ExitCode = &code
		proc.Release()
		m.process = nil
	}()
}
func (m *Manager) SessionStatus() Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session.State == "" {
		return Session{State: "idle"}
	}
	return m.session
}
func (m *Manager) StopSession(force bool, expectedID ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(expectedID) > 0 && expectedID[0] != "" && expectedID[0] != m.session.ID {
		return fail("session_changed", "the active session changed; refresh before requesting exit")
	}
	if m.process == nil {
		return fail("not_running", "no controllable game session is running")
	}
	return m.process.Stop(force)
}
func (m *Manager) AcknowledgeSession(ctx context.Context, confirmed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !confirmed || !m.unresolved || m.process != nil {
		return fail("invalid_state", "explicit confirmation that the interrupted emulator is closed is required")
	}
	_, err := m.db.ExecContext(ctx, "UPDATE play_sessions SET state='exited',ended_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE state IN ('starting','running','interrupted')")
	if err == nil {
		m.unresolved = false
		m.session = Session{State: "idle"}
	}
	return err
}
func (m *Manager) recoverSession(ctx context.Context) error {
	var id, game, state, purpose, emulatorID string
	var pid sql.NullInt64
	var identity sql.NullString
	err := m.db.QueryRowContext(ctx, "SELECT id,game_id,state,process_id,process_identity,purpose,emulator_id FROM play_sessions WHERE state IN ('starting','running','interrupted') ORDER BY started_at DESC LIMIT 1").Scan(&id, &game, &state, &pid, &identity, &purpose, &emulatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	m.session = Session{ID: id, GameID: game, PID: int(pid.Int64), State: "interrupted", Purpose: purpose, EmulatorID: emulatorID}
	if !pid.Valid || !identity.Valid || state == "interrupted" {
		m.unresolved = true
		return nil
	}
	proc, err := recoverNative(int(pid.Int64), identity.String)
	if err != nil {
		return err
	}
	if proc == nil {
		_, err = m.db.ExecContext(ctx, "UPDATE play_sessions SET state='exited',ended_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?", id)
		m.session.State = "exited"
		return err
	}
	m.process = proc
	m.session.State = "running"
	m.watch(proc, id)
	return nil
}
