# Catalog database and Go API

Updated November 11, 2026. PostgreSQL stores NCade's shared emulator, artifact, genre/Arcade Suite and onboarding catalog. Cabinet installation state, operations, play sessions, saves and recovery belong to local SQLite and filesystem journals.

| Requirement | Catalog tables |
| --- | --- |
| Offered emulators and latest downloads | `emulators`, `emulator_releases`; `latest_emulator_releases` view exposes URLs and byte counts |
| Typed downloadable artifacts | `artifacts`, `artifact_sources`, `artifact_dependencies`, `game_edition_artifacts` |
| Games grouped into Arcade Suites | `games`, `game_editions`, `platforms`, `genres`, `game_genres`, `suites`, `suite_releases`, `suite_games` |
| Emulator onboarding | `onboarding_steps` |

There are 15 application tables. Game editions retain platform/region/revision identity and candidate metadata. Suite releases retain ordered membership and pinned emulator versions so publishing a newer emulator does not silently change an existing suite. Artifact sources retain local imports separately from downloadable URLs. Dependency links support shared firmware and supporting artifacts. These relationships serve installation and curation rather than duplicating the same records.

Migration 005 folds `pilot_games` into `game_editions`, `pilot_sources` into `game_edition_artifacts`, `pilot_genres` into suite release selection, and `emulator_packages` into `emulator_releases`. It removes source-research tables, remote management/installation reporting, and remote copies of cabinet-local tables. Per-game research metadata remains on editions. Historical migrations stay immutable; old pilot DDL remains only as a migration test fixture. QA retains two migration-history tables in addition to the application tables.

The migration executes under a transaction and advisory lock, uses explicit DROP statements without CASCADE, and rolls back when an unexpected dependency exists. Normal databases use migrations 001–005. Manually provisioned databases use `--cleanup-catalog` and a separate cleanup ledger rather than claiming that the historical foundation migrations ran. Always take a restorable backup before cleanup.

The trusted `ncade-api` service inserts artifact metadata using a server-configured Sengoku-Admin bearer credential. The stored artifact records the authenticated subject; callers cannot claim a role or actor in the request. SQL parameters, bounded requests and generic database errors protect the service boundary. A dedicated runtime database role should have only the catalog access it needs. Multi-user identity, hosted deployment and signed cabinet delivery are separate future work, and do not create placeholder tables now.

See [API setup, endpoint contracts and cleanup commands](catalog-admin-api.md), [catalog installation](postgres-e2e.md) and [migration/testing instructions](development.md). The C# desktop still uses the local Go core through IPC and requires no PostgreSQL or admin API credentials.
