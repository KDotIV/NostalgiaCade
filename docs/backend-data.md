# Go backend and PostgreSQL design

Status: foundation catalog/management tables and an additive pilot installation module are implemented, November 10, 2026. Azure contains five emulator packages, 100 draft game candidates, nine local-source metadata bindings and source research. The trusted CLI connects to PostgreSQL and reports installations; the authenticated HTTP API and remaining fleet model below are still proposed. See [the implemented E2E workflow and table list](postgres-e2e.md).

## Service boundaries

Use one Go HTTP service with modules for catalog, publication, identity/authorization, fleet commands and auditing. PostgreSQL is the system of record for remote metadata; the cabinet's SQLite database is authoritative for what is actually installed locally.

The separate venue multiplayer coordinator and upstream game services run on the LAN. Remote PostgreSQL stores their approved profiles and optional coarse health metadata, not live gameplay state. Local session records, service credentials and any required game-service databases remain at the venue. See [venue multiplayer](multiplayer.md).

Store large game/package bytes in object storage or leave them at approved upstream URLs. PostgreSQL stores identifiers, digests, versions, sizes, source policies and storage keys. Although PostgreSQL can store binary data, putting multi-gigabyte game images in the operational database would couple content transfer and backup/restore workloads unnecessarily.

The trusted catalog/pilot tools connect to PostgreSQL during this prototype; the production design moves catalog/fleet access behind the Go API. The supplied connection string uses .NET/Npgsql-style options; a Go driver needs its own configuration and pool options. Do not paste it unchanged into Go or a client executable.

Use separate migration and runtime database roles, parameterized SQL, bounded connection pools and verified TLS. Set total pool capacity against the server budget across every API replica, not independently assuming each replica can use the entire limit. Configure `sslmode=verify-full` and appropriate trust roots for Azure; do not disable certificate verification to fix connectivity. See [Azure PostgreSQL TLS guidance](https://learn.microsoft.com/en-us/azure/postgresql/security/security-tls-how-to-connect).

Secrets come from development environment injection and a deployment secret manager. The exposed discovery credential needs rotation. Redact connection strings, authorization headers, signed download URLs and private paths from diagnostics.

## Sengoku-Admin and local-library authority

Only the application role `Sengoku-Admin` may add games to the shared PostgreSQL catalog. Derive that role from trusted server-side identity/role assignments, never a request body's claimed role, a cabinet token or a local operator PIN. It is not the PostgreSQL administrative username. Cabinet and venue-operator credentials cannot create global game records.

Recommended policy also reserves canonical catalog edits, deletions, import jobs, suite publication and approved multiplayer-service/profile publication to Sengoku-Admin. All write routes, including bulk import and CLI/API paths, check the same authorization rule. Initial seed/migration tooling is a controlled administrative operation, not an operator-facing bypass. Audit actor, role/scope and affected catalog records. Role assignment itself requires controlled identity administration; no self-promotion from cabinet or operator APIs.

Users may create `local_games` entries in cabinet SQLite and install/launch local content through local-library access. These entries can optionally link to an existing catalog edition but cannot silently create or modify one. Do not upload local titles, private paths or content automatically. A Sengoku-Admin explicitly creates a reviewed canonical record when a local candidate is later selected for the shared catalog.

Separate read/fleet database access from catalog-writing access where practical, granting the writer role only the needed catalog operations. Go authorization remains necessary: SQL credentials alone do not identify the human caller. Unauthorized direct API calls must fail even if a user discovers a hidden dashboard endpoint.

## Proposed table families

This table describes the broader target model, including future entities and fields. Executable foundation DDL lives under `internal/database/migrations/postgres`; the implemented pilot additions live in `internal/pilot/schema.sql`. Do not assume every proposed field below already exists.

| Tables | Main fields and purpose |
| --- | --- |
| `genres` | UUID, unique slug, display name, description, display order |
| `platforms` | UUID, unique slug, manufacturer/name, console/arcade category |
| `games`, `game_genres` | Canonical game identity, title, original release year, player counts, many-to-many genre classification |
| `game_editions` | Game/platform foreign keys, region, edition/revision, platform title ID, edition release date; determines exact supported files |
| `emulators`, `emulator_releases` | Official project identity, pinned upstream release/build, OS/architecture, adapter key/version, executable artifact and license reference |
| `artifacts` | Immutable UUID/digest, kind, download bytes, installed-size bound, format, license/provenance evidence, retirement state |
| `artifact_sources` | Artifact foreign key, acquisition mode, provider URL or object key, allowed hosts, availability, source policy and review date |
| `artifact_dependencies` | Artifact-to-artifact edges for shared BIOS/device files, parent sets, runtimes and other required content |
| `game_edition_artifacts` | Edition-to-artifact edges, role and required/optional status; supports multi-file and multi-disc games |
| `suites`, `suite_releases` | Genre, immutable release number, draft/published/withdrawn lifecycle, manifest digest/signature, minimum supported client and publication metadata |
| `suite_games` | Suite-release/edition membership, ordering, pinned emulator release, launch/configuration profile and compatibility profile |
| `suite_dependencies` | Suite-release-to-artifact edges for dependencies beyond the selected games |
| `hardware_profiles`, `suite_hardware_profiles` | Supported rig/device identifiers, utility artifact references, mapping schema, suite association and setup-guide revision |
| `setup_guides` | Versioned, typed guide steps, supported hardware/software context, source links and review date |
| `compatibility_profiles` | Emulator/game scope, hard requirements, render settings, tested hardware/evidence, observed result and last review |
| `multiplayer_profiles` | Versioned game-edition/mode, mechanism, players/cabinets, emulator/settings/content agreement, state/save authority, LAN policy and verification status |
| `multiplayer_service_releases`, `multiplayer_profile_services` | Pinned local service artifacts, adapter/schema versions, runtime/storage needs, health checks, protocol endpoints and profile dependency edges |
| `multiplayer_case_studies` | Edition/profile, evidence links, test versions/date, local/online/cross-cabinet findings, unresolved dependencies and Sengoku-Admin publication review |
| `venue_servers` | Venue-scoped server identity, approved profile/configuration revision and optional coarse last-reported health; no public lobby address or session tunnel |
| `organizations`, `venues`, `memberships` | Tenant/venue structure, external identity subject, operator role and scoped permissions |
| `cabinets` | Organization/venue, unique device identity, enrollment/revocation state, last-seen, hardware summary, actual/desired suite and state revision |
| `cabinet_commands`, `command_events` | Idempotent command ID, actor, expected state revision, target suite, deadline, delivery/execution state and sequenced progress |
| `content_permissions` | Game/artifact and organization scope, acquisition/distribution/venue-use evidence, review status, expiry where applicable |
| `audit_events` | Actor, scope, action, resource, correlation ID, outcome and timestamp; no tokens or raw secrets |

Use UUID keys, `timestamptz` for timestamps, nonnegative `bigint` byte counts, foreign keys and unique constraints. Record release identity separately from a mutable download URL. Use JSONB for versioned requirements, mappings and typed guide steps; relational identity and membership should not disappear into an unvalidated JSON blob.

Published suite releases refer to immutable game/profile/artifact revisions or embed a complete signed snapshot of mutable authoring data. A published manifest cannot silently change when a Sengoku-Admin edits an upstream game row. Withdrawing a source is a separate recorded availability event.

Publish multiplayer metadata alongside exact game-edition references, with per-mode support states and immutable service/profile revisions. Seed unknown modes as unresearched, not supported. `local_games`, live `sessions`, `session_participants`, `service_instances` and detailed session journals belong in local stores; the venue coordinator owns session state and the cabinet core owns its active reservations. Upstream services retain their native local database requirements.

Index genre/game membership, suite revision lookup, artifact digest, venue/cabinet lookup and pending commands by cabinet/status. Scope every tenant resource query by authorized organization/venue. Prevent cross-tenant references in database constraints and service authorization; UI filtering is not isolation.

## Sources and permission metadata

Supported acquisition modes are `object_store`, `authorized_url`, `user_import` and `manual_vendor_step`. Store object keys, not expiring signed URLs, as the durable identity. Mint short-lived download access only after authorization and log the artifact ID instead of the URL secret.

A game can have catalog metadata without a downloadable source. For proprietary imports, store expected identifiers/hash information where available; the operator supplies content locally. Unknown imports must not be silently treated as a tested game edition. Do not upload, redistribute or copy imported ROMs between cabinets as a side effect of central management.

Track download/redistribution permission separately from suitability for commercial venue use. Preserve the evidence source and review status; "freeware" is descriptive metadata, not an automatic permission flag. Commercially unsuitable or unreviewed dependencies are not published as approved turnkey venue packages.

Emulator requirements must include exact release terms. For example, DuckStation's current documented restrictions require a separate integration decision. Other emulators also need license/notice/source handling appropriate to the selected build. Do not assume one project's terms apply to the others.

## Catalog publication

1. Sengoku-Admin authors or imports a draft with explicit game editions, pinned emulator versions, sources, profiles and guides.
2. Validate dependency closure, acyclic artifact dependencies, hashes, sizes, paths, source policies, commercial-use evidence and supported adapter capabilities.
3. Run the selected cabinet/profile validation; record evidence and any exclusions.
4. Publish an immutable manifest with schema version, revision, digest, signature and minimum client compatibility. Keep signing keys outside the database/client.
5. Cabinets pin the published release for an operation. Updating "latest" does not change an installation already in progress.

Manifests contain declarative data understood by installed adapters. Neither catalog administration nor a compromised catalog row should be able to introduce a new arbitrary executable action without a signed application/adapter release.

## API sketch

| Endpoint family | Purpose |
| --- | --- |
| `GET /v1/genres`, `/v1/suites`, `/v1/suites/{id}/releases/{version}` | Discover collections and fetch versioned manifests |
| `POST /v1/artifacts/{id}/download-access` | Obtain authorized source information/short-lived access |
| `/v1/catalog/games`, `/v1/catalog/drafts`, `/v1/catalog/publications` | Sengoku-Admin-only canonical game creation and catalog authoring/publication |
| `/v1/enrollments` | Expiring operator-approved cabinet enrollment; never shared embedded credentials |
| `GET /v1/venues/{id}/cabinets`, `/v1/cabinets/{id}` | Scoped fleet inventory and actual state |
| `POST /v1/cabinets/{id}/commands` | Queue a limited typed action with idempotency key and expected state revision |
| `GET /v1/device/commands`, `POST /v1/device/command-events` | Cabinet polls its own commands and reports durable progress |
| `POST /v1/device/heartbeat` | Actual state, health and last-known operation; no continuous private gameplay collection |

Use pagination, explicit API versions, validation limits, request deadlines and stable error codes. Long operations return command/operation IDs. Retry-safe requests reuse the same idempotency key; a reused key with different content is rejected.

The venue coordinator has a distinct LAN-only API for local service status, session creation/join/end and participant readiness. It is not reverse-proxied under the public API. Session tokens are venue-scoped and cannot be substituted with a cloud operator login. Detailed LAN endpoint/port contracts are finalized after the per-game service survey.

## Local and central coordination

The hosted API authorizes a typed request; only the cabinet can report that it actually executed. Delivery is at least once. The core persists received command IDs and deduplicates them before any destructive step. Server acceptance, device receipt and successful execution are separate states.

Each request names the expected cabinet state revision. The cabinet serializes local and remote requests through the same durable operation queue and rejects/replans conflicts. Proposed policy: wait for the current game to exit and any multiplayer reservation to end, do not interrupt players remotely by default, and expire stale queued requests. The local running-session lock prevents genre deletion/update even if a coordinator lease or cloud heartbeat expires. Local operations update the server on reconnect so the dashboard does not keep forcing an obsolete desired genre.

One operation owns the library lock. A cancellation arriving after removal cannot restore missing files; its result reports that limitation. Report progress with monotonically increasing sequence numbers and reconcile snapshots after reconnect rather than assuming every event was delivered.

The dashboard can batch a genre change across cabinets, but each cabinet has an independent plan/result. Cap concurrent downloads per venue and stagger starts to avoid multiplying large content transfers by the entire fleet at once. Exact concurrency and refresh targets depend on pilot size and network capacity.

## Identity, offline operation and deployment

Use an established OIDC provider for operator sign-in and enforce organization/venue roles in Go. The Blazor client is a public client with no embedded client secret. Device enrollment issues credentials scoped to one cabinet; support revocation and rotation. Apply the same authorization checks to artifact download access and command creation.

Cabinets retain the last verified installed suite and required guides. Single-player and same-cabinet modes follow recorded terms without an additional network dependency. LAN-qualified modes depend only on their approved local peers/services and remain usable without Internet; backend-dependent modes can be unavailable when the venue host is down. Offline central commands remain visibly pending/expired, never silently "complete."

Deploy the API as a container, initially beside the existing Azure PostgreSQL environment if its networking/operations requirements fit. Static dashboard assets can share its origin. Use a migration step with a dedicated role, health/readiness endpoints, database backups with a tested restore, redacted structured logs and audit retention. A message broker, Redis and separate microservices are not initial dependencies.

## Prototype seed plan

Seed the five confirmed genres and proposed platform/emulator metadata. Create draft suite releases with 20 intended slots each, then populate them with researched, reviewed game editions. Empty slots are not fake playable games. Seed actual artifact sources only after verification; no invented download URLs, placeholder digests or proprietary ROM payloads in fixtures.

Use synthetic fixtures or explicitly redistributable test content for automated tests. Implement and validate migrations against disposable local PostgreSQL first. A remote schema deployment is a later implementation/deployment step after the design and rotated credentials are ready.
