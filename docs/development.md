# Development guide

Updated November 10, 2026 for local installation/play. This document describes executable behavior; the other architecture documents also include future milestones. See the [operator workflow and examples](operator-cli.md).

## Project map

| Path | Responsibility |
| --- | --- |
| `src/NostalgiaCade.Desktop` | Avalonia/XAML and MVVM presentation; no database or installation policy |
| `src/NostalgiaCade.Client` | C# IPC framing, version checks, discovery and process lifecycle |
| `cmd/ncade-core` | Go composition root; lock, database, endpoint and IPC server |
| `internal/core` | Cabinet status/catalog queries |
| `internal/library` | Storage setup, imports/downloads, durable switching and Dolphin sessions |
| `internal/cli`, `cmd/ncade` | Operator commands and first-run console guide; mutations go through IPC |
| `internal/ipc` | Transport, framing and JSON-RPC dispatch |
| `internal/database` | SQLite/PostgreSQL adapters and embedded migration runner |
| `cmd/ncade-migrate` | Explicit migration CLI for development/deployment tooling |
| `tests/NostalgiaCade.Tests` | C# protocol, real Go-process integration and Avalonia UI tests |

The original Cobra entry point remains separate. Use the scripts below for the new desktop; `go run .` still invokes the earlier CLI scaffold.

## Build and run

Open `NostalgiaCade.slnx` for C# development. The Go module remains at the repository root. Use PowerShell 7 on Windows x64:

```powershell
./scripts/Build.ps1
./scripts/Run.ps1
```

Build produces `artifacts/bin/ncade-core.exe`, `artifacts/bin/ncade.exe`, `artifacts/bin/ncade-migrate.exe`, `artifacts/bin/ncade-catalog.exe`, `artifacts/bin/ncade-pilot.exe`, and the desktop under `src/NostalgiaCade.Desktop/bin/Debug/net10.0`. Only core and operator CLI are copied into the desktop's `core` subdirectory; migration, catalog publishing and PostgreSQL-backed pilot installation remain admin tools. Keep that folder beside the desktop. A direct .NET build without first building Go cannot produce a runnable cabinet bundle.

`Environment.ps1` puts dependency/build caches in ignored `.cache`, enables Go toolchain selection, and disables .NET/Avalonia build telemetry. NuGet dependencies use lock files; Go uses `go.sum`. Do not commit caches, database files, logs, binaries or secrets. Release builds use `./scripts/Build.ps1 -Configuration Release`; installer signing and size measurements remain release work.

## Local state and process lifecycle

The application-state directory defaults to `%LOCALAPPDATA%/NostalgiaCade`. Override `NCADE_DATA_DIR` for an isolated cabinet. Setup accepts either one explicit root (expanded into six content subfolders) or individual emulator/ROM/firmware/save/settings/cache paths. The state directory may be their parent or a separate location, but cannot sit inside a content folder. Setup does not move the running database or an existing library; see the [operator guide](operator-cli.md) for a new installation entirely under one root. `NCADE_CORE_PATH` can override the bundled core executable during debugging. Neither setting provides a remote connection.

The core exclusively locks `core.lock`, migrates `cabinet.db`, then publishes `core-endpoint.json` containing a random pipe name, protocol version and process ID. The desktop discovers an existing core or starts its bundled copy. Its connection is serialized and each exchange has a deadline. Refresh reconnects after a failure; a killed core leaves a harmless stale endpoint which a new core replaces. Closing the desktop requests shutdown only when it connected to the process it directly launched. A second desktop attaching to that core does not stop it on close. The desktop never kills a process based on a discovery-file PID.

An abandoned core can remain running after a UI crash or interrupted shutdown; the next desktop reattaches. Shutdown is rejected while a mutation/game is active. CLI work continues after disconnection. Dolphin sessions survive core crashes and recover using PID plus creation time; ambiguous launches stay blocked for explicit operator reconciliation.

Windows named-pipe access is restricted to the current Windows user and rejects remote clients. This is a local transport boundary, not venue/operator authentication. The prototype assumes a trusted Windows login; it is not a locked-down cabinet. Future privileged device setup must use a separate narrow elevation boundary.

## Database migrations

SQL files are embedded in the core and migration CLI. SQLite migrations run before pipe discovery, with WAL, foreign keys and a busy timeout. Tables cover genres, settings, games, artifacts, operations/steps, active-suite state, emulator profiles and play sessions. Migration 003 upgrades the foundation without editing its applied files, retaining existing local rows and permitting multiple entries to share one artifact.

PostgreSQL migrations create `catalog` and `management` schemas: genres, platforms, games/editions, sources/artifacts/dependencies, emulator releases, versioned suites, organizations, venues, cabinets, memberships and audit records. Large game payloads belong in approved object storage or source URLs; PostgreSQL stores metadata. Runtime database roles/grants will be provisioned with the authenticated API. The database owner running migrations remains privileged.

Seeds create five genres and the application-role labels `Sengoku-Admin` and `Venue-Operator`. They create no accounts, games, URLs or download permissions. There is no catalog-write endpoint. Seeding a role label does not implement authorization: Sengoku-Admin checks must be enforced by every future shared-catalog mutation route. Local additions will remain independent.

To migrate an explicitly selected local SQLite file, ensure its parent directory exists:

```powershell
./artifacts/bin/ncade-migrate.exe --database sqlite --path ./artifacts/dev-cabinet.db
```

For PostgreSQL, supply a Go/pgx-compatible PostgreSQL URI or keyword DSN through `NCADE_POSTGRES_DSN`, then run:

```powershell
./artifacts/bin/ncade-migrate.exe --database postgres
```

The CLI does not accept an Npgsql-style semicolon connection string. Remote environments must use TLS with hostname/certificate verification (`sslmode=verify-full`) and a separately provisioned migration identity. `Set-CatalogConnection.ps1` accepts a hidden password and constructs the URL without printing it. Never paste credentials into tracked files or distribute them with the cabinet. The CLI does not provision an Azure server, network rules, users or credentials. Azure's manually created foundation lacks migration history; reconcile it before running foundation migrations there. The additive pilot module has a separate ledger and can extend the existing foundation without replaying its DDL. See [PostgreSQL E2E installation](postgres-e2e.md).

Migrations run in lexical filename order in one transaction. SQLite uses `BEGIN IMMEDIATE`; PostgreSQL holds a transaction-scoped advisory lock. `schema_migrations` records version and SHA-256 after normalizing line endings. A failed migration rolls back the batch; repeated runs are no-ops. Changed applied files or unknown schema versions fail startup. After a migration is deployed, add a new numbered file rather than changing the old one. This is a forward-only runner: back up before deployment, and use a new repair migration or a verified backup restore. Application rollback cannot assume a schema downgrade is safe.

## Verification

```powershell
./scripts/Test.ps1
./scripts/Test-Postgres.ps1
# For a nonstandard local PostgreSQL installation:
./scripts/Test-Postgres.ps1 -PostgresBin 'D:/PostgreSQL/16/bin'
```

The PostgreSQL script initializes a fresh cluster under `.cache/postgres-tests/<guid>`, binds an ephemeral loopback port, creates `ncade_test_foundation`, runs checks and stops the server in `finally`. Its trust authentication is solely for synthetic localhost test data. It does not use an existing service/data directory. Logs and cluster files remain in the ignored cache for diagnosis.

Tests cover migration upgrade/rollback/checksums, constraints, IPC behavior, setup/import, remove-first ordering, retention, cancellation and recovery. A synthetic Win32 process tests Dolphin argument boundaries, graceful exit and reconnecting a live game after a core crash. UI checks render populated XAML using Avalonia's headless Skia backend and save previews to `artifacts/preview`. The fixture executable is built by Test.ps1 and is never bundled with the desktop.

Local verification used Windows build 19045, .NET 10.0.13 and PostgreSQL 16. Physical Windows 11 cabinet, gamepad/peripheral, emulator launch/return-focus and installer tests are still required. Headless rendering does not establish those capabilities. The GitHub workflow is provided but has not been run on GitHub in this session.

Framework references: [Avalonia headless testing](https://docs.avaloniaui.net/docs/testing/setting-up-the-headless-platform), [Avalonia xUnit integration](https://docs.avaloniaui.net/docs/testing/headless-xunit), [go-winio](https://github.com/microsoft/go-winio), [pgx](https://github.com/jackc/pgx).
