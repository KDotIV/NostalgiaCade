package pilot

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Report returns bounded, non-secret remote observations for operator diagnosis.
func Report(ctx context.Context, db *sql.DB) (map[string]any, error) {
	result := map[string]any{}
	queries := map[string]string{
		"fileResearch":   `SELECT jsonb_build_object('games',count(*),'observations',COALESCE(sum(jsonb_array_length(metadata->'externalFileResearch')),0)) FROM catalog.pilot_games WHERE metadata ? 'externalFileResearch'`,
		"events":         `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT run_id,step_sequence,state,phase,observed_at FROM management.installation_events ORDER BY id DESC LIMIT 200) v`,
		"genres":         `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT g.genre_id,count(DISTINCT p.game_key) AS candidates,count(DISTINCT s.game_key) AS source_bindings FROM catalog.pilot_genres g JOIN catalog.suite_games sg ON sg.release_id=g.release_id JOIN catalog.pilot_games p ON p.edition_id=sg.edition_id LEFT JOIN catalog.pilot_sources s ON s.game_key=p.game_key GROUP BY g.genre_id ORDER BY g.genre_id) v`,
		"runs":           `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT r.id,c.name AS cabinet,r.state,r.selection,r.error_code,r.created_at,r.updated_at FROM management.installation_runs r JOIN management.cabinets c ON c.id=r.cabinet_id ORDER BY r.created_at DESC LIMIT 50) v`,
		"inventory":      `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT c.name AS cabinet,i.kind,i.item_key,i.detail FROM management.cabinet_installations i JOIN management.cabinets c ON c.id=i.cabinet_id ORDER BY c.name,i.kind,i.item_key LIMIT 500) v`,
		"sourceResearch": `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT * FROM catalog.source_research ORDER BY url) v`,
		"steps":          `SELECT COALESCE(jsonb_agg(v),'[]') FROM (SELECT run_id,sequence,kind,state,phase,detail FROM management.installation_steps ORDER BY updated_at DESC LIMIT 200) v`,
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
