# dataprotection v2

`dataprotection` 是一套面向 Kubernetes 的通用数据保护控制面。

它把“备份脚本”从具体中间件安装流程里抽离出来，统一抽象成：

- 备份接入：`BackupAddon` + `BackupSource`
- 备份后端：`BackupStorage`
- 定时策略：`BackupPolicy`
- 手动执行：`BackupJob`
- 快照登记：`Snapshot`
- 恢复入口：`RestoreJob`
- 保留策略：`RetentionPolicy`
- 通知回调：`NotificationEndpoint`

当前这套控制面已经支持：

- NFS 与 MinIO 两类备份后端
- 平台内 `Snapshot` 恢复
- 离线导入恢复：`RestoreJob.spec.importSource`
- 多后端 fan-out：一个策略同时写多个 `BackupStorage`
- 删除过期 `Snapshot` 时同步清理 MinIO / NFS 后端归档文件

## 推荐理解方式

更推荐把这个项目理解为“平台级数据保护能力”，而不是“某一个中间件的备份脚本合集”。

- `core/operator` 负责调度、打包、上传、下载、保留、通知
- `addon` 只负责具体中间件的数据导出与导入
- 中间件项目可以在安装阶段自动注册自己的 `BackupAddon / BackupSource / BackupPolicy`

## 当前支持矩阵

`dataprotection` core 当前可以承载任意自定义 addon，但这个仓库随附的官方 addon 交付物只覆盖下面几类：

| 中间件 | addon 状态 | 说明 |
| --- | --- | --- |
| MySQL | 已内置 | 支持 `mysqldump` 备份与恢复 |
| Redis | 已内置 | 支持 standalone / cluster 的 RDB 导出 |
| MinIO | 已内置 | 支持 bucket/prefix mirror 备份与恢复 |
| Milvus | 已内置 | 支持 `milvus-backup` CLI，当前标记 beta |
| RabbitMQ | 未内置 | core 可以承载，但本仓库当前 tag 不附带官方 RabbitMQ addon 包 |

这意味着：

- MySQL / Redis / MinIO / Milvus 可以直接参考本仓库样例与 addon 包使用
- RabbitMQ 如果已经在你们自己的中间件安装器里注册了 `BackupAddon / BackupSource`，可以直接复用 core/operator 的策略、后端和恢复流程
- 如果还没有 RabbitMQ addon，需要先补 addon，再接入这套控制面

## 推荐落地顺序

1. 安装 `dataprotection` operator
2. 准备 `backup-system` 命名空间与运行时密钥
3. 准备 MinIO / NFS 备份后端
4. 安装中间件，并注册各自的 `BackupAddon / BackupSource`
5. 按需创建 `BackupPolicy`、`RetentionPolicy`、`NotificationEndpoint`
6. 用 `BackupJob` 做首轮 smoke backup
7. 用 `RestoreJob` 做恢复演练与导入恢复验证

## 文档导航

- 快速上手：[`docs/QUICKSTART.zh-CN.md`](docs/QUICKSTART.zh-CN.md)
- 详细操作手册：[`docs/OPERATIONS-RUNBOOK.zh-CN.md`](docs/OPERATIONS-RUNBOOK.zh-CN.md)
- 场景说明：[`docs/USER-CASES.zh-CN.md`](docs/USER-CASES.zh-CN.md)
- 手工测试计划：[`docs/MANUAL-TEST-PLAN.zh-CN.md`](docs/MANUAL-TEST-PLAN.zh-CN.md)
- 样例入口：[`config/samples`](config/samples)
- quickstart 样例：[`config/samples/quickstart`](config/samples/quickstart)

## 最常用的资源对象

### 1. `BackupSource`

表示一个具体的受保护数据源实例，例如：

- `mysql-prod`
- `redis-cluster`
- `minio-source`
- `milvus-prod`

它描述：

- 备份使用哪个 addon
- 连接哪个实例
- 使用哪些参数和密钥

### 2. `BackupStorage`

表示备份资产最终写到哪里，当前支持：

- `spec.type: minio`
- `spec.type: nfs`

### 3. `BackupPolicy`

表示定时备份策略。重点是：

- `spec.storageRefs` 支持多个后端
- operator 会为每个后端生成独立 `CronJob`
- `status.cronJobNames` 会记录实际下发的 `CronJob`

### 4. `RestoreJob`

恢复任务当前有两种来源：

- `snapshotRef`：从平台内登记的 `Snapshot` 恢复
- `importSource`：从 NFS / MinIO 上已有的离线导出包、目录或单文件恢复

## 构建与校验

```bash
make generate
make manifests
make test
APP_VERSION="$(cat VERSION)" bash scripts/assemble-install.sh install.sh
```

## operator 安装

```bash
./data-protection-operator-amd64.run install -y
```

安装完成后重点检查：

```bash
kubectl get crd | grep dataprotection
kubectl get deploy -n data-protection-system
```

## 典型样例

- 单 MinIO 周期备份：[`config/samples/quickstart/07-backuppolicy-minio-every-3m.yaml`](config/samples/quickstart/07-backuppolicy-minio-every-3m.yaml)
- 手动 NFS 备份：[`config/samples/quickstart/08-backupjob-manual-nfs.yaml`](config/samples/quickstart/08-backupjob-manual-nfs.yaml)
- Snapshot 恢复：[`config/samples/quickstart/09-restorejob-from-snapshot.yaml`](config/samples/quickstart/09-restorejob-from-snapshot.yaml)
- 导入包恢复：[`config/samples/quickstart/10-restorejob-from-import.yaml`](config/samples/quickstart/10-restorejob-from-import.yaml)
- MinIO + NFS 双落点：[`config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml`](config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml)

## 与中间件项目的关系

更推荐的项目交付方式是：

- `apps_mysql`、`apps_redis`、`apps_milvus-cluster`、`apps_minio-cluster` 在安装时自动把备份对象注册到平台
- 运维侧只维护：
  - 存储后端
  - 保留策略
  - 通知
  - 恢复演练

这样就不会让现场同学再理解两套割裂的交付入口。
