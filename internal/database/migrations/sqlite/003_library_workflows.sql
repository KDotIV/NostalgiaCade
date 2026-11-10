CREATE TABLE emulator_profiles (
    id TEXT NOT NULL PRIMARY KEY,
    adapter TEXT NOT NULL CHECK (adapter IN ('dolphin')),
    executable_path TEXT NOT NULL,
    executable_sha256 TEXT NOT NULL,
    version_label TEXT NOT NULL
);
-- Multiple entries may reference one content-addressed file. Rebuild to remove
-- the original unique content_path constraint without changing applied SQL.
CREATE TABLE local_games_next (
    id TEXT NOT NULL PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    content_path TEXT NOT NULL CHECK (length(content_path) > 0),
    catalog_edition_id TEXT,
    ownership TEXT NOT NULL DEFAULT 'external' CHECK (ownership IN ('external','managed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    genre_id TEXT REFERENCES genres(id),
    emulator_id TEXT REFERENCES emulator_profiles(id),
    sha256 TEXT,
    size_bytes INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    artifact_id TEXT REFERENCES installed_artifacts(id),
    scope TEXT NOT NULL DEFAULT 'local' CHECK (scope IN ('local','suite'))
);
INSERT INTO local_games_next(id,title,content_path,catalog_edition_id,ownership,created_at)
    SELECT id,title,content_path,catalog_edition_id,ownership,created_at FROM local_games;
DROP TABLE local_games;
ALTER TABLE local_games_next RENAME TO local_games;
ALTER TABLE operations ADD COLUMN phase TEXT NOT NULL DEFAULT 'queued';
ALTER TABLE operations ADD COLUMN error TEXT NOT NULL DEFAULT '';
ALTER TABLE operations ADD COLUMN result_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(result_json));
ALTER TABLE active_suite ADD COLUMN genre_id TEXT REFERENCES genres(id);
ALTER TABLE active_suite ADD COLUMN last_operation_id TEXT;
CREATE TABLE play_sessions (
    id TEXT NOT NULL PRIMARY KEY,
    game_id TEXT NOT NULL,
    process_id INTEGER,
    process_identity TEXT,
    state TEXT NOT NULL CHECK (state IN ('starting','running','exited','interrupted')),
    exit_code INTEGER,
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ended_at TEXT
);
CREATE INDEX local_games_by_artifact ON local_games(artifact_id);
CREATE INDEX local_games_by_scope ON local_games(scope,genre_id);
