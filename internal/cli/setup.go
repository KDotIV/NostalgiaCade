package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/KdotIV/NostalgiaCade/internal/library"
)

func setupGuide(dataDir string, prompt func(string) (string, error), out io.Writer) (library.Setup, error) {
	fmt.Fprintln(out, "Choose how to organize your content. No folder is preselected.\nSaves, settings, firmware and emulators survive genre changes.")
	var value library.Setup
	for {
		value = library.Setup{}
		fmt.Fprintln(out, "1) One root folder (creates Emulators, ROMs, Firmware, Saves, Settings and Cache subfolders)\n2) Individual folders\nq) Cancel")
		choice, err := prompt("Storage layout [1/2/q]")
		if err != nil {
			return value, err
		}
		switch strings.ToLower(choice) {
		case "1", "root":
			value.Root, err = prompt("Root folder (for example C:/.ncade or D:/ROMS.bk/ncade)")
			if err != nil {
				return value, err
			}
			if value.Root == "" {
				fmt.Fprintln(out, "Enter a root folder explicitly.")
				continue
			}
		case "2", "individual":
			for _, field := range []struct {
				label string
				path  *string
			}{
				{"Emulators folder", &value.Paths.Emulators}, {"ROMs folder", &value.Paths.ROMs},
				{"Firmware / BIOS archive folder", &value.Paths.Firmware}, {"Saves folder", &value.Paths.Saves},
				{"Settings / Dolphin user-data folder", &value.Paths.Settings}, {"Cache folder", &value.Paths.Cache},
			} {
				*field.path, err = prompt(field.label)
				if err != nil {
					return value, err
				}
			}
		case "q", "cancel":
			return value, fmt.Errorf("setup canceled; no paths saved")
		default:
			fmt.Fprintln(out, "Choose 1 for one root folder, 2 for individual folders, or q to cancel.")
			continue
		}
		resolved, err := library.NormalizeSetup(value, dataDir)
		if err != nil {
			fmt.Fprintf(out, "Cannot use these folders: %s\nPlease choose the layout and paths again. Nothing has been saved.\n\n", err)
			continue
		}
		value = resolved
		break
	}
	for {
		hosts, err := prompt("Approved download hosts (comma-separated; leave blank to disable downloads)")
		if err != nil {
			return value, err
		}
		value.DownloadHosts = []string{}
		if hosts != "" {
			value.DownloadHosts = strings.Split(hosts, ",")
		}
		resolved, err := library.NormalizeSetup(value, dataDir)
		if err != nil {
			fmt.Fprintln(out, err)
			continue
		}
		value = resolved
		break
	}
	fmt.Fprintf(out, "Application database and connection files: %s\n", dataDir)
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return value, err
	}
	answer, err := prompt("Create/use these folders? Type yes")
	if err != nil {
		return value, err
	}
	if !strings.EqualFold(answer, "yes") {
		return value, fmt.Errorf("setup canceled; no paths saved")
	}
	return value, nil
}
