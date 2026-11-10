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
	if !identifier.MatchString(e.ID) || e.Adapter != "dolphin" || strings.TrimSpace(e.Version) == "" || len(e.Version) > 100 {
		return e, fail("invalid_emulator", "specify a profile ID, dolphin adapter and version label")
	}
	e.Executable, err = cleanPath(e.Executable)
	if err != nil {
		return e, err
	}
	if !contained(s.Paths.Emulators, e.Executable) || !strings.EqualFold(filepath.Ext(e.Executable), ".exe") {
		return e, fail("invalid_emulator", "select the actual Dolphin .exe inside the chosen emulators folder")
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
	s, err := m.configured(ctx)
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
	var e Emulator
	var hash string
	if err = m.db.QueryRowContext(ctx, "SELECT id,adapter,executable_path,executable_sha256 FROM emulator_profiles WHERE id=?", g.EmulatorID).Scan(&e.ID, &e.Adapter, &e.Executable, &hash); err != nil {
		return Session{}, err
	}
	if !contained(s.Paths.Emulators, e.Executable) {
		return Session{}, fail("unsafe_path", "emulator is outside the configured root")
	}
	exeStat, err := os.Stat(e.Executable)
	if err != nil {
		return Session{}, err
	}
	if err = verifyFile(ctx, e.Executable, hash, exeStat.Size()); err != nil {
		return Session{}, fail("emulator_changed", "executable changed; register the reviewed version again")
	}
	for _, p := range []string{s.Paths.Settings, s.Paths.Saves} {
		root, e2 := m.openOwned(p)
		if e2 != nil {
			return Session{}, e2
		}
		e2 = root.MkdirAll(e.ID, 0700)
		root.Close()
		if e2 != nil {
			return Session{}, e2
		}
		if e2 = noLinks(filepath.Join(p, e.ID)); e2 != nil {
			return Session{}, e2
		}
	}
	session := Session{ID: newID(), GameID: g.ID, State: "starting"}
	if _, err = m.db.ExecContext(ctx, "INSERT INTO play_sessions(id,game_id,state) VALUES(?,?,'starting')", session.ID, g.ID); err != nil {
		return Session{}, err
	}
	proc, err := startNative(e.Executable, dolphinArgs(s.Paths, e.ID, g.Path), filepath.Dir(e.Executable))
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
func (m *Manager) StopSession(force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	var id, game, state string
	var pid sql.NullInt64
	var identity sql.NullString
	err := m.db.QueryRowContext(ctx, "SELECT id,game_id,state,process_id,process_identity FROM play_sessions WHERE state IN ('starting','running','interrupted') ORDER BY started_at DESC LIMIT 1").Scan(&id, &game, &state, &pid, &identity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	m.session = Session{ID: id, GameID: game, PID: int(pid.Int64), State: "interrupted"}
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
