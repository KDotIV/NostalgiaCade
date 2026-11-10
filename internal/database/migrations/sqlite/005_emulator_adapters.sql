-- Rebuild both sides of the foreign key without disabling foreign_keys.
CREATE TABLE emulator_profiles_next (
    id TEXT NOT NULL PRIMARY KEY,
    adapter TEXT NOT NULL CHECK (adapter IN ('dolphin','duckstation','pcsx2','rpcs3','eden')),
    executable_path TEXT NOT NULL,
    executable_sha256 TEXT NOT NULL,
    version_label TEXT NOT NULL,
    prepared_hash TEXT NOT NULL DEFAULT ''
);
INSERT INTO emulator_profiles_next(id,adapter,executable_path,executable_sha256,version_label)
    SELECT id,adapter,executable_path,executable_sha256,version_label FROM emulator_profiles;
INSERT OR IGNORE INTO emulator_profiles_next(id,adapter,executable_path,executable_sha256,version_label)
    SELECT slug,slug,executable_path,executable_sha256,version FROM emulator_packages
    WHERE slug IN ('duckstation','pcsx2','rpcs3','eden');
CREATE TABLE local_games_next (
    id TEXT NOT NULL PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    content_path TEXT NOT NULL CHECK (length(content_path) > 0),
    catalog_edition_id TEXT,
    ownership TEXT NOT NULL DEFAULT 'external' CHECK (ownership IN ('external','managed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    genre_id TEXT REFERENCES genres(id),
    emulator_id TEXT REFERENCES emulator_profiles_next(id),
    sha256 TEXT,
    size_bytes INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    artifact_id TEXT REFERENCES installed_artifacts(id),
    scope TEXT NOT NULL DEFAULT 'local' CHECK (scope IN ('local','suite'))
);
INSERT INTO local_games_next SELECT * FROM local_games;
DROP TABLE local_games;
DROP TABLE emulator_profiles;
ALTER TABLE emulator_profiles_next RENAME TO emulator_profiles;
ALTER TABLE local_games_next RENAME TO local_games;
CREATE INDEX local_games_by_artifact ON local_games(artifact_id);
CREATE INDEX local_games_by_scope ON local_games(scope,genre_id);
ALTER TABLE play_sessions ADD COLUMN purpose TEXT NOT NULL DEFAULT 'game';
ALTER TABLE play_sessions ADD COLUMN emulator_id TEXT NOT NULL DEFAULT '';
