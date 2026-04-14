# dataprotection 操作手册

这份手册面向“operator 已安装完成，接下来要开始真正做备份、恢复、演练和验收”的场景。

目标不是解释 CRD 概念，而是帮助运维、实施同学，或者后续接手项目的 AI，直接按步骤把下面几类事情做完：

- 准备备份后端
- 注册中间件备份接入
- 创建定时备份与手动备份
- 做 Snapshot 恢复演练
- 做离线导出包导入恢复演练
- 验证 MinIO / NFS 后端中的备份资产与清理行为

## 1. 当前适用范围

本仓库当前 tag 下，官方随附 addon 交付物覆盖：

- MySQL
- Redis
- Milvus
- MinIO

RabbitMQ 当前情况需要单独说明：

- `dataprotection core/operator` 可以承载 RabbitMQ 的备份 addon
- 但这个仓库当前没有随附官方 `RabbitMQ BackupAddon` 包
- 所以下面会给出 RabbitMQ 的接入建议、验证口径和推荐契约
- 但不会把 RabbitMQ 写成“开箱即用、已经内置在本 tag 里”

## 2. 安装后的验收基线

先确认 operator 已经健康：

```bash
kubectl get crd | grep dataprotection
kubectl get deploy -n data-protection-system
kubectl get pods -n data-protection-system
```

重点结果：

- 所有 `dataprotection.archinfra.io/*` CRD 已存在
- `data-protection-operator-controller-manager` 为 `Ready`
- controller 日志中没有持续报错

建议再确认默认镜像参数：

```bash
kubectl get deploy -n data-protection-system data-protection-operator-controller-manager -o yaml
```

重点关注：

- `DP_DEFAULT_MINIO_HELPER_IMAGE`
- `DP_DEFAULT_UTILITY_IMAGE`

如果你们是离线环境，要确认这些已经指向内网镜像。

## 3. 推荐命名空间与共享密钥

默认建议把平台资源放在 `backup-system`：

```bash
kubectl apply -f config/samples/quickstart/00-namespace-secrets.yaml
```

这个样例会准备：

- `Namespace/backup-system`
- `Secret/mysql-runtime-auth`
- `Secret/minio-credentials`

如果你的现场统一使用别的命名空间，例如 `back-system`，把所有样例里的命名空间统一替换掉即可。

## 4. 备份后端手册

### 4.1 MinIO 作为主备份后端

适用场景：

- 需要跨集群导入恢复
- 需要对象存储统一管理
- 需要与离线导出包场景打通

创建方式：

```bash
kubectl apply -f config/samples/quickstart/03-backupstorage-minio.yaml
```

检查方式：

```bash
kubectl get bst -n backup-system
kubectl describe bst minio-primary -n backup-system
```

重点字段：

- `status.phase`
- `status.lastProbeResult`
- `status.lastProbeMessage`
- `status.lastProbeTime`

只要 `lastProbeResult=Succeeded`，就说明 operator 能访问这个 MinIO bucket/prefix。

### 4.2 NFS 作为第二落点

适用场景：

- 需要目录型备份落点
- 需要和现网 NFS 体系对接
- 希望对象存储与文件存储双保险

创建方式：

```bash
kubectl apply -f config/samples/quickstart/04-backupstorage-nfs.yaml
```

检查方式：

```bash
kubectl get bst -n backup-system
kubectl describe bst nfs-primary -n backup-system
```

### 4.3 MinIO + NFS 双落点 fan-out

这是当前更推荐的生产模式：

- MinIO 负责对象化存储、跨集群导入恢复、长期留存
- NFS 负责本地快速查看、文件系统式排障、第二落点

先准备两个 `BackupStorage`，再创建 fan-out `BackupPolicy`：

```bash
kubectl apply -f config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml
```

检查方式：

```bash
kubectl get bp -n backup-system
kubectl describe bp mysql-smoke-fanout -n backup-system
kubectl get cronjob -n backup-system
```

你应该能看到：

- 一个 `BackupPolicy`
- 多个 `CronJob`
- `status.cronJobNames` 中有按存储后端拆开的调度任务

## 5. 平台通用排障命令

日常最常用的一组命令：

```bash
kubectl get ba,bsrc,bst,bp,bj,rj,snap,rp,ne -n backup-system
kubectl get cronjob,job,pod -n backup-system
```

看某次备份：

```bash
kubectl describe bj <backupjob-name> -n backup-system
kubectl get pod -n backup-system
kubectl logs -n backup-system job/<job-name> -c addon
kubectl logs -n backup-system job/<job-name> -c storage
```

看某次恢复：

```bash
kubectl describe rj <restorejob-name> -n backup-system
kubectl logs -n backup-system job/<job-name> -c addon
kubectl logs -n backup-system job/<job-name> -c storage
```

看快照与后端路径：

```bash
kubectl get snap -n backup-system
kubectl describe snap <snapshot-name> -n backup-system
```

重点字段：

- `spec.storageRef`
- `spec.backendPath`
- `status.artifactReady`
- `status.latest`

## 6. 导出 / 导入 / 恢复场景手册

### 6.1 场景 A：平台内 Snapshot 恢复

适合：

- 同一个集群内做恢复演练
- 某次定时备份已经成功
- 不想手工关心后端具体文件路径

步骤：

1. 先找到一个成功的 `Snapshot`
2. 把它填入 `RestoreJob.spec.snapshotRef`
3. 执行恢复

示例：

```bash
kubectl get snap -n backup-system
kubectl apply -f config/samples/quickstart/09-restorejob-from-snapshot.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

### 6.2 场景 B：离线导出包导入恢复

适合：

- A 集群导出，B 集群导入
- 先手工把包上传到了 MinIO / NFS
- 本地已有 `.tgz` / `.tar.gz` / `.tar` / 目录 / 单文件

使用对象：

- `RestoreJob.spec.importSource`

示例：

```bash
kubectl apply -f config/samples/quickstart/10-restorejob-from-import.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

`importSource` 关键字段：

- `storageRef.name`：指向已有 `BackupStorage`
- `path`：相对存储根目录的路径
- `format`：`auto | archive | filesystem`
- `series`：导入后想记录的逻辑系列名
- `snapshot`：导入后想使用的快照名

### 6.3 场景 C：跨集群迁移

推荐做法：

1. 源集群正常跑一次备份，得到 `Snapshot`
2. 从后端拿到归档包，例如 `xxx.tgz`
3. 把归档包上传到目标集群可访问的 `BackupStorage`
4. 在目标集群创建同类型 `BackupSource`
5. 用 `RestoreJob.spec.importSource` 做恢复

最稳妥的实践是：

- 源集群和目标集群都统一用 MinIO 作为跨集群中转后端
- `series` 带上集群来源，例如 `import/cluster-a/mysql-prod`
- `snapshot` 带上来源标识，例如 `mysql-prod-cluster-a-export`

### 6.4 场景 D：验证保留策略是否同步删除后端文件

这个场景非常重要，因为它直接验证你前面关心的“CR 删除了，但对象存储里旧文件还残留”的问题是否已经收住。

步骤：

1. 为同一 `BackupSource` 连续产生多次备份
2. 保留策略设置较小值，例如 `keepLast: 3`
3. 等待超过保留窗口
4. 同时检查：
   - `Snapshot` 数量
   - MinIO bucket/prefix 中对象数量
   - NFS 目录中文件数量

预期结果：

- 旧 `Snapshot` 被删掉
- 对应的 `.tgz`
- 对应的 `.sha256`
- 对应的 `.metadata.json`

也会从 MinIO / NFS 后端一起删掉

## 7. MySQL 备份与恢复手册

### 7.1 适用前提

你可以走两种路径：

- 推荐路径：由 `apps_mysql` 安装器自动注册 MySQL 备份对象
- 兼容路径：单独安装 `dataprotection-addon-mysql-<arch>.run`

如果走兼容路径：

```bash
./dataprotection-addon-mysql-amd64.run install -y
```

### 7.2 注册 MySQL 数据源

样例：

```bash
kubectl apply -f addons/mysql/manifests/samples/backupsource.yaml
```

最关键的参数是：

- `endpoint.host`
- `endpoint.port`
- `endpoint.username`
- `endpoint.passwordFrom`
- `parameters.database`

如果不传 `parameters.database`，addon 会执行全库 `mysqldump --all-databases`。

### 7.3 做一次手动备份

```bash
kubectl apply -f config/samples/dataprotection_v1alpha1_backupjob.yaml
kubectl get bj -n backup-system
kubectl get snap -n backup-system
```

### 7.4 备份验证手册

建议先做一个明确的 smoke 数据：

1. 在业务库里创建 `dp_smoke` 表
2. 插入一条唯一标记数据，例如时间戳
3. 执行 `BackupJob`
4. 确认 `Snapshot` 生成成功

验证点：

- `BackupJob.status.phase=Succeeded`
- `Snapshot.status.artifactReady=true`
- MinIO / NFS 后端能看到归档包

如果要看 addon 是否真的导出了 SQL：

```bash
kubectl logs -n backup-system job/<job-name> -c addon
```

MySQL addon 的输出文件是：

- `/workspace/output/dump.sql`

### 7.5 恢复手册

路径一：从 `Snapshot` 恢复

```bash
kubectl apply -f addons/mysql/manifests/samples/restorejob.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

路径二：从导出包恢复

```bash
kubectl apply -f config/samples/quickstart/10-restorejob-from-import.yaml
```

### 7.6 恢复验证手册

最稳妥的验证方式：

1. 备份前先写入唯一 smoke 数据
2. 备份成功后删除这条数据，或删除整张 smoke 表
3. 执行恢复
4. 重新查询数据库，确认 smoke 数据回来

建议验收命令：

```bash
mysql -h <mysql-host> -P 3306 -u root -p -e "SHOW DATABASES;"
mysql -h <mysql-host> -P 3306 -u root -p -e "SELECT * FROM <db>.dp_smoke;"
```

## 8. Redis 备份与恢复手册

### 8.1 适用前提

兼容路径：

```bash
./dataprotection-addon-redis-amd64.run install -y
```

### 8.2 注册 Redis 数据源

Standalone：

```bash
kubectl apply -f addons/redis/manifests/samples/backupsource-standalone.yaml
```

Cluster：

```bash
kubectl apply -f addons/redis/manifests/samples/backupsource-cluster.yaml
```

关键参数：

- `endpoint.host`
- `endpoint.port`
- `endpoint.passwordFrom`
- `parameters.mode=standalone|cluster`
- `parameters.filePrefix`

### 8.3 备份验证手册

建议先写一组 smoke key：

```bash
redis-cli -h <redis-host> -p 6379 -a '<password>' SET dp:smoke:ts "$(date +%s)"
redis-cli -h <redis-host> -p 6379 -a '<password>' SET dp:smoke:env "backup-test"
```

然后执行备份：

```bash
kubectl apply -f addons/redis/manifests/samples/backupjob.yaml
kubectl get bj -n backup-system
kubectl get snap -n backup-system
```

Redis addon 导出物：

- standalone：`/workspace/output/<prefix>.rdb`
- cluster：`/workspace/output/masters/*.rdb`

### 8.4 恢复与验证手册

当前这个仓库随附的 Redis addon 样例里只包含备份样例，没有提供官方恢复样例。

所以当前最稳妥的落地方式是：

- 用 dataprotection 管理 Redis RDB 导出与留存
- Redis 恢复流程按你们现有 Redis 集群恢复方案执行
- dataprotection 侧主要验证“RDB 归档是否完整、是否可下载、是否可导入到 Redis 恢复流程”

推荐恢复验收口径：

1. 取回某次备份归档
2. 确认其中存在预期的 RDB 文件
3. 按你们 Redis 集群既有恢复流程装载 RDB
4. 用 `GET dp:smoke:ts`、`GET dp:smoke:env` 验证 key 回来

如果后续你们要把 Redis 恢复也完全纳入 operator，需要补一个带 `restoreTemplate` 的 Redis addon。

## 9. Milvus 备份与恢复手册

### 9.1 适用前提

兼容路径：

```bash
./dataprotection-addon-milvus-amd64.run install -y
```

当前 Milvus addon 仍标记 beta，原因是：

- 它依赖 `milvus-backup` CLI
- 恢复效果需要你们实际 Milvus 版本与部署方式一起验证

### 9.2 注册数据源

```bash
kubectl apply -f addons/milvus/manifests/samples/backupsource.yaml
```

关键参数：

- `endpoint.host`
- `endpoint.port`
- `endpoint.username`
- `endpoint.passwordFrom`
- `parameters.backupNamePrefix`
- `parameters.collections`
- `parameters.database`

### 9.3 备份验证手册

建议先准备一份明确的 smoke 数据：

1. 新建一个测试 collection，例如 `dp_smoke_collection`
2. 插入固定数量向量，例如 10 条
3. 执行备份

执行：

```bash
kubectl apply -f addons/milvus/manifests/samples/backupjob.yaml
kubectl get bj -n backup-system
kubectl get snap -n backup-system
```

预期：

- 生成成功 `Snapshot`
- 后端出现 Milvus backup 归档

### 9.4 恢复与验证手册

执行恢复：

```bash
kubectl apply -f addons/milvus/manifests/samples/restorejob.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

恢复验证建议：

1. 备份后删除 `dp_smoke_collection`
2. 执行恢复
3. 用你们现有 Milvus SDK、业务探针或运维脚本确认：
   - collection 名称恢复
   - 行数恢复
   - 向量查询可正常返回

推荐验收口径：

- collection 存在
- row count 与备份前一致
- 至少一条已知向量查询返回命中

## 10. MinIO 备份与恢复手册

### 10.1 适用前提

兼容路径：

```bash
./dataprotection-addon-minio-amd64.run install -y
```

### 10.2 注册数据源

```bash
kubectl apply -f addons/minio/manifests/samples/backupsource.yaml
```

关键参数：

- `endpoint.scheme`
- `endpoint.host`
- `endpoint.port`
- `endpoint.usernameFrom`
- `endpoint.passwordFrom`
- `parameters.bucket`
- `parameters.prefix`

### 10.3 备份验证手册

建议先往源 bucket/prefix 写一组 smoke 对象：

- `smoke/a.txt`
- `smoke/b.json`
- `smoke/subdir/c.bin`

然后执行：

```bash
kubectl apply -f addons/minio/manifests/samples/backupjob.yaml
kubectl get bj -n backup-system
kubectl get snap -n backup-system
```

MinIO addon 的逻辑是：

- 从源 bucket/prefix `mc mirror` 到 `/workspace/output/data`
- operator 再把它打包并上传到备份后端

### 10.4 恢复与验证手册

执行恢复：

```bash
kubectl apply -f addons/minio/manifests/samples/restorejob.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

验证口径：

1. 备份后先删除或篡改 smoke 对象
2. 执行恢复
3. 用 `mc ls` / `mc cat` / `mc stat` 验证对象回来

建议检查：

- 对象数量一致
- 对象路径一致
- 文件大小一致
- 关键对象内容一致

## 11. RabbitMQ 备份与恢复手册

### 11.1 当前状态

这个仓库当前没有官方随附的 RabbitMQ addon 包，所以这里给的是“接入建议与验收手册”，不是“本 tag 已内置可直接安装的 RabbitMQ addon”。

### 11.2 推荐 addon 契约

如果你们后续要把 RabbitMQ 接入 dataprotection，建议把 addon 目标限定为：

- 导出 `definitions.json`
- 导出必要的 vhost / exchange / queue / policy / user / permission 元数据
- 明确声明是否支持“消息体级恢复”

强烈建议不要在没有额外设计前，就对“队列中的所有消息可完全恢复”做过度承诺。

### 11.3 推荐备份验证口径

如果你们已经有 RabbitMQ addon，建议至少验证：

1. 创建一个 smoke vhost
2. 创建一个 smoke queue / exchange / binding
3. 创建一条策略或参数
4. 执行备份
5. 检查导出包中是否包含预期 definitions

### 11.4 推荐恢复验证口径

1. 删除 smoke vhost / queue / policy
2. 执行恢复
3. 验证：
   - vhost 回来
   - queue 回来
   - binding 回来
   - policy 回来
4. 再做一轮 publish / consume smoke 验证

只有当你们的 RabbitMQ addon 明确支持消息体导出恢复时，才把“历史消息恢复”列入验收标准。

## 12. 通知与审计

如果你们接了 `NotificationEndpoint`，建议在下面三个对象上都配置：

- `BackupPolicy.notificationRefs`
- `BackupJob.notificationRefs`
- `RestoreJob.notificationRefs`

这样可以同时拿到：

- 周期备份结果
- 手动备份结果
- 恢复任务结果

排障时关注：

- `status.notification.phase`
- `status.notification.attempts`
- `status.notification.message`

## 13. 运维验收建议

对每一种已接入中间件，建议至少完成下面 6 项验收：

1. 存储后端探测成功
2. 手动备份成功
3. 周期备份成功
4. `Snapshot` 正常登记
5. 至少一次恢复演练成功
6. 保留策略能同步清理后端旧文件

## 14. 最终建议

如果你的交付目标是“让后来的人或 AI 能长期维护”，最推荐的方式不是只交付一组 YAML，而是把使用方式固定成下面这条顺序：

1. 安装 core operator
2. 安装备份后端
3. 安装中间件
4. 中间件在安装时自动注册自己的 `BackupSource`
5. 平台侧统一维护 `BackupStorage / BackupPolicy / RetentionPolicy / RestoreJob`

这样平台职责和中间件职责是清楚分开的，后续演进也最稳。
