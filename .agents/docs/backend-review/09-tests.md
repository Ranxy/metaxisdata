# 09 · 测试与测试基础设施

**范围**：`backend/test/integration/`（`env/service_env.go`、`env/testenv.go`、`runner/*`）、全部 `*_test.go`（hermetic 与 integration）、`Makefile`、`.github/workflows/`。

**结论**：AGENTS.md 关于"`go test ./...` 是 hermetic 的"这一点成立（本环境实测 exit 0、无需 Docker/DB）。但 CI **从不运行** hermetic 测试；集成测试在缺少 Docker 时不是 skip 而是 `os.Exit(1)`；存在约 500 行死测试基础设施；AGENTS.md 要求的"查询形状 guard 测试"在两处内联 GUID 谓词和数据库范围谓词上缺失；`backend/api/auth` 零测试文件。

**阶段 0 更新**：新增 2 个测试文件（`filter_injection_test.go`、扩展 `audit_test.go`），T-H5 ◐（脱敏谓词已有表驱动测试）；T-C1/T-C2/T-H1/T-H3/T-H4 与 CI 相关条目**未处理**。另：审查时无法运行的 `golangci-lint` 在阶段 0 复测中已可运行（`0 issues`），"lint 清洁度未验证"这一限制已解除。

**阶段 1 更新**：新增 2 个测试文件（`filter_type_safety_test.go`、`instance_data_source_test.go`）并扩展 2 个（`filter_injection_test.go`、`syncer_test.go`），共 8 个新测试见下表。T-C1（CI 不跑 hermetic 测试）仍未修——`make build-release` 只是构建目标，仓库里依然没有 CI job 或 Dockerfile。T-H3 的 store 侧 guard 仍缺失。

**阶段 2 更新**：新增 4 个测试文件（`store/principal_test.go`、`store/db_connection_test.go`、`api/v1/common_test.go`、`component/llm/agent_test.go`）并扩展 2 个（`store/meta_resource_test.go`、`plugin/openlineage/resolver_test.go`），共 15 个新测试函数见下表；首次为 `component/llm`、`store/db_connection.go` 建立测试。T-C1/T-H3、`api/auth` 零测试等仍未处理。
**阶段 3 更新**：T-C1 ◐（新增 `.github/workflows/ci.yml`：`go test -race -count=1 ./...` 与 `golangci-lint` v2.13.1，`d3d96c1`；**经确认本轮只做 Go 单测与 lint**，前端 job、`./backend/migrator/...` 并入集成 target 推迟）、T-H2 ✅（删除第二套死 harness 与 `skipIfDockerUnavailable`，并在 `.golangci.yaml` 加 `run.build-tags: [integration]`，让集成文件首次进入 lint——正是这一步暴露出 `main_test.go` 的 6 个 revive 问题，已修，`b9a48a3`）、T-H4 ✅（`api/auth` 从零建立测试：token 提取/签发/校验、方法注解、cookie、gateway modifier，`0dae0b7`）。**T-H3 仍未处理**：store 侧 `listSublevelMetaRegistryResourceImpl`/`...HistoryImpl`/`listDatabaseImpl`（旧名 `listDatabaseImplV2`）的查询形状 guard 仍缺失（阶段 3 给这三处补了 offset 支持，但没有加 guard 测试）。**T-C2 未处理**（经确认推迟）：缺 Docker 时集成测试仍硬失败。**M6 部分**：CI 已有 race + lint，仍无覆盖率、无前端 job。

**阶段 3 收尾更新**：无新增测试文件（proto 表面收敛是删除性改动，`go test ./...` 全绿即为回归证据）；但 `go test -race -count=1 ./...` 全量跑时暴露出 `TestObfuscateRoundTrip` 的随机失败并已修复（`4afe1ba`）——这是本仓库第一例由 CI 的 unit 命令（而非新增测试）发现的测试缺陷。**新增待办**：`Engine` 收敛到 5 个值、`DataSource` 删除 30 余字段后，应当给 `convertToEngine`/`convertEngine`（现各 5 case）与 `mergeDataSource` 补表驱动 guard 测试，当前仍只有 `instance_data_source_test.go` 的字段保留/覆盖断言。

**阶段 3 续更新**：本机 Docker 可用，**首次真正跑了集成套件**（`go test -count=1 -tags=integration ./backend/test/integration/...`），除一个既有失败外全部通过。该失败是 `TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration`（`backend/test/integration/runner/schemasync_lineage_postgres_service_test.go:87`，`require.Eventually` 报 `Condition never satisfied`：等待被 DROP 的 VIEW 的 meta_registry 行与 column_lineage 行都消失），**与本轮无关**——在 `27d6261`（阶段 2 末尾，早于阶段 3 的删码）上同样稳定失败，在 `f7cfb0d`、`904fb09` 与当前工作树也都失败，需要单独一轮定位（怀疑 lineage analyzer 用陈旧 metadata 重新写回该 GUID 的行）。迁移验证用的是手工临时程序直连 PostgreSQL 16（跑完即删、未入库），不是永久测试；`./backend/migrator/...` 的 integration 测试（testcontainers）通过。

**阶段 3 补遗更新**：① 上一段那条「既有失败」的根因已定位并修复——**不是** lineage analyzer 回写，而是集成 harness 的 `inspectStore`（第二个 in-process store）缓存了 server 进程删除前的 VIEW 行，而缓存失效不跨进程传播；新增 `store.WithCacheDisabled()` 并让 harness 的两个 `inspectStore` 启用后，**集成套件全部通过**（`runner` 48.56s、`migrator` 12.83s，exit 0；该用例单测复跑 11.47s PASS，两个邻居用例同跑 PASS，`f50fbbc`）。② 修掉 `backend/server` 测试的 `-race` 竞态：dev/prod 两个 `sync.Once` 在并行测试下同时调用 echo-contrib 的 `registerMetrics`，约 1/5 概率失败；改为一个 Once 顺序构建两个 server，连续 8 次 `-race` 全绿（`a16c8d4`）。③ 新增 `backend/common/guid_test.go`、`backend/common/cel_test.go`（含「未绑定变量必须 fail closed」用例）与 `backend/api/v1/pagination_test.go` 的血缘分页用例。**仍未做**：T-C2（缺 Docker 时 skip 而非硬失败）、T-H3（store 三处查询形状 guard）、`api/v1` 各 handler 与 `debug_interceptor` 测试、`backend/server` 启停路径测试、前端 Vitest 与 CI 前端 job、`./backend/migrator/...` 并入集成 target。

**阶段 3 收尾二更新**：本节剩下的欠账本轮全部收口。**T-C2 ✅** + **M7 ✅**（`061208c`）：新增 `backend/test/integration/dockerutil`（用 testcontainers provider 探测容器运行时），无 Docker 时两个套件都打印跳过信息并 exit 0；`TestMain` 的 setup panic 被 recover 并上报，不再让收集端等到 workflow 超时。**M2/M3/M9 ✅**（同 commit）：`ValidateIntegrationEnv` 拒绝部分外部服务配置、服务器启动失败换端口重试并在进程提前退出时立刻带日志失败、DSN 凭据可用 `INTEGRATION_*_USER/PASSWORD` 覆盖且只 DROP 自己派生的 `*_integration` 库。**M1 ✅**（`48d65be`）：migrator 集成测试的 `DROP DATABASE` 从已关闭的管理池上跑、错误被丢弃，改为管理池活到 DROP 之后且失败即测试失败。**T-H1 ✅ / M10 ✅**（`8823ac2`）：补上 `make test-integration-mysql`，`test-integration`/`test-integration-smoke` 纳入 `./backend/migrator/...`，迁移三条路径进入 CI。**T-H3 ✅**（`df97e0a` `f65baaa`）：两处内联 GUID 子树谓词与数据库列表范围谓词改成纯构造函数并补形状 guard（含"占位符编号 = 参数长度"）。**M8 ✅**（`711c0aa`）：`patchIamPolicyBindings`（顺带确定化顺序）与 manual SQL 四个 helper、`generateEtag` 补测试。**T-H5 剩余 ✅ / handler 与拦截器缺口 ✅**（`c162bc0`）：审计 helper 全套表驱动测试（含"已认证用户优先于请求字段"）、`debug_interceptor` 的 `[TRUNCATED]` 截断、`backend/server` 真实启停路径、`component/llm` 的 fetcher/message/tools，并新增真实 server 的未认证反向集成测试与 `DataSource` 生命周期集成测试（`513940f`）。**M6 ✅**（`65f4eaa`）：CI 新增 frontend job（`biome ci` + `lint:ci` + `vue-tsc` + `vitest run` + 生产构建）与 Go 单测 `-cover`；前端首次有 12 个 Vitest 用例（`frontend/src/utils/error.test.ts`、`frontend/src/api/lineage.test.ts`）。**仍未做**：CI workflow 从未在 GitHub 实跑、前端覆盖率（需要新增 `@vitest/coverage-v8`）、M5（harness 与 runner 的 fixture DDL 重复）与低优先项（`waitForHTTPReady` 把 5xx 当 ready、`SELECT 1;` 空断言、部分测试缺 `t.Parallel()`/风格不一致）。


**阶段 3 续更正**：① T-H1 的“从不执行”在本地手动跑过一次（`./backend/migrator/...` 通过），但 `Makefile`/CI 仍未包含它，结论不变；② store 侧 impl helper 的 V2 后缀已随 `8b328ae` 去掉（`listDatabaseImplV2` → `listDatabaseImpl`），T-H3 的 store 侧 guard 仍缺失这一结论不变。

**阶段 5 更新**：测试随凭证加密回滚调整（`7870016`）——`common/utils_test.go` 删除 `TestObfuscateRoundTrip`/`TestObfuscateUsesAFreshNonce`/`TestObfuscateEmptyInputStaysEmpty`/`TestObfuscateFailsClosed`（保留 `TestOpenLineageResourceNames`），`store/setting_test.go` 从「密钥优先级/短 key 拒绝」改写为「已解析 secret 走缓存、不查库」的 hermetic guard。阶段 3 收尾提到的 `TestObfuscateRoundTrip`（`4afe1ba`）已不存在。前端新增 OpenLineage 保留天数输入，`vitest` 仍为既有用例；`go test ./...` 除既有失败 `TestMarshalRolePermissionsIsDeterministic` 外全绿。

---

## 严重（Critical）

### T-C1. CI 从不运行 hermetic 单测
- **位置**：`.github/workflows/mysql-integration.yml:75-76`、`Makefile:6,9`
- **证据**：唯一 workflow 的最后一步是 `run: make test-integration`，而 `Makefile:9` 是 `go test -v -count=1 -tags=integration -run 'RealServerIntegration' ./backend/test/integration/runner`。仓库中没有任何地方运行 `go test ./...`。
- **影响**：store guard 测试、migrator 测试、api/v1 helper 测试、runner 测试都不构成 PR 门禁；非 integration 包里的编译错误也可能绿灯合入。
- **修复**：新增 `unit` job，在每个 PR 上跑 `go test -race -count=1 ./...`（paths: `backend/**`、`go.mod`、`go.sum`），integration 单独保留。

### T-C2. 缺 Docker 时集成测试硬失败而非 skip
> **✅ 已修复（阶段 3 收尾二）** · `061208c`：新增 `backend/test/integration/dockerutil`（`Available`/`IsUnavailable`/`WrapUnavailable`），runner 的 `TestMain` 在没有外部服务且探测失败时打印 `skipping integration tests: docker is unavailable` 并 exit 0；migrator 集成测试同样复用该探测。容器启动失败若被判定为"运行时不可达"也会转成同一个 sentinel，避免中途 Docker 掉线时误报产品失败。
- **位置**：`backend/test/integration/env/service_env.go:765-767,844-846`、`runner/main_test.go:73-88`
- **证据**：`startPostgresForEnv`/`startMySQLForEnv` 出错直接 `return err`，`TestMain` 随后打印并 `os.Exit(1)`。AGENTS.md 写的是"requires a working Docker daemon and skips when Docker is unavailable"。
- **影响**：`make test-integration`/`make test-integration-smoke` 在没有 Docker 的开发机和 CI runner 上直接失败。唯一的 Docker-skip 逻辑在死代码 `testenv.go:451 skipIfDockerUnavailable` 里。
- **修复**：在 `*ForEnv` 路径集中做 Docker 可用性检查，或返回 `ErrDockerUnavailable` 让 `TestMain` 映射为 exit 0/skip；字符串匹配要覆盖 socket 缺失、权限拒绝、podman。

---

## 高（High）

### T-H1. migrator 集成测试被孤立，从不执行
> **✅ 已修复（阶段 3 收尾二）** · `8823ac2`：`make test-integration` 与 `test-integration-smoke` 都包含 `./backend/migrator/...`（CI 调用的就是 `make test-integration`），本地实测 `runner` 49.48s + `migrator` 11.29s 通过。

- **位置**：`backend/migrator/migrator_integration_test.go`（`//go:build integration`）、`Makefile:6,9`
- **证据**：`test-integration-smoke` 跑 `./backend/test/integration/...`，`test-integration`/CI 跑 `./backend/test/integration/runner`，都不含 `./backend/migrator`。
- **影响**：全新安装、升级、legacy adoption 三条迁移路径在 CI 中完全无验证，迁移回归只能到生产才暴露。
- **修复**：把 `./backend/migrator/...` 加入 smoke target 与 CI，或把文件移到 `backend/test/integration/` 下。

### T-H2. 约 500 行死测试基础设施（其中包含唯一的 Docker-skip 逻辑）
- **位置**：`testenv.go:67 SetupMySQLEnv`、`service_env.go:160 SetupMySQLServiceEnv`、`:179 SetupPostgresServiceEnv`、`runner/main_test.go:108 sharedMySQLServiceEnv`、`:134 sharedPostgresServiceEnv`、mysql test `:274 hasDetailedEdge`
- **证据**：grep 只有定义、无调用者；`resetMySQLSchema`/`resetPostgresSchema` 只能通过这些死包装到达。
- **影响**：两套竞争性 harness 增加维护成本；死掉的 `SetupMySQLEnv` 路径恰恰是唯一会在缺 Docker 时 skip 的实现，说明 live 路径在替换 harness 时丢了这个行为。`hasDetailedEdge` 等 integration-tagged helper 也逃过 golangci-lint（配置没有 build-tags）。
- **修复**：删除 `SetupMySQLEnv`/`TestEnv` 接线与未用的 `Setup*ServiceEnv`/`shared*ServiceEnv`/`hasDetailedEdge`；在 `.golangci.yaml` 加 `build-tags: [integration]`。

### T-H3. 内联 GUID-subtree 谓词与数据库范围谓词没有 guard 测试
> **✅ 已修复（阶段 3 收尾二）** · `df97e0a` `f65baaa`：两处内联谓词改为调用/复用 `appendGUIDSubtreeCondition` 的纯构造函数 `buildSublevelMetaRegistryResourceQuery`，数据库列表范围谓词抽到 `buildListDatabaseQuery`；`backend/store/meta_resource_query_test.go` 与 `backend/store/database_test.go` 断言谓词形状、LIKE 元字符转义与占位符编号始终等于参数长度。

- **位置**：`backend/store/meta_resource.go:774,846`、`backend/store/database.go:360-398`
- **证据**：`appendGUIDSubtreeCondition`（`meta_resource.go:85`）有 `meta_resource_test.go:13` 覆盖，但同一谓词被复制粘贴到 `listSublevelMetaRegistryResourceImpl`（:774）和 `listSublevelMetaRegistryResourceHistoryImpl`（:846）却没有测试；`listDatabaseImplV2`（`database.go:360-398`）的 `ShowDeleted`/大小写/项目/环境/实例范围谓词也没有 guard。
- **影响**：AGENTS.md 明确要求"当查询形状本身就是不变量时"补 guard 测试，而恰好这类回归（丢掉 `ESCAPE`/`deleted = false` 谓词会静默扩大结果）在两处 GUID 站点和数据库列表查询上没有保护。
- **修复**：把内联谓词改为调用 `appendGUIDSubtreeCondition`；为 `listDatabaseImplV2` 加表驱动 guard 测试，断言生成的 SQL/args。

### T-H4. `backend/api/auth` 零测试
- **证据**：`go test ./...` 显示 `backend/api/auth [no test files]`。未测试项：`GetTokenFromMetadata`（`auth.go:198`）、`GetTokenFromHeaders`（:221）、`audienceContains`（:247）、`GenerateAPIToken`/`GenerateAccessToken`/`generateToken`（:262-299）、`getAuthContext`（:301）、`IsAuthenticationAllowed`（`config.go:10`）以及两条拦截器路径（`WrapUnary:74`、`WrapStreamingHandler:108`）。
- **影响**：token 解析、audience/过期拒绝、免认证判定都是安全关键逻辑，完全无验证；也没有"无 token 应返回 `Unauthenticated`"的集成反向测试。
- **修复**：加表驱动 hermetic 测试（Bearer 大小写、畸形头、cookie 优先级、过期/错误 audience/错误 kid 拒绝、`IsAuthenticationAllowed`），并加一个集成反向测试。

### T-H5. 审计脱敏谓词与拦截器 helper 几乎无测试
> **◐ 部分修复（阶段 0）** · `89ef84a`：新增 `TestIsSensitiveAuditField`（表驱动，覆盖每个精确匹配标记）与 `TestMarshalAuditMessageRedactsSecrets`（`CreateAPIKeyResponse.key`、`DataSource.sslCert`/`sslKey`/`gcpCredential`）。**剩余**：`shouldSkipAudit`/`resolveParent`/`resolveResource`/`resolveActor`/`mapSeverity`/`buildAuditStatus`/`buildRequestMetadata`/`getServiceData` 仍未测试；大小写/空白归一与嵌套数组的覆盖仍偏薄。
> **✅ 阶段 3 收尾二（`c162bc0`）**：除 `getServiceData`（已随死代码删除）外全部补齐：`shouldSkipAudit` 的 `validate_only` 分支、`resolveParent`/`resolveResource` 的优先级、`resolveActor` 的"已认证用户优先于请求/响应字段"、`mapSeverity` 的客户端/服务端错误划分、`buildAuditStatus` 的三种形态、`buildRequestMetadata` 的 XFF/网关头/peer 地址/UA 回退与 `getNestedString` 的嵌套与非字符串分支。

- **位置**：`backend/api/v1/audit.go:197-208`
- **证据**：只有 `marshalAuditMessage` 与 `isNilConnectValue` 有测试（`audit_test.go`），覆盖 `password` 与 `idpContext`。`isSensitiveAuditField` 的完整标记列表以及 `shouldSkipAudit:146`、`resolveParent:210`、`resolveResource:222`、`resolveActor:238`、`mapSeverity:277`、`buildAuditStatus:293`、`buildRequestMetadata:304`、`getServiceData:326` 均未测试。
- **影响**：新增敏感字段名或改动标记列表可能把凭据写进 `audit_log` 而没有任何测试失败（这正是 `sslKey`/`content`/`key` 漏洞未被发现的原因）。
- **修复**：对 `isSensitiveAuditField` 做表驱动测试（每个标记、大小写/空白归一、嵌套数组），并覆盖 severity/status 映射与 IP/UA 回退。

---

## 阶段 0 新增的 guard 测试（已落地）

| 文件 | 测试 | 保护的不变量 |
| --- | --- | --- |
| `backend/api/v1/filter_injection_test.go`（新，`3321801`） | `TestFilterParsersDoNotSpliceLiterals` | user/instance/database-name/database-label 四条 CEL→SQL 路径收到注入载荷时，载荷不出现在生成的 `Where` 中，且值出现在 `Args` 里（原 database-table 用例已在阶段 1 随过滤器删除） |
| 同上 | `TestLikePatternEscapesWildcards` | `%`/`_`/`\` 先转义再包成 `%...%` |
| `backend/api/v1/audit_test.go`（扩展，`89ef84a`） | `TestMarshalAuditMessageRedactsSecrets` | `CreateAPIKeyResponse.key`、`DataSource` 的 `sslCert`/`sslKey`/`gcpCredential` 不得进入审计 payload |
| 同上 | `TestIsSensitiveAuditField` | 脱敏标记列表的精确匹配行为 |

> 注意：T-H3 指出的 **store 侧** guard（`listSublevelMetaRegistryResourceImpl`/`listSublevelMetaRegistryResourceHistoryImpl`/`listDatabaseImpl`（旧名 `listDatabaseImplV2`））仍缺失，新测试只覆盖 API 层 filter 翻译器。

## 阶段 1 新增的 guard 测试（已落地）

| 文件 | 测试 | 保护的不变量 |
| --- | --- | --- |
| `backend/api/v1/filter_type_safety_test.go`（新，`ff914ac`） | `TestFilterParsersRejectMistypedOperands` | 12 个畸形过滤器（`email == 123`、`engine in [1]`、`name.matches(ident)`、裸 `matches("x")`、`exclude_unassigned == "true"` 等）在 user/instance/database/audit 四个解析器上都返回 `InvalidArgument`，不 panic |
| `backend/api/v1/filter_injection_test.go`（扩展，`bb93ee0`） | `TestListDatabaseFilterRejectsTableFilter` | 被删除的 `table` 过滤器返回 `InvalidArgument`，而不是生成 join 不存在表的 SQL |
| `backend/api/v1/instance_data_source_test.go`（新，`20e284b`） | `TestMergeDataSourcePreservesUnreturnedFields` | 请求未携带的密码/SSL/SSH 私钥/`verify_tls_certificate`/`additional_addresses` 必须保留 store 中的值，且不改动原对象 |
| 同上 | `TestMergeDataSourceOverlaysProvidedValues` | 请求携带的字段（含新密码、地址列表）覆盖 store 值，重复字段不被追加 |
| 同上 | `TestMergeDataSourcesKeysByID` | 按 ID 合并；缺席 ID 被删除、未知 ID 原样加入 |
| 同上 | `TestCheckInstanceDataSourcesRequiresOneAdmin` | 数据源列表必须恰好一个 ADMIN，ID 唯一性仍校验 |
| `backend/runner/schemasync/syncer_test.go`（扩展，`fcb6a98`） | `TestShouldSyncNowRejectsNeverSyncInterval`、`TestShouldSyncNowRespectsInterval` | 停用实例（interval=0）永不被排队；正常间隔的到期判断正确 |

---

## 阶段 2 新增的 guard 测试（已落地）

| 文件 | 测试 | 保护的不变量 |
| --- | --- | --- |
| `backend/store/meta_resource_test.go`（扩展，`f22f61e`） | `TestMetaRegistryGUIDCacheKeyIncludesObjectType` | GUID 缓存 key 必须含 `object_type`；仅带 GUID 的查询不可缓存（否则 TABLE/VIEW 同名 GUID 串用） |
| `backend/store/principal_test.go`（新，`ff9b22a`） | `TestIsUniqueViolation` | 23505 在 `*pgconn.PgError`（真实驱动）与 `*pq.Error` 两种形态下都被识别，其他码与普通错误不被误判 |
| `backend/store/db_connection_test.go`（新，`fb8ca14`） | `TestClampMaxOpenConns` | 池上限落在 `[1, 50]`：`max_connections <= reserved` 或异常 `SHOW` 结果不得产出 0（会被解释为无上限） |
| `backend/api/v1/common_test.go`（新，`ff9b22a`） | `TestConnectErrorForWriteMapsConflictToAlreadyExists` | store 的 `common.Conflict` 映射为 `CodeAlreadyExists`，其他错误保持 `CodeInternal` |
| `backend/plugin/openlineage/resolver_test.go`（扩展，`8c34542`） | `TestRequestScopedResolverMemoizesPreview`、`TestNewResolverDoesNotMemoize` | 请求内解析缓存命中不触库；采集用的 `NewResolver` 不缓存 |
| `backend/component/llm/agent_test.go`（新，`8acbfe6`） | `TestParseStreamEmitsContentAndDone` | 正常 SSE 逐块产出 content + Done |
| 同上 | `TestParseStreamAcceptsDoneWithoutFinishReason` | `data: [DONE]` 视为正常结束（兼容不设 `finish_reason` 的 provider） |
| 同上 | `TestParseStreamKeepsToolCallsWithNonContiguousIndexes` | 非连续/乱序 tool-call index 不再丢调用，参数按块拼接 |
| 同上 | `TestParseStreamRejectsTruncatedAndIncompleteResponses` | `finish_reason=length`、无终止标记的 EOF、畸形 chunk、未知 finish_reason 都报错（不进缓存） |
| 同上 | `TestParseStreamPropagatesReadErrors`、`TestMaxBytesReaderFailsInsteadOfTruncating` | 超限是错误而非静默截断 |
| 同上 | `TestSendEventStopsOnCancelledContext` | 消费端消失时发送不再永久阻塞（C-H1 回归守卫） |
| 同上 | `TestBoundedBufferCapsDebugCopyOnly` | 调试日志副本有上限，且 `Write` 返回完整长度（否则 `TeeReader` 会把短写当错误） |
| 同上 | `TestIdleTimeoutReaderCancelsAStalledStream` | 空闲读超时会取消请求 |

---

## 中（Medium）

- **M1. migrator 集成测试清理是静默空操作**（**✅ 阶段 3 收尾二**：`48d65be`，管理池活到 DROP 之后且失败即测试失败）：`migrator_integration_test.go:151,156-158`，`defer admin.Close()` 在 `newTestDatabase` 返回时执行，而 `t.Cleanup` 的 `DROP DATABASE` 在测试结束时对已关闭的连接池执行，错误被 `_, _ =` 丢弃。
- **M2. 部分外部服务 env 会静默混用模式**（**✅ 阶段 3 收尾二**：`061208c`，`ValidateIntegrationEnv` 直接拒绝）：`service_env.go:729-748,819-829` 只按引擎检查各自变量；只设 MySQL 变量会让 PostgreSQL 仍走 testcontainers。README:93 与 AGENTS.md 说的是"partial env config fails fast"。
- **M3. `reservePort` 存在 TOCTOU**（**✅ 阶段 3 收尾二**：`061208c`，readiness 失败换端口重试最多 3 次，进程提前退出立刻失败）：`service_env.go:704-711`，读取端口后关闭 listener，服务器稍后再绑定；`TestMain` 并发启动 MySQL 与 PostgreSQL env（`main_test.go:56-57`），可能撞端口 → 60s 超时 → 整个 integration job 失败。
- **M4. 容器清理注册太晚**（**✅ 阶段 3 收尾二**：`061208c` 的启动重构把 `cleanupServiceResources` 放在每条失败路径上，并在服务器进程启动前后都持有容器句柄；死掉的 `serverDir` 一并删除）：`testenv.go:106-110`，在 `startPostgres`、`store.New`、`MigrateSchema`、`UpsertSettingV2`、`startMySQL` 之后才注册；中间任何 `require` 失败都会泄漏容器（Ryuk 可缓解）。
- **M5. 跨 harness 与 runner 的 fixture 重复**：`testenv.go:260-429` 与 `mysql test:196-221`/`postgres test:286-318` 重复声明同一套 `users`/`orders`/`user_order_view` DDL；启动时创建的 `it_app`/`it_drop_me` 未被 per-test DB 使用。
- **M6. CI 缺 race/覆盖率/lint/前端 job**（**✅ 阶段 3 收尾二**：`65f4eaa`，Go 单测 job 加 `-cover`，新增 frontend job 跑 `biome ci`/`lint:ci`/`vue-tsc`/`vitest run`/生产构建；前端覆盖率仍缺，因为需要新增 `@vitest/coverage-v8` 依赖）：单 job、无 `-race`、无 `-cover`、无 `golangci-lint`、无 `pnpm --dir frontend test`；`frontend` 里 `*.test.ts(x)` 数量为 0（尽管 AGENTS.md 记录了 Vitest + jsdom）。
- **M7. `TestMain` 在启动 panic 时可能永久挂起**（**✅ 阶段 3 收尾二**：`061208c`，setup goroutine 内 recover 并把 panic 作为该 env 的错误上报）：`runner/main_test.go:38-71`，两个 goroutine 向容量 2 的 channel 发送，panic 则无人发送，`for range 2 { <-results }` 永不返回，只能等 workflow 20 分钟超时。
- **M8. store 纯函数不变量无测试**（**✅ 阶段 3 收尾二**：`711c0aa`，`patchIamPolicyBindings` 与 `buildManualSQLGUID`/`normalizeManualSQLTags`/`normalizeManualSQLAttributes`/`buildManualSQLStoredMetadata`/`generateEtag` 补齐）：`manual_sql.go:69,73,107,171`（`buildManualSQLGUID`/`normalizeManualSQLTags`/`normalizeManualSQLAttributes`/`buildManualSQLStoredMetadata`）、`policy.go:24,41`（`generateEtag`/`PatchWorkspaceIamPolicy`）；只有 delete 语句构造器有 guard。
- **M9. 外部模式硬编码凭据并派生性地 DROP 数据库**（**✅ 阶段 3 收尾二**：`061208c`，新增 `INTEGRATION_POSTGRES_USER/PASSWORD` 与 `INTEGRATION_MYSQL_USER/PASSWORD`，且只 DROP 自己派生的 `*_integration` 库）：`service_env.go:204,289,435,796-817,887`、`testenv.go:55,215,261`；DSN 硬编码 `postgres:postgres`/`root:root`，`recreatePostgresDatabase` 对派生名 `{INTEGRATION_POSTGRES_DB}_{scope}_integration` 无条件 `DROP DATABASE IF EXISTS`。无 admin 用户/密码覆盖。
- **M10. 文档中的 `make test-integration-mysql` 目标不存在**（**✅ 阶段 3 收尾二**：`8823ac2`，补齐 target）：`README:23,86` 与 `Makefile:1` 的 `.PHONY` 都提到，但 Makefile 里没有该 target。

---

## 低（Low）

- `waitForHTTPReady`（`service_env.go:681-702`）把任何 HTTP 响应（含 5xx）都当作 ready。
- `serverDir` 是死字段（`service_env.go:229,308`，恒为空）。
- `schemasync_lineage_postgres_service_test.go:218` 的 `require.NoError(t, env.ExecPostgres(..., "SELECT 1;"))` 是无意义断言。
- `waitForPostgresReady`（`testenv.go:198`）每次重试都新建完整 `store.Store`；skip 检测靠 `strings.Contains(msg, "docker") && ... "daemon"`（`testenv.go:451`），很脆弱。
- 风格不一致：`migrator_test.go`/`openlineage_api_key_test.go`/`analyzer_test.go` 用 `t.Fatalf` 而非 testify；`openlineage_dataset_test.go` 缺 `t.Parallel()`；`analyzer_test.go:64` 有冗余 `tt := tt`；`meta_resource_test.go:32` 断言 `toOpen` 切片顺序（实现细节）。

---

## 覆盖缺口（明确清单）

**完全没有测试文件的模块**（`go test ./...` 输出确认）：
- ~~`backend/api/auth`~~（**阶段 3 已建测试**：JWT 生成/校验、header/cookie 提取、拦截器、`IsAuthenticationAllowed`；**阶段 3 收尾二**又加了真实 server 的未认证反向集成测试）。
- ~~`backend/server`~~（**阶段 3 收尾二**：`echo_routes_test.go` 覆盖路由/CORS/pprof/占位页，`server_lifecycle_test.go` 覆盖真实监听端口的 Run/Shutdown 与 runner 等待）。
- ~~`backend/api/v1` 的 `debug_interceptor.go`~~（**阶段 3 收尾二**：`debug_interceptor_test.go` 覆盖截断与透传）、`auth_service.go`、`user_service.go`、`instance_service.go`、`database_service.go`、`lineage_service.go`、`llm_service.go`、`explain_sql_service.go`、`openlineage_service.go`、`openlineage_handler.go`、`setting_service.go`（阶段 0 新增，无测试）、`acl_interceptor.go`（阶段 0 新增，无测试）、`common.go`（阶段 0 后为 6 个纯 helper 测试文件：新增 `filter_injection_test.go`）。
- `backend/component/llm`（8 个文件）—— agent 循环、tools、registry、fetcher、message/event。（**阶段 2 部分**：`agent_test.go` 覆盖 SSE 解析、超时、截断与发送取消。**阶段 3 收尾二**：新增 `fetcher_test.go`（`ValidateBaseURL` 表驱动、`FetchModels` 对 stub provider 的正常/非 200/空列表/8MiB 上限/未知字段）、`message_test.go`（`ConvertToLlm` 角色映射与 assistant 文本+tool call）、`tools_test.go`（`BuildContextFromMetadata` 的 GUID 配对与不支持类型跳过、`ExplainSQLTools` 形状）；registry 的缓存/分页仍只由集成路径覆盖。）
- `backend/component/state`、`backend/component/dbfactory`、`backend/config`、`backend/metric`、`backend/bin/server/cmd`、`backend/test/integration/env`（harness 自身无自测）。
- `backend/common`（CEL 构建、GUID/resource name、错误码）、`common/log`、`common/stacktrace`、`backend/utils`。
- ~~`frontend`~~ —— **阶段 3 收尾二**：首批 12 个用例（`src/utils/error.test.ts`、`src/api/lineage.test.ts`），并进入 CI 的 frontend job；覆盖率仍缺（需要 `@vitest/coverage-v8`）。

**无直接测试的 store 文件**（只有 `audit_log.go`、`manual_sql.go` 的 delete builder、`meta_resource.go` 的 helper、`openlineage_api_key.go` 的 mask 有测试）：`policy.go`、`role.go`、`group.go`、~~`principal.go`~~（阶段 2 新增 23505 判定测试）、`project.go`、`database.go`、`instance.go`、`column_lineage.go`、`openlineage_run.go`、`openlineage_task.go`、`llm.go`、`setting.go`、`idp.go`、`namespace_mapping.go`、`stats.go`、`explain_sql.go`、`external_dataset.go`、~~`db_connection.go`~~（阶段 2 新增池上限测试）、`environment.go`、`common.go`、`store.go`。

**Runner**：`schemasync` 只测纯 helper（`convertMetadataToGUID`、`normalizeMetadataForHash`、`batchMetaCreate.diff`、间隔/上次同步默认值、异步入队），同步循环/缓存更新/DB 交互未测；`lineageanalyzer` 只测 `buildSQL`。

**Migrator**：`migrator_test.go` 只覆盖版本/路径纯逻辑；覆盖 fresh install/upgrade/legacy adoption 的集成文件从不执行。

**AGENTS.md 要求但缺失的 guard**：~~`listSublevelMetaRegistryResourceImpl`、`listSublevelMetaRegistryResourceHistoryImpl`、`listDatabaseImpl`~~ —— **阶段 3 收尾二已补齐**（`df97e0a` `f65baaa`，见 T-H3）。

---

## 待确认

- ~~缺 Docker 时的 skip 行为是有意移除，还是替换 harness 时丢失？~~ **✅ 阶段 3 收尾二关闭**：已按 AGENTS.md 的语义恢复 skip（`061208c`），并把探测集中到 `dockerutil`。
- ~~`make test-integration-smoke` 是否应覆盖 `./backend/migrator/...`？~~ **✅ 阶段 3 收尾二关闭**：应覆盖，smoke 与 `test-integration` 都已纳入（`8823ac2`）。
- "partial env config fails fast" 是按引擎还是跨两个引擎？AGENTS.md 与 README:93 读起来是跨引擎，代码只实现了按引擎。
- ~~前端测试是有意缺失吗？~~ **✅ 阶段 3 收尾二关闭**：不是有意缺失，首批用例与 CI frontend job 已落地（`65f4eaa`）；覆盖率仍作为已知剩余项。
- **golangci-lint 在本沙箱无法运行**（**阶段 0 已解除**）：审查当时报 `context loading failed: no go files to analyze`，加 `--no-config`/显式路径后报 `loading compiled Go files from cache: ... cache entry not found` 与 `/home/ran/.cache/golangci-lint` 只读；在最小临时模块上同样复现，因此当时判定为环境限制而非仓库缺陷。阶段 0 修复后复测 `golangci-lint run --allow-parallel-runners` 输出 `0 issues.`，**lint 清洁度现已验证**。`go build ./...`、`go vet ./...`、`go test ./...` 均已通过（exit 0）。
