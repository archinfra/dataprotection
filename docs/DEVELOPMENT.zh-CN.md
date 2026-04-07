# Data Protection Operator 开发说明

## 1. 仓库定位

这个仓库已经从 MySQL 项目中独立出来，目标是承载一个通用的数据保护控制面：

- 通用控制面：备份源、备份仓库、备份计划、手工备份、恢复请求
- 统一执行模型：`BackupPolicy -> CronJob`，`BackupRun/RestoreRequest -> Job`
- 可扩展数据面：先落 MySQL，后续再接 Redis、MongoDB、MinIO、RabbitMQ、Milvus

当前仓库地址约定为：

- `https://github.com/archinfra/dataprotection.git`

Go module 为：

- `github.com/archinfra/dataprotection`

## 2. 当前工程结构

- `api/v1alpha1`
  负责 CRD 类型、命名规则、字段校验。
- `controllers`
  负责 controller、幂等编排、状态回填，以及内建 runtime。
- `config/crd/bases`
  `controller-gen` 生成后的 CRD 清单。
- `config/samples`
  样例 CRD。
- `manifests`
  operator 安装模板。
- `scripts/install`
  `.run` 离线安装器源码。
- `.github/workflows`
  CI、镜像发布、离线安装包构建。

## 3. 当前闭环

已经跑通的主链路：

- `BackupSource` / `BackupRepository` 状态回填
- `BackupPolicy -> CronJob`
- `BackupRun -> Job`
- `RestoreRequest -> Job`
- 子 `Job` 状态聚合回 CRD
- MySQL + NFS
- MySQL + S3 / MinIO

## 4. MySQL 内建 Runtime 说明

位置：

- `controllers/mysql_runtime.go`

设计原则：

- 控制面继续用 Go controller 保证幂等、可观测和长期维护
- 数据面在 `Job` 里执行，避免把所有备份恢复逻辑塞进 controller 进程
- 对外暴露为“内建 runtime”，必要时仍允许用户通过 `execution.command/args` 覆盖

当前已支持：

- `mysqldump` 逻辑备份
- `.sql.gz` 恢复
- NFS 仓库
- S3 / MinIO 仓库
- 指定库
- 指定表
- `merge`
- `wipe-all-user-databases`
- 校验和
- 保留策略

## 5. 默认镜像策略

Controller 运行时默认镜像已经做成 Deployment 环境变量可配置：

- `DP_DEFAULT_RUNNER_IMAGE`
- `DP_DEFAULT_MYSQL_RUNNER_IMAGE`
- `DP_DEFAULT_S3_HELPER_IMAGE`

这样有两个好处：

- 在线安装时，可以直接指向 GitHub Actions 推送到 Docker Hub / 阿里云的镜像
- 离线安装时，可以由 `.run` 安装器渲染成目标私有仓库地址

## 6. 开发流程

推荐顺序：

1. 修改 API 或 controller
2. 运行 `make generate`
3. 运行 `make manifests`
4. 运行 `make test`
5. 运行 `make build`
6. 如需手工调试安装器，再运行 `bash build.sh --arch amd64`，但正式产包推荐走 GitHub Actions

常用命令：

```bash
bash hack/bootstrap-dev-env.sh
make fmt
make generate
make manifests
make test
make build
bash build.sh --arch amd64
```

## 7. GitHub Actions 约定

`ci.yml` 负责：

- 生成代码
- 生成 CRD
- 单测
- 安装器拼装 smoke test

`release.yml` 负责：

- 构建并推送多架构 operator 镜像
- 镜像同步到 Docker Hub 与阿里云
- 构建 `amd64` / `arm64` 的 `.run` 包
- tag 场景下附加到 GitHub Release

## 8. 工程约束

- controller 必须优先保证重入安全和幂等
- `BackupRun` / `RestoreRequest` 属于请求型资源，状态必须可审计
- 配置错误要尽量落成清晰的失败状态，而不是静默重试
- 安装器默认卸载 controller，不默认删 CRD，避免误删历史对象

