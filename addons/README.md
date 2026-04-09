# DataProtection Addon Run Packages

This repository now carries offline addon packages under [addons/mysql](C:/Users/admin/Desktop/release/dataprotection/addons/mysql), [addons/redis](C:/Users/admin/Desktop/release/dataprotection/addons/redis), [addons/minio](C:/Users/admin/Desktop/release/dataprotection/addons/minio), and [addons/milvus](C:/Users/admin/Desktop/release/dataprotection/addons/milvus).

Each addon package builds its own multi-arch `.run` installer and only does three things:

- imports the addon runner image tarballs into the local registry
- applies the cluster-scoped `BackupAddon` manifest
- ships ready-to-edit sample `BackupSource`, `BackupJob`, `BackupPolicy`, and optional `RestoreJob` YAMLs

Current addon matrix:

- `mysql`: backup + restore
- `redis`: backup only
- `minio`: backup + restore
- `milvus`: backup + restore (beta)

The notification component is packaged by the core installer. `data-protection-operator-<arch>.run install` already deploys both:

- `data-protection-operator-controller-manager`
- `data-protection-notification-gateway`

Use [config/samples/quickstart/06-notificationendpoint.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/06-notificationendpoint.yaml) to connect policies or jobs to webhook delivery.
