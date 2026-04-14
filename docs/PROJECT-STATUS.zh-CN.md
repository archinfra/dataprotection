# Data Protection Operator 当前状态

## 一句话判断

当前仓库已经不是概念验证脚本，而是一个可运行的 alpha 版数据保护控制面。

真实打通的数据面目前以 MySQL 为主，控制面对象和调度模型已经具备继续扩展其他 driver 的基础。

## 已经完成的部分

- `BackupSource`、`BackupStorage`、`BackupPolicy`、`BackupRun`、`Snapshot`、`RestoreJob`、`RetentionPolicy` 都有 API、CRD 和状态字段
- `BackupPolicy -> CronJob -> BackupRun -> Job` 调度模型已经落地
- `BackupRun -> Snapshot` 历史沉淀已经落地
- `RestoreJob` 已经支持按 `snapshotRef` 或 `importSource` 恢复
- MySQL 内建 runtime 已支持 NFS、S3/MinIO、按库、按表、恢复、校验和 retention
- 离线 `.run` 安装器和 GitHub Actions 发版链路已经接上

## 当前推荐理解

不要把它理解成“已经完成所有 driver 的通用备份平台”。

更准确的理解是：

- 一个结构清晰的数据保护 Operator 控制面
- 加上一个已经真正跑通的 MySQL 内建执行面

## 当前设计重点

### 1. 不再保留 `BackupRepository`

现在只有 `BackupStorage` 负责存储后端，逻辑落盘路径由 controller 统一计算。

### 2. 定时执行必须留下 CRD 历史

现在的定时链路不是：

```text
BackupPolicy -> Job
```

而是：

```text
BackupPolicy -> CronJob -> BackupRun -> Job
```

这让后续做审计、统计、失败重试、补跑都更自然。

### 3. `Snapshot` 是标准恢复入口，但不是唯一入口

平台内标准恢复仍优先按 `snapshotRef` 做，而不是手工拼 storage path。

同时，为了适配 `A` 集群离线导出、`B` 集群导入恢复，当前也支持通过 `importSource` 直接引用 `BackupStorage` 中的离线包、目录或单文件。

## 当前边界

- 真实可用的数据面仍主要是 MySQL
- 还没有 admission webhook
- 还没有完整 verification controller
- 还没有完整 metrics / events / alerting 体系
- 其他 driver 仍以 schema 和扩展位为主

## 下一阶段最值得做的事

1. 把 MySQL 路径做得更稳定，补更多运行态可观测性
2. 增加更多 driver runtime
3. 补 verification、event、metrics
4. 补 webhook 和更强的 admission 校验
