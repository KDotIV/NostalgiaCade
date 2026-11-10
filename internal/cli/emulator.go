package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/KdotIV/NostalgiaCade/internal/library"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func addEmulatorOnboarding(parent *cobra.Command, ctx context.Context, runtime runtimeClient, out io.Writer, prompt func(string) (string, error), call func(string, any) error, submit func(library.Request) error) {
	var id, kind, path, pup string
	var apply bool
	status := &cobra.Command{Use: "status", Short: "Show profile paths and missing BIOS/firmware prerequisites", RunE: func(*cobra.Command, []string) error { return call("emulators.profile", library.ProfileRequest{ID: id}) }}
	status.Flags().StringVar(&id, "id", "", "Registered profile ID")
	_ = status.MarkFlagRequired("id")
	parent.AddCommand(status)
	prepare := func() error {
		return submit(library.Request{Key: uuid.NewString(), Kind: "emulator_prepare", Prepare: &library.ProfileRequest{ID: id}})
	}
	prep := &cobra.Command{Use: "prepare", Short: "Preview isolated profile paths; --apply creates missing files without resetting settings", RunE: func(*cobra.Command, []string) error {
		if !apply {
			return call("emulators.profile", library.ProfileRequest{ID: id})
		}
		return prepare()
	}}
	prep.Flags().StringVar(&id, "id", "", "Registered profile ID")
	_ = prep.MarkFlagRequired("id")
	prep.Flags().BoolVar(&apply, "apply", false, "Create the isolated profile; existing configuration is preserved")
	parent.AddCommand(prep)
	firmware := func(kind, path string) error {
		return submit(library.Request{Key: uuid.NewString(), Kind: "firmware_import", Firmware: &library.FirmwareRequest{ID: id, Kind: kind, Path: path}})
	}
	fw := &cobra.Command{Use: "firmware", Short: "Copy personally supplied BIOS, keys or extracted firmware without overwriting files", RunE: func(*cobra.Command, []string) error { return firmware(kind, path) }}
	fw.Flags().StringVar(&id, "id", "", "Registered profile ID")
	fw.Flags().StringVar(&kind, "kind", "", "bios, ps3-flash, switch-keys or switch-firmware")
	fw.Flags().StringVar(&path, "path", "", "Absolute source file/directory (original is preserved)")
	for _, flag := range []string{"id", "kind", "path"} {
		_ = fw.MarkFlagRequired(flag)
	}
	parent.AddCommand(fw)
	configure := &cobra.Command{Use: "configure", Short: "Open the isolated emulator's native settings or PS3 firmware installer as a tracked session", RunE: func(*cobra.Command, []string) error {
		return call("emulators.configure", library.ConfigureEmulatorRequest{ID: id, FirmwarePath: pup})
	}}
	configure.Flags().StringVar(&id, "id", "", "Registered profile ID")
	_ = configure.MarkFlagRequired("id")
	configure.Flags().StringVar(&pup, "firmware", "", "RPCS3 only: first-time installation from a local PS3UPDAT.PUP")
	parent.AddCommand(configure)
	onboard := &cobra.Command{Use: "onboard", Short: "Interactive guide for profile paths, BIOS/firmware and controller/graphics configuration", RunE: func(*cobra.Command, []string) error {
		var state library.ProfileStatus
		if err := runtime.call(ctx, "emulators.profile", library.ProfileRequest{ID: id}, &state); err != nil {
			return err
		}
		if err := call("emulators.profile", library.ProfileRequest{ID: id}); err != nil {
			return err
		}
		fmt.Fprintln(out, "These locations use your selected storage roots. Existing emulator profiles are kept separate; no saves/settings are copied or reset.")
		if !state.Prepared {
			answer, err := prompt("Create this profile? Type yes")
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "yes") {
				return nil
			}
			if err = prepare(); err != nil {
				return err
			}
		}
		var steps []struct{ kind, label string }
		switch state.Adapter {
		case "duckstation", "pcsx2":
			steps = append(steps, struct{ kind, label string }{"bios", "BIOS file"})
		case "rpcs3":
			steps = append(steps, struct{ kind, label string }{"ps3-flash", "Existing dev_flash folder (use configure --firmware for a PUP instead)"})
		case "eden":
			steps = append(steps, struct{ kind, label string }{"switch-keys", "prod.keys file"}, struct{ kind, label string }{"switch-firmware", "Extracted firmware folder containing NCA files"})
		}
		for _, step := range steps {
			source, err := prompt(step.label + "; Enter to skip/keep installed")
			if err != nil {
				return err
			}
			if source != "" {
				if err = firmware(step.kind, source); err != nil {
					return err
				}
			}
		}
		if err := call("emulators.profile", library.ProfileRequest{ID: id}); err != nil {
			return err
		}
		fmt.Fprintln(out, "Use native settings to bind controls and choose graphics/CPU options. Close the emulator before importing or switching genres. Use 'stop' for a graceful exit.")
		answer, err := prompt("Open the isolated settings UI now? Type yes")
		if err != nil {
			return err
		}
		if !strings.EqualFold(answer, "yes") {
			return nil
		}
		return call("emulators.configure", library.ConfigureEmulatorRequest{ID: id})
	}}
	onboard.Flags().StringVar(&id, "id", "", "Registered profile ID")
	_ = onboard.MarkFlagRequired("id")
	parent.AddCommand(onboard)
}
