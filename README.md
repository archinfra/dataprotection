# Data Protection Operator

`dataprotection` is a standalone Kubernetes operator repository for backup and restore control planes.

Current scope:

- a generic CRD model for data protection
- idempotent reconciliation for backup schedules and one-shot runs
- a built-in MySQL runtime as the first real data driver
- a Stash-inspired storage split between reusable `BackupStorage` and logical `BackupRepository`
- online image publishing and offline `.run` installation

## Documentation

- current project status and business logic: [docs/PROJECT-STATUS.zh-CN.md](docs/PROJECT-STATUS.zh-CN.md)
- architecture insights inspired by KubeStash concepts: [docs/KUBESTASH-INSIGHTS.zh-CN.md](docs/KUBESTASH-INSIGHTS.zh-CN.md)
- runtime model inferred from public KubeStash installer resources: [docs/KUBESTASH-RUNTIME-MODEL.zh-CN.md](docs/KUBESTASH-RUNTIME-MODEL.zh-CN.md)
- design notes borrowed from open-source Stash: [docs/STASH-INSIGHTS.zh-CN.md](docs/STASH-INSIGHTS.zh-CN.md)
- manual validation and acceptance plan: [docs/MANUAL-TEST-PLAN.zh-CN.md](docs/MANUAL-TEST-PLAN.zh-CN.md)
- development guide: [docs/DEVELOPMENT.zh-CN.md](docs/DEVELOPMENT.zh-CN.md)
- environment guide: [docs/ENVIRONMENT.zh-CN.md](docs/ENVIRONMENT.zh-CN.md)
- publishing guide: [docs/PUBLISHING.zh-CN.md](docs/PUBLISHING.zh-CN.md)

## CRDs

- `BackupStorage`
- `BackupSource`
- `BackupRepository`
- `BackupPolicy`
- `BackupRun`
- `Snapshot`
- `RetentionPolicy`
- `RestoreRequest`

## What Works Today

- `BackupStorage` / `BackupSource` / `BackupRepository` reconcile basic readiness status
- `BackupRepository` can resolve backend settings from `spec.storageRef`
- `BackupRepository.spec.path` appends a logical subpath on top of shared NFS/S3 storage
- legacy inline repository backend fields still work for compatibility
- `BackupPolicy` renders one `CronJob` per repository and cleans up stale children
- `BackupPolicy` can resolve retention from `spec.retentionPolicyRef`
- `BackupRun` renders one `Job` per repository
- `BackupRun` also records one `Snapshot` object per repository target
- `RestoreRequest` renders one restore `Job`
- `RestoreRequest` can resolve restore inputs from `snapshotRef`
- child `Job` phase and message are aggregated back into CRD status
- reconciliation is idempotent and naming is stable under truncation

## Built-In MySQL Runtime

When `BackupSource.spec.driver=mysql` and the execution template does not override `command/args`, the operator renders a built-in MySQL runtime instead of the placeholder runner.

Supported today:

- `mysqldump` logical backup
- restore from `.sql.gz`
- NFS repositories
- S3 / MinIO repositories
- backup all user databases
- backup selected databases
- backup selected tables with `database.table`
- restore mode `merge`
- restore mode `wipe-all-user-databases`
- checksum verification and retention pruning

## Repository Layout

- `api/v1alpha1`: API types and validation
- `controllers`: reconcilers and built-in runtimes
- `config/crd/bases`: generated CRDs
- `config/samples`: sample resources
- `manifests`: install templates for the operator
- `scripts/install`: `.run` installer source modules
- `.github/workflows`: CI, image publishing, installer artifacts

## Local Development

```bash
bash hack/bootstrap-dev-env.sh
make generate
make manifests
make test
make build
```

Build offline installers:

```bash
bash build.sh --arch amd64
bash build.sh --arch all
```

Recommended release path:

- use GitHub Actions for multi-arch images and `.run` artifacts
- keep local builds for development fallback only
- publishing and installation details: `docs/PUBLISHING.zh-CN.md`

## Online Image Publishing

The `release.yml` workflow publishes multi-arch images to both Docker Hub and Aliyun Container Registry.

This is the recommended build path because local developer machines may not reliably pull every base image or cross-platform dependency.

Expected GitHub secrets:

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`
- `ALIYUN_USERNAME`
- `ALIYUN_PASSWORD`

Expected GitHub variables:

- `PUBLISH_DOCKERHUB` (`true` or `false`, default `true`)
- `DOCKERHUB_NAMESPACE` (optional, defaults to repository owner)
- `PUBLISH_ALIYUN` (`true` or `false`, default `true`)
- `ALIYUN_REGISTRY`
- `ALIYUN_NAMESPACE` (optional, defaults to repository owner)

Published images:

- `dataprotection-operator`
- `dataprotection-mysql`
- `dataprotection-minio-mc`
- `dataprotection-busybox`

## Offline Installation

The generated installer packages:

- CRDs
- RBAC
- controller Deployment template
- operator image
- MySQL runtime image
- MinIO client helper image
- placeholder busybox image

Example:

```bash
./data-protection-operator-amd64.run install \
  -y
```

By default the offline installer targets `sealos.hub:5000/kube4`. You can still override it with `--registry <repo-prefix>` when another local registry is required.

## Current Boundaries

Not finished yet:

- scheduled backups still reconcile directly to `CronJob`, not `BackupRun` history objects
- Redis / MongoDB / MinIO / RabbitMQ / Milvus built-in runtimes
- admission webhooks
- richer metrics and events
- backup verification runners beyond MySQL

## Storage Model

The repository now follows a clearer two-layer storage model inspired by Stash:

- `BackupStorage` describes the reusable backend connection and credentials
- `BackupRepository` describes the logical repository path used by an app or environment

Recommended pattern:

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: minio-primary
  namespace: backup-system
spec:
  default: true
  type: s3
  s3:
    endpoint: https://minio.example.com
    bucket: data-protection
    prefix: platform
---
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRepository
metadata:
  name: mysql-prod
  namespace: backup-system
spec:
  storageRef:
    name: minio-primary
  path: mysql/prod
```

Compatibility notes:

- old inline `BackupRepository.spec.type/nfs/s3` remains supported
- if `storageRef` is set, inline backend fields must be omitted
- if `storageRef` is omitted and no inline backend is set, the operator will try to use the namespace default `BackupStorage`
