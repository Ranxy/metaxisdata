> **语言 / Language:** [English](deploy.md) | [中文](deploy_zh.md)

# 部署 Metaxisdata

Metaxisdata 以单个容器镜像交付：服务端二进制内嵌 Web 应用，以 release profile 运行。PostgreSQL 是唯一的外部依赖，schema 在启动时自动迁移，因此一个空数据库只需要一个连接串。

容器只提供明文 HTTP，监听 8083。生产环境请在它前面用反向代理终止 HTTPS——见第 4 步。

## 前置条件

- 一个服务端可访问的 PostgreSQL，且库本身已经创建。
- 一个可在该库中创建表**和扩展**的数据库用户：迁移会执行 `CREATE EXTENSION IF NOT EXISTS pg_trgm`，用于元数据搜索索引（第 2 步）。
- Docker——没有其他宿主机依赖，运行所需的一切都在镜像内。
- 如需自行构建镜像：还需要启用 BuildKit 的 Docker（Docker 20.10+；较新的 Docker Desktop 与 Engine 默认已启用），以及能访问 Go module 与 npm install 的网络。

## 1. 获取镜像

### 拉取已发布的镜像

每次 release 都会把镜像发布到 GHCR，覆盖 `linux/amd64` 与 `linux/arm64`。按你想跟踪的标签拉取：

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3   # 发布时的 release 标签
docker pull ghcr.io/ranxy/metaxisdata:1.2.3    # 去掉 v 的语义化版本
docker pull ghcr.io/ranxy/metaxisdata:1.2      # 次版本线
docker pull ghcr.io/ranxy/metaxisdata:latest   # 最新的非预发布版本
```

- `v1.2.3` 与 `1.2.3` 指向同一个确切的 release。
- `1.2` 跟踪次版本线上最新的 release。
- `latest` 跟踪最新的非预发布 release。预发布版本会发布自己的标签（如 `v1.2.3-rc.1`），但从不移动别名——跟踪 `latest` 的部署绝不会拿到候选版本。
- 每个 release 还携带一个 `sha-<commit>` 标签，对应确切的提交。

### 自行构建镜像

镜像由 [scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh) 依据 [scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server) 构建；`make docker-build` 运行的就是同一个脚本：

```bash
scripts/build_metaxisdata_docker.sh                  # release profile -> :dev 与 :latest
VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh   # release profile -> :v1.2.3 与 :latest
make docker-build                                    # 等同于第一条命令

scripts/build_metaxisdata_docker.sh --dev            # dev profile，仅用于本地试验
make docker-build-dev
```

每次构建都会把镜像打在 `IMAGE`（默认 `metaxisdata/metaxisdata`）下；只有 release 构建才会附加 `:latest`。镜像内只包含服务端二进制——`mxd` CLI 是独立产物，用 `make build-cli` 构建。

构建参数：

| 变量 | 作用 |
| --- | --- |
| `VERSION` | 镜像标签，同时也是二进制对外报告的版本。默认 `dev`。 |
| `IMAGE` | 镜像名。默认 `metaxisdata/metaxisdata`。 |
| `BUILD_PROXY` | Go module 下载与 npm install 所用的代理。 |
| `GOPROXY` | Go module 代理。默认取本机 `go env GOPROXY`。 |
| `NPM_REGISTRY` | npm registry。默认取本机 `npm config get registry`。 |
| `APK_MIRROR` | Alpine CDN 的替换地址，例如 `https://mirrors.aliyun.com/alpine`。该值会被原样写入镜像内的 `/etc/apk/repositories`，并可通过 `docker history` 看到，因此绝不能包含凭据。 |

不要为构建全局 export `HTTPS_PROXY`：BuildKit 会把标准代理变量注入每个构建阶段，运行时阶段也在内，凭据会一并带过去。`BUILD_PROXY` 是只有构建阶段声明的自定义参数，因此传入的值只停留在构建之内。

## 2. 准备 PostgreSQL

把 `PG_URL` 指向一个已存在的数据库。迁移器会在首次启动时创建自己的表，所以空数据库即可：

```
postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable
```

迁移还会创建一个扩展，用于元数据搜索索引：

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_trgm` 属于标准 contrib，随每个受支持的 PostgreSQL 发行版一起提供。如果应用用户无权创建扩展，请先用管理员执行这条语句；迁移随后会发现它已经存在。

## 3. 启动服务端

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:v1.2.3
```

容器以 uid 1001 运行，健康状态在 `/healthz` 上报告：

```bash
curl -fsS http://localhost:8083/healthz
```

### 首次启动

1. 打开 `http://<host>:8083`，创建第一个账号。它会成为工作区管理员。
2. 可选：在 *设置* → *通用设置* 中开启「禁止自助注册」，此后只有管理员能创建用户。
3. 添加一个实例并同步它的 schema。
4. 如需把注册中心提供给 MCP 客户端，请在同一页面启用 MCP 端点。它需要工作区外部地址；见 [mcp.md](mcp.md)。

### 环境变量

| 环境变量 | 说明 |
| --- | --- |
| `PG_URL` | PostgreSQL 连接串（必填）。 |
| `METAXISDATA_PORT` | 监听端口，默认 8083。改端口请改这里，而不要用 `--port`：镜像的健康检查只跟随这个变量。 |
| `METAXISDATA_ENCRYPTION_KEY` | 可选。默认情况下，用于加密已存数据源凭据的密钥就存放在数据库自身之中，任何能访问数据库的人都能解密全部凭据。如需防范数据库泄露，可以配置它：数据密钥会被一把保存在数据库之外的密钥再包裹一层，此后仅凭数据库泄露无法还原出已存的凭据。代价是：这把密钥从此成为打开已存凭据的唯一钥匙——一旦丢失，服务端会拒绝启动，且每个实例、每个 LLM 供应商的已存凭据都无法再读取，只能逐个重新录入。见 [security-posture.md](security-posture.md)（英文）。 |
| `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` | 轮换期间使用的旧密钥，多个时逗号分隔。 |
| `METAXISDATA_JSON_LOGGING=true` | 输出 JSON 日志而不是文本日志。 |
| `METAXISDATA_DEBUG=true` | debug 级日志，并把 `/debug/pprof` 与 `/metrics` 开放给所有能访问该服务端的人。 |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | 允许携带凭据调用 API 的浏览器源，逗号分隔。仅当 SPA 由另一个源提供时需要；留空则不安装任何 CORS 中间件。 |
| `METAXISDATA_TRUSTED_PROXIES` | 记录审计来源地址时可以采信其 `X-Forwarded-For` 的 peer IP/CIDR，逗号分隔（第 4 步）。 |

### 健康检查与构建元数据

| 端点 | 回答 |
| --- | --- |
| `GET /healthz` | 进程正在处理请求时返回 `OK`。镜像的 `HEALTHCHECK` 会轮询它。 |
| `GET /api/version` | 构建该镜像所用的 `version`、`git_commit` 与 `build_time`。SPA 的用户菜单和 *Settings → General* 会读取它。 |

两个端点都是匿名的，除「正在运行哪个 release」之外不泄露任何信息。

### 运维说明

- 要备份的是数据库，而不是容器：容器不挂载任何卷，也不保存本地状态。
- 镜像需要一个可写路径 `/tmp`，MSSQL 驱动会用到其中的临时文件。加固部署请使用 `--read-only --tmpfs /tmp`；裸的 `--read-only` 会移除镜像里唯一可写的目录。
- 不设置 `TZ` 时，时钟与所有日志时间戳都是 UTC。
- 入口脚本把上述环境变量映射为服务端 flag，其余命令参数原样向后传递。显式参数优先于映射来的参数，因此 `docker run … ghcr.io/ranxy/metaxisdata:v1.2.3 --debug` 依然有效。

## 4. 用反向代理终止 HTTPS

在服务端前面放一个带 HTTPS 的反向代理。然后把代理地址告诉服务端，让审计日志记录客户端而不是代理——把 `METAXISDATA_TRUSTED_PROXIES` 设为代理的地址或 CIDR：

```bash
-e METAXISDATA_TRUSTED_PROXIES=10.0.0.0/8
```

不设置它的话，审计日志会把代理地址记成每个请求的来源。

通知流不需要为代理调整超时：服务端会发送 keep-alive 心跳，空闲连接不会被代理的默认读取超时切断。

## 本地试用

在本机跑一个用完即弃的实例与正式部署是两回事：compose 栈，以及如何让它改用已发布的镜像，见 [local-trial_zh.md](local-trial_zh.md)。