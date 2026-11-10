package pilot

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// FileResearch describes publisher-reported archive metadata, never an
// installable source. SHA-1/MD5 are retained for provenance, not verification.
type FileResearch struct {
	GameKey    string `json:"gameKey"`
	SourcePage string `json:"sourcePage"`
	IndexPage  string `json:"indexPage"`
	Filename   string `json:"filename"`
	Bytes      int64  `json:"bytes"`
	SHA1       string `json:"sha1"`
	MD5        string `json:"md5"`
	Container  string `json:"container"`
	Region     string `json:"region"`
	ObservedAt string `json:"observedAt"`
}

func (r FileResearch) Validate() error {
	known := false
	for _, g := range Candidates() {
		if g.Key == r.GameKey && g.Platform == "ps2" {
			known = true
			break
		}
	}
	source, err := url.Parse(r.SourcePage)
	if !known || err != nil || source.Scheme != "https" || source.Host != "archive.org" || source.User != nil || source.RawQuery != "" || source.Fragment != "" || !regexp.MustCompile(`^/details/[A-Za-z0-9_-]+$`).MatchString(source.Path) {
		return fmt.Errorf("invalid public metadata reference")
	}
	if r.IndexPage != "https://r-roms.github.io/Sony/sony-playstation-2" || r.Container != "ZIP" || r.Region != "USA" || !strings.HasSuffix(r.Filename, ".zip") || len(r.Filename) > 300 || strings.ContainsAny(r.Filename, "/\\\r\n") || r.Bytes <= 0 || r.Bytes > 128*1024*1024*1024 {
		return fmt.Errorf("invalid archive metadata")
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(r.SHA1) || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(r.MD5) {
		return fmt.Errorf("invalid publisher checksum")
	}
	if _, err := time.Parse(time.RFC3339, r.ObservedAt); err != nil {
		return fmt.Errorf("invalid observation date")
	}
	return nil
}

func PublishResearch(ctx context.Context, db *sql.DB, subject string, observations []FileResearch) error {
	if strings.TrimSpace(subject) == "" || len(observations) == 0 || len(observations) > 500 {
		return fmt.Errorf("research requires an administrator subject and 1–500 metadata observations")
	}
	grouped := map[string][]FileResearch{}
	for _, r := range observations {
		if err := r.Validate(); err != nil {
			return err
		}
		grouped[r.GameKey] = append(grouped[r.GameKey], r)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(72643002)`); err != nil {
		return err
	}
	for key, records := range grouped {
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT sr.status FROM catalog.pilot_games p JOIN catalog.suite_games sg ON sg.edition_id=p.edition_id JOIN catalog.suite_releases sr ON sr.id=sg.release_id WHERE p.game_key=$1 FOR UPDATE OF sr`, key).Scan(&status); err != nil {
			return err
		}
		if status != "draft" {
			return fmt.Errorf("refusing to mutate published research")
		}
		// Replace this bounded observation set; preserve all unrelated review fields.
		_, err = tx.ExecContext(ctx, `UPDATE catalog.pilot_games SET metadata=metadata || jsonb_build_object('externalFileResearch',$2::jsonb,'externalFileResearchStatus','publisher_metadata_unverified','externalChecksumScope','ZIP archive; not extracted ROM; not locally verified') WHERE game_key=$1`, key, jsonText(records))
		if err != nil {
			return err
		}
		for _, r := range records {
			_, err = tx.ExecContext(ctx, `INSERT INTO catalog.source_research(url,access_status,download_authorization,notes) VALUES($1,'reviewed_index','unverified','Public file metadata inspected via archive.org metadata API; no game payload acquired or approved.') ON CONFLICT(url) DO NOTHING`, r.SourcePage)
			if err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO management.audit_events(actor_subject,action,resource_id,outcome) VALUES($1,'pilot.research.import','public-file-metadata',$2)`, subject, fmt.Sprintf("%d observations; no artifact/download authorization changes", len(observations)))
	if err != nil {
		return err
	}
	return tx.Commit()
}
