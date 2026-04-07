# KubeStash 运行模型对当前仓库的启发

当前对我们仍然有效的运行模型总结如下：

## 运行链路要分层

最有价值的不是具体脚本，而是层次：

- 长期策略对象
- 执行历史对象
- 快照对象
- 恢复对象

在当前仓库里的对应关系是：

- `BackupPolicy`
- `BackupRun`
- `Snapshot`
- `RestoreRequest`

## 存储身份和落盘路径都要显式保存

所以现在：

- `BackupRun.status.storages[]` 会记录 `storagePath`
- `Snapshot.spec` 会记录 `storageRef` 和 `storagePath`

这让 restore 不需要额外依赖 repository 对象。

## 当前落地形式

```text
BackupPolicy -> CronJob -> BackupRun -> Job -> Snapshot
RestoreRequest -> Snapshot/StoragePath -> Job
```

这是当前仓库已经落实的模型，而不是未来计划。
