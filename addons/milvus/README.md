# Milvus addon run package

This package installs the cluster-scoped `BackupAddon/milvus-backup` resource and ships the runner image needed by Milvus backup and restore jobs.

Build:

```bash
cd addons/milvus
./build.sh --arch amd64
./build.sh --arch arm64
```

Install:

```bash
./dist/dataprotection-addon-milvus-amd64.run install -y
```

Export samples:

```bash
./dist/dataprotection-addon-milvus-amd64.run samples --output-dir ./samples/milvus
```

The package includes examples for:

- `BackupSource`
- `BackupJob`
- `BackupPolicy`
- `RestoreJob`

This addon is marked beta because it depends on the upstream `milvus-backup` CLI behavior and has not been cluster-validated in this workspace.

The core `dataprotection` installer must already be installed before you apply those samples.
