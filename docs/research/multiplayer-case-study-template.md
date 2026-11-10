# Multiplayer case study: <game / platform / edition>

Status: template only. Copy for an actual researched edition; do not mark verification complete from documentation alone.

## Identity and scope

- Catalog game/edition ID or local-only reference:
- Platform (distinguish arcade board from console port), region, revision and release year:
- Content identifiers/hashes, updates/DLC/firmware:
- Emulator/adapter/build and host OS:
- Modes examined: same-cabinet co-op, local versus, native LAN/link, emulator netplay, replacement online services, shared host/display/input:
- Maximum players, cabinets, controls per cabinet, and actual target for this study:
- Researcher/date; Sengoku-Admin publication review:
- State: unresearched / researching / lab_verified / venue_verified / unsupported / not_applicable:

## Evidence

| Source URL or permitted public Discord permalink | Publisher/maintainer | Date/build | Claim supported | Reproduced? |
| --- | --- | --- | --- | --- |
| To research | | | | No |

Distinguish documented facts, community reports, inferences and measured results. Record missing/private evidence as unavailable; do not present it as inspected. No credentials, private community messages or proprietary game payloads in this document.

## Topology and dependency inventory

- Simulation authority: one cabinet / synchronized emulator instances / dedicated server:
- Local coordinator/lobby/master server and what each actually does:
- Required platform auth, game backend, signaling and discovery services:
- Upstream repositories, pinned versions/artifact hashes, licenses and intended-use terms:
- Service host OS/architecture, CPU/RAM/storage, databases, accounts and persistent volumes:
- Local addresses/interfaces, DNS names, certificates, TCP/UDP ports, multicast/broadcast and dynamic ranges:
- Every external call observed at first boot, authentication, matchmaking, play and shutdown:
- Supported method to run each mandatory dependency locally:
- Public lobby announcement, relay, traversal, router mappings and cloud fallback disabled/blocked:
- Unresolved dependency or explicit reason the LAN-only requirement cannot yet be met:

## Cabinet and session behavior

- Reproducible setup and teardown procedure using approved artifacts/local imports:
- Required edition/settings/version agreement and mismatch detection:
- Controller-slot assignment, input conflicts, feedback and per-player screen behavior:
- Save source/owner, backup/writeback policy and handling of existing cabinet saves:
- Reservation/launch/end workflow; handling of another game, genre change or update request:
- Disconnect, desync, reconnect, host loss, coordinator loss and unsupported recovery behavior:
- Whether a two-cabinet setup runs two synchronized instances or one host with shared input/display:
- Whether shared display shows the same picture, split screen, or supported independent player views:

## Verification record

| Check | Hardware/build/topology | Result and evidence | Remaining issue |
| --- | --- | --- | --- |
| Same-cabinet mode with WAN and LAN disconnected | | Not tested | |
| Local server cold start and session with WAN disconnected | | Not tested | |
| Two-cabinet join/play/end and input allocation | | Not tested | |
| Supported four-cabinet load or concurrent pairs | | Not tested | |
| Long session: emulation speed, input latency, jitter/desync, audio and server load | | Not tested | |
| Edition/configuration mismatch rejected | | Not tested | |
| Guest/outside segment denied over IPv4 and IPv6 | | Not tested | |
| No public auth/lobby/relay/traversal or automatic port mapping | | Not tested | |
| Disconnect/restart and save integrity | | Not tested | |
| Active/reserved session prevents destructive genre/update/reset actions | | Not tested | |

## Recommendation

- Verdict and supported modes, with separate verdicts for untested modes:
- Hardware/network limits and measured tolerances:
- Required service/adapter work and remaining blockers:
- Commercial/distribution review status for the exact artifacts:
- Published profile revision and test evidence location, when approved:
- Revalidation triggers: game/emulator/service/configuration changes and network topology changes:
