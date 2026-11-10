package pilot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Binding struct {
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	Format    string `json:"format"`
	Reference bool   `json:"reference"`
}
type Bindings struct {
	Version int                `json:"version"`
	Games   map[string]Binding `json:"games"`
	Notes   []string           `json:"notes,omitempty"`
}

var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (b Bindings) Validate() error {
	if b.Version != 1 || b.Games == nil {
		return fmt.Errorf("expected version 1 source bindings")
	}
	known := map[string]Candidate{}
	for _, g := range Candidates() {
		known[g.Key] = g
	}
	for key, source := range b.Games {
		g, ok := known[key]
		if !ok || !filepath.IsAbs(source.Path) || source.Filename != filepath.Base(source.Path) || !digest.MatchString(source.SHA256) || source.Bytes <= 0 {
			return fmt.Errorf("invalid binding for %s", key)
		}
		if !supportedSource(g, source.Path) || source.Reference != (g.Platform == "ps3") || source.Format != strings.TrimPrefix(strings.ToLower(filepath.Ext(source.Path)), ".") {
			return fmt.Errorf("unsupported source binding for %s", key)
		}
	}
	return nil
}
func ReadBindings(path string) (Bindings, error) {
	if path == "" {
		return Bindings{Version: 1, Games: map[string]Binding{}}, nil
	}
	var b Bindings
	err := ReadJSON(path, &b)
	if err == nil {
		err = b.Validate()
	}
	return b, err
}
func ReadJSON(path string, result any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 4*1024*1024))
	d.DisallowUnknownFields()
	if err = d.Decode(result); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("unexpected JSON data")
	}
	return nil
}

var annotations = regexp.MustCompile(`\([^)]*\)|\[[^]]*\]`)
var nonWord = regexp.MustCompile(`[^a-z0-9]`)

func normalized(s string) string {
	return nonWord.ReplaceAllString(strings.ToLower(annotations.ReplaceAllString(s, "")), "")
}
func supportedSource(g Candidate, path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch g.Platform {
	case "ps1":
		return ext == ".chd" || ext == ".pbp"
	case "ps2":
		return ext == ".iso" || ext == ".chd" || ext == ".cso"
	case "gamecube", "wii":
		return ext == ".iso" || ext == ".gcm" || ext == ".rvz" || ext == ".wbfs" || ext == ".gcz"
	case "ps3":
		return strings.EqualFold(filepath.Base(path), "EBOOT.BIN") && strings.EqualFold(filepath.Base(filepath.Dir(path)), "USRDIR")
	}
	return false
}
func Scan(ctx context.Context, roots []string) (Bindings, error) {
	result := Bindings{Version: 1, Games: map[string]Binding{}, Notes: []string{}}
	matches := map[string][]string{}
	games := Candidates()
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return result, err
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			return result, fmt.Errorf("source root is not a readable directory")
		}
		err = filepath.WalkDir(absolute, func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(path)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			title := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			if strings.EqualFold(d.Name(), "EBOOT.BIN") {
				title = filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
			}
			for _, g := range games {
				if normalized(title) == normalized(g.Title) {
					if supportedSource(g, path) {
						matches[g.Key] = append(matches[g.Key], path)
					} else {
						result.Notes = append(result.Notes, g.Key+": matching file needs a supported image format (archives and CUE/BIN sets are not auto-converted)")
					}
				}
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	for _, g := range games {
		paths := matches[g.Key]
		if len(paths) > 1 {
			return result, fmt.Errorf("multiple matching images for %s; supply an explicit bindings file", g.Key)
		}
		if len(paths) == 0 {
			continue
		}
		path := paths[0]
		f, err := os.Open(path)
		if err != nil {
			return result, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return result, err
		}
		if g.Platform == "ps3" {
			sfo := filepath.Join(filepath.Dir(filepath.Dir(path)), "PARAM.SFO")
			if info, err := os.Stat(sfo); err != nil || !info.Mode().IsRegular() {
				f.Close()
				return result, fmt.Errorf("%s requires its complete PS3_GAME folder", g.Key)
			}
		}
		hash := sha256.New()
		n, err := io.Copy(hash, &contextReader{ctx, f})
		f.Close()
		if err != nil {
			return result, err
		}
		after, err := os.Stat(path)
		if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || n != info.Size() {
			return result, fmt.Errorf("source changed while hashing %s", g.Key)
		}
		result.Games[g.Key] = Binding{Path: path, Filename: filepath.Base(path), SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Bytes: n, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), Reference: g.Platform == "ps3"}
	}
	return result, result.Validate()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
