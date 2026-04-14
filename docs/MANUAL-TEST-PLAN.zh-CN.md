# Data Protection Operator 手工测试方案

这份文档面向当前真实架构：

- 不再使用 `BackupRepository`
- 调度模型为 `BackupPolicy -> CronJob -> BackupRun -> Job`
- 恢复使用 `RestoreJob`，可按 `snapshotRef` 或 `importSource`
- `BackupPolicy` 调度身份由控制器自动在业务 namespace 下创建

## 测试目标

1. 安装器能把 operator 正常装起来
2. `BackupSource`、`BackupStorage`、`RetentionPolicy`、`BackupPolicy` 能进入可用状态
3. `BackupPolicy` 能为每个 storage 创建一个 `CronJob`
4. `CronJob` 能自动创建 `BackupRun`
5. `BackupRun` 能创建数据 `Job`
6. `BackupRun` 成功后能沉淀 `Snapshot`
7. `RestoreJob` 能按 `snapshotRef` 或 `importSource` 恢复
8. S3/MinIO 缺 bucket 时能自动创建
9. NFS 场景能正常写入和恢复
10. `Forbid / Replace` 并发策略真正作用到 `BackupRun`
11. 完成后的历史 `Job` 能按 TTL 自动收敛

## 推荐测试命名

- namespace: `backup-system`
- source: `mysql-smoke`
- s3 storage: `minio-primary`
- nfs storage: `nfs-primary`
- policy: `mysql-smoke-daily`
- run: `mysql-smoke-manual`
- restore: `mysql-smoke-restore`

## 推荐资源清单

### 1. Namespace 与 Secret

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

### 2. BackupSource

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupSource
metadata:
  name: mysql-smoke
  namespace: backup-system
spec:
  driver: mysql
  endpoint:
    host: <MYSQL_HOST>
    port: 3306
    username: root
    passwordFrom:
      name: mysql-runtime-auth
      key: password
```

### 3. S3 / MinIO BackupStorage

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: minio-primary
  namespace: backup-system
spec:
  type: s3
  s3:
    endpoint: <S3_ENDPOINT>
    bucket: <S3_BUCKET>
    prefix: smoke
    accessKeyFrom:
      name: minio-credentials
      key: access-key
    secretKeyFrom:
      name: minio-credentials
      key: secret-key
```

### 4. NFS BackupStorage

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: nfs-primary
  namespace: backup-system
spec:
  type: nfs
  nfs:
    server: <NFS_SERVER>
    path: <NFS_EXPORT_PATH>
```

要求：

- `<NFS_EXPORT_PATH>` 需要是实际存在且可写的导出目录
- 运行时会在这个目录下面再写控制器生成的 `storagePath`

### 5. RetentionPolicy

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

### 6. BackupPolicy

联调建议先用每 3 分钟一次：

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

### 7. Manual BackupRun

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

### 8. RestoreJob

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: RestoreJob
metadata:
  name: mysql-smoke-restore
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-smoke
  snapshotRef:
    name: mysql-smoke-manual-minio-primary-snapshot
  # importSource:
  #   storageRef:
  #     name: nfs-primary
  #   path: imports/cluster-a/mysql-smoke.tgz
  #   format: auto
  #   snapshot: mysql-smoke-cluster-a-export
  #   series: import/cluster-a/mysql-smoke
```

## 建议测试顺序

1. 安装 operator，确认 `Deployment` 正常
2. apply `BackupSource`、`BackupStorage`、`RetentionPolicy`
3. apply `BackupPolicy`，确认自动生成 `CronJob`
4. 确认 `BackupPolicy` 所在 namespace 自动生成调度 `ServiceAccount`、`Role`、`RoleBinding`
5. 手工创建一个 `BackupRun`，确认生成备份 `Job`
6. 观察备份成功后生成 `Snapshot`
7. 等待一次定时调度，确认 `CronJob` 能自动创建 `BackupRun`
8. 用 `snapshotRef` 创建 `RestoreJob`
9. 再用导入包补测一次 `importSource` 恢复
10. 再补测 NFS 场景
11. 再补测 MinIO bucket 不存在时的自动创建

## 关键观察命令

### 安装与控制器

```bash
kubectl get pods -n data-protection-system
kubectl logs -n data-protection-system deploy/data-protection-operator-controller-manager
```

### 核心 CRD 状态

```bash
kubectl get backupsource -n backup-system
kubectl get backupstorage -n backup-system
kubectl get retentionpolicy -n backup-system
kubectl get backuppolicy -n backup-system
kubectl get backuprun -n backup-system
kubectl get snapshot -n backup-system
kubectl get restorejob -n backup-system
```

### 调度身份与定时资源

```bash
kubectl get sa -n backup-system
kubectl get role -n backup-system
kubectl get rolebinding -n backup-system
kubectl get cronjob -n backup-system
kubectl get job -n backup-system
kubectl get pod -n backup-system
```

### 详情与日志

```bash
kubectl get backuppolicy mysql-smoke-daily -n backup-system -o yaml
kubectl get backuprun mysql-smoke-manual -n backup-system -o yaml
kubectl describe job -n backup-system <job-name>
kubectl describe pod -n backup-system <pod-name>
kubectl logs -n backup-system <pod-name> -c mysql-backup
kubectl logs -n backup-system <pod-name> -c s3-upload
kubectl logs -n backup-system <pod-name> -c s3-prefetch
kubectl logs -n backup-system <pod-name> -c mysql-restore
kubectl logs -n backup-system <pod-name> -c s3-download
```

## 重点验证点

### A. `BackupPolicy` 调度身份自动创建

预期：

- `backup-system` 中自动出现一个触发 `ServiceAccount`
- 同 namespace 自动出现对应 `Role`
- 同 namespace 自动出现对应 `RoleBinding`
- `CronJob.spec.jobTemplate.spec.template.spec.serviceAccountName` 指向这个 SA

这一步通过后，说明已经不需要再手工补调度 SA。

### B. MinIO / S3 自动创建 bucket

做法：

- 创建一个不存在的 bucket 名称
- 触发备份

预期：

- `mysql-backup` 正常完成
- `s3-upload` 日志里能看到创建 bucket 的信息
- 远端 bucket 被自动创建
- `BackupRun` 最终成功

### C. NFS 备份

做法：

- 使用 `nfs-primary`
- 触发 `BackupRun`

预期：

- Pod 能正常挂载 NFS
- 备份目录能写入 `.sql.gz`、`.sha256`、`.meta`、`latest.txt`
- `BackupRun` 和 `Snapshot` 最终成功

### D. 定时调度

做法：

- 等待 cron 到点，或直接观察下一次 3 分钟触发

预期：

- 先出现一个短命 trigger Pod
- trigger Pod 完成后生成一个新的 `BackupRun`
- `BackupRun` 再生成真正的数据 `Job`

补充检查：

- 如果 policy 是 `Forbid`，当上一次 `BackupRun` 仍处于 `Pending/Running` 时，新一轮调度不应再创建新的 `BackupRun`
- 如果 policy 是 `Replace`，新一轮调度前会先删除旧的活跃 `BackupRun`

### E. 为什么会出现多个 Pod / 容器

如果是“定时 + S3 / MinIO”，常见现象是：

- 一个短命 trigger Pod，只有 `backup-trigger`
- 一个真正的备份 Pod

备份 Pod 内部通常包含：

- `s3-prefetch` init container
- `mysql-backup` 主容器
- `s3-upload` 辅助容器

所以你可能会同时看到：

- `Completed` 的 trigger Pod
- `Running` 或 `Completed` 的备份 Pod

这不是重复执行，而是调度层和数据层分离后的正常结果。

### F. Job 历史收敛

做法：

- 跑完一次手工或定时备份
- 观察 `Job` 上是否带 `ttlSecondsAfterFinished`

预期：

- trigger `CronJob` 历史条数较少
- 真正的数据 `Job` 默认带 `ttlSecondsAfterFinished=86400`
- 超过 TTL 后，`Job/Pod` 会被 Kubernetes 自动清理

### G. 恢复

做法：

- 先等待 `Snapshot` 生成
- 使用 `snapshotRef` 创建一个 `RestoreJob`
- 再把离线导出包放到 `BackupStorage` 对应路径，用 `importSource` 创建第二个 `RestoreJob`

预期：

- 两种恢复模式至少各跑通一次
- 生成 restore Job
- restore Job 成功
- 目标 MySQL 数据恢复

## 结果判断

如果以上链路都通过，可以判断当前仓库已经具备：

- 可维护的调度模型
- 可审计的执行历史
- 可恢复的快照与导入包模型
- MySQL alpha 级真实数据面
