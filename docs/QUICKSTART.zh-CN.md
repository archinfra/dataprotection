# Quickstart

这份 quickstart 按“更接近真实交付”的顺序来写，不再把重点放在“单独演示一个 MySQL addon 包”。

## 推荐流程

1. 安装 `dataprotection` operator
2. 准备 `backup-system` 命名空间和运行时密钥
3. 准备备份专用 MinIO
4. 可选，再准备一个 NFS 作为第二落点
5. 注册 MySQL 的 `BackupAddon / BackupSource`
6. 创建周期备份策略
7. 需要时执行手动备份
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

### 方案 B：双落点 fan-out 定时备份

```bash
kubectl apply -f config/samples/quickstart/11-backuppolicy-fanout-minio-nfs.yaml
```

这个策略会同时向：

- `minio-primary`
- `nfs-primary`

各写一份备份。控制器会生成多个 `CronJob`，并把名称记录到：

- `BackupPolicy.status.cronJobNames`

检查：

```bash
kubectl get bp -n backup-system
kubectl get cronjob -n backup-system
kubectl describe bp mysql-smoke-fanout -n backup-system
```

## 6. 执行一次手动备份

如果你只想立刻打一份到某个特定存储，用 `BackupJob`：

```bash
kubectl apply -f config/samples/quickstart/08-backupjob-manual-nfs.yaml
```

检查：

```bash
kubectl get bj -n backup-system
kubectl get job,pod -n backup-system
kubectl describe bj mysql-smoke-manual-nfs -n backup-system
```

## 7. 观察执行结果

常用总览命令：

```bash
kubectl get ba,bsrc,bst,bp,bj,rj,snap,rp,ne -n backup-system
kubectl get cronjob,job,pod -n backup-system
```

成功后重点关注：

- `BackupJob.status.snapshotRef`
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

当同一条 series 超过 `keepLast` 后，控制器会：

- 删除多余的 `Snapshot`
- 删除 NFS 上旧的 `.tgz / .sha256 / .metadata.json`
- 删除 MinIO 上旧的 `.tgz / .sha256 / .metadata.json`

这一步建议配合后端实际目录一起验证。

## 11. 排障建议

### 看资源状态

```bash
kubectl get bj,rj,snap,bst,bp -n backup-system
kubectl describe bj <name> -n backup-system
kubectl describe rj <name> -n backup-system
```

### 看底层 Job / Pod

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
kubectl get bj,rj -n backup-system -o yaml | grep -A8 notification:
```
