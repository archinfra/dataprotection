# Quickstart

如果你现在不是做最小样例，而是准备真正落地到 MySQL / Redis / Milvus / MinIO 等中间件，请优先配合阅读：

- `docs/EXECUTION-FLOW.zh-CN.md`
- `docs/OPERATIONS-RUNBOOK.zh-CN.md`
- `docs/USER-CASES.zh-CN.md`

这份 quickstart 按“更接近真实交付”的顺序来写。

## 推荐流程

1. 安装 `dataprotection` operator
2. 准备 `backup-system` 命名空间和运行时密钥
3. 准备备份专用 MinIO
4. 可选，再准备一个 NFS 作为第二落点
5. 注册 MySQL 的 `BackupAddon / BackupSource`
6. 创建周期备份策略
7. 需要时直接创建 `BackupExecution` 执行手动备份
8. 从 `Snapshot` 或导入包执行恢复

如果你们现场使用的命名空间叫 `back-system`，把下面文档中的 `backup-system` 统一替换掉即可。

## 1. 安装 operator

```bash
./data-protection-operator-amd64.run install -y
```

确认核心资源：

```bash
kubectl get crd | grep dataprotection
kubectl get deploy -n data-protection-system
kubectl get backupexec -A
```

## 2. 准备命名空间和密钥

```bash
kubectl apply -f config/samples/quickstart/00-namespace-secrets.yaml
```

这一步会创建：

- `Namespace/backup-system`
- `Secret/mysql-runtime-auth`
- `Secret/minio-credentials`

## 3. 准备备份存储

最小可用方案是只准备一套 MinIO：

```bash
kubectl apply -f config/samples/quickstart/03-backupstorage-minio.yaml
kubectl apply -f config/samples/quickstart/05-retentionpolicy.yaml
kubectl apply -f config/samples/quickstart/06-notificationendpoint.yaml
```

如果需要第二落点，再补一套 NFS：

```bash
kubectl apply -f config/samples/quickstart/04-backupstorage-nfs.yaml
```

检查存储状态：

```bash
kubectl get bst -n backup-system
kubectl describe bst minio-primary -n backup-system
kubectl describe bst nfs-primary -n backup-system
```

重点关注：

- `status.phase`
- `status.lastProbeResult`
- `status.lastProbeMessage`

## 4. 注册 MySQL 备份接入

```bash
kubectl apply -f config/samples/quickstart/01-backupaddon-mysql.yaml
kubectl apply -f config/samples/quickstart/02-backupsource-mysql.yaml
```

检查：

```bash
kubectl get ba
kubectl get bsrc -n backup-system
```

## 5. 创建周期备份策略

### 方案 A：单落点定时备份

```bash
kubectl apply -f config/samples/quickstart/07-backuppolicy-minio-every-3m.yaml
```

这个策略每 3 分钟向 `minio-primary` 写一份备份。

当前第一阶段的定时链路是：

```text
BackupPolicy -> CronJob -> Kubernetes Job -> BackupExecution(adopt) -> Snapshot
```

因此实际执行历史统一从 `BackupExecution` 查看：

```bash
kubectl get backupexec -n backup-system
```

### 方案 B：双落点定时备份

```bash
kubectl apply -f config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml
```

当前实现会针对 `minio-primary` 和 `nfs-primary` 分别生成独立 `CronJob` 和独立 `BackupExecution`。这仍属于现有兼容行为，并不代表“一次导出、多目标分发”的真正 fan-out。

检查：

```bash
kubectl get bp -n backup-system
kubectl get cronjob -n backup-system
kubectl get backupexec -n backup-system
kubectl describe bp mysql-smoke-fanout -n backup-system
```

## 6. 执行一次手动备份

新的手动备份直接创建 `BackupExecution`：

```bash
kubectl apply -f config/samples/quickstart/08-backupexecution-manual-nfs.yaml
```

检查：

```bash
kubectl get backupexec -n backup-system
kubectl get job,pod -n backup-system
kubectl describe backupexec mysql-smoke-manual-nfs -n backup-system
```

`BackupJob` 只作为旧配置的兼容入口保留。旧 `BackupJob` 会被 controller 转换成同名 `BackupExecution`，新配置不要再以 `BackupJob` 作为主入口。

## 7. 观察执行结果

常用总览命令：

```bash
kubectl get ba,bsrc,bst,bp,backupexec,rj,snap,rp,ne -n backup-system
kubectl get cronjob,job,pod -n backup-system
```

成功后重点关注：

- `BackupExecution.status.phase`
- `BackupExecution.status.nativeJobName`
- `BackupExecution.status.snapshotRef`
- `Snapshot.status.artifactReady`
- `Snapshot.status.latest`
- `Snapshot.spec.backendPath`

## 8. 按平台快照恢复

先找一个成功快照：

```bash
kubectl get snap -n backup-system
```

修改 `config/samples/quickstart/09-restorejob-from-snapshot.yaml` 里的 `snapshotRef.name`，然后执行：

```bash
kubectl apply -f config/samples/quickstart/09-restorejob-from-snapshot.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

## 9. 按导入包恢复

如果是 A 集群导出，B 集群导入，不一定会先有一个平台内 `Snapshot` CR。此时可以把导出包先放到 MinIO 或 NFS，再直接恢复：

```bash
kubectl apply -f config/samples/quickstart/10-restorejob-from-import.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

`importSource` 规则：

- `storageRef.name` 指向已有 `BackupStorage`
- `path` 是相对该存储根目录的路径
- `format=auto` 时会自动判断
- `.tgz/.tar.gz/.tar` 按归档包解压到 `/workspace/input`
- 其它路径按文件系统内容处理
- 如果是目录，会把目录内容拷贝到 `/workspace/input`
- 如果是单文件，会拷贝到 `/workspace/input/<文件名>`

## 10. 如何验证保留策略真的生效

检查 `RetentionPolicy`：

```bash
kubectl get rp -n backup-system
kubectl describe rp keep-last-3 -n backup-system
```

当前实现中，同一条 series 超过 `keepLast` 后会同时收敛 Snapshot 和后端文件。retention 的单一职责收敛属于后续重构，不在本次执行模型统一范围内。

## 11. 排障建议

### 看领域执行状态

优先看 `BackupExecution`：

```bash
kubectl get backupexec -n backup-system
kubectl describe backupexec <name> -n backup-system
kubectl get backupexec <name> -n backup-system -o yaml
```

### 看底层 Job / Pod

`BackupExecution.status.nativeJobName` 会告诉你对应的 Kubernetes Job：

```bash
kubectl get job,pod -n backup-system
kubectl logs -n backup-system job/<job-name> -c addon
kubectl logs -n backup-system job/<job-name> -c storage
```

### 看存储探测

```bash
kubectl describe bst <storage-name> -n backup-system
```

### 看通知结果

```bash
kubectl get backupexec,rj -n backup-system -o yaml | grep -A8 notification:
```
