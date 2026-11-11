// ncade-pilot is a trusted operator-side E2E coordinator. It is never bundled
// into the player desktop and never passes PostgreSQL credentials over IPC.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/KdotIV/NostalgiaCade/internal/database"
	"github.com/KdotIV/NostalgiaCade/internal/pilot"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	action := flag.String("action", "list", "schema, seed, scan, research, list, report, plan, install or sync")
	researchFile := flag.String("research-file", "", "Reviewed public archive metadata JSON; never enables game downloads")
	roots := flag.String("sources", "", "Semicolon-separated source directories; scan reads and hashes without moving games")
	bindingsPath := flag.String("bindings", "", "Local source bindings JSON")
	output := flag.String("out", "", "JSON output path (scan requires this)")
	subject := flag.String("subject", "", "Administrator attribution; database role supplies privileges")
	genre := flag.String("genre", "", "Genre ID")
	partial := flag.Bool("available-only", false, "Explicit partial E2E selection; missing catalog entries remain missing")
	allowDraft := flag.Bool("allow-draft", false, "Permit operator-only testing of an unsigned draft catalog")
	yes := flag.Bool("yes", false, "Accept the remove-first installation plan")
	dataDir := flag.String("data-dir", "", "Cabinet application data directory")
	root := flag.String("root", "", "User-selected storage root for an unconfigured cabinet")
	cabinet := flag.String("cabinet", "", "Stable name for this E2E cabinet (never share across different data directories)")
	archives := flag.String("archives", "", "Optional directory containing reviewed slug.zip/slug.7z emulator archives")
	firmware := flag.String("firmware-from", "", "Existing NCade Firmware directory to copy prerequisites from")
	journal := flag.String("journal", "", "Local pilot-runs journal for read-only operation reconciliation")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	emit := func(v any) error {
		if *output != "" {
			b, e := json.MarshalIndent(v, "", "  ")
			if e != nil {
				return e
			}
			return os.WriteFile(*output, b, 0600)
		}
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		return e.Encode(v)
	}
	switch *action {
	case "schema", "seed", "scan", "research", "list", "report", "plan", "install", "sync":
	default:
		return fmt.Errorf("unknown pilot action")
	}
	if *action == "scan" {
		if *roots == "" || *output == "" {
			return fmt.Errorf("scan requires --sources and --out")
		}
		b, err := pilot.Scan(ctx, strings.Split(*roots, ";"))
		if err != nil {
			return err
		}
		return emit(b)
	}
	if *action == "sync" {
		if *journal == "" || *dataDir == "" {
			return fmt.Errorf("sync requires --journal and --data-dir")
		}
		if err := pilot.SyncJournal(ctx, *dataDir, *journal); err != nil {
			return fmt.Errorf("report reconciliation failed: %s", safeError(err))
		}
		return emit(map[string]bool{"synced": true})
	}
	bindings, err := pilot.ReadBindings(*bindingsPath)
	if err != nil {
		return err
	}
	db, err := database.OpenPostgres(ctx, os.Getenv("NCADE_POSTGRES_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	if *action == "research" {
		var records []pilot.FileResearch
		if err = pilot.ReadJSON(*researchFile, &records); err != nil {
			return err
		}
		if err = pilot.PublishResearch(ctx, db, *subject, records); err != nil {
			return fmt.Errorf("research import failed: %s", safeError(err))
		}
		return emit(map[string]any{"researchObservations": len(records), "downloadAuthorization": "unchanged"})
	}
	if *action == "report" {
		result, e := pilot.Report(ctx, db)
		if e != nil {
			return fmt.Errorf("report failed: %s", safeError(e))
		}
		return emit(result)
	}
	if *action == "schema" {
		if !*yes {
			return fmt.Errorf("catalog cleanup requires --yes; back up the remote database first")
		}
		if err = pilot.Migrate(ctx, db); err != nil {
			return fmt.Errorf("pilot schema extension failed: %s", safeError(err))
		}
		return emit(map[string]any{"catalogSchema": "005_catalog_cleanup.sql"})
	}
	if *action == "seed" {
		if *subject == "" {
			return fmt.Errorf("seed requires --subject")
		}
		if err = pilot.Seed(ctx, db, *subject, bindings); err != nil {
			return fmt.Errorf("catalog seed failed: %s", safeError(err))
		}
		return emit(map[string]any{"candidates": 100, "perGenre": 20, "sources": len(bindings.Games), "status": "draft", "years": "1990-2015"})
	}
	if *action == "list" {
		type summary struct {
			Genre        string `json:"genre"`
			Entries      int    `json:"entries"`
			BoundSources int    `json:"boundSources"`
		}
		rows, e := db.QueryContext(ctx, `SELECT g.genre_id,count(DISTINCT p.game_key),count(DISTINCT s.edition_id) FROM catalog.suites g JOIN catalog.suite_games sg ON sg.release_id=g.selected_release_id JOIN catalog.game_editions p ON p.id=sg.edition_id LEFT JOIN catalog.game_edition_artifacts s ON s.edition_id=p.id GROUP BY g.genre_id ORDER BY g.genre_id`)
		if e != nil {
			return fmt.Errorf("cannot read pilot catalog; apply the extension and seed first")
		}
		defer rows.Close()
		out := []summary{}
		for rows.Next() {
			var v summary
			if e = rows.Scan(&v.Genre, &v.Entries, &v.BoundSources); e != nil {
				return e
			}
			out = append(out, v)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		return emit(out)
	}
	selection, err := pilot.Plan(ctx, db, *genre, bindings, *partial)
	if *action == "plan" {
		if emitErr := emit(selection); emitErr != nil {
			return emitErr
		}
		return err
	}
	if err != nil {
		return err
	}
	if !*allowDraft || !*yes {
		return fmt.Errorf("pilot install requires --allow-draft and --yes; inspect --action plan first")
	}
	if *dataDir == "" || *cabinet == "" || *subject == "" {
		return fmt.Errorf("install requires --data-dir, --cabinet and --subject")
	}
	if *archives != "" {
		*archives, err = filepath.Abs(*archives)
		if err != nil {
			return err
		}
	}
	result, err := pilot.Install(ctx, selection, pilot.InstallOptions{DataDir: *dataDir, Root: *root, Cabinet: *cabinet, Subject: *subject, Archives: *archives, FirmwareFrom: *firmware}, func(s string) { fmt.Fprintln(os.Stderr, s) })
	if e := emit(result); err == nil {
		err = e
	}
	return err
}
func safeError(err error) string {
	// SQL errors may contain value/detail text. Keep only the PostgreSQL code in
	// operator output; domain errors are not DSNs but may include local paths.
	type sqlState interface{ SQLState() string }
	if e, ok := err.(sqlState); ok {
		return "PostgreSQL " + e.SQLState() + " (check schema and privileges)"
	}
	return err.Error()
}
