package library

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ConfigureEmulatorRequest struct {
	ID           string `json:"id"`
	FirmwarePath string `json:"firmwarePath,omitempty"`
}

func (p profileLayout) command(game string) ([]string, []string) {
	switch p.e.Adapter {
	case "dolphin":
		if game == "" {
			return dolphinArgs(p.paths, p.e.ID, "")[3:], nil
		}
		return dolphinArgs(p.paths, p.e.ID, game), nil
	case "duckstation":
		if game == "" {
			return []string{}, nil
		}
		return []string{"-batch", "-fullscreen", "--", game}, nil
	case "pcsx2":
		args := []string{"-datapath", p.home}
		if game != "" {
			args = append(args, "-batch", "-fullscreen", "--", game)
		}
		return args, nil
	case "rpcs3":
		env := []string{"RPCS3_CONFIG_DIR=" + filepath.ToSlash(p.home) + "/"}
		if game == "" {
			return []string{}, env
		}
		return []string{"--no-gui", "--fullscreen", game}, env
	case "eden":
		if game == "" {
			return []string{}, nil
		}
		return []string{"-f", "-g", game}, nil
	}
	return nil, nil
}
func (m *Manager) ConfigureEmulator(ctx context.Context, r ConfigureEmulatorRequest) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.idle(); err != nil {
		return Session{}, err
	}
	p, err := m.profile(ctx, r.ID)
	if err != nil {
		return Session{}, err
	}
	args, env := p.command("")
	purpose := "configure"
	if r.FirmwarePath != "" {
		if p.e.Adapter != "rpcs3" {
			return Session{}, fail("unsupported_firmware", "native PUP installation is only supported by RPCS3")
		}
		path, err := cleanPath(r.FirmwarePath)
		if err != nil {
			return Session{}, err
		}
		st, err := os.Stat(path)
		if err != nil {
			return Session{}, err
		}
		if !st.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".pup") || st.Size() < 1024 || st.Size() > 1024*1024*1024 {
			return Session{}, fail("invalid_firmware", "select a PS3 update PUP file (maximum 1 GiB)")
		}
		file, err := os.Open(path)
		if err != nil {
			return Session{}, err
		}
		header := make([]byte, 8)
		_, err = io.ReadFull(file, header)
		file.Close()
		if err != nil || string(header) != "SCEUF\x00\x00\x00" {
			return Session{}, fail("invalid_firmware", "unrecognized PS3 PUP header")
		}
		if _, err = os.Stat(filepath.Join(p.paths.Firmware, p.e.ID, "dev_flash", "vsh", "module", "vsh.self")); !os.IsNotExist(err) {
			return Session{}, fail("firmware_conflict", "firmware is already installed; preserved. Use native settings for an intentional update")
		}
		args = []string{"--installfw", path}
		purpose = "firmware"
	}
	return m.startProfileSession(ctx, p, "", purpose, args, env)
}

func validateAdapterFormat(adapter, filename string, reference bool) error {
	if _, err := supportedExtension(filename); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(filename))
	formats := map[string]string{
		"dolphin":     ".iso .gcm .rvz .wbfs .ciso .gcz .dol .elf",
		"duckstation": ".chd .pbp .exe .ps-exe",
		"pcsx2":       ".iso .chd .cso .gz .elf",
		"rpcs3":       ".bin .elf .self",
		"eden":        ".nsp .xci .nro",
	}
	for _, allowed := range strings.Fields(formats[adapter]) {
		if ext == allowed {
			if adapter == "rpcs3" && !reference {
				return fail("reference_required", "RPCS3 boot files require a local --reference import with the complete game folder kept in place")
			}
			if adapter == "rpcs3" && ext == ".bin" && !strings.EqualFold(filename, "EBOOT.BIN") {
				return fail("unsupported_format", "RPCS3 BIN imports must select EBOOT.BIN")
			}
			return nil
		}
	}
	return fail("unsupported_format", "%s does not support this format in NCade; use a supported self-contained image (PS1: CHD/PBP; PS2: ISO/CHD; PS3: referenced EBOOT.BIN/ELF/SELF; Switch: NSP/XCI/NRO)", adapter)
}
