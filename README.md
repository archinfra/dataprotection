# dataprotection v2

`dataprotection` v2 是一个面向 Kubernetes 的通用数据保护控制面。它不把“备份”做成某个中间件内部的脚本堆砌，而是把调度、存储、快照记录、通知、导入恢复和插件接入统一抽象出来，让 MySQL、MinIO、Milvus、Redis 这类数据面都可以接入同一套控制逻辑。

## 当前已经具备的能力

- 统一备份接入模型：`BackupAddon` + `BackupSource`
- 多种备份落点：`BackupStorage` 当前支持 `nfs` 和 `minio`
- 定时备份：`BackupPolicy`
- 手动单次备份：`BackupJob`
- 平台内快照恢复：`RestoreJob.spec.snapshotRef`
- 离线导入恢复：`RestoreJob.spec.importSource`
- 备份保留策略：`RetentionPolicy`
- Webhook 通知：`NotificationEndpoint`
- 快照资产记录：`Snapshot`
- 多存储 fan-out：一个 `BackupPolicy` 可以同时写入多个 `BackupStorage`
- 后端清理联动：保留策略不仅清理 `Snapshot` CR，也会清理 MinIO / NFS 上过期的归档文件

## 核心资源模型

### `BackupAddon`

定义某一类数据源如何执行“备份”和“恢复”容器逻辑。

职责边界：

- 备份时把结果写到 `/workspace/output`
- 恢复时从 `/workspace/input` 读取内容
- 不直接负责上传 NFS / MinIO，也不直接负责保留策略

### `BackupSource`

表示一个具体的数据源实例，绑定到某个 `BackupAddon`。

它可以携带：

- `targetRef`：指向被保护对象
- `endpoint`：连接信息
- `parameters`：普通参数
- `secretRefs`：敏感参数
- `paused`：暂停该数据源的调和

### `BackupStorage`

定义备份资产最终落到哪里。

当前支持：

- `spec.type: nfs`
- `spec.type: minio`

控制器会在执行前做存储 preflight，并把结果写入：

- `status.lastProbeResult`
- `status.lastProbeMessage`
- `status.lastProbeTime`

### `RetentionPolicy`

定义保留策略：

- `spec.successfulSnapshots.keepLast`
- `spec.failedExecutions.keepLast`

保留策略现在不只是“删 CR”，也会同步删除后端 NFS / MinIO 上超出的历史备份归档、校验文件和元数据文件。

### `BackupPolicy`

表示周期性备份策略。

关键点：

- `spec.storageRefs` 支持多个存储目标
- 控制器会按存储目标生成多个原生 `CronJob`
- `status.cronJobNames` 会记录实际生成的 `CronJob`
- `spec.suspend` 可暂停调度

这意味着“同一个数据源同时写入多个备份中心”已经是原生能力，而不是额外脚本拼接。

### `BackupJob`

表示一次手动触发的单次备份。

典型场景：

- 升级前手工打一份备份
- 演练时只向某一个存储目标打一份备份
- 验证 addon 是否工作正常

### `RestoreJob`

表示一次恢复任务，当前有两种来源：

#### 1. 平台内快照恢复

使用 `spec.snapshotRef`，适合恢复平台内成功生成的标准快照。

#### 2. 导入恢复

使用 `spec.importSource`，适合下面这些场景：

- A 集群导出，B 集群导入
- 本地离线包上传到 MinIO 后恢复
- NFS 上已经有目录或单文件，希望直接恢复

`importSource` 关键字段：

- `storageRef`：从哪个 `BackupStorage` 读取
- `path`：相对存储根路径
- `format`：`auto | archive | filesystem`
- `series`：导入后写入的 series 名称
- `snapshot`：导入后使用的快照名

### `Snapshot`

平台内对一次成功备份资产的登记记录。

关键字段：

- `spec.series`
- `spec.storageRef`
- `spec.backendPath`
- `spec.snapshot`
- `spec.checksum`
- `spec.size`
- `status.artifactReady`
- `status.latest`

## 推荐交付方式

更接近真实生产的流程应该是：

1. 先安装 `dataprotection` operator
2. 在 `backup-system` 中准备一套只用于备份的 MinIO
3. 如有需要，再补一套只用于备份的 NFS
4. 再安装具体中间件
5. 在中间件安装阶段自动注册自己的 `BackupAddon / BackupSource / BackupPolicy`

这也是现在更推荐的方向：不要让现场再额外理解一套“独立 addon 安装包”，而是在中间件安装时顺带把备份接入注册进去。

## 新增能力梳理

### 1. 多存储定时备份

`BackupPolicy.spec.storageRefs` 现在是数组。一个策略可以同时写入多个 `BackupStorage`。

例如：

- `minio-primary`
- `nfs-primary`

控制器会为每个存储目标分别生成一个 `CronJob`，这样调度、失败、重试和观测都更清晰。

### 2. 导入恢复

`RestoreJob` 不再只能依赖平台内 `Snapshot`。现在可以直接从外部导入路径恢复：

- `.tgz`
- `.tar.gz`
- `.tar`
- 目录
- 单文件

`format=auto` 时：

- `.tgz/.tar.gz/.tar` 按归档包处理
- 其它路径按文件系统内容处理

### 3. 保留策略同步清理后端文件

当保留策略超出 `keepLast` 时，控制器会同时处理：

- `Snapshot` 记录
- NFS 后端的 `.tgz / .sha256 / .metadata.json`
- MinIO 后端的 `.tgz / .sha256 / .metadata.json`

这就是你前面遇到“后端还有过期文件残留”的核心改进点。

### 4. 恢复时覆盖连接目标

`RestoreJob` 可以覆盖来自 `BackupSource` 的默认目标信息：

- `targetRef`
- `endpoint`
- `parameters`
- `secretRefs`

也就是说，同一份备份可以恢复到不同环境或不同实例，而不必强制复制一套新的 `BackupSource`。

### 5. 通知统一收敛

`BackupPolicy`、`BackupJob`、`RestoreJob` 都支持 `notificationRefs`。

通知结果会写回状态中，便于排障：

- `status.notification.phase`
- `status.notification.attempts`
- `status.notification.message`

## 归档格式说明

当前平台内部标准备份归档格式是：

- `${snapshot}.tgz`
- `${snapshot}.tgz.sha256`
- `${snapshot}.metadata.json`

平台会把 addon 导出的 `/workspace/output` 打成归档，再上传到 NFS / MinIO。

## 当前作用边界

`dataprotection core` 负责：

- 调度 `CronJob -> Job`
- 存储 preflight
- 归档打包
- 上传 / 下载 NFS 或 MinIO
- 创建与维护 `Snapshot`
- 执行保留策略
- 触发通知

`BackupAddon` 负责：

- 面向具体业务的数据导出
- 面向具体业务的数据导入

## Quickstart

推荐直接看：

- [docs/QUICKSTART.zh-CN.md](docs/QUICKSTART.zh-CN.md)
- [docs/USER-CASES.zh-CN.md](docs/USER-CASES.zh-CN.md)
- [config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml](config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml)

## 当前样例目录

- [config/samples](config/samples)
- [config/samples/quickstart](config/samples/quickstart)
- [addons](addons)

## 构建

```bash
make generate
make manifests
make test
APP_VERSION="$(cat VERSION)" bash scripts/assemble-install.sh install.sh
```

## 当前更适合怎么理解这个项目

一句话版本：

`dataprotection` 不是“某个中间件的备份脚本合集”，而是一套通用的数据保护控制面；中间件 addon 只是这个控制面上的业务插件。
