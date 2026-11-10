# PostgreSQL-backed pilot installation

The trusted operator tool `ncade-pilot.exe` reads the Azure catalog, installs all five reviewed emulator packages through the local Go core, imports supplied BIOS/firmware, and installs the selected genre. It records installation runs, steps, observed transitions and cabinet inventory in PostgreSQL. The Avalonia desktop continues to use local IPC and SQLite. This is the CLI E2E bridge; the authenticated cloud API, dashboard and signed installer are later work.

## Catalog and source availability

The draft catalog contains 100 candidate entries: 20 each for Racing, Fighting, Action Adventure, Platform and Hack and Slash, within **1990–2015**. See [the candidate research list](catalog-research.md). These are popularity-informed selections, not a certified sales ranking. Metadata, commercial-use suitability, controller fit and per-game compatibility still need review. Eden is installed as requested, but no Switch release falls within this date window.

The first scan of `D:\ROMS.bk` identified nine supported sources: three fighting, one action-adventure, two platform and three hack-and-slash games. Racing has no matching local source. Soul Blade's CUE/BIN set needs multi-file import support or an operator-prepared supported image; archives are not automatically converted. Unsupported games, updates and DLC are not silently treated as base games.

PostgreSQL stores identifiers, title/platform/year, draft player metadata, genre membership, pinned emulator releases, source policy, filenames, sizes and SHA-256 values. Actual game files and absolute source paths remain local. For PS3 folder games, the recorded digest/size describes **EBOOT.BIN only**, not the entire game directory; the source directory must remain available. Those games are referenced, never copied or removed by genre cleanup. PS2 ISO imports are copied into managed storage and removed when the suite changes.

The supplied r/ROMs platform indexes are research references in `catalog.source_research`, not authorized download artifacts. Public collection metadata reached through the PS2 index supplied 51 file observations for 40 candidates, including six Racing games. Their archive filenames, sizes and publisher SHA-1/MD5 values are stored separately in `pilot_games.metadata.externalFileResearch`; they are not verified playable-ROM hashes. Snahp is recorded as inaccessible/unverified: web access failed, and Firefox Computer Use was blocked by browser URL-policy support. No forum content, cookies or credentials were collected. Finding a title in an index does not establish distribution permission. The seed preserves `user_import` and unreviewed permissions for these commercial candidates.

## Connection and schema

`NCADE_POSTGRES_DSN` comes from the Windows user environment configured with `scripts/Set-CatalogConnection.ps1 -PersistForUser`. The install helper temporarily copies it into its process environment and restores the previous value afterward. `cmd/ncade-pilot/main.go` reads it with `os.Getenv`; `internal/database/database.go` opens the bounded connection pool. No DSN belongs in a repository file, desktop bundle, operation journal or emulator environment. The supplied Azure configuration uses verified TLS.

The existing Azure foundation was created manually and has no `public.schema_migrations` history. **Do not replay the foundation migration command against it.** The pilot uses a separate checksummed `management.pilot_migrations` ledger and transactional, additive DDL from `internal/pilot/schema.sql`. It refuses untracked table collisions or modified migration checksums. On a new empty database, apply the normal PostgreSQL foundation migrations and publish the emulator catalog first, then apply the pilot extension.

```powershell
./scripts/Build.ps1
$env:NCADE_POSTGRES_DSN = [Environment]::GetEnvironmentVariable('NCADE_POSTGRES_DSN','User')
./artifacts/bin/ncade-pilot.exe --action schema --yes
./artifacts/bin/ncade-pilot.exe --action scan --sources 'D:\ROMS.bk' --out ./local-imports.json
./artifacts/bin/ncade-pilot.exe --action seed --subject Sengoku-Admin --bindings ./local-imports.json
./artifacts/bin/ncade-pilot.exe --action research --subject Sengoku-Admin --research-file catalog/pilot-public-file-metadata.json
./artifacts/bin/ncade-pilot.exe --action list
```

Keep local binding files outside version control. The scanner reads/hashes only matching supported files and fails on ambiguous editions. A reviewed binding can later be seeded without duplicating existing games or sources. It does not overwrite already-authored candidate metadata. `--subject` supplies audit attribution; the database role supplies privileges. It does not authenticate an application-level Sengoku-Admin. Keep this administrative tool and its write credentials with the trusted operator until server-side identity enforcement is implemented.

## Preview and install

Use a separate directory for the test to preserve the established `C:\.ncade` profiles. The following command previews the available Platform subset. Omit `-AvailableOnly` to require all 20 imports; a missing source then blocks the run before setup, package installation or game removal.

```powershell
./scripts/Install-CatalogSuite.ps1 `
  -Root 'C:\.ncade\E2E\postgres-pilot' `
  -Genre platform `
  -Bindings 'C:\.ncade\Catalog\local-imports.json' `
  -Cabinet postgres-e2e-01 `
  -AvailableOnly
```

Add `-Apply -FirmwareFrom 'C:\.ncade\Firmware'` to execute. For another genre change, change `-Genre` and keep the same root and cabinet name. Existing identical firmware imports are idempotent; different files are rejected without replacement. Omitting `-FirmwareFrom` requires prerequisites to be onboarded already. Package URLs, byte counts, SHA-256 values and executable paths come from the remote catalog. Suites retain their pinned emulator releases. Optional `-Archives <directory>` uses already-reviewed `dolphin.7z`, `duckstation.zip`, `eden.zip`, `pcsx2.7z`, and `rpcs3.7z`; hash checks remain mandatory.

The operation order is emulator installation/preparation, supplied prerequisite imports, readiness checks, local source/hash preflight, old managed game removal, new game acquisition, then inventory publication. Saves, settings, firmware and emulator installations are retained. A failed download after removal leaves the old managed games absent by design. External originals are never cleanup targets.

Open the resulting local library without rebuilding a running core:

```powershell
$env:NCADE_DATA_DIR = 'C:\.ncade\E2E\postgres-pilot'
dotnet run --project ./src/NostalgiaCade.Desktop --no-build
```

Native emulator welcome screens and controller configuration may still need the operator. “Prerequisites ready” does not mean a game has passed playability or hardware/peripheral validation. The existing Eden shutdown reliability limitation remains.

## Operation records and recovery

| Table | Purpose |
| --- | --- |
| `catalog.pilot_games`, `pilot_genres` | Candidate keys, review metadata and draft suite associations |
| `catalog.pilot_sources` | Verified local-import metadata associated with actual artifacts |
| `catalog.onboarding_steps` | Typed BIOS/firmware steps and official documentation links |
| `catalog.source_research` | Research provenance/access status, separate from download permission |
| `management.installation_runs` | Cabinet, release, actor attribution, partial-selection status and outcome |
| `management.installation_steps`, `installation_events` | Local operation IDs and observed state/phase transitions |
| `management.cabinet_installations` | Observed emulator versions, prerequisite readiness, games and suite inventory |

Events are polling observations, not a guaranteed copy of every local phase. The core's durable SQLite journal remains authoritative. Local coordinator journals live in `<data-dir>/pilot-runs`. If PostgreSQL disconnects, an already-submitted local operation may continue; the coordinator stops submitting later steps. Inspect local operations before restarting. Reconciliation reports observed state and does not resume installation automatically:

```powershell
./artifacts/bin/ncade-pilot.exe --action report
./artifacts/bin/ncade-pilot.exe --action sync `
  --data-dir 'C:\.ncade\E2E\postgres-pilot' `
  --journal 'C:\.ncade\E2E\postgres-pilot\pilot-runs\RUN-ID.json'
```

`sync` updates remote observations; the original local coordinator journal is retained. Installation completion requires a completed final switch. Moving an installation to another machine/data directory or changing its cabinet name creates a separate pilot identity. The four-cabinet fleet model is supported by distinct identities; this E2E run registers only the cabinet actually tested.

## Observed E2E results — November 10, 2026

| Check | Observed result |
| --- | --- |
| Azure catalog | 100 draft candidates, 20 per genre; nine real local-source metadata bindings |
| Fresh install | All five pinned packages downloaded from official URLs and installed; all five profiles report prerequisites ready |
| Platform selection | Jak II and Sly 2 installed as managed ISO copies; partial selection explicitly recorded |
| Switch to Hack and Slash | Samurai Warriors and Samurai Warriors 2 copied; God of War Collection referenced from its original PS3 folder |
| Remove-first sequence | Remote observations show `removing_old` at 18:35:19 EST, `acquiring` at 18:35:25 and `ready` at 18:35:52; both old Platform copies are absent |
| Retention | 5,080 E2E saves/settings/firmware/emulator files hash-identical across the switch, including a save-retention test marker |
| Earlier user files | All 490 files in the earlier preservation baseline remain hash-identical |
| Operation reporting | Two completed installation runs, five installed emulators, five ready profiles and three active games in the remote inventory |
| Missing Racing sources | Strict preflight rejected the selection before any setup or removal; external file research does not change that outcome |
| Automated checks | Go vet/unit tests, 16 .NET desktop/IPC tests and disposable PostgreSQL migration/catalog/pilot tests passed |

Diagnostics are saved locally at `C:\.ncade\Diagnostics\postgres-pilot-20261110`. The separate E2E root retains the three Hack and Slash entries for the user's next test; its idle core was shut down after validation. This pass tested acquisition, installation and switching, not new real-game launches, frame rates, controller mappings or a packaged Windows installer.
