# Data Protection Operator 开发说明

## 仓库定位

这个仓库承载的是数据保护控制面，而不是单一数据库脚本工程。

当前核心模型已经稳定在下面这几类对象：

- `BackupSource`
- `BackupStorage`
- `BackupPolicy`
- `BackupRun`
- `Snapshot`
- `RestoreRequest`
- `RetentionPolicy`

其中最重要的设计取舍是：

- 调度交给 Kubernetes `CronJob`
- 执行历史交给 `BackupRun`
- 备份资产交给 `Snapshot`
- 真正的数据动作放进 `Job`

## 当前主链路

### 定时备份

```text
BackupPolicy -> CronJob -> trigger-backup-run -> BackupRun -> Job -> Snapshot
```

### 手工备份

```text
BackupRun -> Job -> Snapshot
```

### 恢复

```text
RestoreRequest -> resolve snapshot/storage/path -> Job
```

## 目录说明

- `api/v1alpha1`
  API 类型、字段校验、命名规则
- `controllers`
  controller、状态回填、公共渲染逻辑、内建 runtime
- `config/crd/bases`
  生成后的 CRD
- `config/samples`
  当前推荐样例
- `manifests`
  operator 安装模板
- `scripts/install`
  离线安装器源码

## 最关键的代码入口

- [main.go](/C:/Users/admin/Desktop/release/dataprotection/main.go)
  operator 启动入口
- [trigger_backuprun.go](/C:/Users/admin/Desktop/release/dataprotection/trigger_backuprun.go)
  `CronJob -> BackupRun` 触发桥接
- [controllers/backuppolicy_controller.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/backuppolicy_controller.go)
  调度层
- [controllers/backuprun_controller.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/backuprun_controller.go)
  执行层与快照层
- [controllers/restorerequest_controller.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/restorerequest_controller.go)
  恢复层
- [controllers/runtime_helpers.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/runtime_helpers.go)
  公共依赖解析和 storage path 规则
- [controllers/mysql_runtime.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/mysql_runtime.go)
  MySQL 内建数据面

## 为什么不用 controller 直接定时触发 Job

因为那样会把两件事耦在一起：

- 调度
- 执行历史

现在的实现把它们拆开了：

- `CronJob` 负责“按时触发”
- `BackupRun` 负责“记录这一次执行”
- `Job` 负责“做这一次数据动作”

这比 controller 自己算时间更好维护，也更接近 Stash 的成熟思路。

## Storage 路径规则

当前不再保留 `BackupRepository` 这层对象，统一由控制器计算稳定路径：

- policy 备份：
  `backups/<driver>/<namespace>/<source>/policies/<policy>`
- 手工 run：
  `backups/<driver>/<namespace>/<source>/runs/<run>`

这条规则由 [controllers/runtime_helpers.go](/C:/Users/admin/Desktop/release/dataprotection/controllers/runtime_helpers.go) 中的 `backupArtifactPath()` 维护。

## 本地开发命令

```bash
bash hack/bootstrap-dev-env.sh
make fmt
make generate
make manifests
make test
make build
```

## 改 API / controller 后的推荐顺序

1. 修改 Go 代码
2. 运行 `make fmt`
3. 运行 `make generate`
4. 运行 `make manifests`
5. 运行 `make test`
6. 如需离线包，再跑 `bash build.sh --arch amd64`

## 当前默认镜像环境变量

operator Deployment 会注入这些默认镜像：

- `DP_DEFAULT_RUNNER_IMAGE`
- `DP_DEFAULT_MYSQL_RUNNER_IMAGE`
- `DP_DEFAULT_S3_HELPER_IMAGE`
- `DP_DEFAULT_CONTROLLER_IMAGE`
- `DP_DEFAULT_TRIGGER_SERVICE_ACCOUNT`

其中 `DP_DEFAULT_CONTROLLER_IMAGE` 专门用于 `CronJob` 里的触发容器。

## 给后续维护者的建议

- 先看 README，再看 `runtime_helpers.go`
- 想改调度链路，优先改 `BackupPolicy` 和 `trigger_backuprun.go`
- 想改执行链路，优先改 `BackupRun` controller 和对应 runtime
- 想新增 driver，不要先碰调度层，先把数据面做成独立 runtime
