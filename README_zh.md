> **语言 / Language:** [English](README.md) | [中文](README_zh.md)

# Metaxisdata

Metaxisdata 是一个自托管的数据治理与元数据平台。它连接 MySQL/TiDB 与 PostgreSQL 实例，把它们的 schema 同步进一个可搜索的元数据注册中心，并从 SQL 定义和摄入的 OpenLineage 事件中推导表级与列级血缘。

## 功能

- **元数据注册中心** —— 把各实例的 schema 同步进一个统一且可搜索的注册中心，每个表和每个列都保存着自己的 DDL 与变更历史。
- **表级与列级血缘** —— 血缘从视图、物化视图和手动 SQL 中分析得来，精确到列。血缘图可以按深度展开，也可以沿着单个列的轨迹往下追踪。
- **OpenLineage** —— 摄入 run 事件，映射 namespace，签发 API key；浏览 job、dataset 和 event，并提供跳回 Airflow 的链接。
- **SQL 解释** —— 借助 LLM 解释一条 SQL 语句，解释范围限定在你所选的元数据之内，并带缓存。
- **访问控制与审计** —— 用户、用户组、命名角色和访问策略编辑器；需要审计的操作会写入一份永久保留的审计日志。
- **Agent 集成** —— `mxd` 命令行客户端（[cli/README.md](cli/README.md)）让 Agent 每条命令只拿到一个 JSON 文档，退出码稳定，并支持设备码登录。启用 MCP 后，注册中心还会以只读工具的形式提供，由 OAuth 2.1 授权服务器保护（[docs/mcp.md](docs/mcp.md)）。

## 快速上手

需要准备 Docker，以及一个容器可达的 PostgreSQL 数据库。镜像发布在 GHCR，覆盖 `linux/amd64` 与 `linux/arm64`：

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://user:password@db-host:5432/metaxisdata?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:latest
```

打开 `http://<host>:8083`，然后：

1. **创建第一个账号。** 它会成为工作区管理员。
2. **添加一个实例并同步它的 schema。**

后续步骤（可选）：

- **关闭自助注册。** 在 *设置* → *通用设置* 中开启「禁止自助注册」，此后只有管理员能创建用户。
- **启用 HTTPS。** 容器按设计只提供明文 HTTP。请在它前面用反向代理终止 HTTPS，并把 `METAXISDATA_TRUSTED_PROXIES` 设为代理地址，让审计日志记录客户端而不是代理。
- **保护已存凭据。** 设置 `METAXISDATA_ENCRYPTION_KEY`，这样仅凭一份数据库转储将无法解密平台为各实例保存的凭据——并妥善保管这把密钥：一旦丢失，已存的凭据将无法读取。
- **接入 MCP 客户端。** 在 *设置* → *通用设置* 中启用 MCP 端点（需要先配置工作区外部地址），再参照 [docs/mcp.md](docs/mcp.md) 配置客户端。

> 完整的部署指南——每个环境变量、健康检查与版本端点，以及如何自行构建镜像——见 [docs/deploy_zh.md](docs/deploy_zh.md)。

## 本地试用

[docker-compose.yml](docker-compose.yml) 会把 PostgreSQL 和服务端一起启动，因此在你的机器上看一眼构建出来的镜像，只需要一条命令：

```bash
make docker-up      # 构建镜像，然后启动 PostgreSQL + 服务端，访问 http://localhost:8083
make docker-down    # 停止两者；compose 的数据卷会保留
```

打开 <http://localhost:8083> 并创建第一个账号。这个栈为「快速试一个构建产物」而生——端口对外发布、开发密码硬编码——并不是为生产环境准备的。细节与注意事项见 [docs/local-trial_zh.md](docs/local-trial_zh.md)。

## 截图

**元数据浏览** —— 一个已同步数据库的表清单，附带行数、大小、列数与索引。

![元数据浏览：已同步数据库的表清单，含行数、大小、列数与索引](docs/images/metaxisdata_metadata_small.png)

**表级与列级血缘** —— 某张表的上下游血缘边，已展开到逐列的字段轨迹。

![血缘图：某张表的上下游血缘边，已展开到列级字段轨迹](docs/images/metaxisdata_lineage_small.png)

## 工作原理

- 服务端是一个 Go 二进制，在同一端口（8083）上提供 ConnectRPC API 和前端——一个内嵌进镜像的 Vue 3 单页应用。启用 MCP 时，MCP 端点也在同一端口上。
- PostgreSQL 是唯一的外部依赖。schema 在启动时自动迁移，因此一个全新的数据库只需要一个连接串。
- 后台 runner 负责同步 schema、分析并校验血缘，以及执行维护任务。

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

不带内嵌 tag 的构建只会用占位页面顶替单页应用：要么运行 Vite 开发服务器，要么用 `make build-embed` 构建。

`go test ./...` 不需要数据库。集成测试套件在 `integration` build tag 下用真实服务端跑在真实的 PostgreSQL 和 MySQL 上；Docker 不可用时会自动跳过：

```bash
make test-integration-smoke
make test-integration
```

开发规范、lint 规则和完整命令集见 [AGENTS.md](AGENTS.md)。

## 技术栈

- **后端**：Go、PostgreSQL、ConnectRPC（gRPC/HTTP）、Protobuf / buf
- **前端**：Vue 3、TypeScript、Vite、Tailwind CSS、shadcn-vue
- **命令行**：Go