# Data Protection Operator 当前状态

## 一句话判断

当前仓库是一套可运行的 Kubernetes 中间件逻辑备份与恢复控制面，核心领域模型正在收敛到统一的 `BackupExecution` 执行对象。

它不是“全 Kubernetes / 全存储形态”的通用灾备平台；当前重点仍是中间件逻辑备份、统一存储、快照登记与恢复编排。

## 当前核心 API

| API | 当前定位 |
| --- | --- |
| `BackupAddon` | 中间件导出 / 导入扩展定义 |
| `BackupSource` | 被保护的数据源实例 |
| `BackupStorage` | NFS / MinIO 备份后端 |
| `BackupPolicy` | 定时备份策略 |
| `BackupExecution` | **一次具体备份执行的唯一事实源** |
| `Snapshot` | 成功执行产生的可恢复资产 |
| `RestoreJob` | 恢复执行入口 |
| `RetentionPolicy` | 保留策略 |
| `NotificationEndpoint` | 通知目标 |
| `BackupJob` | 旧版手工备份兼容入口，不再直接执行 native Job |

`BackupRun` 不属于当前 API，不应再出现在新的架构说明里。

## 当前执行模型

### 手工执行

```text
BackupExecution
    -> Kubernetes Job
    -> Snapshot
```

### 旧 BackupJob 兼容

```text
BackupJob
    -> BackupExecution
    -> Kubernetes Job
    -> Snapshot
```

`BackupJob` controller 只做请求映射和状态镜像，不再维护第二套执行状态机。

### 定时执行：第一阶段兼容桥

当前仍使用 Kubernetes `CronJob` 作为调度载体：

```text
BackupPolicy
    -> CronJob
    -> Kubernetes Job
    -> BackupExecution(adopt)
    -> Snapshot
```

`JobObserver` 现在只负责把策略产生的 native Job 物化为 `BackupExecution`。

它不再负责：

- 终态判断
- Snapshot 创建
- 成功 / 失败通知
- 独立执行历史

这些职责统一由 `BackupExecution` controller 收敛。

后续可以继续把定时调度演进成真正的：

```text
BackupPolicy -> BackupExecution -> Kubernetes Job
```

但本阶段先确保“执行事实源只有一个”。

## 当前已经具备的能力

- `BackupExecution` 手工备份
- `BackupPolicy` 定时备份
- NFS / MinIO 存储后端
- `Snapshot` 登记
- Snapshot 恢复
- `RestoreJob.spec.importSource` 离线导入恢复
- retention
- notification
- addon 扩展机制
- MySQL / Redis / MinIO / Milvus 官方 addon
- 离线 `.run` 安装器与 GitHub Actions 构建链路

## 当前仍存在的架构问题

### 1. multi-storage 还不是真正的 fan-out

`BackupPolicy.spec.storageRefs` 当前会为不同 storage 创建独立执行链，因此会重复执行中间件 export。

它更准确的语义是“一个策略展开成多个独立备份执行”，而不是“一次导出，多目标分发”。

后续应单独收敛这一模型。

### 2. retention 目前仍有多处生命周期逻辑

上传 helper 与 Snapshot/controller 都参与历史清理，后续需要收敛为单一生命周期权威。

### 3. Snapshot 删除语义仍需要显式 deletion policy

当前 Snapshot CR 与后端 artifact 清理耦合较强，后续应增加明确的 Retain / Delete 语义，避免误删备份资产。

### 4. runtime helper 仍然过重

controller 里仍有较多 storage / artifact / shell orchestration 逻辑，后续应拆成明确的 execution、storage、artifact、runner 层。

### 5. 可观测性仍不完整

还需要逐步补齐：

- stage-level status
- Kubernetes Events
- Prometheus metrics
- verification
- retry / attempt
- alerting
- audit

## 当前范围边界

当前项目主要解决：

- Kubernetes 中间件逻辑备份
- 手工 / 定时执行
- NFS / MinIO 存储
- Snapshot catalog
- retention
- restore
- offline import
- notification

当前不应宣称已经提供：

- Kubernetes 全集群灾备
- etcd 备份
- CSI VolumeSnapshot
- 通用文件系统备份
- CDP
- PITR
- block-level incremental backup
- immutable / WORM backup
- cross-region replication

除非对应能力后续真正实现并验证。

## 下一阶段建议顺序

1. 完成并验证 `BackupExecution` 执行模型统一。
2. 收敛 `BackupPolicy` 与 multi-storage 语义。
3. 收敛 retention 与 Snapshot deletion policy。
4. 拆分 runtime helper / storage driver / runner。
5. 增加 stage status、Events、metrics。
6. 再做 verification、retry、告警和更高级的数据保护能力。
