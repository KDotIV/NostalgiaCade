# Architecture proposal

Status: design for discussion, November 10, 2026. User-confirmed requirements are distinguished from recommended implementation choices. See [open decisions](mvp-plan.md#open-decisions).

The Avalonia/Go/IPC/database foundation is now implemented. [Development instructions](development.md) describe its actual scope; the remaining architecture here is the target design.

November 11 implementation update: the current remote API is catalog-only. Fleet,
venue management and reporting tables were removed from PostgreSQL; they remain
future capabilities in this proposal. See the [current Go structure](go-project-layout.md)
and [local/Azure host](api-hosting.md). The API is portable, with Azure deployment
assets prepared but no hosted environment deployed by this work.

## Confirmed product

- Commercial arcade cabinets and venues, with free play in the MVP.
- C# user interfaces and Go application/backend logic.
- Both local operator controls and a central management dashboard.
- A separate venue multiplayer server, implemented in the final MVP feature milestone, hosts supported local online services/lobbies. Access is restricted to the cabinets' venue network with no outside multiplayer connections.
- Same-cabinet couch/split-screen co-op requires no network. Shared-session play across two cabinets is supported where a per-game feasibility study verifies it.
- Shared-catalog game additions are restricted to the application role `Sengoku-Admin`; local-only user installations remain independent.
- Four cabinets in the pilot. Full-screen application for the prototype; Windows kiosk lockdown is a later deployment concern.
- Shared cabinet saves/unlocks, with an operator-controlled reset.
- Racing, Fighting, Action Adventure, Platform, and Hack and Slash; 20 catalog entries per genre.
- Games from 1990 through 2015, inclusive, mixing arcade and console systems. Cabinet suitability and tested compatibility take priority over a popularity ranking.
- User-imported games plus downloads from authorized homebrew/freeware sources.
- Remove the old genre's unneeded managed games before downloading the new genre. Keep emulators, firmware/software, saves, and settings.
- A lightweight Windows installer with guided setup. Wheel, pedal, and controller models will follow later.
- Upper reference hardware: the user's i7-12800HX / RTX 3070 Ti Laptop GPU / 32 GB RAM / 1 TB SSD machine. This is a validation ceiling for the curated catalog, not a minimum requirement or a reason to reject stronger PCs.

## Recommended components

| Component | Technology | Owns |
| --- | --- | --- |
| Cabinet interface | C# Avalonia, XAML, MVVM, supported .NET LTS | Player launcher, operator screens, onboarding, presentation and navigation |
| Desktop core | Go child process in the interactive Windows session | Inventory, installation plans, disk ownership, downloads, hardware adapters, emulator processes, durable jobs |
| Local state | SQLite, accessed only by the Go core | Installed inventory, active suite, operations, cached manifests, settings and recovery journal |
| Central dashboard | Standalone C# Blazor WebAssembly | Cabinet inventory, catalog curation, operator actions, rollout status |
| Hosted API | Go modular monolith, HTTPS JSON API | Catalog publishing, operator authorization, device enrollment, commands, audit history |
| Venue multiplayer host | Separate LAN server: Go coordinator plus approved upstream services | Local lobbies, session coordination, game/platform backends, isolated service lifecycles and local persistence |
| Remote metadata | PostgreSQL | Relational catalog, artifact metadata, compatibility evidence, organizations/venues/cabinets and operations |
| Large assets | Object storage, initially Azure Blob Storage where hosting is authorized | Downloadable game assets, artwork and approved packages |
| External downloads | Approved official/provider URLs | Artifacts that are fetched from their authorized upstream source |

The cabinet interface talks to its Go core through a local Windows named pipe. The Go core talks outbound to the hosted API and approved download sources, and to the local venue coordinator for multiplayer. Depending on the verified game profile, gameplay flows between cabinet peers or through a local game service. The central dashboard talks only to the hosted API; it cannot proxy access to local multiplayer. PostgreSQL credentials never reach either interface or the cabinets.

There are three first-party Go deployment units: the desktop core, hosted catalog/fleet API, and local venue coordinator. The coordinator is the new separate-server requirement and runs only at the venue. Keep other modules within their deployment units. Existing homebrew services may use their own languages/databases; integrate them through versioned adapters instead of rewriting their network protocols. The desktop core remains the cabinet orchestration layer.

The [multiplayer design](multiplayer.md) separates same-cabinet play, native LAN/link, emulator netplay, recreated online services and a possible single-host input/display arrangement. A lobby is not assumed to own the simulation. Exact title/edition, version alignment, network dependencies and save ownership require case studies before support is advertised.

## Why these UI choices

Avalonia supports desktop C#/XAML and Windows, with other desktop targets available later. It fits the chosen C# interface requirement while leaving downloads and installation policy in Go. Gamepad navigation, focus restoration and emulator handoff still require application work and physical-device testing. See [Avalonia supported platforms](https://docs.avaloniaui.net/docs/supported-platforms).

Standalone Blazor WebAssembly keeps the browser dashboard in C#. Its static assets can be hosted alongside the Go API; it does not require a separate ASP.NET application server. This brings a browser runtime download and two UI frameworks to maintain, but cabinet and fleet interfaces have different interaction needs. Share API contracts and C# presentation utilities where useful, without duplicating Go business policy. See [Microsoft's hosting models](https://learn.microsoft.com/en-us/aspnet/core/blazor/hosting-models?view=aspnetcore-10.0).

WPF is a reasonable cabinet alternative if Windows-only support and existing WPF experience outweigh portability. Wails plus a web UI was considered: its current documentation identifies v3 as stable for desktop and requires WebView2 on Windows, but it is not the selected direction after the user's C# preference. Fyne offers a Go UI path, but would not use that preference. Python introduces another language/runtime without a clear advantage for this product. These are architecture judgments, not performance benchmark results. Sources: [Wails status](https://wails.io/status/), [Fyne documentation](https://docs.fyne.io/started/).

## Boundaries and contracts

- Keep domain/application packages independent of Avalonia, HTTP, SQL drivers and emulator-specific file formats.
- Use interfaces where external behavior varies: filesystem, artifact source, hardware probe, emulator adapter and persistence. Avoid an interface or repository wrapper for every trivial operation.
- Define the hosted API in OpenAPI; generate C# clients and check Go handlers against the contract.
- Define local message schemas separately; use JSON-RPC requests, bounded length-prefixed frames and operation progress notifications over the named pipe. Include a protocol version handshake and request IDs.
- Let the Go core enforce operator permissions and installation invariants. Hiding an operator button in C# is insufficient.
- Use stable typed errors, cancellation, deadlines and idempotency keys. Return an operation ID immediately for long-running actions.
- Never interpret catalog fields or remote commands as arbitrary shell scripts. Emulator and device adapters implement an explicit set of supported actions.

## Installation and updates

Distribute one signed setup `.exe` that installs the C# interface, Go core, uninstall metadata and prerequisites. A single installer does not require a single executable at runtime. Start with a self-contained cabinet application for predictable installations; compare a runtime-downloading bootstrapper during packaging to quantify size savings. Do not promise an installer size before measuring a release build. Self-contained .NET publishing includes the runtime, which affects size: [Microsoft deployment guidance](https://learn.microsoft.com/en-us/dotnet/core/deploying/single-file/overview).

Pin tested emulator versions per suite release. Application updates and emulator updates are independent, signed/verified operations scheduled outside gameplay. Apply coordinated UI/core updates with protocol compatibility checks aaand retain a previous application release for recovery. An application's update rollback does not imply rollback of removed game content or flashed firmware.

## Operational defaults proposed for the MVP

- Windows 11 x64 first; no permanent administrator requirement for launching or downloading games.
- One active genre per cabinet. Switching is a maintenance operation between games; switching among already installed games is fast.
- Installed single-player/couch modes remain playable without an API or venue-server connection. Verified venue multiplayer works without Internet but may require the LAN server. New cloud downloads and central actions require connectivity; missing local imports can be supplied locally.
- Outbound polling for fleet commands, with backoff and jitter, and no public cabinet management ports. Approved multiplayer profiles may open narrowly scoped LAN listeners. Disable public lobby/relay/traversal and automatic router mapping paths; enforce IPv4/IPv6 boundaries at the host and network.
- Local operator requests and remote requests enter the same durable command queue. No simultaneous mutations of a cabinet library.
- Signed catalog manifests, immutable artifact hashes, limited database roles, scoped operator/device credentials and structured redacted logs.
- Persistent player/save data stays outside disposable game/package directories. Guests share cabinet saves/unlocks; an explicit operator action can reset them.
- User-added local-only games are stored in the cabinet library and are not inserted into the shared catalog. Keep their files outside automatic genre cleanup unless the user explicitly assigns managed ownership.

## Catalog authority

`Sengoku-Admin` is an application identity role, independent of any PostgreSQL login or OS administrator. Only that role may add shared game records through the API, imports or administrative tooling. Recommend applying the same restriction to shared catalog edits, deletion and publication so another mutation route cannot bypass ownership. Ordinary operators can choose published suites and manage authorized cabinets; users with local-library access can add local entries without becoming catalog administrators. Enforce these rules in Go and database access boundaries, not just in the dashboard.

## Commercial dependencies

Catalog entries must record acquisition permissions and suitability for the intended commercial use separately. A free download or ownership of a copy does not itself settle the terms for redistribution or venue use. User imports are local; the prototype does not automatically upload or share them between cabinets.

DuckStation is not a default bundled dependency for the commercial MVP. Its current README identifies CC-BY-NC-ND terms and treats preconfigured packages/settings as modifications. Resolve applicable permission before making it a managed commercial integration; linking to a download does not by itself settle the intended use. See [DuckStation's README](https://github.com/stenzek/duckstation/blob/master/README.md).

For other emulators, retain the relevant license, notices and source obligations for the exact release used. A generic emulator download URL is not sufficient publication metadata.

## Prototype versus deployed cabinet

The reference listing includes Windows 11 Home. The user selected a full-screen application for the prototype and OS lockdown later. A full-screen launcher with an operator PIN is not an OS security boundary. Windows Assigned Access is available on specified Pro/Enterprise/Education/IoT editions; Shell Launcher has a narrower edition list and requires additional access policies. Select the deployed cabinet OS before implementing that later lockdown work. Sources: [reference laptop](https://www.newegg.com/p/N82E16834156554), [Assigned Access](https://learn.microsoft.com/en-us/windows/configuration/assigned-access/), [Shell Launcher](https://learn.microsoft.com/en-us/windows/configuration/shell-launcher/).
