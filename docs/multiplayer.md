# Venue multiplayer design and research plan

Status: requirements/design update, November 10, 2026. Venue multiplayer is part of the MVP and is its final feature implementation milestone. No multiplayer server or game integration has been implemented or tested yet.

## Confirmed requirements

- A separate server on the cabinets' local network hosts the homebrew services, frameworks, lobbies and game-specific online dependencies needed by supported games.
- Multiplayer servers/lobbies are accessible only within that venue network. Outside players, remote access to those services and public matchmaking are excluded.
- Research the exact requirements per game/edition before promising support: peer-to-peer, lobby/master server, dedicated gameplay server and replacement platform services are different possibilities.
- Same-cabinet couch/split-screen co-op must work without an online connection. Two cabinets sharing a session is a research-dependent capability where technically feasible.
- Only Sengoku-Admin can add games to the shared PostgreSQL catalog. Users may add locally installed games to their local library independently.

Interpret the network restriction as applying to venue multiplayer services, their administration endpoints and game traffic. The previously agreed hosted catalog and central fleet dashboard remain separate management services. They must not proxy, tunnel or expose a venue lobby, session, controller stream or game server. If all management is later required to be on-premises too, that is a separate deployment change.

## Supported mechanisms are explicit per title

| Mechanism | Where the game runs | Role of the venue server | Conditions |
| --- | --- | --- | --- |
| Same-cabinet local multiplayer | One cabinet, multiple local controls | None required | Works with LAN and Internet disconnected |
| Native LAN/link play | Participating cabinets run their supported game editions | Local discovery/coordination, plus any required native service | Actual game and emulator must support the network/link protocol |
| Emulator netplay of a local multiplayer game | Multiple emulator instances synchronize inputs/state | Local session directory and coordination; a cabinet can host gameplay | Matching content/build/settings and a tested netplay implementation |
| Recreated online services | Cabinets plus self-hosted platform/game services | Local authentication, lobby, signaling, game backend and/or dedicated server as required | Every mandatory dependency must function locally without a public fallback |
| One host with shared display/input | One cabinet runs the game; a second supplies a player control/display | Optional coordination; transport must remain local | Research candidate for games without suitable netplay; latency and input isolation must be tested |

Co-op and competitive play are separate game modes, not separate network architectures. One game's profile can expose several mechanisms with different player limits. A lobby may only discover peers; it does not necessarily simulate the game or carry gameplay packets. NostalgiaCade's Go coordinator manages sessions and approved service lifecycles rather than attempting to reproduce every game's networking protocol.

Two independent copies do not automatically share a running game state. Prefer a proven emulator synchronization mechanism, a game's native link/LAN mode, or a single authoritative host with local input/display sharing. A duplicated display may show the same split-screen image on both cabinets; separate player cameras are only possible if the selected game/implementation supplies them. Same-cabinet play needs no network; multi-cabinet play still needs a local connection even when it needs no Internet.

## Venue deployment

Add a third first-party Go deployment unit: a **venue coordinator** on a separate local host for each isolated venue network. It manages cabinet discovery/pairing, capability checks, reservations, session metadata and approved service adapters. Reuse upstream homebrew servers in their original languages as isolated processes/containers where practical. Do not rewrite those services in Go just to keep the application stack uniform.

The venue host owns its local session journal and approved cached multiplayer profiles. Each upstream service may need its own storage engine and local accounts. Keep those databases and protocol-specific credentials local; remote PostgreSQL remains the catalog/management store and is not in the gameplay path. The host OS, CPU/RAM/storage and supported container/VM approach follow the service survey; the cabinet laptop's specification does not establish server sizing.

The coordinator is not the authoritative simulation host for every mode. A profile declares whether state authority belongs to a cabinet, a dedicated game process or synchronized emulator instances, and where durable game progression lives. Server outage handling follows that declaration: same-cabinet play continues; new server-dependent sessions are unavailable; an active session continues only when its verified protocol supports it.

## Enforcing the venue boundary

1. Prefer a dedicated wired cabinet network/VLAN for the pilot. Define the allowed interfaces, cabinet addresses/devices and venue identity explicitly. An arbitrary private IP address or matching venue identifier is insufficient proof of access.
2. Bind coordinator, lobby, discovery, game and service-admin listeners to the intended local interfaces. Restrict both host and network firewalls to the declared devices/ports, covering IPv4 and IPv6. Verify UDP, broadcast/multicast and any dynamically allocated ports as well as TCP.
3. Block Internet/guest-network ingress and routing from outside networks, including VPN/overlay paths. Disable router forwarding, UPnP/NAT-PMP mappings, public lobby announcement, Internet traversal/relay services and reverse tunnels. No public fallback is permitted on failure.
4. During play, allow game/service traffic only to the approved venue peers and local dependencies. Replace or disable required public authentication, DNS/discovery, signaling and backend calls through supported configuration. A game still requiring an external service is not LAN-qualified.
5. Enroll cabinets locally and authenticate the coordinator protocol with device identities and short-lived session credentials. Legacy game protocols may lack modern authentication; isolate their ports and peers and document residual limitations. A lobby password alone is not network isolation.
6. Download signed service packages/metadata through a distinct maintenance path when needed. Restrict that path to updates/catalog operations; it is not a tunnel to multiplayer or an external session-control endpoint. Local UI manages the venue host. The cloud dashboard may receive minimal cached health summaries but cannot join, create, control or expose local sessions.
7. Prove the boundary with WAN-disconnected cold starts, LAN gameplay, IPv4/IPv6 reachability tests from an unauthorized test segment, and network captures. Internet loss must never cause fallback to a public lobby or relay.

The existing outbound fleet client remains separate. Central genre/update requests must respect active or reserved multiplayer sessions, just as they respect a running single-player game. Multiplayer adds explicitly authorized LAN listeners where needed; it does not create public cabinet management ports.

## Session lifecycle and persistence

Use `available -> reserved -> validating -> ready -> running -> ended`, with `failed`, `expired` and `canceled` outcomes. Session records include a unique ID, venue, mechanism/profile revision, edition/content identity, participants, input slots, host/authority, endpoints and version requirements. Keep controller frames, audiovisual streams and simulation state out of the catalog API and database.

Before reserving two cabinets, check both are available, on the same approved venue network, have the matching required content and emulator/service versions, and have compatible mapped controls. Each core persists a local reservation and locks the required artifacts. Do not silently replace a cabinet's genre, copy a ROM between cabinets, or launch with a mismatched game edition to satisfy a join request.

Reservation leases coordinate availability, but the local core's running-game lock is authoritative for file safety. Expired leases, an unreachable coordinator or stale cloud status never authorize removal of files used by a running session. Delay genre switches, emulator/service updates and save resets until the relevant session is safely ended. One pending or live session owns a cabinet at a time in the MVP.

Default save policy for synchronized couch play: choose one explicit host cabinet's existing shared save as the session source, back it up locally, and commit supported progress to that same owner after a clean session end. Do not merge independent cabinet saves or overwrite every participant's local progress. Profiles may instead require local backend-owned progression or read-only/no-save sessions; show that policy before starting. No player cloud-save system is introduced.

On a disconnect, release unused reservations, preserve durable saves and show actionable status. Resume/rejoin, host migration and crash recovery are supported only where the exact adapter/protocol has been verified. Avoid claiming that a saved file is a universally portable live-state checkpoint.

## Research workflow

Classify every selected catalog edition for local/competitive/co-op modes. Complete a detailed case study for each edition proposed for multiplayer; record `not_applicable`, `unresearched`, `researching`, `lab_verified`, `venue_verified` or `unsupported` with a reason. A working single-player emulator entry does not imply a working network profile.

Use the [case-study template](research/multiplayer-case-study-template.md). Start with official operator manuals, emulator docs, upstream source/configuration and open issue trackers. Public Discord documentation can provide leads; record a durable URL, date, build and reproducible test. Community reports without reproduction remain unverified. Do not treat private channels as accessible evidence or send messages to communities without separate authorization.

Each study identifies exact game/platform/region/revision, network protocol and topology, local service dependencies and versions, account/certificate/DNS needs, ports/interfaces, startup sequence, save ownership, two-cabinet behavior, latency/desync limits, licensing and all observed external network calls. Prefer existing supported software/configuration. A universal title-server emulator is not assumed.

First classify the whole curated catalog, then deeply validate representatives of native LAN, backend-dependent play and synchronized couch play. Expand only from reproducible profiles. Single-cabinet couch support is implemented with the base launcher; the venue server and multi-cabinet integration are implemented last.

## Initial research evidence, not game certifications

- **Dolphin:** its guide documents local multiplayer through Netplay, direct connections, matching game revisions/emulator versions and host-save choices. This makes direct LAN netplay a candidate mechanism; each title still needs testing. Source: [official Netplay guide](https://dolphin-emu.org/docs/guides/netplay-guide/).
- **RetroArch/libretro:** its FAQ describes synchronization of local multiplayer instances and requires matching versions/content. Capability varies by core; this is not a generic way to enable a game's original online mode. Source: [official Netplay FAQ](https://docs.libretro.com/guides/netplay-faq/).
- **Network defaults:** RetroArch documents public lobby announcement, relays and automatic UPnP mapping. Approved venue profiles must explicitly disable these external paths and test the effective configuration. Source: [official setup guide](https://docs.libretro.com/guides/netplay-getting-started/).
- **RPCN:** upstream describes rooms/matchmaking, scoreboards and title storage for RPCS3. This identifies a candidate platform service, not proof that a given title's additional backend exists. Its sample configuration listens on wildcard IPv4/IPv6 addresses, so service isolation must be deliberate. Sources: [RPCN README](https://github.com/RipleyTom/rpcn/blob/master/README.md), [RPCN configuration](https://github.com/RipleyTom/rpcn/blob/master/rpcn.cfg).
- **Tekken examples:** arcade and console editions need separate research. Bandai Namco's Tekken 6 page identifies the arcade release but does not establish an emulator/network recipe. Tekken 7 is a useful linked-cabinet research reference; its listed 2015 arcade release fits the catalog window, but there is no supported arcade adapter in the current prototype. Its specific local topology is unverified here. Sources: [Tekken 6 arcade page](https://bandainamco-am.co.jp/am/english/aa/tekken6/), [Bandai Namco arcade catalog](https://bandainamco-am.co.jp/am/bnaf11ls.php).

No complete per-game case studies, physical multiplayer tests or Discord research have been performed yet. The above sources justify the design categories, not a claim of tested game support.

## Final MVP milestone acceptance

- The separate venue server runs at least one proven locally hosted game/platform service with participating pilot cabinets. An architecture document or successful single-player launch does not satisfy this server requirement.
- Every curated edition has a recorded multiplayer classification. Every advertised multiplayer mode has a verified profile and reproduction evidence; unsupported/unknown modes are labeled. The concrete title support matrix is agreed after research, with unresolved requested support remaining visible.
- Demonstrate same-cabinet co-op without either WAN or LAN. Demonstrate two-cabinet shared-session play for a feasible selected title and record its save/input/host behavior.
- Test all four pilot cabinets, including two independent two-cabinet sessions or a supported four-player session, matching the selected games' actual limits. Measure the resulting venue-server load.
- With the Internet disconnected before startup, validated LAN modes can discover/authenticate/join/play/end using local services. Same-cabinet modes remain usable when the venue server is unavailable.
- Unauthorized external/guest segments and unregistered devices cannot access the coordinator/lobbies or allowed legacy game ports; test IPv4/IPv6 and verify no public announcement, traversal, forwarding or relay traffic.
- Mismatched editions/configuration, unavailable dependencies, duplicate joins, timeouts, host/coordinator crashes, disconnects, pending genre changes and save conflicts produce defined results without deleting live game files or corrupting saves.
- A failed feasibility study is documented as an unresolved capability or requires an explicit scope decision; it does not silently move the entire multiplayer server out of the MVP.
