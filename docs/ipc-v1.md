# Local IPC protocol v1

Transport: Windows byte-mode named pipe `\\.\pipe\NostalgiaCade.<random>`, published in the cabinet data directory's `core-endpoint.json`. Only the current Windows user can connect; remote clients are rejected. There is no TCP listener or PostgreSQL credential in the desktop.

Every message is a 4-byte unsigned little-endian payload length followed by UTF-8 JSON. Length must be 1 through 1,048,576 bytes. Reads/writes must handle fragmentation. Invalid framing closes the connection. The C# client closes a connection after transport, protocol or timeout failures so a late response cannot be mistaken for a subsequent request.

Requests use JSON-RPC 2.0 objects with a nonempty string or signed 64-bit integer `id`; its serialized representation is at most 128 bytes. The C# client uses string IDs. Responses echo the ID and contain either `result` or `error`. This version supports ordered request/response exchanges only; batches and notifications are not supported. The mandatory first call is `system.hello` with matching protocol version `1`.

| Method | Params | Result |
| --- | --- | --- |
| `system.hello` | `{ "protocolVersion": 1 }` | `{ "protocolVersion": 1, "coreVersion": "0.1.0-dev" }` |
| `system.status` | omitted or null | `version`, `protocolVersion`, `platform`, `databaseVersion`, `localGameCount`, `readiness`, `uptimeSeconds` |
| `catalog.genres` | omitted or null | Array of `{ "id", "name", "targetEntries" }` |
| `system.shutdown` | omitted or null | Stops only when no mutation/game is active; busy requests fail |

For example, the JSON body after the binary header is:

```json
{"jsonrpc":"2.0","id":"1","method":"system.hello","params":{"protocolVersion":1}}
```

```json
{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1,"coreVersion":"0.1.0-dev"}}
```

Errors use `{ "code": number, "message": string }`: `-32700` parse failure, `-32600` invalid request, `-32601` unknown method, `-32602` invalid handshake parameters, `-32603` internal query failure, `-32001` incompatible protocol and `-32002` handshake required. Invalid/unusable IDs receive null. No SQL or credentials are returned in query errors.

The server accepts at most 16 clients, with a 5-second initial handshake deadline, 90-second idle read deadline and 5-second request/write deadlines. The C# client allows 1.5 seconds to connect and 5 seconds per exchange. A desktop startup can wait up to 15 seconds for discovery. Idle connections may close; Refresh reconnects.

## Local workflow extension (core 0.2)

The wire framing remains v1; the following additive methods require core 0.2. Parameter models are defined in `internal/library/types.go`, with concrete examples in `examples/`.

| Method | Parameters | Result |
| --- | --- | --- |
| `setup.status` | null | Configured flag, optional common root, resolved storage paths and approved download hosts |
| `setup.configure` | Setup object | Validated persisted setup |
| `emulators.register` | ID, adapter, executable, version | Registered local profile |
| `library.games` | null | Installed local/active-suite games |
| `library.plan` | Manifest | Revision, replacement entries and space/retention plan |
| `operations.submit` | Key, kind (`import`/`switch`), matching request body | Durable operation ID/state; work continues independently |
| `operations.get` / `operations.retry` | `{ "id": "..." }` | Operation state, phase, error and serialized result |
| `operations.cancel` | `{ "id": "..." }` | Cancellation accepted; poll until terminal |
| `session.launch` | `{ "id": "game-id" }` | Session ID, PID and state |
| `session.status` | null | Current/last session state and exit code |
| `session.stop` | `{ "force": false, "expectedSessionId"?: "..." }` | Graceful stop requested; poll for exit. Optional expected ID rejects a stale request under the session lock. |
| `session.acknowledge` | `{ "confirmed": true }` | Operator reconciliation of an ambiguous interrupted launch |

`setup.configure` accepts either `root` with optional `downloadHosts`, or the existing `paths` object with six absolute folders. Root mode resolves `Emulators`, `ROMs`, `Firmware`, `Saves`, `Settings`, and `Cache` beneath the supplied root. If both forms are supplied, individual paths must match those derived from the root. Responses include the resolved paths and, in root mode, the root. Storage folders must not overlap each other or contain the application-state directory; they may share that directory as their parent. Normalization is shared with the CLI, while the core repeats validation before creating folders and persisting settings. This additive field uses the existing settings JSON and requires no database migration.

Workflow parameters reject unknown fields. Typed domain failures use `-32010` and a stable code prefix in the message (`setup_required`, `busy`, `game_running`, `stale_plan`, etc.). Long file operations return immediately and are polled; the five-second RPC deadline does not cancel a submitted job. Operations are serialized, idempotency keys reject changed payloads, and cancellation does not restore removed ROMs. Explicit retry reconciles journaled state after restart.

There are no shared-catalog or arbitrary-shell mutation routes. Access currently means the trusted Windows user; venue/operator authentication remains a later milestone. The multiplayer server is not implemented or exposed here.

## Emulator packages (core 0.3)

`operations.submit` additionally accepts kind `emulator_install` with `install: { package: <reviewed package manifest>, archivePath?: <local archive> }`. Exactly one request body is permitted. It runs through the same durable queue, busy/session gate and retry/cancel methods. `emulators.installed` returns installed slug, version, executable, deployment review and `launchSupported`. All five reviewed packages register profiles; non-Dolphin profiles require preparation/prerequisites before gameplay. No catalog database credentials or remote publication methods are exposed over cabinet IPC. Package fields and URL/hash policy are specified by `internal/emucatalog`.

## Emulator onboarding (core 0.4)

| Method/body | Parameters | Result |
| --- | --- | --- |
| `emulators.profile` | `{ "id": "pcsx2" }` | Paths, prepared/ready flags, missing prerequisites and operator guidance; read-only |
| `emulators.configure` | `{ "id": "pcsx2", "firmwarePath"?: "absolute PS3 PUP path" }` | Tracked native settings/firmware session; preparation required |
| `operations.submit` kind `emulator_prepare` | `prepare: { "id": "pcsx2" }` | Durable operation creating an isolated profile without rewriting existing configuration |
| `operations.submit` kind `firmware_import` | `firmware: { "id", "kind", "path" }` | Durable local copy operation; kind `bios`, `ps3-flash`, `switch-keys` or `switch-firmware` |

Session results add `purpose` (`game`, `configure`, `firmware`) and `emulatorId`; non-game sessions have an empty `gameId`. Existing stop/status/recovery methods apply. Native settings sessions exclude library changes and other launches. A `ready` profile has structurally present prerequisites and expected storage routing; actual game/firmware compatibility still requires emulator validation. Eden NRO launch bypasses the console key/firmware requirement, but never profile preparation or routing checks.

SQLite migration 005 widens the adapter constraint, preserves existing game foreign keys, creates profiles for installed packages, records the prepared executable hash, and adds session purpose/profile metadata. PostgreSQL schemas are unchanged. No arbitrary shell arguments or configuration scripts are accepted over IPC.
