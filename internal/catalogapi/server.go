// Package catalogapi provides the trusted Sengoku-Admin catalog service.
package catalogapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/emucatalog"
	"github.com/google/uuid"
)

type artifactStore interface {
	CreateArtifact(context.Context, ArtifactInput, string) (Artifact, error)
	GetArtifact(context.Context, string) (Artifact, error)
}
type Config struct{ AdminToken, AdminSubject string }

func (c Config) Validate() error {
	if len(c.AdminToken) < 32 || strings.TrimSpace(c.AdminToken) != c.AdminToken || strings.ContainsAny(c.AdminToken, " \t\r\n") || strings.TrimSpace(c.AdminSubject) == "" {
		return fmt.Errorf("NCADE_ADMIN_TOKEN (at least 32 characters) and NCADE_ADMIN_SUBJECT are required")
	}
	return nil
}

// NewHandler maps a server-configured bearer credential to Sengoku-Admin.
// No body, query, role header, or client-supplied subject can grant this role.
func NewHandler(db *sql.DB, config Config) (http.Handler, error) {
	return newHandler(db, Store{DB: db}, config)
}
func newHandler(db *sql.DB, store artifactStore, config Config) (http.Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/admin/artifacts", func(w http.ResponseWriter, r *http.Request) {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			problem(w, 415, "Content-Type must be application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var input ArtifactInput
		if err = decoder.Decode(&input); err != nil {
			badJSON(w, err)
			return
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			badJSON(w, err)
			return
		}
		if err = input.Validate(); err != nil {
			problem(w, 400, err.Error())
			return
		}
		artifact, err := store.CreateArtifact(r.Context(), input, config.AdminSubject)
		if err != nil {
			storeError(w, err)
			return
		}
		w.Header().Set("Location", "/v1/catalog/artifacts/"+artifact.ID)
		respond(w, 201, artifact)
	})
	mux.HandleFunc("GET /v1/catalog/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			problem(w, 400, "invalid artifact id")
			return
		}
		a, err := store.GetArtifact(r.Context(), id.String())
		if err != nil {
			storeError(w, err)
			return
		}
		respond(w, 200, a)
	})
	mux.HandleFunc("GET /v1/catalog/emulators", func(w http.ResponseWriter, r *http.Request) {
		c, err := emucatalog.Export(r.Context(), db)
		if err != nil {
			storeError(w, err)
			return
		}
		respond(w, 200, c)
	})
	mux.HandleFunc("GET /v1/catalog/genres", jsonQuery(db, `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT id,name,display_order AS "displayOrder",target_entries AS "targetEntries" FROM catalog.genres ORDER BY display_order) v`))
	mux.HandleFunc("GET /v1/catalog/games", func(w http.ResponseWriter, r *http.Request) {
		// Bound output and expose the IDs needed to attach an uploaded artifact.
		rawQuery(w, r, db, `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT g.id,g.title,e.id AS "editionId",e.game_key AS "gameKey",e.emulator_slug AS "emulatorSlug",p.slug AS platform,ARRAY(SELECT genre_id FROM catalog.game_genres WHERE game_id=g.id ORDER BY genre_id) AS genres FROM catalog.games g JOIN catalog.game_editions e ON e.game_id=g.id JOIN catalog.platforms p ON p.id=e.platform_id WHERE ($1='' OR EXISTS(SELECT 1 FROM catalog.game_genres gg WHERE gg.game_id=g.id AND gg.genre_id=$1)) ORDER BY g.title,e.id LIMIT 1000) v`, r.URL.Query().Get("genre"))
	})
	mux.HandleFunc("GET /v1/catalog/emulators/{slug}/onboarding", func(w http.ResponseWriter, r *http.Request) {
		rawQuery(w, r, db, `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT step_key AS "stepKey",instructions FROM catalog.onboarding_steps WHERE emulator_slug=$1 ORDER BY step_key) v`, r.PathValue("slug"))
	})
	expected := sha256.Sum256([]byte(config.AdminToken))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		actual := sha256.Sum256([]byte(token))
		if !ok || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Sengoku-Admin"`)
			problem(w, 401, "Sengoku-Admin authentication required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	}), nil
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func badJSON(w http.ResponseWriter, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		problem(w, 413, "request body is too large")
		return
	}
	problem(w, 400, "expected one valid JSON object with known fields")
}
func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrConflict):
		problem(w, 409, "artifact with this kind and SHA-256 already exists")
	case errors.Is(err, ErrNotFound):
		problem(w, 404, "artifact or game edition not found")
	default:
		problem(w, 500, "catalog operation failed")
	}
}
func jsonQuery(db *sql.DB, query string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { rawQuery(w, r, db, query) }
}
func rawQuery(w http.ResponseWriter, r *http.Request, db *sql.DB, query string, args ...any) {
	var raw []byte
	if err := db.QueryRowContext(r.Context(), query, args...).Scan(&raw); err != nil {
		storeError(w, err)
		return
	}
	respond(w, 200, json.RawMessage(raw))
}
