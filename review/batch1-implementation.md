# 架构评审第一批落地记录

> 基线：`ba80a0e`（main，2026-10-31）· 分支：stack `arch-review/batch1-*`（8 个堆叠 PR）· 每项修复一个独立提交。

## 结论

第一批 12 项中，**11 项完成**；仅第 3 项（proto CI 门禁）按用户决定不做——原因只在于 CI 环境的 buf 授权，本机 `buf generate` 可正常执行，因此 BC-8、SEC-6 的 proto 变更与代码生成已在本机完成并提交。

| # | 事项 | 条目 | 状态 | 提交 |
| --- | --- | --- | --- | --- |
| 1 | 主 API 监听器 HTTP 超时 | SEC-2 | 完成 | `1f15cf6`、`40aa6e5`（保留 1 MiB header 上限） |
| 2 | qualityrunner 调度 worker panic 边界 | DP-1 | 完成 | `d9ef596` |
| 3 | proto CI 门禁（buf format/lint/generate-diff/breaking） | API-2 | **暂缓（用户决定，buf 授权）** | — |
| 4 | 集成测试按包全量跑 + `-race` | SEC-3 | 完成 | `a78b01a` |
| 5 | 分页默认值与 proto 文档对齐 | API-3 / BC-8 | 完成（BC-8 在补做后完成） | `8e5b850`、`8d680d4` |
| 6 | JSONB key 与 MCP tool schema 对账测试 | API-1 / API-7 | 完成 | `54b3e35`、`c92e3db`（lint 修正） |
| 7 | 重复实例 ID 映射 AlreadyExists + LLM 写路径审计 | BC-10 / SEC-6 | 完成（SEC-6 在补做后完成） | `3527f40`、`add205c` |
| 8 | MetadataBrowserPage 迁到共享层级 composable | FD1 | 完成 | `fe131e2` |
| 9 | store `Queryer` + 21 处只读事务 | BC-1 前半 / BC-3 | 完成（20 处降级，`manual_sql` 保留并升级隔离级别） | `918e16d` |
| 10 | 三处读放大 | BC-5/6/7 | 完成 | `1a3e613` |
| 11 | 失败执行记录实际 revision | DP-3 | 完成 | `97cc084` |
| 12 | 授权守卫测试 + last-admin 权限判定 | SEC-1(B) | 完成 | `28f83db`、`c78029a`（lint 修正） |

## PR 栈（自底向上）

| 层 | 分支 | 内容 | 提交 |
| --- | --- | --- | --- |
| 1 | `arch-review/batch1-reliability` | 服务端超时、runner panic 边界、集成测试 race、失败执行 revision | `1f15cf6`…`97cc084` |
| 2 | `arch-review/batch1-api-contract` | 分页默认值、重复实例 ID、JSONB/MCP 契约测试 | `8e5b850`…`3527f40` |
| 3 | `arch-review/batch1-store-reads` | `Queryer` 只读事务降级、三处读放大 | `918e16d`、`1a3e613` |
| 4 | `arch-review/batch1-list-pagination` | 五个 List RPC 补分页（proto 变更） | `8d680d4` |
| 5 | `arch-review/batch1-audit-coverage` | LLM provider 写路径审计（proto 变更） | `add205c` |
| 6 | `arch-review/batch1-authorization` | 授权守卫测试、last-admin 权限判定 | `28f83db`、`c78029a` |
| 7 | `arch-review/batch1-metadata-browser` | 元数据浏览器迁到共享 composable | `fe131e2` |
| 8 | `arch-review/batch1-tracking` | 本记录 | 本提交 |

## 暂缓项（本次不做）

- **第 3 项：proto CI 门禁（API-2）——用户决定暂缓，原因是 CI 环境的 buf 授权。**
  本机 `buf generate` 可正常执行（`buf format -w`、`buf lint`、`buf generate` 均通过且生成物与提交一致），只是不在 CI 中新增需要授权的 buf job。BC-8 与 SEC-6 所需的 proto 变更、代码生成和前后端生成物已全部在本机完成，随对应提交一起入库。
- 恢复 CI buf 授权后可直接补上 API-2，验收方式见评审报告（在 `review-architecture-2026-10` 分支，不随本 stack 提交）第一批表格。

## 实施说明与偏差

- **API-3**：`pageSize` 增加显式 `defaultLimit`，由共享分页器在请求未给页大小时使用；按 proto 注释写入各 List（DQ 50、suggestion jobs 20、notifications/connected apps/search 50、databases/instances/users 10），并把 Connected apps、SearchMetadata、lineage、DQ runs/jobs/targets 里手写的默认值收敛到同一机制。proto 注释未改动（无需重新生成）。
- **BC-1 前半 / BC-3**：新增 `store.Queryer`（`*sql.DB` 与 `*sql.Tx` 的共同读接口），把 15 个读 helper 的 `*sql.Tx` 参数泛化；20 个单语句读从 `BEGIN`/`COMMIT` 降级为池直读。唯一的多语句读 `ListManualSQL` 保留事务，并显式 `Isolation: sql.LevelRepeatableRead`（原 READ COMMITTED 下没有快照一致性）。
- **FD1**：`useMetadataChildren` 扩展了两点：`metaTypes` 为空时保留响应里的全部群组；搜索在无类型过滤时走一次未限定请求。列表与搜索各用一个 composable 实例（搜索不再覆盖列表）。`MetadataTabNav` 的 props 类型从 protobuf `MetadataResponse_Metadata[]` 放宽为 `{ metaType }[]`，`MetadataList` 继续接收 `StoredMetadata[]`。页面中的分页/搜索请求现在由 composable 统一做 ticket + AbortController。
- **SEC-1(B)**：新增 `TestPublicMethodsThatAlsoDeclareAPermission`，把"`allow_without_credential` 与 `permission` 并存"的两个方法固定成白名单；last-admin 判定改为解析绑定角色的 `iam.setPolicy` 权限（与 `CheckPermission` 同源），不再比较 `roles/workspaceAdmin` 字符串（关闭 D16）。
- **BC-5**：OpenLineage 过滤菜单的每个 UNION 分支先 `ORDER BY count DESC LIMIT 200` 再合并；数据集维度同样处理。未做"摄取时维护计数行"（更大改动），总量 `count(*)` 仍是单次扫描。
- **BC-7**：`FindMetaRegistryResourceMessage` 新增 `ObjectTypes` 批量条件（语义与 `GUIDs` 一致，空切片匹配零行）；`rebuildDatabaseObjects` 从固定 10 次查询变为一次。
- **BC-8**：五个原本无分页的 List RPC（IdP、IdP binding、application API key、OpenLineage API key、namespace mapping）补 `page_size`/`page_token`/`next_page_token`，共享分页器默认 50、上限 1000；store 查询加 `LIMIT/OFFSET`。OpenLineage filter-options 菜单保持无分页（它是菜单，不是集合浏览，review 也把这一点列为语义可接受）。
- **SEC-6**：LLM provider profile 的 Create/Update/Delete 补 `option (metaxisdata.v1.audit) = true`，并新增 `TestMutatingMethodsAreAudited` 守卫：遍历 proto registry，凡是 `Create/Update/Delete/Set/Revoke/Batch` 开头的 RPC 都必须审计或进入带原因的 allowlist（当前 4 条：SSO state、BatchGetUsers、通知的标记/删除）。审计脱敏按 `api_key`/`apikey` 子串生效。
- **SEC-2 的 header 上限**：评审建议 `MaxHeaderBytes = 16 KiB`，但 `TestDeviceLoginStoresABoundedUserAgentRealServerIntegration` 明确要求 512 KiB 的 User-Agent 能被接收并在审计层截断，因此保留 Go 默认的 1 MiB（显式写出并注释）。慢连接由 `ReadHeaderTimeout`/`ReadTimeout`/`IdleTimeout` 约束，header 上限不是这条防线。

## 验证

- 栈内逐层验证：每层分支上 `go build ./...` 与受影响包测试通过；栈顶内容与单分支版本一致（仅本文件的提交哈希更新）。
- Proto：`cd proto && buf format -w && buf lint && buf generate` 通过，生成物（Go、proto-es、grpc-doc/openapi）与 proto 同步提交；BC-8、SEC-6 各做一次生成。
- Go：`gofmt`；`go build ./...`；`go test -race -count=1 ./...` 全绿；`make build-release` 成功；
  `go test -tags=integration ./backend/test/integration/runner`（整包，BC-8 后与 SEC-6 后各跑一次）与 `./backend/migrator/...` 全绿。
- 前端：proto-es 重新生成后 `vue-tsc --build --force` 通过（BC-8 后）。
- 前端（第一批改动时）：`vue-tsc --build --force`、`biome ci`、ESLint、i18n 门禁、`vitest run --coverage`（132 个文件 / 1320 条用例，共享层 per-file 阈值通过）、`vite build` 全绿。
- `golangci-lint`（本机 v2.14.0）仅报既有 `multiline-if-init`（`backend/plugin/db/mysql|mssql/sync.go`，未改动；CI 固定 v2.13.1）；改动文件无新增告警。
- 说明：`make test-integration` 的 `-race` 与全量选测已改到 Makefile，CI 将在下次运行时执行；本机已直接运行整包集成套件（无 `-race`）验证。
