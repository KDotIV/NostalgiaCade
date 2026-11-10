package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) validateEntry(ctx context.Context, s Setup, e Entry) error {
	if !identifier.MatchString(e.ID) || strings.TrimSpace(e.Title) == "" || len(e.Title) > 200 || !digest.MatchString(e.SHA256) || e.Bytes <= 0 || e.Bytes > 128*1024*1024*1024 {
		return fail("invalid_entry", "entry requires an ID, title, SHA-256 and a size between 1 byte and 128 GiB")
	}
	if _, err := supportedExtension(e.Filename); err != nil {
		return err
	}
	var n int
	if err := m.db.QueryRowContext(ctx, "SELECT count(*) FROM emulator_profiles WHERE id=?", e.EmulatorID).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fail("emulator_missing", "register the entry's Dolphin profile first")
	}
	switch e.Source.Kind {
	case "local":
		if e.Source.URL != "" || !filepath.IsAbs(e.Source.Path) {
			return fail("invalid_source", "a local source needs an absolute file path")
		}
		if contained(s.Paths.ROMs, e.Source.Path) {
			return fail("invalid_source", "import sources must be outside the managed ROM root")
		}
		// A retained, content-addressed target can be used without reattaching
		// original import media. The worker verifies its full hash before removal.
		if info, e2 := os.Lstat(filepath.Join(s.Paths.ROMs, contentName(e))); e2 == nil && info.Mode().IsRegular() && info.Size() == e.Bytes {
			return nil
		}
		if err := noLinks(e.Source.Path); err != nil {
			return err
		}
		info, err := os.Stat(e.Source.Path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != e.Bytes {
			return fail("invalid_source", "local source is missing or its size has changed")
		}
	case "download":
		evidence, err := url.Parse(e.Source.AuthorizationURL)
		if !e.Source.Authorized || err != nil || evidence.Scheme != "https" || evidence.Host == "" || evidence.User != nil || e.Source.Path != "" {
			return fail("authorization_required", "download needs explicit authorization and an HTTPS permission/license evidence URL")
		}
		return allowedURL(e.Source.URL, s.DownloadHosts)
	default:
		return fail("invalid_source", "source kind must be local or download")
	}
	return nil
}
func (m *Manager) validateGenre(ctx context.Context, id string) error {
	var n int
	if err := m.db.QueryRowContext(ctx, "SELECT count(*) FROM genres WHERE id=?", id).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fail("invalid_genre", "unknown genre")
	}
	return nil
}

type artifact struct {
	ID, Path, Hash string
	Bytes          int64
}

func (m *Manager) artifacts(ctx context.Context) ([]artifact, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT id,relative_path,sha256,size_bytes FROM installed_artifacts WHERE kind='game'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []artifact
	for rows.Next() {
		var a artifact
		if err = rows.Scan(&a.ID, &a.Path, &a.Hash, &a.Bytes); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (m *Manager) plan(ctx context.Context, s Setup, manifest Manifest) (Plan, []artifact, error) {
	p := Plan{RemoveGames: []string{}, RetainArtifacts: []string{}}
	if manifest.Version != 1 || !identifier.MatchString(manifest.ID) || len(manifest.Games) == 0 || len(manifest.Games) > 100 {
		return p, nil, fail("invalid_manifest", "expected version 1, an ID and 1–100 games")
	}
	if err := m.validateGenre(ctx, manifest.GenreID); err != nil {
		return p, nil, err
	}
	if err := m.db.QueryRowContext(ctx, "SELECT revision FROM active_suite WHERE singleton=1").Scan(&p.Revision); err != nil {
		return p, nil, err
	}
	keep := map[string]bool{}
	entries := map[string]bool{}
	for _, e := range manifest.Games {
		if entries[e.ID] {
			return p, nil, fail("invalid_manifest", "duplicate game ID")
		}
		entries[e.ID] = true
		if err := m.validateEntry(ctx, s, e); err != nil {
			return p, nil, err
		}
		a := artifactName(e)
		if !keep[a] {
			info, err := os.Lstat(filepath.Join(s.Paths.ROMs, contentName(e)))
			if errors.Is(err, os.ErrNotExist) {
				p.AcquireBytes += e.Bytes
			} else if err != nil {
				return p, nil, err
			} else if !info.Mode().IsRegular() || info.Size() != e.Bytes {
				return p, nil, fail("content_changed", "managed target file is invalid; resolve it before switching")
			}
		}
		keep[a] = true
	}
	games, err := m.Games(ctx)
	if err != nil {
		return p, nil, err
	}
	for _, g := range games {
		if g.Scope == "suite" {
			p.RemoveGames = append(p.RemoveGames, g.ID)
		} else if g.ArtifactID != "" {
			keep[g.ArtifactID] = true
		}
	}
	stored, err := m.artifacts(ctx)
	if err != nil {
		return p, nil, err
	}
	var remove []artifact
	for _, a := range stored {
		if keep[a.ID] {
			p.RetainArtifacts = append(p.RetainArtifacts, a.ID)
		} else {
			remove = append(remove, a)
			if info, e := os.Lstat(filepath.Join(s.Paths.ROMs, a.Path)); e == nil {
				p.ReclaimBytes += info.Size()
			} else if !errors.Is(e, os.ErrNotExist) {
				return p, nil, e
			}
		}
	}
	free, err := m.freeBytes(s.Paths.ROMs)
	if err != nil {
		return p, nil, err
	}
	if free+p.ReclaimBytes < p.AcquireBytes+m.reserveBytes {
		return p, nil, fail("disk_full", "target does not fit even after removing old managed games")
	}
	return p, remove, nil
}
func (m *Manager) Plan(ctx context.Context, manifest Manifest) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return Plan{}, err
	}
	s, err := m.configured(ctx)
	if err != nil {
		return Plan{}, err
	}
	p, _, err := m.plan(ctx, s, manifest)
	return p, err
}
func (m *Manager) recordArtifact(ctx context.Context, e Entry) error {
	_, err := m.db.ExecContext(ctx, "INSERT INTO installed_artifacts(id,sha256,kind,relative_path,size_bytes) VALUES(?,?,'game',?,?) ON CONFLICT(id) DO NOTHING", artifactName(e), e.SHA256, contentName(e), e.Bytes)
	return err
}
func insertGame(ctx context.Context, tx *sql.Tx, id, genre, scope, ownership, path, artifactID string, e Entry) error {
	var aid any
	if artifactID != "" {
		aid = artifactID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO local_games(id,title,content_path,ownership,genre_id,emulator_id,sha256,size_bytes,artifact_id,scope)
      VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, id, e.Title, path, ownership, genre, e.EmulatorID, e.SHA256, e.Bytes, aid, scope)
	return err
}
func (m *Manager) runImport(ctx context.Context, id string, r ImportRequest) error {
	if err := m.phase(ctx, id, "validating"); err != nil {
		return err
	}
	s, err := m.configured(ctx)
	if err != nil {
		return err
	}
	if err = m.validateGenre(ctx, r.GenreID); err != nil {
		return err
	}
	if err = m.validateEntry(ctx, s, r.Entry); err != nil {
		return err
	}
	if r.Reference && r.Entry.Source.Kind != "local" {
		return fail("invalid_source", "only local files can be referenced")
	}
	if r.Entry.Source.Kind == "local" {
		if err = verifyFile(ctx, r.Entry.Source.Path, r.Entry.SHA256, r.Entry.Bytes); err != nil {
			return err
		}
	}
	path := r.Entry.Source.Path
	ownership := "external"
	aid := ""
	if !r.Reference {
		if err = m.phase(ctx, id, "acquiring"); err != nil {
			return err
		}
		root, err := m.openOwned(s.Paths.ROMs)
		if err != nil {
			return err
		}
		defer root.Close()
		if err = m.recordArtifact(ctx, r.Entry); err != nil {
			return err
		}
		if err = m.acquire(ctx, s, root, r.Entry, id); err != nil {
			return err
		}
		path = filepath.Join(s.Paths.ROMs, contentName(r.Entry))
		ownership = "managed"
		aid = artifactName(r.Entry)
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	gameID := "local-" + id
	if err = insertGame(ctx, tx, gameID, r.GenreID, "local", ownership, path, aid, r.Entry); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE active_suite SET revision=revision+1 WHERE singleton=1"); err != nil {
		return err
	}
	result, _ := json.Marshal(map[string]string{"gameId": gameID})
	if _, err = tx.ExecContext(ctx, "UPDATE operations SET phase='ready',result_json=?,state='completed' WHERE id=?", string(result), id); err != nil {
		return err
	}
	return tx.Commit()
}
func (m *Manager) runSwitch(ctx context.Context, id string, r SwitchRequest) error {
	if err := m.phase(ctx, id, "planning"); err != nil {
		return err
	}
	s, err := m.configured(ctx)
	if err != nil {
		return err
	}
	var last, ready string
	var revision int64
	if err = m.db.QueryRowContext(ctx, "SELECT COALESCE(last_operation_id,''),readiness,revision FROM active_suite WHERE singleton=1").Scan(&last, &ready, &revision); err != nil {
		return err
	}
	if last == id && ready == "ready" {
		return nil
	} // Activation committed before the core stopped.
	if last != id && revision != r.ExpectedRevision {
		return fail("stale_plan", "library changed after planning; create a new plan")
	}
	_, remove, err := m.plan(ctx, s, r.Manifest)
	if err != nil {
		return err
	}
	root, err := m.openOwned(s.Paths.ROMs)
	if err != nil {
		return err
	}
	defer root.Close()
	// Validate every local source and retained target before any deletion.
	for _, e := range r.Manifest.Games {
		if _, e2 := root.Lstat(contentName(e)); e2 == nil {
			if err = verifyFile(ctx, filepath.Join(s.Paths.ROMs, contentName(e)), e.SHA256, e.Bytes); err != nil {
				return err
			}
			continue
		}
		if e.Source.Kind == "local" {
			if err = verifyFile(ctx, e.Source.Path, e.SHA256, e.Bytes); err != nil {
				return err
			}
		}
	}
	for _, a := range remove {
		if err = m.validateRemoval(ctx, s, root, a); err != nil {
			return err
		}
	}
	if err = m.phase(ctx, id, "removing_old"); err != nil {
		return err
	}
	// Invalidate the old suite and detach only suite-owned records atomically.
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE active_suite SET readiness='maintenance',last_operation_id=?,revision=revision+1 WHERE singleton=1", id); err == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM local_games WHERE scope='suite'")
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, a := range remove {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = m.validateRemoval(ctx, s, root, a); err != nil {
			return err
		}
		if err = root.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err = m.db.ExecContext(ctx, "DELETE FROM installed_artifacts WHERE id=? AND kind='game'", a.ID); err != nil {
			return err
		}
	}
	// No acquisition starts until every required old-file removal has completed.
	if err = m.phase(ctx, id, "acquiring"); err != nil {
		return err
	}
	for _, e := range r.Manifest.Games {
		if err = m.recordArtifact(ctx, e); err != nil {
			return err
		}
		if err = m.acquire(ctx, s, root, e, id); err != nil {
			return err
		}
	}
	if err = m.phase(ctx, id, "activating"); err != nil {
		return err
	}
	tx, err = m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range r.Manifest.Games {
		if err = insertGame(ctx, tx, "suite-"+r.Manifest.GenreID+"-"+e.ID, r.Manifest.GenreID, "suite", "managed", filepath.Join(s.Paths.ROMs, contentName(e)), artifactName(e), e); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE active_suite SET release_id=?,genre_id=?,readiness='ready',revision=revision+1,last_operation_id=? WHERE singleton=1", r.Manifest.ID, r.Manifest.GenreID, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE operations SET state='completed',phase='ready' WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (m *Manager) validateRemoval(ctx context.Context, s Setup, root *os.Root, a artifact) error {
	ext, err := supportedExtension(a.ID)
	if err != nil || !digest.MatchString(a.Hash) || a.ID != a.Hash+ext || a.Path != "content/"+a.ID {
		return fail("unsafe_inventory", "refusing an invalid managed game path")
	}
	info, err := root.Lstat(a.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || isReparse(info) {
		return fail("unsafe_path", "refusing to remove a link or non-regular file")
	}
	if err = verifyFile(ctx, filepath.Join(s.Paths.ROMs, a.Path), a.Hash, a.Bytes); err != nil {
		return fmt.Errorf("managed file changed; refusing removal: %w", err)
	}
	return nil
}
