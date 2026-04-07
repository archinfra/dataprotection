# Data Protection Operator 发布与使用说明

## 1. GitHub 仓库里要配置什么

当前发布工作流读取两类配置：

- `Secrets`
  用于保存敏感信息，例如账号和密码
- `Variables`
  用于保存非敏感配置，例如目标命名空间、是否启用某个镜像仓库

GitHub 仓库页面路径：

`Settings -> Secrets and variables -> Actions`

在这个页面里：

- `Secrets` 点 `New repository secret`
- `Variables` 点 `New repository variable`

## 2. 必配 Variables

### `PUBLISH_DOCKERHUB`

是否推送到 Docker Hub。

可选值：

- `true`
- `false`

推荐：

- `true`

### `DOCKERHUB_NAMESPACE`

Docker Hub 的 namespace，也就是最终镜像名前缀里的组织/用户名。

例如：

- `archinfra`

最终镜像会长成：

- `docker.io/archinfra/dataprotection-operator:latest`
- `docker.io/archinfra/dataprotection-mysql:8.0.45`

如果不填，workflow 会默认用 GitHub 仓库 owner。

### `PUBLISH_ALIYUN`

是否推送到阿里云容器镜像服务。

可选值：

- `true`
- `false`

推荐：

- `true`

### `ALIYUN_REGISTRY`

阿里云镜像仓库登录地址。

这个值不要瞎猜，直接去 ACR 控制台复制。

常见形态示例：

- 新版个人版：`crpi-xxxx.cn-hangzhou.personal.cr.aliyuncs.com`
- 企业版：`xxx-registry.cn-hangzhou.cr.aliyuncs.com`
- 某些兼容域名场景也可能是：`registry.cn-hangzhou.aliyuncs.com`

你最终应该以控制台展示的登录地址为准。

### `ALIYUN_NAMESPACE`

阿里云镜像仓库里的 namespace。

例如：

- `archinfra`

最终镜像会长成：

- `<ALIYUN_REGISTRY>/archinfra/dataprotection-operator:latest`

如果不填，workflow 会默认用 GitHub 仓库 owner。

## 3. 必配 Secrets

### Docker Hub

如果 `PUBLISH_DOCKERHUB=true`，需要：

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

推荐不要用密码，直接用 Docker Hub Access Token。

### 阿里云 ACR

如果 `PUBLISH_ALIYUN=true`，需要：

- `ALIYUN_USERNAME`
- `ALIYUN_PASSWORD`

这个用户名和密码要以 ACR 控制台里给你的“镜像仓库登录凭证”为准。
不要混用阿里云控制台登录密码。

## 4. 一套可直接照抄的示例

假设你要推到：

- Docker Hub：`docker.io/archinfra`
- 阿里云：`crpi-2zeabcde.cn-hangzhou.personal.cr.aliyuncs.com/archinfra`

那就这样配：

Variables:

- `PUBLISH_DOCKERHUB=true`
- `DOCKERHUB_NAMESPACE=archinfra`
- `PUBLISH_ALIYUN=true`
- `ALIYUN_REGISTRY=crpi-2zeabcde.cn-hangzhou.personal.cr.aliyuncs.com`
- `ALIYUN_NAMESPACE=archinfra`

Secrets:

- `DOCKERHUB_USERNAME=<你的 Docker Hub 用户名>`
- `DOCKERHUB_TOKEN=<你的 Docker Hub PAT>`
- `ALIYUN_USERNAME=<你的 ACR 登录用户名>`
- `ALIYUN_PASSWORD=<你的 ACR 登录密码>`

## 5. 发布后会生成什么

`release.yml` 会发布这些镜像：

- `dataprotection-operator`
- `dataprotection-mysql`
- `dataprotection-minio-mc`
- `dataprotection-busybox`

标签策略：

- 每次发布都有 `VERSION`
- 每次发布都有 `sha-<commit>`
- 推送到 `main` 时附带 `latest`
- 打 tag 时附带同名 tag

## 6. 以后怎么触发发布

两种方式：

### 方式一：推 main

直接 push 到 `main`，会自动触发：

- 单测和生成校验
- 多架构镜像推送
- `.run` 安装包构建

### 方式二：打 tag

例如：

```bash
git tag v0.1.0
git push origin v0.1.0
```

会额外生成 GitHub Release，并把 `.run` 产物挂到 release 里。

## 7. 发布成功后怎么使用

### 在线安装

如果集群能直接拉你推送好的镜像：

```bash
./data-protection-operator-amd64.run install \
  --registry crpi-2zeabcde.cn-hangzhou.personal.cr.aliyuncs.com/archinfra \
  --skip-image-prepare \
  -y
```

这里 `--skip-image-prepare` 的意思是：

- 目标仓库里已经有 operator 和 runtime 镜像
- 安装器不用再把打包镜像重新 push 一遍

### 离线安装

如果集群所在环境不能直接访问外网：

1. 从 GitHub Actions artifact 或 GitHub Release 下载 `.run`
2. 把 `.run` 带到可访问目标私有仓库的机器上
3. 执行：

```bash
./data-protection-operator-amd64.run install \
  --registry registry.example.com/archinfra \
  --registry-user admin \
  --registry-password '<password>' \
  -y
```

安装器会：

- 从 payload 导入镜像
- 推送到你指定的目标仓库
- 安装 CRD / RBAC / Deployment
- 把默认 runtime 镜像地址渲染成你指定仓库里的地址

## 8. 推荐运维约定

推荐长期固定以下约定：

- Docker Hub 作为公开分发地址
- 阿里云 ACR 作为国内拉取地址
- `.run` 作为离线交付物
- 集群内实际安装统一走自己的私有仓库

这样会形成三层发布链路：

1. GitHub Actions 产出标准镜像
2. `.run` 负责离线搬运和落地
3. 集群最终只依赖你自己的目标仓库

## 9. 常见问题

### Q1：阿里云到底填哪个 registry？

答：填 ACR 控制台给你的登录地址，不要手写猜测。

### Q2：阿里云 namespace 需要提前建吗？

答：建议提前建好。不同版本 ACR 的自动建仓行为可能不同，但 namespace 至少要先确认存在。

### Q3：能只推 Docker Hub 不推阿里云吗？

可以。

把：

- `PUBLISH_DOCKERHUB=true`
- `PUBLISH_ALIYUN=false`

即可。

### Q4：能只推阿里云不推 Docker Hub 吗？

可以。

把：

- `PUBLISH_DOCKERHUB=false`
- `PUBLISH_ALIYUN=true`

即可。

### Q5：后面 operator 安装时一定要用 `.run` 吗？

不是。

你也可以直接 `kubectl apply` 安装清单。
但对于离线交付、跨环境搬运和统一落仓，`.run` 体验会更稳。

