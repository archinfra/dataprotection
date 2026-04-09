# Quickstart

## 1. 安装 operator

```bash
./data-protection-operator-amd64.run install -y
```

## 2. 应用最小样例

```bash
kubectl apply -f config/samples/quickstart/00-namespace-secrets.yaml
kubectl apply -f config/samples/quickstart/01-backupaddon-mysql.yaml
kubectl apply -f config/samples/quickstart/02-backupsource-mysql.yaml
kubectl apply -f config/samples/quickstart/03-backupstorage-minio.yaml
kubectl apply -f config/samples/quickstart/04-backupstorage-nfs.yaml
kubectl apply -f config/samples/quickstart/05-retentionpolicy.yaml
kubectl apply -f config/samples/quickstart/06-notificationendpoint.yaml
kubectl apply -f config/samples/quickstart/07-backuppolicy-minio-every-3m.yaml
kubectl apply -f config/samples/quickstart/08-backupjob-manual-nfs.yaml
```

## 3. 观察状态

```bash
kubectl get ba,bsrc,bst,bp,bj,rj,snap,rp,ne -n backup-system
kubectl get cronjob,job,pod -n backup-system
```

## 4. 从快照恢复

先找到一个 `Succeeded` 的 `Snapshot`：

```bash
kubectl get snap -n backup-system
```

然后修改 [09-restorejob-from-snapshot.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/09-restorejob-from-snapshot.yaml) 里的 `snapshotRef.name`，再执行：

```bash
kubectl apply -f config/samples/quickstart/09-restorejob-from-snapshot.yaml
kubectl get rj -n backup-system
kubectl get job,pod -n backup-system
```

## 5. 关键说明

- `BackupPolicy` 直接产出原生 `CronJob -> Job`
- 手工一次性备份使用 `BackupJob`
- 手工恢复使用 `RestoreJob`
- `Snapshot` 只表示成功且可恢复的资产
- 存储健康不做后台探测，而是在每次执行前 preflight
