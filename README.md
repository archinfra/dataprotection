# Data Protection Operator

`dataprotection` is a standalone Kubernetes operator repository for backup and restore control planes.

Current scope:

- a generic CRD model for data protection
- idempotent reconciliation for backup schedules and one-shot runs
- a built-in MySQL runtime as the first real data driver
- online image publishing and offline `.run` installation

## CRDs

- `BackupSource`
- `BackupRepository`
- `BackupPolicy`
- `BackupRun`
- `RestoreRequest`

## What Works Today

- `BackupSource` / `BackupRepository` reconcile basic readiness status
- `BackupPolicy` renders one `CronJob` per repository and cleans up stale children
- `BackupRun` renders one `Job` per repository
- `RestoreRequest` renders one restore `Job`
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
  --registry registry.example.com/archinfra \
  --registry-user admin \
  --registry-password '<password>' \
  -y
```

The installer pushes the packaged images to the target registry and then deploys the controller with those registry references as runtime defaults.

## Current Boundaries

Not finished yet:

- Redis / MongoDB / MinIO / RabbitMQ / Milvus built-in runtimes
- admission webhooks
- richer metrics and events
- backup verification runners beyond MySQL
