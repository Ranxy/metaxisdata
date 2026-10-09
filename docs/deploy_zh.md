> **语言 / Language:** [English](deploy.md) | [中文](deploy_zh.md)

# 部署

Metaxisdata 以单个容器部署：内嵌已构建 SPA 的服务端二进制，运行在 release（prod）profile 下。全部状态都存在 PostgreSQL 中，schema 在启动时自动迁移，因此一个空数据库只需要一个连接串。

容器只提供明文 HTTP，监听 8083。生产环境请在它前面放一个带 HTTPS 的反向代理（见 §4）。

## 前置条件

- 一个可从服务端访问的 PostgreSQL，且其中数据库已经存在。
- 一个可以在该数据库里创建表**和扩展**的用户——迁移会为元数据搜索索引执行 `CREATE EXTENSION IF NOT EXISTS pg_trgm`（见 §2）。
- 拉取已发布镜像：无额外要求，`docker pull` 就够了。
- 自己构建镜像：需要启用 BuildKit 的 Docker（Docker 20.10+；较新的 Docker Desktop 和 Engine 默认已启用），以及能访问 Go module 和 npm install 的外网（或构建代理）。

## 1. 获取镜像

### 1a. 拉取已发布的镜像（推荐）

每个 GitHub Release 都会把镜像发布到 GHCR，覆盖 `linux/amd64` 与 `linux/arm64`。拉取你想跟踪的标签：

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3   # 发布时的 release 标签
docker pull ghcr.io/ranxy/metaxisdata:1.2.3    # 去掉 v 的语义化版本
docker pull ghcr.io/ranxy/metaxisdata:1.2      # 次版本线
docker pull ghcr.io/ranxy/metaxisdata:latest   # 最新的非预发布版本
```

`:latest` 只会在非预发布的 release 上移动，所以跟踪它的部署永远不会拿到候选版本；每个 release 同时还有一个 `sha-` 标签，如果你想钉住某个确切的提交。

如果拉取失败，说明还没有发布过 Release——请改为自己构建镜像（§1b）。

### 1b. 自己构建镜像

镜像由 [scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh) 依据 [scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server) 构建；`make docker-build` 运行的就是这个脚本：

```bash
scripts/build_metaxisdata_docker.sh                 # release，打 :dev 和 :latest 标签
VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh # release，打 :v1.2.3 和 :latest 标签
make docker-build                                  # 等同于第一行

scripts/build_metaxisdata_docker.sh --dev          # dev profile，仅用于本地试验
make docker-build-dev
```

每次构建都会把镜像打在 `IMAGE`（默认 `metaxisdata/metaxisdata`）下。只有 release 构建才会占用 `:latest`；dev 构建不会。

构建参数：

| 变量 | 作用 |
| --- | --- |
| `VERSION` | 镜像标签**同时**也是二进制对外报告的版本。默认 `dev`。 |
| `IMAGE` | 镜像名。默认 `metaxisdata/metaxisdata`。 |
| `BUILD_PROXY` | Go module 下载和 npm install 使用的代理。 |
| `GOPROXY` | Go module 代理。默认取本机 `go env GOPROXY`。 |
| `NPM_REGISTRY` | npm registry。默认取本机 `npm config get registry`。 |
| `APK_MIRROR` | Alpine CDN 的替换地址，例如 `https://mirrors.aliyun.com/alpine`。该值会被原样写入**已发布镜像内部**的 `/etc/apk/repositories`，并可通过 `docker history` 看到，所以绝不要在里面放凭据。 |

不要为 `docker build` 全局 export `HTTPS_PROXY`：BuildKit 会把它注入到每个阶段，包括 runtime 阶段，凭据也一并带过去。`BUILD_PROXY` 是只有构建阶段声明的自定义参数，因此你传入的值被限制在这些阶段之内。

## 2. 准备 PostgreSQL

把 `PG_URL` 指向一个已存在的数据库。迁移器会在首次启动时创建自己的表，所以一个空数据库就够了：

```
postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable
```

迁移还会为元数据搜索索引创建一个扩展：

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_trgm` 属于标准 contrib，随每个受支持的 PostgreSQL 发行版一起提供。如果应用用户没有创建扩展的权限，请先用管理员执行上面这行；迁移随后会发现它已经存在。

## 3. 启动服务端

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:v1.2.3
```

容器以 uid 1001 运行，并会检查 `/healthz`。用下面这条命令验证：

```bash
curl -fsS http://localhost:8083/healthz
```

打开 `http://<host>:8083`，创建第一个账号。它会成为工作区管理员；在管理员于 *设置* → *通用设置* 中开启「禁止自助注册」之前，注册通道一直开放。接着添加一个实例并同步它的 schema。若要把注册中心交给 MCP 客户端使用，请在同一个地方启用 MCP 端点——它还需要工作区的「外部访问地址」。

服务端环境变量：

| 环境变量 | 说明 |
| --- | --- |
| `PG_URL` | PostgreSQL 连接串（必填）。 |
| `METAXISDATA_PORT` | 监听端口，默认 8083。改端口请改这里，而不要用 `--port`：镜像的健康检查只跟随这个变量。 |
| `METAXISDATA_ENCRYPTION_KEY` | 包裹每个部署各自的数据密钥，该密钥用于加密存储的数据源凭据，使一份数据库转储本身无法解密它们。请在首次启动前设置；见 [security-posture.md](security-posture.md)（英文）。 |
| `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` | 轮换期间使用的上一些密钥，逗号分隔。 |
| `METAXISDATA_JSON_LOGGING=true` | 输出 JSON 日志而不是文本日志。 |
| `METAXISDATA_DEBUG=true` | debug 级日志，并会把 `/debug/pprof` 和 `/metrics` 端点开放给任何能访问该服务端的人。 |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | 允许携带凭据调用 API 的浏览器源，逗号分隔。只有当 SPA 由另一个源提供时才需要；留空则不安装任何 CORS 中间件。 |
| `METAXISDATA_TRUSTED_PROXIES` | 记录审计来源地址时可以采信其 `X-Forwarded-For` 的 peer IP/CIDR，逗号分隔（见 §4）。 |

健康检查与构建元数据：

| 端点 | 回答 |
| --- | --- |
| `GET /healthz` | 进程正在处理请求时返回 `OK`。镜像的 `HEALTHCHECK` 会轮询它。 |
| `GET /api/version` | 构建该镜像所用的 `version`、`git_commit` 和 `build_time`。SPA 的用户菜单和 *Settings → General* 会读取它。 |

两者都是匿名端点，且除了"正在运行哪个版本"之外不泄露任何信息。

说明：

- 要备份的是数据库，不是容器。容器不挂载任何卷，也不保存本地状态。
- 镜像需要一个可写路径 `/tmp`，MSSQL 驱动会用到其中的临时文件。加固过的部署应当用 `--read-only --tmpfs /tmp`，而不是裸的 `--read-only`，后者会移除镜像里唯一可写的目录。
- 不设置 `TZ` 时，时钟和所有日志时间戳都是 UTC。
- 入口脚本会把这些变量映射为服务端 flag，然后把命令其余部分原样传下去；显式参数优先于映射来的参数，所以 `docker run … ghcr.io/ranxy/metaxisdata:v1.2.3 --debug` 依然有效。

## 4. 外部访问

在服务端前面放一个带 HTTPS 的反向代理，然后把代理的地址告诉服务端，让审计日志记录客户端而不是代理。把 `METAXISDATA_TRUSTED_PROXIES` 设为代理的地址或 CIDR：

```bash
-e METAXISDATA_TRUSTED_PROXIES=10.0.0.0/8
```

不设置它的话，审计日志会把代理的地址记成每个请求的来源。

通知流不需要额外调整超时：服务端会发送 keep-alive 心跳，因此代理不会因为自己的默认读取超时而断开空闲连接。

## 本地试用

在本机跑一个用完即弃的实例，和部署一个实例是两回事——compose 栈、它的注意事项，以及如何让它直接使用已发布镜像而不是自己构建，见 [local-trial_zh.md](local-trial_zh.md)。
