CREATE TABLE emulator_packages (
    slug TEXT PRIMARY KEY NOT NULL,
    version TEXT NOT NULL,
    package_sha256 TEXT NOT NULL,
    executable_path TEXT NOT NULL,
    executable_sha256 TEXT NOT NULL,
    manifest_json TEXT NOT NULL CHECK (json_valid(manifest_json)),
    installed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
