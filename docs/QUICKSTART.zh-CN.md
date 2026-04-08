# Data Protection Operator Quickstart

这份文档给你一套从零开始的联调路径，目标是先把基础控制面跑通，再按 source addon 分别验证 MySQL、Redis、MinIO。

## 1. 安装 controller

```bash
./data-protection-operator-amd64.run install --image-pull-policy Always -y
```

确认：

```bash
kubectl get pods -n data-protection-system
kubectl get deploy -n data-protection-system data-protection-operator-controller-manager
```

## 2. 先创建基础资源

```bash
kubectl apply -f config/samples/quickstart/00-namespace-secrets.yaml
kubectl apply -f config/samples/quickstart/02-backupstorage-minio.yaml
kubectl apply -f config/samples/quickstart/03-backupstorage-nfs.yaml
kubectl apply -f config/samples/quickstart/04-retentionpolicy.yaml
```

你至少需要修改这些占位值：

- `00-namespace-secrets.yaml`
  - `<MYSQL_ROOT_PASSWORD>`
  - `<REDIS_PASSWORD>`
  - `<S3_ACCESS_KEY>`
  - `<S3_SECRET_KEY>`
  - `<MINIO_SOURCE_ACCESS_KEY>`
  - `<MINIO_SOURCE_SECRET_KEY>`
- `02-backupstorage-minio.yaml`
  - `endpoint`
  - `bucket`
  - `prefix`
- `03-backupstorage-nfs.yaml`
  - `server`
  - `path`

## 3. MySQL 联调

创建 source 和策略：

```bash
kubectl apply -f config/samples/quickstart/01-backupsource-mysql.yaml
kubectl apply -f config/samples/quickstart/05-backuppolicy-every-3m.yaml
kubectl apply -f config/samples/quickstart/06-backuprun-manual.yaml
```

恢复：

```bash
kubectl apply -f config/samples/quickstart/07-restorerequest.yaml
```

观察：

```bash
kubectl get bp,br,snap,rr -n backup-system
kubectl get job,pod -n backup-system
kubectl logs -n backup-system <pod-name> -c mysql-backup
kubectl logs -n backup-system <pod-name> -c s3-upload
```

## 4. Redis 单点联调

创建 source、策略和一次手工备份：

```bash
kubectl apply -f config/samples/quickstart/11-backupsource-redis-standalone.yaml
kubectl apply -f config/samples/quickstart/14-backuppolicy-redis-standalone-every-5m.yaml
kubectl apply -f config/samples/quickstart/17-backuprun-redis-standalone-manual.yaml
```

重点观察：

```bash
kubectl get br,snap -n backup-system
kubectl get job,pod -n backup-system
kubectl logs -n backup-system <pod-name> -c redis-backup
kubectl logs -n backup-system <pod-name> -c s3-upload
```

当前 Redis addon 的预期产物是：

- 单点：一个 `tar.gz` 快照，里面包含 `standalone.rdb`
- 集群：一个 `tar.gz` 快照，里面包含 `nodes/*.rdb`

## 5. Redis Cluster 联调

创建 cluster source 和策略：

```bash
kubectl apply -f config/samples/quickstart/12-backupsource-redis-cluster.yaml
kubectl apply -f config/samples/quickstart/15-backuppolicy-redis-cluster-every-5m.yaml
```

验证点：

1. seed endpoint 是否能连通
2. 日志里是否打印每个 master 的 RDB 拉取
3. 快照包里是否有多个 `nodes/*.rdb`

如果你想先手工测一轮，可以复制 `17-backuprun-redis-standalone-manual.yaml`，把 `sourceRef.name` 改成 `redis-cluster`。

## 6. MinIO source 联调

创建 MinIO source 和策略：

```bash
kubectl apply -f config/samples/quickstart/13-backupsource-minio.yaml
kubectl apply -f config/samples/quickstart/16-backuppolicy-minio-every-10m.yaml
kubectl apply -f config/samples/quickstart/18-backuprun-minio-manual.yaml
```

默认样例把 MinIO source 备份到 `nfs-primary`。如果你想改成 S3，只要把策略里的 `storageRefs` 改成 `minio-primary`。

验证点：

1. 指定 bucket / prefix 是否落到了 snapshot 目录
2. `latest.txt` 是否更新
3. retention 是否会清理旧 snapshot 目录

恢复时，可以创建一个 `RestoreRequest`，指向 `snapshotRef`，让 addon 把对象重新 mirror 回源 MinIO。

## 7. 推荐观察命令

```bash
kubectl get bsrc,bst,bp,br,snap,rr,rp -n backup-system
kubectl get job,pod -n backup-system
kubectl describe job -n backup-system <job-name>
kubectl describe pod -n backup-system <pod-name>
kubectl logs -n backup-system <pod-name> -c s3-upload
kubectl logs -n backup-system <pod-name> -c s3-download
kubectl logs -n data-protection-system deploy/data-protection-operator-controller-manager
```

## 8. shortName

```bash
kubectl get bsrc -A
kubectl get bst -A
kubectl get bp -A
kubectl get br -A
kubectl get snap -A
kubectl get rr -A
kubectl get rp -A
```

## 9. 当前边界

- Redis restore 还没有做成内建 addon。
- MinIO `includeVersions=true` 还不支持。
- `Ready` 仍偏静态校验，联调时要同时看真实 Job 日志。
