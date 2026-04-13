# MySQL addon run package

这个包不是“再给你一份手工 YAML”，而是 **MySQL addon 的正式安装包**。

它安装的内容有两类：

1. `BackupAddon/mysql-dump`
   这是 cluster-scoped 的 addon 资源。
2. mysql addon runner image
   这个镜像会被导入、重打 tag、推送到你指定的目标仓库，然后写回 `BackupAddon` 的 `backupTemplate.image` / `restoreTemplate.image`。

所以标准路径应该是：

1. 先安装 core `dataprotection` operator
2. 再安装这个 mysql addon 包
3. 然后只 apply `BackupSource / BackupStorage / RetentionPolicy / BackupPolicy / BackupJob / RestoreJob`

而不是：

1. 安装 addon 包
2. 再手工 apply 一个写死 `mysql:8.0.45` 的 `BackupAddon` 样例

后者只是开发调试路径，不是标准使用方式。

## Build

```bash
cd addons/mysql
./build.sh --arch amd64
./build.sh --arch arm64
```

## Install

```bash
./dist/dataprotection-addon-mysql-amd64.run install -y
```

安装完成后建议确认：

```bash
kubectl get ba
kubectl get backupaddon mysql-dump -o yaml
```

你应该能看到：

- `BackupAddon/mysql-dump` 已存在
- image 已经是目标仓库地址
- 不再是样例里的公开 `mysql:8.0.45`

## Export samples

```bash
./dist/dataprotection-addon-mysql-amd64.run samples --output-dir ./samples/mysql
```

这个 `samples` 主要是给你导出“消费 addon 的资源样例”，例如：

- `BackupSource`
- `BackupJob`
- `BackupPolicy`
- `RestoreJob`

它们的目标是让你在 addon 已安装的前提下，快速创建业务资源。

## About `01-backupaddon-mysql.yaml`

仓库里的：

- `config/samples/quickstart/01-backupaddon-mysql.yaml`

只是一个开发样例。

它适合：

- 阅读 addon 模板结构
- 不走 addon `.run` 包时做纯 YAML 调试

它不适合：

- 标准离线交付
- 已经执行过 `dataprotection-addon-mysql-*.run install` 的场景

## Dependency

在安装 mysql addon 包之前，core `dataprotection` operator 必须已经安装完成。
