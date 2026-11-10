package library

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FirmwareRequest struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}
type firmwareFile struct {
	source, target, hash string
	size                 int64
}

// Firmware sources are always local. NCade never supplies console keys or BIOS
// downloads. Validation here checks structure, not authenticity/compatibility.
func (m *Manager) runFirmwareImport(ctx context.Context, op string, r FirmwareRequest) error {
	p, err := m.profile(ctx, r.ID)
	if err != nil {
		return err
	}
	if p.prepared != p.hash {
		return fail("profile_not_prepared", "prepare this emulator profile first")
	}
	if err = p.validateRouting(); err != nil {
		return err
	}
	source, err := cleanPath(r.Path)
	if err != nil {
		return err
	}
	if err = m.phase(ctx, op, "validating_firmware"); err != nil {
		return err
	}
	destination := ""
	directory := false
	fw := filepath.Join(p.paths.Firmware, p.e.ID)
	switch {
	case r.Kind == "bios" && (p.e.Adapter == "duckstation" || p.e.Adapter == "pcsx2"):
		st, err := os.Stat(source)
		if err != nil {
			return err
		}
		valid := st.Size() == 512*1024 || st.Size() == 1024*1024
		if p.e.Adapter == "pcsx2" {
			valid = st.Size() == 4*1024*1024
		}
		if !st.Mode().IsRegular() || !valid {
			return fail("invalid_firmware", "BIOS size does not match the selected system")
		}
		destination = filepath.Join(fw, "bios", "bios.bin")
	case r.Kind == "switch-keys" && p.e.Adapter == "eden":
		st, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() || st.Size() > 1024*1024 {
			return fail("invalid_firmware", "select a prod.keys text file smaller than 1 MiB")
		}
		b, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if !validSwitchKeys(b) {
			return fail("invalid_firmware", "prod.keys has no recognized key entries")
		}
		destination = filepath.Join(fw, "keys", "prod.keys")
	case r.Kind == "ps3-flash" && p.e.Adapter == "rpcs3":
		if st, err := os.Stat(filepath.Join(source, "vsh", "module", "vsh.self")); err != nil || !st.Mode().IsRegular() || st.Size() < 1 {
			return fail("invalid_firmware", "select the dev_flash directory containing vsh/module/vsh.self")
		}
		destination = filepath.Join(fw, "dev_flash")
		directory = true
	case r.Kind == "switch-firmware" && p.e.Adapter == "eden":
		destination = filepath.Join(fw, "nand", "system", "Contents", "registered")
		directory = true
	default:
		return fail("invalid_firmware", "firmware kind is not supported by this adapter")
	}
	if contained(source, destination) || contained(destination, source) {
		return fail("invalid_source", "firmware source and destination must be separate")
	}
	var files []firmwareFile
	var total int64
	add := func(src, dst string) error {
		if len(files) >= 20000 {
			return fail("invalid_firmware", "too many firmware files")
		}
		if err := noLinks(src); err != nil {
			return err
		}
		st, err := os.Stat(src)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() || (st.Size() < 1 && r.Kind != "ps3-flash") || st.Size() > 2*1024*1024*1024 {
			return fail("invalid_firmware", "firmware must contain regular files no larger than 2 GiB; empty files are allowed only in PS3 flash trees")
		}
		total += st.Size()
		if total > 8*1024*1024*1024 {
			return fail("invalid_firmware", "firmware exceeds 8 GiB")
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		hash, n, err := hashStream(ctx, f, nil, st.Size(), nil)
		f.Close()
		if err != nil {
			return err
		}
		files = append(files, firmwareFile{src, dst, hash, n})
		return nil
	}
	if directory {
		st, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fail("invalid_firmware", "select an extracted firmware directory")
		}
		err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := noLinks(path); err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			if r.Kind == "switch-firmware" && (!strings.EqualFold(filepath.Ext(rel), ".nca") || filepath.Base(rel) != rel) {
				return fail("invalid_firmware", "Switch firmware must be a flat directory of NCA files")
			}
			return add(path, filepath.Join(destination, rel))
		})
		if err != nil {
			return err
		}
	} else {
		if err = add(source, destination); err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return fail("invalid_firmware", "firmware directory is empty")
	}
	if r.Kind == "switch-keys" {
		mirror := files[0]
		mirror.target = filepath.Join(p.runtime, "user", "keys", "prod.keys")
		files = append(files, mirror)
		total += mirror.size
	}
	// Validate every existing destination before publishing any files. A different
	// BIOS/key/firmware is an explicit conflict, never an overwrite or reset.
	for _, file := range files {
		if err = noLinks(file.target); err != nil {
			return err
		}
		if _, e := os.Lstat(file.target); e == nil {
			if err = verifyFile(ctx, file.target, file.hash, file.size); err != nil {
				return fail("firmware_conflict", "different firmware already exists; it has been preserved: %s", file.target)
			}
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if err = m.checkSpace(p.paths.Firmware, total); err != nil {
		return err
	}
	if err = m.phase(ctx, op, "copying_firmware"); err != nil {
		return err
	}
	for _, file := range files {
		if _, e := os.Lstat(file.target); e == nil {
			continue
		}
		if err = m.mkdirProfilePath(p, filepath.Dir(file.target)); err != nil {
			return err
		}
		// One journal-named partial on the destination volume. Startup cleanup
		// can find it without scanning or touching user files.
		base := p.paths.Firmware
		if contained(p.paths.Settings, file.target) {
			base = p.paths.Settings
		}
		if err = m.checkSpace(base, file.size); err != nil {
			return err
		}
		temp := filepath.Join(base, ".firmware-"+op+".partial")
		if err = noLinks(temp); err != nil {
			return err
		}
		if err = os.Remove(temp); err != nil && !os.IsNotExist(err) {
			return err
		}
		err = copyExclusive(ctx, file.source, temp, file.hash, file.size)
		if err == nil {
			if _, e := os.Lstat(file.target); !os.IsNotExist(e) {
				err = fail("firmware_conflict", "destination appeared during import; original preserved")
			} else {
				err = os.Rename(temp, file.target)
			}
		}
		_ = os.Remove(temp)
		if err != nil {
			return err
		}
	}
	result, _ := json.Marshal(p.status())
	_, err = m.db.ExecContext(ctx, "UPDATE operations SET result_json=? WHERE id=?", string(result), op)
	return err
}
func validSwitchKeys(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "header_key" && !strings.HasPrefix(key, "master_key_") {
			continue
		}
		decoded, err := hex.DecodeString(strings.TrimSpace(value))
		if err == nil && ((key == "header_key" && len(decoded) == 32) || (strings.HasPrefix(key, "master_key_") && len(decoded) == 16)) {
			return true
		}
	}
	return false
}
