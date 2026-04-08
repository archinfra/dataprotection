# Data Protection Operator

`dataprotection` 是一个独立的数据保护 Operator 仓库。当前目标不是继续堆安装脚本，而是把“调度、执行、快照、恢复、保留策略”做成一套可维护的 Kubernetes 控制面。

当前已经跑通的真实数据面是 MySQL，后续可以沿着同一套模型继续扩展到其他 driver 或 addon。

## 当前架构

当前架构决策已经明确：

- 不再保留 `BackupRepository`
- 存储后端统一使用 `BackupStorage`
- 调度模型采用 `BackupPolicy -> CronJob -> BackupRun -> Job`
- 恢复优先使用 `Snapshot`
- 调度身份按 `BackupPolicy` 所在 namespace 自动下发，不再依赖 operator namespace 的 ServiceAccount

这套模型借鉴了 Stash / KubeStash 的成熟思路，但实现是我们自己的控制器和 CRD。

## 资源模型

### `BackupSource`

描述“谁需要被保护”。

例如：

- 一个 MySQL 实例
- 一个 Redis 服务
- 一个 MinIO 集群

### `BackupStorage`

描述“备份最终落到哪里”。

当前支持：

- `type: s3`
- `type: nfs`

### `BackupPolicy`

描述长期策略：

- 备份哪个 `BackupSource`
- 写到哪些 `BackupStorage`
- 用什么 cron 调度
- 用什么保留策略

控制器会为每个 storage 生成一个独立的 `CronJob`。

### `BackupRun`

描述一次具体备份执行。

来源可以是：

- 手工创建
- `BackupPolicy` 调度触发

控制器会为每个 storage 生成一个独立的 `Job`，并把结果写回 `status.storages[]`。

### `Snapshot`

描述一个可恢复的备份资产。

它会记录：

- 来自哪个 `BackupRun`
- 属于哪个 `BackupStorage`
- 对应的 `storagePath`
- 实际的 snapshot 文件名

### `RestoreRequest`

描述一次恢复请求。

推荐优先使用：

- `spec.snapshotRef`

这样恢复时可以直接拿到 storage identity、storage path 和 snapshot name，不再依赖手工拼路径。

### `RetentionPolicy`

描述可复用的保留策略。`BackupPolicy` 可以通过 `retentionPolicyRef` 引用它。

## 调度模型为什么这样设计

当前使用的是：

```text
BackupPolicy
  -> CronJob
    -> trigger-backup-run
      -> BackupRun
        -> Job
          -> Snapshot
```

而不是“controller 自己算时间然后直接创建 Job”。

这样做的原因：

- `CronJob` 的定时、并发控制、错过窗口补偿是 Kubernetes 原生能力
- `BackupRun` 把每次执行沉淀成 CRD 级历史，便于审计、追踪和告警
- `Job` 只负责真正的数据动作，控制器不需要把执行态塞进内存
- 后续要做暂停、补跑、重试、观测时，这个模型更稳定

## MySQL 当前能力

当前 MySQL 是第一条已经跑通的数据面能力，代码在 [`controllers/mysql_runtime.go`](/C:/Users/admin/Desktop/release/dataprotection/controllers/mysql_runtime.go)。

已经支持：

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
- 缺失 bucket 时自动创建 bucket

## 快速开始

### 1. 安装 operator

```bash
./data-protection-operator-amd64.run install -y
```

### 2. 创建认证 Secret

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: backup-system
---
apiVersion: v1
kind: Secret
metadata:
  name: mysql-runtime-auth
  namespace: backup-system
type: Opaque
stringData:
  password: "<MYSQL_ROOT_PASSWORD>"
---
apiVersion: v1
kind: Secret
metadata:
  name: minio-credentials
  namespace: backup-system
type: Opaque
stringData:
  access-key: "<S3_ACCESS_KEY>"
  secret-key: "<S3_SECRET_KEY>"
```

### 3. 创建 `BackupSource`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupSource
metadata:
  name: mysql-smoke
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

### 4. 创建 S3 / MinIO 存储

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: minio-primary
  namespace: backup-system
spec:
  type: s3
  s3:
    endpoint: http://minio.minio.svc.cluster.local:9000
    bucket: data-protection
    prefix: smoke
    accessKeyFrom:
      name: minio-credentials
      key: access-key
    secretKeyFrom:
      name: minio-credentials
      key: secret-key
```

说明：

- 如果 bucket 不存在，MySQL 内建备份上传会自动创建
- 如果 restore 时 bucket 或 path 不存在，会直接失败，避免误恢复

### 5. 创建 NFS 存储

也可以直接用 NFS：

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: nfs-primary
  namespace: backup-system
spec:
  type: nfs
  nfs:
    server: 10.0.0.20
    path: /exports/data-protection
```

要求：

- `server:path` 必须是可挂载的 NFS 导出目录
- 该路径需要允许 Pod 写入
- MySQL 运行时会在控制器生成的 `storagePath` 下写入快照

### 6. 创建保留策略

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: RetentionPolicy
metadata:
  name: mysql-daily-retention
  namespace: backup-system
spec:
  successfulSnapshots:
    last: 3
  failedSnapshots:
    last: 1
```

### 7. 创建定时策略

下面这个例子是每 3 分钟跑一次，适合联调：

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupPolicy
metadata:
  name: mysql-smoke-daily
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-smoke
  storageRefs:
    - name: minio-primary
  schedule:
    cron: "*/3 * * * *"
    concurrencyPolicy: Forbid
  retentionPolicyRef:
    name: mysql-daily-retention
  retention:
    keepLast: 3
```

### 8. 手工触发一次备份

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRun
metadata:
  name: mysql-smoke-manual
  namespace: backup-system
spec:
  policyRef:
    name: mysql-smoke-daily
  sourceRef:
    name: mysql-smoke
  storageRefs:
    - name: minio-primary
  reason: manual smoke backup
```

### 9. 按快照恢复

备份成功后会自动生成 `Snapshot`，然后可以这样恢复：

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: RestoreRequest
metadata:
  name: mysql-smoke-restore
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-smoke
  snapshotRef:
    name: mysql-smoke-manual-minio-primary-snapshot
  target:
    mode: InPlace
    driverConfig:
      mysql:
        restoreMode: merge
```

## 调度身份和权限

现在 `BackupPolicy` 的 CronJob 不再使用 operator namespace 的 SA。

每个 `BackupPolicy` 会自动在自己的 namespace 下生成：

- 一个 `ServiceAccount`
- 一个最小权限 `Role`
- 一个 `RoleBinding`

这个触发身份只负责两件事：

- `get` 自己对应的 `BackupPolicy`
- `create` `BackupRun`

这意味着：

- `backup-system` 里的策略可以独立调度
- 不需要手工在每个业务 namespace 补 SA
- 权限范围比共享 controller SA 更小

## 为什么会看到多个 Pod / 容器

如果你配置的是“定时 + S3 / MinIO”，一次调度通常会看到两层资源：

### 第一层：触发 Pod

这是 `CronJob` 自己拉起来的短命 Pod。

它只有一个容器：

- `backup-trigger`

作用：

- 调用 `trigger-backup-run`
- 创建一个新的 `BackupRun`

执行完成后它会很快 `Completed`。

### 第二层：真正的数据备份 Pod

这是 `BackupRun` 控制器创建的实际数据面 Pod。

如果后端是 S3 / MinIO，通常会有：

- `s3-prefetch` init container
- `mysql-backup` 主容器
- `s3-upload` 辅助容器

分别负责：

- `s3-prefetch`: 先把远端已有 metadata / snapshot 拉到共享目录
- `mysql-backup`: 执行 `mysqldump`，写本地快照和状态文件
- `s3-upload`: 等待备份完成，必要时自动建 bucket，再把产物同步到对象存储

所以你看到的：

- 两个 `Completed` 的短命 Pod
- 两个 `Running` 的备份 Pod

这是正常现象，说明调度触发和真正数据执行是分层的。

## 常用观察命令

```bash
kubectl get backupsource -n backup-system
kubectl get backupstorage -n backup-system
kubectl get backuppolicy -n backup-system
kubectl get backuprun -n backup-system
kubectl get snapshot -n backup-system
kubectl get restorerequest -n backup-system
kubectl get cronjob -n backup-system
kubectl get job -n backup-system
kubectl get pod -n backup-system
```

看某次备份详情：

```bash
kubectl get backuprun mysql-smoke-manual -n backup-system -o yaml
kubectl describe job -n backup-system <job-name>
kubectl describe pod -n backup-system <pod-name>
```

看容器日志：

```bash
kubectl logs -n backup-system <pod-name> -c mysql-backup
kubectl logs -n backup-system <pod-name> -c s3-upload
kubectl logs -n backup-system <pod-name> -c s3-prefetch
```

恢复场景看：

```bash
kubectl logs -n backup-system <pod-name> -c mysql-restore
kubectl logs -n backup-system <pod-name> -c s3-download
```

## 本地开发

```bash
bash hack/bootstrap-dev-env.sh
make fmt
make generate
make manifests
make test
make build
```

## 当前边界

当前已经是一套可运行的 alpha 控制面，但边界仍然明确：

- 已真实打通的数据面主要是 MySQL
- 还没有 admission webhook
- 还没有完整 metrics / events / verification controller
- 其他 driver 目前仍以模型位和扩展位为主

## 相关文档

- [开发说明](/C:/Users/admin/Desktop/release/dataprotection/docs/DEVELOPMENT.zh-CN.md)
- [项目状态](/C:/Users/admin/Desktop/release/dataprotection/docs/PROJECT-STATUS.zh-CN.md)
- [调度模型 ADR](/C:/Users/admin/Desktop/release/dataprotection/docs/ADR-2026-04-08-scheduling-model.zh-CN.md)
- [Stash 借鉴说明](/C:/Users/admin/Desktop/release/dataprotection/docs/STASH-INSIGHTS.zh-CN.md)
- [手工测试方案](/C:/Users/admin/Desktop/release/dataprotection/docs/MANUAL-TEST-PLAN.zh-CN.md)
