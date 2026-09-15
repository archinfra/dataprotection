# Backup Execution Flow

## 目的

这份文档说明 `dataprotection v2` 中备份执行的职责边界，以及 `BackupExecution`、Kubernetes `Job`、`Snapshot` 三者的关系。

## 核心结论

`BackupExecution` 表示**一次具体的备份执行**，并且是这次执行状态的唯一事实源。

```text
BackupExecution
    │
    │ reconcile
    ▼
Kubernetes Job
    │
    │ successful artifact
    ▼
Snapshot
```

三者的语义严格区分：

- `BackupExecution`：领域执行对象，回答“这一次备份发生了什么”。
- Kubernetes `Job`：底层运行载体，负责真正运行备份 Pod。
- `Snapshot`：成功执行后登记的可恢复资产。

因此业务上不再把 Kubernetes `Job` 当作备份执行记录。

## BackupExecution 从哪里来

### 手工执行

新的手工执行直接创建 `BackupExecution`：

```text
User / API
    -> BackupExecution(trigger=Manual)
    -> Kubernetes Job
    -> Snapshot
```

示例：

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupExecution
metadata:
  name: mysql-prod-manual-nfs
  namespace: backup-system
spec:
  trigger: Manual
  sourceRef:
    name: mysql-prod
  storageRef:
    name: nfs-primary
```

查看：

```bash
kubectl get backupexec -n backup-system
```

### BackupJob 兼容入口

`BackupJob` 暂时保留用于兼容旧配置，但已经不再直接创建 Kubernetes `Job`。

```text
BackupJob (legacy)
    -> BackupExecution(trigger=Manual)
    -> Kubernetes Job
    -> Snapshot
```

`BackupJob` controller 只负责：

1. 创建同名 `BackupExecution`。
2. 把旧 `BackupJob.spec` 映射到 `BackupExecution.spec`。
3. 把 `BackupExecution.status` 镜像回旧 `BackupJob.status`。

因此旧接口不会形成第二套执行状态机。

### 定时执行

当前阶段继续使用 Kubernetes `CronJob` 作为已有调度载体：

```text
BackupPolicy
    -> CronJob
    -> Kubernetes Job
    -> BackupExecution(trigger=Scheduled, adopt Job)
    -> Snapshot
```

这是第一阶段的兼容桥接。

`JobObserver` 的职责已经缩小为：

> 发现 `BackupPolicy` 产生的原生 Job，并为它创建对应的 `BackupExecution`。

`JobObserver` 不再：

- 判断最终备份结果
- 创建 Snapshot
- 发送备份成功/失败通知
- 维护独立的执行状态

这些职责统一由 `BackupExecution` controller 完成。

后续可以进一步把调度链路演进为：

```text
BackupPolicy
    -> scheduler
    -> BackupExecution
    -> Kubernetes Job
```

但这不是本阶段必须完成的改动。

## BackupExecution 记录什么

一次执行至少包含：

```text
spec
├── policyRef            可选，来源策略
├── sourceRef            备份谁
├── storageRef           放哪里
├── retentionRef         保留策略
├── notificationRefs     通知目标
├── jobRuntime           运行参数
├── snapshotName         可选指定快照名
├── reason               触发原因
└── trigger              Manual / Scheduled

status
├── phase                Pending / Running / Succeeded / Failed / Paused
├── startedAt
├── completedAt
├── nativeJobName
├── series
├── snapshotRef
├── storageProbeResult
├── storageProbeMessage
├── notification
└── conditions
```

这样查看 `BackupExecution` 就能知道一次备份的完整结果，而不需要再同时理解 `BackupPolicy`、CronJob、JobObserver 和原生 Job 的多套状态。

## Pod 内的数据流

备份/恢复实际执行仍发生在 Kubernetes Job Pod 中。

工作目录：

- `/workspace/output`：addon 输出备份数据
- `/workspace/input`：addon 读取恢复数据
- `/workspace/status`：core helper 之间交换执行状态和 artifact 信息

这些目录来自 Pod 临时工作空间，不是长期备份存储。

## addon 与 core 的边界

### BackupAddon

addon 只负责中间件本身的数据导出 / 导入：

```text
源系统 <-> Job Pod workspace
```

例如：

- MySQL：`mysqldump`
- Redis：`redis-cli --rdb`
- MinIO：源 bucket/prefix mirror
- Milvus：`milvus-backup`

addon 不负责 Snapshot CR、远端存储生命周期和通知。

### core/operator

core 负责：

```text
Job Pod workspace <-> BackupStorage
```

以及：

- 执行状态
- 存储探测
- artifact 打包
- 上传 / 下载
- Snapshot 登记
- retention
- notification

## 备份 Pod 阶段

当前备份 Job 内主要包含：

```text
storage-preflight
      ↓
addon-backup
      ↓
artifact-package
      ↓
artifact-upload
```

含义：

1. `storage-preflight`：检查 NFS / MinIO 是否可用。
2. `addon-backup`：把业务数据写入 `/workspace/output`。
3. `artifact-package`：生成统一备份归档、checksum 和 metadata。
4. `artifact-upload`：把 artifact 写入目标 `BackupStorage`。

Job 成功后，`BackupExecution` controller 读取 artifact summary，并创建 / 更新 `Snapshot`。

## Snapshot 的语义

`Snapshot` 只代表：

> 已成功产生、并被控制面登记的可恢复资产。

它不是一次执行本身。

因此：

```text
BackupExecution = execution history
Snapshot        = recovery asset
```

失败的 `BackupExecution` 可以存在，但不会产生一个成功可恢复的 `Snapshot`。

## 恢复链路

恢复目前仍使用 `RestoreJob`：

```text
RestoreJob
    -> Kubernetes Job
        -> storage-preflight
        -> artifact-download
        -> addon-restore
```

恢复 Job 中：

- core 把远端 artifact 下载 / 解包到 `/workspace/input`
- addon 从 `/workspace/input` 恢复业务数据

`RestoreExecution` 的统一命名可以作为后续 API 收敛项，本阶段不与 BackupExecution 改动混在一起。

## 当前阶段的架构边界

第一阶段只解决一个核心问题：**备份执行只有一个权威对象。**

完成后：

```text
Manual --------------------┐
                           │
Legacy BackupJob ----------+--> BackupExecution --> Job --> Snapshot
                           │
BackupPolicy/CronJob ------┘
```

这为后续继续做以下能力提供统一基础：

- stage-level status
- retry / attempt
- metrics
- audit
- failure retention
- verification
- operations agent diagnosis

这些后续能力都应该围绕 `BackupExecution` 扩展，而不是再给 CronJob、JobObserver 或 BackupJob 建新的执行状态模型。
