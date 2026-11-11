package catalogapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KdotIV/NostalgiaCade/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const testToken = "test-only-admin-token-12345678901234567890"

type fakeStore struct {
	calls   int
	subject string
	err     error
}

func (s *fakeStore) CreateArtifact(_ context.Context, a ArtifactInput, subject string) (Artifact, error) {
	s.calls++
	s.subject = subject
	return Artifact{ID: "00000000-0000-0000-0000-000000000001", ArtifactInput: a}, s.err
}
func (s *fakeStore) GetArtifact(context.Context, string) (Artifact, error) { return Artifact{}, s.err }
func validInput() ArtifactInput {
	return ArtifactInput{Kind: "save_data", Bytes: 128, URL: "https://downloads.example.org/save.bin", SHA256: strings.Repeat("a", 64), Format: "bin"}
}
func TestArtifactAuthenticationAndValidation(t *testing.T) {
	good, _ := json.Marshal(validInput())
	for _, tc := range []struct {
		name, token, body string
		status            int
	}{
		{"missing credential", "", string(good), 401},
		{"claimed role is not a credential", "Venue-Operator", string(good), 401},
		{"invalid credential", strings.Repeat("x", 40), string(good), 401},
		{"unknown role field", testToken, strings.TrimSuffix(string(good), "}") + `,"role":"Sengoku-Admin"}`, 400},
		{"trailing object", testToken, string(good) + `{}`, 400},
		{"oversized", testToken, `{"url":"` + strings.Repeat("x", 70*1024) + `"}`, 413},
		{"missing required fields", testToken, `{"kind":"game"}`, 400},
		{"valid", testToken, string(good), 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			h, err := newHandler(nil, store, Config{AdminToken: testToken, AdminSubject: "trusted-server-subject"})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/v1/admin/artifacts", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Role", "Sengoku-Admin")
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if tc.status != 201 && store.calls != 0 {
				t.Fatal("rejected request reached database")
			}
			if tc.status == 201 && (store.subject != "trusted-server-subject" || w.Header().Get("Location") == "") {
				t.Fatal("missing server subject or resource location")
			}
		})
	}
	if _, err := NewHandler(nil, Config{}); err == nil {
		t.Fatal("empty authentication accepted")
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{ErrConflict, 409}, {ErrNotFound, 404}, {fmt.Errorf("password=secret database detail"), 500}} {
		store := &fakeStore{err: tc.err}
		h, _ := newHandler(nil, store, Config{testToken, "admin"})
		r := httptest.NewRequest("POST", "/v1/admin/artifacts", strings.NewReader(string(good)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestArtifactInput(t *testing.T) {
	for _, change := range []func(*ArtifactInput){
		func(a *ArtifactInput) { a.Kind = "other" }, func(a *ArtifactInput) { a.Bytes = -1 }, func(a *ArtifactInput) { a.Bytes = 0 },
		func(a *ArtifactInput) { a.InstalledBytes = -1 }, func(a *ArtifactInput) { a.URL = "http://example.org/a" },
		func(a *ArtifactInput) { a.URL = "https://user:password@example.org/a" }, func(a *ArtifactInput) { a.URL = "https://127.0.0.1/a" },
		func(a *ArtifactInput) { a.SHA256 = "bad" }, func(a *ArtifactInput) { a.PermissionStatus = "approved" },
		func(a *ArtifactInput) { a.EditionID = "bad" }, func(a *ArtifactInput) { a.Filename = "../save.bin" },
	} {
		a := validInput()
		change(&a)
		if a.Validate() == nil {
			t.Fatalf("accepted invalid input: %+v", a)
		}
	}
}
func TestPostgresArtifacts(t *testing.T) {
	dsn := os.Getenv("NCADE_TEST_POSTGRES_DSN")
	if dsn == "" || os.Getenv("NCADE_TEST_CATALOG") != "1" {
		t.Skip("isolated PostgreSQL required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(config.Database, "ncade_test_") || (config.Host != "127.0.0.1" && config.Host != "localhost") {
		t.Fatal("disposable loopback database required")
	}
	ctx := context.Background()
	db, err := database.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := Store{db}
	input := validInput()
	input.SHA256 = strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
	var before, after int
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifacts`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	missing := input
	missing.EditionID = uuid.NewString()
	missing.Filename = "save.bin"
	if _, err = store.CreateArtifact(ctx, missing, "admin"); err != ErrNotFound {
		t.Fatal("missing edition", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifacts`).Scan(&after); err != nil || after != before {
		t.Fatal("failed link leaked artifact", err)
	}
	a, err := store.CreateArtifact(ctx, input, "admin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetArtifact(ctx, a.ID)
	if err != nil || got.Kind != "save_data" || got.Bytes != 128 || got.URL != input.URL {
		t.Fatal(got, err)
	}
	if _, err = store.CreateArtifact(ctx, input, "admin"); err != ErrConflict {
		t.Fatal("duplicate", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM catalog.artifact_sources WHERE artifact_id=$1`, a.ID).Scan(&after); err != nil || after != 1 {
		t.Fatal("duplicate source", err)
	}
	h, _ := NewHandler(db, Config{testToken, "admin"})
	for _, path := range []string{"/v1/catalog/emulators", "/v1/catalog/genres", "/v1/catalog/games?genre=platform", "/v1/catalog/emulators/pcsx2/onboarding", "/v1/catalog/artifacts/" + a.ID} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
