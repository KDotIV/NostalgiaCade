# Local installation and play

The Go core owns imports, authorized raw-file downloads, managed genre switching and Dolphin sessions. The desktop lists installed games and provides Play/Exit. Storage setup and collection administration use the operator CLI while the UI design evolves.

## First launch

```powershell
./scripts/Run.ps1
```

This builds the app and runs `ncade setup --if-needed` before opening the desktop. Launching the desktop executable directly also opens the console guide when unconfigured. Canceling leaves a setup-required state; no content is installed.

The guide first asks how to organize storage:

1. **One root folder:** enter an absolute local path such as `C:/.ncade` or `D:/ROMS.bk/ncade`. Setup creates `Emulators`, `ROMs`, `Firmware`, `Saves`, `Settings`, and `Cache` subfolders there.
2. **Individual folders:** choose each of those six paths separately, including on different local drives.

No content location is preselected. The guide validates the paths, shows the resolved layout, then asks for confirmation. Conflicts identify both folders and let you retry in the same guide. Content folders cannot equal or contain one another; sharing a parent is allowed. Network shares, drive roots and links/junctions are rejected. Each writable content folder receives a cabinet ownership marker.

The application database, log and connection files stay in `%LOCALAPPDATA%/NostalgiaCade` unless `NCADE_DATA_DIR` or the CLI's `--data-dir` selects another location. The guide displays that location too. It may be the parent of the six content folders, but it cannot be inside any of them. For a new cabinet with **application state and content all under one root**, set the state location before starting, then choose option 1 and enter that same root:

```powershell
$env:NCADE_DATA_DIR = 'C:/.ncade'
./scripts/Run.ps1
```

Use the same `NCADE_DATA_DIR` on subsequent launches and for both the desktop and CLI. Changing it selects a different cabinet database; it does not relocate an existing installation.

For scripted setup, supply a root directly, or edit the [root example](../examples/setup-root.example.json) or [individual-path example](../examples/setup.example.json):

```powershell
./artifacts/bin/ncade.exe setup --root 'D:/ROMS.bk/ncade'
# Alternative: choose paths and download hosts in a JSON file.
./artifacts/bin/ncade.exe setup --config ./my-setup.json
./artifacts/bin/ncade.exe paths
```

An empty `downloadHosts` list disables downloads. Add exact reviewed distribution hostnames when needed. With `--root`, use `--download-host host.example.org` (repeat or comma-separate for multiple hosts); omitting it preserves the existing allowlist. Setup can update this allowlist while keeping the existing paths. Established paths cannot be silently reassigned: relocation needs a future workflow that moves data and inventory together. Do not edit ownership markers to simulate a move.

## Register Dolphin

Place your reviewed Dolphin distribution, including its supporting files, inside the chosen emulators folder:

```powershell
./artifacts/bin/ncade.exe emulator add --id dolphin --exe 'D:/Cabinet/Emulators/Dolphin/Dolphin.exe' --version 'YOUR-REVIEWED-VERSION'
```

The core pins the executable's SHA-256. Changes require deliberate re-registration before launch. Alternatively, `ncade emulator install --id dolphin` downloads a reviewed official package and registers it automatically. Package installation also supports DuckStation, PCSX2, RPCS3 and Eden; only Dolphin has game launch support currently. See [emulator installation, catalog administration and configuration research](emulators.md). Vendor installers and firmware are not executed or distributed.

The adapter uses batch/exec/user switches and temporary command-line configuration, without rewriting controller settings. GameCube memory cards/GCI saves and Wii NAND/SD data use the chosen saves root. Dolphin's user directory uses `settings/<profile-id>`; native state saves and caches that Dolphin keeps there also remain persistent. Both roots survive switches. Existing external Dolphin profiles/saves are not automatically moved or merged. Configure Dolphin input against the same user directory.

HLE and skipped GameCube boot-ROM startup mean this raw-file flow needs no external BIOS. The firmware folder is preserved for later adapters. Installed WAD titles and their NAND cleanup are not supported; Wii NAND/system data stays with the protected saves.

References: [Dolphin command-line options](https://github.com/dolphin-emu/dolphin/blob/master/Source/Core/UICommon/CommandLineParse.cpp), [user directory](https://dolphin-emu.org/docs/guides/controlling-global-user-directory/), [save-path configuration](https://github.com/dolphin-emu/dolphin/blob/master/Source/Core/Core/Config/MainSettings.cpp).

## Import or download

```powershell
./artifacts/bin/ncade.exe import --path 'E:/Imports/Game.rvz' --genre racing --title 'My game'
./artifacts/bin/ncade.exe import --path 'E:/Imports/Homebrew.dol' --genre platform --reference
./artifacts/bin/ncade.exe games
```

Imports copy and verify a single regular file; `--reference` leaves it in place. Both create **persistent local entries** independent of the active genre. Local copies and referenced originals survive switches. No import uploads to PostgreSQL or creates a canonical shared-catalog record.

Supported raw files: `.iso`, `.gcm`, `.rvz`, `.wbfs`, `.ciso`, `.gcz`, `.dol`, `.elf`. Archives, playlists, split files and installed WADs need additional adapter work. Extract approved archives outside the managed library before importing.

For a download, edit [the entry example](../examples/download-entry.example.json) with real size/hash/source/permission evidence, set `authorized` after review, and approve its source/redirect hosts in setup:

```powershell
./artifacts/bin/ncade.exe download --entry ./approved-entry.json --genre platform
```

Examples contain placeholders, not real distribution permission. Authorization records an operator assertion; a hash or free download does not establish permission. These local manifests do not replace the future Sengoku-Admin shared-catalog publication service.

The downloader requires verified HTTPS, approved initial/redirect hosts, exact size and SHA-256. URL credentials, proxies and private/loopback destinations are disallowed. It stages one file at a time beside its destination, then renames after verification. Transport/server failures receive bounded retries; integrity failures stop. Partial transfers restart rather than using unvalidated HTTP ranges. The ROM volume keeps a 1 GiB free-space reserve, checked before and during writing.

## Remove-first genre switching

Edit [the collection example](../examples/genre.example.json). Each entry needs a unique ID, registered emulator profile, filename, expected size/hash and a local or authorized download source. For local metadata, use `Get-FileHash -Algorithm SHA256` and `(Get-Item -LiteralPath $path).Length`; hashes must be lowercase. The prototype accepts 1–100 entries; the curated product target remains 20 per genre.

```powershell
./artifacts/bin/ncade.exe genre plan --manifest ./racing.json
./artifacts/bin/ncade.exe genre switch --manifest ./racing.json
./artifacts/bin/ncade.exe genre switch --manifest ./fighting.json --yes
```

The plan shows revision, replaced suite entries, reclaimable/required bytes and retained artifacts. `--yes` explicitly accepts the removal plan for automation. The core refuses stale plans, simultaneous mutations and switching during a game.

Order: validate inputs/budget → journal intent → mark the old suite unavailable → detach old suite records → remove unneeded inventoried game files → acquire/verify the target → activate. There is no hidden backup of the previous ROM collection. Shared target artifacts and persistent local entries are retained. Cleanup removes individual owned files, never entire genre, emulator, firmware, save or settings folders.

After removal, failure/cancellation can leave the collection in maintenance; it cannot promise rollback. Persistent local entries remain intact. Use the operation ID printed by the CLI:

```powershell
./artifacts/bin/ncade.exe operation show --id OPERATION_ID
./artifacts/bin/ncade.exe operation retry --id OPERATION_ID
./artifacts/bin/ncade.exe operation cancel --id OPERATION_ID
```

Closing the CLI does not cancel core work. Restart marks interrupted operations for explicit retry; completed transfers can be reused. The core cleans partial names only when tied to recorded operations. Missing source media can still prevent completion. Changed inventoried files cause deletion to be refused. Do not manually edit the managed ROM `content` directory.

## Launch and exit

After CLI changes, Refresh the desktop, select a genre/game, and Play. The app checks session state every two seconds and requests focus when the game exits. F11 toggles full screen.

```powershell
./artifacts/bin/ncade.exe launch --game GAME_ID
./artifacts/bin/ncade.exe session
./artifacts/bin/ncade.exe stop
./artifacts/bin/ncade.exe stop --force  # Stuck sessions only; unsaved progress may be lost
./artifacts/bin/ncade.exe shutdown
```

Graceful exit sends `WM_CLOSE` only to windows owned by the tracked process. Forced exit uses its held process handle, never an executable-name search. Recovery checks Windows creation time as well as PID. Games survive a core crash, and the next core reconnects while keeping mutations blocked. An ambiguous interrupted launch requires the operator to verify the emulator is closed, then run `ncade session acknowledge --confirm-closed`.

Closing the desktop does not terminate games or operations. Core shutdown is rejected while either is active. Register the actual Dolphin executable; launchers that hand gameplay to another process are outside this adapter.

## Verification limits

`./scripts/Test.ps1` covers schema upgrade, path/source policy, remove-first ordering, retention, cancellation, integrity failure, retry/recovery, both CLI storage layouts and rendered game selection. A synthetic Win32 executable accepts Dolphin-shaped arguments and tests import, graceful exit and core-crash reconnection with a persistent save in both layouts, including application state inside the common root. It is not a claim of real Dolphin compatibility or performance.

Physical Dolphin/content/peripheral testing, gamepad focus behavior, signed remote manifests/authentication, more launch adapters, game-archive extraction, range resume, path relocation and installer packaging remain subsequent work. The multiplayer server remains the final feature milestone. Five emulator package records and their download URLs have been published to Azure through the separate admin tool.
