package emucatalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func id(key string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("nostalgiacade/emulator/"+key)).String()
}

// Publish is an administrative operation. The caller authenticates through the
// database role; subject records attribution and does not grant privileges.
func Publish(ctx context.Context, db *sql.DB, c Catalog, subject string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if subject == "" {
		return fmt.Errorf("publisher subject is required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range c.Packages {
		emulatorID, artifactID := id(p.Slug), id("artifact/"+p.SHA256)
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.emulators(id,slug,name,official_url) VALUES($1,$2,$3,$4) ON CONFLICT(slug) DO UPDATE SET name=excluded.name,official_url=excluded.official_url`, emulatorID, p.Slug, p.Name, p.OfficialURL)
		if err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT id FROM catalog.emulators WHERE slug=$1`, p.Slug).Scan(&emulatorID); err != nil {
			return err
		}
		// Installed size is measured during extraction. Catalog value zero means
		// not measured yet; it is never used as the extraction or disk-space limit.
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.artifacts(id,kind,sha256,download_bytes,installed_bytes,format,permission_status,license_evidence_url) VALUES($1,'emulator',$2,$3,0,$4,'unreviewed',$5) ON CONFLICT(sha256,kind) DO NOTHING`, artifactID, p.SHA256, p.Bytes, p.Format, p.LicenseURL)
		if err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT id FROM catalog.artifacts WHERE sha256=$1 AND kind='emulator'`, p.SHA256).Scan(&artifactID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.artifact_sources(id,artifact_id,mode,source_url) VALUES($1,$2,'authorized_url',$3) ON CONFLICT(id) DO NOTHING`, id("source/"+p.SHA256+"/"+p.URL), artifactID, p.URL)
		if err != nil {
			return err
		}
		releaseID := id("release/" + p.Slug + "/" + p.Version)
		requirements, _ := json.Marshal(p.Requirements)
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.emulator_releases(id,emulator_id,artifact_id,upstream_version,os,architecture,adapter_key,requirements) VALUES($1,$2,$3,$4,'windows','x64',$5,$6) ON CONFLICT(emulator_id,upstream_version,os,architecture) DO NOTHING`, releaseID, emulatorID, artifactID, p.Version, p.Slug, string(requirements))
		if err != nil {
			return err
		}
		var existingArtifact string
		if err = tx.QueryRowContext(ctx, `SELECT id,artifact_id FROM catalog.emulator_releases WHERE emulator_id=$1 AND upstream_version=$2 AND os='windows' AND architecture='x64'`, emulatorID, p.Version).Scan(&releaseID, &existingArtifact); err != nil {
			return err
		}
		if existingArtifact != artifactID {
			return fmt.Errorf("release %s %s already has different content; publish a new version", p.Slug, p.Version)
		}
		body, _ := json.Marshal(p)
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog.emulator_packages(release_id,manifest,verified_at,published_by_subject) VALUES($1,$2,$3,$4) ON CONFLICT(release_id) DO UPDATE SET manifest=excluded.manifest,verified_at=excluded.verified_at,published_by_subject=excluded.published_by_subject`, releaseID, string(body), p.VerifiedAt, subject)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func Export(ctx context.Context, db *sql.DB) (Catalog, error) {
	c := Catalog{Version: 1, Packages: []Package{}}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT ON(e.slug) p.manifest FROM catalog.emulator_packages p JOIN catalog.emulator_releases r ON r.id=p.release_id JOIN catalog.emulators e ON e.id=r.emulator_id ORDER BY e.slug,p.verified_at DESC,r.upstream_version DESC`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return c, err
		}
		var p Package
		if err = json.Unmarshal(body, &p); err != nil {
			return c, err
		}
		c.Packages = append(c.Packages, p)
	}
	return c, rows.Err()
}
