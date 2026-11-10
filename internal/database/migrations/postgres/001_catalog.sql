CREATE SCHEMA catalog;
REVOKE ALL ON SCHEMA catalog FROM PUBLIC;

CREATE TABLE catalog.genres (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_order INTEGER NOT NULL UNIQUE,
    target_entries INTEGER NOT NULL DEFAULT 20 CHECK (target_entries > 0)
);
CREATE TABLE catalog.platforms (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('arcade', 'console'))
);
CREATE TABLE catalog.games (
    id UUID PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    release_year INTEGER NOT NULL CHECK (release_year BETWEEN 1950 AND 2200),
    players_min INTEGER NOT NULL DEFAULT 1 CHECK (players_min > 0),
    players_max INTEGER NOT NULL DEFAULT 1 CHECK (players_max >= players_min),
    created_by_subject TEXT NOT NULL CHECK (length(created_by_subject) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE catalog.game_genres (
    game_id UUID NOT NULL REFERENCES catalog.games(id),
    genre_id TEXT NOT NULL REFERENCES catalog.genres(id),
    PRIMARY KEY (game_id, genre_id)
);
CREATE INDEX game_genres_by_genre ON catalog.game_genres(genre_id, game_id);
CREATE TABLE catalog.game_editions (
    id UUID PRIMARY KEY,
    game_id UUID NOT NULL REFERENCES catalog.games(id),
    platform_id UUID NOT NULL REFERENCES catalog.platforms(id),
    region TEXT NOT NULL,
    revision TEXT NOT NULL,
    platform_title_id TEXT,
    UNIQUE (game_id, platform_id, region, revision)
);
CREATE TABLE catalog.artifacts (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('game', 'emulator', 'firmware', 'utility', 'artwork')),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    download_bytes BIGINT NOT NULL CHECK (download_bytes >= 0),
    installed_bytes BIGINT NOT NULL CHECK (installed_bytes >= 0),
    format TEXT NOT NULL,
    permission_status TEXT NOT NULL DEFAULT 'unreviewed' CHECK (permission_status IN ('unreviewed', 'approved', 'denied')),
    license_evidence_url TEXT,
    UNIQUE (sha256, kind)
);
CREATE TABLE catalog.artifact_sources (
    id UUID PRIMARY KEY,
    artifact_id UUID NOT NULL REFERENCES catalog.artifacts(id),
    mode TEXT NOT NULL CHECK (mode IN ('object_store', 'authorized_url', 'user_import', 'manual_vendor_step')),
    source_url TEXT,
    object_key TEXT,
    CHECK (
        (mode = 'authorized_url' AND source_url IS NOT NULL AND source_url LIKE 'https://_%' AND object_key IS NULL) OR
        (mode = 'object_store' AND object_key IS NOT NULL AND length(trim(object_key)) > 0 AND source_url IS NULL) OR
        (mode IN ('user_import', 'manual_vendor_step') AND source_url IS NULL AND object_key IS NULL)
    )
);
CREATE TABLE catalog.artifact_dependencies (
    artifact_id UUID NOT NULL REFERENCES catalog.artifacts(id),
    dependency_id UUID NOT NULL REFERENCES catalog.artifacts(id),
    CHECK (artifact_id <> dependency_id),
    PRIMARY KEY (artifact_id, dependency_id)
);
CREATE TABLE catalog.game_edition_artifacts (
    edition_id UUID NOT NULL REFERENCES catalog.game_editions(id),
    artifact_id UUID NOT NULL REFERENCES catalog.artifacts(id),
    role TEXT NOT NULL,
    PRIMARY KEY (edition_id, artifact_id)
);
CREATE TABLE catalog.emulators (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    official_url TEXT NOT NULL CHECK (official_url LIKE 'https://%')
);
CREATE TABLE catalog.emulator_releases (
    id UUID PRIMARY KEY,
    emulator_id UUID NOT NULL REFERENCES catalog.emulators(id),
    artifact_id UUID NOT NULL REFERENCES catalog.artifacts(id),
    upstream_version TEXT NOT NULL,
    os TEXT NOT NULL,
    architecture TEXT NOT NULL,
    adapter_key TEXT NOT NULL,
    requirements JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(requirements) = 'object'),
    UNIQUE (emulator_id, upstream_version, os, architecture)
);
CREATE TABLE catalog.suites (
    id UUID PRIMARY KEY,
    genre_id TEXT NOT NULL REFERENCES catalog.genres(id),
    name TEXT NOT NULL
);
CREATE TABLE catalog.suite_releases (
    id UUID PRIMARY KEY,
    suite_id UUID NOT NULL REFERENCES catalog.suites(id),
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'withdrawn')),
    manifest JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(manifest) = 'object'),
    manifest_sha256 TEXT,
    signature TEXT,
    created_by_subject TEXT NOT NULL,
    published_at TIMESTAMPTZ,
    CHECK (status = 'draft' OR (published_at IS NOT NULL AND signature IS NOT NULL AND manifest_sha256 IS NOT NULL AND manifest_sha256 ~ '^[0-9a-f]{64}$')),
    UNIQUE (suite_id, version)
);
CREATE TABLE catalog.suite_games (
    release_id UUID NOT NULL REFERENCES catalog.suite_releases(id),
    edition_id UUID NOT NULL REFERENCES catalog.game_editions(id),
    emulator_release_id UUID NOT NULL REFERENCES catalog.emulator_releases(id),
    display_order INTEGER NOT NULL CHECK (display_order >= 0),
    PRIMARY KEY (release_id, edition_id),
    UNIQUE (release_id, display_order)
);

-- No public/client write grants. Application authorization for Sengoku-Admin is
-- added with the authenticated catalog API; the foundation exposes no writes.
REVOKE ALL ON ALL TABLES IN SCHEMA catalog FROM PUBLIC;
