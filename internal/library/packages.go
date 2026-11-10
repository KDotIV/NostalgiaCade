package library

import (
	"archive/zip"
	"context"
	"debug/pe"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"github.com/bodgit/sevenzip"
)

type EmulatorInstall struct {
	Package     emucatalog.Package `json:"package"`
	ArchivePath string             `json:"archivePath,omitempty"`
}
type InstalledEmulator struct {
	Slug             string `json:"slug"`
	Version          string `json:"version"`
	Executable       string `json:"executable"`
	LaunchSupported  bool   `json:"launchSupported"`
	DeploymentReview string `json:"deploymentReview"`
}
type packageFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type packageReceipt struct {
	PackageSHA256 string        `json:"packageSha256"`
	Files         []packageFile `json:"files"`
}
type archiveFile struct {
	name string
	mode fs.FileMode
	size uint64
	open func() (io.ReadCloser, error)
}

func (m *Manager) InstalledEmulators(ctx context.Context) ([]InstalledEmulator, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT slug,version,executable_path,manifest_json FROM emulator_packages ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InstalledEmulator{}
	for rows.Next() {
		var v InstalledEmulator
		var body string
		if err = rows.Scan(&v.Slug, &v.Version, &v.Executable, &body); err != nil {
			return nil, err
		}
		var p emucatalog.Package
		if err = json.Unmarshal([]byte(body), &p); err != nil {
			return nil, err
		}
		v.DeploymentReview = p.DeploymentReview
		v.LaunchSupported = supportedAdapter(v.Slug)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (m *Manager) runEmulatorInstall(ctx context.Context, operation string, request EmulatorInstall) error {
	p := request.Package
	if err := p.Validate(); err != nil {
		return fail("invalid_package", "%s", err)
	}
	if err := allowedURL(p.URL, p.AllowedHosts); err != nil {
		return err
	}
	s, err := m.configured(ctx)
	if err != nil {
		return err
	}
	root, err := m.openOwned(s.Paths.Emulators)
	if err != nil {
		return err
	}
	defer root.Close()
	base := ".install-" + operation
	// These names are derived only from the journaled operation ID, beneath
	// the owned emulator root. Published releases and user data are never swept.
	cleanup := func() {
		_ = root.Remove(base + ".archive")
		_ = root.Remove(base + ".download")
		_ = root.RemoveAll(base + ".stage")
	}
	cleanup()
	defer cleanup()
	if err = m.phase(ctx, operation, "validating_package"); err != nil {
		return err
	}
	destination := path.Join("packages", p.Slug, p.SHA256)
	fullDestination := filepath.Join(s.Paths.Emulators, filepath.FromSlash(destination))
	var receipt packageReceipt
	if _, statErr := root.Lstat(destination); statErr == nil {
		receipt, err = verifyPackage(ctx, fullDestination, p.SHA256)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	} else {
		if err = m.phase(ctx, operation, "acquiring_package"); err != nil {
			return err
		}
		source := Source{Kind: "download", URL: p.URL}
		if request.ArchivePath != "" {
			archive, err := cleanPath(request.ArchivePath)
			if err != nil {
				return err
			}
			if contained(s.Paths.Emulators, archive) {
				return fail("invalid_source", "package source must be outside the emulator root")
			}
			source = Source{Kind: "local", Path: archive}
		}
		downloadSetup := s
		downloadSetup.Paths.ROMs = s.Paths.Emulators
		downloadSetup.DownloadHosts = p.AllowedHosts
		entry := Entry{SHA256: p.SHA256, Bytes: p.Bytes, Source: source}
		if err = m.acquireOnce(ctx, downloadSetup, root, entry, base+".archive", base+".download"); err != nil {
			return err
		}
		if err = m.phase(ctx, operation, "extracting_package"); err != nil {
			return err
		}
		if err = root.Mkdir(base+".stage", 0700); err != nil {
			return err
		}
		stagePath := filepath.Join(s.Paths.Emulators, base+".stage")
		receipt, err = m.extractPackage(ctx, filepath.Join(s.Paths.Emulators, base+".download"), stagePath, p)
		if err != nil {
			return err
		}
		if err = checkPE(filepath.Join(stagePath, filepath.FromSlash(p.Executable))); err != nil {
			return err
		}
		body, _ := json.Marshal(receipt)
		stage, err := os.OpenRoot(stagePath)
		if err != nil {
			return err
		}
		err = writeNewFile(stage, ".ncade-package.json", body)
		stage.Close()
		if err != nil {
			return err
		}
		if err = root.MkdirAll(path.Dir(destination), 0700); err != nil {
			return err
		}
		if err = noLinks(fullDestination); err != nil {
			return err
		}
		// Publish once. A restart after rename revalidates the receipt and finishes
		// the transaction without overwriting an already installed release.
		if err = root.Rename(base+".stage", destination); err != nil {
			return err
		}
	}
	executable := filepath.Join(fullDestination, filepath.FromSlash(p.Executable))
	if err = checkPE(executable); err != nil {
		return err
	}
	var executableHash string
	for _, f := range receipt.Files {
		if f.Name == p.Executable {
			executableHash = f.SHA256
		}
	}
	if executableHash == "" {
		return fail("invalid_package", "declared executable is missing")
	}
	if err = m.phase(ctx, operation, "registering_package"); err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	body, _ := json.Marshal(p)
	_, err = tx.ExecContext(ctx, `INSERT INTO emulator_packages(slug,version,package_sha256,executable_path,executable_sha256,manifest_json) VALUES(?,?,?,?,?,?) ON CONFLICT(slug) DO UPDATE SET version=excluded.version,package_sha256=excluded.package_sha256,executable_path=excluded.executable_path,executable_sha256=excluded.executable_sha256,manifest_json=excluded.manifest_json,installed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, p.Slug, p.Version, p.SHA256, executable, executableHash, string(body))
	if err != nil {
		return err
	}
	if supportedAdapter(p.Slug) {
		var conflicts int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM emulator_profiles WHERE id=? AND adapter<>?", p.Slug, p.Slug).Scan(&conflicts); err != nil {
			return err
		}
		if conflicts != 0 {
			return fail("adapter_conflict", "package ID is occupied by another adapter")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO emulator_profiles(id,adapter,executable_path,executable_sha256,version_label) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET executable_path=excluded.executable_path,executable_sha256=excluded.executable_sha256,version_label=excluded.version_label`, p.Slug, p.Slug, executable, executableHash, p.Version)
		if err != nil {
			return err
		}
	}
	result, _ := json.Marshal(InstalledEmulator{p.Slug, p.Version, executable, supportedAdapter(p.Slug), p.DeploymentReview})
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET result_json=? WHERE id=?`, string(result), operation); err != nil {
		return err
	}
	return tx.Commit()
}

func writeNewFile(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func checkPE(executable string) error {
	if err := noLinks(executable); err != nil {
		return err
	}
	f, err := pe.Open(executable)
	if err != nil {
		return fail("invalid_executable", "package executable is not a readable Windows PE")
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return fail("invalid_executable", "package requires a Windows x64 executable")
	}
	return nil
}
func verifyPackage(ctx context.Context, dir, hash string) (packageReceipt, error) {
	var receipt packageReceipt
	if err := noLinks(dir); err != nil {
		return receipt, err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return receipt, err
	}
	defer r.Close()
	f, err := r.Open(".ncade-package.json")
	if err != nil {
		return receipt, err
	}
	defer f.Close()
	if err = json.NewDecoder(io.LimitReader(f, 8*1024*1024)).Decode(&receipt); err != nil {
		return receipt, err
	}
	if receipt.PackageSHA256 != hash || len(receipt.Files) == 0 || len(receipt.Files) > 20000 {
		return receipt, fail("package_changed", "installed package receipt does not match")
	}
	for _, v := range receipt.Files {
		if !emucatalog.SafeName(v.Name) {
			return receipt, fail("unsafe_path", "invalid receipt path")
		}
		if err = verifyFile(ctx, filepath.Join(dir, filepath.FromSlash(v.Name)), v.SHA256, v.Bytes); err != nil {
			return receipt, err
		}
	}
	return receipt, nil
}

func (m *Manager) extractPackage(ctx context.Context, archive, stage string, p emucatalog.Package) (packageReceipt, error) {
	receipt := packageReceipt{PackageSHA256: p.SHA256, Files: []packageFile{}}
	var files []archiveFile
	if p.Format == "zip" {
		r, err := zip.OpenReader(archive)
		if err != nil {
			return receipt, err
		}
		defer r.Close()
		for _, f := range r.File {
			files = append(files, archiveFile{f.Name, f.Mode(), f.UncompressedSize64, f.Open})
		}
	} else {
		r, err := sevenzip.OpenReader(archive)
		if err != nil {
			return receipt, err
		}
		defer r.Close()
		for _, f := range r.File {
			files = append(files, archiveFile{f.Name, f.Mode(), f.UncompressedSize, f.Open})
		}
	}
	if len(files) == 0 || len(files) > 20000 {
		return receipt, fail("invalid_package", "archive entry count exceeds limit")
	}
	var total int64
	seen := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSuffix(f.name, "/")
		if !emucatalog.SafeName(name) || strings.EqualFold(name, ".ncade-package.json") || (!f.mode.IsRegular() && !f.mode.IsDir()) {
			return receipt, fail("unsafe_archive", "archive contains an unsafe name or link")
		}
		key := strings.ToLower(name)
		if seen[key] {
			return receipt, fail("unsafe_archive", "duplicate archive path")
		}
		seen[key] = true
		if f.size > 2*1024*1024*1024 {
			return receipt, fail("invalid_package", "archive member too large")
		}
		total += int64(f.size)
		if total > 2*1024*1024*1024 {
			return receipt, fail("invalid_package", "extracted package exceeds 2 GiB")
		}
	}
	if err := m.checkSpace(stage, total); err != nil {
		return receipt, err
	}
	root, err := os.OpenRoot(stage)
	if err != nil {
		return receipt, err
	}
	defer root.Close()
	for _, f := range files {
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		name := strings.TrimSuffix(f.name, "/")
		if f.mode.IsDir() {
			if err = root.MkdirAll(name, 0700); err != nil {
				return receipt, err
			}
			continue
		}
		if err = root.MkdirAll(path.Dir(name), 0700); err != nil {
			return receipt, err
		}
		in, err := f.open()
		if err != nil {
			return receipt, err
		}
		out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return receipt, err
		}
		hash, n, copyErr := hashStream(ctx, in, out, int64(f.size), func() error { return m.checkSpace(stage, 256*1024) })
		in.Close()
		if copyErr == nil {
			copyErr = out.Sync()
		}
		closeErr := out.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return receipt, copyErr
		}
		if n != int64(f.size) {
			return receipt, fmt.Errorf("archive member size mismatch")
		}
		receipt.Files = append(receipt.Files, packageFile{name, hash, n})
	}
	return receipt, nil
}
