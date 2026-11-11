# Go project structure for C# developers

NCade uses one Go module (`go.mod`), several executable packages under `cmd`, and
reusable implementation packages under `internal`. This is the conventional
[Go server project layout](https://go.dev/doc/modules/layout).

| Go concept | Closest C# comparison |
| --- | --- |
| `go.mod` | Dependency/version boundary; not a separate project per executable |
| `cmd/ncade-api/main.go`, `package main` | An executable project with `Program.cs` |
| `internal/catalogapi`, `package catalogapi` | An implementation library with a compiler-enforced import boundary |
| Exported identifiers starting with capitals | Public members within the package's permitted import boundary |
| Small consumer-owned interfaces | Interfaces introduced at actual substitution/test seams |

`ncade-api`, `ncade-core`, and `ncade-catalog` are executable names, not competing
namespaces. Each binary has one `main` package. Shared behavior belongs in an
importable package, never in another executable's `main` package. One module is
appropriate while these components share versions and release together; separate
modules or microservices would add coordination without a current requirement.

| Command | Responsibility | Distribution |
| --- | --- | --- |
| `ncade-api` | HTTP catalog reads and administrator artifact insertion | Local admin process or Azure container |
| `ncade-core` | Local SQLite, installation jobs, emulators and named-pipe IPC | Cabinet desktop bundle |
| `ncade` | Operator commands through local IPC | Cabinet desktop bundle |
| `ncade-catalog` | Trusted catalog publication/export | Administrative tooling |
| `ncade-migrate` | Explicit, versioned database deployment | Private deployment tooling |
| `ncade-pilot` | Temporary PostgreSQL-to-local installation verification | Trusted pilot tooling; not the production cabinet protocol |

The obsolete root Cobra scaffold has been removed. Run a specific command, such
as `go run ./cmd/ncade-api`; `go run .` no longer represents an application.

The HTTP composition is `cmd/ncade-api` → `internal/apihost` →
`internal/catalogapi` and `internal/postgres`. The entry point supplies environment
configuration, JSON logging and shutdown signals. The host owns HTTP timeouts,
health checks and lifecycle. Catalog handlers own request validation,
authorization and catalog transactions. The remote PostgreSQL connector is
separate from cabinet SQLite and embedded migrations, so the API binary has no
SQLite driver or migration DDL dependency.

Keep packages organized by capability instead of copying C# folders for every
controller/service/repository class. The current small catalog API does not need
an interface for each SQL operation or a dependency injection framework. Extract
application services when HTTP and another real transport need the same workflow.
Use versioned HTTP DTOs for a future generated C# client; keep those contracts
separate from local IPC and database records.

The implemented remote scope is catalog metadata, genres, artifacts and emulator
onboarding. Fleet, venue and multiplayer designs are future milestones and do not
justify empty remote tables now. The desktop continues to work through its local
core; it does not receive the administrative token or PostgreSQL credentials.

Before a public commercial launch, complete the deployment's runtime database
role, restore rehearsal, container execution/load checks, and installer signing
and physical-cabinet testing. Introduce scoped cabinet authentication and
OIDC/Entra identity when clients or additional administrators need API access;
the current token is deliberately a single administrator credential. These are
remaining release gates, not features supplied by a folder layout.

See [API hosting](api-hosting.md), [Postman setup](../postman/README.md), and
[private migration delivery](private-migrations.md).
