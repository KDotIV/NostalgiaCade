# Frontend and UX proposal

Status: design, November 10, 2026. Recommended implementation: Avalonia for cabinets and standalone Blazor WebAssembly for the central dashboard. Core policy remains in Go.

## Players, operators and catalog administrators

Players need a fast, controller-friendly launcher that returns reliably after a game closes. Operators need installation, calibration, diagnostics and fleet controls. Keep operator features behind an authenticated operator mode. Local PIN access is an application-level control; deployed cabinet lockdown is a separate OS configuration.

No public player accounts, payments or timed paid sessions are included in the MVP. Protocol-specific identities required by a local multiplayer service are provisioned within the venue and do not introduce guest cloud logins. Guests share cabinet saves and unlocks, retained across genre changes. An authenticated operator can reset a selected game's progress through a separate, explicit action. Show what will be reset and offer a backup before deletion; changing genre never triggers that reset.

Only Sengoku-Admin sees shared-catalog creation/publishing controls, with authorization enforced by the API as well. A user's local-library flow can add an installed game without a Sengoku-Admin role; that entry stays on the cabinet and is clearly labeled local-only. It does not become shared catalog content or gain a verified multiplayer badge automatically.

## First installation

1. **Welcome and cabinet registration.** Identify the cabinet and venue; enroll through an expiring code approved by a signed-in operator. Set up the local operator credential and recovery process.
2. **System check.** Show detected OS, CPU, GPU, memory, free storage and connected input devices. Report unsupported capabilities separately from uncertain performance.
3. **Storage.** Choose the managed library location and maximum content budget. Show that replacing a genre removes its old managed games before the new download starts. Explain that original imported files are preserved and can still occupy disk space.
4. **Choose a genre.** Five genre cards, each with 20 planned titles. Show eligible, missing-import and unvalidated counts, approximate download size, installed size and hardware needs.
5. **Content and prerequisites.** Identify downloadable authorized content, operator-supplied game files, BIOS/firmware imports and required utilities. Show missing items before removing an existing genre.
6. **Review installation.** List games, retained dependencies, files to remove, disk forecast, external vendor steps and any reboot requirement. A remote request gets the same reviewed plan in the central dashboard.
7. **Hardware setup.** For racing, select a supported rig profile, connect devices, install the official vendor utility where needed, calibrate steering/pedals and validate the emulator mapping. Let the operator use a standard controller profile if compatible.
8. **Install and validate.** Show per-step progress, remaining bytes, resume/cancel state and a short first-launch check. Save wizard progress across restart/reboot.
9. **Ready for players.** Enter the launcher only when selected games and required setup checks are ready. If some catalog games lack imports, show the operator the exact available subset; do not label all 20 installed.

Hardware steps may occur before or during content installation as their dependencies require. The operator remains in the same resumable onboarding flow. Firmware installation uses the vendor's supported workflow and may require hands-on action; do not promise silent universal rig setup.

## Cabinet screens

| Screen | Main behavior |
| --- | --- |
| Player library | Active genre, ready games, local multiplayer indicators, clear Play action |
| Game details | Controls, player count, tested profile and permitted launch action |
| Operator home | Current operation, storage, setup health, network state and active suite |
| Genre catalog | Preview the five collections and compare content/storage requirements |
| Installation review | Exact removal/install plan and expected downtime before starting |
| Operation progress | Removal, download, verification, extraction and configuration as distinct steps |
| Hardware setup | Device detection, input tests, guide progress and per-game mapping profiles |
| Recovery | Resume, retry source, provide missing import, or choose another suite |
| Settings/support | Operator access, enrollment, library location, update policy and redacted support export |
| Local games | User-added local-only entries, optional association to existing catalog editions, explicit file ownership |
| Venue play | Verified same-network sessions, participating cabinets, readiness and controller slots |
| Local multiplayer setup | Pair the cabinet with the venue host, verify local dependencies and show service health |

Player mode never exposes shell paths, provider URLs, database settings or driver installers. Operator screens use plain descriptions first, with expandable diagnostics. Unknown compatibility must be visible rather than silently presented as success.

## Genre change UX

The installation review explicitly says: "Current games are removed first. This cabinet will be unavailable until the next collection is ready. Saves and settings are kept."

Preflight resolves the exact target and checks missing imports, necessary device setup and space before destructive work begins. If a game or reserved multiplayer session is active, queue the operation until it ends. A local and remote request cannot race to delete files. Requests with a stale expected cabinet revision need a fresh plan.

After old files have been deleted, canceling cannot restore the old genre immediately. Offer resume, another eligible collection, or reacquiring the old collection from its authorized source/import. Do not offer a misleading "undo" action. Players see a maintenance screen while no collection is ready.

If a file is shared by the old and new collections, keep it as a target dependency. This satisfies removal of the prior genre's unneeded content without wasting bandwidth or breaking the new genre.

## Central dashboard

- Venue/cabinet table: online status, last-seen timestamp, actual active genre, requested target, operation progress, free space and application version.
- Cabinet detail: hardware profile, ready/missing games, setup checks, recent failures and operation history.
- Sengoku-Admin catalog editor: add canonical games, create a draft suite, choose game editions, emulator releases, sources, hardware/multiplayer profiles and guides; validate and publish a new immutable revision.
- Change genre: select cabinets, review each cabinet's plan, and send an authorized command. Report offline, busy and unsupported cabinets individually.
- Rollout tracking: queued, accepted, waiting for idle, removing, installing, ready, failed, canceled and expired are separate statuses.
- Access management: operators are scoped to their organization/venue; the Sengoku-Admin role controls shared game creation and the recommended catalog-publication policy.
- Venue multiplayer summary: optional cached host health and capability status. Live lobby/session access and host administration remain on the venue LAN; the cloud dashboard provides no join link, tunnel or remote session-control action.

A successful API submission means the command was queued, not that the cabinet changed. Cached status must show its age. An offline cabinet cannot be reported as ready based only on a desired genre assignment.

The pilot has four cabinets. The overview must make all four cabinets' current and requested states easy to compare without opening individual detail pages. Do not infer the number of venues from the cabinet count.

For the first release, use simple browser refresh/polling for status and outbound cabinet polling. Introduce streaming status only if the measured operator experience needs it.

## Multiplayer UX, implemented in the final MVP feature milestone

Show per-game modes separately: **On this cabinet**, **Play with nearby cabinets**, and **Venue server required**, with tested player limits. Unsupported or unresearched cross-cabinet modes explain their status instead of offering a nonfunctional Join button. Same-cabinet couch/split-screen modes never prompt for cloud login or require the venue server.

For a supported cross-cabinet mode: choose the game/mode, show compatible available cabinets, reserve participants, verify matching game/emulator/settings and local services, assign player inputs, show the host/save owner, then launch together. A joining cabinet with a missing game or different active genre receives an operator action; the app never silently deletes its library to join a lobby. Discovery and session traffic remain within the approved venue network.

Internet connectivity and venue-server availability are separate status indicators. A venue-server failure can disable server-dependent modes while leaving local couch play available. Reconnect, desync and host-loss messages must match the adapter's actual recovery capabilities. Resetting a shared save or changing genres waits until the session ends.

If two cabinets use one host with shared input/video, explain that both displays may show the same output/split screen. Do not promise independent views unless that game/profile supports them. Network/service configuration stays in the local operator setup flow, outside player mode. Details and evidence are in the [multiplayer design](multiplayer.md).

## Input and accessibility

### Implemented prototype return and game-change flow

One **Return to NCade** action is shared by the Windows global shortcut, keyboard-encoder cabinet buttons and XInput controller combinations. The default is `Ctrl+Alt+Home`, or hold **Back + Start** for 1.5 seconds. The four XInput slots are polled while the tracked game owns the foreground; held buttons must first be released and the gesture fires once per hold. This uses `RegisterHotKey` and XInput, with no global keyboard interception or game-process injection. A failed shortcut registration displays an Alt+Tab fallback.

Operators can copy `examples/cabinet-controls.example.json` to `<data-dir>/cabinet-controls.json` and edit it before starting the desktop. `default` supplies the fallback and `genres` supplies complete overrides keyed by genre ID. The running game's genre selects the mapping; browsing another genre does not change it. A dedicated keyboard encoder can emit a modifier chord or an unused F13–F24 key. The example racing F13 mapping must be assigned on the actual cabinet encoder before use. Invalid mappings report an error and use defaults. Controller combinations require at least two distinct buttons and a 750–5000 ms hold. These mappings are local settings, not shared game catalog data.

In the library, XInput D-pad left/right selects a genre, up/down selects a game, A selects/confirms, and B cancels or returns to the game. Buttons must be released before acting on a newly shown menu/confirmation. Keyboard/mouse navigation continues to work. Raw HID/DirectInput wheels, PlayStation devices without an XInput translation layer, device-specific labels and physical hold testing remain pending the operator's hardware list. Do not claim those devices are supported just because the emulator supports them.

The return action brings the library forward without ending the game. **Back to game** minimizes NCade to reveal the previous game/native prompt. Games can continue running and accepting their own input in the background: generic pausing/input isolation is not implemented. Per-adapter focus-loss pause and background-input policies must be validated before unattended cabinet deployment; NCade does not suspend processes or rewrite existing preferences to simulate pause.

Selecting another installed game offers **End game and switch**, explains unsaved progress, and captures the target and current session ID. Confirmation sends a graceful close bound to that session ID. Polling retains the waiting state and launches the captured target only after that session reports `exited` with code 0. A changed session, interrupted/failed exit, disconnect, or 60-second timeout cancels the next launch. **Cancel next launch** cannot retract an already delivered close request. No forced stop or genre deletion occurs in this player flow. Native prompts remain visible in the emulator; Eden currently requires its exit confirmation. Closing NCade forgets any pending next launch.

RPCS3's keyboard and mouse fullscreen shortcuts can be disabled independently of render resolution using the backed-up `scripts/Set-Rpcs3CabinetControls.ps1` helper. It changes only `GSFrame.disableKbHotkeys` and `GSFrame.disableMouse` in the isolated GUI INI, after checking the session is idle. This disables emulator hotkeys broadly, including its pause/save-state shortcuts; normal emulated controller bindings remain separate. OS shortcuts and Windows lockdown are outside these controls. See [emulator validation](emulators.md#validation-on-november-10-2026).

The current emulator-onboarding surface is the operator CLI: `emulator onboard --id <id>` previews user-selected paths, offers BIOS/firmware imports, reports missing prerequisites, and opens the emulator's isolated native settings. `prepare`, `firmware`, `status`, and `configure` provide scriptable equivalents. The desktop uses the same game launch route for all five adapters and labels configuration/firmware sessions separately. Native settings remain the control-binding and CPU/GPU interface until cabinet controls and styling are finalized. These sessions block genre changes, imports and another launch; operators close the native window or request Stop before continuing.

- Keyboard and controller navigation, predictable focus order, visible focus and large targets.
- An exit-to-library action that works for each supported emulator and restores focus without terminating unrelated processes.
- Controller disconnection/reconnection and multiple identical controllers handled explicitly.
- Optional wheel-as-menu-input only when mappings are validated; steering input must not accidentally trigger destructive operator actions.
- Readable contrast, scalable text, reduced-motion preference, and no color-only status indicators.
- Validate common 1080p and the reference machine's 1440p display, including Windows scaling; the game render resolution is a separate setting.

## Proposed usability acceptance

- A new operator completes onboarding using only the app and the explicitly identified vendor setup steps.
- A player can choose, launch, exit and change games without a mouse for every supported control profile.
- Killing/restarting the interface during an installation reconnects to durable operation state without creating another job.
- An interrupted download or missing BIOS explains what is missing and presents an actionable recovery path.
- Hardware setup completes before a suite is marked fully configured; unsupported rigs are labeled as such.
- An operator cannot modify a cabinet outside their venue scope through either the dashboard or direct API requests.
- A non-Sengoku-Admin can add a local-only game but cannot create a shared catalog record through the UI, API or import tooling.
- Same-cabinet co-op works with the network disconnected; LAN-qualified sessions work with Internet disconnected and expose no external join/control path.
