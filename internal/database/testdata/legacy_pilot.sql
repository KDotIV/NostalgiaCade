-- Additive pilot module; the existing catalog and management foundation is required.
CREATE TABLE catalog.pilot_games (
    game_key TEXT PRIMARY KEY,
    edition_id UUID NOT NULL UNIQUE REFERENCES catalog.game_editions(id),
    emulator_slug TEXT NOT NULL REFERENCES catalog.emulators(slug),
    metadata JSONB NOT NULL CHECK (jsonb_typeof(metadata)='object')
);
CREATE TABLE catalog.pilot_sources (
    game_key TEXT NOT NULL REFERENCES catalog.pilot_games(game_key),
    artifact_id UUID NOT NULL REFERENCES catalog.artifacts(id),
    filename TEXT NOT NULL,
    reference_only BOOLEAN NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(game_key,artifact_id)
);
CREATE TABLE catalog.pilot_genres (
    genre_id TEXT PRIMARY KEY REFERENCES catalog.genres(id),
    release_id UUID NOT NULL UNIQUE REFERENCES catalog.suite_releases(id)
);
CREATE TABLE catalog.onboarding_steps (
    emulator_slug TEXT NOT NULL REFERENCES catalog.emulators(slug),
    step_key TEXT NOT NULL,
    instructions JSONB NOT NULL CHECK (jsonb_typeof(instructions)='object'),
    PRIMARY KEY(emulator_slug,step_key)
);
CREATE TABLE catalog.source_research (
    url TEXT PRIMARY KEY CHECK(url LIKE 'https://%'),
    access_status TEXT NOT NULL CHECK(access_status IN ('reviewed_index','not_accessible')),
    download_authorization TEXT NOT NULL CHECK(download_authorization='unverified'),
    notes TEXT NOT NULL,
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE management.installation_runs (
    id UUID PRIMARY KEY,
    cabinet_id UUID NOT NULL REFERENCES management.cabinets(id),
    release_id UUID NOT NULL REFERENCES catalog.suite_releases(id),
    created_by_subject TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('planned','running','completed','failed','interrupted')),
    selection JSONB NOT NULL CHECK(jsonb_typeof(selection)='object'),
    error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX installation_runs_by_cabinet ON management.installation_runs(cabinet_id,created_at DESC);
CREATE TABLE management.installation_steps (
    run_id UUID NOT NULL REFERENCES management.installation_runs(id),
    sequence INTEGER NOT NULL CHECK(sequence>=0),
    kind TEXT NOT NULL,
    local_operation_id TEXT,
    state TEXT NOT NULL,
    phase TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(detail)='object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(run_id,sequence)
);
CREATE TABLE management.cabinet_installations (
    cabinet_id UUID NOT NULL REFERENCES management.cabinets(id),
    kind TEXT NOT NULL CHECK(kind IN ('emulator','game','firmware','suite')),
    item_key TEXT NOT NULL,
    detail JSONB NOT NULL CHECK(jsonb_typeof(detail)='object'),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(cabinet_id,kind,item_key)
);
CREATE TABLE management.installation_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES management.installation_runs(id),
    step_sequence INTEGER NOT NULL,
    state TEXT NOT NULL,
    phase TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
REVOKE ALL ON catalog.pilot_games,catalog.pilot_sources,catalog.pilot_genres,catalog.onboarding_steps,catalog.source_research FROM PUBLIC;
REVOKE ALL ON management.installation_runs,management.installation_steps,management.cabinet_installations,management.installation_events FROM PUBLIC;
