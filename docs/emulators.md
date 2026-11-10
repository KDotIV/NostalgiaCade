# Emulator installation and configuration

Verified November 10, 2026. The diagnostic cabinet is `C:\.ncade`. Five official Windows x64 packages were downloaded, hashed, extracted there and recorded in its SQLite inventory. The same five releases and their source URLs were published to the Azure `cade-archives` catalog and read back successfully. Original emulator folders in Documents were inspected without modification.

## Reviewed releases

| Emulator | Selected release | Official downloads | Current NostalgiaCade support |
| --- | --- | --- | --- |
| DuckStation | Rolling package updated November 7, 2026 | [Windows downloads](https://www.duckstation.org/windl) | Package installation; launch/configuration adapter pending |
| PCSX2 | v2.8.2 stable | [Release](https://github.com/PCSX2/pcsx2/releases/tag/v2.8.2) | Package installation; launch/configuration adapter pending |
| Dolphin | 2609b release | [Downloads](https://dolphin-emu.org/download/) | Package installation, profile registration, game launch/exit and persistent save paths |
| RPCS3 | 0.0.43-20581, fd027c99 | [Official Windows builds](https://github.com/RPCS3/rpcs3-binaries-win/releases) | Package installation; firmware/title installation and launch adapter pending |
| Eden | v0.2.1 stable, x64 Clang PGO | [Official downloads](https://eden-emu.dev/downloads/) | Package installation; firmware/key onboarding and launch adapter pending |

Eden is the selected Switch candidate for this pilot. Its official site provides a maintained release channel, documented requirements, and recommends PGO builds when available. The supplied Eden folder already contains the same stable version using the MSVC build; the catalog chooses the upstream-recommended PGO variant. The old Citron website returned no usable release content during verification, and the supplied Citron 0.11.0 is not evidence of a current maintained release. This selection is an integration recommendation, not a measured claim that Eden performs best for every game. Benchmark representative titles before promoting a cabinet profile. See [Eden release notes](https://git.eden-emu.dev/eden-emu/eden/releases) and [requirements](https://eden-emu.dev/system-requirements/).

DuckStation's supplied and upstream licenses are **CC-BY-NC-ND-4.0**, with a noncommercial restriction. It is cataloged and installed for prototype investigation; the catalog explicitly records that commercial venue deployment needs licensing review/permission. Catalog publication does not approve redistribution or game rights. See the [upstream license](https://github.com/stenzek/duckstation/blob/master/LICENSE).

The machine has the Microsoft Visual C++ x64 runtime registered as v14.51.36247.00. Runtime installation, firmware onboarding, real gameplay and peripheral testing are distinct from archive installation. No BIOS, console keys, firmware, games or user profiles were copied from Documents or downloaded by this milestone.

## Cabinet commands

```powershell
$env:NCADE_DATA_DIR = 'C:/.ncade'
./artifacts/bin/ncade.exe emulator catalog
./artifacts/bin/ncade.exe emulator install --id dolphin
./artifacts/bin/ncade.exe emulator list
```

Other IDs are `duckstation`, `pcsx2`, `rpcs3`, and `eden`. `--archive 'E:/Packages/package.7z'` uses a local official archive and still enforces the catalog's exact size and SHA-256. `--catalog ./reviewed-catalog.json` consumes an exported catalog. Omitting that option uses the reviewed snapshot embedded in `internal/emucatalog/windows.json`.

The installer runs as a durable core operation: validate → acquire → extract → verify Windows x64 executable → publish release directory → commit inventory/profile. Existing operation show/retry/cancel commands apply. A second installation of the same package verifies its recorded file hashes; modified installed files are rejected. The active profile changes only after successful publication. Old releases remain available; automatic emulator garbage collection is not implemented.

Releases live at `Emulators/packages/<slug>/<archive-sha256>/`. Archives are extracted with Go ZIP/7z readers, without executing installers or scripts. Names are checked for traversal, device names, alternate streams, links and case collisions. Limits are 1 GiB compressed, 2 GiB expanded and 20,000 entries, plus the cabinet's existing free-space reserve. Downloads use reviewed vendor hosts and verified TLS, including redirect/DNS checks. Game download hosts remain a separate setup policy. A changing rolling URL fails checksum validation until an operator reviews a new catalog snapshot.

Staging uses journaled operation names beneath the owned emulator root and is cleaned on failure/restart. Genre switching continues to remove only inventoried game files. It never cleans emulator package directories. ZIP/7z support here applies to emulator packages; importing game archives is still outside the game-import adapter.

`launchSupported` currently means a NostalgiaCade launch adapter exists, not that every title, controller, firmware or machine is ready. Only Dolphin is registered automatically. Avoid manually starting the other package executables as a configuration shortcut: their default user-data locations have not yet been connected to the cabinet's selected save/settings roots.

## Remote catalog administration

`ncade-catalog.exe` is an admin tool built into `artifacts/bin`; it is not copied into the desktop bundle. It reads only `NCADE_POSTGRES_DSN`. The supplied semicolon/Npgsql string is not passed to pgx; the helper creates a PostgreSQL URL with certificate/hostname verification and a connection timeout:

```powershell
./scripts/Set-CatalogConnection.ps1 -Server YOUR_SERVER -Database YOUR_DATABASE -Username YOUR_ADMIN
# Password is entered at a hidden prompt. Add -PersistForUser only when desired.
./artifacts/bin/ncade-catalog.exe --action inspect
./artifacts/bin/ncade-catalog.exe --action publish --subject YOUR_ADMIN_SUBJECT
./artifacts/bin/ncade-catalog.exe --action export > ./reviewed-catalog.json
```

An already configured Windows user variable can be loaded into a fresh terminal with `$env:NCADE_POSTGRES_DSN = [Environment]::GetEnvironmentVariable('NCADE_POSTGRES_DSN','User')`. Never print its value or include it in a catalog export. Child cores and emulator processes remove the catalog credential from their inherited environment.

Publishing validates the entire catalog and uses one transaction with idempotent identifiers. Existing release versions cannot silently change artifact hashes. Data goes to `catalog.emulators`, `catalog.emulator_releases`, `catalog.artifacts`, `catalog.artifact_sources`, and `catalog.emulator_packages`. Permission status remains `unreviewed`; download provenance and deployment permission are distinct. The subject is audit attribution, not an authorization bypass: database-role privileges authorize this admin tool. The future authenticated API must enforce Sengoku-Admin publication and serve signed manifests to cabinets.

The inspected Azure database already has the catalog/package tables, alongside separately created `public` tables, but **no `public.schema_migrations` history**. No DDL or migration-history adoption was performed there. Do not run the fresh-database migration command against that database until its manually applied schema has been reconciled with a verified baseline. Local SQLite remains the cabinet's inventory; remote PostgreSQL remains catalog/fleet data. A cabinet can install from an exported catalog offline using verified local archives, without a PostgreSQL connection.

For a fresh PostgreSQL database, the existing migration runner applies `001` through `004`. Automated tests use a disposable local server and cover migration constraints, repeated publication, export and rollback on conflicting releases. They never target `NCADE_POSTGRES_DSN`.

## Reviewing updates

`scripts/Refresh-EmulatorCatalog.ps1` discovers releases from upstream APIs/pages, downloads archives into ignored `artifacts/catalog-review`, checks GitHub-provided digests where available, and measures size/hash. It writes a candidate `windows.json`; it does not publish or change a cabinet. Dolphin's website may require a browser: pass `-DolphinReleaseURL` with a link independently verified on its official download page. Dolphin and Eden digests in this snapshot were measured from downloads over verified HTTPS; they are not claimed to be upstream signatures.

Review extracted contents, license changes and compatibility before replacing the embedded snapshot or publishing a candidate with `ncade-catalog --action publish --file ...`. Upstream license notices remain in installed archives. Do not use an unreviewed URL or silently upgrade a working suite to a nightly build.

## Configuration and patch research

Local inspection found these configuration surfaces. This is the implementation plan for the next adapter work; no existing preferences were rewritten.

| Emulator | Evidence found | Proposed integration |
| --- | --- | --- |
| DuckStation | `settings.ini` with CPU/GPU, BIOS, MemoryCards, Folders, Pad1–8 and Hotkeys sections; inputprofiles and gamesettings directories | Read/write only owned INI keys through a versioned adapter; keep BIOS paths, saves and controller profiles external to release folders. Confirm the pinned version's user-directory selection before launch support. |
| PCSX2 | Supplied path has `PCSX2_ui.ini`, `PCSX2_vm.ini`, GSdx and LilyPad plugin settings; no executable | Treat this as legacy user data. Current Qt PCSX2 loads `inis/PCSX2.ini` and checks settings version. Build an explicit migration preview for bindings/cards; do not paste legacy plugin files into the new installation. |
| Dolphin | Actual Config folder under `%APPDATA%/Dolphin Emulator`, including Dolphin.ini, GFX.ini, GCPadNew.ini and WiimoteNew.ini | Existing adapter already selects cabinet user/save locations. Add an explicit profile-import preview and controller onboarding, preserving unknown INI keys. |
| RPCS3 | `config/config.yml`, patch_config.yml, input-related files and a LogitechG27 profile; dev_flash/dev_hdd0 under the supplied install | Configure CPU/GPU and per-title overrides through versioned YAML handling; keep virtual filesystem, firmware, installed-title data and saves separate. A G27 profile file does not establish which hardware is connected. |
| Eden | `%APPDATA%/eden/config/qt-config.ini` with Controls, Data Storage, Core, Cpu and Renderer sections | Map per-player devices and persistent NAND/SD/user paths after validating the pinned release's portable/data-root behavior. Keep keys, account tokens and firmware outside the shared catalog. |

The current PCSX2 settings layout and version checks are documented in its [v2.8.2 Qt host source](https://github.com/PCSX2/pcsx2/blob/v2.8.2/pcsx2-qt/QtHost.cpp). Dolphin's [user-directory guide](https://dolphin-emu.org/docs/guides/controlling-global-user-directory/) describes its supported relocation model. Eden links its [user documentation](https://git.eden-emu.dev/eden-emu/eden) from the official website. RPCS3's [quickstart](https://rpcs3.net/quickstart) is the firmware/setup starting point.

For automatic settings, use a baseline per emulator/version, a hardware/controller profile, then narrowly scoped per-game overrides. Probe GPU capabilities and controller identities locally; keep device IDs out of shared templates. Start from upstream CPU timing defaults and native resolution until a measured profile is approved. The reference RTX 3070 Ti laptop is a test target, not a guarantee for every RPCS3 title. Wheel/pedal/controller models and desired button mappings are still needed before finalizing bindings.

Game compatibility patches need a separate manifest keyed by exact platform title/serial, region, revision/update, game hash and emulator version. Store patch source, license/evidence, checksum, expected target and rollback information. Apply supported emulator patch formats to isolated per-game profiles; keep original imports unchanged. Validate game-specific fixes independently from widescreen/60-FPS/graphics modifications. RPCS3 installed titles and Switch update/DLC storage need inventory and removal rules before genre switching can manage them safely. No generic patch or hardware-performance preset is enabled in this milestone.
