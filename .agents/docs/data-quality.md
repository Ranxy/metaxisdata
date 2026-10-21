# 数据质量（Data Quality）— Reference

> Status: **implemented**（v1 与 v1.1 全部落地；单副本假设）。维护参考，覆盖 `backend/component/dataquality/`、`backend/runner/qualityrunner/`、`backend/api/v1/data_quality_*.go`、`backend/store/quality_*.go`、`frontend/src/pages/data-quality/`。用户侧说明见 [docs/data-quality.md](../../docs/data-quality.md)；刻意决策见 [security-posture.md](../../docs/security-posture.md)。
> 关联：[instance-connections.md](instance-connections.md)（内部批次的连接准入）、[runner.md](runner.md)（后台执行器契约、单副本）、[notifications.md](notifications.md)（站内信与结构化 detail）、[iam-and-permissions.md](iam-and-permissions.md)（权限目录/注解）、[rate-limiting.md](rate-limiting.md)（principal 预算）、[object-ddl-storage.md](object-ddl-storage.md)（DDL 读取）、[lineage-analyzer.md](lineage-analyzer.md)（血缘上下文）、[frontend-ui.md](frontend-ui.md)（骨架/布局门禁）、[i18n-glossary.md](i18n-glossary.md)（术语）、[security-open-items.md](security-open-items.md)。规则：[backend/AGENTS.md](../../backend/AGENTS.md)、[proto/AGENTS.md](../../proto/AGENTS.md)、[backend/migrator/AGENTS.md](../../backend/migrator/AGENTS.md)。

## 0. 它是什么

规则引擎负责**确定性判定**“数据是否达标”；LLM 只做三件辅助——把自然语言变成**规则草稿**、把失败结果**解释**成假设、把字段画像变成**语义标注与规则建议**。LLM 不执行判定、不写回数据、不自动建规则/启用/执行。

**对外唯一的执行实体是 `QualityExecution`（一条规则被检查一次）**。`quality_run` 退为**内部派发批次**：它只负责按实例领一份连接预算、聚合一个 tick 的规则、合并风暴通知并级联保留期，没有资源名、RPC 或页面。旧的 `quality_rule_result` 已并入 `quality_execution`。

```
SPA ──ConnectRPC──► DataQualityService (api/v1) ──► component/dataquality（校验/编译/判定/画像/CEL）
                               │                        │
                               │  手动运行/定时扫描：规划 PENDING quality_execution 行，按实例组成内部批次
                               ├──► store（PG：quality_* 9 张表）◄── qualityrunner（tick 10s / 手动队列）
                               │        quality_run = 内部派发批次（不对外）；quality_execution = 对外执行
                               ├──► component/llm（L1/L2/L3，复用 provider）
                               └──► notification.Service（问题迁移 → 站内信）        │
                                                     qualityrunner ──► db.ReadOnlyQueryer ──► 目标库（READ_ONLY 数据源）
```

## 1. 决策（D-01…D-35 的有效结论）

需求稿的早期编号 D1–D24 与下表重复，已并入、不再单列。

| # | 决策 | 后果 / 边界 |
| --- | --- | --- |
| D-01 | 领域语义集中在 `backend/component/dataquality`；API 与 runner 只做编排 | 可 golden、可单测，不依赖 HTTP/DB 细节 |
| D-02 | 目标库查询走新增可选接口 `db.ReadOnlyQueryer`，业务层不碰 `driver.GetDB()` | MySQL 家族/PG/MSSQL/StarRocks 各实现；不实现该接口的引擎执行期 `ERROR(UNSUPPORTED)` |
| D-03 | 查询型规则**强制** `READ_ONLY` 数据源，缺失不回退 ADMIN | 预览/执行拒绝并给出配置入口；创建期不校验（见 §7） |
| D-04 | `filter` 是结构化 AND 子句（列/比较符/值），不是 SQL 片段 | 10 种 op、≤10 子句、IN ≤100；复杂条件用 `custom_sql` |
| D-05 | 每次执行一个六态结果；内部批次的 outcome = 最坏 rank，仅供运行时统计；`ERROR` 不建问题、走独立告警 | `ERROR` 与 `FAIL` 永不合并；“没数据”默认不当作 PASS |
| D-06 | 内部派发批次 = 一个实例的一批规则（定时按实例分批 ≤50，手动按选择集分组）；对外的执行 = 一条规则被检查一次 | 连接预算与失败合并以实例批次为单位；批次不对外 |
| D-07 | 规则 head 与不可变 revision **同事务**双写；执行记 `rule_revision` | 编辑不改历史判定 |
| D-08 | 同一规则至多一个未解决 issue（部分唯一索引 `status <> 3`） | 并发/重试不会开两个问题 |
| D-09 | 告警只用站内信：`NOTIFICATION_TYPE_DATA_QUALITY` + `store.DataQualityDetail` | 无邮件/webhook；正文由前端 i18n 渲染 |
| D-10 | DQ 设置放 `WorkspaceProfileSetting.data_quality`（store 与 v1 两份 proto） | 复用 `settings.get/update` 与设置页，不新增 settings RPC |
| D-11 | 权限目录新增 `metaxisdata.dataQuality.*`（15 项）；`workspaceMember` 基线加 7 项只读+explain，并含 `users.list`（成员目录） | ACL 拦截器逐 RPC 校验；owner 是展示、通知路由、规则列表筛选与负责人列；目录只读，供选择器与列解析成员 |
| D-12 | LLM 复用 `component/llm`：补 temperature/JSON/usage、共享 provider 解析；不另建 LLM 栈 | `ParseJSON[T]` 严格解码，ExplainSQL 回归绿 |
| D-13 | 字段画像在 API 请求内同步执行（列数取设置、120s、只读源），不建 job 表 | 长任务不进 API，由上限保证 |
| D-14 | CEL 是规则类型：Go 内求值、采样级、不生成 SQL | 方言无关；精确需求用 `custom_sql` |
| D-15 | 前端第 6 个顶层导航 + 5 个入口子页（概览/规则/执行/问题/建议）；规则另有独立详情页；趋势图与最近执行用内联 SVG/状态色块条（`QualityTrendChart`、`QualityResultStrip`） | 修订 frontend-ui 的顶层导航约定；布局审计登记路由（详情页为动态 id，与执行/问题详情一样不登记） |
| D-16 | 保留期：执行随内部批次 90d、关闭问题 90d、画像/LLM 缓存 7d、已决建议 90d；由 maintenance runner 每 6h 清理 | 删除批次级联删除其执行；只清已解决的问题与已决建议 |
| D-17 | 不新增运行时依赖：CEL 用 `cel-go`，SQL 校验用 `omni`，图表自绘 | 保持单二进制 |
| D-18 | 调度 = 进程内 tick + inflight map + 表内 `next_run_at` | 单副本；重启收敛、不补跑 |
| D-19 | count 型规则公共 `threshold_metric`（COUNT/PERCENT） | PERCENT 仅 10 类（§3.2）；`null_percent` 用它自己的 `max_null_count`/`max_null_pct` 二选一 |
| D-20 | RuleSet（contract-like 整体验证）后续再做 | v1/v1.1 不引入该实体；前置能力见 §8 非目标 |
| D-21 | `custom_sql` 是缺口逃生口：人写单条 SELECT/WITH ≤8 KiB | 只读、超时/上限、审计；反复出现的形态应升格为规则类型 |
| D-22 | 统一 `fail_threshold`/`warn_threshold`（optional double），单位由 `threshold_metric` 定 | 默认 fail=0、warn 可选；比较严格（等于阈值通过） |
| D-23 | 执行拆 `state`（PENDING/RUNNING/COMPLETED/ABANDONED）+ 六态 `outcome`（完成前 `UNSPECIFIED`）；内部批次另有同形 lifecycle/outcome/counts，不对外 | 前端能区分“排队/执行中/已完成/已放弃”；列表可按 state 与 outcome 过滤 |
| D-24 | `target_schema_changed` 用 registry **schema_hash**（`ListMetaRegistrySchemaHashes`）口径；规则保存时写同一口径 | 路径快照仅展示；两套 hash 混用会让“结构已变”永真 |
| D-25 | 手动运行的规则集不进队列参数：API 先建内部批次并为每条选中规则规划 PENDING 执行行，再经进程内队列 `EnqueueQualityExecutions(batchID, instance, executionIDs)` 交给 runner；runner 只执行这些行 | 队列容量 64 个批次；满则放弃批次及其执行（ABANDONED、无 outcome）并返回 `ResourceExhausted` |
| D-26 | 设置零值=默认；`data_quality` message 存在性区分“未配置”和显式 false；返回生效值不落库默认 | 0 是“取默认值”，**不是**“不限制/不存”（§4.1） |
| D-27 | 通知：issue opened/recovered 按 issue 资源名 1h 去重、收件人 owner→admins；execution-failed 只发 admins 且不受静音；同一内部批次内 FAIL 执行 >10 合并 STORM_SUMMARY（每批次一条，去重 key 用内部批次 id，detail 带实例与计数）；恢复不受 storm 抑制 | 静音只影响问题通知；风暴仍创建 issue，只抑制逐条消息 |
| D-28 | 执行资源名最后一段 = `quality_execution` **行 id**（该表无 resource_id）；`ExplainQualityExecution` 按其查 `store.GetQualityExecution` | 列表、问题引用的 first/last seen 与解析同一 id |
| D-29 | L2 先收集完整 JSON、解析后再一次性发 content；progress 一帧 + 终帧 metadata；usage 只入缓存行与日志 | `ExplainSQLMetadata` 无 usage/prompt_version 字段，不为统计改协议 |
| D-30 | L1/L3 只产出 PENDING 建议；接受 LABEL 建议即写 `column_semantic_label`；接受 RULE_DRAFT 只改建议状态 | 规则由编辑器保存才创建；不自动建/启用/执行 |
| D-31 | v1.1 第一批：`schema_drift`（R15，元数据比对）与 `group_check`（R16，每组行数阈值） | 最小可解释语义，不做通用 DSL |
| D-32 | `schema_drift` 不取只读源、无 SQL（`QueryMetadata`）；`sampled=false`、样本是漂移项列表；`group_check` 走普通只读查询 | 元数据规则不能因没配只读源而阻塞 |
| D-33 | v1.1 第二批：`aggregate`（R17）、`cross_table`（R18）、`null_percent.null_values`；PERCENTILE 仅 PG/MSSQL，其它引擎保存期拒绝；cross_table 限同实例同库 | 不引入跨库/跨实例；复杂对账仍用 `custom_sql` |
| D-34 | 画像的聚合事实**由同一张类型能力表推导**（D-35），不再单独维护白名单：`COUNT(DISTINCT)`/`MIN`/`MAX`/长度分别取 `countDistinct`/`extrema`/`length`；非空计数优先 `COUNT(col)`，只有 `noColumnCount`（MSSQL text/ntext/image）才用 `COUNT(CASE WHEN col IS NOT NULL …)`。distinct 无精确能力时取自样本并标 `distinct_estimated`；样式只对 `text` 类型取；`binary` 类型（MSSQL binary/varbinary/image/uniqueidentifier/hierarchyid、MySQL binary/varbinary/blob/空间、PG bytea、SR varbinary、bitmap/hll、Doris bitmap/hll）的极值与 Top 值**一律**以 `0x…` 展示（既避开 protojson 的 UTF-8 限制，也避免字节串与 ASCII `0x…` 碰撞）。PG 数组按**元素的 btree 排序能力**推导（仅“可比较”不够：`box[]` 会报 `could not identify a comparison function`），表数组 `_int4`、物化视图 `integer[]`/`character varying[]` 一致处理；PG 数组统一按 String 归类以便绑定数组字面量 | `MIN(jsonb)`/`CHAR_LENGTH(jsonb)`/`COUNT(text)`/`COUNT(CASE … bitmap)` 这类整表语句错误不再出现；jsonb/uuid/数组保留精确 distinct；MSSQL binary 类、MySQL JSON 保持可测统计；macaddr/macaddr8 不再伪造 MIN/MAX；Doris DECIMAL（`decimalv3`）归为 numeric |
| D-35 | 规则保存期按**逐项能力**校验，而不是只比 `ColumnKind`：`comparison`（filter/accepted_values/null_values/referential join）、`grouping`（unique/unique_combination/group_check 的 GROUP BY）、`countDistinct`（画像精确 distinct）、`regex`、`length`、`extrema`（MIN/MAX）、`text`（画像样式），外加 `noColumnCount`/`noNullTest` 两个否定例外，按引擎族维护**允许表**（未列出即拒绝：保存期报错，而不是运行期整条 SQL 失败）。普通 `null_percent`、`IS [NOT] NULL` filter 需要 NULL 判定能力，`unique`/`unique_combination` 在 `count_nulls=false`（会生成 `IS NOT NULL`）时也需要，`group_check` 不需要；只有 StarRocks bitmap/hll/percentile 拒绝（Doris 4.1.4 实测可以 `IS NULL`，故不设该限制）。`comparison` 表示列与列可比较（referential join）；filter/accepted_values/null_values 另外要求 `valueComparison`（标量值可绑定），**数组只能列与列比较**，不能绑定标量。PG 的 schema-qualified 用户自定义类型保留历史规则允许面（enum 的 `= $1`/GROUP BY 可用；composite 谓词绑定是已记录的前置限制），画像只数非空、不声称精确 distinct/MIN/MAX；`referential_integrity` 还要求两侧类型兼容；`cross_table` 保存期解析右表（TABLE/EXTERNAL_TABLE/VIEW/MATERIALIZED_VIEW），**任何非 COUNT 聚合列（左右两侧）必须是 numeric**；`aggregate` 的 MIN/MAX 同样只接受 numeric（观测值按数值读取，时间戳会 BAD_DATA），PG money 只允许 SUM/MIN/MAX（AVG/STDDEV/PERCENTILE 引擎未定义），money 字符串按“最后一个分隔符是小数点”解析（兼容逗号小数 locale） | PG 的 json/xml/几何类型不再被生成不可执行的 SQL；`box` 允许比较但不允许分组，`box[]`/`line[]` 等几何数组直接零能力；`macaddr/macaddr8` 比较/分组/`COUNT(DISTINCT)`（无 MIN/MAX）；MSSQL text/ntext/image 不再被 COUNT/LEN/比较、binary/uniqueidentifier/hierarchyid 统计完整且按 hex 展示；StarRocks JSON 允许比较、拒绝 GROUP BY/REGEXP，bitmap/hll 拒绝 NULL 判定但保留 `COUNT(DISTINCT)` 画像；Doris JSON 允许 GROUP BY/REGEXP/长度、拒绝比较与 COUNT(DISTINCT)；MySQL JSON 比较被拒（绑定字符串永不等于 JSON 值），MySQL 空间类型的 REGEXP/长度按 WKB 字节解释因而拒绝；PG `bit` 按位串（String）而非布尔归类，参数可绑定；PG money 字符串（`$1.23`）在聚合观测里剥离货币符号后解析 |

## 2. 工作负载形状

### 2.1 实体与资源名

```
meta_registry_resource（只读）──< quality_rule >──< quality_rule_revision（不可变）
                                        │
                                        ├──< quality_run（内部派发批次，不对外）──< quality_execution（对外执行，一条规则一次）
                                        │                                              │
                                        │                                              └──< quality_issue（每规则至多一个未解决）
                                        └──< quality_suggestion（待审建议）      column_profile / column_semantic_label（按表 GUID+列名）
```

| 资源 | 名称 | id 来源 |
| --- | --- | --- |
| 规则 | `workspaces/{ws}/qualityRules/{quality_rule}` | `common.RandomString(12)`，不可回收 |
| 执行 | `workspaces/{ws}/qualityExecutions/{quality_execution}` | **数据库行 id**（BIGSERIAL 数字） |
| 内部批次 | 无 API 资源名；`quality_run.resource_id` 仅供内部诊断 | `common.RandomString(12)` |
| 问题 | `workspaces/{ws}/qualityIssues/{quality_issue}` | `common.RandomString(12)` |
| 建议 | `workspaces/{ws}/qualitySuggestions/{quality_suggestion}` | `common.RandomString(12)` |
| 目标 | registry GUID + `MetaType`（`(guid, object_type)` 成对） | 路径快照只用于展示/重绑 |

`parent` 接受 `workspaces/-`（当前工作区）；资源名里的 workspace 段必须是部署的唯一工作区，否则 NotFound。

### 2.2 状态模型

**执行结果六态**（优先级 rank：`ERROR 5 > FAIL 4 > WARN 3 > NOT_EVALUATED 2 > SKIPPED 1 > PASS 0`，不是枚举值大小）：

| 状态 | 含义 |
| --- | --- |
| `PASS` / `WARN` / `FAIL` | 已执行：在阈值内 / 越过 warn 未越 fail / 越过 fail |
| `ERROR` | 未完成判定：SQL/连接/权限/超时/编译失败；监控自身故障，不建问题 |
| `SKIPPED` | 有意不执行：目标 orphaned、规则已归档、同规则并发执行中 |
| `NOT_EVALUATED` | 已执行但无可判定数据：过滤后 0 行、`MAX(ts)` NULL、聚合 NULL、无基线 |

执行生命周期：`state ∈ {PENDING, RUNNING, COMPLETED, ABANDONED}`。`outcome`（六态）只在 `COMPLETED` 时存在；`ABANDONED` 从不产生判定（重启丢弃、准入超时、批次超时/未为全部执行落终态、写失败等）。启动时把超过 15 分钟的 PENDING/RUNNING 执行与批次标 `ABANDONED`（不补跑）。内部批次另有同形的 lifecycle/outcome/counts（`outcome` 为其已完成执行的最坏 rank），从不对外。

**问题生命周期**（由 `dataquality.PlanIssueTransition` 决定，判定在执行终态写入之后执行）：

| 本次执行结果 | 既有 open issue | 动作 | 通知 |
| --- | --- | --- | --- |
| `FAIL` | 无 | 新建 `OPEN`（first/last seen = 本次执行） | ISSUE_OPENED（除非静音/风暴） |
| `FAIL` | 有 | TOUCH：last_seen/执行、occurrence_count++ | 1h 去重窗口内不重复 |
| `PASS` 且上次执行 `FAIL` | 有 | `RESOLVED(AUTO_RECOVERED)` | ISSUE_RECOVERED（除非静音） |
| `PASS` 且上次不是 `FAIL`（如 `ERROR→PASS`） | 有 | 不动 | — |
| `WARN` / `NOT_EVALUATED` / `SKIPPED` 且上次 `FAIL` | 有 | TOUCH（不自动恢复） | — |
| `ERROR` | 任意 | 不动 | EXECUTION_FAILED（admins，不受静音；风暴时并入摘要） |

规则生命周期：`ENABLED`（创建即启用，首次运行在下一排期）↔ `PAUSED`（人工/资产 orphaned）→ `ARCHIVED`（软删除，历史保留）。更新 schedule 或重新启用时才重算 `next_run_at = now + interval`；暂停不动它。

## 3. 核心机制

### 3.1 规则定义与 revision

- 创建/更新走同一校验链：`ResolveTarget`（registry 当前行 + 实例未删 + 数据库未隐藏 + 列存在）→ `ValidateConfig`（按类型的参数校验 + 列类型/能力校验 + 引用父表校验）→ `ValidateRule` 的 registry 步骤（referential 父表、cross_table 右表）→ `ParseFilter` → `Compile`（SQL/占位符/args/列名）→ 保存时写 `target_meta_hash`（schema_hash 口径）。
- 语义变更（类型、目标、config、severity/dimension 等）产生新 revision；head 与 revision 在一个事务里写，`SELECT … FOR UPDATE` 串行化并发编辑（`ExpectedRevision` 不匹配 → `Conflict` → 客户端 `Aborted`）。
- 执行行先以 PENDING 规划、执行期 RUNNING、终态一次写入判定与证据；问题状态是可变列；删除规则 = `ARCHIVED`，历史执行/问题保留。
- 干跑 `PreviewQualityRule`：校验 + 编译 + （非元数据规则）只读源 `LIMIT 0 / TOP 0` 探测（10s 超时），不写库、不产生执行行或批次。元数据规则（`schema_drift`）跳过探测。探测与执行走同一个实例连接预算（[instance-connections.md](instance-connections.md)）。
- 保存时只校验/编译，不校验只读源是否存在；`schema_drift` 在缺失快照时由 API 用当前列填充。

### 3.2 编译、方言与判定

规则类型 18 类（v1 13 类 + CEL + v1.1 四类）：`row_count`、`row_count_change`、`null_percent`、`unique`、`unique_combination`、`accepted_values`、`range`、`regex`、`string_length`、`referential_integrity`、`cross_column`、`freshness`、`custom_sql`、`cel_expression`、`schema_drift`、`group_check`、`aggregate`、`cross_table`。

- 编译器表 `map[storepb.QualityRuleType]compilerFunc` → `CompiledQuery{SQL, Args, Kind, Columns}`；`Kind` 为 `Aggregate`（一行聚合）、`FailedRows`、`Sample`（样本行）、`Metadata`（无 SQL，元数据比对）。
- 方言矩阵：MySQL 家族/TiDB/MariaDB/OceanBase/StarRocks/Doris（反引号、`?`、`LIMIT`、`REGEXP`、`CHAR_LENGTH`）、PostgreSQL（`"`、`$n`、`~`/`!~`）、MSSQL（`[ ]`、`@pN`、无正则、`LEN`、`OFFSET … FETCH`、`TOP 0` 探测）。表名按方言 `db.table` / `schema.table` 限定；所有标识符 quote，所有值参数化。
- 类型能力（`capabilities.go`）：`ColumnKind` 只决定字面量怎么绑定，还不足以决定能生成哪些 SQL。能力按 `comparison`/`grouping`/`countDistinct`/`regex`/`length`/`extrema`/`text` 逐项维护，并有两个否定例外 `noColumnCount`（COUNT(col) 非法）与 `noNullTest`（IS NULL 非法）；表是**按引擎族的允许表**，未列出的类型视为零能力（保存期拒绝，而不是猜一个可能在引擎上不存在的操作）。校验与编译器共用 `requireComparisonColumn`/`requireValueComparisonColumn`/`requireGroupingColumn`/`requireRegexColumn`/`requireLengthColumn`/`requireNullTestColumn`；画像用 `capabilitiesFor` 推导 facts 与非空计数方式，因此两者不会漂移。普通 `null_percent`/`IS [NOT] NULL` filter 需要 NULL 判定能力，`unique`/`unique_combination` 在 `count_nulls=false` 时也需要（`group_check` 不需要）；filter/accepted_values/null_values 还要求 `valueComparison`（数组不可绑定标量）。`cross_table` 与 `aggregate` 的非 COUNT 聚合列必须是 numeric；`money`/`smallmoney` 列只允许 SUM/MIN/MAX（PG money 引擎本身如此，校验对所有引擎保守执行）。PG schema-qualified 用户自定义类型保留历史规则允许面（composite 谓词绑定为已记录限制）。
- 聚合行扫描按 `CompiledQuery.Columns` 取列：缺列/类型不符/行数≠1 → `BAD_DATA`；空集 `SUM` 为 NULL 时按 0 处理（count 型）或按 `on_empty`（aggregate/cross_table 用 `ObservedNull`）。
- 判定（`Evaluate`）：阈值严格比较（等于不越界）；`null_percent` 用 `max_null_count` 或 `max_null_pct` 二选一；`row_count` 用 min/max/exact + warn；`freshness` 的 lag 在 SQL 内用数据库时钟算（PG `EXTRACT(EPOCH …)`、MySQL `TIMESTAMPDIFF`、MSSQL `DATEDIFF(SECOND, …, SYSUTCDATETIME())`）；空数据由 `on_empty ∈ {NOT_EVALUATED(默认), PASS, FAIL}` 决定并写入 `QualityThresholds` 快照。
- `row_count_change`：基线 = 本规则最近一条 PASS/WARN/FAIL 执行的 `observed_value`（无 7 天/间隔窗口，见 §7）；`baseline` 为空或 0 → `on_empty`；变化比例与 `max_increase_pct`/`max_decrease_pct` 比较。
- `aggregate`：观测值 = 聚合值，`total_rows = COUNT(*)`；区间判定（默认开区间，`inclusive` 改闭）；NULL 聚合 → `on_empty`；`PERCENTILE` 仅 PG/MSSQL；`COUNT` 不是聚合规则的函数（与 `row_count` 重复），保存期和执行前 `ValidateConfig` 都拒绝，因此存量 COUNT 聚合规则会得到 `ERROR(UNSUPPORTED)`。
- `group_check`：`total_rows` = 组数、`failed_rows` = 越界组数；阈值按 COUNT 或 PERCENT；样本返回违规组（分组列 + 组行数）。
- `cross_table`：左右各一个 COUNT/SUM/AVG/MIN/MAX（不同实例/库不支持）；只应用左表 filter；`observed = |left-right|`，判定用 `max(0, |diff| - tolerance)` 与 fail/warn 阈值（`>`）；任一侧 NULL → `on_empty`。
- `cel_expression`：Go 内求值；SQL 只取 `SELECT <引用列> FROM <t> [WHERE filter] LIMIT sample_rows`（默认 1 万、上限 10 万行，列 ≤20，表达式 ≤4 KiB）；表达式只能引用目标列（名字必须是 CEL 标识符）；`sampled=true`；`matches()` 模式必须是常量；引用 `money`/`smallmoney` 列在保存期拒绝（值以货币文本渲染，无法稳定读成数值）；数字文本先按普通小数解析，货币文本仅在无歧义时接受（`$1,234.50`、`($1.23)`），`1,5`、`1 234,50` 这类歧义写法按 `BAD_DATA` 处理。
- `schema_drift`：把 registry 当前列与保存的快照比对，漂移项 = 缺失列 / 类型或可空性不符（`check_types`，未设置时默认 true；可空性漂移项为 `nullable: NOT NULL -> NULL` 或反向）/ 顺序不符（`check_order`，默认 false）/ 多余列（`allow_extra_columns=false` 时）；可空性只在快照记录了该列的可空性时比较（`optional bool`；未设置 = 不判断，因此旧快照不会产生假漂移），编辑器和 API 的填充路径总会写入；`total_rows = len(expected_columns)`、`failed_rows = 漂移项数`、样本是漂移项列表；无 SQL、无只读源。
- `custom_sql`：人写的单条 SELECT/WITH（≤8 KiB，omni 解析校验）；执行 `SELECT COUNT(*) FROM (<sql>) dq`，样本 `SELECT * FROM (<sql>) dq LIMIT n`；无 `total_rows`，因此拒绝 PERCENT。SQL 本身负责 filter，DQ 不重写。
- **PERCENT 允许集（10 类）**：`unique`、`unique_combination`、`accepted_values`、`range`、`regex`、`string_length`、`referential_integrity`、`cross_column`、`group_check`、`cel_expression`；其余（`custom_sql`、`freshness`、`row_count`、`row_count_change`、`null_percent`、`schema_drift`、`aggregate`、`cross_table`）保存期拒绝。`null_percent` 不读取共享 `threshold_metric`（其 config 用 `max_null_count`/`max_null_pct` 自选单位），提交 PERCENT 会被拒绝并指向自己的字段。
- 对象类型：TABLE/EXTERNAL_TABLE 全类型；VIEW/MATERIALIZED_VIEW 只允许表级规则（`row_count`、`row_count_change`、`freshness`、`custom_sql`、`schema_drift`）。
- 执行 SQL 截断 8 KiB；样本值截断 256 字节；样本行数默认 20、上限 100（工作区设置可下可上，硬上限 100）；单次执行结果内存上限 1 MiB。

### 3.3 执行与调度（`backend/runner/qualityrunner`）

| 常量 | 值 | 含义 |
| --- | --- | --- |
| tick | 10s（不 jitter） | 到期扫描 |
| 每轮 due 扫描 | ≤200 条；每个内部批次 ≤50 条执行 | 分批 |
| 并发 | 至多 2 个实例同时执行；单实例的批次串行 | 进程内 |
| 连接准入 | scheduled 等待 ≤2m；manual 等待 ≤5m | 批次开始前向实例连接预算领 lease；scheduled 拿不到则不建批次、规则保持 due；manual 拿不到则把已规划的执行与批次标 `ABANDONED`（无 outcome），不写 `ERROR(QUOTA)`（[instance-connections.md](instance-connections.md)） |
| 手动队列 | 64 个批次 | 满则 API 放弃该批次及其执行并返回 `ResourceExhausted` |
| 单批次超时 | 15m | 覆盖批次内所有执行与查询；超时后未开始的执行与批次标 `ABANDONED`，不写部分 rollup |
| 单次执行查询超时 | 60s（固定；无按规则覆盖） | `QueryOptions.Timeout` |
| 执行结果/样本上限 | 1 MiB / 20 行（设置） | `ExecOptions` |
| 风暴阈值 | 一次内部批次 FAIL 执行 >10 | 合并通知 |
| 启动清扫 | 15m 前的 PENDING/RUNNING 执行与批次 → ABANDONED | 不补跑 |
| 每日执行上限 | UTC 日起算 `COUNT(quality_execution)`（含手动规划的行） | 触顶**只跳过定时 due 扫描**，手动运行仍可执行 |

- 循环：先排空手动队列，再处理 tick；每次扫描/手动前重读设置（读失败保留旧值）。
- 每日执行上限：触顶时跳过本轮 due 扫描（规则保持 due，次日继续），不写 ERROR、不告警（§7）。
- 定时：到期规则按实例分组、按 ≤50 切内部批次；每个批次先落 PENDING 批次行并规划每条规则一条 PENDING 执行（revision、target 快照、实例、trigger、started_at），规划成功后才推进 `next_run_at`（失败不回退、规则保持 due），然后执行。
- 手动：API 解析选择集（规则列表/实例/目标）、按实例分组，每实例建内部批次并规划 PENDING 执行（trigger MANUAL、initiator、目标快照来自 head revision），再入队 `EnqueueQualityExecutions(batchID, instance, executionIDs)`；runner 只执行这些行，绝不扩选。队列满 → API 放弃批次与执行并返回 `ResourceExhausted`。
- 逐条执行：批次标 RUNNING → inflight 去重（重复触发记 `SKIPPED`）→ 读最新 revision 快照（head revision 不一致时按最新执行，终态记实际 revision）→ resolve target（孤儿 → `SKIPPED(ORPHANED)`）→ `RequireTarget`/`ValidateConfig`（不满足 → `ERROR(UNSUPPORTED)`）→ 元数据规则跳过连接；其余在批次持有的实例 lease 下 `GetReadOnlyDatabaseDriver` → `Executor.Execute` → `row_count_change` 补基线 → `Evaluate` → `FinishQualityExecution`（终态、六态、观测/阈值/行数/样本/SQL/错误/耗时）。每条执行是独立提交点。
- 单条执行 panic 隔离为 `ERROR(EXECUTION)`，其余继续；终态写失败、批次超时或任一任务未能产出已写入的判定（读取/标记失败、行已被别的写入者终结）→ 放弃该批次剩余执行（ABANDONED）并以 ABANDONED 结束批次，绝不把部分 rollup 标成 COMPLETED；已写入的判定仍会迁移问题并发送逐条通知，但该批次不发风暴摘要。
- 查询阶段结束（最后一条执行完成）立即释放 lease；随后按批次已写入的判定做问题迁移（`PlanIssueTransition`，用该规则上一条终态执行）与通知（best-effort），最后写批次 counts/outcome（内部）并 `COMPLETED`；风暴摘要等批次完成后再发。
- 只读通道：`db.ReadOnlyQueryer`；PG 事务内 `SET LOCAL statement_timeout`，MySQL 会话 `MAX_EXECUTION_TIME` 并复位；其余依赖只读账号 + 语句白名单。无 READ_ONLY 数据源 → `ERROR(NO_READ_ONLY_SOURCE)`。
- 错误码（`dataquality.ClassifyError`，写 `error_code`）：`CONNECTION`、`AUTH`、`TIMEOUT`、`PERMISSION`、`UNSUPPORTED`、`SQL_SYNTAX`、`BAD_DATA`、`LIMIT_EXCEEDED`、`ORPHANED`、`NO_READ_ONLY_SOURCE`、`EXECUTION`（未归类的驱动错误，消息脱敏 + 截断 1 KiB）。`QUOTA` 已不再由 runner 产生，仅为历史执行行保留（前端仍会本地化它）。
- 日志：没有每批次 INFO 汇总行；只有失败、放弃、清扫等 WARN/ERROR（带 batch/execution/rule id），不打印 SQL 与样本值；准入、延后、放弃、完成、规则漏斗等指标见 [metrics.md](metrics.md)。

### 3.4 问题、通知与静音

- issue 行：`resource_id`、`rule_id`、target、`severity`（复制规则严重级别）、first/last seen 执行 id + 时间、`occurrence_count`、`resolution_reason/note`、ack 人/时间、`muted_until`。部分唯一索引 `(rule_id) WHERE status <> 3` 保证每规则至多一个未解决问题。
- 通知 detail（store/v1 同形）：`kind`、`rule`、`rule_title`、`target_guid`、`target_path`、`from_status`/`to_status`、`severity`、`observed_value`/`observed_unit`、`issue`、`execution`、`failed_rows`/`total_rows`、`error_code`；风暴摘要另有 `instance` 与 `failed_rule_count`/`total_rule_count`（没有 execution/run 链接）；不含正文/样本值。
- 收件人：ISSUE_OPENED/RECOVERED → 规则 owner，owner 为空/无效 → workspace admins；EXECUTION_FAILED/STORM_SUMMARY → admins。去重：`notification.DataQualityDedupeKey`，窗口 1h（storm 的 key 以内部批次 id 为 target，等效每批次一条；实例只出现在 detail 里）。
- 静音：规则级 `mute_until` 与问题级 `muted_until` 只压制 ISSUE_OPENED/RECOVERED；EXECUTION_FAILED 不受静音；风暴抑制逐条消息但 issue 照建、恢复消息照发。
- 通知失败只记日志；`notification` 表永久保留，因此 detail 必须限长（构造函数里截断）。

### 3.5 LLM 三能力（总开关默认关闭）

| 能力 | RPC | prompt 版本 | 输入 | 输出/落库 |
| --- | --- | --- | --- | --- |
| L1 规则草稿 | `SuggestQualityRule` | `dq.rule_draft.v1` | 目标 schema/DDL/列注释、已有规则摘要、意图、请求列 | 1–5 条草稿 → 校验/编译 → `quality_suggestion(RULE_DRAFT, NATURAL_LANGUAGE, PENDING)` |
| L2 失败解释 | `ExplainQualityExecution`（server streaming） | `dq.explain.v1` | 执行快照、rule revision 的 config、最近 10 条执行（不含当前）、schema history ≤5、lineage ≤10、open issue | hypotheses+evidence+next_step → `data_quality_llm_cache`（命中则回放） |
| L3 标注与建议 | `SuggestColumnLabels` | `dq.column_label.v1` | 近 7 天画像摘要（含 Top-K 摘要）、schema、lineage、已有标注；每批 ≤20 列、≤3 批 | label 建议 + 确定性映射的规则草稿 → `quality_suggestion(PENDING)` |

- provider：`llm.ResolveConfig(configs, allowlist, settings.LLMProviderProfile)`；未指定时取允许列表首个；无可用 profile → `FailedPrecondition`。
- 结构化输出：`temperature=0` + JSON mode；`llm.ParseJSON[T]`（剥 code fence、`DisallowUnknownFields`、拒绝多文档/空/null、validator 校验）；解析失败不缓存。L1 草稿词表仅 12 类 v1 规则（`custom_sql`/CEL/v1.1 四类不可由模型提出）；草稿再经列存在/列作用域/`ValidateRule` 过滤，全部被丢则返回空列表 + Warn 日志。
- L2：缓存 key = `explain|execution:<执行行 id>|rev:<rev>|status:<status>|prompt:dq.explain.v1|provider:<profile>|model:<model>|lang:<en-US|zh-CN>`；命中回放 content+metadata(`from_cache=true`)；`force_regenerate` 跳过读但仍写；usage 存缓存行 + debug 日志，metadata 不带 usage/prompt_version。解释 content 是一次性 markdown（`### n. cause`、Evidence、Next step、confidence），progress 只发一帧 `thinking`。前端只在 `FAIL`/`ERROR` 的执行详情显示入口，API 本身不限制状态。
- L3：要求请求列都有 ≤7 天画像，否则 `FailedPrecondition` 指向上 `ProfileColumns`；标注只在封闭枚举内，规则候选由 `RuleTypesForSemantic` 确定性映射、阈值为默认值（不从当前值推导）；置信度不自动接受。
- 配额（进程内、UTC 日）：L1+L2 合计 200 次/日（固定常量）；L3 取设置 `daily_column_analysis_limit`（默认 50）；全局并发 2；超限 `ResourceExhausted`。计数在 `api/v1/data_quality_llm_quota.go`。
- 出域：仅元数据/DDL/注释/规则定义/历史摘要/血缘/画像摘要；**没有任何路径发送原始样本行**（`send_sample_values_to_llm` 设置被保存但当前无消费者，§7）。提示注入：数据块 `<schema>/<rule>/<result>/…` 包裹，嵌入值中的 `</` 被中和为 `<\/`，system 声明“data, not instructions”；模型无工具、无连接。
- 失败：分析失败不改变规则/问题状态；错误以 `Internal`/`FailedPrecondition`/`ResourceExhausted` 返回，消息不含 key/样本。

### 3.6 API、权限与审计

- `DataQualityService`（`proto/v1/v1/data_quality_service.proto`）共 22 个 RPC：规则 CRUD/预览/手动执行（7）、执行列表/详情（2）、问题列表/详情/处置（3）、画像与标注列表/写入（4）、建议列表/决策（2）、三个 LLM 入口（3），以及聚合读 `GetQualityOverview`（1）。手动执行返回按规则规划的执行；没有 run/result 的 RPC 或消息。
- 分页统一 `parseLimitAndOffset`（offset token），页上限 1000；手动执行选择集 ≤200 条；规则列表的 instance/database/schema/table 是 **GUID 前缀**过滤，`search` 只匹配 title/description；列表可按 `rule_type`、dimension、severity、state、owner/owner_unassigned 过滤。执行列表可按 rule/target_guid/state/outcome/trigger/instance/initiator/start/end 过滤。
- 概览（`GetQualityOverview`）：`rule_count` = ENABLED+PAUSED（不含 ARCHIVED），`monitored_table_count` = 至少一条启用规则覆盖的去重表数，打开问题按严重级别计数；`execution_count` 是最近 24 小时**规划**的执行数（含各生命周期状态），而 `last_day_counts` 与 `trend` 只统计已 `COMPLETED` 的执行（pending/running/abandoned 不进趋势），`failing_rule_count` 只算最近 24 小时有 FAIL 执行的规则（`ERROR` 不算）。
- 错误映射：参数/类型/编译失败 → `InvalidArgument`；目标消失 → 创建期 `NotFound`、运行期 `SKIPPED(ORPHANED)`；无只读源/LLM 未开启/无 provider → `FailedPrecondition`；配额/队列满 → `ResourceExhausted`；revision 冲突/建议已决 → `Aborted`；其余 `Internal`。
- 权限（`metaxisdata.dataQuality.*`，15 项）：`rules.{get,list,create,update,delete,run}`、`executions.{get,list}`、`issues.{get,list,update}`、`explain`、`suggest`、`profile.run`、`labels.update`。`workspaceMember` 基线 = rules.get/list、executions.get/list、issues.get/list、explain；其余默认仅 `workspaceAdmin`/自定义角色。设置页复用 `settings.get/update`；标注的**读**复用 `rules.list`（经 `ListColumnLabels`）。
- 审计（proto `audit=true`，每次调用 1 行）：规则创建/更新/删除、手动执行、问题处置、建议决策、画像、标注更新、三个 LLM RPC；只读列表/详情与定时执行不审计。审计行只含请求元数据，不含 LLM 正文/样本。
- 限流：所有带审计注解的 DQ RPC 自动进入 principal 预算（`(principal, procedure)` 3000/min、部署 30000/min，见 [rate-limiting.md](rate-limiting.md)）；LLM 另有 §3.5 的配额。

### 3.7 前端与 i18n

- 路由：`/data-quality`（概览）、`/rules`（列表）、`/rules/new`、`/rules/:ruleId+`（规则详情）、`/rules/:ruleId+/edit`、`/suggestions`、`/issues`、`/issues/:issueId+`、`/executions`、`/executions/:executionId+`；`/settings/data-quality` 挂在 Settings。每条路由带 permission，侧栏按权限隐藏（不置灰）。父路由 `contentWidth: "full"`。
- 导航：侧栏第 6 组“数据质量”（概览/规则/执行/问题/建议），SettingsNav 增加“数据质量”。规则详情从规则列表标题、执行详情的规则名、问题详情的规则名与资产页质量区进入（`rules.get` 在 member 基线内，只读可见）。
- 页面/组件：`pages/data-quality/`（概览、规则列表、规则详情、规则编辑器、执行列表/详情、问题列表/详情、建议队列）；`components/data-quality/`（状态/严重级别徽标、趋势图、最近执行状态色块条、规则表单与 filter 编辑器、失败样本、SQL 查看、建议卡、字段分析面板、AI 解释抽屉、草稿对话框、目标选择器、资产页质量区）。复用 shadcn-vue 原语，不新增 tabs 原语。
- 规则列表筛选：标题/描述文本、**目标范围**（实例 → 数据库 → Schema → 表/视图；复用 `TargetPicker` 的 `mode="scope"`，在任一层可停，点表/视图行直接筛到该表）、**负责人**（复用 `UserSelect`；owner 可空，所以除成员外还有“未指定”一项；成员目录一次加载后同时供选择器与列表的负责人列复用，选择器支持懒加载与预载）、**规则类型**（18 类，复用 `ListQualityRules` 的 `rule_type`；“全部类型”发 UNSPECIFIED 即不筛选）、维度、严重级别、状态；改动后点“应用”才查询。列表还显示负责人列（邮箱，未指定时显示“未指定”）。`TargetScopeFilter` 与负责人控件只持有待应用值，请求由 `ListQualityRules` 的 instance/database/schema/table（GUID 前缀）以及 owner（`users/{user}`；空表示不限）与 owner_unassigned（只取无负责人的规则，与 owner 互斥）字段承受。
- 规则编辑器的规则类型选择：按 4 组（数量与时效 / 取值与关系 / 聚合与对账 / 结构与自定义）分组，选中项下方显示一句说明；目标为 VIEW/MATERIALIZED_VIEW 时只列出后端 `RuleTypeForTarget` 允许的 5 类（行数、行数变化、时效性、自定义 SQL、结构漂移）并显示提示；切换类型会重置 `threshold_metric`（新类型不读该字段时归零为 COUNT）；`on_empty` 仅对 `row_count`/`custom_sql`/`schema_drift` 隐藏（对它们永远无效）。规则类型名称、说明与分组都在 `dataQuality.ruleType*` 文案中维护。结构漂移的 `check_types` 文案说明类型与可空性都参与比较；若存量规则的类型不在当前目标允许集内，编辑器仍显示该类型（带视图提示），不会静默改写。
- 执行列表：一行一条执行（state/outcome 徽标、执行 id、规则、目标、观测值、失败/总行数、trigger、耗时、开始时间）；筛选 state/outcome/trigger，跨页链接带来的 `?rule=`/`?target=` 显示为可移除 chip；offset 分页；点击行进入执行详情。
- 执行详情：状态/六态、规则链接、目标路径、观测值/阈值、行数（含 sampled/truncated 提示）、失败样本、执行 SQL（可复制）、错误码/消息、耗时、trigger/initiator/instance/revision/engine version、开始/结束时间；`PENDING`/`RUNNING` 时轮询到终态；`FAIL`/`ERROR` 显示 AI 解释入口。
- 规则详情的信息层级：顶部“最近结果”大状态（图标+状态词+相对时间+实际值/阈值/失败行数）→ 最近 20 次执行状态色块条（旧→新，按状态着色，点击进执行详情）→ 状态计数与通过率 → 最近执行表 → 规则定义（目标/字段/类型/维度/严重级别/负责人/调度/版本/时间/标签；负责人显示成员目录里的邮箱，未指定时显示“未指定”）。运行与编辑分别按 `rules.run`、`rules.update` 显示；“运行”后单条执行跳执行详情，多条进入按规则筛选的执行列表。
- API/工具：`api/data-quality.ts` + `api/client.ts` 的 `dataQualityClient`；纯函数在 `src/utils/dataQuality*.ts`（状态/严重级别映射、执行状态辅助、表单校验、LLM 提示、概览、设置、建议）；`lib/notificationText.ts` 渲染通知文案（问题/执行失败链到执行详情，风暴摘要回退到执行列表）；`composables/useUserDirectory.ts` 是规则页与详情页共用的成员目录（邮箱解析、失败回退原始 `users/{id}`，仅持 `users.list` 时请求）；无跨页 Pinia 模块。
- i18n：`dataQuality.*`、`dataQualitySettings.*`、`menu.dataQuality*`、`notifications.dataQuality*`；执行状态/触发器等动态 key 前缀已在 `DYNAMIC_PREFIXES`；术语进 `frontend/glossary.json` 的 **Data Quality** 分类。
- 布局门禁：新路由登记 `frontend/scripts/audit-ui-layout.mjs`（含各表 `tableTopMaxY` 预算；`/data-quality/executions` 为 200）；`ERROR` 与 `FAIL` 文案/徽标必须不同；编辑器保存前必须干跑（`PreviewQualityRule`）。

### 3.8 画像、语义标注与建议队列

- 画像（确定性、无 LLM）：每表一条聚合查询（行数/null/distinct/min/max/长度）+ 一条样本查询（`LIMIT 1000`）；画像打开目标库前先领实例连接预算（[instance-connections.md](instance-connections.md)）；Go 内算 Top-K（仅 distinct ≤20 的列）、样式、长度分布；值截断 64 字节、最多 20 个；写 `column_profile`（列级 upsert，`sample_method="limit"`、`engine_version="dq.v1"`）；单次 ≤设置列数、120s、4 MiB 结果。
- 聚合事实由类型能力表推导（D-34）：`countDistinct` → 精确 distinct，`extrema` → min/max，`length` → 长度上界，`text` → 样式；没有 `countDistinct` 时 distinct 来自样本并标 `distinct_estimated=true`（样本覆盖全表时为精确值），前端与 L3 提示词显示 `≈`/`~`。完全没有能力的列只取非空计数：默认 `COUNT(col)`，MSSQL text/ntext/image 用 `COUNT(CASE WHEN col IS NOT NULL …)`；`binary` 类型的极值/Top 值一律以 `0x…` 展示。推导入口是 `newProfileColumn`，画像与规则校验共用同一张表。
- 标注：`column_semantic_label` 唯一键 `(table_guid, table_type, column_name)`；封闭枚举 `QualitySemanticType`；`source=MANUAL|LLM`、`status`、`confidence`、`evidence`；人工标注可覆盖（Upsert），LLM 建议不自动覆盖。
- 规则候选映射（代码固定）：`ID/BUSINESS_KEY → unique+null_percent`；`EMAIL/PHONE/URL/UUID/IP → regex+null_percent`；`COUNTRY_CODE/CURRENCY/ENUM → accepted_values`；`AMOUNT/QUANTITY → range+null_percent`；`PERCENTAGE → range(0-100)`；`TIMESTAMP/DATE → freshness+range`；`FREE_TEXT → string_length+null_percent`；`UNKNOWN → 不生成规则`。阈值为默认值，不从当前值推导。
- 建议：`quality_suggestion(kind=RULE_DRAFT|LABEL, source=NATURAL_LANGUAGE|PROFILE, payload, status=PENDING, provider/model/prompt_version)`；决策只允许 PENDING→ACCEPTED/REJECTED（并发重复决策返回 `Conflict` → 客户端 `Aborted`），接受 LABEL 即 upsert 标注，接受 RULE_DRAFT 只改状态。

## 4. 配置映射

### 4.1 设置（`WorkspaceProfileSetting.data_quality`，零值=默认）

| 字段 | 默认 | 解析规则 | 生效点 |
| --- | --- | --- | --- |
| `enable_llm_analysis` | false | 按值 | L1/L2/L3 总开关；关闭时规则引擎不受影响 |
| `llm_provider_profile` | 空 | trim；空=允许列表首个 | DQ 分析 provider |
| `store_failure_samples` | true | message 缺席时取默认；显式 false 生效 | 执行是否取失败样本 |
| `failure_sample_limit` | 20 | ≤0→20；>100→100 | 每次执行样本行数 |
| `default_schedule_interval` | 1h | ≤0→1h；夹到 15m…7d | 新建规则默认间隔 |
| `daily_execution_limit` | 10000 | ≤0→10000 | 定时扫描的日执行上限（统计 UTC 日已规划执行，含手动规划的行；触顶只跳过定时 due 扫描，手动运行仍可执行） |
| `daily_column_analysis_limit` | 50 | ≤0→50 | L3 日分析次数 |
| `max_columns_per_analysis` | 50 | ≤0→50；写入校验上限 200 | 单次画像/标注列数 |
| `send_sample_values_to_llm` | false | 按值 | **当前无消费者**（§7） |

保留期不在设置里（常量）：执行随内部批次 90d、关闭问题 90d、画像/LLM 缓存 7d、已决建议 90d —— 见 §7，已由 maintenance 接线。

### 4.2 规则 config（`proto/store/store/data_quality.proto`，公共字段在 oneof 外）

| 规则 | config 字段 |
| --- | --- |
| `row_count` | `min_rows`、`max_rows`、`exact_rows`、`warn_min_rows`、`warn_max_rows` |
| `row_count_change` | `max_increase_pct`、`max_decrease_pct`、`warn_increase_pct`、`warn_decrease_pct` |
| `null_percent` | `column`、`max_null_count`/`warn_null_count` 或 `max_null_pct`/`warn_null_pct`（二选一）、`null_values`（≤20，自定义缺失值） |
| `unique` | `column`、`count_nulls`、`fail_threshold`、`warn_threshold` |
| `unique_combination` | `columns`（≤10）、`all_columns`、`count_nulls`、`fail_threshold`、`warn_threshold` |
| `accepted_values` | `column`、`values`（≤200）、`allow_null`、`fail_threshold`、`warn_threshold` |
| `range` | `column`、`min`/`max`（字符串，按列类型解析）、`inclusive`、`fail_threshold`、`warn_threshold` |
| `regex` | `column`、`pattern`、`fail_threshold`、`warn_threshold` |
| `string_length` | `column`、`min_len`、`max_len`、`fail_threshold`、`warn_threshold` |
| `referential_integrity` | `child_column`、`parent_table`（`schema.table`/`database.table`）、`parent_column`、`allow_null`、`fail_threshold`、`warn_threshold` |
| `cross_column` | `left_column`、`right_column`、`op`、`tolerance`、`fail_threshold`、`warn_threshold` |
| `freshness` | `timestamp_column`、`max_lag_seconds`、`warn_lag_seconds` |
| `custom_sql` | `sql`（≤8 KiB）、`fail_threshold`、`warn_threshold` |
| `cel_expression` | `expression`（≤4 KiB）、`columns`、`sample_rows`（默认 10000，上限 100000）、`fail_threshold`、`warn_threshold` |
| `schema_drift` | `expected_columns[{name,type,nullable}]`、`check_types`（可选，未设置=true；比较类型与可空性，可空性仅当快照记录了该列时）、`check_order`（默认 false）、`allow_extra_columns`（默认 false）、`fail_threshold`、`warn_threshold` |
| `group_check` | `columns`（1–5）、`min_rows_per_group`/`max_rows_per_group`（至少一个）、`fail_threshold`、`warn_threshold` |
| `aggregate` | `column`、`function`（SUM/AVG/MIN/MAX/STDDEV/PERCENTILE；COUNT 保存期与执行前均拒绝，用 `row_count`）、`percentile`、`min`/`max`（至少一个）、`warn_min`/`warn_max`、`inclusive` |
| `cross_table` | `right_table`、`left_function`/`right_function`（COUNT/SUM/AVG/MIN/MAX）、`left_column`/`right_column`、`tolerance`、`fail_threshold`、`warn_threshold` |
| 公共 | `threshold_metric`（COUNT/PERCENT；仅 §3.2 的 10 类读取，`null_percent` 用自己的字段）、`filter`（AND 子句）、`on_empty` |

### 4.3 JSONB 列 → store 消息

| 列 | 消息 |
| --- | --- |
| `quality_rule.target_path` / revision | `QualityTargetPath` |
| `quality_rule.config` / revision | `QualityRuleConfig`（oneof + 公共字段） |
| `quality_run.counts` | `QualityStatusCounts`（六态 + total；内部批次 rollup） |
| `quality_execution.target_path` | `QualityTargetPath`（执行规划时的目标快照） |
| `quality_execution.thresholds` | `QualityThresholds`（warn/fail、upper、comparison、on_empty 快照） |
| `quality_execution.sample` | `QualityFailureSamples`（`FailureSample{ColumnValue{column,value}}`，值已截断） |
| `column_profile.profile` | `ColumnProfile` |
| `column_semantic_label.evidence` | `SemanticLabelEvidence` |
| `quality_suggestion.payload` | `QualitySuggestionPayload`（`rule`/`label` oneof） |
| `data_quality_llm_cache.result_json` | `DataQualityLLMResult`（`rule_draft`/`explanation` + provider/model/prompt/token 用量） |

JSONB 一律 `protojson.Marshal`，反序列化用 `common.ProtojsonUnmarshaler`；列注释写消息名。

### 4.4 引擎 × 能力

| 能力 | MySQL 家族/TiDB/MariaDB/OceanBase | StarRocks/Doris | PostgreSQL | MSSQL |
| --- | --- | --- | --- | --- |
| 查询型规则（除 regex/percentile） | 是 | 是 | 是 | 是 |
| `regex` | 是（`REGEXP`） | 是 | 是（`~`） | **否**（保存期拒绝） |
| `aggregate.PERCENTILE` | 否 | 否 | 是（`PERCENTILE_CONT … WITHIN GROUP`） | 是（`… OVER ()`） |
| `cel_expression` | 是（Go 求值） | 是 | 是 | 是 |
| `schema_drift` | 是（不查目标库） | 是 | 是 | 是 |
| `cross_table` | 是（同库） | 是（同库） | 是（同 schema） | 是（同 schema） |

### 4.5 错误码（执行行 `error_code`）

`CONNECTION` / `AUTH` / `TIMEOUT` / `PERMISSION` / `UNSUPPORTED` / `SQL_SYNTAX` / `BAD_DATA` / `LIMIT_EXCEEDED` / `ORPHANED` / `NO_READ_ONLY_SOURCE` / `EXECUTION`（未归类驱动错误，脱敏截断）。`QUOTA` 是历史值：容量竞争在批次开始前就被延后，runner 不再产生它（[instance-connections.md](instance-connections.md)）。LLM 侧错误以 Connect code 返回，不写执行行。

## 5. 不变量

| # | 不变量 | 守卫 |
| --- | --- | --- |
| I-1 | 目标身份 = `(guid, object_type)` + 路径快照；解析校验 registry 当前行、实例未删、数据库未隐藏 | `dataquality.ResolveTarget` 单测 + store 读路径 |
| I-2 | head config 与最新 revision config 一致；执行记实际执行的 revision + engine version | 更新事务 + `quality_rule_test.go` |
| I-3 | 每规则至多一个未解决 issue | 部分唯一索引 + `OpenQualityIssue`/`PlanIssueTransition` 测试 |
| I-4 | 每次执行一个六态判定；内部批次 outcome = 最坏 rank（仅内部统计）；`ERROR` 与 `FAIL` 不合并 | `Rollup`/`CountResults` 单测 + 前端徽标测试 |
| I-5 | 空数据语义由 `on_empty` 决定并写入阈值快照；默认 NOT_EVALUATED | evaluator 单测 |
| I-6 | 查询型规则只走 READ_ONLY 数据源；SQL 只允许单条 SELECT/WITH | `ReadOnlyQueryer` + ValidateRule/Compile 双层 |
| I-7 | 执行结果/样本在配置上限内，超限置 `truncated=true`（不是失败） | 扫描器 + 测试 |
| I-8 | 告警按状态迁移；issue 通知 1h 去重；执行失败不可被静音；风暴只抑制逐条消息 | issue 迁移表 + 通知单测 |
| I-9 | 手动批次只执行 API 规划的执行行，绝不回落到“该实例全部启用规则” | 进程内队列 `EnqueueQualityExecutions(batchID, instance, executionIDs)` + runner 测试 |
| I-10 | 定时批次先规划执行行并推进 `next_run_at`，再执行（失败不回退）；inflight 去重保证同一规则不并发打库 | runner 单测 |
| I-11 | LLM 输出必须通过封闭 schema 校验才落库/缓存；解析失败不缓存 | `llm.ParseJSON` + 各 `Parse*` 单测 |
| I-12 | LLM 不接触目标库、无工具、不自动建规则/启用/执行；L1/L3 只产 PENDING 建议 | handler 流程 + 建议决策只改状态 |
| I-13 | 提示中的数据库内容是不可信数据块（`</` 被中和）；模型无连接/凭据 | prompt 构造单测 |
| I-14 | 执行资源名最后一段是 `quality_execution` 行 id（表无 resource_id）；规则/问题/建议用不可回收 resource_id；内部批次没有对外名字 | convert 单测 + handler 解析 |
| I-15 | 软删除保留 revision/执行/问题；表不物理删除规则 | archive 路径 + store 查询过滤 |
| I-16 | 单副本：inflight/配额/LLM 计数都在进程内；重启收敛、不补跑 | 代码结构 + runner 测试 |
| I-17 | 编译 SQL 的占位符与 `Args` 必须一起下发：`db.ReadOnlyQueryer.QueryReadOnly(ctx, sql, args, opts)`；预览、执行、画像都传各自的 args（画像无参数传 nil） | `execution_test.go` 的参数断言 + 真 server 集成场景（带 filter 的 range 规则 dry run/执行） |
| I-18 | 内部批次的连接预算在批次开始前一次性准入，持有到查询阶段结束（持久化/通知前释放）：scheduled 拿不到 → 不建批次、规则保持 due；manual 拿不到 → 已规划的执行与批次 `ABANDONED`（无 outcome）；规则在准入后目标被改到别的实例 → 由 `ruleConnectionLease` 对实际实例 try-acquire 一次（拿不到记 `SKIPPED`）；任何情况下容量竞争都不写成执行结果或 `ERROR(QUOTA)` | `qualityrunner_test.go` 的 `admitBatch`/`ruleConnectionLease` 测试 + governor 单测（[instance-connections.md](instance-connections.md)） |
| I-19 | `quality_run` 只做内部派发批次：v1 没有 `QualityRun` 资源/消息/RPC，前端没有 `/runs` 页面，问题引用、通知 detail 与概览都不出现批次；对外一律 `QualityExecution` | proto 无 QualityRun、路由与审计清单无 runs、convert/notification 测试 |

## 6. 失败模式

| 场景 | 行为 |
| --- | --- |
| 实例没有 READ_ONLY 数据源 | 查询型规则**创建即拒绝**（`FailedPrecondition`，文案含配置入口）；创建后数据源被移除则运行期 `ERROR(NO_READ_ONLY_SOURCE)`；预览/执行同样拒绝；不回退 ADMIN |
| 目标库连接失败/凭据失效 | 执行 `ERROR(CONNECTION/AUTH/PERMISSION)`，执行失败通知（admins）；下一次排期自然重试 |
| 查询超时 | `ERROR(TIMEOUT)`；不自动重试 |
| 结果集超上限 | 截断并 `truncated=true`，状态照常判定；UI 显示“已截断” |
| 保存后列被删/类型变了 | 运行期 `ERROR(UNSUPPORTED)`（ValidateConfig 失败）；预览阶段则拒绝保存 |
| 目标被删/实例被删/数据库隐藏 | 规则显示 `orphaned_reason`；运行期 `SKIPPED(ORPHANED)`；不通知 |
| 同一规则并发触发 | 进程内 inflight 去重 → 后到者 `SKIPPED` |
| 规则编辑中 | 执行永远带实际 revision；历史不变 |
| 服务重启 | 启动时清扫超过 15 分钟的 PENDING/RUNNING 执行与批次 → `ABANDONED`；规则按 `next_run_at` 收敛；不补跑 |
| 单条执行 panic | 该执行 `ERROR(EXECUTION)`，同批次其余执行继续 |
| 执行终态写失败 / 批次超时 / 任务未产出判定 | 该批次剩余执行 `ABANDONED`、批次 `ABANDONED`（不写部分 rollup）；已写入判定的 issue 迁移与逐条通知照常（不发风暴摘要） |
| 批次无法进入 RUNNING（重复派发/已被清扫/已终结） | 该批次未完成执行与批次标 `ABANDONED`（`mark_running_failed`）；不重试、不补跑；清扫写失败的残留行留给下次启动清扫 |
| 放弃执行/批次的写入本身失败 | 批次仍尝试以 `ABANDONED` 结束（不因一次清扫写失败停在 RUNNING）；残留 PENDING/RUNNING 行留给下次启动清扫（仅处理启动时已超过 15 分钟的行），不补跑 |
| 通知写失败 | 只记日志；执行与问题状态不受影响 |
| 告警风暴（一次批次 FAIL 执行 >10） | 逐条 opened/execution-failed 抑制，发一条 STORM_SUMMARY（带实例与计数）；issue 照建、恢复照发 |
| 规则/问题静音期内失败或恢复 | 不发送问题通知；执行失败通知不受影响 |
| 每日执行配额触顶 | 跳过本轮定时 due 扫描（规则保持 due，次日继续）；手动运行不受影响；不写 ERROR、不告警（§7） |
| 实例连接预算被占满 | 定时批次等待 ≤2m 后不建批次、规则保持 due、下个 tick 重试；手动批次等待 ≤5m 后其执行与批次 `ABANDONED`；不写 `ERROR(QUOTA)`、不通知（[instance-connections.md](instance-connections.md)） |
| 手动队列满 | 该批次及其执行标 `ABANDONED`（无 outcome）并返回 `ResourceExhausted` |
| LLM 未开启/无 provider | `FailedPrecondition`；规则引擎完全可用 |
| LLM 解析失败/超时/配额耗尽 | 请求失败（Internal/FailedPrecondition/ResourceExhausted）；不缓存、不改规则/问题状态；L3 已入队的建议保持 PENDING |
| 画像失败 | 请求失败并返回可渲染错误；已写入的画像行保留，重试会覆盖 |
| 建议重复决策 | `Conflict` → `Aborted` |
| 语义标注冲突 | LLM 建议只在人工接受后写标注；人工标注可随时覆盖，LLM 不会自动改写 |
| schema 变更 | `target_schema_changed` 按 schema_hash 提示复核；不自动暂停 |
| 多副本 | 不支持；两个副本会重复执行（单副本假设见 [security-posture.md](../../docs/security-posture.md)） |

## 7. 已知缺口与刻意取舍

| # | 事项 | 现状 / 边界 |
| --- | --- | --- |
| 1 | 日执行配额触顶 | 跳过本轮 due 扫描并记日志（规则保持 due，次日继续）；**不**写 `ERROR(QUOTA)`、不发告警；手动运行不经过该检查——与需求文案有差异，避免为一条配额事件伪造执行结果 |
| 2 | 单规则查询超时 | 固定 60s；设计提到的“规则 config 可覆盖到 300s”没有对应字段，未实现 |
| 3 | `send_sample_values_to_llm` | 已存储/展示但无消费者：所有 LLM 路径都不发原始样本行（比开关默认更严格）；接线前该开关无效 |
| 4 | 每 workspace/每表规则数上限 | 已实现（1000 / 100，创建时 `ResourceExhausted`） |
| 5 | 创建期只读源校验 | 已实现：查询型规则创建时若无 READ_ONLY 数据源直接 `FailedPrecondition`；元数据规则不要求 |
| 6 | `row_count_change` 基线窗口 | 已实现：≤7 天且 ≤2×interval，否则视为无基线 → `NOT_EVALUATED` |
| 7 | 保留期清理 | 已实现：maintenance 清理内部批次（级联执行）/resolved issue/column_profile/LLM cache/decided suggestion（§8 `backend/runner/maintenance`） |
| 8 | L2 的 `ExplainSQLMetadata` 无 usage/prompt_version | usage 只在缓存行与 debug 日志；未改 proto（有意） |
| 9 | 建议队列无 Get RPC | 编辑器 `?suggestion=` 预填按队列全量查找后匹配（数据量有界；如需可加 GetQualitySuggestion） |
| 10 | 多副本 | 不支持（单副本假设，见 [security-posture.md](../../docs/security-posture.md)）；两个副本会重复执行、LLM 配额各自计数 |
| 11 | 分布漂移/ML 异常、分组内多种检查（每组 null/aggregate）、RuleSet | 非目标（§非目标）；RuleSet 在 v1.1 之后单独评审 |
| 12 | 连接等待的可观测性 | 批次的准入/延后/放弃与在途执行数已有指标（见 [metrics.md](metrics.md)）；连接等待时长直方图仍是 P1，尚未实现 |

## 8. Where things live

| 面 | 位置 |
| --- | --- |
| 领域/编译/判定 | `backend/component/dataquality/`：`model.go`、`types.go`、`capabilities.go`、`target.go`、`validate.go`、`compile.go`、`compile_rules.go`、`compile_util.go`、`compile_samples.go`、`dialect.go`、`support.go`、`evaluate.go`、`execution.go`、`executor.go`、`errors.go`、`filter.go`、`issue.go`、`settings.go`、`profile*.go`、`cel.go`、`label.go`、`prompt.go` |
| 只读查询能力 | `backend/plugin/db/query.go`（`ReadOnlyQueryer`/`QueryOptions`/`QueryResult`、扫描上限）；`plugin/db/{mysql,pg,mssql,starrocks}/query.go`；`backend/component/dbfactory/readonly.go`（`ErrNoReadOnlyDataSource`） |
| runner | `backend/runner/qualityrunner/qualityrunner.go`（调度/规划执行/执行/问题迁移/通知）；`backend/server/server.go` 注册第 5 个 runner；保留期清理在 `backend/runner/maintenance/maintenance.go` |
| 连接准入 | `backend/component/state/connection.go`（governor/优先级/等待队列）；`qualityrunner` 的 `admitBatch`；契约与各调用方预算见 [instance-connections.md](instance-connections.md) |
| API | `backend/api/v1/data_quality_service.go`（规则 CRUD/手动执行/执行查询/issue/profile/preview/overview）、`data_quality_convert.go`、`data_quality_llm_analysis.go`（L1/L2）、`data_quality_label_analysis.go`（L3）、`data_quality_llm_client.go`、`data_quality_llm_quota.go`、`data_quality_overview.go`、`data_quality_cross_table.go` |
| store | `backend/store/quality_{rule,execution,run,issue,profile,label,suggestion,llm_cache,overview}.go`（9 张表；`quality_run` 为内部批次） |
| proto/迁移 | `proto/store/store/data_quality.proto`、`proto/v1/v1/data_quality_service.proto`、notification/setting 扩展；`backend/migrator/migration/0.1/0024##data_quality.sql` + `0025##quality_execution.sql`（`quality_rule_result` 改名 `quality_execution` 并补执行列）+ `LATEST.sql` |
| 前端 | `frontend/src/pages/data-quality/`、`pages/settings/DataQualitySettingsPage.vue`、`components/data-quality/`、`api/data-quality.ts`、`utils/dataQuality*.ts`、`lib/notificationText.ts`；路由 `router/index.ts`，侧栏 `AppSidebar.vue`，`SettingsNav.vue` |
| 单元测试 | `component/dataquality/*_test.go`（19 个测试文件：编译 golden ×4 方言、判定、filter、custom_sql、CEL、画像、标注/prompt、schema_drift、group_check、aggregate、cross_table、null_values、issue、execution、errors、逐项能力矩阵）；`store/quality_execution_test.go` 等 `store/quality_*_test.go`；`runner/qualityrunner/qualityrunner_test.go`；`api/v1/data_quality_*_test.go`；`component/notification/data_quality_test.go`、`api/v1/notification_service_test.go`；前端 `*.test.ts` 与 `lib/notificationText.test.ts` |
| 集成测试 | `backend/test/integration/runner/data_quality_service_test.go`（规则生命周期、问题/通知、元数据+只读规则、画像/标注、jsonb 画像、不可运行列类型在保存期被拒、json 列上的 plain null_percent 真执行、cross_table 右表为视图、LLM 默认关闭） |
| 权限 | `backend/common/permission/permission.json` + `permission_gen.go`；`store/predefined_roles.go`（member 基线）；前端镜像 `frontend/src/lib/permissions.ts` |
| 启用点 | 迁移 `0024` + `0025`（随二进制自动执行）；每个实例需配 `READ_ONLY` 数据源；LLM 需设置页开启 `enable_llm_analysis` 且存在允许的 provider profile；无其他开关 |
| 门禁 | Go：`gofmt`、`golangci-lint`、`go test ./component/dataquality/ ./store/ ./runner/qualityrunner/ ./api/v1/`；proto：`buf format/lint/generate`；迁移链测试；前端：`biome:check`、`lint`、`i18n`、`type-check`、`test run`、`audit:ui` |

> 引擎覆盖待办：CI 目前只有 PostgreSQL + MySQL 的 testcontainers。MSSQL / StarRocks / Doris 的类型能力矩阵由单测 + 手工容器 probe 固化（`capabilities_test.go`），**待 CI 环境增加这三种引擎后补真实集成测试**。MariaDB/TiDB/OceanBase 继承 MySQL 家族规则但未逐一探测（MariaDB 的 JSON 是 LONGTEXT），列入同一待办。

**非目标**（不做）：RuleSet/contract-like 整体验证（D-20）、模板/示例库、进程内 Python/WASM/任意代码、LLM-as-judge 判定、自由 SQL 生成并执行、自动修复数据/阈值、邮件/webhook/Slack/PagerDuty、质量分/scorecard、流式/行级检查、分布漂移/季节基线、MCP/CLI 暴露、跨副本调度、跨实例/跨库对账（cross_table 限同库）。
