# Data Protection Operator 手工测试方案

## 1. 文档目标

这份文档不是“理想规划清单”，而是为了验证项目当前**真实已实现能力**。

目标有 3 个：

- 帮你判断现在这个项目到底已经能做什么
- 帮你按业务链路理解每个 CRD 的职责
- 帮你手工验收“能不能跑、跑完是什么结果、失败时怎么看”

## 2. 测试原则

本轮测试建议坚持 4 条原则：

1. 先验安装和控制面，再验数据面。
2. 先验正向用例，再验破坏性恢复和失败场景。
3. `Ready` 只能说明规格校验通过，不能代替真实连通性验证。
4. `wipe-all-user-databases` 有破坏性，只能在隔离测试库执行。

## 3. 当前建议的验收结论标准

如果下面这些都通过，可以认为项目当前达到了“**MySQL alpha 可用**”：

- `.run` 安装链路正常
- operator Deployment 正常运行
- 5 个 CRD 都可创建、可回填状态
- `BackupPolicy` 可按 repository 正确生成多个 `CronJob`
- `BackupRun` 可在 NFS 和 S3/MinIO 上都生成真实备份产物
- `RestoreRequest` 可完成至少一轮恢复并验证数据
- 幂等场景不会重复创建一堆脏 `Job/CronJob`
- 常见错误场景能在状态或日志里定位

## 4. 环境准备

### 4.1 必备组件

- 一个可用 Kubernetes 集群
- 一个本地镜像仓库，默认按 `sealos.hub:5000/kube4`
- 一个 MySQL 测试实例
- 一个 NFS 共享目录
- 一个 S3/MinIO 仓库
- `kubectl`
- 离线安装包，例如 `data-protection-operator-amd64.run`

### 4.2 推荐测试命名

为避免误删，建议统一使用下面这些名字：

- namespace: `backup-system`
- MySQL source: `mysql-smoke`
- NFS repository: `nfs-lab`
- S3 repository: `minio-lab`
- policy: `mysql-smoke-daily`
- backup run: `mysql-smoke-manual`
- restore request: `mysql-smoke-restore`

### 4.3 推荐测试数据

建议在 MySQL 中至少准备两个库：

- `orders`
- `inventory`

并且每个库至少有一张表和几条数据。  
这样后续可以分别验证：

- 全量用户库备份
- 指定数据库备份
- 指定表备份
- 恢复后数据是否真的回来了

## 5. 测试前置数据准备

### 5.1 创建测试命名空间

```bash
kubectl create namespace backup-system
```

### 5.2 创建 MySQL 访问 Secret

```bash
kubectl -n backup-system create secret generic mysql-runtime-auth \
  --from-literal=password='<MYSQL_PASSWORD>'
```

如果你希望连用户名也放进 secret，可以后续改成 `usernameFrom` 模式；当前 smoke test 先用明文用户名更直观。

### 5.3 创建 MinIO/S3 访问 Secret

```bash
kubectl -n backup-system create secret generic minio-credentials \
  --from-literal=access-key='<S3_ACCESS_KEY>' \
  --from-literal=secret-key='<S3_SECRET_KEY>'
```

### 5.4 初始化 MySQL 测试数据

下面用的是逻辑示例，你可以通过现有 MySQL Pod、mysql client Pod 或外部客户端执行：

```sql
CREATE DATABASE IF NOT EXISTS orders;
CREATE TABLE IF NOT EXISTS orders.t_order (
  id INT PRIMARY KEY,
  note VARCHAR(64)
);
REPLACE INTO orders.t_order VALUES (1, 'init-order');

CREATE DATABASE IF NOT EXISTS inventory;
CREATE TABLE IF NOT EXISTS inventory.t_stock (
  id INT PRIMARY KEY,
  sku VARCHAR(64),
  qty INT
);
REPLACE INTO inventory.t_stock VALUES (1, 'sku-001', 10);
```

## 6. 推荐样例清单

### 6.1 `BackupSource`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupSource
metadata:
  name: mysql-smoke
  namespace: backup-system
spec:
  driver: mysql
  endpoint:
    host: <MYSQL_HOST>
    port: 3306
    username: root
    passwordFrom:
      name: mysql-runtime-auth
      key: password
```

### 6.2 NFS `BackupRepository`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRepository
metadata:
  name: nfs-lab
  namespace: backup-system
spec:
  type: nfs
  nfs:
    server: <NFS_SERVER>
    path: <NFS_EXPORT_PATH>
```

### 6.3 S3/MinIO `BackupRepository`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRepository
metadata:
  name: minio-lab
  namespace: backup-system
spec:
  type: s3
  s3:
    endpoint: <S3_ENDPOINT>
    bucket: <S3_BUCKET>
    prefix: smoke
    accessKeyFrom:
      name: minio-credentials
      key: access-key
    secretKeyFrom:
      name: minio-credentials
      key: secret-key
```

### 6.4 多中心 `BackupPolicy`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupPolicy
metadata:
  name: mysql-smoke-daily
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-smoke
  repositoryRefs:
    - name: nfs-lab
    - name: minio-lab
  schedule:
    cron: "0 2 * * *"
    concurrencyPolicy: Forbid
  retention:
    keepLast: 3
  verification:
    enabled: true
    mode: Metadata
```

### 6.5 手工 `BackupRun`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: BackupRun
metadata:
  name: mysql-smoke-manual
  namespace: backup-system
spec:
  policyRef:
    name: mysql-smoke-daily
  sourceRef:
    name: mysql-smoke
  reason: manual smoke backup
```

### 6.6 `RestoreRequest`

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: RestoreRequest
metadata:
  name: mysql-smoke-restore
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-smoke
  repositoryRef:
    name: nfs-lab
  snapshot: latest
  target:
    mode: InPlace
    driverConfig:
      mysql:
        restoreMode: merge
```

## 7. 测试矩阵

| 编号 | 测试主题 | 目标 |
| --- | --- | --- |
| T01 | 安装包帮助信息 | 确认 `.run` 可执行且帮助参数正常 |
| T02 | 离线安装 | 确认 CRD、RBAC、Deployment 可正常安装 |
| T03 | 基础状态回填 | 确认 `BackupSource` / `BackupRepository` 会进入 `Ready` |
| T04 | 多中心定时策略 | 确认一个 policy 会生成多个 `CronJob` |
| T05 | 定时模型理解 | 确认 `BackupPolicy` 不会自动生成 `BackupRun` |
| T06 | 手工备份 | 确认 `BackupRun` 会生成多个 `Job` 并成功结束 |
| T07 | 备份产物检查 | 确认 NFS / S3 上都有真实快照与校验文件 |
| T08 | 指定数据库备份 | 确认只备份某个数据库 |
| T09 | 指定表备份 | 确认只备份某些表 |
| T10 | `merge` 恢复 | 确认恢复链路可用 |
| T11 | `wipe-all-user-databases` 恢复 | 确认破坏性恢复模式可用 |
| T12 | 幂等性 | 确认重复 apply / 重复 reconcile 不产生脏资源 |
| T13 | 失败场景 | 确认错误会在 `status` 或日志中体现 |
| T14 | 卸载行为 | 确认默认保留 CRD，可选删除 CRD |

## 8. 详细测试步骤

### T01 安装包帮助信息

### 目标

确认离线包入口是正常的，尤其是之前出过问题的 `-h` / `--help`。

### 步骤

```bash
chmod +x ./data-protection-operator-amd64.run
./data-protection-operator-amd64.run -h
./data-protection-operator-amd64.run --help
./data-protection-operator-amd64.run help
```

### 预期结果

- 三条命令都能输出帮助信息
- 不应再出现 `Unsupported action: -h`

### T02 离线安装

### 目标

确认 `.run` 能把 operator 正确装进集群。

### 步骤

```bash
./data-protection-operator-amd64.run install -y
kubectl get ns data-protection-system
kubectl get deploy -n data-protection-system
kubectl get crd | grep dataprotection.archinfra.io
```

### 预期结果

- `data-protection-system` namespace 存在
- `data-protection-operator-controller-manager` 存在
- 5 个 CRD 都存在

### 补充检查

```bash
kubectl rollout status deployment/data-protection-operator-controller-manager \
  -n data-protection-system --timeout=300s
kubectl get pods -n data-protection-system -o wide
kubectl logs -n data-protection-system deploy/data-protection-operator-controller-manager
```

### 预期结果

- Deployment successfully rolled out
- Pod 进入 `Running`
- 日志中没有持续 crash / panic

### T03 基础状态回填

### 目标

确认 `BackupSource` / `BackupRepository` 的 status 能正常回填。

### 步骤

1. apply `BackupSource`
2. apply `nfs-lab`
3. apply `minio-lab`
4. 查看资源状态

```bash
kubectl apply -f backup-source.yaml
kubectl apply -f backup-repository-nfs.yaml
kubectl apply -f backup-repository-s3.yaml

kubectl get backupsource -n backup-system
kubectl get backuprepository -n backup-system
kubectl get backupsource mysql-smoke -n backup-system -o yaml
kubectl get backuprepository nfs-lab -n backup-system -o yaml
kubectl get backuprepository minio-lab -n backup-system -o yaml
```

### 预期结果

- 3 个对象都能创建成功
- `status.phase` 进入 `Ready`
- `status.observedGeneration` 已回填
- `status.conditions` 有 `Ready=True`

### 重要理解

这一步通过，只能说明：

- spec 合法

这一步**不能**说明：

- MySQL 一定能连上
- NFS 一定能挂上
- MinIO 一定能访问

### T04 多中心定时策略

### 目标

确认一个 `BackupPolicy` 关联两个 repository 时，controller 会创建两个独立 `CronJob`。

### 步骤

```bash
kubectl apply -f backup-policy.yaml
kubectl get backuppolicy mysql-smoke-daily -n backup-system -o yaml
kubectl get cronjob -n backup-system
```

### 预期结果

- `BackupPolicy.status.phase=Ready`
- `status.cronJobNames` 中有 2 个名字
- 实际 `CronJob` 数量也是 2
- 这 2 个 `CronJob` 都应该带有该 `BackupPolicy` 的 owner reference

### 补充检查

```bash
kubectl describe backuppolicy mysql-smoke-daily -n backup-system
kubectl describe cronjob -n backup-system
```

### T05 定时模型理解

### 目标

确认当前定时流是 `BackupPolicy -> CronJob`，而不是 `BackupPolicy -> BackupRun`。

### 步骤

```bash
kubectl get backuprun -n backup-system
kubectl get cronjob -n backup-system
```

### 预期结果

- 可以看到 `CronJob`
- 此时不会因为 apply `BackupPolicy` 自动出现 `BackupRun`

### 结论

如果这里没有 `BackupRun` 历史对象，不是 bug，而是当前实现就是这样设计的。

### T06 手工备份

### 目标

确认 `BackupRun` 能对两个 repository 生成两个一次性 `Job`，并最终成功。

### 步骤

```bash
kubectl apply -f backup-run.yaml
kubectl get backuprun mysql-smoke-manual -n backup-system -o yaml
kubectl get job -n backup-system
kubectl logs -n backup-system job/$(kubectl get job -n backup-system -o name | grep mysql-smoke-manual | head -n 1)
```

### 预期结果

- `BackupRun.status.jobNames` 中有 2 个 Job 名字
- `BackupRun.status.repositories` 中有 2 条 repository 运行状态
- 最终 `BackupRun.status.phase=Succeeded`
- 两个 `Job` 都成功完成

### T07 备份产物检查

### 目标

确认这不是“只有 Kubernetes 资源成功”，而是真的把备份产物写出来了。

### NFS 检查路径

理论路径：

- `<NFS_EXPORT_PATH>/backups/mysql/backup-system/mysql-smoke/`

建议检查：

- `latest.txt`
- `snapshots/*.sql.gz`
- `snapshots/*.sql.gz.sha256`
- `snapshots/*.meta`

### S3/MinIO 检查路径

理论路径：

- `<S3_BUCKET>/smoke/backups/mysql/backup-system/mysql-smoke/`

建议检查：

- `latest.txt`
- `snapshots/*.sql.gz`
- `snapshots/*.sql.gz.sha256`
- `snapshots/*.meta`

### 预期结果

- NFS 和 S3/MinIO 两边都能看到快照
- `latest.txt` 指向最新快照
- `.sha256` 存在
- `.meta` 中可看到范围信息、创建时间等元数据

### T08 指定数据库备份

### 目标

确认 `driverConfig.mysql.databases` 生效。

### 方法

把 `BackupRun` 或 `BackupPolicy` 改成下面这种形式：

```yaml
spec:
  policyRef:
    name: mysql-smoke-daily
  sourceRef:
    name: mysql-smoke
  driverConfig:
    mysql:
      databases:
        - orders
```

### 步骤

1. 新建一个只备份 `orders` 的 `BackupRun`
2. 等待备份完成
3. 解压或导出 `.sql.gz` 内容检查

### 预期结果

- dump 中包含 `orders`
- dump 中不应包含 `inventory`

### T09 指定表备份

### 目标

确认 `driverConfig.mysql.tables` 生效。

### 方法

```yaml
spec:
  policyRef:
    name: mysql-smoke-daily
  sourceRef:
    name: mysql-smoke
  driverConfig:
    mysql:
      tables:
        - orders.t_order
```

### 预期结果

- dump 中包含 `orders.t_order`
- 不应包含其他未选中的表

### 补充说明

表选择器必须写成 `database.table`，否则应被判为非法配置。

### T10 `merge` 恢复

### 目标

确认当前最常规的恢复链路可用。

### 步骤

1. 确认已经至少有一份备份成功
2. 手工修改 MySQL 数据，例如把 `orders.t_order.id=1` 的内容改掉
3. apply 一个 `merge` 模式的 `RestoreRequest`

```bash
kubectl apply -f restore-request.yaml
kubectl get restorerequest mysql-smoke-restore -n backup-system -o yaml
kubectl get job -n backup-system
```

### 预期结果

- `RestoreRequest.status.phase=Succeeded`
- 对应 restore Job 成功
- 目标数据回到备份时状态

### 重要理解

`merge` 的准确含义是“不先全量清空所有用户库”。  
它不等于“完全无覆盖行为”。

### T11 `wipe-all-user-databases` 恢复

### 目标

确认破坏性恢复模式真的会生效。

### 步骤

1. 在测试 MySQL 中新建一个临时库，例如 `temp_after_backup`
2. 创建下面这种恢复请求

```yaml
apiVersion: dataprotection.archinfra.io/v1alpha1
kind: RestoreRequest
metadata:
  name: mysql-smoke-restore-wipe
  namespace: backup-system
spec:
  sourceRef:
    name: mysql-smoke
  repositoryRef:
    name: nfs-lab
  snapshot: latest
  target:
    mode: InPlace
    driverConfig:
      mysql:
        restoreMode: wipe-all-user-databases
```

### 预期结果

- restore Job 成功
- `temp_after_backup` 这类备份后新增的用户库被清空
- 原备份中的业务库被恢复回来

### 风险提示

这个用例一定要在隔离测试库上跑，不要在共享环境跑。

### T12 幂等性

### 目标

确认 controller 具备基本工程化重入能力。

### 步骤 A：重复 apply

```bash
kubectl apply -f backup-policy.yaml
kubectl apply -f backup-policy.yaml
kubectl get cronjob -n backup-system
```

### 预期结果

- `CronJob` 数量不增加
- 仍然只有和 repository 数一致的数量

### 步骤 B：重启 controller

```bash
kubectl rollout restart deployment/data-protection-operator-controller-manager \
  -n data-protection-system
kubectl rollout status deployment/data-protection-operator-controller-manager \
  -n data-protection-system --timeout=300s
kubectl get cronjob -n backup-system
kubectl get job -n backup-system
```

### 预期结果

- controller 重启后不会重复制造一批新的同类资源
- 原有资源保持稳定

### 步骤 C：策略收缩

把 `BackupPolicy.spec.repositoryRefs` 从两个改成一个，再 apply。

### 预期结果

- 被移除 repository 对应的 `CronJob` 会被清理
- 保留的 repository 对应 `CronJob` 继续存在

### T13 失败场景

### 目标

确认当前故障能暴露在 `status` 或日志里，而不是静默失败。

### 用例 A：错误的 S3 Secret

1. 故意把 `minio-credentials` 改成错误值
2. 重新创建一个 `BackupRun`

### 预期结果

- `BackupRepository` 仍可能显示 `Ready`
- 但 `BackupRun` 中对应 S3 repository 的 `Job` 会失败
- 失败信息可以从 `Job` 日志或 `BackupRun.status.repositories[].message` 里定位

### 用例 B：非法表选择器

```yaml
driverConfig:
  mysql:
    tables:
      - bad-selector
```

### 预期结果

- 对应对象会进入 `Failed`
- `conditions` 中能看到非法配置原因

### 用例 C：引用不存在的 repository

把 `BackupRun.spec.repositoryRefs` 指向一个不存在的名字。

### 预期结果

- `BackupRun` 不会成功
- 状态会体现依赖未就绪或引用无效

### T14 卸载行为

### 目标

确认离线包的卸载策略符合预期。

### 步骤 A：默认卸载

```bash
./data-protection-operator-amd64.run uninstall -y
kubectl get deploy -n data-protection-system
kubectl get crd | grep dataprotection.archinfra.io
```

### 预期结果

- controller Deployment 被删除
- CRD 默认仍保留

### 步骤 B：删除 CRD

这是可选的破坏性步骤：

```bash
./data-protection-operator-amd64.run uninstall --delete-crds -y
kubectl get crd | grep dataprotection.archinfra.io
```

### 预期结果

- 5 个 CRD 被删除

## 9. 验收时重点关注的状态字段

建议重点观察这些字段：

- `BackupSource.status.phase`
- `BackupRepository.status.phase`
- `BackupPolicy.status.phase`
- `BackupPolicy.status.cronJobNames`
- `BackupRun.status.phase`
- `BackupRun.status.jobNames`
- `BackupRun.status.repositories`
- `RestoreRequest.status.phase`
- `RestoreRequest.status.jobName`

## 10. 验收时重点关注的日志

### controller 日志

```bash
kubectl logs -n data-protection-system deploy/data-protection-operator-controller-manager
```

用途：

- 看 reconcile 是否报错
- 看依赖解析、render、创建子资源是否失败

### backup / restore Job 日志

```bash
kubectl logs -n backup-system job/<job-name>
```

用途：

- 看 MySQL 连通性
- 看 NFS 挂载 / S3 同步
- 看 mysqldump / restore 具体报错

## 11. 这套测试真正能证明什么

如果你完整跑完这份清单，并且核心正向用例都通过，说明当前项目已经证明了下面这些事情：

- 它已经是一个真实可运行的 operator，而不只是 CRD 壳子
- 它对 MySQL 的 NFS / S3 逻辑备份恢复已经能闭环
- 它具备了继续扩展 Redis、MongoDB、MinIO 等 driver 的控制面基础

如果失败场景也能按预期暴露错误，说明它在工程化上已经具备继续迭代的基础。

## 12. 当前不建议用这份测试清单去证明的事情

跑完这份清单，也**不能**证明下面这些结论：

- Redis 已可用
- MongoDB 已可用
- MinIO 对象数据保护已可用
- RabbitMQ 已可用
- Milvus 已可用
- Admission webhook 已完整
- 通用校验系统已完整
- 可以直接无评审用于生产关键库

这些目前都还不属于“已经真实交付完成”的范畴。
