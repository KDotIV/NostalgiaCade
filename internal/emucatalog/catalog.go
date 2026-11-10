// Package emucatalog defines reviewed emulator distributions. Catalog data is
// separate from both local game inventory and credentials for catalog publishing.
package emucatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

//go:embed windows.json
var snapshot []byte

type Package struct {
	Slug             string         `json:"slug"`
	Name             string         `json:"name"`
	Version          string         `json:"version"`
	OS               string         `json:"os"`
	Architecture     string         `json:"architecture"`
	OfficialURL      string         `json:"officialUrl"`
	DownloadPage     string         `json:"downloadPage"`
	URL              string         `json:"url"`
	SHA256           string         `json:"sha256"`
	Bytes            int64          `json:"bytes"`
	Format           string         `json:"format"`
	Executable       string         `json:"executable"`
	AllowedHosts     []string       `json:"allowedHosts"`
	License          string         `json:"license"`
	LicenseURL       string         `json:"licenseUrl"`
	DeploymentReview string         `json:"deploymentReview"`
	VerifiedAt       string         `json:"verifiedAt"`
	Requirements     map[string]any `json:"requirements"`
}

type Catalog struct {
	Version  int       `json:"version"`
	Packages []Package `json:"packages"`
}

func Builtin() Catalog {
	var c Catalog
	if err := json.Unmarshal(snapshot, &c); err != nil {
		panic(err)
	}
	return c
}

func Read(r io.Reader) (Catalog, error) {
	var c Catalog
	d := json.NewDecoder(io.LimitReader(r, 1024*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, fmt.Errorf("unexpected catalog data")
	}
	return c, c.Validate()
}

func (c Catalog) Validate() error {
	if c.Version != 1 || len(c.Packages) < 1 || len(c.Packages) > 100 {
		return fmt.Errorf("expected a version 1 catalog with 1–100 packages")
	}
	seen := map[string]bool{}
	for _, p := range c.Packages {
		if seen[p.Slug] {
			return fmt.Errorf("duplicate emulator slug")
		}
		seen[p.Slug] = true
		if err := p.Validate(); err != nil {
			return fmt.Errorf("%s: %w", p.Slug, err)
		}
	}
	return nil
}

func (p Package) Validate() error {
	switch p.Slug {
	case "duckstation", "pcsx2", "dolphin", "rpcs3", "eden":
	default:
		return fmt.Errorf("unknown emulator")
	}
	if p.Name == "" || len(p.Name) > 100 || p.Version == "" || len(p.Version) > 100 || p.OS != "windows" || p.Architecture != "x64" {
		return fmt.Errorf("expected a named Windows x64 release")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(p.SHA256) || p.Bytes <= 0 || p.Bytes > 1024*1024*1024 {
		return fmt.Errorf("package needs SHA-256 and size of at most 1 GiB")
	}
	if p.Format != "zip" && p.Format != "7z" {
		return fmt.Errorf("only zip and 7z packages are supported")
	}
	if !SafeName(p.Executable) || !strings.HasSuffix(strings.ToLower(p.Executable), ".exe") {
		return fmt.Errorf("executable must be a safe relative .exe path")
	}
	if _, err := time.Parse(time.RFC3339, p.VerifiedAt); err != nil {
		return fmt.Errorf("verifiedAt must be RFC3339")
	}
	for _, raw := range []string{p.URL, p.OfficialURL, p.DownloadPage, p.LicenseURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Port() != "" || u.Fragment != "" {
			return fmt.Errorf("catalog URLs must be HTTPS without credentials or fragments")
		}
	}
	if p.License == "" || p.DeploymentReview == "" {
		return fmt.Errorf("license and deployment review are required")
	}
	knownHosts := map[string]bool{"github.com": true, "release-assets.githubusercontent.com": true, "dl.dolphin-emu.org": true, "stable.eden-emu.dev": true}
	if len(p.AllowedHosts) == 0 || len(p.AllowedHosts) > 4 {
		return fmt.Errorf("expected reviewed vendor download hosts")
	}
	for _, h := range p.AllowedHosts {
		if !knownHosts[h] {
			return fmt.Errorf("unreviewed emulator download host")
		}
	}
	u, _ := url.Parse(p.URL)
	if !knownHosts[u.Hostname()] {
		return fmt.Errorf("unreviewed emulator source")
	}
	if p.Requirements == nil {
		return fmt.Errorf("requirements must be an object")
	}
	return nil
}

// SafeName rejects Windows device aliases, alternate streams, traversal, and
// normalization collisions before archive paths reach the filesystem.
func SafeName(name string) bool {
	if name == "" || len(name) > 240 || strings.ContainsAny(name, "\\:\x00\r\n<>\"|?*") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || regexp.MustCompile(`^(COM|LPT)[1-9¹²³]$`).MatchString(base) {
			return false
		}
	}
	return true
}
