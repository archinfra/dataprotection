# 参考 KubeStash 的设计借鉴

本文只参考 KubeStash 在官网和公开文档中能观察到的产品抽象与控制面设计，不复制其闭源实现。

## 当前判断

你的 `dataprotection` 与 KubeStash 最相似的地方，不是某一个 MySQL 备份脚本，而是这条控制面主链：

- `BackupSource`: 描述被保护对象
- `BackupRepository`: 描述备份落点
- `BackupPolicy`: 描述长期调度策略
- `BackupRun`: 描述一次备份执行
- `RestoreRequest`: 描述一次恢复执行

这条链路已经说明项目方向是对的。真正需要继续补强的，是把“执行历史、快照产物、可复用策略模板”从实现细节提升为一等资源。

## KubeStash 值得借鉴的点

### 1. 配置对象和执行对象分离

KubeStash 的公开概念里，长期配置和一次执行是分开的：

- 长期配置负责“应该怎么备份”
- 执行对象负责“某次备份实际发生了什么”

这点你已经基本具备：

- `BackupPolicy` 对应长期配置
- `BackupRun` / `RestoreRequest` 对应执行对象

建议继续坚持这个方向，不要回退到“一个策略对象既表达计划又承载全部历史”的模型。

### 2. Snapshot 是一等资源

KubeStash 的设计里，快照不是一个只存在于日志或对象存储目录里的字符串，而是一个可以被引用、追踪、审计的业务对象。

这次已经在仓库中先落了一版：

- 新增 `Snapshot` CRD
- `BackupRun` 会按 repository 自动沉淀 `Snapshot`
- `RestoreRequest` 新增 `spec.snapshotRef`

这样后续的恢复、验证、审计、保留策略都可以围绕 `Snapshot` 做，而不是每次都去猜 `latest.txt` 或拼文件名。

### 3. 调度历史应该沉淀到 CRD，而不只是 CronJob/Job

KubeStash 的公开产品体验很强调“平台视角的历史对象”，而不仅仅是底层 Kubernetes Job 历史。

你当前还有一个很值得继续演进的点：

- 现在 `BackupPolicy -> CronJob`
- 但定时触发还没有自动沉淀成 `BackupRun` 历史

建议后续演进为：

- `BackupPolicy` 只负责调度
- 每次调度落地成一个 `BackupRun`
- `BackupRun` 再驱动具体 `Job`

这样会比“CronJob 直接跑业务 Job”更接近平台化产品。

### 4. 恢复入口应优先面向快照对象

KubeStash 风格的产品更偏向：

- 恢复一个明确快照
- 而不是恢复一个模糊路径或目录中的“最新文件”

你现在已经有：

- `snapshotRef`
- `backupRunRef`
- `snapshot`

建议后续对外主推优先级改成：

1. `snapshotRef`
2. `backupRunRef`
3. 原始 `snapshot` 字符串

这样 API 会更稳定，也更便于审计。

### 5. 策略模板和执行模板应继续抽象

KubeStash 公开设计里有比较强的“模板化”和“函数化”味道，比如可复用的运行时定义、策略组合、hook 能力。

你现在已经有一个不错的起点：

- `ExecutionTemplateSpec`
- `DriverConfig`

建议后续继续补两个方向：

- `HookTemplate` 或 Pre/Post hooks
- `RetentionPolicy` 独立 CRD，而不是只内嵌在 `BackupPolicy.spec.retention`

## 本次已经落地到代码的借鉴

本次改动没有照搬 KubeStash，而是按你当前仓库的复杂度做了最小可落地实现：

- 新增 `Snapshot` CRD
- `BackupRun` 自动创建和更新 `Snapshot`
- `RepositoryRunStatus` 新增 `snapshotRef`
- `RestoreRequest` 支持 `snapshotRef`
- 生成新的 Snapshot CRD manifest

这一步的价值是：

- 让快照成为平台对象，而不是字符串
- 为后续校验、审计、保留策略、UI 展示打基础
- 保持当前 `BackupRun` / `RestoreRequest` 兼容，不推翻现有实现

## 推荐的下一步路线

### P0

- 把 `BackupPolicy` 的定时执行改成“创建 `BackupRun`”而不是“直接业务 `CronJob`”
- 完整回填 `BackupPolicy.status.nextScheduleTime`
- 在 `RestoreRequest.status` 中记录解析到的 `snapshotRef`

### P1

- 独立 `RetentionPolicy` CRD
- Snapshot 级别的保留与清理控制器
- Snapshot 校验状态和校验时间
- Admission webhook，提前拒绝明显错误的引用关系

### P2

- `HookTemplate` / preBackup / postBackup / preRestore / postRestore
- 运行时驱动注册机制，而不是继续在 controller 中堆 if/else
- Redis / MongoDB / MinIO 等 driver 的统一 runtime 接口

## 原则

参考 KubeStash 时，建议坚持这三个原则：

- 学抽象，不学表象
- 学对象边界，不学命名照搬
- 学控制面分层，不学闭源实现细节

对你这个项目来说，最值得学的不是“它某个脚本怎么写”，而是：

- 什么对象是长期配置
- 什么对象是一次执行
- 什么对象是快照产物
- 什么对象负责策略、校验和保留
