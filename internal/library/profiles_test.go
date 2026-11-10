package library

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func installedProfile(t *testing.T, adapter string) (*testLibrary, profileLayout) {
	t.Helper()
	f := fixture(t)
	r := packageFixture(t, f, "Dolphin.exe", fakePE())
	r.Package.Slug = adapter
	if op := submit(t, f, Request{Kind: "emulator_install", Install: &r}); op.State != "completed" {
		t.Fatal(op)
	}
	if op := submit(t, f, Request{Kind: "emulator_prepare", Prepare: &ProfileRequest{ID: adapter}}); op.State != "completed" {
		t.Fatal(op)
	}
	p, err := f.m.profile(context.Background(), adapter)
	if err != nil {
		t.Fatal(err)
	}
	return f, p
}
func TestProfilesPreserveTuningAndSaves(t *testing.T) {
	for _, adapter := range []string{"duckstation", "pcsx2", "rpcs3", "eden"} {
		t.Run(adapter, func(t *testing.T) {
			f, p := installedProfile(t, adapter)
			before, err := os.ReadFile(p.config)
			if err != nil {
				t.Fatal(err)
			}
			before = append(before, []byte("\n# Operator's custom tuning is retained\n")...)
			if err = os.WriteFile(p.config, before, 0600); err != nil {
				t.Fatal(err)
			}
			save := filepath.Join(p.paths.Saves, adapter, "keep-save")
			write(t, save, "progress")
			for i := 0; i < 2; i++ {
				if op := submit(t, f, Request{Kind: "emulator_prepare", Prepare: &ProfileRequest{ID: adapter}}); op.State != "completed" {
					t.Fatal(op)
				}
			}
			after, _ := os.ReadFile(p.config)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("configuration was rewritten")
			}
			after, _ = os.ReadFile(save)
			if string(after) != "progress" {
				t.Fatal("save changed")
			}
			status, err := f.m.ProfileStatus(context.Background(), adapter)
			if err != nil || !status.Prepared || status.Ready {
				t.Fatalf("expected prepared with missing firmware: %+v %v", status, err)
			}
			if adapter == "duckstation" || adapter == "eden" {
				if p.exe == p.e.Executable || !contained(p.paths.Settings, p.exe) {
					t.Fatal("runtime not isolated")
				}
				if _, err = os.Stat(filepath.Join(filepath.Dir(p.e.Executable), "settings.ini")); !os.IsNotExist(err) {
					t.Fatal("package was configured in place")
				}
			}
		})
	}
}
func TestProfileConflictNeverRewritesExistingConfig(t *testing.T) {
	f, p := installedProfile(t, "pcsx2")
	text := "[UI]\nSettingsVersion=1\n[Folders]\nBios=C:/existing-biOS\n"
	write(t, p.config, text)
	op := submit(t, f, Request{Kind: "emulator_prepare", Prepare: &ProfileRequest{ID: "pcsx2"}})
	if op.State != "failed" || !strings.Contains(op.Error, "profile_conflict") {
		t.Fatal(op)
	}
	b, _ := os.ReadFile(p.config)
	if string(b) != text {
		t.Fatal("existing config changed")
	}
	if _, err := f.m.ConfigureEmulator(context.Background(), ConfigureEmulatorRequest{ID: "pcsx2"}); err == nil {
		t.Fatal("launched with conflicting routing")
	}
}
func TestBIOSImportIsIdempotentAndNeverOverwrites(t *testing.T) {
	for _, adapter := range []string{"duckstation", "pcsx2"} {
		t.Run(adapter, func(t *testing.T) {
			f, p := installedProfile(t, adapter)
			size := 512 * 1024
			if adapter == "pcsx2" {
				size = 4 * 1024 * 1024
			}
			source := filepath.Join(f.base, "supplied.bin")
			body := make([]byte, size)
			body[0] = 1
			if err := os.WriteFile(source, body, 0600); err != nil {
				t.Fatal(err)
			}
			request := FirmwareRequest{ID: adapter, Kind: "bios", Path: source}
			for i := 0; i < 2; i++ {
				if op := submit(t, f, Request{Kind: "firmware_import", Firmware: &request}); op.State != "completed" {
					t.Fatal(op)
				}
			}
			status, _ := f.m.ProfileStatus(context.Background(), adapter)
			if !status.Ready {
				t.Fatal(status)
			}
			body[0] = 2
			if err := os.WriteFile(source, body, 0600); err != nil {
				t.Fatal(err)
			}
			op := submit(t, f, Request{Kind: "firmware_import", Firmware: &request})
			if op.State != "failed" || !strings.Contains(op.Error, "firmware_conflict") {
				t.Fatal(op)
			}
			installed, _ := os.ReadFile(filepath.Join(p.paths.Firmware, adapter, "bios", "bios.bin"))
			if installed[0] != 1 {
				t.Fatal("BIOS overwritten")
			}
		})
	}
}
func TestFirmwareConflictsPreflightWholeTree(t *testing.T) {
	f, p := installedProfile(t, "rpcs3")
	source := filepath.Join(f.base, "dev_flash")
	if err := os.MkdirAll(filepath.Join(source, "vsh", "module"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "vsh", "module", "vsh.self"), "existing firmware")
	write(t, filepath.Join(source, "vsh", "module", "dummy.txt"), "")
	request := FirmwareRequest{ID: "rpcs3", Kind: "ps3-flash", Path: source}
	if op := submit(t, f, Request{Kind: "firmware_import", Firmware: &request}); op.State != "completed" {
		t.Fatal(op)
	}
	write(t, filepath.Join(source, "a-new-file"), "must not be copied before conflict")
	write(t, filepath.Join(source, "vsh", "module", "vsh.self"), "changed firmware")
	if op := submit(t, f, Request{Kind: "firmware_import", Firmware: &request}); op.State != "failed" {
		t.Fatal(op)
	}
	if _, err := os.Stat(filepath.Join(p.paths.Firmware, "rpcs3", "dev_flash", "a-new-file")); !os.IsNotExist(err) {
		t.Fatal("partially published conflicting firmware")
	}
}
func TestEdenKeysAndFirmwareAreIsolated(t *testing.T) {
	f, p := installedProfile(t, "eden")
	keys := filepath.Join(f.base, "prod.keys")
	write(t, keys, "# Test-only synthetic key\nmaster_key_00 = "+strings.Repeat("01", 16)+"\n")
	req := FirmwareRequest{ID: "eden", Kind: "switch-keys", Path: keys}
	if op := submit(t, f, Request{Kind: "firmware_import", Firmware: &req}); op.State != "completed" {
		t.Fatal(op)
	}
	for _, path := range []string{filepath.Join(p.paths.Firmware, "eden", "keys", "prod.keys"), filepath.Join(p.runtime, "user", "keys", "prod.keys")} {
		b, err := os.ReadFile(path)
		original, _ := os.ReadFile(keys)
		if err != nil || !reflect.DeepEqual(b, original) {
			t.Fatal("key copy mismatch")
		}
	}
	source := filepath.Join(f.base, "firmware-input")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "test.nca"), "structural fixture only")
	req = FirmwareRequest{ID: "eden", Kind: "switch-firmware", Path: source}
	if op := submit(t, f, Request{Kind: "firmware_import", Firmware: &req}); op.State != "completed" {
		t.Fatal(op)
	}
	status, _ := f.m.ProfileStatus(context.Background(), "eden")
	if !status.Ready {
		t.Fatal(status)
	}
	if contained(p.paths.Saves, filepath.Join(p.paths.Firmware, "eden", "nand")) {
		t.Fatal("NAND firmware overlaps saves")
	}
	write(t, filepath.Join(source, "not-firmware.txt"), "invalid")
	if op := submit(t, f, Request{Kind: "firmware_import", Firmware: &req}); op.State != "failed" {
		t.Fatal(op)
	}
}
func TestAdapterCommandsAndFormatBoundaries(t *testing.T) {
	for _, adapter := range []string{"duckstation", "pcsx2", "rpcs3", "eden"} {
		t.Run(adapter, func(t *testing.T) {
			f, p := installedProfile(t, adapter)
			game := filepath.Join(f.base, "A game with spaces.bin")
			args, env := p.command(game)
			if args[len(args)-1] != game {
				t.Fatal("game path must remain a single literal argument", args)
			}
			if adapter == "rpcs3" {
				if len(env) != 1 || !strings.HasSuffix(env[0], "/") {
					t.Fatal("RPCS3 config path requires a trailing slash", env)
				}
			}
			if adapter == "pcsx2" {
				if args[0] != "-datapath" || args[1] != p.home {
					t.Fatal(args)
				}
				if p.config != filepath.Join(p.home, "PCSX2", "inis", "PCSX2.ini") {
					t.Fatal("PCSX2 appends its own subdirectory", p.config)
				}
			}
			if adapter == "duckstation" {
				if args[0] != "-batch" || args[2] != "--" {
					t.Fatal(args)
				}
			}
			if adapter == "eden" {
				if args[0] != "-f" || args[1] != "-g" {
					t.Fatal(args)
				}
			}
		})
	}
	for _, test := range []struct {
		adapter, file    string
		reference, valid bool
	}{
		{"duckstation", "game.chd", false, true}, {"duckstation", "game.cue", true, false},
		{"pcsx2", "game.iso", false, true}, {"pcsx2", "game.nsp", false, false},
		{"rpcs3", "EBOOT.BIN", false, false}, {"rpcs3", "EBOOT.BIN", true, true}, {"rpcs3", "game.iso", true, false},
		{"eden", "game.nro", false, true}, {"eden", "game.xci", false, true}, {"eden", "game.exe", false, false},
	} {
		if err := validateAdapterFormat(test.adapter, test.file, test.reference); (err == nil) != test.valid {
			t.Errorf("%+v: %v", test, err)
		}
	}
}

func TestOnboardingRestartCleansOnlyJournaledPartials(t *testing.T) {
	f, p := installedProfile(t, "eden")
	op := submit(t, f, Request{Kind: "emulator_prepare", Prepare: &ProfileRequest{ID: "eden"}})
	stage := filepath.Join(p.home, ".prepare-"+op.ID)
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stage, "partial"), "interrupted runtime copy")
	for _, base := range []string{p.paths.Settings, p.paths.Firmware} {
		write(t, filepath.Join(base, ".firmware-"+op.ID+".partial"), "interrupted firmware copy")
		write(t, filepath.Join(base, ".firmware-unrelated.partial"), "keep")
	}
	f.m.Close()
	manager, err := New(context.Background(), f.db, f.m.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.m = manager
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("journaled staging directory remains", err)
	}
	for _, base := range []string{p.paths.Settings, p.paths.Firmware} {
		if _, err := os.Stat(filepath.Join(base, ".firmware-"+op.ID+".partial")); !os.IsNotExist(err) {
			t.Fatal("journaled partial remains", err)
		}
		if b, err := os.ReadFile(filepath.Join(base, ".firmware-unrelated.partial")); err != nil || string(b) != "keep" {
			t.Fatal("unrelated file changed")
		}
	}
	if err = p.validateRouting(); err != nil {
		t.Fatal("configuration changed", err)
	}
}

func TestPCSX2PortableMarkerBlocksDataRootOverride(t *testing.T) {
	f, p := installedProfile(t, "pcsx2")
	write(t, filepath.Join(filepath.Dir(p.exe), "portable.txt"), "C:/existing-profile")
	if err := p.verifySource(context.Background()); err == nil {
		t.Fatal("portable marker would bypass storage routing")
	}
	if _, err := f.m.ConfigureEmulator(context.Background(), ConfigureEmulatorRequest{ID: "pcsx2"}); err == nil {
		t.Fatal("conflicting profile launched")
	}
}
