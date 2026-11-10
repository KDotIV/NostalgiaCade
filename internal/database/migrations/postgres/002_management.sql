CREATE SCHEMA management;
REVOKE ALL ON SCHEMA management FROM PUBLIC;
CREATE TABLE management.roles (name TEXT PRIMARY KEY);
INSERT INTO management.roles(name) VALUES ('Sengoku-Admin'), ('Venue-Operator');

CREATE TABLE management.organizations (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL
);
CREATE TABLE management.venues (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES management.organizations(id),
    name TEXT NOT NULL,
    UNIQUE (organization_id, id)
);
CREATE TABLE management.memberships (
    organization_id UUID NOT NULL REFERENCES management.organizations(id),
    subject TEXT NOT NULL,
    role TEXT NOT NULL REFERENCES management.roles(name),
    PRIMARY KEY (organization_id, subject, role)
);
CREATE TABLE management.cabinets (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    venue_id UUID NOT NULL,
    name TEXT NOT NULL,
    state_revision BIGINT NOT NULL DEFAULT 0 CHECK (state_revision >= 0),
    last_seen_at TIMESTAMPTZ,
    FOREIGN KEY (organization_id, venue_id) REFERENCES management.venues(organization_id, id),
    UNIQUE (organization_id, id)
);
CREATE TABLE management.audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id UUID REFERENCES management.organizations(id),
    actor_subject TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    outcome TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_by_organization ON management.audit_events(organization_id, occurred_at DESC);
REVOKE ALL ON ALL TABLES IN SCHEMA management FROM PUBLIC;
