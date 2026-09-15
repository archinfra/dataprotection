# dataprotection v2

`dataprotection` 是一套面向 Kubernetes 中间件的逻辑备份与恢复控制面。

它把中间件自身的“数据导出 / 数据导入”与平台统一的“调度 / 执行 / 存储 / 快照 / 保留 / 通知”拆开。

## 核心对象

| 对象 | 作用 |
| --- | --- |
| `BackupAddon` | 定义某类中间件如何导出、导入数据 |
| `BackupSource` | 定义具体要保护的数据源实例 |
| `BackupStorage` | 定义备份资产存放位置 |
| `BackupPolicy` | 定义何时执行备份 |
| `BackupExecution` | **一次具体的备份执行，是备份执行状态的唯一事实源** |
| `Snapshot` | 一次成功执行产生的可恢复资产 |
| `RestoreJob` | 发起一次恢复 |
| `RetentionPolicy` | 定义备份资产保留策略 |
| `NotificationEndpoint` | 定义通知目标 |

`BackupJob` 仍然保留用于向后兼容，但不再直接执行 Kubernetes `Job`。新建备份应直接使用 `BackupExecution`。

`BackupExecution` 的 kubectl 简称为：

```bash
kubectl get backupexec -A
```

## 执行模型

所有备份最终统一到同一个执行对象：

```text
BackupSource
    │
BackupPolicy / Manual Request
    │
    ▼
BackupExecution
    │
    ▼
Kubernetes Job
    │
    ▼
Snapshot
```

`BackupExecution` 负责记录一次备份从开始到结束的完整状态，包括：

- 数据源与目标存储
- 触发方式：`Manual` / `Scheduled`
- 原生 Kubernetes Job
- 开始 / 完成时间
- 执行状态与错误信息
- 存储探测结果
- 最终产生的 `Snapshot`
- 通知投递状态

### 当前定时调度过渡实现

当前 `BackupPolicy` 仍使用 Kubernetes `CronJob` 作为调度载体。为了兼容现有实现，定时链路暂时是：

```text
BackupPolicy
    -> CronJob
    -> Kubernetes Job
    -> BackupExecution (adopt)
    -> Snapshot
```

这里 `JobObserver` 只负责把 CronJob 产生的原生 Job 物化为 `BackupExecution`；它不再负责终态处理、Snapshot 创建和通知。执行状态最终都由 `BackupExecution` 收敛。

手工执行直接走：

```text
BackupExecution
    -> Kubernetes Job
    -> Snapshot
```

旧接口兼容链路：

```text
BackupJob (legacy)
    -> BackupExecution
    -> Kubernetes Job
    -> Snapshot
```

## core 与 addon 的边界

- `addon` 只负责：`源系统 <-> Job Pod 工作目录`
- `core/operator` 负责：调度、执行控制、打包、上传、下载、快照登记、保留、通知

因此不同中间件不需要重复实现 NFS / MinIO 上传、Snapshot 管理和通知逻辑。

## 当前能力

当前控制面支持：

- NFS 与 MinIO 两类备份后端
- 手工与定时备份
- `Snapshot` 登记与恢复
- 离线导入恢复：`RestoreJob.spec.importSource`
- 保留策略
- 通知回调
- 多个官方 addon：MySQL、Redis、MinIO、Milvus

当前 `BackupPolicy.spec.storageRefs` 仍允许多个后端，并会为每个后端创建独立执行链。后续会继续收敛多目标语义，避免把“重复导出”误解为真正的一次导出、多目标分发。

## 当前支持矩阵

| 中间件 | addon 状态 | 说明 |
| --- | --- | --- |
| MySQL | 已内置 | 支持 `mysqldump` 备份与恢复 |
| Redis | 已内置 | 支持 standalone / cluster 的 RDB 导出 |
| MinIO | 已内置 | 支持 bucket/prefix mirror 备份与恢复 |
| Milvus | 已内置 | 支持 `milvus-backup` CLI，当前标记 beta |
| RabbitMQ | 未内置 | core 可以承载，但本仓库当前不附带官方 addon |

## 推荐落地顺序

1. 安装 `dataprotection` operator。
2. 准备运行命名空间与密钥。
3. 创建 `BackupStorage`。
4. 中间件安装时注册 `BackupAddon` 与 `BackupSource`。
5. 按需创建 `RetentionPolicy`、`NotificationEndpoint`、`BackupPolicy`。
6. 直接创建 `BackupExecution` 做首次 smoke backup。
7. 检查 `backupexec` 与 `Snapshot` 状态。
8. 使用 `RestoreJob` 做恢复演练。

手工执行示例：

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupExecution
metadata:
  name: mysql-prod-manual-nfs
  namespace: backup-system
spec:
  trigger: Manual
  sourceRef:
    name: mysql-prod
  storageRef:
    name: nfs-primary
  retentionRef:
    name: keep-last-3
  reason: manual smoke backup
```

查看执行：

```bash
kubectl get backupexec -n backup-system
kubectl describe backupexec mysql-prod-manual-nfs -n backup-system
```

## 文档导航

- 执行链路：[`docs/EXECUTION-FLOW.zh-CN.md`](docs/EXECUTION-FLOW.zh-CN.md)
- 快速上手：[`docs/QUICKSTART.zh-CN.md`](docs/QUICKSTART.zh-CN.md)
- 详细操作手册：[`docs/OPERATIONS-RUNBOOK.zh-CN.md`](docs/OPERATIONS-RUNBOOK.zh-CN.md)
- 场景说明：[`docs/USER-CASES.zh-CN.md`](docs/USER-CASES.zh-CN.md)
- 手工测试计划：[`docs/MANUAL-TEST-PLAN.zh-CN.md`](docs/MANUAL-TEST-PLAN.zh-CN.md)
- 样例入口：[`config/samples`](config/samples)

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

安装后重点检查：

```bash
kubectl get crd | grep dataprotection
kubectl get deploy -n data-protection-system
kubectl get backupexec -A
```
