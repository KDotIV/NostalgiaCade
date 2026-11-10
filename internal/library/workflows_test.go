package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/KdotIV/NostalgiaCade/internal/database"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testLibrary struct {
	m     *Manager
	db    *sql.DB
	setup Setup
	base  string
}

func fixture(t *testing.T) *testLibrary {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "state")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := database.OpenSQLite(context.Background(), filepath.Join(data, "cabinet.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	m, err := New(context.Background(), db, data)
	if err != nil {
		t.Fatal(err)
	}
	f := &testLibrary{m: m, db: db, base: base}
	t.Cleanup(func() { f.m.Close(); db.Close() })
	f.setup = Setup{Paths: Paths{filepath.Join(base, "emulators"), filepath.Join(base, "roms"), filepath.Join(base, "firmware"), filepath.Join(base, "saves"), filepath.Join(base, "settings"), filepath.Join(base, "cache")}, DownloadHosts: []string{"approved.example"}}
	if f.setup, err = m.Configure(context.Background(), f.setup); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(f.setup.Paths.Emulators, "Dolphin.exe")
	write(t, exe, "test-only fake executable")
	if _, err = m.RegisterEmulator(context.Background(), Emulator{"dolphin", "dolphin", exe, "test fixture"}); err != nil {
		t.Fatal(err)
	}
	return f
}
func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func entry(t *testing.T, f *testLibrary, id, body string) Entry {
	t.Helper()
	path := filepath.Join(f.base, id+".dol")
	write(t, path, body)
	return Entry{ID: id, Title: id, EmulatorID: "dolphin", Filename: id + ".dol", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), Bytes: int64(len(body)), Source: Source{Kind: "local", Path: path}}
}
func waitOperation(t *testing.T, m *Manager, op Operation) Operation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		busy := m.busy
		m.mu.Unlock()
		latest, err := m.Operation(context.Background(), op.ID)
		if err != nil {
			t.Fatal(err)
		}
		if busy != op.ID && (latest.State == "completed" || latest.State == "failed" || latest.State == "canceled") {
			return latest
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation timed out")
	return op
}
func submit(t *testing.T, f *testLibrary, r Request) Operation {
	t.Helper()
	if r.Key == "" {
		r.Key = newID()
	}
	op, err := f.m.Submit(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return waitOperation(t, f.m, op)
}
func switchTo(t *testing.T, f *testLibrary, genre string, entries ...Entry) Operation {
	t.Helper()
	manifest := Manifest{Version: 1, ID: genre + "-test", GenreID: genre, Games: entries}
	p, err := f.m.Plan(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	op := submit(t, f, Request{Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision}})
	if op.State != "completed" {
		t.Fatalf("switch: %+v", op)
	}
	return op
}

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func downloadable(e Entry) Entry {
	e.Source = Source{Kind: "download", URL: "https://approved.example/game.dol", Authorized: true, AuthorizationURL: "https://author.example/license"}
	return e
}

type canceledBody struct {
	ctx     context.Context
	started chan struct{}
	first   bool
}

func (b *canceledBody) Read(p []byte) (int, error) {
	if !b.first {
		b.first = true
		close(b.started)
		return copy(p, []byte("partial")), nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *canceledBody) Close() error { return nil }
func TestCancellationSerializesWorkAndCleansPartial(t *testing.T) {
	f := fixture(t)
	old := entry(t, f, "old", "old")
	switchTo(t, f, "racing", old)
	next := downloadable(entry(t, f, "next", "partial then rest"))
	started := make(chan struct{})
	f.m.httpClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: next.Bytes, Body: &canceledBody{ctx: r.Context(), started: started}, Header: make(http.Header)}, nil
	})}
	manifest := Manifest{1, "cancel-test", "fighting", []Entry{next}}
	plan, err := f.m.Plan(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.m.Submit(context.Background(), Request{Key: "cancel", Kind: "switch", Switch: &SwitchRequest{manifest, plan.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	if _, err = f.m.Plan(context.Background(), manifest); err == nil {
		t.Fatal("simultaneous mutation admitted")
	}
	if err = f.m.PrepareShutdown(); err == nil {
		t.Fatal("shutdown admitted during operation")
	}
	if err = f.m.Cancel(context.Background(), op.ID); err != nil {
		t.Fatal(err)
	}
	op = waitOperation(t, f.m, op)
	if op.State != "canceled" {
		t.Fatal(op)
	}
	if _, err = os.Stat(filepath.Join(f.setup.Paths.ROMs, "content/.partial-"+op.ID)); !os.IsNotExist(err) {
		t.Fatal("partial survives cancel")
	}
	if _, err = os.Stat(filepath.Join(f.setup.Paths.ROMs, contentName(old))); !os.IsNotExist(err) {
		t.Fatal("unexpected old-library rollback")
	}
}

func TestRemoveFirstRetainsLocalSharedAndPersistentData(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	old := entry(t, f, "old", "old ROM")
	shared := entry(t, f, "shared", "shared ROM")
	local := entry(t, f, "local", "local ROM")
	next := downloadable(entry(t, f, "next", "next ROM"))
	for _, reference := range []bool{false, true} {
		op := submit(t, f, Request{Kind: "import", Import: &ImportRequest{local, "platform", reference}})
		if op.State != "completed" {
			t.Fatal(op)
		}
	}
	switchTo(t, f, "racing", old, shared)
	// Retained content remains usable after the operator detaches its import media.
	if err := os.Remove(shared.Source.Path); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{f.setup.Paths.Saves, f.setup.Paths.Settings, f.setup.Paths.Firmware, f.setup.Paths.Emulators} {
		write(t, filepath.Join(root, "keep.txt"), "persistent")
	}
	requested := false
	f.m.httpClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		requested = true
		if _, err := os.Stat(filepath.Join(f.setup.Paths.ROMs, contentName(old))); !os.IsNotExist(err) {
			t.Error("download began before old ROM removal")
		}
		if _, err := os.Stat(filepath.Join(f.setup.Paths.ROMs, contentName(shared))); err != nil {
			t.Error("shared ROM removed")
		}
		return &http.Response{StatusCode: 200, ContentLength: next.Bytes, Body: io.NopCloser(strings.NewReader("next ROM")), Header: make(http.Header)}, nil
	})}
	switchTo(t, f, "fighting", shared, next)
	if !requested {
		t.Fatal("download not requested")
	}
	for _, root := range []string{f.setup.Paths.Saves, f.setup.Paths.Settings, f.setup.Paths.Firmware, f.setup.Paths.Emulators} {
		b, err := os.ReadFile(filepath.Join(root, "keep.txt"))
		if err != nil || string(b) != "persistent" {
			t.Fatalf("persistent data changed: %s %v", root, err)
		}
	}
	for _, path := range []string{old.Source.Path, local.Source.Path, filepath.Join(f.setup.Paths.ROMs, contentName(local))} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("original/local content lost", err)
		}
	}
	games, err := f.m.Games(ctx)
	if err != nil || len(games) != 4 {
		t.Fatalf("games: %+v %v", games, err)
	}
}
func TestFailedDownloadLeavesMaintenanceAndCanRetry(t *testing.T) {
	f := fixture(t)
	old := entry(t, f, "old", "old")
	next := downloadable(entry(t, f, "next", "new"))
	switchTo(t, f, "racing", old)
	payload := "bad"
	f.m.httpClient = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 3, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	manifest := Manifest{1, "second", "fighting", []Entry{next}}
	p, err := f.m.Plan(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	op := submit(t, f, Request{Key: "stable", Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision}})
	if op.State != "failed" || !strings.Contains(op.Error, "hash_mismatch") {
		t.Fatal(op)
	}
	var readiness string
	f.db.QueryRow("SELECT readiness FROM active_suite").Scan(&readiness)
	if readiness != "maintenance" {
		t.Fatal(readiness)
	}
	if _, err = os.Stat(filepath.Join(f.setup.Paths.ROMs, contentName(old))); !os.IsNotExist(err) {
		t.Fatal("old game was kept")
	}
	if _, err = os.Stat(filepath.Join(f.setup.Paths.ROMs, "content/.partial-"+op.ID)); !os.IsNotExist(err) {
		t.Fatal("partial download was kept")
	}
	payload = "new"
	op, err = f.m.Retry(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	op = waitOperation(t, f.m, op)
	if op.State != "completed" {
		t.Fatal(op)
	}
	// A duplicate submission returns the same operation without deleting again.
	duplicate, err := f.m.Submit(context.Background(), Request{Key: "stable", Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision}})
	if err != nil || duplicate.ID != op.ID {
		t.Fatalf("%+v %v", duplicate, err)
	}
}
func TestBadPlanAndModifiedInventoryCannotDeleteFiles(t *testing.T) {
	f := fixture(t)
	old := entry(t, f, "old", "old")
	next := entry(t, f, "next", "new")
	switchTo(t, f, "racing", old)
	manifest := Manifest{1, "second", "fighting", []Entry{next}}
	p, err := f.m.Plan(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	op := submit(t, f, Request{Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision - 1}})
	if !strings.Contains(op.Error, "stale_plan") {
		t.Fatal(op)
	}
	outside := filepath.Join(f.base, "protected.dol")
	write(t, outside, "old")
	if _, err = f.db.Exec("UPDATE installed_artifacts SET relative_path='../protected.dol' WHERE id=?", artifactName(old)); err != nil {
		t.Fatal(err)
	}
	op = submit(t, f, Request{Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision}})
	if !strings.Contains(op.Error, "unsafe_inventory") {
		t.Fatal(op)
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != "old" {
		t.Fatal("outside file changed")
	}
	if _, err = os.Stat(filepath.Join(f.setup.Paths.ROMs, contentName(old))); err != nil {
		t.Fatal("old file removed on failed preflight")
	}
}
func TestRestartReconcilesInterruptedRemoval(t *testing.T) {
	f := fixture(t)
	old := entry(t, f, "old", "old")
	next := entry(t, f, "next", "new")
	switchTo(t, f, "racing", old)
	manifest := Manifest{1, "second", "fighting", []Entry{next}}
	p, err := f.m.Plan(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Key: "recover", Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision}}
	body, _ := json.Marshal(request)
	id := newID()
	if _, err = f.db.Exec("INSERT INTO operations(id,idempotency_key,kind,state,phase,request_json) VALUES(?,'recover','switch','running','removing_old',?)", id, string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE active_suite SET readiness='maintenance',last_operation_id=?,revision=revision+1", id); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("DELETE FROM local_games WHERE scope='suite'"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(f.setup.Paths.ROMs, contentName(old))); err != nil {
		t.Fatal(err)
	} // Crash before inventory delete.
	f.m.Close()
	f.m, err = New(context.Background(), f.db, filepath.Join(f.base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.m.Operation(context.Background(), id)
	if err != nil || op.Phase != "interrupted" || op.State != "failed" {
		t.Fatalf("%+v %v", op, err)
	}
	op, err = f.m.Retry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if op = waitOperation(t, f.m, op); op.State != "completed" {
		t.Fatal(op)
	}
}
func TestSetupAndSourcePolicy(t *testing.T) {
	f := fixture(t)
	changed := f.setup
	changed.Paths.Saves = filepath.Join(changed.Paths.ROMs, "saves")
	if _, err := f.m.Configure(context.Background(), changed); err == nil {
		t.Fatal("overlapping roots accepted")
	}
	changed = f.setup
	changed.Paths.Saves = filepath.Join(f.base, "other-saves")
	if _, err := f.m.Configure(context.Background(), changed); err == nil {
		t.Fatal("live root relocation accepted")
	}
	e := downloadable(entry(t, f, "game", "game"))
	e.Source.Authorized = false
	if err := f.m.validateEntry(context.Background(), f.setup, e); err == nil {
		t.Fatal("unauthorized source accepted")
	}
	for _, raw := range []string{"http://approved.example/game", "https://evil.example/game", "https://user:secret@approved.example/game", "https://approved.example:443/game", "https://127.0.0.1/game"} {
		if err := allowedURL(raw, f.setup.DownloadHosts); err == nil {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	client := secureHTTP(f.setup.DownloadHosts)
	req, _ := http.NewRequest("GET", "https://evil.example/game", nil)
	if client.CheckRedirect(req, nil) == nil {
		t.Fatal("redirect escaped allowlist")
	}
	e = entry(t, f, "game2", "test")
	e.Filename = "../escape.dol"
	if err := f.m.validateEntry(context.Background(), f.setup, e); err == nil {
		t.Fatal("path traversal accepted")
	}
	f.m.freeBytes = func(string) (int64, error) { return 0, nil }
	if _, err := f.m.Plan(context.Background(), Manifest{1, "full", "racing", []Entry{entry(t, f, "large", "123")}}); err == nil {
		t.Fatal("disk budget not enforced")
	}
}
func TestSymlinkRootAndChangedOwnedFileAreRejected(t *testing.T) {
	f := fixture(t)
	old := entry(t, f, "old", "old")
	switchTo(t, f, "racing", old)
	write(t, filepath.Join(f.setup.Paths.ROMs, contentName(old)), "new")
	next := entry(t, f, "next", "next")
	manifest := Manifest{1, "second", "fighting", []Entry{next}}
	p, err := f.m.Plan(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	op := submit(t, f, Request{Kind: "switch", Switch: &SwitchRequest{manifest, p.Revision}})
	if !strings.Contains(op.Error, "refusing removal") {
		t.Fatal(op)
	}
	link := filepath.Join(f.base, "alias")
	if err = os.Symlink(f.setup.Paths.Saves, link); err != nil {
		t.Skip("OS does not permit unprivileged symlink creation")
	}
	if err = noLinks(link); err == nil {
		t.Fatal("symlink accepted")
	}
}
