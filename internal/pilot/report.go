package pilot

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Report describes catalog availability. Operation journals remain on the cabinet.
func Report(ctx context.Context, db *sql.DB) (map[string]any, error) {
	result := map[string]any{}
	queries := map[string]string{
		"genres":     `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT s.genre_id,count(DISTINCT e.id) AS candidates,count(DISTINCT a.edition_id) AS source_bindings FROM catalog.suites s JOIN catalog.suite_games sg ON sg.release_id=s.selected_release_id JOIN catalog.game_editions e ON e.id=sg.edition_id LEFT JOIN catalog.game_edition_artifacts a ON a.edition_id=e.id GROUP BY s.genre_id ORDER BY s.genre_id) v`,
		"artifacts":  `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT kind,count(*) AS count,sum(download_bytes) AS bytes FROM catalog.artifacts GROUP BY kind ORDER BY kind) v`,
		"emulators":  `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT * FROM catalog.latest_emulator_releases ORDER BY slug) v`,
		"onboarding": `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT * FROM catalog.onboarding_steps ORDER BY emulator_slug,step_key) v`,
	}
	for name, query := range queries {
		var raw []byte
		if err := db.QueryRowContext(ctx, query).Scan(&raw); err != nil {
			return nil, err
		}
		result[name] = json.RawMessage(raw)
	}
	return result, nil
}
