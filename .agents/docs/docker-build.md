# 容器镜像构建（Docker）— Reference

> Status：**implemented**（本分支）。维护参考，对应 `scripts/docker/Dockerfile.server`、`scripts/docker/metaxisdata-entrypoint.sh`、`scripts/build_init.sh`、`scripts/build_docker_common.sh`、`scripts/build_metaxisdata_docker.sh`、`.dockerignore`、`docker-compose.yml`。
> Related：`docs/deploy.md`（面向运维的构建与运行说明）、`docs/local-trial.md`（compose 本地试用）、`backend/common/version`（构建元数据）、`docs/security-posture.md`（部署侧已知取舍）。

## 这是什么

一个镜像：**服务端二进制（release/prod profile）+ 编译进去的 SPA**，运行期只依赖 PostgreSQL。laelia 的镜像是 manager / machine / machine-runtime / provisioner 四个角色各一个，本项目只有一个可部署服务，所以只需要一个 Dockerfile、一个构建脚本，没有嵌入式交叉编译产物、没有 pi 下载、没有 agent 运行时的 node/python 环境。

构建分三段：`frontend`（node 构建 SPA）→ `server-build`（golang 编译带 `embed_frontend` 的二进制）→ `server`（alpine 运行镜像，非 root，无数据卷）。`docker-compose.yml` 只是把 PostgreSQL 和这个镜像拼起来的本地试用环境，不是生产拓扑。

## 决策

| 决策 | 理由 |
| --- | --- |
| 镜像默认 `release`（prod profile），`--dev` 才用 dev | dev profile 的唯一增量是 CORS 白名单多了两个 Vite 开发源（`defaultDevCORSOrigins`，宽 CORS 早已在 `976ebc5` 移除），但部署镜像应当跑被测过的那套 profile，所以 prod 是默认、dev 需显式开启。laelia 的 manager 脚本默认 dev，是有意与本项目不同的一处。 |
| 版本元数据用 `-ldflags -X backend/common/version.{Version,GitCommit,BuildTime}` 注入 | laelia 每个服务一个 `version` 包，本项目只有一个服务，一个共享包就够；`mxd` 不能导入 `backend/`（depguard），所以 CLI 不带这些信息。 |
| 代理走自定义构建参数 `BUILD_PROXY`，不导出全局 `HTTPS_PROXY` | BuildKit 会把标准 `http_proxy` 等参数自动注入**每个** stage，包括最终运行镜像；自定义参数只有构建 stage 声明。npm 走 `NPM_REGISTRY`，Go 走 `GOPROXY`。三者都按 `${ARG:-上游默认}` 取值，所以 compose 可以无条件转发 `${GOPROXY:-}` 这类空值而不必知道默认值。 |
| compose 的 `make docker-up` 目标从本机取 `GOPROXY`/`NPM_REGISTRY` 与 `GIT_COMMIT`/`BUILD_TIME` 传给构建 | compose 自己读不到 git/`go env`；不接管的话，`docker compose up --build` 在需要镜像源的网络上会失败，镜像也会自称 `unknown` 构建。 |
| pnpm 固定 `10.24.0` | `frontend/package.json` 的 `packageManager` 与 CI 都用它，`pnpm-lock.yaml` 也是它写的。 |
| 只有 release 构建打 `:latest` | `:latest` 应当指向“部署预期运行的配置”，本地实验不是。注意 `:dev` 同时是 `make docker-build`（release）和 `make docker-build-dev`（dev profile）的标签，所以 compose 永远用 `--build`（`make docker-up` 已如此），避免拿 dev 构建当 release 跑。 |
| 运行镜像用 alpine + `ca-certificates` + `tzdata`，uid 1001，无卷 | 二进制 `CGO_ENABLED=0`，状态全在 PostgreSQL；TLS 只用于出站（源库、LLM、IdP）。`tzdata` 是为源库配置：MSSQL 实例可以带 `timezone` DSN 参数，`go-mssqldb` 用 `time.LoadLocation` 解析，没有 zoneinfo 就会报 `unknown time zone`，而这个配置在宿主机上能跑。`/tmp` 是唯一可写路径（MSSQL driver 的临时文件），要加固就用 `--read-only --tmpfs /tmp`，裸 `--read-only` 会把它也拿掉。 |
| `IMAGE` 默认 `metaxisdata/metaxisdata`，`IMAGE_ALT` 默认 `ghcr.io/ranxy/metaxisdata` | 两个名字同时打标签；`IMAGE_ALT=` 置空即只打一个。 |
| 多平台用「构建阶段固定 `$BUILDPLATFORM` + `GOOS/GOARCH` 交叉编译」，只有运行阶段的 `apk add` 走 QEMU | 两个构建阶段的产物本来就与宿主架构无关（SPA 是 JS，CGO 关闭）；如果在 arm64 上模拟跑 `vue-tsc`，一次构建要几十分钟。 |
| 只在 GitHub Release `published` 时推 GHCR；别名（`:latest`、`:1.2`）只由非预发布产生 | 镜像与 Release 一一对应；跟踪别名的部署不会被塞进 RC。手动 `workflow_dispatch` 在分支上只推 `:sha-<commit>`（分支名不是版本），在 tag 上重推该 tag 的集合、同样不动别名。 |

## 不变量

| 规则 | 破坏后果 |
| --- | --- |
| Dockerfile 里 `-X` 的包路径必须与 `backend/common/version` 一致，且与 `Makefile` 的 `LDFLAGS` 一致 | 镜像里的 `--version`、`/api/version`、前端“关于”卡片会显示 `dev`/`unknown`，无法定位到底跑的是什么构建。 |
| `.dockerignore` 必须排除 `frontend/node_modules`、`**/dist`、`backend/server/frontend_dist`，且 Dockerfile 在拷贝新 SPA 前仍 `rm -rf backend/server/frontend_dist` | 构建机上残留的旧 SPA 会被编进镜像，页面看起来"没更新"。 |
| 先有 SPA 再编译，且带 `embed_frontend` 标签 | 否则 `server_frontend_not_embed.go` 的占位页会被部署出去。 |
| 别名 tag（`:latest`、`:1.2`）的产出必须经 `flavor: latest=false`，不能只靠 `type=raw` 的 `enable` | `docker/metadata-action` 按**优先级降序**处理标签（semver 900、ref 600、raw 200、sha 100，见 `tag.ts` 的 `DefaultPriorities`），`version.latest` 由**第一个**产出值的条目定下且不再改（`setVersion()`）。默认 `flavor.latest=auto` 时：semver tag 由 semver 自己的预发布规则决定（正确），但 semver 解析不了的 tag（如 `rc-2026`）会把决定权交给 ref 条目，它直接置 `latest=true`——**实测该情形下预发布会推 `:latest`**；稳定发布还会把 `latest` 输出两次（semver 一次 + raw 落进 partial 一次）。`flavor: latest=false` 掐掉所有隐式来源，`:latest` 只由带 `enable` 的 raw 条目产生，与顺序无关。 |
| 入口脚本派生的 flag 必须排在调用方参数之前（`set -- --flag value "$@"`） | 同名 flag 后者生效；顺序反了，`docker run … --port 9090` 会被派生值静默覆盖。 |
| 构建阶段必须留在 `--platform=$BUILDPLATFORM` 上，`GOOS/GOARCH` 只由 `TARGETOS/TARGETARCH` 决定 | 把构建阶段放到目标平台上，arm64 构建会退回 QEMU 模拟（CI 从几分钟变成几十分钟）。 |
| workflow 里的 OCI labels 只由 Dockerfile 的构建参数产生，不叠加 `docker/metadata-action` 的 `labels` | 两处各写一份 `org.opencontainers.image.version`，标签会和 `metaxisdata --version` 说不同的版本号。 |
| CI 的 `images:` 必须小写（`${GITHUB_REPOSITORY,,}`） | GHCR 拒绝大写 owner 路径；本仓库 module path 里拼作 `Ranxy`。 |
| dev 镜像不得打 `:latest` | 部署方 `docker pull …:latest` 会拿到本地实验构建；dev profile 的实际差异不大（只多两个开发源），但标签语义会被破坏。 |
| 运行镜像不得继承构建期的代理设置 | 代理凭据随镜像发布出去。`BUILD_PROXY`/`GOPROXY`/`NPM_REGISTRY` 都只在构建 stage 声明，实测不会进运行镜像；**例外是 `APK_MIRROR`**——它被运行 stage 自己消费，会原样写进镜像的 `/etc/apk/repositories` 并出现在 `docker history` 里，所以镜像源的 URL 里不能带凭据。 |
| `.dockerignore` 必须排除机器本地的前端覆盖文件（`**/.env*.local`、`**/*.tsbuildinfo`） | `frontend/.gitignore` 正好忽略这两类，所以开发机上通常存在；Vite 在 production 模式下也会最后加载 `.env.production.local`，一旦被 `COPY frontend/` 带进构建，镜像里的 SPA 会把 API 指向开发者的 `localhost`。 |

## 失败场景

| 场景 | 行为 |
| --- | --- |
| 没设 `PG_URL` | 进程打印 `must set PG_URL environment variable` 后从 `start()` 返回，日志走完就 **`Exited (0)`**（`root.go` 不调 `os.Exit`），容器不会进入 unhealthy——healthcheck 对已退出的容器根本不运行，所以编排侧要靠 `restart:` 或退出码判断。 |
| 数据库不可达 | 启动即失败，schema 迁移在 `server.NewServer` 里做，迁移失败同样退出。 |
| 前端请求 `/api/version` 失败（老镜像没有该路由、代理只转发 `/v1`） | `useBuildInfo` 返回 `failed` 状态、不抛错：用户菜单整块不渲染，设置→通用里那行显示 `—` 而不是一直「Loading…」。 |
| 构建机 `pnpm` 版本 ≠ 10.24.0 时执行 `pnpm --dir frontend …` | 被 `packageManager` 校验拒绝（本机 pnpm 11 shim 的现象）；`cd frontend && pnpm …` 会自动切到 10.24.0。这与镜像构建无关，镜像里用的是固定版本。 |
| `APK_MIRROR` 设成了非 alpine 镜像站 | 只替换 `https://dl-cdn.alpinelinux.org/alpine` 前缀，`apk add` 失败即构建失败。 |
| 同时设 `METAXISDATA_PORT` 又显式传 `--port` | 入口脚本的派生值是默认，调用方参数在后、pflag 后者生效，于是服务监听 `--port`、healthcheck 探 `METAXISDATA_PORT`，容器一直 unhealthy 但服务正常。只改端口就用 `METAXISDATA_PORT`。 |

## 代码与测试位置

- 镜像与脚本：`scripts/docker/Dockerfile.server`、`scripts/docker/metaxisdata-entrypoint.sh`、`scripts/build_init.sh`、`scripts/build_docker_common.sh`、`scripts/build_metaxisdata_docker.sh`、`.dockerignore`、`docker-compose.yml`、`Makefile`（`docker-build` / `docker-build-dev` / `docker-up` / `docker-down`）。
- 发布工作流：`.github/workflows/release-image.yml`（`release: published` → buildx 多平台 → GHCR；`VERSION` 取 release tag，`GIT_COMMIT` 取 `github.sha`，`BUILD_TIME` 取运行时的 UTC 时间）。本地要复现多平台构建：`docker buildx build --platform linux/amd64,linux/arm64 --output type=oci,dest=/tmp/x.tar -f scripts/docker/Dockerfile.server .`。
- 构建元数据：`backend/common/version/version.go`；注入点 `Makefile` 的 `LDFLAGS` 与 Dockerfile 的 `server-build` 段；出口 `backend/bin/server/cmd/root.go`（`--version`，经 cobra 的 `SetVersionTemplate`）与 `backend/server/echo_routes.go`（`GET /api/version`，测试在 `backend/server/echo_routes_test.go`）。
- 前端：`frontend/src/api/version.ts`（`/api/version` 不是 ConnectRPC，所以这里是唯一的普通 `fetch`，见 `frontend/AGENTS.md` 的 API 分层）、`frontend/src/composables/useBuildInfo.ts`（测试 `useBuildInfo.test.ts`，属受覆盖率门禁的 composable 层）、`frontend/src/components/layout/UserMenu.vue`（测试 `UserMenu.test.ts` 断言下拉菜单真的渲染出版本行，以及请求失败时什么都不渲染）、`frontend/src/pages/settings/GeneralSettingsPage.vue`、`frontend/vite.config.ts`（dev server 代理 `/api`）、`frontend/src/locales/{en-US,zh-CN}.json`。
- 门禁：Go 侧 `gofmt`、`golangci-lint run --allow-parallel-runners`、`go test ./...`、`go build`；前端按 `frontend/AGENTS.md` 的顺序；镜像本身用 `scripts/build_metaxisdata_docker.sh` 构建后起 compose 验证 `/healthz`、`/api/version` 与 SPA。

## 未做项

- 没有镜像签名（cosign）与 SBOM：buildx 的 provenance 证明默认开启，但没有额外的供应链产物。
- 没有 `mxd` 镜像：CLI 是单独的产物（`make build-cli`），agent 场景一般直接把二进制装进环境。
- 没有 Kubernetes chart / 部署清单，`docker-compose.yml` 只服务本地试用。
- 本地构建脚本仍是单平台（本机架构）；多平台只在 CI 里做，本地要验证得直接用 `docker buildx`。
