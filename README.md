# Data Protection Operator

`dataprotection` 是一个独立的数据保护 Operator 仓库，目标是把“调度、执行、快照、恢复”做成可维护的 Kubernetes 控制面，而不是继续堆脚本。

当前已经落地的第一条真实数据链路是 MySQL，后续会在同一套控制面模型上继续扩展 Redis、MongoDB、MinIO、RabbitMQ、Milvus 等 driver。

## 当前设计结论

这次架构决策已经明确：

- 不再保留 `BackupRepository`
- 调度模型采用 `BackupPolicy -> CronJob -> BackupRun -> Job`
- 恢复优先采用 `Snapshot`，而不是手工拼接路径
- 存储后端统一由 `BackupStorage` 描述

这套模型主要借鉴了开源 Stash / KubeStash 的成熟思路：

- 原生调度交给 Kubernetes `CronJob`
- 执行历史交给我们自己的 CRD
- 快照是独立资源，不只是一个字符串
- 控制面对象和数据执行对象分层

## 为什么不用 “controller 直接定时触发 Job”

`BackupPolicy` 直接由 controller 算时间、自己创建 `Job`，短期能跑，但长期不是更优设计。

更推荐现在这套 `BackupPolicy -> CronJob -> BackupRun -> Job`，原因很明确：

- `CronJob` 是 Kubernetes 原生调度能力，错过调度、并发策略、历史保留这些语义都已经成熟
- `BackupRun` 让每一次定时执行都有自己的 CRD 历史，后续审计、告警、统计、重试都更容易扩展
- `Job` 仍然只负责真正的数据动作，控制器不需要把备份逻辑塞进进程内存里
- 调度层和执行层解耦后，未来想加 webhook、暂停、补跑、重试策略也更自然

如果直接让 controller 自己定时触发：

- 需要自己维护调度时钟和补偿逻辑
- leader 切换和漏触发更难处理
- 执行历史容易和控制器内部状态耦合
- 后面要补“每次定时执行的资源对象”时，还得再重构一遍

所以当前仓库已经把这个决策直接落成代码，而不只是停留在讨论阶段。

## 当前资源模型

### `BackupSource`

描述“谁需要被保护”。

典型例子：

- 一个 MySQL 实例
- 一个 Redis 服务
- 一个 MinIO 集群

### `BackupStorage`

描述“数据最终写到哪”。

当前支持：

- `type: nfs`
- `type: s3`

它只负责后端连接和凭据，不再承担“业务路径编排”的职责。

### `BackupPolicy`

描述长期策略：

- 备份哪个 `BackupSource`
- 写到哪些 `BackupStorage`
- 什么时候调度
- 保留多少历史

controller 会为每个 storage 渲染一个独立的 `CronJob`。

### `BackupRun`

描述一次具体备份执行。

来源可以是：

- 人工创建
- `CronJob` 自动触发创建

controller 会为每个 storage 渲染一个独立的 `Job`，并把结果按 storage 维度写回 `status.storages[]`。

### `Snapshot`

描述一个可恢复的备份资产。

它会记录：

- 来自哪个 `BackupRun`
- 属于哪个 `BackupStorage`
- 实际 storage path 是什么
- snapshot 文件名是什么

这让恢复不再依赖“自己猜 latest.txt 或者自己拼目录”。

### `RestoreRequest`

描述一次恢复请求。

推荐优先用：

- `spec.snapshotRef`

这样恢复时可以直接继承：

- storage identity
- storage path
- snapshot name

### `RetentionPolicy`

描述可复用的保留策略。`BackupPolicy` 可以通过 `retentionPolicyRef` 引用它。

## 当前执行链路

### 定时备份

```text
BackupPolicy
  -> CronJob (每个 storage 一个)
    -> trigger-backup-run
      -> BackupRun
        -> Job (每个 storage 一个)
          -> Snapshot
```

### 手工备份

```text
BackupRun
  -> Job (每个 storage 一个)
    -> Snapshot
```

### 恢复

```text
RestoreRequest
  -> resolve Snapshot / BackupRun / StoragePath
    -> Job
```

## 备份落盘路径

当前不再通过 `BackupRepository.spec.path` 管理逻辑仓库，而是统一由控制器生成稳定路径：

- policy 触发的备份：
  `backups/<driver>/<namespace>/<source>/policies/<policy>`
- 手工 BackupRun：
  `backups/<driver>/<namespace>/<source>/runs/<run>`

这样做的好处：

- 路径规则统一
- Snapshot 能直接记录最终落点
- 恢复时不再依赖额外的 repository 对象

## MySQL 当前能力

内建 MySQL runtime 位于 [controllers/mysql_runtime.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/mysql_runtime.go)。

当前已支持：

- `mysqldump` 逻辑备份
- `.sql.gz` 恢复
- NFS 存储
- S3 / MinIO 存储
- 按库备份
- 按表备份
- `merge` 恢复
- `wipe-all-user-databases` 恢复
- `sha256` 校验
- `latest.txt` 回退
- retention 清理

## 关键样例

### 1. BackupSource

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupSource
metadata:
  name: mysql-prod
  namespace: backup-system
spec:
  driver: mysql
  endpoint:
    host: mysql.default.svc.cluster.local
    port: 3306
    username: root
    passwordFrom:
      name: mysql-runtime-auth
      key: password
```

### 2. BackupStorage

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: minio-primary
  namespace: backup-system
spec:
  type: s3
  s3:
    endpoint: https://minio.example.com
    bucket: data-protection
    prefix: platform
    accessKeyFrom:
      name: minio-credentials
      key: access-key
    secretKeyFrom:
      name: minio-credentials
      key: secret-key
```

### 3. BackupPolicy

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupPolicy
metadata:
  name: mysql-prod-daily
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-prod
  storageRefs:
    - name: minio-primary
    - name: nfs-secondary
  schedule:
    cron: "0 2 * * *"
    concurrencyPolicy: Forbid
  retentionPolicyRef:
    name: mysql-daily-retention
  retention:
    keepLast: 7
```

### 4. Manual BackupRun

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRun
metadata:
  name: mysql-prod-manual
  namespace: backup-system
spec:
  policyRef:
    name: mysql-prod-daily
  sourceRef:
    name: mysql-prod
  storageRefs:
    - name: minio-primary
  reason: manual smoke backup
```

### 5. RestoreRequest

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: RestoreRequest
metadata:
  name: mysql-prod-restore
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-prod
  snapshotRef:
    name: mysql-prod-manual-minio-primary-snapshot
  target:
    mode: InPlace
    driverConfig:
      mysql:
        restoreMode: merge
```

## 目录说明

- `api/v1alpha1`
  CRD 类型、校验、命名规则
- `controllers`
  reconciler、内建 runtime、状态回填
- `config/crd/bases`
  生成后的 CRD 清单
- `config/samples`
  最小可运行样例
- `manifests`
  operator 安装模板
- `scripts/install`
  `.run` 离线安装器源码

## 对维护者最重要的代码入口

- [main.go](/C:/Users/admin/Desktop/release/dataprotection/main.go)
  operator 启动入口，以及 `trigger-backup-run` 子命令入口
- [trigger_backuprun.go](/C:/Users/admin/Desktop/release/dataprotection/trigger_backuprun.go)
  `CronJob` 触发 `BackupRun` 的桥接逻辑
- [controllers/backuppolicy_controller.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/backuppolicy_controller.go)
  `BackupPolicy -> CronJob`
- [controllers/backuprun_controller.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/backuprun_controller.go)
  `BackupRun -> Job -> Snapshot`
- [controllers/restorerequest_controller.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/restorerequest_controller.go)
  `RestoreRequest -> Job`
- [controllers/runtime_helpers.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/runtime_helpers.go)
  交叉资源解析、公共渲染、storage path 规则
- [controllers/mysql_runtime.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/mysql_runtime.go)
  MySQL 内建执行面

## 本地开发

```bash
bash hack/bootstrap-dev-env.sh
make fmt
make generate
make manifests
make test
make build
```

## 离线安装

安装器会打包：

- CRD
- RBAC
- operator Deployment 模板
- operator image
- MySQL runner image
- MinIO helper image
- busybox placeholder image

安装示例：

```bash
./data-protection-operator-amd64.run install -y
```

## 当前边界

当前已经是一个可以运行的 alpha 控制面，但仍有明确边界：

- 现在真实打通的数据面主要是 MySQL
- 还没有 admission webhook
- 还没有完整 metrics / events / verification controller
- 其他 driver 仍以模型和扩展位为主

## 相关文档

- 开发说明: [docs/DEVELOPMENT.zh-CN.md](/C:/Users/admin/Desktop/release/dataprotection/docs/DEVELOPMENT.zh-CN.md)
- 当前状态: [docs/PROJECT-STATUS.zh-CN.md](/C:/Users/admin/Desktop/release/dataprotection/docs/PROJECT-STATUS.zh-CN.md)
- 架构决策: [docs/ADR-2026-04-08-scheduling-model.zh-CN.md](/C:/Users/admin/Desktop/release/dataprotection/docs/ADR-2026-04-08-scheduling-model.zh-CN.md)
- Stash 借鉴说明: [docs/STASH-INSIGHTS.zh-CN.md](/C:/Users/admin/Desktop/release/dataprotection/docs/STASH-INSIGHTS.zh-CN.md)
