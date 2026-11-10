-- Exact package metadata is published by the operator/admin tool. Cabinets
-- consume an exported catalog; PostgreSQL credentials never enter the desktop.
CREATE TABLE catalog.emulator_packages (
    release_id UUID PRIMARY KEY REFERENCES catalog.emulator_releases(id),
    manifest JSONB NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    verified_at TIMESTAMPTZ NOT NULL,
    published_by_subject TEXT NOT NULL CHECK (length(trim(published_by_subject)) > 0)
);
REVOKE ALL ON catalog.emulator_packages FROM PUBLIC;
