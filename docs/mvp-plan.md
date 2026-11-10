# MVP scope and delivery plan

Status: design baseline, November 10, 2026. Architecture choices are recommendations; the product decisions below reflect the user's answers.

Implementation update: the first foundation slice now provides Avalonia, the Go core, named-pipe IPC and SQLite/PostgreSQL migrations. See [current implementation and verification](development.md). The broader proof items below (physical Windows 11/controller tests, first emulator launch, hosted API contract and installer measurements) remain tracked work; no multiplayer server has been implemented.

Local installation/play update: CLI storage setup, raw-file copy/reference imports, authorized downloads, remove-first genre switching, cancellation/retry and the Dolphin session adapter are implemented. Automated tests exercise two synthetic collections, persistent-file retention and Win32 process recovery. Actual Dolphin gameplay/peripheral validation still requires supplied executable/content. See [operator instructions and exact limits](operator-cli.md); multiplayer stays last.

## Confirmed decisions

| Topic | Decision |
| --- | --- |
| Audience | Commercial cabinets/venues |
| Pilot | Four cabinets; venue count not yet specified |
| Interfaces | C#; recommend Avalonia cabinet app and Blazor WebAssembly central dashboard |
| Application logic | Go desktop core, Go hosted API and a separate Go venue coordinator with approved upstream multiplayer services |
| Remote persistence | PostgreSQL metadata, object storage or authorized URLs for large assets |
| Administration | Local operator controls and a central dashboard |
| Shared catalog authority | Only Sengoku-Admin can add canonical games; recommend the same boundary for shared edits/publication |
| Local additions | Users can maintain local-only installed games independently; no automatic shared-catalog insertion |
| Play model | Free play; no payments or timed paid sessions |
| Venue multiplayer | Separate server on the cabinets' LAN; no outside access to services/lobbies; final MVP feature implementation milestone |
| Couch and linked play | Same-cabinet modes need no network; two-cabinet shared-session support is researched per game where feasible |
| Saves | Shared cabinet saves/unlocks, retained on genre changes, explicit operator reset |
| Prototype shell | Full-screen application; OS kiosk lockdown later |
| Content | Local user imports plus authorized homebrew/freeware downloads |
| Curation | Arcade and console games released in 1990-2010, prioritizing cabinet suitability and tested compatibility |
| Storage switching | Remove old unneeded managed content before downloading the new genre |
| Reference hardware | i7-12800HX, RTX 3070 Ti Laptop GPU, 32 GB RAM, 1 TB SSD as upper reference |
| Peripheral scope | Exact supported models to be supplied later |

## Full MVP catalog target

| Genre | Entries |
| --- | ---: |
| Racing | 20 |
| Fighting | 20 |
| Action Adventure | 20 |
| Platform | 20 |
| Hack and Slash | 20 |
| Total genre entries | 100 |

Twenty entries is a curated collection size, not a guarantee that 20 files are automatically downloadable or playable on every machine. A catalog entry can require an operator import, missing prerequisite or different tested hardware profile. The operator sees readiness for each entry; players see only ready, permitted games.

Aim for distinct selections by assigning each title a primary curation genre, while allowing multiple genre tags in the schema. If a title appears in two suites, treat it as one shared edition/artifact rather than duplicating its files. Track the original release year and the exact selected platform/region/edition separately.

The 100-title shortlist has not yet been researched or populated. Do not present draft slots as selected games. The upcoming catalog pass should use official release/platform information and emulator compatibility records, with reception/history as an editorial input rather than claiming an objective universal "top 20."

## Curation process

For each candidate, record title, release year, platform/edition, primary genre, player count, input suitability, typical session suitability, storage footprint, emulator/version, setup complexity, compatibility evidence, content acquisition mode and commercial-use permission status.

Also classify competitive/co-op modes, same-cabinet versus cross-cabinet support, native LAN/emulator netplay/backend requirements, service dependencies and network evidence. Classify every selected edition; use the [case-study template](research/multiplayer-case-study-template.md) for each multiplayer candidate. Keep unresearched or unsupported modes visible. A Sengoku-Admin reviews and publishes canonical additions and verified profiles; user-added local entries remain outside this catalog workflow.

First filter for the 1990-2010 window, viable content acquisition, acceptable intended-use terms and an emulator integration appropriate for commercial deployment. Then rank cabinet interaction, measured reliability, controller fit, audience recognition and storage/setup cost. Action-adventure games with longer progression can use shared saves, but still need a practical exit/return flow.

Use candidate -> researched -> locally validated -> venue-approved publication states. A well-known title can be documented as a candidate without bundling its ROM or asserting venue-use permission. An emulator's compatibility listing is supporting evidence, not a substitute for testing our exact version/settings and peripherals.

Do not assume every named emulator belongs in the first release. PCSX2 and RPCS3 are the intended spellings for the PS2 and PS3 projects. Select emulator integrations from the approved catalog rather than supporting every available project up front. MAME may be necessary for original arcade-board titles; its versioned set dependencies must be modeled explicitly.

## Delivery milestones

1. **Foundation and architecture proof.** Validate Avalonia-to-Go IPC, startup/reconnection, controller focus and one emulator launch/exit on Windows 11 x64. Establish the hosted OpenAPI contract, PostgreSQL/SQLite migrations and a local development environment. Measure cabinet installer size before fixing a size target.
2. **One complete install/switch flow.** Implement two small test collections using the first permitted emulator integration. Exercise authorized download/import, requirements, guide progress, remove-first switching, savedata preservation, recovery and offline launching. This is an integration milestone, not a reduction of the final catalog target.
3. **Four-cabinet management.** Add operator authentication, Sengoku-Admin-only shared game creation, independent local-library additions, device enrollment, local authorization, central dashboard, per-cabinet command lifecycle, audit records and conflict handling. Test disconnected and busy cabinets alongside online ones.
4. **Hardware and catalog expansion.** Add the supplied peripheral profiles, required emulator adapters and all five 20-entry collections. Validate each supported edition/profile and document content that still requires operator imports or permissions.
5. **Base application release preparation.** Signed installer/update packages, restart/reboot recovery, dependency/license notices, restore drills, performance evidence and operator documentation. Establish a reliable local-play baseline before implementing venue networking; this is not completion of the full MVP.
6. **Venue multiplayer research and implementation, last.** Complete per-game studies against the now-tested editions, select local service stacks, deploy the separate venue coordinator/server, and integrate approved native LAN, replacement backend and feasible two-cabinet shared-session modes. Verify WAN independence, external-network denial, session/save ownership and four-cabinet operation. Finish integrated pilot acceptance/release checks after this final feature; the server remains required for MVP completion.

OS kiosk lockdown, payments, public player accounts/cloud saves, arbitrary community install scripts, automatic universal firmware flashing and remote arbitrary shell access are outside this MVP. Central management, the five collections, guided hardware support and the venue multiplayer server are in scope. Firmware/vendor interactions can be operator-assisted. Protocol-specific local service identities or local backend progression may be necessary for a researched multiplayer mode and do not add a public account/cloud-save feature.

Document network boundaries and reserve extension points now. Detailed per-game service integration and the venue server are built last. A failed study is recorded as an unresolved capability or explicit unsupported mode; it does not automatically remove the server requirement. See [multiplayer architecture and acceptance](multiplayer.md).

## Acceptance matrix

| Area | Evidence required |
| --- | --- |
| Initial setup | Clean Windows machine installs from one setup executable and completes the selected genre's wizard |
| Catalog | Five published genre manifests with 20 reviewed entries each; exact readiness/acquisition status is visible |
| Gameplay | Every claimed supported edition/profile launches, accepts intended input, exits and returns focus to the library |
| Switching | Old unneeded managed games removed first; emulators, required shared files, saves and settings retained |
| Recovery | Restart/power-loss injection at removal/download/extraction/activation boundaries produces a resumable or clearly actionable state |
| Storage | Budget and real free space checked using peak expanded usage, with bounded temporary/cache use |
| Fleet | Four cabinets receive independent commands; duplicate delivery cannot repeat deletion or installation |
| Conflicts | Simultaneous local/remote changes serialize; stale plans are rejected; running games are not interrupted by default |
| Offline | Single-player/couch modes work without LAN/WAN; verified venue modes work without WAN; stale cloud state is labeled |
| Access | Venue scoping and core/API permissions enforced; non-Sengoku-Admin shared additions rejected through every write/import route |
| Local library | User can add a local-only entry without changing shared PostgreSQL catalog; its files survive ordinary genre cleanup unless explicitly managed |
| Venue server | Separate host provides a proven local service/lobby integration; detailed game support matrix and dependencies recorded |
| Linked play | Feasible selected two-cabinet session demonstrated with matched builds/content, assigned inputs and defined save ownership |
| Network boundary | External/guest access denied over IPv4/IPv6; no public discovery, relays, traversal, router mappings or cloud session tunnels |
| Session safety | Active/reserved multiplayer blocks cleanup/update/reset; disconnect and coordinator/host failure preserve files and saves |
| Integrity | Bad hashes, unsafe archives, untrusted manifests and invalid source redirects fail before execution |
| Saves | Shared progress survives swaps; an explicit operator reset affects only the selected save set |
| Hardware | Each declared rig profile completes detection, calibration and tested per-game mappings |
| Performance | Record exact settings and full-speed behavior for each claimed game/profile on the reference and any advertised lower tier |
| Operations | Redacted support export, signed updates, migration recovery and database backup restore demonstrated |

Use Go unit tests for dependency resolution and state transitions, integration tests with temporary filesystems/SQLite/PostgreSQL, IPC/API contract checks, and real UI/hardware smoke tests. Failure injection and deletion-boundary tests are especially valuable. Test fixtures must use synthetic or explicitly redistributable content.

A launch smoke test alone does not justify a "smooth gameplay" claim. Capture sustained representative gameplay, audio behavior and frame pacing after warm-up, along with the machine's GPU mode/power state. Establish exact performance tolerances during the first playable milestone; preserve the game's native timing target.

## Open decisions

These remaining details do not prevent starting the foundation work:

- **Storage budget per cabinet:** not supplied. Make it a required onboarding setting based on measured free space, with an OS reserve; do not assume that a 1 TB drive is entirely available for games.
- **Peripheral list:** owner will supply wheel, pedals, controllers and any other rig hardware. Exact guided automation and force-feedback promises depend on it.
- **Pilot venues/network:** four cabinets confirmed; grouping, bandwidth and offline duration expectations remain to be recorded. Propose one concurrent large download per venue initially, configurable by the operator.
- **Reference profiles:** collect the owner's tested games/settings and validate lower hardware tiers before publishing their requirements.
- **Content permissions:** identify actual download permissions and terms appropriate for commercial use; choose the first distributable/import-based validation set and resolve any emulator restrictions.
- **Cloud identity/deployment:** choose the operator identity provider and hosting/network configuration alongside the existing Azure PostgreSQL environment.
- **Venue multiplayer host:** hardware/OS, local topology and exact supported titles/services follow case studies; keep the separate-server milestone in the MVP. The LAN-only boundary applies to multiplayer while the existing cloud catalog/dashboard remains a separate management path.

Recommended defaults: Windows 11 x64, one active genre per cabinet, queue genre changes until games/reservations end, retain installed single-player/couch modes for offline play within their terms, and never auto-remove original import sources or unassigned local-only installations. For multiplayer, prefer wired LAN, a nominated save owner and no public fallback.

## Research notes and limits

Research reviewed November 10, 2026. Recheck the exact upstream release when publishing a package; requirements, download links and license terms can change.

Initial primary-source multiplayer findings, their limitations and per-game research requirements are maintained in [the venue multiplayer document](multiplayer.md#initial-research-evidence-not-game-certifications).

- [Avalonia platform support](https://docs.avaloniaui.net/docs/supported-platforms): supports the C# desktop recommendation; portability does not guarantee every emulator/driver is portable.
- [Blazor hosting models](https://learn.microsoft.com/en-us/aspnet/core/blazor/hosting-models?view=aspnetcore-10.0): standalone WebAssembly supports a C# dashboard with static hosting and a separate Go API.
- [DuckStation README](https://github.com/stenzek/duckstation/blob/master/README.md): BIOS requirements and redistribution/configuration restrictions must inform integration choices.
- [PCSX2 requirements](https://pcsx2.net/docs/setup/requirements/): requirements vary with title and rendering settings; do not reduce compatibility to one GPU model.
- [Dolphin README](https://github.com/dolphin-emu/dolphin/blob/master/Readme.md): official platform requirements and launch/user-directory controls provide adapter research inputs.
- [RPCS3 README](https://github.com/RPCS3/rpcs3/blob/master/README.md): official integration starting point. The linked [quickstart](https://rpcs3.net/quickstart) returned HTTP 403 during this research, so no unverified current RPCS3 numeric requirements were copied into the design.
- [MAME ROM/set model](https://docs.mamedev.org/usingmame/aboutromsets.html): supports explicit dependency modeling rather than flat lists of ROM URLs.
- [Logitech wheel troubleshooting](https://support.logi.com/hc/en-us/articles/4404187621911-Troubleshoot-G923-G29-G920-for-connection-Issues): illustrates vendor-specific device detection and calibration; it is not a commitment to support a model the owner has not selected.
- [Reference laptop specification](https://www.newegg.com/p/N82E16834156554): confirms the supplied hardware target and Windows Home listing; no measured emulation result was inferred from the listing.

## Repository work in this design phase

Added these design documents and a README. Preserved the existing Go/Cobra scaffold and the pre-existing edit in `cmd/root.go`. No ROMs/emulators were downloaded, no application stack was installed, and no remote database was contacted or changed. The proposed table model is ready to translate into reviewed migration files during implementation.

The requirements update adds the LAN-only venue server, multiplayer research template, Sengoku-Admin catalog boundary, local-library ownership rules and revised final milestone. These are documented requirements, not implemented or game-tested capabilities.
