# Data Protection Operator 手工验证方案

这份文档只关注当前真实架构：

- 不再使用 `BackupRepository`
- 定时模型是 `BackupPolicy -> CronJob -> BackupRun -> Job`
- 恢复优先使用 `Snapshot`

## 验证目标

1. 安装器能把 operator 正常装起来
2. `BackupSource`、`BackupStorage`、`BackupPolicy` 能正常进入可用状态
3. `BackupPolicy` 能生成每个 storage 一个 `CronJob`
4. `CronJob` 触发后能创建 `BackupRun`
5. `BackupRun` 能生成 `Job` 和 `Snapshot`
6. `RestoreRequest` 能按 `snapshotRef` 恢复

## 推荐测试命名

- namespace: `backup-system`
- source: `mysql-smoke`
- storage: `minio-primary`
- policy: `mysql-smoke-daily`
- run: `mysql-smoke-manual`
- restore: `mysql-smoke-restore`

## 推荐样例

### BackupSource

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

### BackupStorage

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

### BackupPolicy

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
    cron: "0 2 * * *"
    concurrencyPolicy: Forbid
  retention:
    keepLast: 3
```

### Manual BackupRun

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

### RestoreRequest

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

## 建议验证顺序

1. 安装 operator，确认 Deployment 正常
2. apply `BackupSource` 和 `BackupStorage`
3. apply `BackupPolicy`，确认生成 `CronJob`
4. 手工创建一个 `BackupRun`，确认生成 `Job` 和 `Snapshot`
5. 等一次定时调度，确认 `CronJob` 能自动创建 `BackupRun`
6. 用 `snapshotRef` 创建 `RestoreRequest`

## 核心观察点

- `kubectl get backuppolicy -o yaml`
  关注 `status.cronJobNames`
- `kubectl get backuprun -o yaml`
  关注 `status.jobNames` 和 `status.storages`
- `kubectl get snapshot -o yaml`
  关注 `spec.storageRef`、`spec.storagePath`、`spec.snapshot`
- `kubectl get restorerequest -o yaml`
  关注 `status.jobName`

## 结果判断

如果上面这条链路都通过，可以判断当前仓库已经具备：

- 可维护的调度模型
- 可审计的执行历史
- 可恢复的快照模型
- MySQL alpha 级真实数据面
