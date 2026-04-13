# quickstart 使用说明

这组 quickstart 样例现在有两种使用方式。

## 1. 标准使用方式

如果你已经执行过：

```bash
./data-protection-operator-amd64.run install -y
./dataprotection-addon-mysql-amd64.run install -y
```

那么请直接从这些资源开始：

```bash
kubectl apply -f 00-namespace-secrets.yaml
kubectl apply -f 02-backupsource-mysql.yaml
kubectl apply -f 03-backupstorage-minio.yaml
kubectl apply -f 04-backupstorage-nfs.yaml
kubectl apply -f 05-retentionpolicy.yaml
kubectl apply -f 06-notificationendpoint.yaml
kubectl apply -f 07-backuppolicy-minio-every-3m.yaml
kubectl apply -f 08-backupjob-manual-nfs.yaml
```

不要再执行：

```bash
kubectl apply -f 01-backupaddon-mysql.yaml
```

原因是 mysql addon 安装包已经完成了：

- `BackupAddon/mysql-dump` 安装
- addon runner image 导入和目标仓库渲染

## 2. 开发调试方式

如果你没有使用 mysql addon `.run` 包，只是想手工调试 addon 模板，可以：

```bash
kubectl apply -f 01-backupaddon-mysql.yaml
```

但这只是开发样例，不是标准交付路径，因为它写死了公开镜像：

- `mysql:8.0.45`

## 3. 文件职责

- `00-namespace-secrets.yaml`
  命名空间和 Secret
- `01-backupaddon-mysql.yaml`
  仅开发调试用的手工 addon 样例
- `02-backupsource-mysql.yaml`
  业务数据源
- `03-backupstorage-minio.yaml`
  MinIO 后端
- `04-backupstorage-nfs.yaml`
  NFS 后端
- `05-retentionpolicy.yaml`
  保留策略
- `06-notificationendpoint.yaml`
  webhook 通知目标
- `07-backuppolicy-minio-every-3m.yaml`
  定时备份
- `08-backupjob-manual-nfs.yaml`
  手工一次性备份
- `09-restorejob-from-snapshot.yaml`
  从快照恢复
