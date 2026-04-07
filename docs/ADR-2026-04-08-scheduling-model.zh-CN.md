# ADR: 调度采用 `BackupPolicy -> CronJob -> BackupRun -> Job`

## 状态

已采纳

## 背景

我们需要在两个方案里做选择：

1. `BackupPolicy` 由 controller 自己计算时间并直接创建 `Job`
2. `BackupPolicy` 渲染 `CronJob`，`CronJob` 再创建 `BackupRun`，最后 `BackupRun` controller 创建 `Job`

同时，这一轮重构已经决定移除 `BackupRepository`，不再为兼容旧模型保留额外对象层。

## 决策

采用方案 2：

```text
BackupPolicy -> CronJob -> BackupRun -> Job
```

并配套保留：

- `BackupStorage` 作为可复用存储后端
- `Snapshot` 作为独立备份资产
- `RestoreRequest` 优先按 `snapshotRef` 恢复

## 主要原因

### 1. 调度交给 Kubernetes 原生能力

`CronJob` 已经提供了成熟的：

- 调度语义
- 并发策略
- 漏触发补偿语义
- 原生状态与历史行为

### 2. 执行历史交给我们自己的 API

`BackupRun` 让每一次执行都成为 CRD 对象，后续更容易扩展：

- 审计
- 统计
- 重试
- 失败归因
- 手工补跑

### 3. 控制器职责更清晰

controller 负责：

- 依赖解析
- 资源渲染
- 状态聚合

`Job` 负责：

- 真正的数据备份与恢复

### 4. 更接近 Stash 的成熟思路

Stash 给我们的启发，不是某个脚本，而是：

- 配置对象和执行对象分层
- 原生调度和 operator 历史分层
- 快照作为一等资源

## 放弃的方案

### `BackupPolicy -> Job`

没有采用的原因：

- controller 需要自己维护调度时钟
- leader 切换和错过调度处理更复杂
- 执行历史天然不完整
- 后续还得再补一层执行对象，等于先做一版再推翻一版

## 结果

当前仓库已经按这个决策落地：

- `BackupPolicy` 渲染每个 storage 一个 `CronJob`
- `CronJob` 执行 `trigger-backup-run`
- `trigger-backup-run` 创建 `BackupRun`
- `BackupRun` controller 创建实际 `Job`
- `BackupRun` 同时沉淀 `Snapshot`
