package pilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"github.com/KdotIV/NostalgiaCade/internal/library"
)

type Selection struct {
	Genre            string             `json:"genre"`
	ReleaseID        string             `json:"releaseId"`
	CatalogEntries   int                `json:"catalogEntries"`
	AvailableEntries int                `json:"availableEntries"`
	Missing          []string           `json:"missing"`
	Partial          bool               `json:"partial"`
	Manifest         library.Manifest   `json:"manifest"`
	Emulators        emucatalog.Catalog `json:"emulators"`
	Onboarding       []Prerequisite     `json:"onboarding"`
}

func Plan(ctx context.Context, db *sql.DB, genre string, bindings Bindings, availableOnly bool) (Selection, error) {
	p := Selection{Genre: genre, Missing: []string{}, Manifest: library.Manifest{Version: 1, GenreID: genre, Games: []library.Entry{}}}
	if err := bindings.Validate(); err != nil {
		return p, err
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT s.selected_release_id,r.status FROM catalog.suites s JOIN catalog.suite_releases r ON r.id=s.selected_release_id WHERE s.genre_id=$1`, genre).Scan(&p.ReleaseID, &state); err != nil {
		return p, fmt.Errorf("pilot genre is not seeded")
	}
	if state != "draft" {
		return p, fmt.Errorf("pilot workflow requires a draft release; use the production signed-manifest workflow for published suites")
	}
	p.Manifest.ID = "pilot-" + genre + "-v1"
	rows, err := db.QueryContext(ctx, `SELECT e.game_key,g.title,g.release_year,p.slug,e.emulator_slug,g.players_max FROM catalog.suite_games s JOIN catalog.game_editions e ON e.id=s.edition_id JOIN catalog.games g ON g.id=e.game_id JOIN catalog.platforms p ON p.id=e.platform_id JOIN catalog.game_genres gg ON gg.game_id=g.id AND gg.genre_id=$2 WHERE s.release_id=$1 ORDER BY s.display_order`, p.ReleaseID, genre)
	if err != nil {
		return p, err
	}
	games := []Candidate{}
	for rows.Next() {
		g := Candidate{Genre: genre}
		if err = rows.Scan(&g.Key, &g.Title, &g.Year, &g.Platform, &g.Emulator, &g.PlayersMax); err != nil {
			rows.Close()
			return p, err
		}
		if g.Key == "" || g.Emulator == "" {
			rows.Close()
			return p, fmt.Errorf("invalid candidate metadata")
		}
		games = append(games, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	p.CatalogEntries = len(games)
	if p.CatalogEntries != 20 {
		return p, fmt.Errorf("a pilot genre must contain exactly 20 candidate entries")
	}
	for _, g := range games {
		b, ok := bindings.Games[g.Key]
		if !ok {
			entry, found, e := downloadEntry(ctx, db, g)
			if e != nil {
				return p, e
			}
			if found {
				p.Manifest.Games = append(p.Manifest.Games, entry)
			} else {
				p.Missing = append(p.Missing, g.Key+": local import or approved download required")
			}
			continue
		}
		var registered bool
		if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM catalog.game_edition_artifacts s JOIN catalog.game_editions e ON e.id=s.edition_id JOIN catalog.artifacts a ON a.id=s.artifact_id WHERE e.game_key=$1 AND a.sha256=$2 AND a.download_bytes=$3 AND s.reference_only=$4 AND s.filename=$5)`, g.Key, b.SHA256, b.Bytes, b.Reference, b.Filename).Scan(&registered); err != nil {
			return p, err
		}
		if !registered {
			return p, fmt.Errorf("%s source is not registered in the remote catalog; seed the reviewed binding first", g.Key)
		}
		info, e := os.Stat(b.Path)
		if e != nil || !info.Mode().IsRegular() || info.Size() != b.Bytes {
			p.Missing = append(p.Missing, g.Key+": import is unavailable or changed")
			continue
		}
		p.Manifest.Games = append(p.Manifest.Games, library.Entry{ID: g.Key, Title: g.Title, EmulatorID: g.Emulator, Filename: b.Filename, SHA256: b.SHA256, Bytes: b.Bytes, Reference: b.Reference, Source: library.Source{Kind: "local", Path: b.Path}})
	}
	p.AvailableEntries = len(p.Manifest.Games)
	p.Partial = len(p.Missing) > 0
	p.Emulators, err = emucatalog.Export(ctx, db)
	if err != nil {
		return p, err
	}
	if err = p.Emulators.Validate(); err != nil {
		return p, err
	}
	p.Onboarding, err = LoadPrerequisites(ctx, db)
	if err != nil {
		return p, err
	}
	// Suites retain their explicitly selected emulator releases even when a
	// newer package is added to the catalog later. Other adapters use the latest
	// reviewed catalog entry for the user's install-all-emulators test.
	pinned, err := db.QueryContext(ctx, `SELECT DISTINCT ep.manifest FROM catalog.suite_games sg JOIN catalog.emulator_releases ep ON ep.id=sg.emulator_release_id WHERE sg.release_id=$1`, p.ReleaseID)
	if err != nil {
		return p, err
	}
	seen := map[string]string{}
	for pinned.Next() {
		var body []byte
		var pkg emucatalog.Package
		if err = pinned.Scan(&body); err != nil {
			pinned.Close()
			return p, err
		}
		if err = json.Unmarshal(body, &pkg); err != nil {
			pinned.Close()
			return p, err
		}
		if err = pkg.Validate(); err != nil {
			pinned.Close()
			return p, err
		}
		if previous, exists := seen[pkg.Slug]; exists && previous != pkg.SHA256 {
			pinned.Close()
			return p, fmt.Errorf("suite requires conflicting emulator versions")
		}
		seen[pkg.Slug] = pkg.SHA256
		for i := range p.Emulators.Packages {
			if p.Emulators.Packages[i].Slug == pkg.Slug {
				p.Emulators.Packages[i] = pkg
			}
		}
	}
	err = pinned.Err()
	pinned.Close()
	if err != nil {
		return p, err
	}
	if p.Partial && !availableOnly {
		return p, fmt.Errorf("%d of 20 game imports are missing; no installation or removal was started (use --available-only explicitly for a partial E2E test)", len(p.Missing))
	}
	if p.AvailableEntries == 0 {
		return p, fmt.Errorf("no usable imports for this genre; no installation or removal was started")
	}
	return p, nil
}

// downloadEntry uses only approved, fully described game payloads. Research
// metadata and save/firmware artifacts can never become a game download.
func downloadEntry(ctx context.Context, db *sql.DB, game Candidate) (library.Entry, bool, error) {
	entry := library.Entry{ID: game.Key, Title: game.Title, EmulatorID: game.Emulator, Source: library.Source{Kind: "download", Authorized: true}}
	err := db.QueryRowContext(ctx, `SELECT a.sha256,a.download_bytes,l.filename,s.source_url,a.license_evidence_url FROM catalog.game_editions e JOIN catalog.game_edition_artifacts l ON l.edition_id=e.id JOIN catalog.artifacts a ON a.id=l.artifact_id JOIN catalog.artifact_sources s ON s.artifact_id=a.id WHERE e.game_key=$1 AND a.kind='game' AND a.permission_status='approved' AND a.license_evidence_url IS NOT NULL AND s.mode='authorized_url' AND l.filename IS NOT NULL AND NOT l.reference_only ORDER BY a.created_at DESC,a.id,s.id LIMIT 1`, game.Key).Scan(&entry.SHA256, &entry.Bytes, &entry.Filename, &entry.Source.URL, &entry.Source.AuthorizationURL)
	if err == sql.ErrNoRows {
		return entry, false, nil
	}
	return entry, err == nil, err
}
