# Private migration assets

SQL is supplied privately and ignored by Git. This placeholder lets public
checkouts compile the independent API without shipping database provisioning SQL.
Desktop and migration-tool builds require the complete verified bundle; see
[private migration delivery](../../../docs/private-migrations.md).

Do not edit applied migrations. Add a numbered migration, update the private
bundle and its public checksum manifest together, then run the database tests.
