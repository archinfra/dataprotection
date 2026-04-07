# 从开源 Stash 借鉴到的下一步演进

这次不是照搬 Stash 的实现，而是吸收它比较成熟的资源分层思路，再按 `dataprotection` 当前代码形态落地。

## 这次已经落地的能力

- 新增 `BackupStorage`，负责承载可复用的 NFS / S3 存储后端配置
- `BackupRepository` 变成逻辑仓库对象，新增 `spec.storageRef` 和 `spec.path`
- `BackupRepository.spec.path` 会叠加到共享存储根路径上
- 继续兼容旧的 inline `spec.type/nfs/s3` 写法，避免现有资源一次性失效
- 当 `BackupRepository` 没有显式 `storageRef` 且没有 inline backend 时，会尝试使用命名空间内默认的 `BackupStorage`

## 为什么这一步最值得先做

Stash 给人的最大启发，不是“备份脚本怎么写”，而是对象边界非常清楚：

- 存储后端是一类对象
- 逻辑仓库是一类对象
- 执行历史是一类对象
- 快照资产是一类对象
- 保留策略是一类对象

如果把这些职责都塞进一个 `BackupRepository`，短期能跑，长期会越来越难维护：

- 多个业务仓库不能复用同一套 MinIO / NFS 后端配置
- 路径和凭据耦合在一起，不方便做默认存储和权限治理
- 后续要补删除策略、完整性探测、容量统计时，字段边界会越来越混乱

## 现在的推荐模型

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupStorage
metadata:
  name: minio-primary
  namespace: backup-system
spec:
  default: true
  type: s3
  s3:
    endpoint: https://minio.example.com
    bucket: data-protection
    prefix: platform
---
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRepository
metadata:
  name: mysql-prod
  namespace: backup-system
spec:
  storageRef:
    name: minio-primary
  path: mysql/prod
```

这样实际落盘路径会变成：

- S3: `platform/mysql/prod/...`
- NFS: `<storage.nfs.path>/mysql/prod/...`

## 下一步最值得继续借鉴 Stash 的点

1. 把定时链路从 `BackupPolicy -> CronJob -> Job` 升级成 `BackupPolicy -> CronJob -> BackupRun -> Job`
2. 给 `Snapshot.status` 增加更完整的产物元数据，比如大小、时间、完整性结果
3. 引入 `HookTemplate`，把 pre/post backup 与 restore 的 hook 做成可复用模板

这三个方向里，第一项收益最大，因为它会让定时备份也拥有 CRD 级历史对象，而不是只留在 `CronJob/Job` 里。
