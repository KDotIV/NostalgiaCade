# Catalog administration API

`ncade-api.exe` is a trusted admin service, built by `scripts/Build.ps1` or independently with `go build ./cmd/ncade-api`. It is not bundled with the desktop. All catalog/admin routes require `Authorization: Bearer <token>`; health probes are anonymous. Configure `NCADE_ADMIN_TOKEN` with a random secret of at least 32 characters and `NCADE_ADMIN_SUBJECT` with your administrator identity. The service maps that credential to **Sengoku-Admin**; request bodies and role headers cannot assign roles. This is a single-administrator credential, not an OIDC login or a multi-user identity service.

For the configured Windows workflow, use `scripts/Manage-CatalogApi.ps1 -Action Start`. See [local/Azure hosting](api-hosting.md) for encrypted token storage, manual process management, container configuration and deployment. Import the [Postman collection and environments](../postman/README.md) to exercise all completed endpoints.

Set these variables in the server's environment or secret manager, then run:

```powershell
$env:NCADE_POSTGRES_DSN = [Environment]::GetEnvironmentVariable('NCADE_POSTGRES_DSN','User')
$env:NCADE_ADMIN_SUBJECT = 'Sengoku-Admin'
# Supply NCADE_ADMIN_TOKEN securely in this process; never put it in the repository.
./artifacts/bin/ncade-api.exe
```

The default listener is `127.0.0.1:8090`. A listener outside loopback requires `--tls-cert` and `--tls-key`, or explicit `--behind-tls-proxy` with a controlled HTTPS ingress. Use a dedicated runtime database login with catalog schema usage; SELECT on the catalog; INSERT on artifacts, artifact_sources and game_edition_artifacts; and the UPDATE privilege needed for `SELECT FOR KEY SHARE` on game_editions. Do not distribute database or admin API credentials to cabinets. Both Go and C# child-process launchers strip the administrative environment variables.

## Routes

| Method and path | Result |
| --- | --- |
| `GET /health/live` | Anonymous process liveness |
| `GET /health/ready` | Anonymous database/schema readiness; 503 when unavailable or draining |
| `POST /v1/admin/artifacts` | Inserts an artifact, HTTPS source and optional edition association atomically |
| `GET /v1/catalog/artifacts/{id}` | Returns type, byte counts, checksum, format, permission and a download URL |
| `GET /v1/catalog/emulators` | Latest reviewed Windows/x64 emulator package manifests, including download URLs |
| `GET /v1/catalog/genres` | Arcade Suite genre identifiers and display order |
| `GET /v1/catalog/games?genre=racing` | Game/edition IDs and genre membership; bounded to 1,000 editions |
| `GET /v1/catalog/emulators/{slug}/onboarding` | Emulator onboarding instructions |

Catalog/admin routes currently serve the administrator. The desktop still talks to the local Go core over IPC; trusted catalog tooling builds its installation manifests. These endpoints do not introduce a cabinet authentication protocol.

## Insert an artifact

Send `Content-Type: application/json` and one object:

```json
{
  "kind": "game",
  "bytes": 1048576,
  "installedBytes": 1048576,
  "url": "https://downloads.example.org/game.iso",
  "sha256": "replace-with-the-64-character-lowercase-sha256",
  "format": "iso",
  "permissionStatus": "approved",
  "licenseEvidenceUrl": "https://example.org/distribution-permission",
  "editionId": "replace-with-an-edition-uuid-from-the-games-endpoint",
  "filename": "game.iso"
}
```

Replace the example values with real metadata. Supported kinds are `game`, `save_data`, `firmware`, `emulator`, `utility` and `artwork`. `bytes` must be positive. `installedBytes` defaults to zero when not measured. SHA-256, format and an HTTPS URL are required. `permissionStatus` defaults to `unreviewed`; `approved` needs an HTTPS evidence URL. An edition association is optional; include `filename` when supplying `editionId`. This endpoint records metadata; it does not upload file bytes, certify a URL's contents, create a new game, or publish an emulator release. Publish complete emulator manifests with `ncade-catalog --action publish` so executable paths, allowed hosts and requirements remain validated.

```powershell
$headers = @{ Authorization = "Bearer $env:NCADE_ADMIN_TOKEN" }
Invoke-RestMethod http://127.0.0.1:8090/v1/catalog/games?genre=racing -Headers $headers
Invoke-RestMethod http://127.0.0.1:8090/v1/admin/artifacts `
  -Method Post -Headers $headers -ContentType application/json `
  -InFile ./artifact.json
```

Success returns **201**, the created artifact and a `Location` header. Invalid or unknown fields return **400**, missing/invalid authentication **401**, missing editions **404**, duplicate `(kind, sha256)` **409**, oversized bodies **413**, and the wrong content type **415**. Database failures return a generic **500** without SQL values or credentials. Duplicate submissions do not change existing content or permissions.

Approved game artifacts linked to seeded editions can be selected by `ncade-pilot --action plan` when no local binding is supplied. Saves, firmware and research observations are excluded from game acquisition. Before installation, configure the download hostname through the existing local setup guide; redirects also need approved hosts. The core continues to verify file size/hash and enforce adapter formats. Archived game containers still need a supported playable image; metadata insertion does not add extraction support.

## Schema cleanup

Authorized builds import the [private migration bundle](private-migrations.md) first. Fresh databases use `ncade-migrate --database postgres` (migrations 001–005). For an existing manually provisioned foundation, back up first and run:

```powershell
./artifacts/bin/ncade-migrate.exe --database postgres --cleanup-catalog
```

The cleanup uses a transaction, advisory lock, lock timeout and explicit table drops without `CASCADE`. Unexpected dependencies abort it. It consolidates pilot metadata before dropping duplicate tables, keeps onboarding and pinned releases, removes remote fleet/reporting state and accidental remote cabinet tables, and preserves the previous pilot ledger in `catalog.legacy_pilot_migrations`. The manual foundation gets `catalog.cleanup_migrations`; it does not receive invented historical foundation checksums. Original numbered migrations remain immutable.

Back up with PostgreSQL's custom format and validate the archive before applying changes. Restore first to a separate database when recovering, then validate catalog data and grants before switching the service. PostgreSQL documents the supported archive/restore formats in [pg_dump](https://www.postgresql.org/docs/current/app-pgdump.html).

Checks: `scripts/Test.ps1` covers the Go build, vet, unit tests and .NET/IPC/desktop integration; `scripts/Test-Postgres.ps1` covers fresh and legacy migrations, rollback on unexpected dependencies, checksum validation, retained catalog data, publication, approved-download planning and artifact API transactions.
