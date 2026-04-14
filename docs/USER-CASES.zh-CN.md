# User Cases

如果你已经完成 operator 安装，下一步要进入实际运维与演练，建议同时打开：

- `docs/OPERATIONS-RUNBOOK.zh-CN.md`

这份文档按“真实使用场景”来解释 `dataprotection` 当前适合怎么用。

## 场景 1：只把备份写到备份专用 MinIO

### 适用场景

- 只有一套主备份中心
- 希望对象存储统一管理
- 后续需要跨集群导入恢复

### 资源组合

- `BackupStorage/minio-primary`
- `RetentionPolicy/keep-last-3`
- `BackupPolicy/mysql-smoke-minio`

### 推荐做法

- 周期备份只写 MinIO
- 手动备份也优先写 MinIO
- 恢复时优先用平台内 `Snapshot`

## 场景 2：同一份数据同时写入 MinIO + NFS

### 适用场景

- 既要对象存储，也要目录型备份
- 需要多中心 / 多落点
- 想让恢复路径更多样

### 核心能力

`BackupPolicy.spec.storageRefs` 支持多个目标，例如：

```yaml
storageRefs:
  - name: minio-primary
  - name: nfs-primary
```

控制器会为每个目标生成一个独立 `CronJob`，并写入：

- `BackupPolicy.status.cronJobNames`

### 建议验证

```bash
kubectl get cronjob -n backup-system
kubectl get snap -n backup-system
```

你应该能看到同一个 `sourceRef` 对应多个不同 `storageRef` 的快照。

## 场景 3：升级前手动打一份备份

### 适用场景

- 升级中间件之前
- 改 schema 之前
- 做一次性的演练或 smoke test

### 推荐资源

- `BackupJob`

### 为什么不用 `BackupPolicy`

`BackupPolicy` 适合长期周期性运行；手工立即执行时，用 `BackupJob` 更直接，不会引入额外 `CronJob`。

## 场景 4：从平台内成功快照恢复

### 适用场景

- 平台内已经成功跑过备份
- 你希望从 `Snapshot` 直接恢复

### 推荐资源

- `RestoreJob.spec.snapshotRef`

### 优点

- 语义最清晰
- 不需要手工指定后端路径
- 恢复来源是平台已登记的可恢复资产

## 场景 5：从外部导入包恢复

### 适用场景

- A 集群导出，B 集群导入
- 手头只有 `.tgz`
- 后端已经有一个目录或单文件要恢复

### 推荐资源

- `RestoreJob.spec.importSource`

### 当前支持的导入形态

- `.tgz`
- `.tar.gz`
- `.tar`
- 目录
- 单文件

### 关键字段

- `storageRef`
- `path`
- `format`
- `series`
- `snapshot`

## 场景 6：恢复到不同目标

### 适用场景

- 同一份备份要恢复到另一套实例
- 恢复环境的连接地址、密码、参数与源环境不同

### 当前能力

`RestoreJob` 可以覆盖或补充：

- `targetRef`
- `endpoint`
- `parameters`
- `secretRefs`

这意味着：

- `BackupSource` 可以存“默认目标”
- `RestoreJob` 可以临时覆盖为“本次恢复目标”

## 场景 7：希望保留策略同时清理后端垃圾

### 适用场景

- 之前后端残留很多历史文件
- 不希望只删 CR，不删对象或目录

### 当前行为

保留策略超出 `keepLast` 时，会同步清理：

- `Snapshot`
- NFS 上旧的 `.tgz / .sha256 / .metadata.json`
- MinIO 上旧的 `.tgz / .sha256 / .metadata.json`

### 验证方法

同时观察：

- `kubectl get snap -n backup-system`
- NFS 目录
- MinIO bucket/prefix

## 场景 8：希望接入 webhook 通知

### 适用场景

- 备份成功 / 失败要回调到运维平台
- 恢复完成要通知业务方

### 推荐资源

- `NotificationEndpoint`
- `notificationRefs`

### 适用对象

- `BackupPolicy`
- `BackupJob`
- `RestoreJob`

## 场景 9：把 dataprotection 作为平台能力，而不是中间件脚本

### 推荐理解方式

不要把它理解成：

- “MySQL 备份脚本”
- “Redis 备份脚本”

更适合理解成：

- 平台层数据保护控制面
- 中间件通过 addon 接入
- 备份执行逻辑属于 addon
- 调度、上传、保留、通知、恢复入口属于 core

## 当前最推荐的落地路径

1. 安装 `dataprotection` core
2. 安装备份专用存储
3. 安装中间件
4. 在中间件安装阶段自动注册自己的 `BackupAddon / BackupSource / BackupPolicy`
5. 运维侧只维护：
   - 存储
   - 策略
   - 保留
   - 通知
