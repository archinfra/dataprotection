# MySQL addon run package

This package is kept as a compatibility delivery for standalone addon installation.

For real project delivery, the recommended flow is now:

- install the core `dataprotection` platform first
- then install MySQL through `apps_mysql`
- let the MySQL installer register `BackupAddon/BackupSource/BackupPolicy` automatically

That recommended path avoids teaching the on-site team two separate delivery entries, and it no longer requires a separately delivered addon runner image.

This package still installs the cluster-scoped `BackupAddon/mysql-dump` resource for clusters that explicitly want the legacy standalone addon workflow.

Build:

```bash
cd addons/mysql
./build.sh --arch amd64
./build.sh --arch arm64
```

Install:

```bash
./dist/dataprotection-addon-mysql-amd64.run install -y
```

Export samples:

```bash
./dist/dataprotection-addon-mysql-amd64.run samples --output-dir ./samples/mysql
```

The package includes examples for:

- `BackupSource`
- `BackupJob`
- `BackupPolicy`
- `RestoreJob`

The core `dataprotection` installer must already be installed before you apply those samples.

`RestoreJob` now supports both:

- `snapshotRef` for platform-managed snapshots
- `importSource` for offline export packages, directories, or single files stored in `BackupStorage`
