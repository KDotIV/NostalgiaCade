# Go desktop core proposal

Status: design, November 10, 2026. This is the product's local orchestration layer, independent of either C# interface.

Implementation status: the initial local workflow subset is implemented; see [the operator guide](operator-cli.md) for actual behavior and limits. The broader module, archive, hardware and multiplayer capabilities below remain target architecture.

## Responsibilities and process model

Run one Go core per cabinet library in the interactive user session. The Avalonia application starts or reconnects to it. Use a single-instance lock, a per-launch named pipe restricted to the intended Windows user, and an explicit UI/core protocol handshake. Do not expose an unauthenticated localhost server.

The core owns all local durable state and mutations. C# sends commands and renders snapshots/events; it does not write SQLite, generate emulator settings, delete game folders or execute installers directly. Keep the core responsive by running long work through bounded workers with cancellation and timeouts.

A dropped interface connection is not a failed operation. After a process or machine restart, reconcile the job journal and filesystem before accepting another mutation. Define a clean shutdown that checkpoints work; do not silently kill an active game because an operator closes a window.

## Modules

| Module | Responsibility |
| --- | --- |
| Catalog cache | Store verified published manifests and required guides for offline use |
| Inventory | Record installed artifacts, ownership, file paths, hashes and references |
| Planner | Resolve the target dependency graph, hardware policy, source availability and disk forecast |
| Operations | Serialize local/remote requests, persist steps and reconcile interruptions |
| Downloads | Bounded parallelism, resume where supported, retry/backoff and integrity checks |
| Packages | Validate/extract supported archives, reject unsafe paths and track installed files |
| Emulator adapters | Prerequisites, configuration, content validation, launch/exit and save locations |
| Hardware adapters | Probe supported devices and guide vendor setup/calibration |
| Compatibility | Evaluate hard requirements and evidence-based performance profiles |
| Fleet client | Enroll, authenticate, poll commands and report actual operation state |
| Local library | User-added entries and file ownership, independent of Sengoku-Admin's shared catalog |
| Venue multiplayer client | Local coordinator pairing, verified profiles, session reservations, input allocation and adapter lifecycle; final MVP feature milestone |

Begin with one tested emulator adapter in the first integration milestone, then expand to the catalog's actual requirements. The finished MVP still targets all five genres with 20 curated entries each.

## Local persistence

SQLite stores `installed_artifacts`, `artifact_files`, `artifact_references`, `active_suite`, `operations`, `operation_steps`, `download_checkpoints`, `received_commands`, `guide_progress`, `hardware_profiles` and `settings`. A cached published manifest is immutable and addressed by its digest/revision.

Add `local_games` for user-added entries and `session_reservations` for local multiplayer locks. A local game may reference an existing published edition but does not insert or change remote `games` rows. The core exposes separate local-library commands; only the hosted Sengoku-Admin API can create canonical catalog entries. Store approved multiplayer profile caches and venue-pairing metadata locally for WAN-independent startup.

Keep filesystem paths and private import locations local. Store cloud device credentials using Windows-protected storage rather than plaintext SQLite. Local operator credential verification uses a salted password/PIN hash and rate limiting, not a hardcoded default PIN.

Persistent data has distinct ownership classes:

| Class | Genre-switch behavior |
| --- | --- |
| App-managed game content, including game-specific updates/DLC | Remove when no longer needed by the target suite |
| Shared game/BIOS dependencies used by the target | Retain |
| Emulators, system BIOS/firmware, vendor utilities | Retain; separate operator maintenance handles retirement |
| Saves, memory cards, input profiles and settings | Retain outside disposable directories |
| Shader/download caches | Bounded; reclaim by declared cache policy, not by deleting save trees |
| Original user-imported files outside the library | Never auto-delete |
| Local-only user installations | Retain on genre switches unless the user explicitly assigns managed ownership; include their storage in the disk forecast |

Default imports copy into the managed library or reference an external file read-only. Referenced originals are not owned content and cannot be counted as reclaimable space. Make this distinction visible before importing a large collection. A future explicit "move into managed library" action needs separate UX and is not assumed here.

## Genre-switch state machine

The user selected removal before download. Normal progression:

`requested -> planning -> waiting_for_idle -> removing_old -> acquiring -> verifying -> configuring -> ready -> completed`

An operation can enter `awaiting_operator`, `awaiting_reboot`, `retryable_failure`, `failed` or `canceled` where appropriate. Persist both the current phase and completed step identities. The active suite has independent readiness state; once its required content is removed it must no longer appear playable.

1. **Plan without deleting.** Verify the target manifest signature/schema, resolve pinned dependencies, check compatibility, validate required local imports and calculate space. Inspect download sources where possible; a successful preflight cannot guarantee a provider will stay available.
2. **Acquire the library lock.** Recheck the expected cabinet revision and storage; wait for any game session and multiplayer reservation to end. Reject stale plans rather than applying an obsolete local or remote request. An expired coordinator lease never overrides a locally running game's artifact lock.
3. **Journal intent.** Persist the exact removal/install set, savedata locations, target revision and recovery information. Back up small configuration files if needed; do not retain a hidden second copy of the old ROM library.
4. **Remove unneeded managed content.** Mark the old suite unavailable before removing required files. Delete only inventory-owned files inside the configured managed root, excluding shared target dependencies and all persistent data.
5. **Acquire the target.** Download missing artifacts in bounded batches or copy/import verified local files. Check declared lengths and digests; resume partial transfers only when source identity/validators still match. Refresh expiring URLs without changing artifact identity.
6. **Extract and configure.** Stage one bounded package at a time on the destination volume, validate it, then move it into its final managed location. Track extraction and installed files. Preserve personal settings unless an explicitly reviewed change is needed.
7. **Complete prerequisites.** Finish required vendor steps, BIOS imports, mappings and reboot checks. Never repeat a firmware flash simply because a journal entry was interrupted; re-detect device state first.
8. **Validate and activate.** Check the selected ready game set and dependencies. Update the active-suite pointer in a short SQLite transaction only after required file operations succeeded. Report the actual ready set to the server.

Filesystem operations and SQLite cannot form one ACID transaction. The journal, idempotent steps and startup reconciliation provide recovery across that boundary. Use same-volume staging where atomic renames are needed; do not imply arbitrary cross-volume moves are atomic.

After step 4, rollback may require redownloading or reimporting the previous genre. Canceling releases work and cleans owned temporary data, but does not promise immediate restoration. Removable import media or a dead upstream URL can make recovery require operator action.

## Disk accounting

Forecast peak usage, not just ZIP/download sizes. Include retained binaries/firmware, target expanded content, installed game data, save reservation, package staging, download cache, and an OS free-space reserve. Account for each affected volume separately.

Preflight must reject a target that cannot fit even after the planned removals. Recheck free space during extraction because other processes may consume it. Stop safely before crossing the reserve. Publish artifact expanded-size bounds and extraction limits; unknown sizes cannot be treated as zero.

Use dependency references rather than "delete every folder for the old genre." Games can belong to multiple genres, multi-disc titles have multiple files, and MAME sets may depend on parent/BIOS/device sets or CHDs. See [MAME's set documentation](https://docs.mamedev.org/usingmame/aboutromsets.html).

## File and executable safety

- Resolve and validate managed paths before deletion; guard against `..`, absolute paths, junctions, symlinks/reparse points and alternate path escapes. Reject unexpected path/ownership changes rather than deleting outside the approved inventory.
- Extract to owned staging directories with file-count and expanded-byte limits; never execute directly from an unverified archive.
- Require HTTPS, allowed source/redirect hosts, expected hashes and reviewed execution recipes. A matching hash checks integrity; trusted manifest signing and source review establish provenance.
- Pass process arguments as an argument vector. No shell concatenation, arbitrary remote scripts, or executable command strings in the catalog.
- Run normal work unelevated. Launch a known vendor installer with explicit operator consent when elevation is required. Keep administrator privileges out of the general core process.
- Recognize installed external utilities without claiming ownership of them. Genre cleanup never uninstalls a vendor driver or rolls back physical firmware.

## Emulator and rig adapters

Each adapter declares the supported emulator release range, exact executable, prerequisite checks, supported game formats, configuration schema, user-data directories and process exit behavior. Track child processes and only control the specific launched session. Avoid killing every process with a matching name.

RPCS3-like layouts need special handling because installed game data and persistent saves can share a larger emulator user directory. Preserve the adapter-declared save/firmware paths when removing only a title's disposable content. Do not implement cleanup as deletion of the emulator's entire data directory.

A rig profile contains vendor/product identifiers, supported utilities, detection capability, guide steps, axis/button mappings, steering range, dead zones and per-game feedback support. A detected wheel does not prove that every emulated game supports wheel input or force feedback. Implement and test support for the user's later device list; unsupported devices get a guided manual path, not a false success.

Hardware firmware changes remain vendor-supported, operator-assisted steps. Record version/detection evidence and any reboot requirement. The genre-switch recovery model does not claim to undo firmware changes.

## Local multiplayer integration

Same-cabinet couch/split-screen play uses local input mappings and does not depend on the hosted API or venue coordinator. The final MVP multiplayer milestone adds a separate local venue host plus mode-specific cabinet adapters; the core delegates the actual game protocol to the selected emulator/service implementation.

Verify local venue identity, exact game edition and content, emulator/service/profile revisions, network policy, controller slots and save ownership before participating in a session. Reserve the cabinet and its artifacts before launch; prevent simultaneous sessions and conflicting genre changes, updates or save resets. Where a peer needs a missing game, require an explicit installation plan rather than transferring content from another cabinet or silently switching its genre.

A local-only game can use a matching approved profile after its required identity/configuration checks. An unmatched local game remains available for normal local use but is not automatically granted shared-catalog status, a verified network profile, arbitrary server execution or public network access. Local library ownership is not a way to install unreviewed network services through the venue coordinator.

For synchronized couch play, default to the nominated host's shared save and write back only to that owner when the supported mode ends cleanly. Preserve other cabinets' saves. Backend-owned progress uses that service's local storage policy; do not merge it with cabinet saves without an explicit adapter. Keep safe local backups around setup/writeback. Genre cleanup cannot delete pending save recovery data.

Allow only profile-declared LAN listeners/peers and verify both IPv4/IPv6 policy. Do not allow an emulator to announce a public lobby, request automatic router mappings, select a public relay or fall back to external authentication. Detect/report unsupported external dependencies. LAN session failure must not interrupt unrelated local-only gameplay; reconnect/host migration is conditional on verified adapter support. See [venue multiplayer](multiplayer.md) for topology and acceptance criteria.

## Requirements and performance

Separate three questions: can the emulator start, is this game compatible with this emulator release, and does this hardware/configuration sustain the intended gameplay speed? CPU/GPU names alone cannot answer all three.

Hard requirements can include architecture, instruction-set support, graphics API support, OS version, RAM and storage. A missing hard requirement blocks launch. Performance predictions use verified profiles with game edition, emulator version, render settings, hardware details, evidence date and confidence. Unrecognized hardware yields "unverified," not a fabricated score.

The user's reference laptop is the upper validation profile. Curate games/settings that work there; establish lower profiles through testing rather than an arbitrary universal minimum. Preserve each title's intended frame rate and full emulation speed instead of requiring 60 FPS from every game. PCSX2 explicitly notes per-game variation and the effect of graphics settings: [official requirements](https://pcsx2.net/docs/setup/requirements/).

## Failure scenarios to verify during implementation

Power loss in each destructive/install phase; network failure after old-content removal; duplicate remote delivery; two conflicting operators; malicious archive paths; checksum mismatch; disk exhaustion; locked files; absent removable imports; emulator crash; controller unplug/replug; pending reboot; UI/core version mismatch; and a save directory colocated with game data.

Also verify simultaneous joins, incompatible peer editions/settings, unavailable venue host, host loss/desync, reservation expiry while a game still runs, multiple same-game save writers, an update during a linked session, preservation of local-only installations, external-network denial and prevention of local-library imports from writing the shared catalog.
