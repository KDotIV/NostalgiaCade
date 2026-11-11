# Private migration delivery

Migration SQL is ignored and removed from the current Git index. Existing local
SQL is preserved. Historical commits still contain the nine previously tracked
files; this change does not rewrite history or revoke existing copies.

The public repository contains only a placeholder and
`build/migrations.lock.json`, which records filenames and normalized SHA-256
checksums. The private bundle contains ten current PostgreSQL/SQLite migration
files. It contains no database passwords. Keep bundles in access-controlled
storage and supply them only to trusted build/deployment jobs.

```powershell
# On an authorized machine with the SQL already present:
./scripts/Private-Migrations.ps1 -Action Check
./scripts/Private-Migrations.ps1 -Action Export -ArchivePath artifacts/private/ncade-migrations.zip

# On another authorized checkout, after receiving the bundle privately:
./scripts/Private-Migrations.ps1 -Action Import -ArchivePath <private-bundle.zip>
./scripts/Build.ps1
./scripts/Test.ps1 -Postgres
```

Export refuses to overwrite an existing archive. Import checks every path and
checksum, rejects extra/missing/duplicate entries, and preserves edited local SQL.
It verifies the complete bundle before writing. Line endings normalize to LF,
matching the migration ledger. Build fails early with an actionable error if any
required migration is absent or changed. It never downloads SQL from a public URL.

For a new migration, retain immutable applied files, add a numbered SQL file,
update the checksum manifest with the normalized SHA-256 of the new file, export
a new private bundle, and run integration tests before deployment. Keep earlier
bundles for rebuilding earlier application versions.

## Private CI setup

The workflow always builds/tests the independent API without SQL. On November 11,
2026, `KDotIV/NostalgiaCade` was configured with the `private-build` environment,
an exact `main` branch restriction, `KDotIV` as required reviewer, and the
environment-only `NCADE_MIGRATIONS_ZIP_BASE64` secret. The repository variable
`NCADE_PRIVATE_BUILDS_ENABLED` enables the desktop and database jobs. Both jobs
must explicitly reference `environment: private-build` to access that secret.

For a new repository, reproduce these settings:

1. Create a protected GitHub environment named `private-build`, restrict it to
   the trusted default branch, and require approval for its secrets.
2. Store the base64-encoded ZIP as its `NCADE_MIGRATIONS_ZIP_BASE64` secret. Base64
   is transport encoding; GitHub's secret storage/access controls provide privacy.
3. Set the repository variable `NCADE_PRIVATE_BUILDS_ENABLED` to `true`.

For each trusted `main` run, open **Actions → Foundation checks → Review
deployments**, select `private-build`, and approve it after reviewing the commit.
The owner may approve their own run, which is necessary for this single-owner
repository. Approval releases both private jobs; pull requests do not qualify.

To prepare the value locally without printing SQL/base64 into a terminal log:

```powershell
$bundle = [IO.File]::ReadAllBytes((Resolve-Path artifacts/private/ncade-migrations.zip))
Set-Clipboard -Value ([Convert]::ToBase64String($bundle))
# Paste into the protected environment secret, then clear the clipboard.
```

The jobs run only for the default branch on push/manual dispatch. Pull requests
never receive this bundle. Go caching is disabled in private jobs because compiled
packages embed SQL. Do not upload private SQL, the archive, or binaries
embedding it as public CI artifacts. The current ZIP is small enough for a secret;
move to authenticated private artifact storage if the bundle grows beyond GitHub's
secret size limit. When updating the bundle, replace the environment secret rather
than creating a repository-wide secret, and verify the corresponding checksum
manifest before approving the next run.

See GitHub's [environment protection](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)
and [secret limits](https://docs.github.com/en/actions/reference/security/secrets).

The local desktop needs its SQLite schema to install and run; authorized binaries
embed migration assets. Embedded SQL can be recovered from distributed binaries.
Keeping source assets private controls source distribution, not database access.
Runtime credentials, network access, least-privilege grants and validated handlers
remain the deployed security boundaries. A public checkout alone intentionally
cannot build a complete working desktop until its owner supplies this bundle.
