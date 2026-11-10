package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"github.com/KdotIV/NostalgiaCade/internal/library"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func readJSON(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1024*1024))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("unexpected data after JSON")
	}
	return nil
}
func Run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	runtime := runtimeClient{dataDir: defaultDataDir()}
	input := bufio.NewReader(in)
	print := func(value any) error { e := json.NewEncoder(out); e.SetIndent("", "  "); return e.Encode(value) }
	prompt := func(label string) (string, error) {
		fmt.Fprint(out, label+": ")
		line, err := input.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("input ended; setup was not applied")
		}
		return strings.Trim(strings.TrimSpace(line), `"`), ctx.Err()
	}
	var root *cobra.Command
	root = &cobra.Command{Use: "ncade", Short: "NostalgiaCade local cabinet operator CLI", SilenceUsage: true, SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			var err error
			runtime.dataDir, err = filepath.Abs(runtime.dataDir)
			return err
		}}
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(out)
	root.PersistentFlags().StringVar(&runtime.dataDir, "data-dir", runtime.dataDir, "Application state directory; content paths are selected by setup")
	call := func(method string, params any) error {
		var result any
		if err := runtime.call(ctx, method, params, &result); err != nil {
			return err
		}
		return print(result)
	}
	wait := func(op library.Operation) error {
		previous := ""
		for {
			if state := op.State + " / " + op.Phase; state != previous {
				fmt.Fprintf(out, "%s  %s\n", op.ID, state)
				previous = state
			}
			switch op.State {
			case "completed":
				return print(op)
			case "failed", "canceled":
				return fmt.Errorf("%s: %s (operation %s)", op.State, op.Error, op.ID)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("CLI stopped; operation %s continues in the core. Use operation cancel --id to cancel", op.ID)
			case <-time.After(500 * time.Millisecond):
			}
			if err := runtime.call(ctx, "operations.get", map[string]string{"id": op.ID}, &op); err != nil {
				return err
			}
		}
	}
	submit := func(r library.Request) error {
		var op library.Operation
		if err := runtime.call(ctx, "operations.submit", r, &op); err != nil {
			return err
		}
		return wait(op)
	}
	var configFile, storageRoot string
	var downloadHosts []string
	var onlyIfNeeded bool
	setup := &cobra.Command{Use: "setup", Short: "First-run guide: choose one root folder or individual storage paths", RunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("root") && configFile != "" {
			return fmt.Errorf("choose --root or --config, not both")
		}
		if cmd.Flags().Changed("download-host") && !cmd.Flags().Changed("root") {
			return fmt.Errorf("--download-host requires --root; otherwise specify hosts in the guide or config JSON")
		}
		var old library.Setup
		if err := runtime.call(ctx, "setup.status", nil, &old); err != nil {
			return err
		}
		if old.Configured && onlyIfNeeded {
			return nil
		}
		var value library.Setup
		if configFile != "" {
			if err := readJSON(configFile, &value); err != nil {
				return err
			}
		} else if cmd.Flags().Changed("root") {
			if storageRoot == "" {
				return fmt.Errorf("--root requires an explicit directory")
			}
			value = library.Setup{Root: storageRoot, DownloadHosts: old.DownloadHosts}
			if cmd.Flags().Changed("download-host") {
				value.DownloadHosts = downloadHosts
			}
		} else {
			if old.Configured {
				fmt.Fprintln(out, "Paths are already fixed for this library. To change download hosts, supply --config with the existing paths.")
				return print(old)
			}
			var err error
			value, err = setupGuide(runtime.dataDir, prompt, out)
			if err != nil {
				return err
			}
		}
		value, err := library.NormalizeSetup(value, runtime.dataDir)
		if err != nil {
			return err
		}
		return call("setup.configure", value)
	}}
	setup.Flags().StringVar(&configFile, "config", "", "Explicit setup JSON for noninteractive setup")
	setup.Flags().StringVar(&storageRoot, "root", "", "Create separate content subfolders under this explicit root (noninteractive)")
	setup.Flags().StringSliceVar(&downloadHosts, "download-host", nil, "Approved download hosts for --root; repeat or comma-separate")
	setup.Flags().BoolVar(&onlyIfNeeded, "if-needed", false, "Skip the guide when storage is already configured")
	root.AddCommand(setup)
	root.AddCommand(&cobra.Command{Use: "paths", Short: "Show configured storage", RunE: func(*cobra.Command, []string) error { return call("setup.status", nil) }})
	emulator := &cobra.Command{Use: "emulator", Short: "Install, onboard and configure emulator profiles"}
	var catalogFile, packageSlug, archivePath string
	readCatalog := func() (emucatalog.Catalog, error) {
		if catalogFile == "" {
			c := emucatalog.Builtin()
			return c, c.Validate()
		}
		f, err := os.Open(catalogFile)
		if err != nil {
			return emucatalog.Catalog{}, err
		}
		defer f.Close()
		return emucatalog.Read(f)
	}
	emulator.PersistentFlags().StringVar(&catalogFile, "catalog", "", "Reviewed catalog JSON exported by ncade-catalog; default is bundled snapshot")
	emulator.AddCommand(&cobra.Command{Use: "catalog", Short: "Show reviewed versions and download sources", RunE: func(*cobra.Command, []string) error {
		c, err := readCatalog()
		if err != nil {
			return err
		}
		return print(c)
	}})
	emulator.AddCommand(&cobra.Command{Use: "list", Short: "Show installed packages and launch support", RunE: func(*cobra.Command, []string) error { return call("emulators.installed", nil) }})
	install := &cobra.Command{Use: "install", Short: "Verify and install an emulator package without touching saves", RunE: func(*cobra.Command, []string) error {
		c, err := readCatalog()
		if err != nil {
			return err
		}
		for _, p := range c.Packages {
			if p.Slug == packageSlug {
				fmt.Fprintln(out, p.DeploymentReview)
				return submit(library.Request{Key: uuid.NewString(), Kind: "emulator_install", Install: &library.EmulatorInstall{Package: p, ArchivePath: archivePath}})
			}
		}
		return fmt.Errorf("emulator not found in reviewed catalog")
	}}
	install.Flags().StringVar(&packageSlug, "id", "", "Catalog emulator slug")
	install.Flags().StringVar(&archivePath, "archive", "", "Optional local official archive; still checked against catalog size and SHA-256")
	emulator.AddCommand(install)
	var profile library.Emulator
	add := &cobra.Command{Use: "add", RunE: func(*cobra.Command, []string) error { return call("emulators.register", profile) }}
	add.Flags().StringVar(&profile.ID, "id", "dolphin", "Local profile ID")
	add.Flags().StringVar(&profile.Adapter, "adapter", "dolphin", "dolphin, duckstation, pcsx2, rpcs3 or eden")
	add.Flags().StringVar(&profile.Executable, "exe", "", "Absolute path to the emulator executable")
	add.Flags().StringVar(&profile.Version, "version", "", "Reviewed version label")
	emulator.AddCommand(add)
	addEmulatorOnboarding(emulator, ctx, runtime, out, prompt, call, submit)
	root.AddCommand(emulator)
	var localPath, title, genre, emulatorID, key string
	var reference bool
	imp := &cobra.Command{Use: "import", Short: "Copy a local game into a persistent local library, or reference its original", RunE: func(*cobra.Command, []string) error {
		path, err := filepath.Abs(localPath)
		if err != nil || localPath == "" {
			return fmt.Errorf("--path is required")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("select a supported game file; for PS3 select EBOOT.BIN within the complete game folder and use --reference")
		}
		fmt.Fprintln(out, "Computing SHA-256 of the import…")
		h := sha256.New()
		if _, err = io.Copy(h, f); err != nil {
			return err
		}
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if key == "" {
			key = uuid.NewString()
		}
		e := library.Entry{ID: "import", Title: title, EmulatorID: emulatorID, Filename: filepath.Base(path), SHA256: fmt.Sprintf("%x", h.Sum(nil)), Bytes: info.Size(), Source: library.Source{Kind: "local", Path: path}}
		return submit(library.Request{Key: key, Kind: "import", Import: &library.ImportRequest{Entry: e, GenreID: genre, Reference: reference}})
	}}
	imp.Flags().StringVar(&localPath, "path", "", "Local source file; originals are never removed")
	imp.Flags().StringVar(&title, "title", "", "Display title")
	imp.Flags().StringVar(&genre, "genre", "", "Genre ID")
	imp.Flags().StringVar(&emulatorID, "emulator", "dolphin", "Registered emulator profile")
	imp.Flags().StringVar(&key, "key", "", "Retry-safe idempotency key")
	imp.Flags().BoolVar(&reference, "reference", false, "Reference the original file without copying")
	root.AddCommand(imp)
	var entryFile, downloadGenre, downloadKey string
	download := &cobra.Command{Use: "download", Short: "Download an authorized game using reviewed metadata", RunE: func(*cobra.Command, []string) error {
		var e library.Entry
		if err := readJSON(entryFile, &e); err != nil {
			return err
		}
		if e.Source.Kind != "download" {
			return fmt.Errorf("entry must use a download source")
		}
		if downloadKey == "" {
			downloadKey = uuid.NewString()
		}
		return submit(library.Request{Key: downloadKey, Kind: "import", Import: &library.ImportRequest{Entry: e, GenreID: downloadGenre}})
	}}
	download.Flags().StringVar(&entryFile, "entry", "", "Entry JSON with URL, authorization, size and SHA-256")
	download.Flags().StringVar(&downloadGenre, "genre", "", "Genre ID")
	download.Flags().StringVar(&downloadKey, "key", "", "Idempotency key")
	root.AddCommand(download)
	genres := &cobra.Command{Use: "genre", Short: "Plan or apply a remove-first managed collection switch"}
	for _, action := range []string{"plan", "switch"} {
		var manifestFile, switchKey string
		var yes bool
		cmd := &cobra.Command{Use: action, RunE: func(*cobra.Command, []string) error {
			var manifest library.Manifest
			if err := readJSON(manifestFile, &manifest); err != nil {
				return err
			}
			var plan library.Plan
			if err := runtime.call(ctx, "library.plan", manifest, &plan); err != nil {
				return err
			}
			if err := print(plan); err != nil {
				return err
			}
			if action == "plan" {
				return nil
			}
			if !yes {
				answer, err := prompt("Remove the listed managed genre games BEFORE acquisition? Type yes")
				if err != nil {
					return err
				}
				if !strings.EqualFold(answer, "yes") {
					return fmt.Errorf("switch canceled")
				}
			}
			if switchKey == "" {
				switchKey = uuid.NewString()
			}
			return submit(library.Request{Key: switchKey, Kind: "switch", Switch: &library.SwitchRequest{Manifest: manifest, ExpectedRevision: plan.Revision}})
		}}
		cmd.Flags().StringVar(&manifestFile, "manifest", "", "Local reviewed collection JSON")
		cmd.Flags().StringVar(&switchKey, "key", "", "Idempotency key")
		cmd.Flags().BoolVar(&yes, "yes", false, "Accept removal of the plan's old managed game files")
		genres.AddCommand(cmd)
	}
	root.AddCommand(genres)
	operations := &cobra.Command{Use: "operation", Short: "Inspect, retry or cancel durable work"}
	for _, action := range []string{"show", "retry", "cancel"} {
		var id string
		cmd := &cobra.Command{Use: action, RunE: func(*cobra.Command, []string) error {
			method := "operations." + action
			if action == "show" {
				method = "operations.get"
			}
			if action == "retry" {
				var op library.Operation
				if err := runtime.call(ctx, method, map[string]string{"id": id}, &op); err != nil {
					return err
				}
				return wait(op)
			}
			return call(method, map[string]string{"id": id})
		}}
		cmd.Flags().StringVar(&id, "id", "", "Operation ID")
		operations.AddCommand(cmd)
	}
	root.AddCommand(operations)
	root.AddCommand(&cobra.Command{Use: "games", Short: "List installed local and active-suite games", RunE: func(*cobra.Command, []string) error { return call("library.games", nil) }})
	var gameID string
	launch := &cobra.Command{Use: "launch", Short: "Launch one game through its pinned emulator adapter", RunE: func(*cobra.Command, []string) error { return call("session.launch", map[string]string{"id": gameID}) }}
	launch.Flags().StringVar(&gameID, "game", "", "Installed game ID")
	root.AddCommand(launch)
	var force bool
	stop := &cobra.Command{Use: "stop", Short: "Request a graceful exit from the owned emulator", RunE: func(*cobra.Command, []string) error {
		if force {
			fmt.Fprintln(out, "Forcing this session to exit; unsaved progress may be lost.")
		}
		return call("session.stop", map[string]bool{"force": force})
	}}
	stop.Flags().BoolVar(&force, "force", false, "Terminate only this tracked process if graceful exit is stuck")
	root.AddCommand(stop)
	session := &cobra.Command{Use: "session", Short: "Show game-session state", RunE: func(*cobra.Command, []string) error { return call("session.status", nil) }}
	var confirmed bool
	ack := &cobra.Command{Use: "acknowledge", Short: "Resolve an interrupted launch only after manually checking the emulator is closed", RunE: func(*cobra.Command, []string) error {
		return call("session.acknowledge", map[string]bool{"confirmed": confirmed})
	}}
	ack.Flags().BoolVar(&confirmed, "confirm-closed", false, "I verified the interrupted emulator is closed")
	session.AddCommand(ack)
	root.AddCommand(session)
	root.AddCommand(&cobra.Command{Use: "shutdown", Short: "Stop the core only when no operation or game is active", RunE: func(*cobra.Command, []string) error { return call("system.shutdown", nil) }})
	root.RunE = func(*cobra.Command, []string) error { return root.Help() }
	return root.ExecuteContext(ctx)
}
