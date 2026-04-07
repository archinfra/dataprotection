# 从公开 Installer 反推的 KubeStash 运行模型

本文依据本地公开安装包中的 Helm chart、CRD 和 installer API 进行分析，不涉及闭源 controller 实现。

## 1. 能明确看到的资源分层

从安装包里的 CRD 可以明确看出，KubeStash 至少把系统拆成了四层：

### 配置层

- `BackupConfiguration`
- `BackupBlueprint`
- `HookTemplate`

这一层表达“应该怎么备份、怎么恢复、要不要挂 hook、保留策略怎么选”。

### 执行层

- `BackupSession`
- `RestoreSession`
- `BackupVerificationSession`
- `BackupBatch`

这一层表达“某一次实际执行发生了什么”。

### 存储层

- `BackupStorage`
- `Repository`
- `Snapshot`
- `RetentionPolicy`

这一层表达“备份放哪、路径怎么组织、快照长什么样、保留规则是什么”。

### 插件/函数层

- `Addon`
- `Function`

这一层表达“驱动能力来自哪个 addon、如何运行某类任务、函数需要什么参数和挂载”。

## 2. 可以推断出的运行主线

即使没有 controller 源码，光从 CRD 结构也能比较清楚地反推出主流程：

1. `BackupConfiguration` 绑定一个备份目标。
2. `BackupConfiguration.spec.backends[]` 绑定多个后端。
3. 每个 backend 里有：
   - `storageRef`
   - `retentionPolicy`
4. `BackupConfiguration.spec.sessions[]` 定义一组会话模板。
5. 调度触发后，系统创建 `BackupSession`。
6. `BackupSession.status.snapshots[]` 记录本次会话在各 repository 上产生的快照。
7. 每个快照最终沉淀为独立 `Snapshot` 资源。
8. 恢复时通过 `RestoreSession` 指向 `repository + snapshot`。
9. retention 不是在执行器里随手做，而是由 `RetentionPolicy` 参与后处理。

## 3. 从 CRD 字段看出来的几个关键设计

### 3.1 Storage 和 Repository 分离

`BackupStorage` 更像“账号/后端提供商/运行时配置”。

`Repository` 更像“某个 app 在某个 storage 下的逻辑仓库路径”。

这意味着：

- 同一个对象存储账号可以承载多个应用仓库
- 凭据和仓库路径不是一个层级的问题
- repository 可以积累自己的统计和完整性状态

### 3.2 Retention 是独立对象

`RetentionPolicy` 不是内嵌在 `BackupConfiguration` 里，而是被 backend 引用。

这意味着：

- retention 可以被多个配置复用
- retention 可以单独审计和演进
- retention 结果可以回写到执行历史中

### 3.3 Session 是一等执行对象

`BackupSession` / `RestoreSession` 非常像“平台历史记录对象”，不是单纯的内部实现细节。

这意味着：

- 调度和执行是分层的
- 一次执行的状态、时长、hook、快照列表都能稳定追踪
- UI/API 可以围绕 session 展示，而不是围绕 Job 展示

### 3.4 Snapshot 是一等产物对象

`Snapshot` CRD 里直接有：

- `repository`
- `session`
- `backupSession`
- `snapshotID`
- `deletionPolicy`

并且 status 里还有组件级统计和大小信息。

这说明在 KubeStash 里，真正的“备份资产”不是某个文件路径，而是 `Snapshot`。

## 4. 对 dataprotection 的直接启发

结合你当前仓库，最值得照着演进的是：

### 已经对齐的

- `BackupSource` 对应保护目标
- `BackupRepository` 对应后端仓库
- `BackupPolicy` 对应长期策略
- `BackupRun` / `RestoreRequest` 对应执行对象
- `Snapshot` 已经补成一等资源

### 正在接近的

- `RetentionPolicy` 独立 CRD
- `BackupPolicy.spec.retentionPolicyRef`

### 还没对齐但最值得做的

1. 定时执行不再直接以 `CronJob -> 业务 Job` 为终态，而是 `CronJob -> BackupRun`。
2. `BackupRepository` 继续拆成：
   - `BackupStorage`
   - `Repository`
3. 增加 `HookTemplate` 和 pre/post hooks。
4. 在 `Snapshot.status` 中累积：
   - snapshotTime
   - size
   - repository
   - integrity / checksum 结果

## 5. 建议的下一阶段优先级

### P0

- 引入 `BackupStorage` CRD，把凭据/endpoint 和逻辑仓库拆开
- 定时调度改成生成 `BackupRun`
- 在 `BackupRun.status` 和 `Snapshot.status` 之间建立更稳定的映射

### P1

- 引入 `HookTemplate`
- 增加 preBackup / postBackup / preRestore / postRestore
- `Snapshot.status` 增加更完整的产物元数据

### P2

- 引入类似 `BackupBlueprint` 的模板对象
- 让应用级默认策略通过 blueprint 生成
- 为不同 driver 抽象统一执行器接口

## 6. 原则

继续参考 KubeStash 时，建议坚持这三个原则：

- 学资源分层
- 学对象边界
- 学运行历史模型

不要照抄它的命名、字段数量或闭源实现细节。

对你来说，最关键的是把系统逐步演进成：

- 配置对象负责声明
- 执行对象负责历史
- 快照对象负责资产
- 存储对象负责后端
- retention 和 hook 负责策略
