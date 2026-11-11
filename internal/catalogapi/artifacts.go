package catalogapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var ErrConflict = errors.New("artifact already exists")
var ErrNotFound = errors.New("record not found")
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var formatName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type ArtifactInput struct {
	Kind               string `json:"kind"`
	Bytes              int64  `json:"bytes"`
	InstalledBytes     int64  `json:"installedBytes"`
	URL                string `json:"url"`
	SHA256             string `json:"sha256"`
	Format             string `json:"format"`
	PermissionStatus   string `json:"permissionStatus"`
	LicenseEvidenceURL string `json:"licenseEvidenceUrl,omitempty"`
	EditionID          string `json:"editionId,omitempty"`
	Filename           string `json:"filename,omitempty"`
}
type Artifact struct {
	ID string `json:"id"`
	ArtifactInput
}

func validHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 4096 && u.Scheme == "https" && u.Hostname() != "" && strings.Contains(u.Hostname(), ".") && net.ParseIP(u.Hostname()) == nil && u.Port() == "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(raw, "\r\n\x00")
}
func (a *ArtifactInput) Validate() error {
	switch a.Kind {
	case "game", "save_data", "firmware", "emulator", "utility", "artwork":
	default:
		return fmt.Errorf("kind must be game, save_data, firmware, emulator, utility or artwork")
	}
	if a.Bytes <= 0 || a.InstalledBytes < 0 {
		return fmt.Errorf("bytes must be positive and installedBytes nonnegative")
	}
	if !validHTTPS(a.URL) {
		return fmt.Errorf("url must use HTTPS and a DNS host, without credentials, a custom port or fragment")
	}
	if !digest.MatchString(a.SHA256) || !formatName.MatchString(a.Format) {
		return fmt.Errorf("a lowercase SHA-256 and artifact format are required")
	}
	if a.PermissionStatus == "" {
		a.PermissionStatus = "unreviewed"
	}
	switch a.PermissionStatus {
	case "unreviewed", "approved", "denied":
	default:
		return fmt.Errorf("invalid permissionStatus")
	}
	if a.LicenseEvidenceURL != "" && !validHTTPS(a.LicenseEvidenceURL) {
		return fmt.Errorf("licenseEvidenceUrl must be an HTTPS URL")
	}
	if a.PermissionStatus == "approved" && a.LicenseEvidenceURL == "" {
		return fmt.Errorf("approved downloads require licenseEvidenceUrl")
	}
	if a.EditionID != "" {
		if _, err := uuid.Parse(a.EditionID); err != nil {
			return fmt.Errorf("editionId must be a UUID")
		}
		if a.Filename == "" {
			return fmt.Errorf("filename is required when linking a game edition")
		}
	}
	if a.Filename != "" && (len(a.Filename) > 255 || strings.ContainsAny(a.Filename, "/\\:\r\n\x00<>\"|?*") || a.Filename == "." || a.Filename == ".." || strings.HasSuffix(a.Filename, ".") || strings.HasSuffix(a.Filename, " ")) {
		return fmt.Errorf("filename must be a plain filename")
	}
	return nil
}

type Store struct{ DB *sql.DB }

func (s Store) CreateArtifact(ctx context.Context, input ArtifactInput, subject string) (Artifact, error) {
	if err := input.Validate(); err != nil {
		return Artifact{}, err
	}
	if strings.TrimSpace(subject) == "" {
		return Artifact{}, fmt.Errorf("administrator subject is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Artifact{}, err
	}
	defer tx.Rollback()
	a := Artifact{ID: uuid.NewString(), ArtifactInput: input}
	// Never silently change the contents, size, or download permission of an
	// existing digest. Duplicate requests return 409 and leave all rows intact.
	err = tx.QueryRowContext(ctx, `INSERT INTO catalog.artifacts(id,kind,sha256,download_bytes,installed_bytes,format,permission_status,license_evidence_url,created_by_subject) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9) ON CONFLICT(sha256,kind) DO NOTHING RETURNING id`, a.ID, a.Kind, a.SHA256, a.Bytes, a.InstalledBytes, a.Format, a.PermissionStatus, a.LicenseEvidenceURL, subject).Scan(&a.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrConflict
	}
	if err != nil {
		return Artifact{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.artifact_sources(id,artifact_id,mode,source_url) VALUES($1,$2,'authorized_url',$3)`, uuid.NewString(), a.ID, a.URL); err != nil {
		return Artifact{}, err
	}
	if a.EditionID != "" {
		// Lock the edition for the association; report a missing record before
		// the transaction can commit a dangling artifact or source.
		var id string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM catalog.game_editions WHERE id=$1 FOR KEY SHARE`, a.EditionID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return Artifact{}, ErrNotFound
		}
		if err != nil {
			return Artifact{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO catalog.game_edition_artifacts(edition_id,artifact_id,role,filename) VALUES($1,$2,$3,$4)`, a.EditionID, a.ID, a.Kind, a.Filename); err != nil {
			return Artifact{}, err
		}
	}
	return a, tx.Commit()
}

func (s Store) GetArtifact(ctx context.Context, id string) (Artifact, error) {
	var a Artifact
	err := s.DB.QueryRowContext(ctx, `SELECT a.id,a.kind,a.download_bytes,a.installed_bytes,s.source_url,a.sha256,a.format,a.permission_status,COALESCE(a.license_evidence_url,'') FROM catalog.artifacts a JOIN catalog.artifact_sources s ON s.artifact_id=a.id WHERE a.id=$1 AND s.mode='authorized_url' ORDER BY s.id LIMIT 1`, id).Scan(&a.ID, &a.Kind, &a.Bytes, &a.InstalledBytes, &a.URL, &a.SHA256, &a.Format, &a.PermissionStatus, &a.LicenseEvidenceURL)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}
