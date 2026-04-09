# dataprotection v2

`dataprotection` v2 是一套面向 Kubernetes 的备份控制面。它把系统拆成四层：

- 存储平面：只管理 `NFS` 和 `MinIO`
- 调度平面：`BackupPolicy -> CronJob -> native Job`
- 插件平面：数据源通过 `BackupAddon` 外置模板接入
- 通知平面：operator 发标准事件到 `notification-gateway`

这版是破坏式重构，不再兼容旧的 `BackupRun / RestoreRequest / 内嵌 driver` 模型。

## 资源模型

- `BackupAddon`
  - cluster-scoped
  - 定义备份/恢复模板镜像、命令、参数约定
- `BackupSource`
  - 指定一个业务数据源，引用 `BackupAddon`
- `BackupStorage`
  - 指定一个后端存储，只支持 `nfs` 和 `minio`
  - controller 只做配置校验
  - 真正连通性在每次执行前做 preflight
- `RetentionPolicy`
  - 成功快照保留数、失败执行保留数
- `BackupPolicy`
  - 定时策略
  - controller 直接为每个 storage 渲染原生 `CronJob`
- `BackupJob`
  - 手工一次性备份
  - 一个 `BackupJob` 只对应一个 `BackupStorage`
- `RestoreJob`
  - 手工一次性恢复
  - 优先引用 `Snapshot`
- `Snapshot`
  - 成功备份后唯一的可恢复资产记录
- `NotificationEndpoint`
  - 描述 webhook 目标

## 执行流

```text
BackupPolicy
  -> CronJob
    -> native backup Job
      -> addon backup container writes /workspace/output
      -> core package/upload container writes snapshot.tgz + latest.json
      -> Snapshot

BackupJob
  -> native backup Job
    -> Snapshot

RestoreJob
  -> native restore Job
    -> core download/extract to /workspace/input
    -> addon restore container restores
```

## 核心约定

### 插件约定

`BackupAddon` 不负责直接上传 NFS / MinIO。

- 备份时：
  - 插件只把数据写到 `/workspace/output`
- 恢复时：
  - 核心先把快照解包到 `/workspace/input`
  - 插件从 `/workspace/input` 读取并恢复

### 存储 preflight

每次 `backup` / `restore` 真正执行前，都会先做统一 preflight：

- `nfs`
  - 检查路径是否可创建、可写
- `minio`
  - 检查 endpoint / credential / bucket
  - 如果 `autoCreateBucket=true`，可自动建 bucket

结果会回写到：

- `BackupStorage.status.lastProbeTime`
- `BackupStorage.status.lastProbeResult`
- `BackupStorage.status.lastProbeMessage`

### Snapshot 与 retention

- `Snapshot` 只记录成功、可恢复的资产
- 失败执行不会生成 `Snapshot`
- `latest` 由核心上传逻辑维护，不再靠插件脚本
- retention 按 series 执行
  - `series = source + policy/manual job + storage`

## 安装

离线安装：

```bash
./data-protection-operator-amd64.run install -y
```

查看状态：

```bash
./data-protection-operator-amd64.run status
```

卸载：

```bash
./data-protection-operator-amd64.run uninstall
```

## 快速上手

直接看这些 sample：

- [00-namespace-secrets.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/00-namespace-secrets.yaml)
- [01-backupaddon-mysql.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/01-backupaddon-mysql.yaml)
- [02-backupsource-mysql.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/02-backupsource-mysql.yaml)
- [03-backupstorage-minio.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/03-backupstorage-minio.yaml)
- [04-backupstorage-nfs.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/04-backupstorage-nfs.yaml)
- [05-retentionpolicy.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/05-retentionpolicy.yaml)
- [06-notificationendpoint.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/06-notificationendpoint.yaml)
- [07-backuppolicy-minio-every-3m.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/07-backuppolicy-minio-every-3m.yaml)
- [08-backupjob-manual-nfs.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/08-backupjob-manual-nfs.yaml)
- [09-restorejob-from-snapshot.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/09-restorejob-from-snapshot.yaml)

建议顺序：

```bash
kubectl apply -f config/samples/quickstart/00-namespace-secrets.yaml
kubectl apply -f config/samples/quickstart/01-backupaddon-mysql.yaml
kubectl apply -f config/samples/quickstart/02-backupsource-mysql.yaml
kubectl apply -f config/samples/quickstart/03-backupstorage-minio.yaml
kubectl apply -f config/samples/quickstart/04-backupstorage-nfs.yaml
kubectl apply -f config/samples/quickstart/05-retentionpolicy.yaml
kubectl apply -f config/samples/quickstart/06-notificationendpoint.yaml
kubectl apply -f config/samples/quickstart/07-backuppolicy-minio-every-3m.yaml
kubectl apply -f config/samples/quickstart/08-backupjob-manual-nfs.yaml
```

## 常用命令

shortName 已经打开：

```bash
kubectl get ba
kubectl get bsrc
kubectl get bst
kubectl get bp
kubectl get bj
kubectl get rj
kubectl get snap
kubectl get rp
kubectl get ne
```

组合观察：

```bash
kubectl get bsrc,bst,bp,bj,rj,snap,rp,ne -n backup-system
kubectl get cronjob,job,pod -n backup-system
```

## 当前边界

- 核心不再内嵌 `mysql / redis / minio / mongodb / rabbitmq` 业务逻辑
- 这些能力都应该以外置 `BackupAddon` 或独立 addon 包提供
- core 安装包只包含：
  - operator
  - `busybox` utility image
  - `minio/mc` helper image

## 开发

```bash
make generate
make manifests
make test
APP_VERSION="$(cat VERSION)" bash scripts/assemble-install.sh install.sh
```

## Addon run packages

The core installer already packages the notification gateway together with the operator.

Addon runner images and `BackupAddon` manifests now live under:

- [addons/mysql](C:/Users/admin/Desktop/release/dataprotection/addons/mysql)
- [addons/redis](C:/Users/admin/Desktop/release/dataprotection/addons/redis)
- [addons/minio](C:/Users/admin/Desktop/release/dataprotection/addons/minio)
- [addons/milvus](C:/Users/admin/Desktop/release/dataprotection/addons/milvus)

Each addon directory can build its own multi-arch `.run` package and export ready-to-apply sample YAMLs.
