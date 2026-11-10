package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (p Paths) all() []string {
	return []string{p.Emulators, p.ROMs, p.Firmware, p.Saves, p.Settings, p.Cache}
}

func pathsEqual(a, b Paths) bool {
	left, right := a.all(), b.all()
	for i := range left {
		if runtime.GOOS == "windows" {
			if !strings.EqualFold(left[i], right[i]) {
				return false
			}
		} else if left[i] != right[i] {
			return false
		}
	}
	return true
}

// NormalizeSetup is shared by the guide and the authoritative core validation.
// It resolves a common container into isolated content roots without writing
// files or relocating an existing application database.
func NormalizeSetup(value Setup, dataDir string) (Setup, error) {
	if value.Root != "" {
		root, err := cleanPath(value.Root)
		if err != nil {
			return Setup{}, err
		}
		value.Root = root
		expected := Paths{
			Emulators: filepath.Join(root, "Emulators"), ROMs: filepath.Join(root, "ROMs"),
			Firmware: filepath.Join(root, "Firmware"), Saves: filepath.Join(root, "Saves"),
			Settings: filepath.Join(root, "Settings"), Cache: filepath.Join(root, "Cache"),
		}
		provided, resolved := value.Paths.all(), expected.all()
		for i, path := range provided {
			if path == "" {
				continue
			}
			clean, err := cleanPath(path)
			if err != nil {
				return Setup{}, err
			}
			if clean != resolved[i] && !(runtime.GOOS == "windows" && strings.EqualFold(clean, resolved[i])) {
				return Setup{}, fail("invalid_layout", "root mode defines \"%s\", but an individual path specifies \"%s\"; choose root mode or individual paths", resolved[i], path)
			}
		}
		value.Paths = expected
	}
	fields := []struct {
		name string
		path *string
	}{
		{"Emulators", &value.Paths.Emulators}, {"ROMs", &value.Paths.ROMs}, {"Firmware", &value.Paths.Firmware},
		{"Saves", &value.Paths.Saves}, {"Settings", &value.Paths.Settings}, {"Cache", &value.Paths.Cache},
	}
	for _, field := range fields {
		path, err := cleanPath(*field.path)
		if err != nil {
			return Setup{}, fmt.Errorf("%s folder: %w", field.name, err)
		}
		*field.path = path
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return Setup{}, fail("invalid_path", "%s folder \"%s\" is an existing file", field.name, path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Setup{}, err
		}
	}
	for i, a := range fields {
		for _, b := range fields[i+1:] {
			if contained(*a.path, *b.path) || contained(*b.path, *a.path) {
				return Setup{}, fail("overlapping_paths", "%s folder \"%s\" overlaps %s folder \"%s\". Choose separate sibling folders, or select one-root mode to create them automatically", a.name, *a.path, b.name, *b.path)
			}
		}
		// Application state files can sit alongside child content directories.
		// They must never be inside, or equal to, a content root.
		if contained(*a.path, dataDir) {
			return Setup{}, fail("overlapping_paths", "%s folder \"%s\" contains or equals the application-data folder \"%s\". Choose a content subfolder or a separate location", a.name, *a.path, dataDir)
		}
	}
	if value.DownloadHosts == nil {
		value.DownloadHosts = []string{}
	}
	for i, h := range value.DownloadHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !validHost(h) {
			return Setup{}, fail("invalid_host", "use exact DNS hostnames, without schemes, ports or wildcards")
		}
		value.DownloadHosts[i] = h
	}
	return value, nil
}
func contained(root, path string) bool {
	rel, err := filepath.Rel(strings.ToLower(root), strings.ToLower(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func cleanPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.ContainsAny(path, "\x00\r\n=") {
		return "", fail("invalid_path", "use an absolute local folder without links, network shares, or '='")
	}
	path = filepath.Clean(path)
	if filepath.Dir(path) == path {
		return "", fail("invalid_path", "a drive/filesystem root cannot be used")
	}
	if err := noLinks(path); err != nil {
		return "", err
	}
	return path, nil
}
func noLinks(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || isReparse(info)) {
			return fail("unsafe_path", "links and reparse points are not allowed: %s", current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}
func (m *Manager) marker() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(filepath.Clean(m.dataDir)))))
}
func (m *Manager) SetupStatus(ctx context.Context) (Setup, error) {
	var result Setup
	result.DownloadHosts = []string{}
	var body string
	err := m.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='storage.v1'").Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}
func (m *Manager) Configure(ctx context.Context, value Setup) (Setup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return Setup{}, err
	}
	old, err := m.SetupStatus(ctx)
	if err != nil {
		return Setup{}, err
	}
	value, err = NormalizeSetup(value, m.dataDir)
	if err != nil {
		return Setup{}, err
	}
	if old.Configured && !pathsEqual(old.Paths, value.Paths) {
		return Setup{}, fail("paths_locked", "paths are already configured; moving an existing library requires an explicit migration, not changing its roots")
	}
	if old.Configured {
		value.Paths = old.Paths
	}
	value.Configured = true
	for _, path := range value.Paths.all() {
		if err = os.MkdirAll(path, 0700); err != nil {
			return Setup{}, err
		}
		if err = noLinks(path); err != nil {
			return Setup{}, err
		}
		root, e := os.OpenRoot(path)
		if e != nil {
			return Setup{}, e
		}
		marker, e := root.ReadFile(".ncade-owner")
		if e == nil && string(marker) != m.marker() {
			root.Close()
			return Setup{}, fail("path_owned", "folder belongs to another cabinet: %s", path)
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			root.Close()
			return Setup{}, e
		}
		if errors.Is(e, os.ErrNotExist) {
			// Never claim a pre-existing managed-content directory.
			if path == value.Paths.ROMs {
				if _, e = root.Stat("content"); !errors.Is(e, os.ErrNotExist) {
					root.Close()
					return Setup{}, fail("path_owned", "choose a ROM folder without an existing content subdirectory")
				}
			}
			f, e := root.OpenFile(".ncade-owner", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				root.Close()
				return Setup{}, e
			}
			_, e = f.WriteString(m.marker())
			if e == nil {
				e = f.Sync()
			}
			closeErr := f.Close()
			if e == nil {
				e = closeErr
			}
			if e != nil {
				root.Close()
				return Setup{}, e
			}
		}
		// A writable probe also verifies previously configured folders.
		probe := ".probe-" + newID()
		f, e := root.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e == nil {
			e = f.Close()
			_ = root.Remove(probe)
		}
		root.Close()
		if e != nil {
			return Setup{}, e
		}
	}
	body, _ := json.Marshal(value)
	_, err = m.db.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('storage.v1',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(body))
	return value, err
}
func (m *Manager) configured(ctx context.Context) (Setup, error) {
	s, err := m.SetupStatus(ctx)
	if err != nil {
		return s, err
	}
	if !s.Configured {
		return s, fail("setup_required", "run ncade setup and choose storage paths first")
	}
	for _, p := range s.Paths.all() {
		r, e := m.openOwned(p)
		if e != nil {
			return s, e
		}
		r.Close()
	}
	return s, nil
}
func (m *Manager) openOwned(path string) (*os.Root, error) {
	if err := noLinks(path); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	info, err := r.Lstat(".ncade-owner")
	if err != nil || !info.Mode().IsRegular() {
		r.Close()
		return nil, fail("path_changed", "storage ownership marker is missing")
	}
	b, err := r.ReadFile(".ncade-owner")
	if err != nil || string(b) != m.marker() {
		r.Close()
		return nil, fail("path_changed", "storage ownership does not match this cabinet")
	}
	return r, nil
}
