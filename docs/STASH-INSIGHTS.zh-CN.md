# 从 Stash 借鉴到的设计原则

这份文档记录的是“我们真正决定借鉴什么”，而不是机械照搬 Stash。

## 借鉴点 1：调度和执行历史分层

Stash 的核心启发不是某个 shell 脚本，而是对象边界。

对我们来说，最重要的落地是：

- `BackupPolicy` 负责长期策略
- `CronJob` 负责原生调度
- `BackupRun` 负责一次执行历史
- `Job` 负责真正数据动作

这比 `BackupPolicy` 直接由 controller 定时触发 `Job` 更工程化。

## 借鉴点 2：Snapshot 是一等资源

备份结果不应该只是一个字符串或者一个 `latest.txt`。

所以现在我们把 `Snapshot` 做成独立 CRD，显式记录：

- source
- backup run
- storage
- storage path
- snapshot 名称

恢复优先基于 `snapshotRef`，这比继续维护一个 `BackupRepository` 对象更清晰。

## 借鉴点 3：存储后端要复用

Stash 的一个成熟点是把“后端连接信息”和“执行对象”拆开。

我们当前的简化落地是：

- 保留 `BackupStorage`
- 去掉 `BackupRepository`
- 由 controller 统一生成路径

这样做比再维护一层 repository path 对象更简单，也更适合当前阶段。

## 借鉴点 4：数据面必须在 Job 里

controller 负责协调，不负责执行实际备份逻辑。

所以：

- MySQL backup/restore 逻辑仍然跑在 `Job` 里
- controller 只做依赖解析、资源渲染、状态聚合

这让未来新增 driver 时不会把 controller 进程越做越重。

## 当前落地后的结论

Stash 最值得借鉴的不是“功能列表”，而是“对象怎么分层”。

所以我们现在的路线是：

```text
BackupSource + BackupStorage
  -> BackupPolicy
  -> CronJob
  -> BackupRun
  -> Job
  -> Snapshot
  -> RestoreRequest
```

这条路足够清晰，也足够适合继续往前演进。
