# Data Protection Operator

`dataprotection` 是一套面向 Kubernetes 的数据保护控制面。当前仓库的重点不是继续堆安装脚本，而是把调度、执行、快照、恢复、保留策略做成一套能维护、能扩展的 Operator。

当前调度模型已经稳定为：

```text
BackupPolicy
  -> CronJob
    -> trigger-backup-run
      -> BackupRun
        -> Job
          -> Snapshot
```

这套模型借鉴了 Stash / KubeStash 的成熟思路，但 CRD、controller、installer 都是我们自己实现和维护。

## 当前能力

| Driver / Addon | 备份 | 恢复 | 说明 |
| --- | --- | --- | --- |
| MySQL addon | 支持 | 支持 | `mysqldump`、NFS/S3、latest 回退、sha256、自动建 bucket |
| Redis addon | 支持 | 暂不内建 | 支持单点和集群，集群会自动发现 master 节点并拉取 RDB |
| MinIO addon | 支持 | 支持 | 把 MinIO 作为被保护源，镜像到 NFS 或 S3 存储 |

说明：

- Redis 当前内建 addon 先把“稳定备份”跑通，恢复先保留给后续版本或自定义 execution。
- MinIO 的 `includeVersions=true` 还没有内建实现，当前会直接拒绝，避免误以为已经备份了版本历史。
- retention 现在会同时治理后端文件和控制面 `Snapshot` 对象，不再只清文件不清 CR。

## 架构原则

### 1. Core 只管控制面

core controller 负责：

- 解析 `BackupSource / BackupStorage / BackupPolicy / BackupRun / Snapshot / RestoreRequest`
- 调度 `CronJob`
- 创建执行 `Job`
- 汇总状态
- 维护快照对象

### 2. 数据面尽量做成静态 addon

当前这版已经把 MySQL、Redis、MinIO 都接到了静态 addon 注册表里。这样后续继续加 MongoDB、RabbitMQ、Milvus 时，不需要再把逻辑直接塞进 reconcile 主流程。

你可以从这些文件开始看：

- `controllers/addon_registry.go`
- `controllers/addon_mysql.go`
- `controllers/addon_redis.go`
- `controllers/addon_minio.go`

### 3. BackupStorage 统一后端

仓库不再保留 `BackupRepository`。后端统一使用：

- `BackupStorage` + `type: nfs`
- `BackupStorage` + `type: s3`

这样 source addon 只需要关心“如何把数据导出到标准目录”，不用再重复发明存储模型。

## CRD 模型

### `BackupSource`

描述“谁需要被保护”。例如：

- 一个 MySQL 实例
- 一个 Redis 单点或 Redis Cluster
- 一个 MinIO 集群

### `BackupStorage`

描述“备份最终落到哪里”。当前支持：

- `nfs`
- `s3`

### `BackupPolicy`

描述长期策略：

- 备份哪个 `BackupSource`
- 写到哪些 `BackupStorage`
- 用什么 cron 调度
- 用什么 retention 规则

### `BackupRun`

描述一次具体执行。它可以由两种方式产生：

- 手工创建
- `BackupPolicy` 调度触发

### `Snapshot`

描述一个可恢复的备份资产。恢复时优先使用 `snapshotRef`，而不是手工拼路径。

### `RestoreRequest`

描述一次恢复请求。建议优先使用：

- `spec.snapshotRef`

### `RetentionPolicy`

描述可复用保留策略，供多个 `BackupPolicy` 共享。

## 安装

### 在线或离线安装

```bash
./data-protection-operator-amd64.run install --image-pull-policy Always -y
```

如果你想覆盖默认镜像，可以使用：

```bash
./data-protection-operator-amd64.run install \
  --operator-image <image> \
  --mysql-runner-image <image> \
  --redis-runner-image <image> \
  --minio-runner-image <image> \
  --s3-helper-image <image> \
  -y
```

### 安装后确认

```bash
kubectl get pods -n data-protection-system
kubectl get deploy -n data-protection-system data-protection-operator-controller-manager
kubectl get crd | grep dataprotection.archinfra.io
```

## 快速开始

完整样例见：

- `docs/QUICKSTART.zh-CN.md`
- `config/samples/quickstart/`

最常用的基础样例：

- `config/samples/quickstart/00-namespace-secrets.yaml`
- `config/samples/quickstart/01-backupsource-mysql.yaml`
- `config/samples/quickstart/02-backupstorage-minio.yaml`
- `config/samples/quickstart/03-backupstorage-nfs.yaml`
- `config/samples/quickstart/04-retentionpolicy.yaml`

新增 addon 样例：

- `config/samples/quickstart/11-backupsource-redis-standalone.yaml`
- `config/samples/quickstart/12-backupsource-redis-cluster.yaml`
- `config/samples/quickstart/13-backupsource-minio.yaml`
- `config/samples/quickstart/14-backuppolicy-redis-standalone-every-5m.yaml`
- `config/samples/quickstart/15-backuppolicy-redis-cluster-every-5m.yaml`
- `config/samples/quickstart/16-backuppolicy-minio-every-10m.yaml`

## kubectl shortName

更新 CRD 后，可以直接这样看：

```bash
kubectl get bsrc -A
kubectl get bst -A
kubectl get bp -A
kubectl get br -A
kubectl get snap -A
kubectl get rr -A
kubectl get rp -A
```

组合查看：

```bash
kubectl get bsrc,bst,bp,br,snap,rr,rp -n backup-system
```

新版 `kubectl get` 里会直接展示更适合运维的列，例如：

- `kubectl get snap -A` 会看到 `Phase / Latest / Ready / Source / Storage`
- `kubectl get br -A` 会看到 `Phase / Policy / Source / Completed`
- `kubectl get bp -A` 会看到 `Phase / Source / Schedule / LastSchedule`

## 手工测试建议

### MySQL

先跑：

1. `BackupSource`
2. `BackupStorage`
3. `RetentionPolicy`
4. `BackupPolicy`
5. `BackupRun`
6. `RestoreRequest`

### Redis 单点

先验证：

1. 手工 `BackupRun`
2. 定时 `BackupPolicy`
3. 切换到 NFS / S3 两种后端

### Redis Cluster

重点验证：

1. 能否从 seed endpoint 自动发现 master 节点
2. 是否每个 master 都生成了 RDB
3. S3/NFS 上快照是否完整

### MinIO

重点验证：

1. 指定 bucket + prefix 是否按预期镜像
2. 留空 bucket 时是否会自动枚举所有 bucket
3. 从 NFS/S3 恢复到源 MinIO 时，缺失 bucket 是否会自动创建

## 已知边界

- Redis 内建 addon 当前只支持备份，不内建 restore。
- MinIO 当前不支持版本化对象历史备份。
- `BackupSource Ready` 目前仍主要是静态配置通过，不等于真实连通性完全探测。

## 开发入口

如果你要继续扩展 addon，建议按这个顺序看代码：

1. `controllers/addon_registry.go`
2. `controllers/runtime_helpers.go`
3. `controllers/addon_mysql.go`
4. `controllers/addon_redis.go`
5. `controllers/addon_minio.go`

这几层已经把“控制面”和“数据面 addon”基本拆开了，后面继续加 driver 时会轻很多。
