# NostalgiaCade

NostalgiaCade is being designed as a managed arcade library for commercial cabinets and venues. Operators select a curated genre locally or through a central dashboard; a Go application core installs its dependencies, guides hardware setup, and launches games through supported emulators.

**Status: local installation and play implemented, November 10, 2026.** The Avalonia desktop connects to the Go core over a Windows named pipe. A first-run CLI guide offers one user-selected root folder or individual storage paths; operators can import/reference games, download authorized raw files, launch/exit Dolphin, and switch genre collections with old-game removal first. PostgreSQL migrations establish the future shared catalog. No ROMs or emulator binaries are included.

## Run locally

On Windows x64, install PowerShell 7, the .NET 10 SDK (10.0.402 or a compatible later patch), and Go with automatic toolchain download enabled. The repository selects Go 1.26.11. The first build downloads dependencies from official registries.

```powershell
./scripts/Run.ps1
```

Complete the storage guide, register Dolphin, and add permitted game files using the [operator guide](docs/operator-cli.md). Use Tab/arrow keys to select a genre/game and Play; F11 toggles full screen. System shows the actual core version, database schema and data directory. The development build requires the .NET runtime; a signed installer comes in the release milestone.

```powershell
./scripts/Test.ps1             # Builds both languages; unit, IPC and rendered UI checks
./scripts/Test.ps1 -Postgres   # Also creates/tests/stops an isolated local PostgreSQL cluster
```

PostgreSQL is optional for running the desktop. The database test requires a complete PostgreSQL 16+ installation. See [development and migration instructions](docs/development.md) and the [IPC contract](docs/ipc-v1.md).

## Implemented scope

- C# Avalonia 12 / .NET 10 shell with Library, System, connection errors and keyboard navigation.
- Go process lifecycle, single core per data directory, named-pipe handshake, bounded messages, deadlines, reconnect and crash recovery.
- Transactional, checksummed SQLite/PostgreSQL migrations; five genre records with 20 planned entries each, without invented game data.
- Build/run/test scripts, dependency lock files and CI workflow.
- User-selected storage roots; copy/reference imports and approved-host HTTPS downloads with size/hash verification.
- Durable remove-first switching, retry/cancel, persistent-data preservation and shared-file retention.
- Dolphin launch/exit and process recovery; desktop game selection and Play/Exit controls.
- Verified ZIP/7z package installation for DuckStation, PCSX2, Dolphin, RPCS3 and Eden; admin-side PostgreSQL catalog publication/export using environment configuration.

Physical Dolphin/cabinet/peripheral validation, more launch/configuration adapters, cloud API/dashboard, authentication and installer packaging remain subsequent work. The catalog admin tool uses database-role authorization; application-level Sengoku-Admin enforcement belongs to the future authenticated API. The separate LAN multiplayer server remains the final MVP feature milestone.

## Design documents

- [Architecture and technology decisions](docs/architecture.md)
- [Operator CLI: setup, imports, downloads, switching and play](docs/operator-cli.md)
- [Emulator installation, remote catalog and configuration research](docs/emulators.md)
- [Frontend and UX](docs/frontend-ux.md)
- [Go desktop core and installation lifecycle](docs/local-core.md)
- [Go backend, PostgreSQL model, and fleet management](docs/backend-data.md)
- [Venue multiplayer architecture and research plan](docs/multiplayer.md)
- [Per-game multiplayer case-study template](docs/research/multiplayer-case-study-template.md)
- [MVP scope, catalog curation, validation, and open decisions](docs/mvp-plan.md)

Confirmed direction: C# interfaces, Go application/backend logic, PostgreSQL catalog, local and central operator controls, free play, and five genres with 20 catalog entries each. Content comes from user imports and authorized homebrew/freeware sources. Genre changes remove the previous genre's unneeded managed game files before downloading replacements.

A separate LAN-only venue multiplayer server is included as the final MVP feature milestone. Same-cabinet co-op works without a network; supported cross-cabinet sessions require per-game research. Only Sengoku-Admin can add games to the shared catalog; users can maintain local-only game entries independently.

No database credentials belong in this repository or a distributed client. Admin tools read `NCADE_POSTGRES_DSN`; the cabinet uses local SQLite and a reviewed catalog snapshot/export. Implementation tests use disposable local databases. Five reviewed emulator records have been published to the existing Azure catalog; see the emulator guide for the remote schema's migration-history limitation.
