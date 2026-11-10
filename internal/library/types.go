package library

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
)

type Paths struct {
	Emulators string `json:"emulators"`
	ROMs      string `json:"roms"`
	Firmware  string `json:"firmware"`
	Saves     string `json:"saves"`
	Settings  string `json:"settings"`
	Cache     string `json:"cache"`
}
type Setup struct {
	Configured    bool     `json:"configured"`
	Root          string   `json:"root,omitempty"`
	Paths         Paths    `json:"paths"`
	DownloadHosts []string `json:"downloadHosts"`
}
type Emulator struct {
	ID         string `json:"id"`
	Adapter    string `json:"adapter"`
	Executable string `json:"executable"`
	Version    string `json:"version"`
}
type Source struct {
	Kind             string `json:"kind"`
	Path             string `json:"path,omitempty"`
	URL              string `json:"url,omitempty"`
	Authorized       bool   `json:"authorized,omitempty"`
	AuthorizationURL string `json:"authorizationUrl,omitempty"`
}
type Entry struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	EmulatorID string `json:"emulatorId"`
	Filename   string `json:"filename"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Source     Source `json:"source"`
}
type Manifest struct {
	Version int     `json:"version"`
	ID      string  `json:"id"`
	GenreID string  `json:"genreId"`
	Games   []Entry `json:"games"`
}
type ImportRequest struct {
	Entry     Entry  `json:"entry"`
	GenreID   string `json:"genreId"`
	Reference bool   `json:"reference"`
}
type SwitchRequest struct {
	Manifest         Manifest `json:"manifest"`
	ExpectedRevision int64    `json:"expectedRevision"`
}
type Request struct {
	Key     string           `json:"key"`
	Kind    string           `json:"kind"`
	Import  *ImportRequest   `json:"import,omitempty"`
	Switch  *SwitchRequest   `json:"switch,omitempty"`
	Install *EmulatorInstall `json:"install,omitempty"`
}
type Game struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	GenreID    string `json:"genreId"`
	EmulatorID string `json:"emulatorId"`
	Ownership  string `json:"ownership"`
	Scope      string `json:"scope"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	ArtifactID string `json:"artifactId"`
}
type Operation struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Phase  string `json:"phase"`
	Error  string `json:"error"`
	Result string `json:"result"`
}
type Plan struct {
	Revision        int64    `json:"revision"`
	RemoveGames     []string `json:"removeGames"`
	ReclaimBytes    int64    `json:"reclaimBytes"`
	AcquireBytes    int64    `json:"acquireBytes"`
	RetainArtifacts []string `json:"retainArtifacts"`
}
type Session struct {
	ID       string `json:"id"`
	GameID   string `json:"gameId"`
	PID      int    `json:"pid"`
	State    string `json:"state"`
	ExitCode *int   `json:"exitCode,omitempty"`
}
type Fault struct {
	Code    string
	Message string
}

func (e *Fault) Error() string                    { return e.Code + ": " + e.Message }
func fail(code, format string, args ...any) error { return &Fault{code, fmt.Sprintf(format, args...)} }
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
