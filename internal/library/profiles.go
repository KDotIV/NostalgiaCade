package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ProfileRequest struct {
	ID string `json:"id"`
}
type ProfileStatus struct {
	ID         string   `json:"id"`
	Adapter    string   `json:"adapter"`
	Prepared   bool     `json:"prepared"`
	Ready      bool     `json:"ready"`
	Executable string   `json:"executable"`
	Settings   string   `json:"settings"`
	Saves      string   `json:"saves"`
	Firmware   string   `json:"firmware"`
	ConfigFile string   `json:"configFile"`
	Missing    []string `json:"missing"`
	Guidance   []string `json:"guidance"`
}
type profileLayout struct {
	e                          Emulator
	hash, prepared             string
	paths                      Paths
	home, runtime, exe, config string
	// Only storage-routing keys are owned by NCade. All tuning is done in the
	// native settings UI. Existing configuration files are never rewritten.
	sections map[string]map[string]string
	yaml     bool
}

func supportedAdapter(adapter string) bool {
	switch adapter {
	case "dolphin", "duckstation", "pcsx2", "rpcs3", "eden":
		return true
	}
	return false
}
func (m *Manager) profile(ctx context.Context, id string) (profileLayout, error) {
	var p profileLayout
	if !identifier.MatchString(id) {
		return p, fail("invalid_profile", "specify a registered profile ID")
	}
	s, err := m.configured(ctx)
	if err != nil {
		return p, err
	}
	p.paths = s.Paths
	err = m.db.QueryRowContext(ctx, "SELECT id,adapter,executable_path,version_label,executable_sha256,prepared_hash FROM emulator_profiles WHERE id=?", id).Scan(&p.e.ID, &p.e.Adapter, &p.e.Executable, &p.e.Version, &p.hash, &p.prepared)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fail("emulator_missing", "install or register this emulator first")
	}
	if err != nil {
		return p, err
	}
	p.home = filepath.Join(s.Paths.Settings, id)
	p.runtime = filepath.Join(p.home, "runtime")
	p.exe = p.e.Executable
	save := filepath.Join(s.Paths.Saves, id)
	firmware := filepath.Join(s.Paths.Firmware, id)
	path := func(parts ...string) string { return filepath.ToSlash(filepath.Join(parts...)) }
	switch p.e.Adapter {
	case "dolphin":
	case "duckstation":
		p.exe = filepath.Join(p.runtime, filepath.Base(p.e.Executable))
		p.config = filepath.Join(p.runtime, "settings.ini")
		p.sections = map[string]map[string]string{
			"BIOS":        {"SearchDirectory": path(firmware, "bios")},
			"MemoryCards": {"Directory": path(save, "memcards")},
			"Folders":     {"SaveStates": path(save, "savestates"), "Cache": path(s.Paths.Cache, id)},
		}
	case "pcsx2":
		// v2.8.2 appends PCSX2 to CustomDataPath, despite the CLI help
		// describing -datapath as the application-data directory itself.
		p.config = filepath.Join(p.home, "PCSX2", "inis", "PCSX2.ini")
		p.sections = map[string]map[string]string{
			"Folders": {"Bios": path(firmware, "bios"), "MemoryCards": path(save, "memcards"), "Savestates": path(save, "savestates"), "Cache": path(s.Paths.Cache, id)},
		}
	case "rpcs3":
		p.config = filepath.Join(p.home, "config", "vfs.yml")
		p.yaml = true
		p.sections = map[string]map[string]string{"": {
			"$(EmulatorDir)": path(p.home) + "/", "/dev_hdd0/": path(save, "dev_hdd0") + "/",
			"/dev_hdd1/":   path(s.Paths.Cache, id, "dev_hdd1") + "/",
			"/dev_flash/":  path(firmware, "dev_flash") + "/",
			"/dev_flash2/": path(firmware, "dev_flash2") + "/", "/dev_flash3/": path(firmware, "dev_flash3") + "/",
		}}
	case "eden":
		p.exe = filepath.Join(p.runtime, filepath.Base(p.e.Executable))
		p.config = filepath.Join(p.runtime, "user", "config", "qt-config.ini")
		p.sections = map[string]map[string]string{"Data%20Storage": {
			"nand_directory": path(firmware, "nand"), "save_directory": path(save, "nand"),
			"sdmc_directory": path(save, "sdmc"), "load_directory": path(p.home, "load"),
			"dump_directory": path(s.Paths.Cache, id, "dump"), "tas_directory": path(p.home, "tas"),
		}}
	default:
		return p, fail("unsupported_adapter", "unknown emulator adapter")
	}
	return p, nil
}
func (m *Manager) ProfileStatus(ctx context.Context, id string) (ProfileStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.profile(ctx, id)
	if err != nil {
		return ProfileStatus{}, err
	}
	return p.status(), nil
}
func (p profileLayout) status() ProfileStatus {
	s := ProfileStatus{ID: p.e.ID, Adapter: p.e.Adapter, Prepared: p.prepared == p.hash, Executable: p.exe, Settings: p.home, Saves: filepath.Join(p.paths.Saves, p.e.ID), Firmware: filepath.Join(p.paths.Firmware, p.e.ID), ConfigFile: p.config, Missing: []string{}, Guidance: []string{}}
	if p.e.Adapter == "dolphin" {
		s.Prepared = true
	}
	if !s.Prepared {
		s.Missing = append(s.Missing, "Run emulator prepare --id "+p.e.ID+" --apply to create the isolated profile.")
	} else if err := p.validateRouting(); err != nil {
		s.Missing = append(s.Missing, err.Error())
	}
	file := func(path string, min int64) bool {
		if noLinks(path) != nil {
			return false
		}
		st, err := os.Stat(path)
		return err == nil && st.Mode().IsRegular() && st.Size() >= min
	}
	switch p.e.Adapter {
	case "duckstation", "pcsx2":
		min := int64(512 * 1024)
		if p.e.Adapter == "pcsx2" {
			min = 4 * 1024 * 1024
		}
		if !file(filepath.Join(s.Firmware, "bios", "bios.bin"), min) {
			s.Missing = append(s.Missing, "Import a personally supplied BIOS with emulator firmware --kind bios --path <file>.")
		}
		s.Guidance = append(s.Guidance, "Open emulator configure to review controller bindings, graphics, and BIOS selection. NCade keeps the emulator's CPU/GPU defaults.")
	case "rpcs3":
		if !file(filepath.Join(s.Firmware, "dev_flash", "vsh", "module", "vsh.self"), 1) {
			s.Missing = append(s.Missing, "Import an existing dev_flash folder with --kind ps3-flash, or run emulator configure --firmware <PS3UPDAT.PUP>.")
		}
		s.Guidance = append(s.Guidance, "Import EBOOT.BIN or a PS3 ELF with --reference, keeping its complete game folder in place. PKG installation and game-specific patches remain native emulator tasks.")
		s.Guidance = append(s.Guidance, "Complete RPCS3's welcome screen in the configuration session and clear Show at startup for unattended game launches. The welcome prompt can otherwise appear even with --no-gui.")
	case "eden":
		if !file(filepath.Join(p.runtime, "user", "keys", "prod.keys"), 1) {
			s.Missing = append(s.Missing, "Import your own prod.keys with emulator firmware --kind switch-keys --path <file> (needed for encrypted games).")
		}
		ncas, _ := filepath.Glob(filepath.Join(s.Firmware, "nand", "system", "Contents", "registered", "*.nca"))
		found := false
		for _, nca := range ncas {
			if file(nca, 1) {
				found = true
				break
			}
		}
		if !found {
			s.Missing = append(s.Missing, "Import your dumped firmware NCA directory with --kind switch-firmware (needed by games using system firmware).")
		}
		s.Guidance = append(s.Guidance, "Native NRO homebrew can launch without console keys/firmware. Key/firmware compatibility is checked by Eden when booting; file presence alone is not a compatibility guarantee.")
		s.Guidance = append(s.Guidance, "A running game can show Eden's native exit confirmation after ncade stop. Confirm it in Eden and check session for state exited; NCade preserves this preference and does not force termination.")
	}
	s.Ready = s.Prepared && len(s.Missing) == 0
	return s
}
func (m *Manager) runProfilePrepare(ctx context.Context, op string, request ProfileRequest) error {
	p, err := m.profile(ctx, request.ID)
	if err != nil {
		return err
	}
	if err = m.phase(ctx, op, "preparing_profile"); err != nil {
		return err
	}
	if err = p.verifySource(ctx); err != nil {
		return err
	}
	for _, root := range []string{p.paths.Settings, p.paths.Saves, p.paths.Firmware, p.paths.Cache} {
		r, e := m.openOwned(root)
		if e != nil {
			return e
		}
		e = r.MkdirAll(p.e.ID, 0700)
		r.Close()
		if e != nil {
			return e
		}
	}
	if p.e.Adapter == "duckstation" || p.e.Adapter == "eden" {
		if err = m.mirrorRuntime(ctx, op, p); err != nil {
			return err
		}
	}
	// Create routed directories before Eden reads them (it ignores nonexistent paths).
	for _, section := range p.sections {
		for _, dir := range section {
			if err = m.mkdirProfilePath(p, dir); err != nil {
				return err
			}
		}
	}
	if p.e.Adapter == "duckstation" {
		if err = preserveOrCreate(filepath.Join(p.runtime, "portable.txt"), []byte{}); err != nil {
			return err
		}
	}
	if p.e.Adapter == "eden" {
		if err = m.mkdirProfilePath(p, filepath.Join(p.runtime, "user", "keys")); err != nil {
			return err
		}
	}
	if p.config != "" {
		body := p.initialConfig()
		if err = m.mkdirProfilePath(p, filepath.Dir(p.config)); err != nil {
			return err
		}
		if err = preserveOrCreate(p.config, []byte(body)); err != nil {
			return err
		}
		if err = p.validateRouting(); err != nil {
			return err
		}
	}
	if _, err = m.db.ExecContext(ctx, "UPDATE emulator_profiles SET prepared_hash=? WHERE id=?", p.hash, p.e.ID); err != nil {
		return err
	}
	p.prepared = p.hash
	result, _ := json.Marshal(p.status())
	_, err = m.db.ExecContext(ctx, "UPDATE operations SET result_json=? WHERE id=?", string(result), op)
	return err
}
func (p profileLayout) verifySource(ctx context.Context) error {
	if !contained(p.paths.Emulators, p.e.Executable) {
		return fail("unsafe_path", "emulator executable is outside its root")
	}
	if err := noLinks(p.e.Executable); err != nil {
		return err
	}
	st, err := os.Stat(p.e.Executable)
	if err != nil {
		return err
	}
	if err = verifyFile(ctx, p.e.Executable, p.hash, st.Size()); err != nil {
		return fail("emulator_changed", "executable changed; register the reviewed version again")
	}
	if p.e.Adapter == "rpcs3" {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(p.exe), "portable")); !os.IsNotExist(err) {
			return fail("profile_conflict", "RPCS3's portable directory would override NCade's isolated profile")
		}
	}
	if p.e.Adapter == "pcsx2" {
		for _, name := range []string{"portable.ini", "portable.txt"} {
			if _, err := os.Lstat(filepath.Join(filepath.Dir(p.exe), name)); !os.IsNotExist(err) {
				return fail("profile_conflict", "PCSX2 portable mode would override NCade's data path")
			}
		}
	}
	return nil
}
func (m *Manager) mkdirProfilePath(p profileLayout, dir string) error {
	for _, base := range []string{p.paths.Settings, p.paths.Saves, p.paths.Firmware, p.paths.Cache} {
		if contained(filepath.Join(base, p.e.ID), dir) {
			if err := noLinks(dir); err != nil {
				return err
			}
			r, err := m.openOwned(base)
			if err != nil {
				return err
			}
			defer r.Close()
			rel, err := filepath.Rel(base, dir)
			if err != nil {
				return err
			}
			return r.MkdirAll(rel, 0700)
		}
	}
	return fail("unsafe_path", "profile directory is outside its storage roots")
}
func preserveOrCreate(path string, body []byte) error {
	if err := noLinks(path); err != nil {
		return err
	}
	if st, err := os.Lstat(path); err == nil {
		if !st.Mode().IsRegular() {
			return fail("profile_conflict", "profile file is not regular")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer r.Close()
	return writeNewFile(r, filepath.Base(path), body)
}
func (p profileLayout) initialConfig() string {
	var b strings.Builder
	if p.e.Adapter == "duckstation" {
		b.WriteString("[Main]\nSetupWizardIncomplete = false\n\n")
	}
	if p.e.Adapter == "pcsx2" {
		b.WriteString("[UI]\nSettingsVersion = 1\nSetupWizardIncomplete = false\n\n[Filenames]\nBIOS = bios.bin\n\n")
	}
	for section, values := range p.sections {
		if !p.yaml {
			fmt.Fprintf(&b, "[%s]\n", section)
		}
		for key, value := range values {
			if p.yaml {
				fmt.Fprintf(&b, "%s: %s\n", strconv.Quote(key), strconv.Quote(value))
			} else if p.e.Adapter == "eden" {
				fmt.Fprintf(&b, "%s=%s\n", key, strconv.Quote(value))
			} else {
				fmt.Fprintf(&b, "%s = %s\n", key, value)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
func unquoteConfig(v string) string {
	v = strings.TrimSpace(v)
	if decoded, err := strconv.Unquote(v); err == nil {
		return decoded
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// This is deliberately a narrow reader of storage paths, not a general config
// serializer. Unknown settings/comments are left byte-for-byte untouched.
func (p profileLayout) validateRouting() error {
	if p.config == "" {
		return nil
	}
	if err := noLinks(p.config); err != nil {
		return err
	}
	st, err := os.Stat(p.config)
	if err != nil {
		return err
	}
	if st.Size() > 8*1024*1024 {
		return fail("invalid_config", "configuration exceeds 8 MiB")
	}
	body, err := os.ReadFile(p.config)
	if err != nil {
		return err
	}
	section := ""
	seen := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if !p.yaml && strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		separator := "="
		if p.yaml {
			separator = ":"
		}
		key, value, ok := strings.Cut(line, separator)
		if !ok {
			continue
		}
		key = unquoteConfig(key)
		if _, wanted := p.sections[section][key]; !wanted {
			continue
		}
		identity := section + "/" + key
		if _, duplicate := seen[identity]; duplicate {
			return fail("profile_conflict", "duplicate storage setting %s; existing file preserved", identity)
		}
		seen[identity] = unquoteConfig(value)
	}
	for section, values := range p.sections {
		for key, expected := range values {
			value, ok := seen[section+"/"+key]
			if !ok || !strings.EqualFold(filepath.Clean(filepath.FromSlash(value)), filepath.Clean(filepath.FromSlash(expected))) {
				return fail("profile_conflict", "storage setting %s/%s differs from NCade's routing; existing file preserved: %s", section, key, p.config)
			}
		}
	}
	return nil
}
func (m *Manager) mirrorRuntime(ctx context.Context, op string, p profileLayout) error {
	var packageHash, exe string
	err := m.db.QueryRowContext(ctx, "SELECT package_sha256,executable_path FROM emulator_packages WHERE slug=?", p.e.Adapter).Scan(&packageHash, &exe)
	if err != nil || !strings.EqualFold(exe, p.e.Executable) {
		return fail("package_required", "DuckStation/Eden isolation requires a reviewed installed package")
	}
	source := filepath.Dir(p.e.Executable)
	receipt, err := verifyPackage(ctx, source, packageHash)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(p.runtime); err == nil {
		_, err = verifyPackage(ctx, p.runtime, packageHash)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	var total int64
	for _, f := range receipt.Files {
		total += f.Bytes
	}
	if err = m.checkSpace(p.paths.Settings, total); err != nil {
		return err
	}
	r, err := m.openOwned(p.paths.Settings)
	if err != nil {
		return err
	}
	defer r.Close()
	stage := filepath.Join(p.e.ID, ".prepare-"+op)
	// Only this journaled operation's staging folder can be replaced.
	if err = r.RemoveAll(stage); err != nil {
		return err
	}
	defer r.RemoveAll(stage)
	if err = r.MkdirAll(stage, 0700); err != nil {
		return err
	}
	for _, f := range receipt.Files {
		target := filepath.Join(p.paths.Settings, stage, filepath.FromSlash(f.Name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err = copyExclusive(ctx, filepath.Join(source, filepath.FromSlash(f.Name)), target, f.SHA256, f.Bytes); err != nil {
			return err
		}
	}
	body, _ := json.Marshal(receipt)
	if err = preserveOrCreate(filepath.Join(p.paths.Settings, stage, ".ncade-package.json"), body); err != nil {
		return err
	}
	return r.Rename(stage, filepath.Join(p.e.ID, "runtime"))
}
func copyExclusive(ctx context.Context, source, target, hash string, size int64) error {
	if err := noLinks(source); err != nil {
		return err
	}
	if err := noLinks(target); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	actual, n, err := hashStream(ctx, in, out, size, nil)
	if err == nil && (n != size || actual != hash) {
		err = fail("content_changed", "source changed during copy")
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
