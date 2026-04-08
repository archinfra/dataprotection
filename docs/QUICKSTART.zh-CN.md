# Data Protection Operator Quickstart

这份文档给你一套从零开始的最小上手流程，目标是先把 MySQL 备份链路跑通，再逐步切到 NFS、恢复和定时。

## 1. 安装 controller

推荐使用新的 installer，并显式指定 `Always`，避免 `latest` 被节点缓存：

```bash
./data-protection-operator-amd64.run install --image-pull-policy Always -y
```

安装完成后确认：

```bash
kubectl get pods -n data-protection-system
kubectl get deploy -n data-protection-system data-protection-operator-controller-manager
```

## 2. 一次性创建基础资源

按顺序 apply：

```bash
kubectl apply -f config/samples/quickstart/00-namespace-secrets.yaml
kubectl apply -f config/samples/quickstart/01-backupsource-mysql.yaml
kubectl apply -f config/samples/quickstart/02-backupstorage-minio.yaml
kubectl apply -f config/samples/quickstart/04-retentionpolicy.yaml
kubectl apply -f config/samples/quickstart/05-backuppolicy-every-3m.yaml
```

如果你要测 NFS，把第 3 步换成：

```bash
kubectl apply -f config/samples/quickstart/03-backupstorage-nfs.yaml
```

然后把 [`05-backuppolicy-every-3m.yaml`](/C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/05-backuppolicy-every-3m.yaml) 里的 `storageRefs` 改成 `nfs-primary`。

## 3. 需要替换的占位符

你至少需要改这些值：

- `00-namespace-secrets.yaml`
  - `<MYSQL_ROOT_PASSWORD>`
  - `<S3_ACCESS_KEY>`
  - `<S3_SECRET_KEY>`
- `01-backupsource-mysql.yaml`
  - `host`
  - 如果你的 MySQL 不在 `aict`，改成实际 DNS
- `02-backupstorage-minio.yaml`
  - `endpoint`
  - `bucket`
  - `prefix`
- `03-backupstorage-nfs.yaml`
  - `<NFS_SERVER>`
  - `<NFS_EXPORT_PATH>`

## 4. 推荐的 MySQL host 写法

如果目标 MySQL 是 StatefulSet，最稳的写法通常是：

```yaml
spec:
  endpoint:
    host: mysql-0.mysql.aict.svc.cluster.local
    port: 3306
```

不要优先使用 NodePort。

## 5. 触发一次手工备份

```bash
kubectl apply -f config/samples/quickstart/06-backuprun-manual.yaml
```

观察：

```bash
kubectl get br -n backup-system
kubectl get job -n backup-system
kubectl get pod -n backup-system
kubectl get snap -n backup-system
```

## 6. 等待定时备份

当前示例策略是每 3 分钟一次：

```yaml
schedule:
  cron: "*/3 * * * *"
```

你可以直接观察：

```bash
kubectl get bp -n backup-system
kubectl get br -n backup-system -w
kubectl get pod -n backup-system -w
```

## 7. 恢复

先等 `Snapshot` 出来，再执行：

```bash
kubectl apply -f config/samples/quickstart/07-restorerequest.yaml
kubectl get rr -n backup-system
kubectl get job -n backup-system
kubectl get pod -n backup-system
```

## 8. kubectl shortName

这版开始支持 shortName，重新安装或更新 CRD 后就可以直接用：

```bash
kubectl get bsrc -A
kubectl get bst -A
kubectl get bp -A
kubectl get br -A
kubectl get snap -A
kubectl get rr -A
kubectl get rp -A
```

常用组合：

```bash
kubectl get bsrc,bst,bp,br,snap,rr,rp -n backup-system
```

## 9. 排查时最常用的命令

看 controller：

```bash
kubectl logs -n data-protection-system deploy/data-protection-operator-controller-manager
```

看某次备份：

```bash
kubectl get br -n backup-system
kubectl get br <run-name> -n backup-system -o yaml
kubectl describe job -n backup-system <job-name>
kubectl describe pod -n backup-system <pod-name>
```

看 MySQL 备份容器：

```bash
kubectl logs -n backup-system <pod-name> -c mysql-backup
```

看 MinIO 上传容器：

```bash
kubectl logs -n backup-system <pod-name> -c s3-upload
```

看恢复：

```bash
kubectl logs -n backup-system <pod-name> -c mysql-restore
kubectl logs -n backup-system <pod-name> -c s3-download
```

## 10. 重要注意事项

- 升级 controller 后，已经创建过的 `BackupRun/Job` 不会自动变成新模板
- `BackupStorage` 通常不需要重建
- 如果你想验证新逻辑，创建一个新的 `BackupRun` 或等待下一次新的调度
- 如果镜像标签还是 `latest`，建议继续使用 `--image-pull-policy Always`
