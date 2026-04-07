# 从 KubeStash 借鉴到的结论

这份文档只保留现在仍然有效的启发，不再保留已经被当前实现推翻的旧设计草稿。

## 借鉴结论

### 1. 配置对象和执行对象要分开

KubeStash 的公开设计里，一个长期配置不会直接等同于一次执行。

对我们当前实现的启发就是：

- `BackupPolicy` 是长期配置
- `BackupRun` 是一次执行

### 2. Snapshot 必须是独立资源

快照不是一个字符串，也不是一个 `latest.txt` 的别名。

所以我们现在保留：

- `Snapshot.spec.storageRef`
- `Snapshot.spec.storagePath`
- `Snapshot.spec.snapshot`

### 3. 恢复要围绕 Snapshot 做

恢复最稳定的入口是 `snapshotRef`，而不是手工拼路径。

这也是为什么当前实现已经不再保留 `BackupRepository`。

### 4. 数据面必须离开 controller

controller 负责协调，`Job` 负责实际备份/恢复。

这条原则同时适用于 Stash 和 KubeStash，也适用于我们当前仓库。
