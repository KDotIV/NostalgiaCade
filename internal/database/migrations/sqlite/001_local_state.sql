CREATE TABLE genres (
    id TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_order INTEGER NOT NULL UNIQUE,
    target_entries INTEGER NOT NULL DEFAULT 20 CHECK (target_entries > 0)
);

CREATE TABLE settings (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL CHECK (json_valid(value)),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE local_games (
    id TEXT NOT NULL PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    content_path TEXT NOT NULL UNIQUE CHECK (length(content_path) > 0),
    catalog_edition_id TEXT,
    ownership TEXT NOT NULL DEFAULT 'external' CHECK (ownership IN ('external', 'managed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE installed_artifacts (
    id TEXT NOT NULL PRIMARY KEY,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    kind TEXT NOT NULL CHECK (kind IN ('game', 'emulator', 'firmware', 'utility', 'cache')),
    relative_path TEXT NOT NULL UNIQUE,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    installed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE operations (
    id TEXT NOT NULL PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'running', 'awaiting_operator', 'failed', 'canceled', 'completed')),
    request_json TEXT NOT NULL CHECK (json_valid(request_json)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE operation_steps (
    operation_id TEXT NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    state TEXT NOT NULL,
    detail_json TEXT NOT NULL CHECK (json_valid(detail_json)),
    PRIMARY KEY (operation_id, sequence)
);

CREATE TABLE active_suite (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    release_id TEXT,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    readiness TEXT NOT NULL DEFAULT 'empty' CHECK (readiness IN ('empty', 'ready', 'maintenance'))
);
INSERT INTO active_suite(singleton) VALUES (1);
