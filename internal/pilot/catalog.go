package pilot

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"github.com/google/uuid"
)

//go:embed candidates.txt
var candidates string

type Candidate struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Genre      string `json:"genre"`
	Year       int    `json:"year"`
	Platform   string `json:"platform"`
	Emulator   string `json:"emulator"`
	PlayersMax int    `json:"playersMax"`
}

var platforms = map[string][2]string{"ps1": {"PlayStation", "duckstation"}, "ps2": {"PlayStation 2", "pcsx2"}, "ps3": {"PlayStation 3", "rpcs3"}, "gamecube": {"GameCube", "dolphin"}, "wii": {"Wii", "dolphin"}}
var GenreOrder = []string{"racing", "fighting", "action-adventure", "platform", "hack-and-slash"}
var research = map[string][]string{
	"racing":           {"https://www.gran-turismo.com/gb/news/00_8342103.html", "https://www.digitaltrends.com/gaming/best-racing-games/", "https://www.playstation.com/en-us/gran-turismo/"},
	"fighting":         {"https://gameinformer.com/2019/04/25/the-30-greatest-fighting-games-of-all-time"},
	"action-adventure": {"https://www.metacritic.com/browse/game/all/action-adventure/"},
	"platform":         {"https://www.metacritic.com/browse/game/all/3d-platformer/", "https://gameranx.com/features/id/139851/article/every-3d-platformer-ever-made/"},
	"hack-and-slash":   {"https://www.dualshockers.com/best-hack-and-slash-video-games/", "https://www.truetrophies.com/genre/Hack-and-Slash"},
}

func ID(key string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("nostalgiacade/pilot/"+key)).String()
}
func Candidates() []Candidate {
	r := csv.NewReader(strings.NewReader(candidates))
	r.Comma = '|'
	r.Comment = '#'
	r.FieldsPerRecord = 6
	records, err := r.ReadAll()
	if err != nil {
		panic(err)
	}
	out := []Candidate{}
	for _, v := range records {
		year, _ := strconv.Atoi(v[3])
		players, _ := strconv.Atoi(v[5])
		out = append(out, Candidate{Key: v[1], Title: v[2], Genre: v[0], Year: year, Platform: v[4], Emulator: platforms[v[4]][1], PlayersMax: players})
	}
	return out
}

// Seed uses the database administrator's privileges. Subject is audit
// attribution, not a caller-supplied role bypass. No cabinet RPC can invoke it.
func Seed(ctx context.Context, db *sql.DB, subject string, bindings Bindings) error {
	if strings.TrimSpace(subject) == "" {
		return fmt.Errorf("publisher subject is required")
	}
	if err := bindings.Validate(); err != nil {
		return err
	}
	catalog, err := emucatalog.Export(ctx, db)
	if err != nil {
		return err
	}
	releases := map[string]string{}
	for _, p := range catalog.Packages {
		var id string
		if err = db.QueryRowContext(ctx, `SELECT r.id FROM catalog.emulator_releases r JOIN catalog.emulators e ON e.id=r.emulator_id WHERE e.slug=$1 AND r.upstream_version=$2 AND r.os='windows' AND r.architecture='x64'`, p.Slug, p.Version).Scan(&id); err != nil {
			return err
		}
		releases[p.Slug] = id
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(72643002)`); err != nil {
		return err
	}
	for slug, platform := range platforms {
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.platforms(id,slug,name,category) VALUES($1,$2,$3,'console') ON CONFLICT(slug) DO NOTHING`, ID("platform/"+slug), slug, platform[0]); err != nil {
			return err
		}
	}
	for _, genre := range GenreOrder {
		suiteID, releaseID := ID("suite/"+genre), ID("release/"+genre+"/1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.suites(id,genre_id,name) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, suiteID, genre, "Pilot "+genre+" (1990–2015)"); err != nil {
			return err
		}
		manifest := jsonText(map[string]any{"kind": "pilot-candidate-plan", "version": 1, "genreId": genre, "yearMin": 1990, "yearMax": 2015, "targetEntries": 20, "curationStatus": "draft", "selection": "Popularity-informed candidates constrained to supported adapters; not a sales ranking or compatibility certification", "research": research[genre]})
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.suite_releases(id,suite_id,version,status,manifest,created_by_subject) VALUES($1,$2,1,'draft',$3,$4) ON CONFLICT(id) DO NOTHING`, releaseID, suiteID, manifest, subject); err != nil {
			return err
		}
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT status FROM catalog.suite_releases WHERE id=$1 FOR UPDATE`, releaseID).Scan(&status); err != nil {
			return err
		}
		if status != "draft" {
			return fmt.Errorf("refusing to mutate a published pilot release")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE catalog.suites SET selected_release_id=$2 WHERE id=$1 AND selected_release_id IS NULL`, suiteID, releaseID); err != nil {
			return err
		}
	}
	order := map[string]int{}
	for _, g := range Candidates() {
		gameID, editionID := ID("game/"+g.Key), ID("edition/"+g.Key)
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.games(id,title,release_year,players_min,players_max,created_by_subject) VALUES($1,$2,$3,1,$4,$5) ON CONFLICT(id) DO NOTHING`, gameID, g.Title, g.Year, g.PlayersMax, subject); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.game_genres(game_id,genre_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, gameID, g.Genre); err != nil {
			return err
		}
		var platformID string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM catalog.platforms WHERE slug=$1`, g.Platform).Scan(&platformID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.game_editions(id,game_id,platform_id,region,revision) VALUES($1,$2,$3,'operator-supplied','verify-on-import') ON CONFLICT(id) DO NOTHING`, editionID, gameID, platformID); err != nil {
			return err
		}
		metadata := jsonText(map[string]any{"candidate": g, "metadataReview": "draft", "localPlayersReview": "pending", "compatibility": "unverified", "commercialUse": "unreviewed", "sourcePolicy": "user_import", "cabinetProfile": "controller mapping and performance validation required", "research": research[g.Genre]})
		if _, err = tx.ExecContext(ctx, `UPDATE catalog.game_editions SET game_key=$1,emulator_slug=$3,metadata=$4 WHERE id=$2 AND game_key IS NULL`, g.Key, editionID, g.Emulator, metadata); err != nil {
			return err
		}
		if releases[g.Emulator] == "" {
			return fmt.Errorf("missing emulator release for %s", g.Emulator)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.suite_games(release_id,edition_id,emulator_release_id,display_order) VALUES($1,$2,$3,$4) ON CONFLICT(release_id,edition_id) DO NOTHING`, ID("release/"+g.Genre+"/1"), editionID, releases[g.Emulator], order[g.Genre]); err != nil {
			return err
		}
		order[g.Genre]++
		if b, ok := bindings.Games[g.Key]; ok {
			artifactID := ID("game-artifact/" + b.SHA256)
			if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.artifacts(id,kind,sha256,download_bytes,installed_bytes,format,permission_status) VALUES($1,'game',$2,$3,$3,$4,'unreviewed') ON CONFLICT(sha256,kind) DO NOTHING`, artifactID, b.SHA256, b.Bytes, b.Format); err != nil {
				return err
			}
			var size int64
			if err = tx.QueryRowContext(ctx, `SELECT id,download_bytes FROM catalog.artifacts WHERE kind='game' AND sha256=$1`, b.SHA256).Scan(&artifactID, &size); err != nil {
				return err
			}
			if size != b.Bytes {
				return fmt.Errorf("conflicting game artifact size")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.artifact_sources(id,artifact_id,mode) VALUES($1,$2,'user_import') ON CONFLICT(id) DO NOTHING`, ID("import-source/"+b.SHA256), artifactID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.game_edition_artifacts(edition_id,artifact_id,role,filename,reference_only) VALUES($1,$2,'operator-import',$3,$4) ON CONFLICT DO NOTHING`, editionID, artifactID, b.Filename, b.Reference); err != nil {
				return err
			}
		}
	}
	for _, step := range Prerequisites() {
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.onboarding_steps(emulator_slug,step_key,instructions) VALUES($1,$2,$3) ON CONFLICT(emulator_slug,step_key) DO NOTHING`, step.Emulator, step.Kind, jsonText(step)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type Prerequisite struct {
	Emulator     string `json:"emulator"`
	Kind         string `json:"kind"`
	RelativePath string `json:"relativePath"`
	GuideURL     string `json:"guideUrl"`
	Note         string `json:"note"`
}

func Prerequisites() []Prerequisite {
	return []Prerequisite{
		{"duckstation", "bios", "duckstation/bios/bios.bin", "https://github.com/stenzek/duckstation#bios", "Supply your own BIOS; preserve cards and settings."},
		{"pcsx2", "bios", "pcsx2/bios/bios.bin", "https://pcsx2.net/docs/usage/setup/", "Supply your own BIOS; complete native controller setup."},
		{"rpcs3", "ps3-flash", "rpcs3/dev_flash", "https://rpcs3.net/quickstart", "Import an extracted dev_flash or use the native first-time PUP installer. Clear the welcome screen for unattended launches."},
		{"eden", "switch-keys", "eden/keys/prod.keys", "https://eden-emu.dev/", "Supply keys from your own console; never publish key contents."},
		{"eden", "switch-firmware", "eden/nand/system/Contents/registered", "https://eden-emu.dev/", "Supply extracted firmware. Startup tested; shutdown reliability remains unqualified."},
	}
}

func LoadPrerequisites(ctx context.Context, db *sql.DB) ([]Prerequisite, error) {
	rows, err := db.QueryContext(ctx, `SELECT emulator_slug,step_key,instructions FROM catalog.onboarding_steps ORDER BY emulator_slug,step_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := []Prerequisite{}
	for rows.Next() {
		var emulator, key string
		var body []byte
		var step Prerequisite
		if err = rows.Scan(&emulator, &key, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &step); err != nil {
			return nil, err
		}
		path := filepath.FromSlash(step.RelativePath)
		if step.Emulator != emulator || step.Kind != key || path == "" || filepath.IsAbs(path) || filepath.Clean(path) == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) || strings.Contains(path, ":") {
			return nil, fmt.Errorf("invalid onboarding step")
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}
