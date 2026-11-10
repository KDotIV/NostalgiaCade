# Emulator installation and configuration

Verified November 10, 2026. The diagnostic cabinet is `C:\.ncade`. Five official Windows x64 packages were downloaded, hashed, extracted there and recorded in its SQLite inventory. The same five releases and their source URLs were published to the Azure `cade-archives` catalog and read back successfully. Original emulator folders in Documents were inspected without modification.

## Reviewed releases

| Emulator | Selected release | Official downloads | Current NostalgiaCade support |
| --- | --- | --- | --- |
| DuckStation | Rolling package updated November 7, 2026 | [Windows downloads](https://www.duckstation.org/windl) | Isolated portable profile, BIOS import, CHD/PBP/homebrew launch and exit |
| PCSX2 | v2.8.2 stable | [Release](https://github.com/PCSX2/pcsx2/releases/tag/v2.8.2) | Explicit data root, BIOS import, disc/homebrew launch and exit |
| Dolphin | 2609b release | [Downloads](https://dolphin-emu.org/download/) | Package installation, profile registration, game launch/exit and persistent save paths |
| RPCS3 | 0.0.43-20581, fd027c99 | [Official Windows builds](https://github.com/RPCS3/rpcs3-binaries-win/releases) | Isolated config/VFS, local firmware onboarding, referenced EBOOT/ELF/SELF launch and exit |
| Eden | v0.2.1 stable, x64 Clang PGO | [Official downloads](https://eden-emu.dev/downloads/) | Isolated portable profile, local keys/NCA firmware onboarding, NSP/XCI/NRO launch and exit |

Eden is the selected Switch candidate for this pilot. Its official site provides a maintained release channel, documented requirements, and recommends PGO builds when available. The supplied Eden folder already contains the same stable version using the MSVC build; the catalog chooses the upstream-recommended PGO variant. The old Citron website returned no usable release content during verification, and the supplied Citron 0.11.0 is not evidence of a current maintained release. This selection is an integration recommendation, not a measured claim that Eden performs best for every game. Benchmark representative titles before promoting a cabinet profile. See [Eden release notes](https://git.eden-emu.dev/eden-emu/eden/releases) and [requirements](https://eden-emu.dev/system-requirements/).

DuckStation's supplied and upstream licenses are **CC-BY-NC-ND-4.0**, with a noncommercial restriction. It is cataloged and installed for prototype investigation; the catalog explicitly records that commercial venue deployment needs licensing review/permission. Catalog publication does not approve redistribution or game rights. See the [upstream license](https://github.com/stenzek/duckstation/blob/master/LICENSE).

The machine has the Microsoft Visual C++ x64 runtime registered as v14.51.36247.00. Runtime installation, firmware onboarding, real gameplay and peripheral testing are distinct from archive installation. Onboarding accepts personally supplied local BIOS, console keys and extracted firmware; none is downloaded or added to the shared catalog. Existing emulator settings and save files are not automatically migrated.

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

`launchSupported` means an adapter exists, not that a title, controller, firmware or machine is verified. All five packages register a profile; SQLite migration 005 also registers the four previously installed packages. Run `emulator onboard --id <id>` or the commands below before first launch. Use `emulator configure` to select the isolated profile; directly opening a package executable can select a different user-data directory.

## BIOS/firmware onboarding and settings

```powershell
./artifacts/bin/ncade.exe --data-dir C:/.ncade emulator onboard --id pcsx2
# Scriptable equivalents; prepare without --apply is a read-only path/prerequisite preview.
./artifacts/bin/ncade.exe --data-dir C:/.ncade emulator prepare --id pcsx2
./artifacts/bin/ncade.exe --data-dir C:/.ncade emulator prepare --id pcsx2 --apply
./artifacts/bin/ncade.exe --data-dir C:/.ncade emulator firmware --id pcsx2 --kind bios --path 'E:/My dumps/PS2 BIOS.bin'
./artifacts/bin/ncade.exe --data-dir C:/.ncade emulator configure --id pcsx2
./artifacts/bin/ncade.exe --data-dir C:/.ncade stop
./artifacts/bin/ncade.exe --data-dir C:/.ncade emulator status --id pcsx2
```

The guide previews paths, creates missing files after confirmation, accepts optional local prerequisite paths, and opens native settings for controller bindings and graphics/CPU configuration. `prepare --apply` provides the noninteractive equivalent. Files already present are preserved; conflicting storage routing produces `profile_conflict`, with the file path to review. NCade does not serialize or reset unknown settings. Do not change storage routing in native settings: game launch refuses a changed route until it is restored. Use a separate profile for an intentional new layout.

For RPCS3, complete its first-run welcome screen and clear **Show at startup** before unattended game launching. The native welcome dialog can otherwise appear even with `--no-gui`. NCade leaves that operator preference under native UI control. First boot of a game may compile PPU/SPU modules and shaders; a running process alone is not evidence that gameplay has started.

For cabinet fullscreen protection, `scripts/Set-Rpcs3CabinetControls.ps1 -DataDirectory C:/.ncade` backs up the isolated GUI INI and enables `GSFrame.disableKbHotkeys` and `GSFrame.disableMouse`. The installed revision reads these in [gs_frame.cpp](https://github.com/RPCS3/rpcs3/blob/fd027c99b8824e9a9b6f5b2d5ae33f9202e29da3/rpcs3/rpcs3qt/gs_frame.cpp); their names/defaults are in [gui_settings.h](https://github.com/RPCS3/rpcs3/blob/fd027c99b8824e9a9b6f5b2d5ae33f9202e29da3/rpcs3/rpcs3qt/gui_settings.h). These settings disable emulator keyboard shortcuts and mouse fullscreen toggles without changing render resolution. Use NCade's independent return control to change games. This does not prevent Windows shortcuts or replace future OS kiosk lockdown.

Eden can show a native exit confirmation during a game even though its empty configuration window closes immediately. `ncade stop` means the request was sent; use `session` to confirm actual exit. NCade preserves this setting and never assumes that accepted means stopped.

| Adapter | Local onboarding | Persistent routing |
| --- | --- | --- |
| DuckStation | `--kind bios --path <512 KiB or 1 MiB BIOS>` | BIOS → `Firmware/<id>/bios/bios.bin`; cards/states → `Saves/<id>` |
| PCSX2 | `--kind bios --path <4 MiB BIOS>` | Same BIOS location; cards/states → `Saves/<id>`; `-datapath Settings/<id>`; v2.8.2 appends `PCSX2`, so configuration is `Settings/<id>/PCSX2/inis/PCSX2.ini` |
| RPCS3 | `--kind ps3-flash --path <dev_flash folder>` or `configure --id rpcs3 --firmware <PS3UPDAT.PUP>` | `RPCS3_CONFIG_DIR` → `Settings/<id>/`; VFS flash → `Firmware/<id>`, HDD0 → `Saves/<id>`, HDD1 → `Cache/<id>` |
| Eden | `--kind switch-keys --path <prod.keys>` and `--kind switch-firmware --path <flat NCA folder>` | Firmware NAND → `Firmware/<id>/nand`; save NAND and SDMC → `Saves/<id>`; keys retained in Firmware and copied into the isolated portable key directory |

Firmware imports validate structure/size, reject links, bound total size/file count, hash copies and preflight **all** destination conflicts before publishing. Identical imports are idempotent. Different existing files are never replaced. RPCS3's native PUP installer is a tracked, interactive first-install session; it performs actual PUP verification/decryption and refuses to replace an already installed flash through this command. Close it before continuing. The PUP route needs an operator-supplied official update file; NCade does not download it. Presence/size checks are prerequisites, not proof of firmware authenticity or compatibility. Eden NRO homebrew can run without console keys/firmware; encrypted games require them.

DuckStation's current Windows build has no user-directory CLI flag; Eden's Windows build uses an adjacent `user` folder. Both therefore get a verified runtime copy under `Settings/<id>/runtime`, with licenses/resources preserved. This consumes one extra unpacked runtime per profile, avoids junctions and leaves original Documents/AppData profiles untouched. Firmware and save routing are configured before startup. Their runtime version is pinned: an in-app update or changed package causes verification to stop; automatic profile upgrades/migration are not implemented. PCSX2 and RPCS3 run from their package directories with explicit data-root selection. Configuration sessions share the same lock, PID identity, graceful stop and crash recovery as game sessions.

RPCS3's HDD0 includes saves **and native installed title data**. NCade preserves that entire directory. Installed PKGs, Switch updates/DLC, multi-file PS1 CUE/BIN sets, playlists and multi-disc management are outside managed genre cleanup. Use CHD/PBP for PS1; PS3 folder games require reference imports pointing at their boot file. No source game, BIOS, key or firmware folder is moved or removed.

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

The connection string is not set in `appsettings.json`, a repository `.env` file, or the emulator catalog. `scripts/Set-CatalogConnection.ps1` constructs it from the supplied host/database/user and hidden password prompt, then sets the process environment; `-PersistForUser` also stores it in the Windows user environment (`HKCU\Environment`). `cmd/ncade-catalog/main.go` and the PostgreSQL branch of `cmd/ncade-migrate/main.go` read `os.Getenv("NCADE_POSTGRES_DSN")`. The cabinet keeps its inventory in `<data-dir>/cabinet.db` and consumes the bundled/reviewed catalog export; it does not continuously connect to Azure. The remote read-only inspection on November 10, 2026 succeeded against `cade-archives` and returned five emulator packages. Remote migration history is still absent, so no migrations were run there during emulator validation.

Publishing validates the entire catalog and uses one transaction with idempotent identifiers. Existing release versions cannot silently change artifact hashes. Data goes to `catalog.emulators`, `catalog.emulator_releases`, `catalog.artifacts`, `catalog.artifact_sources`, and `catalog.emulator_packages`. Permission status remains `unreviewed`; download provenance and deployment permission are distinct. The subject is audit attribution, not an authorization bypass: database-role privileges authorize this admin tool. The future authenticated API must enforce Sengoku-Admin publication and serve signed manifests to cabinets.

The inspected Azure database already has the catalog/package tables, alongside separately created `public` tables, but **no `public.schema_migrations` history**. No DDL or migration-history adoption was performed there. Do not run the fresh-database migration command against that database until its manually applied schema has been reconciled with a verified baseline. Local SQLite remains the cabinet's inventory; remote PostgreSQL remains catalog/fleet data. A cabinet can install from an exported catalog offline using verified local archives, without a PostgreSQL connection.

For a fresh PostgreSQL database, the existing migration runner applies `001` through `004`. Automated tests use a disposable local server and cover migration constraints, repeated publication, export and rollback on conflicting releases. They never target `NCADE_POSTGRES_DSN`.

## Reviewing updates

`scripts/Refresh-EmulatorCatalog.ps1` discovers releases from upstream APIs/pages, downloads archives into ignored `artifacts/catalog-review`, checks GitHub-provided digests where available, and measures size/hash. It writes a candidate `windows.json`; it does not publish or change a cabinet. Dolphin's website may require a browser: pass `-DolphinReleaseURL` with a link independently verified on its official download page. Dolphin and Eden digests in this snapshot were measured from downloads over verified HTTPS; they are not claimed to be upstream signatures.

Review extracted contents, license changes and compatibility before replacing the embedded snapshot or publishing a candidate with `ncade-catalog --action publish --file ...`. Upstream license notices remain in installed archives. Do not use an unreviewed URL or silently upgrade a working suite to a nightly build.

## Configuration and patch research

Local inspection established the configuration surfaces below. Current adapters create only storage routing and first-run markers; tuning and game-specific patches remain operator-controlled in native settings.

| Emulator | Evidence found | Proposed integration |
| --- | --- | --- |
| DuckStation | `settings.ini` with CPU/GPU, BIOS, MemoryCards, Folders, Pad1–8 and Hotkeys sections | Isolated portable runtime; BIOS, cards, states and cache have explicit paths; native settings manage tuning. |
| PCSX2 | Supplied path has `PCSX2_ui.ini`, `PCSX2_vm.ini`, GSdx and LilyPad plugin settings; no executable | Treat this as legacy user data. Current Qt PCSX2 loads `inis/PCSX2.ini` and checks settings version. Build an explicit migration preview for bindings/cards; do not paste legacy plugin files into the new installation. |
| Dolphin | Actual Config folder under `%APPDATA%/Dolphin Emulator`, including Dolphin.ini, GFX.ini, GCPadNew.ini and WiimoteNew.ini | Existing adapter already selects cabinet user/save locations. Add an explicit profile-import preview and controller onboarding, preserving unknown INI keys. |
| RPCS3 | `config/config.yml`, patch_config.yml, input-related files and a LogitechG27 profile | Generated `config/vfs.yml` routes persistent storage; native settings manage CPU/GPU/controllers. Existing G27 settings are preserved in the original installation. |
| Eden | `config/qt-config.ini` with Controls, Data Storage, Core, Cpu and Renderer sections | Isolated portable user folder with separate firmware/save NAND paths; no account tokens are copied. |

The current PCSX2 settings layout and version checks are documented in its [v2.8.2 Qt host source](https://github.com/PCSX2/pcsx2/blob/v2.8.2/pcsx2-qt/QtHost.cpp). Dolphin's [user-directory guide](https://dolphin-emu.org/docs/guides/controlling-global-user-directory/) describes its supported relocation model. Eden links its [user documentation](https://git.eden-emu.dev/eden-emu/eden) from the official website. RPCS3's [quickstart](https://rpcs3.net/quickstart) is the firmware/setup starting point.

Launch/storage contracts were checked against [DuckStation's Qt host](https://github.com/stenzek/duckstation/blob/master/src/duckstation-qt/qthost.cpp) and [data-root code](https://github.com/stenzek/duckstation/blob/master/src/core/core.cpp), [PCSX2 CLI documentation](https://pcsx2.net/docs/advanced/cli/), [pinned RPCS3 CLI](https://github.com/RPCS3/rpcs3/blob/fd027c99b8824e9a9b6f5b2d5ae33f9202e29da3/rpcs3/rpcs3.cpp) and [configuration-directory code](https://github.com/RPCS3/rpcs3/blob/fd027c99b8824e9a9b6f5b2d5ae33f9202e29da3/Utilities/File.cpp), and Eden v0.2.1's [CLI](https://git.eden-emu.dev/eden-emu/eden/src/tag/v0.2.1/src/yuzu/main_window.cpp), [path selection](https://git.eden-emu.dev/eden-emu/eden/src/tag/v0.2.1/src/common/fs/path_util.cpp), and [storage settings](https://git.eden-emu.dev/eden-emu/eden/src/tag/v0.2.1/src/frontend_common/config.cpp).

For automatic settings, use a baseline per emulator/version, a hardware/controller profile, then narrowly scoped per-game overrides. Probe GPU capabilities and controller identities locally; keep device IDs out of shared templates. Start from upstream CPU timing defaults and native resolution until a measured profile is approved. The reference RTX 3070 Ti laptop is a test target, not a guarantee for every RPCS3 title. Wheel/pedal/controller models and desired button mappings are still needed before finalizing bindings.

Game compatibility patches need a separate manifest keyed by exact platform title/serial, region, revision/update, game hash and emulator version. Store patch source, license/evidence, checksum, expected target and rollback information. Apply supported emulator patch formats to isolated per-game profiles; keep original imports unchanged. Validate game-specific fixes independently from widescreen/60-FPS/graphics modifications. RPCS3 installed titles and Switch update/DLC storage need inventory and removal rules before genre switching can manage them safely. No generic patch or hardware-performance preset is enabled in this milestone.

## Validation on November 10, 2026

- Go vet/unit tests pass; .NET build has zero warnings/errors; all 16 integration/UI/control tests pass. Win32 fixtures exercise onboarding, launch, configuration sessions, argument boundaries, environment filtering, graceful exit and sequential game replacement. Additional checks cover stale-session stop rejection and held controller gestures. Focused desktop/control checks also pass after the final dialog layout adjustment.
- `C:\.ncade` was upgraded to local schema 005 after saving a database backup in `Diagnostics/adapter-onboarding-20261110`. All four new profiles were prepared. Personally supplied PS1/PS2 BIOS, the existing RPCS3 `dev_flash`, and Eden keys/firmware were copied into their isolated destinations. All four report structurally ready prerequisites.
- Final hash comparison of 490 pre-existing settings/save files found zero changes or missing files. Both selected game source hashes are unchanged. Two local reference imports were added from `D:\ROMS.bk`; no managed game copies or genre removals were performed. New NCade profiles retain emulator-created UI metadata/caches; original Documents/AppData profiles remain untouched. Before-settings backups and reports are under `C:\.ncade\Diagnostics\runtime-validation-20261110`.
- DuckStation started from the portable runtime and exited gracefully with code 0. PCSX2's native probe exposed its appended `PCSX2` data subdirectory; the adapter and fixture were corrected. Its subsequent log confirmed startup, graphics/controller detection, and the routed BIOS/cards/state settings. The first probe's generated files were preserved in the diagnostic folder.
- A background-desktop launch issue was fixed: Windows process creation explicitly selects `winsta0\default` and a visible startup state, with PID-scoped graceful closing on that desktop. RPCS3 and Eden configuration sessions subsequently displayed their main windows and exited with code 0.
- RPCS3 0.0.43-20581 launched **The King of Fighters XII [BLUS30353]** from its referenced EBOOT.BIN, using Vulkan. The user confirmed shader precompilation followed by the main menu; graceful exit returned 0. A later live test confirmed Esc, Alt+Enter and double-click no longer leave fullscreen with cabinet controls enabled, and Ctrl+Alt+Home opens NCade. Controls/gameplay performance and an in-game save/reload remain unverified for this title.
- Eden v0.2.1 launched **Unicorn Overlord v1.00 [010069401ADB8000]** using the isolated profile and firmware 22.1.0. The user confirmed the title/menu and responding controls. Two exits returned 0 after native confirmation, but both logged a guest `Userspace PANIC`. A subsequent real NCade UI switch successfully closed RPCS3 and launched Eden; this Eden run returned **0xC0000005** after exit confirmation. Thus Eden boot/menu/control checks pass, but shutdown reliability remains **failed/incomplete**, requiring investigation before cabinet qualification. No automatic force-kill was used; logs for each run were preserved. An exit code of 0 alone is not proof of an error-free guest shutdown.
- The final test session is closed. Window screenshot capture repeatedly timed out; visual results above were confirmed by the user, supplemented by process/session/log evidence. Computer Use was stopped with Escape and no further UI automation followed. Actual controller/wheel mappings, generic pause/background-input isolation, PUP installation, sustained gameplay/performance and save/reload round trips remain unverified. Multiplayer remains the final feature milestone.
