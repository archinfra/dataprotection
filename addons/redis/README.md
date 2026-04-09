# Redis addon run package

This package installs the cluster-scoped `BackupAddon/redis-rdb` resource and ships the runner image needed by Redis backup jobs.

Build:

```bash
cd addons/redis
./build.sh --arch amd64
./build.sh --arch arm64
```

Install:

```bash
./dist/dataprotection-addon-redis-amd64.run install -y
```

Export samples:

```bash
./dist/dataprotection-addon-redis-amd64.run samples --output-dir ./samples/redis
```

The package includes examples for:

- `BackupSource` for standalone and cluster
- `BackupJob`
- `BackupPolicy`

The core `dataprotection` installer must already be installed before you apply those samples.
