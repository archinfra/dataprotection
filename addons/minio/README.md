# MinIO addon run package

This package installs the cluster-scoped `BackupAddon/minio-mirror` resource and ships the runner image needed by MinIO source backup and restore jobs.

Build:

```bash
cd addons/minio
./build.sh --arch amd64
./build.sh --arch arm64
```

Install:

```bash
./dist/dataprotection-addon-minio-amd64.run install -y
```

Export samples:

```bash
./dist/dataprotection-addon-minio-amd64.run samples --output-dir ./samples/minio
```

The package includes examples for:

- `BackupSource`
- `BackupJob`
- `BackupPolicy`
- `RestoreJob`

The core `dataprotection` installer must already be installed before you apply those samples.
