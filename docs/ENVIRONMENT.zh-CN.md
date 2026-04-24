# Data Protection Operator 环境处理文档

## 1. 指定 Linux 开发机

当前约定的 controller Linux 开发环境：

- Host: `36.138.61.152`
- User: `root`
- Hostname: `hm-test1`
- OS: `Ubuntu 22.04.4 LTS`

## 2. 已确认环境

机器上已可用：

- `git`
- `docker`
- `kubectl`
- `make`
- `gcc`

Go 不在默认 PATH，但以下路径可直接使用：

- `/usr/local/go/bin/go`
- `/usr/local/go/bin/gofmt`

## 3. 推荐工作目录

```bash
mkdir -p /root/workspace
cd /root/workspace
git config --global http.version HTTP/1.1
git clone https://github.com/archinfra/dataprotection.git
cd dataprotection
```

如果仓库已存在，直接进入目录更新即可。

## 4. 初始化命令

```bash
export PATH=/usr/local/go/bin:$PATH
cd /root/workspace/dataprotection
bash hack/bootstrap-dev-env.sh
```

脚本会：

- 确保 `go` 可执行
- 安装 `controller-gen`
- 执行 `go mod tidy`

## 5. 常用验证命令

```bash
export PATH=/usr/local/go/bin:$PATH
cd /root/workspace/dataprotection
make fmt
make generate
make manifests
make test
make build
```

如需手工验证离线安装包链路：

```bash
bash build.sh --arch amd64
```

## 6. 本轮基线要求

至少保证以下命令稳定通过：

- `bash hack/bootstrap-dev-env.sh`
- `make generate`
- `make manifests`
- `make test`
- `make build`

如果只是做开发侧补充验证，再加：

- `bash build.sh --arch amd64`

## 7. 注意事项

1. 不要用系统自带的旧版 `golang-go` 代替 `/usr/local/go/bin/go`
2. 远端验证优先以 Linux 结果为准
3. 如果从 Windows 同步代码到 Linux，请排除本地产物目录：
   `bin/`
   `dist/`
   `.build-payload/`
4. 新仓库已经独立，请直接从 `dataprotection` 仓库根目录启动
5. 离线安装验证需要 Docker 能正常拉取并保存多架构镜像



补充建议：

- 正式的多架构镜像和 .run 产包，优先通过 GitHub Actions 构建
- 开发机更适合做 generate/manifests/test/build 这类工程验证，不强依赖完整镜像拉取环境
