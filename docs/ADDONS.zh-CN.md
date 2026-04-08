# Addons 设计说明

当前仓库把 driver 侧的数据面能力收敛为“静态 addon”：

- `controllers/addon_registry.go`
- `controllers/addon_mysql.go`
- `controllers/addon_redis.go`
- `controllers/addon_minio.go`

这样做的目标不是实现动态插件系统，而是先把边界做清楚：

## Core 负责什么

- 解析 CRD
- 解析引用关系
- 调度 `CronJob`
- 维护 `BackupRun / Snapshot / RestoreRequest` 状态
- 生成统一的 Job 元数据

## Addon 负责什么

- 决定 driver 是否命中内建 runtime
- 生成 driver 专属的 PodSpec
- 生成 driver 专属脚本和容器组合
- 处理 driver 侧的连接、导出、导入、对象布局

## 当前内建 addon

### MySQL addon

- 逻辑备份和恢复
- NFS / S3 存储
- `latest.txt`
- `sha256`
- 自动建 bucket

### Redis addon

- 单点 RDB 备份
- Cluster master 自动发现
- 每个 master 拉取一份 RDB
- 打包为统一 snapshot tarball

### MinIO addon

- 把 MinIO 作为 source
- bucket / prefix 级 mirror
- 备份到 NFS 或 S3
- 从 snapshot mirror 回源 MinIO

## 为什么不是 controller 直接硬编码

因为如果继续把 Redis、MinIO、MongoDB 都直接塞进 reconcile 主流程，后面每加一个 driver，核心控制器都会越来越难读、越来越难测。

现在这种静态 addon 的组织方式更适合当前阶段：

- 不需要动态加载插件
- 不引入额外部署复杂度
- 但已经把边界拆清楚

后面如果要继续推进，可以沿着这个方向做：

1. 为 addon 增加更细的能力声明
2. 把更多通用存储脚本抽成共享模块
3. 再考虑把 addon 从 `controllers` 目录继续拆到独立 package
