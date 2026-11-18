> **语言 / Language:** [English](deploy.md) | [中文](deploy_zh.md)

# 部署 Metaxisdata

Metaxisdata 以一个自包含的二进制交付：服务端内嵌 Web 应用，以 release profile 运行。每次 release 会以两种方式发布它——GHCR 上的容器镜像，以及可直接在宿主机上运行的 GitHub Releases 预编译二进制——两者交付的是同一个服务端。PostgreSQL 是唯一的外部依赖，schema 在启动时自动迁移，因此一个空数据库只需要一个连接串。

服务端只提供明文 HTTP，监听 8083。生产环境请在它前面用反向代理终止 HTTPS——见第 4 步。

## 前置条件

- 一个服务端可访问的 PostgreSQL，且库本身已经创建。
- 一个可在该库中创建表**和扩展**的数据库用户：迁移会执行 `CREATE EXTENSION IF NOT EXISTS pg_trgm`，用于元数据搜索索引（第 2 步）。
- Docker——没有其他宿主机依赖，运行所需的一切都在镜像内。
- 使用预编译二进制时同样不需要 Docker，也没有其他运行时依赖：二进制是静态的、内嵌 Web 应用。出站 TLS 需要 CA 证书包；仅当 MSSQL 数据源配置了 `timezone` 参数时还需要时区数据库（IANA tzdata）。镜像安装 `ca-certificates` 与 `tzdata` 正是这两个用途；主流发行版默认都已提供。
- 如需自行构建产物：`scripts/` 下的脚本在自己的注释头里说明工具链要求，以及版本、代理与镜像源等设置。镜像构建需要启用 BuildKit 的 Docker（Docker 20.10+）；二进制构建需要 Go 工具链、Node 24（前端 `engines` 字段）与前端锁定的 pnpm 版本（[scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) 会经 corepack 解析它）。

## 1. 获取服务端

两条渠道发布的是同一个产物：SPA 已内嵌、prod profile 已编译进去，`GET /api/version` 会报告该二进制来自哪个 release。按宿主机情况选择其一。

### 拉取已发布的镜像

每次 release 都会把镜像发布到 GHCR，覆盖 `linux/amd64` 与 `linux/arm64`。按你想跟踪的别名拉取：

```bash
docker pull ghcr.io/ranxy/metaxisdata:latest
```

`latest` 跟踪最新的非预发布 release。每次 release 还会发布 release 标签、去掉 `v` 的语义化版本、次版本线与 `sha-<commit>` 标签；[releases 页面](https://github.com/Ranxy/metaxisdata/releases) 列出最新 release，用它的标签替换 `latest` 即可固定版本。预发布版本会发布自己的标签，但从不移动别名。

### 自行构建镜像

镜像由 [scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh) 依据 [scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server) 构建；`make docker-build` 运行的就是同一个脚本。release 构建会把镜像打上 `VERSION` 标签并同时占用 `:latest`；`--dev` 构建 dev profile，仅用于本地试验。镜像内只包含服务端二进制——`mxd` CLI 是独立产物，用 `make build-cli` 构建。代理、registry 与镜像源参数记录在脚本的注释头里；`APK_MIRROR` 会被原样写入镜像内的 `/etc/apk/repositories` 并出现在 `docker history` 中，因此绝不能包含凭据。

### 下载预编译二进制

对于不跑 Docker 的宿主机，每次 release 都会发布五个平台的静态二进制，以及列出所有资产校验和的 `SHA256SUMS`：

| 平台 | 文件 |
| --- | --- |
| Linux (amd64) | `metaxisdata-linux-amd64` |
| Linux (arm64) | `metaxisdata-linux-arm64` |
| macOS (Apple Silicon) | `metaxisdata-darwin-arm64` |
| macOS (Intel) | `metaxisdata-darwin-amd64` |
| Windows (amd64) | `metaxisdata-windows-amd64.exe` |

从该 release 的下载路径取对应平台的资产：

```bash
curl -fsSL -o metaxisdata \
  https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-linux-amd64
chmod +x metaxisdata
```

`releases/latest/download/…` 指向最新的**非预发布** release，预发布版本不会应答这个 URL；预发布请在 URL 里带 tag——`…/releases/download/<tag>/<asset>`。Windows 上请用 `curl.exe` 与 `.exe` 资产。

下载的二进制与 [scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) 产出的一致：SPA 已内嵌、prod profile，`metaxisdata --version` 会报告 release 标签。

### 自行构建二进制

[scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) 构建前端 SPA 并编译出自包含二进制（`make build-binary` 运行的就是同一个脚本）。`--release-assets` 一次产出五个 release 平台的资产与 `SHA256SUMS`，每个目标都显式指定 `GOOS`/`GOARCH`；`--dev` 构建 dev profile，仅用于本地试验。脚本的注释头说明了它读取的构建元数据与网络设置。

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
  ghcr.io/ranxy/metaxisdata:latest
```

容器以 uid 1001 运行，健康状态在 `/healthz` 上报告：

```bash
curl -fsS http://localhost:8083/healthz
```

### 直接运行二进制

预编译二进制（或 [scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) 产出的 `build/metaxisdata`）接受同样的 `PG_URL`，并以前台方式运行——把它交给 systemd 等服务管理器托管，就像容器交给 Docker 一样：

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --port 8083
```

`metaxisdata --version` 会打印版本、提交与构建时间。升级就是替换二进制并重启：待执行的迁移会在启动时自动应用，因此仍按下文运维说明的备份纪律执行。

**绝不能让新旧二进制并行运行。** 早于 connected-app 刷新凭据的二进制在刷新时会删除并重新插入授权行，把已吊销的连接静默恢复成未吊销。请先停掉所有旧副本，再启动新二进制。回滚到旧二进制也是同样的效果：如果必须回滚，请恢复升级前的数据库备份，并轮换部署的签名密钥（`AUTH_SECRET` setting）——所有未过期的令牌都会停止验签，而备份之后做出的吊销也随备份一起丢失，需要重新吊销受影响的已连接应用。

`PG_URL` 与加密密钥两个变量由服务端自身读取，对二进制同样有效。其余 `METAXISDATA_*` 变量属于镜像入口脚本的映射——在裸宿主机上请改用与之对应的 flag：`--port`、`--debug`、`--enable-json-logging`、`--cors-allow-origins`、`--trusted-proxies`、`--metrics-addr`。

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --trusted-proxies 10.0.0.0/8 --cors-allow-origins https://metaxisdata.example.com
```

### 每个元数据库只跑一个服务端进程

每个元数据库只运行一个服务端进程。后台 runner（schema 同步、血缘分析、摄取血缘的重新校验、维护任务）把队列、按库的同步闸门、用户可见的同步操作与重新校验状态都保存在内存里，第二个副本会对同一批实例重复每次同步与分析，并把每个用户的同步结果消息拆到不同进程。扩容元数据库不会让服务端变成可复制的角色；要扩展请扩 PostgreSQL，而不是进程。重启会丢失排队中的工作，但下次扫描会收敛。见 [security-posture.md](security-posture.md)。

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
| `METAXISDATA_DEBUG=true` | debug 级日志，并把 `/debug/pprof` 开放给所有能访问该服务端的人。 |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | 允许携带凭据调用 API 的浏览器源，逗号分隔。仅当 SPA 由另一个源提供时需要；留空则不安装任何 CORS 中间件。 |
| `METAXISDATA_TRUSTED_PROXIES` | 记录审计来源地址时可以采信其 `X-Forwarded-For` 的 peer IP/CIDR，逗号分隔（第 4 步）。 |
| `METAXISDATA_METRICS_ADDR` | 可选。Prometheus 指标端点的监听地址，例如 `127.0.0.1:9090`；不设置则关闭。该端点匿名且**从不在 API 端口**提供服务，请只绑到抓取方能访问的接口。见 [Prometheus 指标](#prometheus-指标)。 |

另有两个变量属于宿主机设置而非部署配置，服务端会从环境中读取：`SSH_AUTH_SOCK`，当数据源没有内联 SSH 私钥时 SSH 隧道使用的 ssh-agent socket；以及 `TZ`，日志时间戳所遵循的进程时区（不设置即为 UTC）。

### 健康检查与构建元数据

| 端点 | 回答 |
| --- | --- |
| `GET /healthz` | 进程正在处理请求时返回 `OK`。镜像的 `HEALTHCHECK` 会轮询它。 |
| `GET /api/version` | 构建该镜像所用的 `version`、`git_commit` 与 `build_time`。SPA 的用户菜单和 *Settings → General* 会读取它。 |

两个端点都是匿名的，除「正在运行哪个 release」之外不泄露任何信息。

### Prometheus 指标

设置 `METAXISDATA_METRICS_ADDR`（直接运行二进制时传 `--metrics-addr`），指标会由独立监听端口提供：

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --port 8083 --metrics-addr 127.0.0.1:9090
```

该监听端口只在 `/metrics` 上提供 `GET` 与 `HEAD`；它不是第二个 API，API 端口也从不提供指标。该端点只支持拉取且匿名，请把它放在可信接口或内网上，并让 Prometheus 指向 `http://<metrics-addr>/metrics`。`--debug` 只控制日志与 pprof。

端点会导出标准 Go 与进程指标、带发布版本与提交的 `metaxisdata_build_info`，以及服务端自己的 `metaxisdata_*` 系列：schema 同步、血缘分析、OpenLineage 摄取、数据质量 run、LLM 调用、HTTP/RPC 流量、队列深度与元数据库连接池。标签只使用有界枚举，例如 engine、status、result、reason、method 与 route；实例、库、表、GUID、SQL 和用户数据不会出现在标签里，因此标签集合不会随注册中心增长。

计数器是进程本地的：重启会归零，而部署只有一个服务端进程，没有第二个副本可以汇总。

### 运维说明

- 要备份的是数据库，而不是容器：容器不挂载任何卷，也不保存本地状态。
- 镜像需要一个可写路径 `/tmp`，MSSQL 驱动会用到其中的临时文件。加固部署请使用 `--read-only --tmpfs /tmp`；裸的 `--read-only` 会移除镜像里唯一可写的目录。
- 入口脚本把上述环境变量映射为服务端 flag，其余命令参数原样向后传递。显式参数优先于映射来的参数，因此 `docker run … ghcr.io/ranxy/metaxisdata:latest --debug` 依然有效。

## 4. 用反向代理终止 HTTPS

在服务端前面放一个带 HTTPS 的反向代理。然后把代理地址告诉服务端，让审计日志记录客户端而不是代理——把 `METAXISDATA_TRUSTED_PROXIES` 设为代理的地址或 CIDR：

```bash
-e METAXISDATA_TRUSTED_PROXIES=10.0.0.0/8
```

不设置它的话，审计日志会把代理地址记成每个请求的来源。

`METAXISDATA_TRUSTED_PROXIES` 是镜像入口脚本对 flag 的映射；直接运行二进制时请改传 flag 本身（`./metaxisdata --trusted-proxies 10.0.0.0/8`）。

通知流不需要为代理调整超时：服务端会发送 keep-alive 心跳，空闲连接不会被代理的默认读取超时切断。

## 本地试用

在本机跑一个用完即弃的实例与正式部署是两回事：compose 栈，以及如何让它改用已发布的镜像，见 [local-trial_zh.md](local-trial_zh.md)。
