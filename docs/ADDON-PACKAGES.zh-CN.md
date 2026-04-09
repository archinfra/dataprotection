# Addon Packages

The v2 control plane no longer embeds MySQL, Redis, MinIO, or Milvus business logic into the main controller reconcile path.

Instead, this repository now carries separate offline addon packages under [addons](C:/Users/admin/Desktop/release/dataprotection/addons):

- [mysql](C:/Users/admin/Desktop/release/dataprotection/addons/mysql)
- [redis](C:/Users/admin/Desktop/release/dataprotection/addons/redis)
- [minio](C:/Users/admin/Desktop/release/dataprotection/addons/minio)
- [milvus](C:/Users/admin/Desktop/release/dataprotection/addons/milvus)

Each package contains:

- an offline multi-arch runner image bundle
- a cluster-scoped `BackupAddon` manifest
- example `BackupSource`, `BackupJob`, `BackupPolicy`, and optional `RestoreJob` YAMLs

Build a package locally:

```bash
cd addons/mysql
./build.sh --arch amd64
./build.sh --arch arm64
```

Install an addon into a cluster:

```bash
./dist/dataprotection-addon-mysql-amd64.run install -y
```

Export samples:

```bash
./dist/dataprotection-addon-mysql-amd64.run samples --output-dir ./samples/mysql
```

The root workflow [addon-installers.yml](C:/Users/admin/Desktop/release/dataprotection/.github/workflows/addon-installers.yml) builds and releases these packages for both `amd64` and `arm64`.
