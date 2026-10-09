> **语言 / Language:** [English](README.md) | [中文](README_zh.md)

> **注意:** Metaxisdata 只以容器镜像的形式发布，面向 `linux/amd64` 与 `linux/arm64`；任何平台都没有预编译二进制。

# Metaxisdata

Metaxisdata 是一个自托管的**数据治理与元数据平台**。它把数据库实例的 schema 同步进一个统一的元数据注册中心，并从 SQL 定义和摄入的 OpenLineage 事件中推导**表级与列级血缘**。

## 它能做什么

- **元数据注册中心**：把实例的 schema 同步进一个可搜索的注册中心，每个对象的 DDL 就存放在它旁边，并保留变更历史。
- **表级与列级血缘**：从视图、物化视图和手动 SQL 中分析出精确到列的血缘，支持按深度和按字段展开。
- **OpenLineage**：摄入 OpenLineage 的 run 事件，映射 namespace，签发 API key，并浏览 job、dataset、event，附带跳回 Airflow 的链接。
- **IAM 与审计**：用户、用户组、命名角色和访问策略编辑器；需要审计的操作会写入永久保留的审计日志。
- **Agent 集成**：`mxd` 命令行客户端让 Agent 每条命令只拿到一个 JSON 文档，退出码稳定，并支持设备码登录（[cli/README.md](cli/README.md)）。启用 MCP 后，注册中心还会以只读工具的形式提供，并由 OAuth 2.1 授权服务器保护（[docs/mcp.md](docs/mcp.md)）。

## 架构概览

Metaxisdata 是一个由 PostgreSQL 支撑的 Go 服务端二进制：

- 服务端提供 ConnectRPC API、其 REST 网关和 Vue 3 单页应用（已内嵌进发布镜像），并在启用时提供 MCP 端点。
- PostgreSQL 是唯一的外部依赖。schema 在启动时自动迁移，因此一个全新的数据库只需要一个连接串。
- 后台 runner 负责同步 schema、分析并校验血缘，以及执行维护性任务。
- `mxd` 是独立的产物，从不包含在服务端镜像中。

## 快速开始

### 正式部署

每次发布 GitHub Release 都会把镜像推送到 GHCR，支持 `linux/amd64` 与 `linux/arm64`：

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://user:password@db-host:5432/metaxisdata?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:latest
```

如果拉取失败，说明还没有发布过 Release：用 `make docker-build` 从本地检出目录构建同一个镜像。

然后打开 `http://<host>:8083`（上面发布的端口）：

1. **准备 PostgreSQL**，并把 `PG_URL` 指向它；首次启动时 schema 会自动迁移。
2. **立刻创建第一个账号。** 它会成为工作区管理员；在管理员于 *设置* → *通用设置* 中开启「禁止自助注册」之前，注册通道一直是开放的。
3. **在界面中添加一个实例**，并同步它的 schema。
4. **可选**：在 *设置* → *通用设置* 中启用 MCP 端点——这还需要先配置工作区的「外部访问地址」。
5. **在服务端前面用反向代理终止 HTTPS**：镜像有意只提供明文 HTTP。请把 `METAXISDATA_TRUSTED_PROXIES` 设为代理的地址，否则审计日志会把代理记成每个请求的来源。

> 完整的部署指南——每个环境变量、健康检查与版本端点，以及如何自行构建镜像——见 [docs/deploy_zh.md](docs/deploy_zh.md)。

### 本地试用

还没有自己的 PostgreSQL？[docker-compose.yml](docker-compose.yml) 会把两者一起启动，所以 `make docker-up` 就够你看一眼构建出来的镜像：

```bash
make docker-up      # 构建并启动 PostgreSQL + 服务端，访问 http://localhost:8083
make docker-down    # 停止两者；执行 `docker compose down -v` 可连数据卷一起删除
```

打开 <http://localhost:8083>，创建第一个账号——它会成为该工作区的管理员。端口是对外发布的，开发用密码是硬编码的，所以它只是用来查看一个构建出来的镜像，并不是生产拓扑。compose 文件本身、如何改用已发布镜像、以及 `:dev` 标签的注意事项，见 [docs/local-trial_zh.md](docs/local-trial_zh.md)。

## 截图

**元数据浏览** —— 一个已同步数据库的全部表，附带行数、大小、列数和索引。

![元数据浏览：已同步数据库的表清单，含行数、大小、列数与索引](docs/images/metaxisdata_metadata_small.png)

**表级与列级血缘** —— 上下游血缘边，可一直展开到逐列的字段轨迹。

![血缘图：某张表的上下游血缘边，已展开到列级字段轨迹](docs/images/metaxisdata_lineage_small.png)

## 开发

```bash
# 后端 —— 需要 PG_URL；8083 端口与 Vite 代理一致
PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --debug

# 前端 —— Vite 跑在 :3000，把 API、OAuth 和 MCP 路由代理到后端
pnpm --dir frontend install
pnpm --dir frontend dev

# 构建
make build           # dev profile
make build-release   # release profile
make build-embed     # release profile，并把单页应用内嵌进二进制
make build-cli       # mxd 客户端，产物为 ./build/mxd
```

`make build` 生成的二进制不会内嵌前端：它只会返回一个占位页面，所以本地开发请运行 Vite 开发服务器，或者改用 `make build-embed` 构建。

`go test ./...` 是纯本地测试，不需要数据库。下面的集成测试套件在 `integration` build tag 下，用真实服务端跑在真实的 PostgreSQL 和 MySQL 上；Docker 不可用时会自动跳过：

```bash
make test-integration-smoke
make test-integration
```

开发规范、lint 规则和完整命令集见 [AGENTS.md](AGENTS.md)。

## 技术栈

- **后端**：Go、PostgreSQL、ConnectRPC（gRPC/HTTP）、Protobuf / buf
- **前端**：Vue 3、TypeScript、Vite、Tailwind CSS、shadcn-vue
- **命令行**：Go
