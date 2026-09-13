# 13 · 未完成修复清单（阶段 8 候选）

本文件把 `01`–`09` 各模块报告中**仍未修复**的条目重新汇总成一份可执行清单。此前各阶段（0–7）已完成大量修复，模块报告里的
`✅/◐/⏳` 标记**已严重过期**（两个方向都有：标着未处理的其实早修好了，标着已修复的其实只做了一半）。因此本清单不沿用旧标记，
而是对每条候选**逐个在当前代码上重新核对**后重新判定。

- **核对基线**：`HEAD = ce3c645`（阶段 7 收尾）。文中行号为核对时快照，改动后可能漂移。
- **核对方式**：逐条 `read`/`grep` 全仓库（含 `_test.go` 与 `integration` build tag、`proto/`、`LATEST.sql`、`Makefile`、`.github/`），
  对高危条目另行运行最小复现（如 `common.GetInstanceDatabaseID("instances//databases/x")`、`runtime.Callers` 在 deferred recover 中的行为）。
- **不包含**：已经修好的条目、以及文档中本就写错的指控（少数重要更正见第九节）。

## 进度（随实施更新）

| 批次 | 内容 | 状态 | 提交 |
| --- | --- | --- | --- |
| 批 1 · 契约与文档一致性 | `C1`、`C4`–`C7`、`C9`、`C10`、`C13`、`D14`–`D16`、`D21`、`B14`；`F1`/`F3` 文档、`F2` 死分支 | ✅ 已完成 | `04b9adc` `21a95a9` `d9b0f16` `c1b6c21` `507e06d` `4f78442` |
| 批 2 · 正确性 | `B2`–`B12`、`B16` | ✅ 已完成 | `39b2ce5` `ce5a4ff` `7ecca87` `e7e64f1` `1c8509b` `29a143a` `2d8e2c2` `e9ce8a5` `2c26648` |
| 批 3 · 安全收尾 | `A4`、`A5`、`A6`、`A8`、`A9` | ✅ 已完成 | `ac616d7` `cdf5ec9` `2968b88` `dd9c32d` |
| 批 4 · 性能与整洁 | `D1`–`D13`、`D17`–`D20`、`C2`、`C3` | ⏳ 未开始 | |
| 批 5 · 测试与 CI | `E1`–`E13`、`D22` | ⏳ 未开始 | |
| 批 6 · 决策后的实现 | `B13`、`C11`、`C14`；`F4`/`F6`/`F7` 文档 | ⏳ 未开始 | |

批 1 的实施细节与逐项对照见第八节末尾「批 1 实施记录」。

## 零、统计

| 报告 | 候选数（约） | 判定仍需处理 | 其中 open | partial | decision-pending |
| --- | --- | --- | --- | --- | --- |
| `01` 入口/装配 | 20 | 5 | 0 | 4 | 1 |
| `02` 认证/授权/审计 | 46 | 9 | 3 | 4 | 2 |
| `03` Store | 74 | 22 | 17 | 4 | 1 |
| `04` api/v1 | 82 | 18 | 8 | 7 | 3 |
| `05` 组件 | 25 | 4 | 2 | 2 | 0 |
| `06` Runner/Migrator | 46 | 16 | 12 | 3 | 1 |
| `07` common/utils | 29 | 6 | 5 | 0 | 0 |
| `08` Proto/契约 | 51 | 8 | 3 | 4 | 1 |
| `09` 测试/CI | 45 | 13 | 5 | 8 | 0 |
| **合计（去重前）** | **~418** | **~101** | | | |

去重后本清单共 **74 项**（跨报告重复的 XOR 混淆、审计脱敏、panic 栈、not-found 错误码、per-resource IAM 等已合并）。
按性质分：安全与正确性 25 项（A 9 + B 16）、契约与文档 14 项（C）、性能与整洁 22 项（D）、测试与 CI 13 项（E）。另有 **10 个产品/部署决策**，**本轮已全部拍板**
（见第六节与 [`14-phase8-decisions.md`](14-phase8-decisions.md)）；其中 F5/F8/F9/F10 的结论追加为 2 个新条目（`B16`/`C14`）。

**一句话结论**：阶段 0–7 已把"高危可利用 + 大面积死代码"清干净。剩下的没有"立刻会被外部利用"的洞，且本轮已把 10 个待确认决策全部关闭：
凭证混淆保持同库种子 XOR（记录威胁模型）、破坏性同步保持仅日志、`audit_log`/history 永久保留——这 3 条从"待决策"转为"已接受的已知风险/设计"。
真正剩下的工作是：① 令牌吊销仍是进程内（单副本无影响，多副本需改共享存储）；② 一批"契约说了但没实现 / 文档与实现不符"的条目（`StoredMetadata`
的 OpenLineage 过滤、`ssl_key`、`domain allowlist`、`LATEST.sql` 的 JSONB 注释）；③ 两个新功能（域白名单、ExplainSQL provider 允许列表与选择器）；
其余多为 Low/Debt 级的健壮性与整洁项。

---

## 一、A 类：安全

| 编号 | 原始 ID | 位置 | 已核对现状 | 建议动作 |
| --- | --- | --- | --- | --- |
| A1 | `04 B-M18`/`05 C-H3`/`07 U-H2` | `backend/common/utils.go:66-98`、`store/setting.go:161-175`、`server/init.go:22-38`、`store/llm.go:41-64` | **partial**。`Obfuscate`/`Unobfuscate` 仍是 `base64(XOR(src, seed))`，seed 是同库 `AUTH_SECRET`，且该值同时是 JWT 签名密钥。无 nonce、无完整性校验、密文确定性（相同明文 ⇒ 相同密文）。空 seed 除零已在阶段 6 修掉，调用方都传播错误。阶段 3 的 AES-GCM + `METADATA_SECRET_KEY` 在阶段 5 被**有意回滚**。 | **已决策（F3=A，本轮）**：保持 `AUTH_SECRET` 种子 XOR，不做 AEAD/库外密钥。实现动作降级为文档：在 `AGENTS.md`/部署文档写明"有 DB 读权限或备份即可解密全部凭证"，并说明密钥与密文同库、`AUTH_SECRET` 兼作 JWT 签名密钥。 |
| A2 | `02 H1` | `backend/component/state/state.go:11-44`、`backend/api/v1/auth_service.go:287`、`backend/api/auth/auth.go:226` | **partial**。吊销集合是进程内 `lru.Cache`（容量硬编码 `tokenRevocationCapacity = 4096`），登出在副本 A 生效、副本 B 继续接受该 token 直到过期（最长 7 天）。密码变更失效走数据库，跨副本有效。 | 若要支持多副本：改为 DB/Redis 存储（按 token 或 `user+iat` 键，带 TTL）。若确定单副本部署：把该约束写进部署文档，并把容量改成可配置项。 |
| A3 | `02 H3`/`08 P-C1`/`09 T-H5` | `backend/api/v1/audit.go:228-244`、`proto/v1/v1/instance_service.proto:401` | **partial**。脱敏按**字段名**精确表 + 子串标记（`password`/`token`/`secret`/…），不按 proto 描述符的 `INPUT_ONLY`/sensitive 行为。写路径与读路径（历史行回读）都用它。`ssl_key` 未按报告建议改名。GCP/Kerberos/external-secret 消息已删除，当前无明文泄漏路径；`audit_test.go` 已断言 `sslKey`/`sslCert`/`sshPrivateKey` 被脱敏。 | 改为基于描述符的脱敏（遍历 message fields，按 `field_behavior`/sensitive 语义判定），至少补上"未来新增凭据字段漏网"的守卫测试；同时决定是否把 `ssl_key` 改为 `ssl_private_key`（proto 破坏性改动，需 `buf generate`）。 |
| A4 | `04 A-H1` | `backend/api/v1/instance_service.go:133-135,591-593` | **partial**。`Ping` 的错误已脱敏（只回显 datasource type），但**driver 构造**失败仍原样回传：`connect.NewError(CodeInternal, errors.Wrapf(err, "failed to get database driver"))`。构造会先拨 SSH（`plugin/db/util/ssh.go:46-49`），因此 workspaceAdmin 用 `validate_only` 可读到 `dial tcp 10.0.0.5:22: connect: connection refused`。 | 按 `Ping` 的同一方式脱敏：明细写 `slog`，对外只回通用 `InvalidArgument`/`Internal`。内网 allow/deny 已作为产品决策放弃，不需要它也能关掉这条信息泄漏。 |
| A5 | `04 B-H7` | `backend/api/v1/openlineage_handler.go:20-26,109-114`、`backend/server/echo_routes.go:23-68` | **partial**。批次上限（1000 事件 / 8MiB → 413）与整批单事务已完成；仍**没有速率限制、没有请求超时**（`receiveEvent` 直接用 `c.Request().Context()`，无 deadline）。全仓库唯一的限流是登录节流。 | 给 `/api/v1/lineage` 加按 key/IP 的限流与请求超时中间件（或 handler 级 ctx deadline）。 |
| A6 | `04 B-H5` | `backend/component/llm/fetcher.go:34-50`、`backend/api/v1/llm_service.go:247-251` | **partial**。`ValidateBaseURL` 只校验"绝对 http/https + 有 host"，**允许 http 与私网/环回/链路本地地址**；`FetchLLMModels` 在请求未带 key 时仍回退 `prof.Metadata.ApiKeyEncrypted`，于是把已存 profile 的 `base_url` 改到攻击者主机再拉模型即可外带该 key。写 profile 已限管理员，故为 medium。 | 非环回主机要求 https 或解析后拒绝私网段；`base_url` 变更后不再回退存量密钥（要求显式传 `api_key`）。 |
| A7 | `01 H3` | `backend/server/grpc_routes.go:149-153`、`backend/api/auth/config.go:10-16` | **decision-pending**。`grpcreflect.NewHandlerV1/V1Alpha(reflector)` **没有传 `handlerOpts`**（对比各业务 handler 都传了 `handlerOpts`），因此反射请求根本不经过 auth/ACL 拦截器；`IsAuthenticationAllowed` 里的 `/grpc.reflection` 前缀豁免因此是**不可达的死代码**。报告原来的"前缀不匹配"指控经核对是错的（v1.3.0 的路径确实以 `/grpc.reflection` 开头）。 | **已决策（F2=A，本轮）**：保持反射匿名。实现动作＝删除 `backend/api/auth/config.go:10-16` 的 `/grpc.reflection` 豁免分支（它因反射 handler 未传 `handlerOpts` 而不可达），并把"反射公开 API schema 定义"写进安全/部署文档；不接拦截器。 |
| A8 | `02 M11` | `backend/api/v1/common.go:123-127,176-188`、`proto/store/store/common.proto:8-11` | **partial**。page token 的 `limit/offset` 仍是 `int32`，只拒负 `limit`、只把负 offset 夹到 0，无上界。伪造一个接近 `2^31` 的 offset 会让 `offset+limit` 回绕成负、下一页 token 落回第 1 页。 | 字段改 `int64` 并在解码时校验 `offset+limit` 上界；或对 token 签名。属 proto 改动，需 `buf generate`。 |
| A9 | `04 B-L-lineage-route` | `backend/server/grpc_routes.go:212-216` | **open(debt)**。OpenLineage 摄取注册为普通 Echo group，不经 `NewDebugInterceptor`/`NewAuditInterceptor`/`NewACLInterceptor`，成功/失败的摄取都不写 `audit_log`（认证是 ingestion key）。 | 若摄取需要可审计：在 handler 内显式写审计记录（或包一层 group），而不是依赖 Connect 拦截器链。 |

---

## 二、B 类：正确性

| 编号 | 原始 ID | 位置 | 已核对现状 | 建议动作 |
| --- | --- | --- | --- | --- |
| B1 | `06 R-H3` | `backend/runner/schemasync/syncer.go:654-656,770-793` | **partial**。`existMap` 里每个剩余对象都直接进 `deletes`；唯一变化是 `logSchemaSyncDeletion`（条数/类型分布/前 20 个 GUID）。**空快照或权限收窄导致的残缺快照仍会清空注册表与 `column_lineage`**。阶段 1 的产品决策是"只记日志、不拦截"。 | **已决策（F4=A，本轮）**：**保持仅日志**（未采纳"空快照拒绝"推荐）。本项转为已接受设计：不加阈值、不加确认开关；仅把 `logSchemaSyncDeletion` 的语义与"权限收窄会清空注册表/血缘"的风险写进运维文档。 |
| B2 | `04 A-H3` | `backend/api/v1/database_metadata.go:225-240,356-368`、`backend/runner/schemasync/syncer.go:487-495`、`backend/plugin/schema/differ.go:268-269,435` | **partial**（原标 ✅ 不成立）。`rebuildDatabaseObjects` 已覆盖 TABLE/EXTERNAL_TABLE/VIEW/MV/FUNCTION/PROCEDURE/SEQUENCE，`buildDiffSummary` 也会统计 EnumTypes/Extensions/EventTriggers/Events，但**重建阶段从不产生这些输入**：`rebuildSchemaContents` 只复制 Name/Owner/Comment/SkipDump，丢掉 `schemaMeta.EnumTypes/Events`；`buildDatabaseSchemaAtTime` 只 `DatabaseSchemaMetadata{Name: parts[1]}`，不读 DATABASE 行的 Extensions/EventTriggers/字符集/排序规则。因此 PG 的 enum/extension/event trigger 变更与 MySQL 的 event 变更仍报 "No changes detected." | 从 as-of 的 `schemaMeta` 复制 `EnumTypes/Events`，并从目标时刻的 DATABASE 注册行读取 `Extensions/EventTriggers`/字符集/排序规则。 |
| B3 | `03 M16`/`04 B-M15` | `backend/store/namespace_mapping.go:156,178`、`backend/store/openlineage_api_key.go:152`、`backend/store/manual_sql.go:319,427,553`、`backend/store/llm.go:115`、`backend/api/v1/openlineage_service.go:226,237,283` | **open**。这些 not-found 仍是裸 `errors.Errorf`，`common.ErrorCode` 把非 `*common.Error` 判为 `Internal`，拦截器于是回 500；部分 handler 还显式包成 `connect.CodeInternal`。原报告"阶段 3 M15 已修"只覆盖到 `store/database.go`、`store/setting.go`。 | store 侧改 `common.Errorf(common.NotFound, ...)`；handler 侧不要主动包 `CodeInternal`，由 `ErrorMappingInterceptor` 映射。 |
| B4 | `04 B-M3` | `backend/api/v1/explain_sql_service.go:315,341,347,384-386,441-445` | **open**。`resolveSource` 已修（store 错误返回 Internal），但上下文/tool 路径仍吞错：`buildContextFromLineage` 在 `ListColumnLineage` 出错时退化为直接取；`buildContextFromSQL` 出错返回空 context；`fetchObjectsByGUIDs` `continue`；`toolGetObjectSchema` 用 `list, _ :=` 后回答 "no object found"。**DB 抖动会被当成"对象不存在"喂给模型，并可能进缓存。** | 传播错误，或至少返回 error 形态的 tool result（tool 路径已有该形态）。 |
| B5 | `04 A-M7` | `backend/api/v1/instance_service.go:411-422`、`proto/v1/v1/instance_service.proto:252-284` | **partial**。`BatchSyncInstances` 已改为逐项 `BatchSyncInstanceResult`，但 `BatchUpdateInstances` 仍是"循环 + 首个错误即 return"，前面的更新已提交而客户端只拿到一个错误；两个 batch RPC 都**没有执行 proto 文档写的 1000 条上限**（只判空）。 | 给 `BatchUpdateInstances` 补逐项结果（或整批事务），两个 RPC 都加 1000 上限。 |
| B6 | `03 M24` | `backend/store/audit_log.go:35-39` | **open**。`CreateAuditLog` 在 proto 带 `CreateTime` 时直接采用（拦截器今天不设，所以现状是服务端时间），但 store 层允许回填，审计顺序可被调用方伪造。 | 始终 `NOW()` 或拒绝调用方提供的 `CreateTime`。 |
| B7 | `03 M4` | `backend/store/principal.go:447-456` | **partial**。缓存写穿竞态已修（`proto.CloneOf`），但 `LastChangePasswordTime` 只在 `patch.Profile == nil` 分支里设；目前没有调用方同时带 Profile 与 PasswordHash，所以是潜伏缺陷。 | 只要 `PasswordHash` 变化就无条件设 `LastChangePasswordTime`，并把 `patch.Profile` clone 后写入。 |
| B8 | `03 TBC-2`/`06 Low-guid` | `backend/plugin/openlineage/metadata.go:101-113`、`backend/runner/schemasync/syncer.go:738-740`、`backend/runner/lineageanalyzer/analyzer.go:233`、`backend/common/const.go:27` | **open**。OpenLineage GUID 用 `url.PathEscape` 后以 `:` 拼接，而 RFC 3986 的 pchar 包含 `:`，分隔符本身不转义 → `(jobType="a", namespace="b:c")` 与 `(jobType="a:b", namespace="c")` 撞同一 GUID（撞 `openlineage_task` 的 unique guid 与 `meta_registry_resource(guid, object_type)`）。同一类问题在 `MetaGUIDSplit = ";"` 的 schema GUID 上：名称含 `;` 会让 `analyzer.go:233` 的 `SplitN(...,4)` 错位。 | 对分隔符本身做百分号编码，或改用长度前缀编码；抽一对共享的 encode/decode helper 给 syncer 与 analyzer。 |
| B9 | `06 M6` | `backend/runner/schemasync/syncer.go:113-135`、`backend/api/v1/database_service.go:43`、`backend/api/v1/instance_service.go:345,390`、`backend/plugin/db/mysql/mysql.go:93` | **open**。`InstanceOutstandingConnections` 只在 10s 的 database checker 里 Increment/Decrement；`SyncInstance`→`GetInstanceMeta` 与 API 触发的 `SyncDatabaseSchema`/`SyncInstance` 直接建 admin driver，绕过限流。MySQL admin driver 每实例仍开 50 连接池。 | 在所有 driver 创建路径（或 `dbfactory` 内）统一 acquire/release 限流。 |
| B10 | `03 LOW-get-multirow` | `backend/store/openlineage_run.go:319-327`、`openlineage_task.go:412-415`、`external_dataset.go:96-99`、`llm.go:167-170`、`namespace_mapping.go:61-64` | **open**。这些 `Get*` 直接返回 `list[0]`，多行时静默取第一；`GetManualSQL`/`GetIdentityProvider` 已会报 conflict。 | 与 `GetManualSQL` 对齐：非唯一即返回 conflict。 |
| B11 | `04 A-L-create-sync` | `backend/api/v1/instance_service.go:161-174` | **open**。`CreateInstance` 成功后用 admin driver 作为同步闸门，`if err == nil { ... SyncInstance ... }` **没有 else 分支、不记日志**；成功路径还会让创建请求阻塞到完整 `SyncInstance` 结束。 | 记日志（或返回）被丢弃的 driver 错误；初始同步改走已有的异步路径。 |
| B12 | `06 Low-get-version-path`/`Low-dup-version`/`Low-adopt-legacy` | `backend/migrator/migrator.go:207-228,238-272,278-299` | **open**。① `getVersionFromPath` 不校验四位宽度与目录等于基线 `MAJOR.MINOR`（`00001##x.sql`、`1##x.sql`、`9.9/0001##x.sql` 都被接受）；② 重复版本不提前检测，靠 ledger 唯一索引在**第二个文件执行到一半**时才失败；③ `adoptLegacySchema` 只建 ledger 并写 `0.1.0`，不校验任何基线表形状，只要 `principal` 表存在就接管。 | 文件名加 `^[0-9]{4}$` 与目录一致性校验；`getSortedVersionedFiles` 用 map 检测重复并在执行任何 DDL 前报错；adopt 前校验一组 `LATEST.sql` 哨兵表/索引。 |
| B13 | `06 Low-view-columns`/`S-Q3` | `backend/runner/schemasync/syncer.go:729-736`、`backend/store/meta_resource.go:583-584`、`frontend/src/components/metadata/ViewMetadataDetail.vue:79-80,176-177` | **open**。`getChildMetadataResources` 只对 `MetaType_TABLE` 生成 COLUMN 资源，而 `getNextLevelObjectType` 对 VIEW/MV/EXTERNAL_TABLE 也宣称下一层是 COLUMN；元数据浏览器用 `listMetadata({parentGuid})`（即 sublevel）列子节点，所以**视图的列页签为空**（视图详情组件则直接读 `viewMetadata.columns`）。 | **已决策（F5=B，本轮）**：收掉声明（未采纳"生成元资源"推荐）。实现动作＝从 `getNextLevelObjectType`（`meta_resource.go:583-584`）去掉 VIEW/MV/EXTERNAL_TABLE 的 COLUMN 声明，浏览器不再声称有列子层级；列只在 `ViewMetadataDetail` 等详情组件可见。需同步检查前端视图节点表现与集成用例。 |
| B14 | `07 M4` | `backend/common/resource_name.go:188-202` | **open**。`GetNameParentTokens` 只比较前缀、不做非空校验。已实测：`common.GetInstanceDatabaseID("instances//databases/x")` → `("","x",nil)`；`GetInstanceID("instances/")` → `("",nil)`。调用方要到 DB 外键/NotFound 才失败，而不是 `InvalidArgument`。 | 循环里加 `parts[2*i+1] != ""` 校验。 |
| B15 | `04 A-L-label-filter` | `backend/api/v1/filter.go:329-344`、`proto/v1/v1/database_service.proto:202,213-215` | **open**。过滤器只注册变量 `label`、只支持 `key:value` 的 `==`，值里含 `:` 直接被拒；proto 文档仍写 `labels.{key}` 与 `labels.region in [...]`，而 `labels.environment == "production"` 会解析成未知变量返回 `InvalidArgument`。 | 实现文档承诺的 `labels.<key>` + `in`，或把 proto 文档改成真实表面（`label`、`key:value`、`==`，并注明值不支持 `:`）。 |
| B16 | 新增（F10 决策） | `backend/api/v1/openlineage_handler.go:253-258`、`backend/store/openlineage_run.go:391`、`backend/store/maintenance.go` | **open**。`eventTime` 只用 `time.Parse(time.RFC3339Nano)` 解析；无时区偏移即失败 → `EventTime = NULL`，排序 `NULLS LAST`，且保留清理豁免 NULL 行。 | **已决策（F10=A，本轮）**：无偏移时按 UTC 兜底（追加 `Z` 后再解析）；补一个"无偏移时间戳进入正确排序位置并可被保留清理"的测试。 |

---

## 三、C 类：契约与文档一致性

| 编号 | 原始 ID | 位置 | 已核对现状 | 建议动作 |
| --- | --- | --- | --- | --- |
| C1 | `08 P-H6` | `proto/v1/v1/database_service.proto:642-666`、`backend/api/v1/database_service.go:178-198,311-336`、`backend/api/v1/database_convert.go:16-63` | **partial**（另一份报告误判为已修）。v1 `StoredMetadata` 注释承诺 "OpenLineage registry rows … are filtered out of ListMetadata/GetMetadata/SearchMetadata"，但**没有任何过滤**：`ListMetadata` 把 `meta_type` 原样传给 store 并逐行转换，`SearchMetadata` 不排除，`convertStoredMetadataMessage` 对两个 OpenLineage summary 落到 default 返回空 `StoredMetadata`。store 侧确实在写 `MetaType_OPENLINEAGE` 行，v1 也暴露了 `OPENLINEAGE=100`。 | 实现文档承诺的过滤（三个 handler 跳过/拒绝 OPENLINEAGE），或删掉该注释；补 oneof 分支是报告已否决的方案。 |
| C2 | `08 M13` | `backend/migrator/migration/LATEST.sql:115,131,163,178,406,421,441` | **open(debt)**，阶段 6/7 两次有意排除。只有 `idp.config`/`principal.profile`/`policy.payload`/`user_group.payload`/`role.permissions` 有 `Stored as <message>` 注释；缺 `instance.metadata`、`db.metadata`、`meta_registry_resource.metadata`、`history.metadata`、`audit_log.payload`、`llm_provider_profile.metadata`、`explain_sql_cache.explanation_json`，`0.1/0007` 的 `key_digest`/`scope_namespace` 在 `LATEST.sql` 里也没注释。 | `AGENTS.md` 把该注释当契约，建议随下一次 schema 变更补：加增量 + 同步 `LATEST.sql` 表注释。 |
| C3 | `08 M2` | `proto/v1/v1/common.proto:17-28` ↔ `proto/store/store/common.proto:15-26`；`database_service.proto:1384-1408` ↔ `store/meta.proto:7-30`；`instance_service.proto:429-433` ↔ `store/instance.proto:88-92` | **partial(debt)**。`Engine`/`MetaType`/`DataSourceType` 在 v1 与 store 双侧重复定义（数值必须一致，因为 `database_convert.go` 靠 `proto.Marshal/Unmarshal` 复用编号）。收敛已完成（28→5、27 case 转换表删除），但**没有任何漂移守卫**，一侧改了另一侧不知道。 | 加一个 CI/单测级别的漂移检查（解析两侧 proto 或对生成枚举做数值对照）。 |
| C4 | `08 dead-store-messages` | `proto/store/store/common.proto:30,37`、`proto/store/store/openlineage.proto:9-13` | **open(debt)**。`Position`/`Range`/`SchemaField` 三个 store 消息零引用（无字段是这两类型）；`SchemaField` 注释仍称存于 `external_dataset.schema_fields`，而该列已被增量 `0.1/0008` 删除、`LATEST.sql:277` 明写"there is no schema_fields column"。 | 删除三个消息并 `reserved` 编号/名字；重写注释。属 proto 改动。 |
| C5 | `08 M8` | `backend/api/v1/instance_service.go:93` | **partial(debt)**。`ListInstanceDatabase` RPC/消息/handler 都已删除，全仓库只剩这一行孤儿注释 `// ListInstanceDatabase list all databases in the instance.`（就在 `CreateInstance` 上方）。 | 删掉注释。 |
| C6 | `08 M20` | `proto/v1/v1/user_service.proto:141-151` | **partial**。`ListUsersRequest.filter` 的示例仍用已删除的枚举名：`user_type == "USER"`、`in ["USER"]` 等（现为 `END_USER=1`），解析走 `v1pb.UserType_value[name]`，所以示例现在直接 `InvalidArgument`。 | 示例改成 `END_USER`，或在 `principalTypeFilterValue` 里兼容旧拼写。 |
| C7 | `02 待确认5` | `proto/v1/v1/user_service.proto:18-19,27,40-43`、`backend/store/predefined_roles.go:39-52` | **open**。proto 注释写 "Any authenticated user can get/list users"，但方法已声明 `metaxisdata.users.get/list`，而 `memberBaselinePermissions` **不含** UsersGet/UsersList，于是普通 `workspaceMember` 被拒——文档与强制行为互相矛盾。 | 更新注释说明所需权限，或把 `users.get/list` 加进 member 基线（取决于产品意图）。 |
| C8 | `02 GetTokenDuration` | `backend/api/auth/header.go:91-96` | **partial(debt)**。`GetTokenDuration(_ ctx, _ *store.Store)` 恒返回硬编码 `DefaultTokenDuration = 7*24h`，参数不用；`token_duration` 设置字段已在阶段 3 删除，所以原来的"设置被静默忽略"指控作废，但 stub 本身还在。 | 删掉/改名该 stub，或真正实现一个 token 时长设置。 |
| C9 | `02 低-UserGetError` | `backend/api/v1/user_service.go:47-67` | **open**。按 email 查找失败时，`:67` 的报错用 `userID`（`GetUIDFromName` 失败返回 0）拼成 `user 0 not found`。 | 区分路径：email 查找失败时回显 email/资源名。 |
| C10 | `04 A-L-pluralize` | `backend/api/v1/database_history_change.go:299,319-327` | **open**。`pluralize` 对非 `y` 结尾一律加 `s`，索引变更摘要渲染成 `+2 indexs`。 | 加不规则/咝音复数映射。 |
| C11 | `08 setting-domain-allowlist-unreachable` | `proto/store/store/setting.proto:34,37`、`backend/api/v1/user_service.go:586-615`、`backend/api/v1/setting_service.go:54-69` | **decision-pending**。`WorkspaceProfileSetting.domains`/`enforce_identity_domain` 被 `validateEmailWithDomains` 读、gating 注册/更新/登录，但**没有任何写入方**：`UpdateWorkspaceProfileSetting` 只接受 4 个 mask 路径，v1 不暴露这两个字段，`init.go` 只写空消息。 | **已决策（F8=B，本轮）**：暴露域白名单。实现动作＝v1 设置消息暴露 `domains`/`enforce_identity_domain`、`UpdateWorkspaceProfileSetting` 增加对应 mask 路径、`/settings/general` 加表单项（两个 locale），并补校验测试（含 `enforce_identity_domain=true` 且列表为空时的语义）。 |
| C12 | `05 registry-apikey-naming` | `backend/component/llm/registry.go:32,108`、`backend/store/llm.go:222`、`proto/store/store/llm.proto:27` | **partial**。`ResolvedConfig.APIKey` 已注释为 "decrypted"，但源字段仍叫 `api_key_encrypted`，且 `ListLLMProfiles` 返回前原地解混淆，于是**名为 encrypted 的字段装着明文**；其它调用方按该名当 Bearer 用。 | **已决策（F3=A，本轮）**：保留 XOR 与现有命名，本项不再作为代码项；仅补注释说明 `ResolvedConfig.APIKey`（明文）与 `api_key_encrypted`（同样在返回前被解密）的命名歧义，避免新调用方误以为它已加密。 |
| C13 | `04 A-L-convert-reflection` | `backend/api/v1/database_convert.go:129-226` | **open(debt)**。12 个 metadata 转换函数都是 `data, _ := proto.Marshal(meta); _ = proto.Unmarshal(data, result)`，两个错误都丢弃；仅当 store/v1 字段号完全相同才正确。 | 换成显式字段映射（参照 `convertManualSQLMetadata`），或至少检查错误；加一组 store↔v1 golden 测试。 |
| C14 | 新增（F9 自定义决策） | `proto/store/store/setting.proto`、`proto/v1/v1/setting_service.proto`、`backend/api/v1/explain_sql_service.go:49-66`、`frontend/src/pages/ExplainSQLPage.vue`、Settings 页面 | **新增功能**。本轮决策：ExplainSQL 前端加 provider/模型选择器，**并在 Settings 增加管理员配置的「允许的 provider 列表」**；服务端解析 `provider_name` 时校验其属于允许列表（不在列表 → `InvalidArgument`，缓存 key 仍按 provider/model 隔离），选择器只列出"允许列表 ∩ 已启用"。 | 新设置字段（workspace setting）+ `UpdateWorkspaceProfileSetting` mask + 前端多选 UI + ExplainSQL 选择器 + 单测（列表为空语义、非允许 provider 被拒）与前端 Vitest；需 schema 增量 + `buf generate`。 |

---

## 四、D 类：性能、健壮性与整洁

| 编号 | 原始 ID | 位置 | 已核对现状 | 建议动作 |
| --- | --- | --- | --- | --- |
| D1 | `06 Low-on2` | `backend/runner/schemasync/syncer.go:331,353` | **open(debt)**。`SyncInstance` 在两处循环里用 `slices.IndexFunc` 按名查找，数据库数量增大时退化为 O(n²)。 | 先建 `map[DatabaseName]*DatabaseMessage` 再按 key 查。 |
| D2 | `06 Low-sync-map` | `backend/runner/schemasync/syncer.go:98-110` | **open**。实例已在 `instanceMap` 中缺失时只打 Debug 并 `return true` 保留条目，checker 每 10s 重扫，条目与日志无限重复；同一处还把已处理的 `err`（此时为 nil）传给 `log.WithError`。 | 实例不存在时删除条目（或失败 N 次后淘汰）；去掉 nil error 属性。 |
| D3 | `03 LOW-single-stmt-tx` | `backend/store/setting.go:109,138`、`idp.go:37`、`principal.go:135`、`manual_sql.go:288`、`meta_resource.go:132` | **open**。单语句读仍包 `BeginTx/Commit`；`GetManualSQL` 还要一次事务 + 3 条查询。 | 单语句直接在连接池上跑；只有多语句不变量才开事务。 |
| D4 | `03 LOW-find-mutation`/`LOW-nil-find` | `backend/store/instance.go:54`、`policy.go:257`、`meta_resource.go:369-370,395-396`；`column_lineage.go:131`、`openlineage_run.go:333`、`openlineage_task.go:421`、`external_dataset.go:105`、`namespace_mapping.go:70`、`llm.go:178` | **open**。前者：`GetInstance`/`GetPolicy`/子层级查询会反写调用方的 `find` 结构（`ShowDeleted`/`ShowAll`/`LimitPreObjectType`），复用的调用方语义被静默改变。后者：多个 list 函数直接解引用 `find`，只有 `listManualSQLImpl` 容忍 nil。 | 前者的字段复制到局部或用显式选项参数；后者统一决定"拒绝 nil"或"默认空过滤器"。 |
| D5 | `03 LOW-limit-offset-sprintf` | `backend/store/manual_sql.go:607-611`、`column_lineage.go:162-167`、`openlineage_run.go:23-33`、`openlineage_task.go:464`、`group.go:110-115`、`principal.go:275-280` | **open**。`limit`/`offset` 仍 `Sprintf` 进 SQL，负数会变成 `LIMIT -1` 直接交给 PG。 | 改占位符绑定，并在构造前 clamp/拒绝负值。 |
| D6 | `03 LOW-list-key-hash`/`LOW-task-latest-event-type`/`LOW-api-key-expiry`/`LOW-manual-sql-id` | `openlineage_api_key.go:110-113,29,82-96`、`openlineage_task.go:516-539`、`store/openlineage.proto:45-65`、`manual_sql.go:24` | **open/partial**。① `ListOpenLineageAPIKey` 注释说 "without hashes exposed"，却仍 select `key_hash`（今天靠 `convertAPIKey` 不填兜住）；② task 行带 `LatestEventType`，但 `buildOpenLineageTaskStoredMetadata` 不填、store summary 也没有该字段；③ OL key 有 scope 但**没有 expires_at**；④ `ManualSQLMessage.ManualSQLID` 没有对应列（读时由 name 推导）。 | 逐项：list 查询去掉 `key_hash`；补 store summary 字段或从 task 行去掉；加过期列或明确"仅手动吊销"；`ManualSQLID` 删除或补列。 |
| D7 | `03 LOW-update-instance-validation` | `backend/store/instance.go:147-190` | **partial**。`CreateInstance` 会 `validateDataSources`（恰好一个 ADMIN），`UpdateInstance` 不会；今天靠 API 层 `checkInstanceDataSources` 兜住，绕过 API 的调用方可以存下 0 个或多个 ADMIN。 | `UpdateInstance` 也调用校验。 |
| D8 | `06 M8` | `backend/runner/schemasync/syncer.go:139-142,182-184` | **open**。同步失败已从 Debug 提到 Warn，但**没有计数器/告警**；`backend/plugin/metric/` 已不存在。 | 等指标设施恢复时加失败计数与告警规则，至少加周期性汇总日志。 |
| D9 | `06 Dead-hardcoded` | `backend/runner/schemasync/syncer.go:31-38`、`backend/runner/lineageanalyzer/analyzer.go:25-30` | **partial(debt)**。"注入 profile 但从不读"的矛盾已随阶段 7 删除 profile 字段消失；间隔/上限仍是编译期常量，运维无法覆盖。 | 想要可配置就 thread profile/flags 进 `NewSyncer`/`NewAnalyzer`；否则作为已接受设计关闭。 |
| D10 | `06 Dead-dup-lineage` | `backend/runner/schemasync/syncer.go:831-847`、`backend/store/manual_sql.go:795-805` | **open(debt)**。血缘删除有两套 SQL：store 版走 `buildDeleteColumnLineageByGUIDStatement`/`appendGUIDSubtreeCondition`（连后代一起删），runner 版是另一段 4 参数裸 SQL（只精确匹配）。两条路径各自演化。 | 合并到一个 helper（带 runner 需要的 source/target 方向），避免转义/子树逻辑漂移。 |
| D11 | `03 DEAD-deletecache` | `backend/store/setting.go:96-101` | **open(debt)**，阶段 7 有意保留。`DeleteCache` 无调用者，且只清 `settingCache`/`policyCache`/`userEmailCache`/`userIDCache`，不含 `idpCache`/`instanceCache`。 | 删除，或补全所有缓存并把未来失效统一走它。 |
| D12 | `03 DEAD-misplaced-func` | `backend/store/openlineage_api_key.go:172-211` | **open(debt)**。`FindExternalDatasetByGUIDs` 只查 `external_dataset`，却定义在 `openlineage_api_key.go` 末尾。 | 移到 `external_dataset.go`。 |
| D13 | `03 DEAD-list-sublevel-asof` | `backend/store/meta_resource.go:388-407` | **open(debt)**。`ListSublevelMetaRegistryResourceAsOf` 全仓库零调用（兄弟函数 `GetMetaRegistryAsOf` 被集成测试用）。 | 删除，或接到需要 as-of 子层级的视图上。 |
| D14 | `02/05 metric-reporter-comments` | `backend/api/v1/auth_service.go:188`、`backend/api/v1/user_service.go:219` | **open(debt)**。两处注释掉的 `s.metricReporter.Report(...)` 块仍在，引用的 `metric`/`metricapi` 包已随阶段 3 删除。 | 删除注释块，或恢复为真实遥测面。 |
| D15 | `07 L-llm-guid-dup` | `backend/component/llm/tools.go:118-124` | **open(debt)**。LLM tools 里用 `strings.Split(guid, ";")` 手工解析 meta GUID，硬编码分隔符，重复 `common.GetSchemaFromGUID`。 | 改用 `common.GetInstanceFromGUID`/`GetSchemaFromGUID`。 |
| D16 | `01 LOW-banner` | `backend/bin/server/cmd/root.go:24-38,170` | **partial(debt)**。启动横幅仍 `fmt.Printf` 直写 stdout，绕过阶段 1 装好的 slog handler，`--enable-json-logging` 对它无效。 | 用 `slog.Info` 打印（或去掉）。 |
| D17 | `01 M6`/`07 L-stacktrace` | `backend/common/stacktrace/stack.go:9-14`、`backend/server/echo_routes.go:102`、`backend/runner/schemasync/syncer.go:163` | **partial**。三个调用点都在 deferred recover 里采集，`runtime.Callers` 拿到的是 recover 帧而非 panic 现场（已用独立程序复现）；今天没有测试断言栈内容。 | 在 panic 现场（或从 panic 值）采集栈，或补一个断言"panic 函数+行号出现在日志里"的回归测试；否则明确接受。 |
| D18 | `07 C4` | `backend/server/grpc_routes.go:157-169` | **open**。REST gateway 的 `grpcConn` 是局部变量，`Server.Shutdown` 从不关闭，也没有注册 stopper。 | `s.AddStopper`/`defer grpcConn.Close()`。影响仅限同进程启停（集成 harness 是独立进程）。 |
| D19 | `04 B-L-provider-error` | `backend/component/llm/agent.go:311-316` | **open**。provider 的错误体最多 4000 字节被原样回传给流式客户端。 | 明细只写服务端日志，对外只回状态码 + 通用信息。 |
| D20 | `04 A-L-hashostport-join` | `backend/store/instance.go:198,275-277` | **partial(debt)**。参数化已完成，但"是否 join 数据源"仍靠 `hasHostPortFilter` 在生成的 SQL 里**子串匹配** `"ds ->> 'host'"`；值已不可控，但 join 与 `filter.go` 的列文本强耦合。 | 由 filter 翻译器返回结构化标记（如 `NeedsDataSourcesJoin`）。 |
| D21 | `04 A-L-history-dead-branch` | `backend/api/v1/database_history.go:186-191` | **open**。外层 `if previous.ValidTo != nil && previous.ValidTo.Equal(row.ValidFrom)` 之内又判断其否定，内层分支不可达。 | 删除内层 `if`。 |
| D22 | `05 registry-test-gap` | `backend/component/llm/registry.go:54-90` | **open(debt)**。Registry 的 30s TTL 缓存、分页遍历（`registryPageSize=100`）与写侧 `Invalidate()` **无任何测试**（报告声称"集成覆盖"是错的）。 | 加 hermetic `registry_test.go`，用 fake store 覆盖 TTL 命中/失效/跨页/禁用过滤；需要像 `utils/member.go` 的 `MemberStore` 那样加一个接口缝。 |

---

## 五、E 类：测试与 CI

| 编号 | 原始 ID | 位置 | 已核对现状 | 建议动作 |
| --- | --- | --- | --- | --- |
| E1 | `06 R-H5` | `backend/migrator/migrator_test.go:135-158`、`backend/migrator/migration/` | **open(debt)**。增量目录已建立，但**没有**任何测试/CI 步骤比对"`LATEST.sql` 全新建库"与"`0001`–`0008` 顺序应用"的最终目录是否一致。 | 加一个测试：两条路径建两个库，diff 表/列/索引/约束；至少做对象与索引的奇偶校验。 |
| E2 | `06 S-Q5` | `backend/migrator/migrator_integration_test.go:1-206` | **open(debt)**。集成文件只有 fresh install/upgrade/legacy adoption；advisory-lock 串行化（`2131420`）与重复版本拒绝都没有测试。 | 加"两个 `MigrateSchema` 并发、一个等待/一个 `lock_timeout` 失败"的集成用例，以及重复版本的单元用例（依赖 B12 先修）。 |
| E3 | `09 M5` | `backend/test/integration/env/testenv.go:88-257`、`schemasync_lineage_{mysql,postgres}_service_test.go` | **open**。harness 与 runner 测试各自重复声明 users/orders/user_order_view 的 DDL（共四处），启动库名与 per-test 库名还不一致。 | 抽一个共享 DDL 常量/helper；删掉未用的启动库。 |
| E4 | `09 LOW-readiness` | `backend/test/integration/env/service_env.go:778-800` | **open**。`waitForServerReady` 对 `/v1/not-found` 只判断 `err == nil`，**5xx 也算 ready**。 | 要求状态码 < 500（这里预期 404）。 |
| E5 | `09 LOW-select1` | `backend/test/integration/runner/schemasync_lineage_postgres_service_test.go:218` | **open**。`require.NoError(t, env.ExecPostgres(ctx, ..., "SELECT 1;"))` 不校验任何场景后置条件。 | 换成对目标后置条件的断言，或删除。 |
| E6 | `09 M6` | `.github/workflows/ci.yml:110-111`、`frontend/package.json:12` | **partial**。前端 job 已跑 `vitest run`，Go job 已有 `-cover`；但 `@vitest/coverage-v8` 不在依赖里，`test:coverage` 无人调用，前端无覆盖率门禁。 | 加依赖 + `--coverage` 步骤（最好带阈值）。 |
| E7 | `09 CI-run` | `.github/workflows/ci.yml` | **partial**。workflow 从未在 GitHub 上真正跑过（仓库内无 run 记录），本地只验证了等价命令。 | 推一次 PR/main 读 Actions 结果；这是流程问题，不是代码问题。 |
| E8 | `01 LOW-ci-artifact` | `.github/workflows/ci.yml`、`Makefile:11-27` | **partial(debt)**。没有任何 workflow 执行 `make build-release` 或 `make build-embed`，仓库也仍无 Dockerfile；`-tags release`/内嵌只有人工验证。 | 加一个 release 模式（最好含 SPA 内嵌）的构建 job/镜像；否则把"部署用 `make build-release`"写进 README 并删掉过期的"无 CI"表述。 |
| E9 | `09 LOW-style` | `backend/api/v1/openlineage_dataset_test.go:17,98,139`、`backend/runner/lineageanalyzer/analyzer_test.go:67`、`backend/migrator/migrator_test.go` | **open**。部分独立测试缺 `t.Parallel()`；`analyzer_test.go` 仍有冗余 `tt := tt`；`migrator_test.go` 用 `t.Fatalf` 15 次未迁到 testify。 | 逐文件规范；无行为风险。 |
| E10 | `09 LOW-metaorder` | `backend/store/meta_resource_test.go:70` | **open**。断言 `[]string{...}` 的**元素顺序**（今天确定性成立，但耦合实现细节）。 | 改集合/包含断言。 |
| E11 | `09 ENG-guard` | `backend/api/v1/common.go:59,76` | **partial**。`convertToEngine`/`convertEngine` 各 5 case，**无测试引用**；`mergeDataSource` 已被 `patchDataSource` 取代且有测试。 | 加表驱动测试：每个引擎值往返，未知值 → `ENGINE_UNSPECIFIED`。 |
| E12 | `09 COV-modules` | `backend/component/dbfactory`、`backend/config`、`backend/bin/server/cmd`、`backend/common/log`、`backend/common/stacktrace`、`backend/utils`、`backend/runner/maintenance`、`backend/plugin/db`(+mysql/pg/util)、`backend/plugin/lineage/catalog`/`model`、`backend/plugin/idp` | **partial**。这些包仍 `[no test files]`（`api/auth`、`server`、`component/state`、`common` 现已补齐）。 | 从纯逻辑开始：`config/profile`、`utils/member`、`dbfactory` 装配，以及 D22 的 registry 缓存。 |
| E13 | `09 COV-store`/`COV-apiv1`/`COV-runner` | `backend/store/`（`column_lineage`/`common`/`environment`/`explain_sql`/`external_dataset`/`group`/`idp`/`llm`/`maintenance`/`namespace_mapping`/`openlineage_run`/`stats`）、`backend/api/v1`（LLMService/ExplainSQLService/GroupService/RoleService handler）、`backend/runner/schemasync/syncer.go:60-260` | **partial**。这些文件/handler/同步主循环仍无直接测试（lineageanalyzer 已补 retry/backoff 两个用例）。 | 按 `meta_resource_query_test.go` 的模式优先补"带范围的 list/delete 查询形状"guard；handler 用 store double；runner 先抽 DB 变更缝。 |

---

## 六、F 类：待确认的产品/部署决策

这些不是代码缺陷，但**决定了上面若干项该不该修、怎么修**。**本轮已全部拍板**：决策卡（现状事实/选项代价/推荐）与完整记录见
[`14-phase8-decisions.md`](14-phase8-decisions.md)。下表保留为决策背景，结论见本表之后的摘要。

| 编号 | 议题 | 现状 | 影响 |
| --- | --- | --- | --- |
| F1 | 单租户，还是需要 per-resource（实例/数据库）授权？ | IAM 只有 WORKSPACE 策略；`instance` 表无 owner 列；`memberBaselinePermissions` 有意给所有已认证用户读基线（`predefined_roles.go:32-52`）。 | 决定 A 类"读路径开放"是否算缺陷。若需要实例边界：要在 IAM 加 owner/scope 维度 + 权限谓词，并收窄读基线。 |
| F2 | gRPC 反射保持匿名还是限权？ | 反射 handler 未传 `handlerOpts`，实际匿名可达；auth 白名单分支是死代码（A7）。 | 限权则给 handler 传 opts + 装配测试；保持匿名则删死分支并写进文档。 |
| F3 | 凭证加密方案：恢复 AEAD+库外密钥，还是接受"同库 XOR"？ | A1；阶段 5 有意回滚，阶段 6 记为"有意接受的已知风险"。 | 决定 A1/C12 是否实施，以及是否需要重建凭证的迁移路径。 |
| F4 | 破坏性 schema 同步：保持仅日志，还是加硬拦截？ | B1；阶段 1 的产品决策。 | 决定是否实现空快照拒绝/收缩阈值/显式确认。 |
| F5 | 视图的列由 sublevel 元资源提供，还是只由 `viewMetadata.columns` 提供？ | B13；前端浏览器走 sublevel，所以视图列页签为空。 | 决定是给视图生成 COLUMN 资源，还是收掉 `getNextLevelObjectType` 的声明。 |
| F6 | `audit_log` 与 `meta_registry_resource_history` 是否需要保留期？ | `runner/maintenance` 只清理 explain/LLM/OpenLineage；这两个表只增不减。 | 决定是否加保留设置 + 清理，或明确"永久保留"。 |
| F7 | 前置反向代理是否规范化/剥离 `Origin`/`Sec-Fetch-Site`/`X-Forwarded-For`？ | CSRF 在头都缺失时判为 trusted（`csrf.go:59-79`），审计 IP 只信 `--trusted-proxies` 名单内的对端。 | 决定是否收紧"双头缺失"兜底、是否要求部署声明可信代理。 |
| F8 | 域白名单（`domains`/`enforce_identity_domain`）要不要暴露？ | C11：有读、无写。 | 暴露就给 setting API + 前端；否则删字段与校验分支。 |
| F9 | ExplainSQL 的 `provider_name` 要不要给 UI 入口？ | 后端已按 provider 解析并进缓存 key；前端封装支持但**没有任何页面传它**（`grep providerName` 只命中生成类型与 `api/explain.ts`）。 | 加 provider 选择器，或把该字段 `reserved`。 |
| F10 | OpenLineage `eventTime` 无时区偏移要不要接受？ | 只用 `time.Parse(time.RFC3339Nano)`，无偏移即解析失败 → `EventTime = NULL`，排序 `NULLS LAST` 且被保留清理豁免。 | 决定"按 UTC 兜底"还是"显式拒绝并回 400"。 |

**决策结论（本轮；完整记录见 [`14-phase8-decisions.md`](14-phase8-decisions.md)）**

| 编号 | 决策 | 对清单的影响 |
| --- | --- | --- |
| F1 | 保持单租户工作区授权 | `04 A-C1`/`04 B-C2`/`02 阶段4-未做`/`03 A-C1` 转为已接受设计（只在文档写明读基线） |
| F2 | 反射保持匿名 + 删死分支 | `A7` 只剩"删 `/grpc.reflection` 豁免分支 + 文档"，不接拦截器 |
| F3 | 保持 `AUTH_SECRET` 种子 XOR | `A1`/`C12` 关闭为已接受风险，只补威胁模型文档 |
| F4 | **保持仅日志**（未采纳推荐） | `B1` 关闭为已接受设计；不加空快照拒绝/阈值/确认开关 |
| F5 | **收掉 COLUMN 声明**（未采纳推荐） | `B13` 改为实现"从 `getNextLevelObjectType` 去掉三类视图的 COLUMN" |
| F6 | **明确永久保留**（未采纳推荐） | 关闭；`audit_log` 与 `meta_registry_resource_history` 永久保留写入文档 |
| F7 | 文档化反向代理契约 | 关闭；部署文档写明代理头契约与 `--trusted-proxies` 要求 |
| F8 | 暴露域白名单 | `C11` 改为实现 v1 字段 + mask + Settings 表单项 |
| F9 | 前端选择器 + **管理员配置的允许 provider 列表**（自定义） | 新增 `C14`：设置字段 + mask + 前端多选 + ExplainSQL 校验；需 schema 增量 |
| F10 | 按 UTC 兜底解析 | 新增 `B16`：无偏移时追加 `Z` 再解析 |

---

## 七、已明确保留（不必重复开启）

以下条目在阶段 5–7 被**有意识地**保留或排除，本清单不把它们重新算作缺陷；若产品方向变化再单独提出：

- **前端默认不内嵌**：默认构建仍"前端单独托管"，`make build-embed` 走 `-tags "release embed_frontend"`（阶段 7 落地）。
- **`metadata` 搜索是子串匹配（trgm 索引）而非全文检索**：FTS 是词元匹配，会改变 `search_objects`/`SearchMetadata` 语义。
- **LLM profile 只有 30s TTL 缓存 + 写侧失效**，没有按需单 profile 加载。
- **`openlineage_run` 默认永久保留**：保留期由 `WORKSPACE_PROFILE.openlineage_retention_days` 控制，默认 0。
- **独立序列 DDL 生成（`CREATE SEQUENCE`）与多文件 SDL 脚手架已按死代码删除**：未来要做多文件 SDL 输出需重新实现。
- **`store.WithCacheDisabled`**（集成 harness 使用）、**`newACLInterceptorWithChecker`**（测试接缝）、**`rolesCache`**（`store/role.go` 在用）等测试/活路径接缝。

本轮决策后又新增以下"已接受"项（**不要重复开启**，结论见 `14`）：

- **单租户/工作区级授权**（F1=A）：成员可读全部实例/数据库/血缘是明确的产品契约，不引入 per-resource IAM。
- **凭证混淆保持 `AUTH_SECRET` 种子 XOR**（F3=A）：同库密钥、无完整性校验、`AUTH_SECRET` 兼作 JWT 签名密钥，均为已接受风险（需在部署文档写明）。
- **破坏性 schema 同步仅日志**（F4=A）：不加空快照拒绝、收缩阈值或确认开关。
- **`audit_log` 与 `meta_registry_resource_history` 永久保留**（F6=A）：不实现保留期/归档。
- **gRPC 反射保持匿名**（F2=A）：删除不可达的豁免分支，并在安全文档写明"反射公开 API schema 定义"。
- **反向代理信任靠部署契约**（F7=A）：要求代理规范化/剥离 `Origin`/`Sec-Fetch-Site`，并把代理地址加入 `--trusted-proxies`，代码不改判定。

---

## 八、建议的下一轮（阶段 8）候选批次

依赖关系：**F1–F10 已全部拍板**（见 [`14-phase8-decisions.md`](14-phase8-decisions.md)），批次可直接执行；每批可独立提交与验证。

**批 1 · 契约与文档一致性（低风险，收益立现）**：
C1（`StoredMetadata` 过滤或删注释）、C4（store 死消息）、C5、C6、C7、C9、C10、C13、D14、D15、D16、D21、B14；
另加两条纯文档/删死分支动作：删反射豁免死分支（F2）、写明单租户读基线与 XOR 威胁模型（F1/F3）。可合并成 3–5 个 commit。

**批 2 · 正确性（不依赖决策的部分）**：
B2（DiffMetadata 输入）、B3（not-found → 404）、B4（ExplainSQL 错误传播）、B5（batch 结果 + 上限）、B6、B7、B8（GUID 分隔符）、
B9（连接限流）、B10、B11、B12（migrator 校验）、B16（eventTime UTC 兜底）。约 8–10 个 commit。

**批 3 · 安全收尾**：A4（driver 错误脱敏）、A5（摄取限流/超时）、A6（base_url 私网 + 存量密钥）、A8（分页 int64）、A9（摄取审计）。
A1/A7 已由决策关闭（A7 只剩删死分支）。约 5 个 commit。

**批 4 · 性能与整洁**：
D1–D13、D17–D20、C2（JSONB 注释，需 schema 增量）、C3（枚举漂移守卫）。约 8–10 个 commit。

**批 5 · 测试与 CI**：
E1（LATEST vs 增量一致性）、E2（migrator 并发/重复版本）、E3、E4、E5、E6、E7、E8、E9、E10、E11、E12、E13、D22。测试批次独立，可与批 4 并行。

**批 6 · 决策后的实现**：
B13（收掉视图 COLUMN 声明，F5）、C11（暴露域白名单，F8）、C14（provider 选择器 + 管理员配置的允许列表，F9）；
另把 F4（破坏性同步仅日志）、F6（audit/history 永久保留）、F7（反代契约）的取舍写进 `AGENTS.md` 与部署文档。

### 批 1 实施记录（已完成）

| 条目 | 落地内容 | 提交 |
| --- | --- | --- |
| `C1` | `ListMetadata`（两条分支）/`SearchMetadata` 跳过 `MetaType_OPENLINEAGE` 行，`GetMetadata` 对其返回 `NotFound`，使 v1 `StoredMetadata` 的契约注释成立 | `21a95a9` |
| `C4` | 删除 store 的 `Position`/`Range`/`SchemaField` 三个零引用消息（含指向已删列的过期注释） | `04b9adc` |
| `C6` | `ListUsersRequest.filter` 示例 `USER` → `END_USER` | `04b9adc` |
| `C7` | `GetUser`/`BatchGetUsers`/`ListUsers` 的 "Any authenticated user can ..." 改为写明所需权限 | `04b9adc` |
| `C5` | 删除 `CreateInstance` 上方残留的 `ListInstanceDatabase` 注释 | `d9b0f16` |
| `C9` | `GetUser` 按 email 查找失败时回显 email 而非 `user 0 not found` | `d9b0f16` |
| `C10` | `pluralize` 修正为按最后一个词变形：`index`→`indexes`、`property`→`properties`、`foreign key`→`foreign keys`（原实现会产出 `indexs` 与 `foreign keies`）；补表驱动测试 | `d9b0f16` |
| `D14` | 删除 `auth_service.go`/`user_service.go` 中引用已删除 `metric` 包的注释块（后者还引用不存在的 `isFirstUser`） | `d9b0f16` |
| `D21` | 删除 `buildMetadataHistoryEventContexts` 中在外层条件内不可达的内层分支 | `d9b0f16` |
| `B14` | `GetNameParentTokens` 拒绝空路径段并返回 `InvalidArgument` 级错误；补测试（`instances//databases/x`、`instances/i/databases/` 现在报错） | `c1b6c21` |
| `D15` | `llm/tools.go` 改用 `common.MetaGUIDSplit` 与 `common.GetSchemaFromGUID`，不再硬编码分隔符 | `c1b6c21` |
| `D16` | 启动横幅改经 `slog.Info`，`--enable-json-logging` 对其生效 | `c1b6c21` |
| `C13` | 新增 `TestStoredMetadataTypesStayWireCompatible`：递归比对 store↔v1 的字段号/kind/cardinality/枚举值（当前全部一致），把转换函数吞掉的 marshal/unmarshal 错误变成测试失败 | `507e06d` |
| `F2` | 删除 `IsAuthenticationAllowed` 中不可达的 `/grpc.reflection` 豁免分支，并在注释/`AGENTS.md` 写明反射匿名 | `4f78442` |
| `F1`/`F3` | `AGENTS.md` 新增「Security and deployment posture」：单租户读基线、凭证 XOR 威胁模型、反射匿名 | `4f78442` |

验证（本地）：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`/`embed_frontend`）、`go test ./...`、
`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`buf format`/`buf lint`/`cd proto && buf generate`（产物可复现）、
`vue-tsc -b`，以及 Docker 集成套件 `go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`
（`runner` 51.6s、`migrator` 12.9s，exit 0）。

### 批 2 实施记录（已完成）

| 条目 | 落地内容 | 提交 |
| --- | --- | --- |
| `B2` | `rebuildSchemaContents` 复制 `SchemaMetadata.Events`/`EnumTypes`；`buildDatabaseSchemaAtTime` 从目标时刻的 DATABASE 注册行恢复 `CharacterSet`/`Collation`/`Extensions`/`Datashare`/`Owner`/`SearchPath`/`EventTriggers`，enum/event/extension/event trigger 的变更不再报 "No changes detected." | `39b2ce5` |
| `B3` | `namespace_mapping`（2 处）、`openlineage_api_key`、`manual_sql`（3 处）、`llm` 的 not-found 改 `common.Errorf(common.NotFound, …)`；`openlineage_service` 的三个 handler 不再强制 `CodeInternal`，交给 `ErrorMappingInterceptor` 映射；`common_test.go` 新增"pkg/errors 包裹不遮蔽 common.Code"用例（实测链路可用） | `ce5a4ff` |
| `B4` | `buildContextFromLineage`/`buildContextFromSQL`/`fetchObjectsByGUIDs` 改为返回 error：store 失败向上传播（handler 回 `Internal`），"对象不存在/无法解析 SQL" 仍是空 context；`toolGetObjectSchema` 查库出错时返回 error 形态的 tool result 而不是 "no object found" | `7ecca87` |
| `B6` | `CreateAuditLog` 不再采用调用方提供的 `CreateTime`，始终 `time.Now().UTC()`（唯一调用方是审计拦截器，本就不设置） | `e7e64f1` |
| `B7` | `UpdateUser` 只要 `PasswordHash` 变化就无条件写 `LastChangePasswordTime`：clone `patch.Profile`（或当前 profile）后打时间戳，不再只在 `patch.Profile == nil` 时设置 | `1c8509b` |
| `B16` | 新增 `parseEventTime`：先按 RFC3339 解析（尊重偏移），失败则追加 `Z` 按 UTC 兜底，二者都失败才告警；补 `TestParseEventTime` | `29a143a` |
| `B12` | `getVersionFromPath` 强制四位数字前缀；`getSortedVersionedFiles` 在任何 DDL 执行前拒绝重复版本；`adoptLegacySchema` 先用 10 个基线哨兵表（`setting`/`policy`/`user_group`/`instance`/`db`/`meta_registry_resource`/`history`/`manual_sql`/`column_lineage`/`audit_log`）校验形状，缺失即拒绝接管；补单元测试 | `2d8e2c2` |
| `B5` | `BatchUpdateInstancesResponse` 由 `repeated Instance` 改为逐项 `BatchUpdateInstanceResult{name,instance,error}`（`instances=1` 保留号并 `reserved`），handler 逐项报错不再遇错即返回；两个 batch RPC 都执行 proto 文档的 1000 条上限 | `e9ce8a5` |
| `B11` | `CreateInstance` 的 admin driver 失败不再被静默丢弃（记 Warn）；**初始同步仍保持同步执行**（见下方偏差说明） | `e9ce8a5` |
| `B10` | 五个 `Get*`（OpenLineage run/task、external dataset、LLM profile、namespace mapping）在匹配多行时返回 `common.Conflict` 而不是静默取第一行 | `ce5a4ff` |
| `B8` | 新增 `common.EscapeGUIDPart`/`UnescapeGUIDPart`/`BuildMetaGUID`/`SplitMetaGUID`；所有 builder（syncer 的多段拼接、manual SQL、lineage 模型、OpenLineage resolver）与 parser（common GUID 读取、lineage analyzer、LLM tools、`buildDatabaseSchemaAtTime`、ExplainSQL scope）统一走它们；OpenLineage GUID 额外把 `:` 编码为 `%3A`（`url.PathEscape` 不转义它）。**对不含分隔符的名字是 no-op，既有 GUID 不变** | `2c26648` |
| `B9` | 限流移入 driver 打开路径：`GetInstanceMeta` 与 `SyncDatabaseSchema` 都 acquire/release `InstanceOutstandingConnections`；checker 不再自己 Increment/Decrement，改为在返回 `errInstanceConnectionsExhausted` 时把数据库重新入队等下一 tick；补限流单元测试 | `2c26648` |
| — | 顺带修掉 `backend/server` 测试的既有 `-race` 竞态（`server_lifecycle_test.go` 的 `configureEchoRouters` 与 `testServers` 的 Once 并发注册 Prometheus，约 1/8 概率失败），改为互斥串行 | `764bc47` |

**批 2 的两处偏差（与原计划不同，已确认合理）**：

- **`B11` 未改为异步**：`SyncAllDatabases` 只入队**已存在**的 `db` 行，而新建实例的数据库正是由 `SyncInstance` 发现的；且实例默认 `sync_interval = 0`，`trySyncAll` 的 `shouldSyncNow` 会显式跳过，因此后台周期扫描不会补做初始同步。改成 goroutine 又缺少生命周期/关停跟踪，故本批只修掉"静默丢错"这一半，保持同步执行；如需异步化，应先给 runner 增加一个受 `runnerWG` 跟踪的入队 API。
- **`B12` 的目录一致性未按原计划收紧为"必须等于基线 `MAJOR.MINOR`"**：保留旧版本行的增量是合法布局（`migrator_test.go` 现即以 `0.0`/`0.2` 目录为有效样例），强行等于基线行会在版本线升级时拒绝历史文件。本批改为严格四位宽度 + 重复版本检测 + 接管哨兵校验；任意 `MAJOR.MINOR` 目录仍被接受，但错版本目录会以"重复版本/账本版本过新"等方式显式失败。

验证（本地，全部通过）：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`/`embed_frontend`）、`go test ./...`、
`go test -race -count=1 ./...`（服务器包连跑 8 次 `-race` 无竞态）、`golangci-lint run --allow-parallel-runners`（0 issues）、
`buf format`/`buf lint`/`cd proto && buf generate`（产物可复现）、`vue-tsc -b`，以及 Docker 集成套件
`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`（`runner` 51.9s、`migrator` 13.3s，exit 0）。

### 批 3 实施记录（已完成）

| 条目 | 落地内容 | 提交 |
| --- | --- | --- |
| `A4` | `CreateInstance` 的 `validate_only` 分支与 `pingDataSource` 在**driver 构造**失败时改为"明细写 `slog`、对外只回 `InvalidArgument: invalid datasource <type>`"，与既有 `Ping` 失败路径一致；不再回传含 SSH `host:port` 的原始错误 | `ac616d7` |
| `A6` | `UpdateLLMProviderProfile` 在**变更 `base_url`** 的请求里要求同时提供 `api_key`（未提供直接 `InvalidArgument: api_key is required when changing base_url`），杜绝把存量密钥静默转发到新端点；`base_url` 未变时行为不变 | `cdf5ec9` |
| `A8` | `store.PageToken` 的 `limit`/`offset` 由 `int32` 改 `int64`；新增 `maxPageOffset = math.MaxInt32` 上界：负 offset 或超界 token 直接 `InvalidArgument`（原实现把负 offset 夹到 0），`getNextPageToken` 在越界时返回空 token（"没有下一页"）而不是回绕到第一页；补表驱动测试 | `2968b88` |
| `A5` | 新增 `backend/server/openlineage_ingestion.go`：摄取路由挂上按**摄取 key 摘要**（无 key 时退回客户端 IP）的令牌桶限流（50 req/s、突发 100、3 分钟过期）与 60s `http.TimeoutHandler` 请求期限；超限返回 429；补中间件测试 | `dd9c32d` |
| `A9` | 摄取 handler 拆出 `handleIngestion`，在返回前写一条审计（`Method` = 请求路径、`Resource`/`User` = ingestion key、按 HTTP 状态映射 severity/status、`LatencyMs`、可信代理下的 `RequestMetadata`）；审计写失败只记日志，不影响摄取；`extractBearerToken` 导出为 `ExtractIngestionKey` 供限流中间件复用；`NewOpenLineageHandler` 增加 `trustedProxies` 参数；补状态映射测试 | `dd9c32d` |

**批 3 的一处偏差（`A6`）**：原计划写的是"非环回主机要求 https 或解析后拒绝私网段"，本轮**没有做 scheme/私网收紧**——自托管场景下 LLM 服务经常就跑在局域网 http（如 Ollama `http://10.x:11434`），拒绝私网或强制 https 会直接破坏该用法（与阶段 0 对实例数据源"放弃内网 deny"的决策一致）。真正的外带路径是"改 URL 后继续用存量密钥"，本批以"改 `base_url` 必须同请求提供 `api_key`"关闭；profile 写操作本就限管理员，叠加此约束后存量密钥不会再被送到调用方新指定的主机。

验证（本地，全部通过）：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`/`embed_frontend`）、`go test ./...`、
`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`buf format`/`buf lint`/`cd proto && buf generate`（仅 `store.PageToken` 相关产物变化，可复现）、
`vue-tsc -b`，以及 Docker 集成套件 `go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`（`runner` 53.7s、`migrator` 14.7s，exit 0）。

> 操作提醒：`buf generate` 的 `clean: true` 会短暂清空生成目录，**不要与 `go test`/`go build` 并行运行**（本轮首次集成运行即因此出现"generated file not found"的假失败，串行重跑即通过）。

---

## 九、核对更正（旧标记为什么不能直接信）

1. **`04` 的 `B-M15` 与 `03` 的 `M16` 是同一件事，且都未修**：`namespace_mapping.go`/`openlineage_api_key.go`/`llm.go` 的 not-found 仍走裸
   `errors.Errorf` → 500；`04` 报告写 "M15 ✅ 阶段 3"，只对 `store/database.go`、`store/setting.go` 成立。
2. **`04` 的 `A-H3` 标 ✅ 不成立**：`rebuildDatabaseObjects` 的类别变多了，但 enum types/events/extensions/event triggers 在重建时**没有输入**，
   摘要计数函数已准备好却永远收到 0。
3. **`P-H6` 被一份报告判为已修、另一份判为 partial**：以代码为准——v1 `StoredMetadata` 注释承诺的 OpenLineage 过滤**没有实现**。
4. **`05`/`02` 的 `metric` 死代码指控已过时**：`backend/metric/`、`backend/plugin/metric/` 早已随阶段 3 删除；残留的只是两个注释块（D14）。
5. **`06` 的 "RETURNING 行序" 待确认已解决**：store 现在按 `(guid, object_type)` 配对返回行，不再按下标。
6. **`06` 的 "runner 注入 profile 但从不读" 已过时**：阶段 7 已删除 `Syncer.profile`/`Analyzer.profile` 与构造参数；硬编码常量是另一件事（D9）。
7. **`01 H3` 的"前缀不匹配导致反射绕过认证"指控是错的**：`connectrpc.com/grpcreflect v1.3.0` 的 v1/v1alpha 路径确实以 `/grpc.reflection` 开头。
   真正的问题是反射 handler **没接拦截器**（A7）。
8. **`06 Low-get-version-path` 的"负数前缀"指控是错的**：`migration/0.1/-1##x.sql` 会因 `semver.Parse` 失败而被拒；真实缺陷是**宽度与目录不校验**（B12）。
9. **`09` 的 "registry 缓存由集成路径覆盖" 是错的**：全仓库（含 `_test.go` 与 `test/`）都没有 `ListEnabled`/`Invalidate` 的测试（D22）。
10. **README 阶段 6 仍写着 "MARIADB/OCEANBASE 的 plugin 覆盖缺口"**：阶段 3 补遗 `729db71` 已让 driver/schema/lineage/OpenLineage resolver
    四个注册表都覆盖 MYSQL/TIDB/MARIADB/OCEANBASE；该句应视为过期。
11. **本地 `go test ./...` 的"失败"多数是环境问题**：本机 `GOCACHE` 只读会让部分包 setup 失败；用 `GOCACHE=$PWD/build/.gocache` 后阶段 7 全绿。

---

## 十、验证建议（阶段 8 每批的验收线）

沿用阶段 6/7 的本地全量门禁：

- `gofmt -l backend/` 空；`go build ./...`；`go vet ./...`（默认/`release`/`integration`/`embed_frontend`/`release embed_frontend`）。
- `go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（跑到 0 issues）、`make build-release`。
- 任何 proto 改动：`buf format -w proto`、`buf lint proto`、`cd proto && buf generate`，并确认重跑无 diff；提交生成产物。
- 任何 schema 改动：同时改 `LATEST.sql` 与 `migration/0.1/{NNNN}`，并在本地 PostgreSQL 实测全新安装 / 增量升级 / 重复执行幂等。
- 前端：`biome check src`、`eslint src --max-warnings=0`、`vue-tsc -b`、`vitest run`、`vite build`。
- 触及同步/血缘/装配：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`（本机 Docker 可用）。
- 触及 A2（吊销）/A5（限流）/A6（私网）/B1（破坏性删除）等行为项时，补**真实 server 集成用例**或最小复现测试后再声明完成。
