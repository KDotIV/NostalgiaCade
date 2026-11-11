# Running the catalog API locally and on Azure

The same Go executable runs as a foreground process or a container. No startup
entry, scheduled task or Windows service is required. The Windows credential
helper is optional; the service itself is portable and configured by environment.

## Local Windows workflow

```powershell
./scripts/Build.ps1 # Requires the private migration bundle for the full desktop build
./scripts/Manage-CatalogApi.ps1 -Action Configure
./scripts/Manage-CatalogApi.ps1 -Action Start
./scripts/Manage-CatalogApi.ps1 -Action Status
./scripts/Manage-CatalogApi.ps1 -Action Stop
```

To build only the API, use `go build -trimpath -o artifacts/bin/ncade-api.exe
./cmd/ncade-api`. This does not need migrations or the .NET SDK.

Configure reads your existing `NCADE_POSTGRES_DSN`, creates a random 256-bit admin
token on first use, and preserves it on subsequent calls. It stores the DSN and
token using Windows DPAPI under `%LOCALAPPDATA%/NCade/CatalogApi`; another Windows
user/machine cannot decrypt that export. It copies the API into that directory so
rebuilding the repository does not overwrite a running executable. Start launches
the copied binary in the background until Stop, process failure or Windows exit.
It does not restart after a crash or login. Rebuild, Configure, Start to update it.

`CopyToken` places the token on the clipboard for Postman Local Vault. `RotateToken`
generates a replacement and restarts the API if it was running; update Postman
after rotation. Logs stay in the runtime directory. Local Stop terminates the
background Windows process; use the foreground executable and Ctrl+C to exercise
graceful shutdown. PostgreSQL rolls back uncommitted transactions after termination.

For a foreground process on Windows or Linux, inject the following environment
variables through your shell's credential tooling, then run `ncade-api`. The
service accepts no secret command-line flags.

| Variable | Purpose/default |
| --- | --- |
| `NCADE_POSTGRES_DSN` | Required pgx PostgreSQL connection; Azure requires `sslmode=verify-full` |
| `NCADE_ADMIN_TOKEN` | Required bearer secret, at least 32 characters; prefer 32 random bytes |
| `NCADE_ADMIN_SUBJECT` | Required audit identity, e.g. `Sengoku-Admin` |
| `NCADE_API_LISTEN` | `127.0.0.1:8090` for native local execution |
| `NCADE_API_TLS_CERT`, `NCADE_API_TLS_KEY` | Paired PEM paths for direct TLS |
| `NCADE_API_BEHIND_TLS_PROXY` | `false`; enable only behind a controlled TLS-terminating ingress |

Listeners outside loopback require direct TLS or explicit trusted-proxy mode.
Proxy mode requires `X-Forwarded-Proto: https` for catalog requests; isolate its
container port from direct clients. The header alone is not proof of TLS when a
client can bypass ingress. Remote configurations also verify the database TLS
certificate. Health probes are anonymous and expose only status, not SQL errors.

## Azure Container Apps

The [Dockerfile](../Dockerfile) builds a Linux binary and a non-root image with CA
certificates. Its build context excludes migrations, credentials, backups and
desktop files. Build with `docker build -t ncade-api:local .`; supply TLS mounts or
a TLS reverse proxy when running locally in Docker. The native loopback process
is simpler for everyday local development.

[deploy/azure/catalog-api.bicep](../deploy/azure/catalog-api.bicep) declares the API
against an existing Container Apps environment, ACR and user-assigned managed
identity. Before deployment, provide that identity with image-pull permission and
access to the specific Key Vault secrets. Configure database networking and a
dedicated runtime database login separately. The template does not create or
migrate the database or create a privileged runtime login.

Supply `environmentId`, `image` (prefer an immutable digest), `registryServer`,
`identityResourceId`, `postgresSecretUrl`, `adminTokenSecretUrl`, and `adminCidrs`
through a deployment parameter file containing resource identifiers, not secrets.
Use a distinct token per environment. Restrict ingress to your admin public CIDRs.
Set `minReplicas=1` if cold starts are undesirable; its default is zero.

```powershell
az deployment group create --resource-group <existing-resource-group> `
  --template-file deploy/azure/catalog-api.bicep `
  --parameters '@path/to/parameters.json'
```

Use a JSON parameter file with the command above; for a `.bicepparam` file use
`--parameters <file.bicepparam>` following Azure CLI's parameter-file syntax.

Azure ingress terminates HTTPS, the container listens on port 8080, Key Vault
references supply secrets, and startup/liveness/readiness probes use `/health`.
The process handles SIGTERM and allows up to 20 seconds for active requests to
finish before closing its pool. The template allows 30 seconds before termination.
Logs are JSON on stdout with request IDs and no tokens, request bodies or raw SQL.

Run schema deployment as a separate private step using the migration identity.
The API runtime only needs catalog reads and artifact insertion/association
permissions described in [catalog-admin-api.md](catalog-admin-api.md). After
deployment set Postman's Azure `baseUrl` to the template output and supply the
Azure-specific token in Local Vault. There is no browser login/refresh endpoint.

The template has been compiled locally and the API cross-compiled for Linux.
Container execution and an Azure deployment remain to be verified in their target
environment; this workspace has no Docker engine. No Azure resources were deployed.

References: [Container Apps ingress](https://learn.microsoft.com/en-us/azure/container-apps/ingress-overview),
[health probes](https://learn.microsoft.com/en-us/azure/container-apps/health-probes),
[Key Vault references](https://learn.microsoft.com/en-us/azure/container-apps/manage-secrets).
