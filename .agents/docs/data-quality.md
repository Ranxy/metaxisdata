# 数据质量（Data Quality）— Reference

> Status: **implemented**（v1 与 v1.1 全部落地；单副本假设）。维护参考，覆盖 `backend/component/dataquality/`、`backend/runner/qualityrunner/`、`backend/api/v1/data_quality_*.go`、`backend/store/quality_*.go`、`frontend/src/pages/data-quality/`。用户侧说明见 [docs/README.md](../../docs/README.md)；刻意决策见 [security-posture.md](../../docs/security-posture.md)。
> 关联：[runner.md](runner.md)（后台执行器契约、单副本）、[notifications.md](notifications.md)（站内信与结构化 detail）、[iam-and-permissions.md](iam-and-permissions.md)（权限目录/注解）、[rate-limiting.md](rate-limiting.md)（principal 预算）、[object-ddl-storage.md](object-ddl-storage.md)（DDL 读取）、[lineage-analyzer.md](lineage-analyzer.md)（血缘上下文）、[frontend-ui.md](frontend-ui.md)（骨架/布局门禁）、[i18n-glossary.md](i18n-glossary.md)（术语）、[security-open-items.md](security-open-items.md)。规则：[backend/AGENTS.md](../../backend/AGENTS.md)、[proto/AGENTS.md](../../proto/AGENTS.md)、[backend/migrator/AGENTS.md](../../backend/migrator/AGENTS.md)。

## 0. 它是什么

规则引擎负责**确定性判定**“数据是否达标”；LLM 只做三件辅助——把自然语言变成**规则草稿**、把失败结果**解释**成假设、把字段画像变成**语义标注与规则建议**。LLM 不执行判定、不写回数据、不自动建规则/启用/执行。

```
SPA ──ConnectRPC──► DataQualityService (api/v1) ──► component/dataquality（校验/编译/判定/画像/CEL）
                               │                        │
                               ├──► store（PG：quality_* 9 张表）◄── qualityrunner（tick 10s / 手动队列）
                               ├──► component/llm（L1/L2/L3，复用 provider）
                               └──► notification.Service（问题迁移 → 站内信）        │
                                                     qualityrunner ──► db.ReadOnlyQueryer ──► 目标库（READ_ONLY 数据源）
```

## 1. 决策（D-01…D-33 的有效结论）

需求稿的早期编号 D1–D24 与下表重复，已并入、不再单列。

| # | 决策 | 后果 / 边界 |
| --- | --- | --- |
| D-01 | 领域语义集中在 `backend/component/dataquality`；API 与 runner 只做编排 | 可 golden、可单测，不依赖 HTTP/DB 细节 |
| D-02 | 目标库查询走新增可选接口 `db.ReadOnlyQueryer`，业务层不碰 `driver.GetDB()` | MySQL 家族/PG/MSSQL/StarRocks 各实现；不实现该接口的引擎执行期 `ERROR(UNSUPPORTED)` |
| D-03 | 查询型规则**强制** `READ_ONLY` 数据源，缺失不回退 ADMIN | 预览/执行拒绝并给出配置入口；创建期不校验（见 §7） |
| D-04 | `filter` 是结构化 AND 子句（列/比较符/值），不是 SQL 片段 | 10 种 op、≤10 子句、IN ≤100；复杂条件用 `custom_sql` |
| D-05 | 结果六态；执行 rollup = 最坏 rank；`ERROR` 不建问题、走独立告警 | `ERROR` 与 `FAIL` 永不合并；“没数据”默认不当作 PASS |
| D-06 | 一次 run = 一个实例的一批规则（定时按实例分批 ≤50，手动按选择集分组） | 连接预算与失败合并以实例为单位 |
| D-07 | 规则 head 与不可变 revision **同事务**双写；结果记 `rule_revision` | 编辑不改历史判定 |
| D-08 | 同一规则至多一个未解决 issue（部分唯一索引 `status <> 3`） | 并发/重试不会开两个问题 |
| D-09 | 告警只用站内信：`NOTIFICATION_TYPE_DATA_QUALITY` + `store.DataQualityDetail` | 无邮件/webhook；正文由前端 i18n 渲染 |
| D-10 | DQ 设置放 `WorkspaceProfileSetting.data_quality`（store 与 v1 两份 proto） | 复用 `settings.get/update` 与设置页，不新增 settings RPC |
| D-11 | 权限目录新增 `metaxisdata.dataQuality.*`（15 项）；`workspaceMember` 基线加 7 项只读+explain | ACL 拦截器逐 RPC 校验；owner 只是展示与通知路由 |
| D-12 | LLM 复用 `component/llm`：补 temperature/JSON/usage、共享 provider 解析；不另建 LLM 栈 | `ParseJSON[T]` 严格解码，ExplainSQL 回归绿 |
| D-13 | 字段画像在 API 请求内同步执行（列数取设置、120s、只读源），不建 job 表 | 长任务不进 API，由上限保证 |
| D-14 | CEL 是规则类型：Go 内求值、采样级、不生成 SQL | 方言无关；精确需求用 `custom_sql` |
| D-15 | 前端第 6 个顶层导航 + 5 个入口子页；规则另有独立详情页；趋势图与最近执行用内联 SVG/状态色块条（`QualityTrendChart`、`QualityResultStrip`） | 修订 frontend-ui 的顶层导航约定；布局审计登记路由（详情页为动态 id，与 run/issue 详情一样不登记） |
| D-16 | 保留期：结果随 run 90d、关闭问题 90d、画像/LLM 缓存 7d、已决建议 90d；由 maintenance runner 每 6h 清理 | 结果随 run 级联删除；只清已解决的问题与已决建议 |
| D-17 | 不新增运行时依赖：CEL 用 `cel-go`，SQL 校验用 `omni`，图表自绘 | 保持单二进制 |
| D-18 | 调度 = 进程内 tick + inflight map + 表内 `next_run_at` | 单副本；重启收敛、不补跑 |
| D-19 | count 型规则公共 `threshold_metric`（COUNT/PERCENT） | PERCENT 仅 10 类（§3.2） |
| D-20 | RuleSet（contract-like 整体验证）后续再做 | v1/v1.1 不引入该实体；前置能力见 §8 非目标 |
| D-21 | `custom_sql` 是缺口逃生口：人写单条 SELECT/WITH ≤8 KiB | 只读、超时/上限、审计；反复出现的形态应升格为规则类型 |
| D-22 | 统一 `fail_threshold`/`warn_threshold`（optional double），单位由 `threshold_metric` 定 | 默认 fail=0、warn 可选；比较严格（等于阈值通过） |
| D-23 | run 拆 `status`（PENDING/RUNNING/COMPLETED/ABANDONED）+ `outcome`（未完成时 NULL） | 前端能区分“排队/执行中/已完成”；列表可按 outcome 过滤 |
| D-24 | `target_schema_changed` 用 registry **schema_hash**（`ListMetaRegistrySchemaHashes`）口径；规则保存时写同一口径 | 路径快照仅展示；两套 hash 混用会让“结构已变”永真 |
| D-25 | 手动 run 的规则集不进库：API 建 run 后经进程内队列 `EnqueueQualityRun(runID, ruleIDs)` 交给 runner | 队列容量 64；满则放弃该 run（ABANDONED/outcome ERROR）并返回 `ResourceExhausted` |
| D-26 | 设置零值=默认；`data_quality` message 存在性区分“未配置”和显式 false；返回生效值不落库默认 | 0 是“取默认值”，**不是**“不限制/不存”（§4.1） |
| D-27 | 通知：issue opened/recovered 按 issue 资源名 1h 去重、收件人 owner→admins；execution-failed 只发 admins 且不受静音；run 内 FAIL 规则 >10 合并 STORM_SUMMARY（每 run 一条）；恢复不受 storm 抑制 | 静音只影响问题通知；风暴仍创建 issue，只抑制逐条消息 |
| D-28 | 结果资源名最后一段 = `quality_rule_result` **行 id**（该表无 resource_id）；`ExplainQualityResult` 按其查 `store.GetQualityRuleResult` | 表结构不变；列表与解析同一 id |
| D-29 | L2 先收集完整 JSON、解析后再一次性发 content；progress 一帧 + 终帧 metadata；usage 只入缓存行与日志 | `ExplainSQLMetadata` 无 usage/prompt_version 字段，不为统计改协议 |
| D-30 | L1/L3 只产出 PENDING 建议；接受 LABEL 建议即写 `column_semantic_label`；接受 RULE_DRAFT 只改建议状态 | 规则由编辑器保存才创建；不自动建/启用/执行 |
| D-31 | v1.1 第一批：`schema_drift`（R15，元数据比对）与 `group_check`（R16，每组行数阈值） | 最小可解释语义，不做通用 DSL |
| D-32 | `schema_drift` 不取只读源、无 SQL（`QueryMetadata`）；`sampled=false`、样本是漂移项列表；`group_check` 走普通只读查询 | 元数据规则不能因没配只读源而阻塞 |
| D-33 | v1.1 第二批：`aggregate`（R17）、`cross_table`（R18）、`null_percent.null_values`；PERCENTILE 仅 PG/MSSQL，其它引擎保存期拒绝；cross_table 限同实例同库 | 不引入跨库/跨实例；复杂对账仍用 `custom_sql` |
| D-34 | 画像按 **类型形状** 选聚合事实（与规则编译的 `ColumnKind` 分离）：规则只关心绑定字面量怎么读，画像还要保证 SQL 合法。只有明确列入文本类型的才取 distinct/min/max/长度；json/jsonb、xml、空间、二进制、数组、用户自定义等类型只取非空计数，distinct 由样本给出并标 `distinct_estimated`，不取极值/长度/样式 | 未知类型默认保守（少几个事实，而不是整表语句报错）；`MIN(jsonb)`/`CHAR_LENGTH(jsonb)` 这类错误不再出现 |

## 2. 工作负载形状

### 2.1 实体与资源名

```
meta_registry_resource（只读）──< quality_rule >──< quality_rule_revision（不可变）
                                        │
                                        ├──< quality_run（一次执行，一个实例）──< quality_rule_result（只插入）
                                        │                                              │
                                        │                                              └──< quality_issue（每规则至多一个未解决）
                                        └──< quality_suggestion（待审建议）      column_profile / column_semantic_label（按表 GUID+列名）
```

| 资源 | 名称 | id 来源 |
| --- | --- | --- |
| 规则 | `workspaces/{ws}/qualityRules/{quality_rule}` | `common.RandomString(12)`，不可回收 |
| 执行 | `workspaces/{ws}/qualityRuns/{quality_run}` | `common.RandomString(12)` |
| 问题 | `workspaces/{ws}/qualityIssues/{quality_issue}` | `common.RandomString(12)` |
| 建议 | `workspaces/{ws}/qualitySuggestions/{quality_suggestion}` | `common.RandomString(12)` |
| 结果 | `workspaces/{ws}/qualityRuleResults/{quality_rule_result}` | **数据库行 id**（BIGSERIAL 数字） |
| 目标 | registry GUID + `MetaType`（`(guid, object_type)` 成对） | 路径快照只用于展示/重绑 |

`parent` 接受 `workspaces/-`（当前工作区）；资源名里的 workspace 段必须是部署的唯一工作区，否则 NotFound。

### 2.2 状态模型

**结果六态**（优先级 rank：`ERROR 5 > FAIL 4 > WARN 3 > NOT_EVALUATED 2 > SKIPPED 1 > PASS 0`，不是枚举值大小）：

| 状态 | 含义 |
| --- | --- |
| `PASS` / `WARN` / `FAIL` | 已执行：在阈值内 / 越过 warn 未越 fail / 越过 fail |
| `ERROR` | 未完成判定：SQL/连接/权限/超时/编译失败；监控自身故障，不建问题 |
| `SKIPPED` | 有意不执行：目标 orphaned、规则已归档、同规则并发执行中 |
| `NOT_EVALUATED` | 已执行但无可判定数据：过滤后 0 行、`MAX(ts)` NULL、聚合 NULL、无基线 |

run：`status ∈ {PENDING, RUNNING, COMPLETED, ABANDONED}`；`outcome` 是结果 rollup（完成前 NULL）。启动时把超过 15 分钟的 PENDING/RUNNING run 标 `ABANDONED`（不补跑）。

**问题生命周期**（由 `dataquality.PlanIssueTransition` 决定，判定在结果提交之后执行）：

| 本次结果 | 既有 open issue | 动作 | 通知 |
| --- | --- | --- | --- |
| `FAIL` | 无 | 新建 `OPEN`（first/last seen = 本次 run） | ISSUE_OPENED（除非静音/风暴） |
| `FAIL` | 有 | TOUCH：last_seen/run、occurrence_count++ | 1h 去重窗口内不重复 |
| `PASS` 且上次结果 `FAIL` | 有 | `RESOLVED(AUTO_RECOVERED)` | ISSUE_RECOVERED（除非静音） |
| `PASS` 且上次不是 `FAIL`（如 `ERROR→PASS`） | 有 | 不动 | — |
| `WARN` / `NOT_EVALUATED` / `SKIPPED` 且上次 `FAIL` | 有 | TOUCH（不自动恢复） | — |
| `ERROR` | 任意 | 不动 | EXECUTION_FAILED（admins，不受静音；风暴时并入摘要） |

规则生命周期：`ENABLED`（创建即启用，首次运行在下一排期）↔ `PAUSED`（人工/资产 orphaned）→ `ARCHIVED`（软删除，历史保留）。更新 schedule 或重新启用时才重算 `next_run_at = now + interval`；暂停不动它。

## 3. 核心机制

### 3.1 规则定义与 revision

- 创建/更新走同一校验链：`ResolveTarget`（registry 当前行 + 实例未删 + 数据库未隐藏 + 列存在）→ `ValidateConfig`（按类型的参数校验 + 列类型/引用父表校验）→ `ParseFilter` → `Compile`（SQL/占位符/args/列名）→ 保存时写 `target_meta_hash`（schema_hash 口径）。
- 语义变更（类型、目标、config、severity/dimension 等）产生新 revision；head 与 revision 在一个事务里写，`SELECT … FOR UPDATE` 串行化并发编辑（`ExpectedRevision` 不匹配 → `Conflict` → 客户端 `Aborted`）。
- 结果只插入；问题状态是可变列；删除规则 = `ARCHIVED`，历史/问题保留。
- 干跑 `PreviewQualityRule`：校验 + 编译 + （非元数据规则）只读源 `LIMIT 0 / TOP 0` 探测（10s 超时），不写库、不产生 run。元数据规则（`schema_drift`）跳过探测。
- 保存时只校验/编译，不校验只读源是否存在；`schema_drift` 在缺失快照时由 API 用当前列填充。

### 3.2 编译、方言与判定

规则类型 18 类（v1 13 类 + CEL + v1.1 四类）：`row_count`、`row_count_change`、`null_percent`、`unique`、`unique_combination`、`accepted_values`、`range`、`regex`、`string_length`、`referential_integrity`、`cross_column`、`freshness`、`custom_sql`、`cel_expression`、`schema_drift`、`group_check`、`aggregate`、`cross_table`。

- 编译器表 `map[storepb.QualityRuleType]compilerFunc` → `CompiledQuery{SQL, Args, Kind, Columns}`；`Kind` 为 `Aggregate`（一行聚合）、`FailedRows`、`Sample`（样本行）、`Metadata`（无 SQL，元数据比对）。
- 方言矩阵：MySQL 家族/TiDB/MariaDB/OceanBase/StarRocks/Doris（反引号、`?`、`LIMIT`、`REGEXP`、`CHAR_LENGTH`）、PostgreSQL（`"`、`$n`、`~`/`!~`）、MSSQL（`[ ]`、`@pN`、无正则、`LEN`、`OFFSET … FETCH`、`TOP 0` 探测）。表名按方言 `db.table` / `schema.table` 限定；所有标识符 quote，所有值参数化。
- 聚合行扫描按 `CompiledQuery.Columns` 取列：缺列/类型不符/行数≠1 → `BAD_DATA`；空集 `SUM` 为 NULL 时按 0 处理（count 型）或按 `on_empty`（aggregate/cross_table 用 `ObservedNull`）。
- 判定（`Evaluate`）：阈值严格比较（等于不越界）；`null_percent` 用 `max_null_count` 或 `max_null_pct` 二选一；`row_count` 用 min/max/exact + warn；`freshness` 的 lag 在 SQL 内用数据库时钟算（PG `EXTRACT(EPOCH …)`、MySQL `TIMESTAMPDIFF`、MSSQL `DATEDIFF(SECOND, …, SYSUTCDATETIME())`）；空数据由 `on_empty ∈ {NOT_EVALUATED(默认), PASS, FAIL}` 决定并写入 `QualityThresholds` 快照。
- `row_count_change`：基线 = 本规则最近一条 PASS/WARN/FAIL 结果的 `observed_value`（无 7 天/间隔窗口，见 §7）；`baseline` 为空或 0 → `on_empty`；变化比例与 `max_increase_pct`/`max_decrease_pct` 比较。
- `aggregate`：观测值 = 聚合值，`total_rows = COUNT(*)`；区间判定（默认开区间，`inclusive` 改闭）；NULL 聚合 → `on_empty`；`PERCENTILE` 仅 PG/MSSQL。
- `group_check`：`total_rows` = 组数、`failed_rows` = 越界组数；阈值按 COUNT 或 PERCENT；样本返回违规组（分组列 + 组行数）。
- `cross_table`：左右各一个 COUNT/SUM/AVG/MIN/MAX（不同实例/库不支持）；只应用左表 filter；`observed = |left-right|`，判定用 `max(0, |diff| - tolerance)` 与 fail/warn 阈值（`>`）；任一侧 NULL → `on_empty`。
- `cel_expression`：Go 内求值；SQL 只取 `SELECT <引用列> FROM <t> [WHERE filter] LIMIT sample_rows`（默认 1 万、上限 10 万行，列 ≤20，表达式 ≤4 KiB）；表达式只能引用目标列（名字必须是 CEL 标识符）；`sampled=true`；`matches()` 模式必须是常量。
- `schema_drift`：把 registry 当前列与保存的快照比对，漂移项 = 缺失列 / 类型不符（`check_types`，未设置时默认 true）/ 顺序不符（`check_order`，默认 false）/ 多余列（`allow_extra_columns=false` 时）；`total_rows = len(expected_columns)`、`failed_rows = 漂移项数`、样本是漂移项列表；无 SQL、无只读源。
- `custom_sql`：人写的单条 SELECT/WITH（≤8 KiB，omni 解析校验）；执行 `SELECT COUNT(*) FROM (<sql>) dq`，样本 `SELECT * FROM (<sql>) dq LIMIT n`；无 `total_rows`，因此拒绝 PERCENT。SQL 本身负责 filter，DQ 不重写。
- **PERCENT 允许集（10 类）**：`unique`、`unique_combination`、`accepted_values`、`range`、`regex`、`string_length`、`referential_integrity`、`cross_column`、`group_check`、`cel_expression`；其余（`custom_sql`、`freshness`、`row_count`、`row_count_change`、`null_percent`、`schema_drift`、`aggregate`、`cross_table`）保存期拒绝。
- 对象类型：TABLE/EXTERNAL_TABLE 全类型；VIEW/MATERIALIZED_VIEW 只允许表级规则（`row_count`、`row_count_change`、`freshness`、`custom_sql`、`schema_drift`）。
- 执行 SQL 截断 8 KiB；样本值截断 256 字节；样本行数默认 20、上限 100（工作区设置可下可上，硬上限 100）；结果内存上限 1 MiB。

### 3.3 执行与调度（`backend/runner/qualityrunner`）

| 常量 | 值 | 含义 |
| --- | --- | --- |
| tick | 10s（不 jitter） | 到期扫描 |
| 每轮 due 扫描 | ≤200 条；每实例每 run ≤50 条 | 分批 |
| 并发 | 至多 2 个实例同时执行；单实例串行 | 进程内 |
| 手动队列 | 64 | 满则 API 放弃 run |
| 单 run 超时 | 15m | 覆盖所有规则与查询 |
| 单规则查询超时 | 60s（固定；无按规则覆盖） | `QueryOptions.Timeout` |
| 结果/样本上限 | 1 MiB / 20 行（设置） | `ExecOptions` |
| 风暴阈值 | 一次 run FAIL 规则 >10 | 合并通知 |
| 启动清扫 | 15m 前的 PENDING/RUNNING → ABANDONED | 不补跑 |

- 循环：先排空手动队列，再处理 tick；每次扫描/手动前重读设置（读失败保留旧值）。
- 每日执行上限：UTC 日起算 `COUNT(quality_run)`；触顶时**跳过本轮 due 扫描**（规则保持 due，次日继续），不写 ERROR、不告警（§7）。
- 到期规则按实例分组、按 ≤50 切 run；每个 run 先落 PENDING，执行时标 RUNNING；执行前推进 `next_run_at`（失败不回退）。
- 逐规则：inflight 去重（重复触发记 `SKIPPED`）→ 读最新 revision 快照（head revision 不一致时重读 head）→ resolve target（孤儿 → `SKIPPED(ORPHANED)`）→ `RequireTarget`/`ValidateConfig`（不满足 → `ERROR(UNSUPPORTED)`）→ 元数据规则跳过连接；其余按实例 `AcquireInstanceConnection` 取连接预算 + `GetReadOnlyDatabaseDriver` → `Executor.Execute` → `row_count_change` 补基线 → `Evaluate`。
- 单规则 panic 隔离为 `ERROR(EXECUTION)`；结果整批插入（`persistResults`），插入失败 → run `ABANDONED`；成功则问题迁移 + 通知（best-effort），最后写 counts/outcome 并 `COMPLETED`。
- 只读通道：`db.ReadOnlyQueryer`；PG 事务内 `SET LOCAL statement_timeout`，MySQL 会话 `MAX_EXECUTION_TIME` 并复位；其余依赖只读账号 + 语句白名单。无 READ_ONLY 数据源 → `ERROR(NO_READ_ONLY_SOURCE)`。
- 错误码（`dataquality.ClassifyError`，写 `error_code`）：`CONNECTION`、`AUTH`、`TIMEOUT`、`PERMISSION`、`UNSUPPORTED`、`SQL_SYNTAX`、`BAD_DATA`、`LIMIT_EXCEEDED`、`ORPHANED`、`QUOTA`、`NO_READ_ONLY_SOURCE`、`EXECUTION`（未归类的驱动错误，消息脱敏 + 截断 1 KiB）。
- 日志：每 run 一行 summary（run/规则数/六态计数/耗时）；`ERROR` 结果打 WARN（rule/target/code/耗时，不打 SQL 与样本值）；无 metrics。

### 3.4 问题、通知与静音

- issue 行：`resource_id`、`rule_id`、target、`severity`（复制规则严重级别）、first/last seen run+时间、`occurrence_count`、`resolution_reason/note`、ack 人/时间、`muted_until`。部分唯一索引 `(rule_id) WHERE status <> 3` 保证每规则至多一个未解决问题。
- 通知 detail（store/v1 同形）：`kind`、`rule`、`rule_title`、`target_guid`、`target_path`、`from_status`/`to_status`、`severity`、`observed_value`/`observed_unit`、`issue`、`run`、`failed_rows`/`total_rows`、`error_code`、风暴的 `failed_rule_count`/`total_rule_count`；不含正文/样本值。
- 收件人：ISSUE_OPENED/RECOVERED → 规则 owner，owner 为空/无效 → workspace admins；EXECUTION_FAILED/STORM_SUMMARY → admins。去重：`notification.DataQualityDedupeKey`，窗口 1h（storm 的 key 以 run 为 target，等效每 run 一条）。
- 静音：规则级 `mute_until` 与问题级 `muted_until` 只压制 ISSUE_OPENED/RECOVERED；EXECUTION_FAILED 不受静音；风暴抑制逐条消息但 issue 照建、恢复消息照发。
- 通知失败只记日志；`notification` 表永久保留，因此 detail 必须限长（构造函数里截断）。

### 3.5 LLM 三能力（总开关默认关闭）

| 能力 | RPC | prompt 版本 | 输入 | 输出/落库 |
| --- | --- | --- | --- | --- |
| L1 规则草稿 | `SuggestQualityRule` | `dq.rule_draft.v1` | 目标 schema/DDL/列注释、已有规则摘要、意图、请求列 | 1–5 条草稿 → 校验/编译 → `quality_suggestion(RULE_DRAFT, NATURAL_LANGUAGE, PENDING)` |
| L2 失败解释 | `ExplainQualityResult`（server streaming） | `dq.explain.v1` | result 快照、rule revision 的 config、最近 10 条结果、schema history ≤5、lineage ≤10、open issue | hypotheses+evidence+next_step → `data_quality_llm_cache`（命中则回放） |
| L3 标注与建议 | `SuggestColumnLabels` | `dq.column_label.v1` | 近 7 天画像摘要（含 Top-K 摘要）、schema、lineage、已有标注；每批 ≤20 列、≤3 批 | label 建议 + 确定性映射的规则草稿 → `quality_suggestion(PENDING)` |

- provider：`llm.ResolveConfig(configs, allowlist, settings.LLMProviderProfile)`；未指定时取允许列表首个；无可用 profile → `FailedPrecondition`。
- 结构化输出：`temperature=0` + JSON mode；`llm.ParseJSON[T]`（剥 code fence、`DisallowUnknownFields`、拒绝多文档/空/null、validator 校验）；解析失败不缓存。L1 草稿词表仅 12 类 v1 规则（`custom_sql`/CEL/v1.1 四类不可由模型提出）；草稿再经列存在/列作用域/`ValidateRule` 过滤，全部被丢则返回空列表 + Warn 日志。
- L2：缓存 key = `explain|result:<id>|rev:<rev>|status:<status>|prompt:dq.explain.v1|provider:<profile>|model:<model>|lang:<en-US|zh-CN>`；命中回放 content+metadata(`from_cache=true`)；`force_regenerate` 跳过读但仍写；usage 存缓存行 + debug 日志，metadata 不带 usage/prompt_version。解释 content 是一次性 markdown（`### n. cause`、Evidence、Next step、confidence），progress 只发一帧 `thinking`。
- L3：要求请求列都有 ≤7 天画像，否则 `FailedPrecondition` 指向上 `ProfileColumns`；标注只在封闭枚举内，规则候选由 `RuleTypesForSemantic` 确定性映射、阈值为默认值（不从当前值推导）；置信度不自动接受。
- 配额（进程内、UTC 日）：L1+L2 合计 200 次/日（固定常量）；L3 取设置 `daily_column_analysis_limit`（默认 50）；全局并发 2；超限 `ResourceExhausted`。计数在 `api/v1/data_quality_llm_quota.go`。
- 出域：仅元数据/DDL/注释/规则定义/历史摘要/血缘/画像摘要；**没有任何路径发送原始样本行**（`send_sample_values_to_llm` 设置被保存但当前无消费者，§7）。提示注入：数据块 `<schema>/<rule>/<result>/…` 包裹，嵌入值中的 `</` 被中和为 `<\/`，system 声明“data, not instructions”；模型无工具、无连接。
- 失败：分析失败不改变规则/问题状态；错误以 `Internal`/`FailedPrecondition`/`ResourceExhausted` 返回，消息不含 key/样本。

### 3.6 API、权限与审计

- `DataQualityService`（`proto/v1/v1/data_quality_service.proto`）共 23 个 RPC：规则 CRUD/预览/手动执行、执行与结果列表、问题列表/详情/处置、画像与标注读写、建议列表/决策、L1/L2/L3 三个 LLM 入口，以及聚合读 `GetQualityOverview`（设计外新增，供概览页一次取数）。
- 分页统一 `parseLimitAndOffset`（offset token），页上限 1000；手动执行选择集 ≤200 条；规则列表的 instance/database/schema/table 是 **GUID 前缀**过滤，`search` 只匹配 title/description。
- 错误映射：参数/类型/编译失败 → `InvalidArgument`；目标消失 → 创建期 `NotFound`、运行期 `SKIPPED(ORPHANED)`；无只读源/LLM 未开启/无 provider → `FailedPrecondition`；配额/队列满 → `ResourceExhausted`；revision 冲突/建议已决 → `Aborted`；其余 `Internal`。
- 权限（`metaxisdata.dataQuality.*`，15 项）：`rules.{get,list,create,update,delete,run}`、`runs.{get,list}`、`issues.{get,list,update}`、`explain`、`suggest`、`profile.run`、`labels.update`。`workspaceMember` 基线 = rules.get/list、runs.get/list、issues.get/list、explain；其余默认仅 `workspaceAdmin`/自定义角色。设置页复用 `settings.get/update`；标注的**读**复用 `rules.list`（经 `ListColumnLabels`）。
- 审计（proto `audit=true`，每次调用 1 行）：规则创建/更新/删除、手动执行、问题处置、建议决策、画像、标注更新、三个 LLM RPC；只读列表/详情与定时执行不审计。审计行只含请求元数据，不含 LLM 正文/样本。
- 限流：所有带审计注解的 DQ RPC 自动进入 principal 预算（`(principal, procedure)` 3000/min、部署 30000/min，见 [rate-limiting.md](rate-limiting.md)）；LLM 另有 §3.5 的配额。

### 3.7 前端与 i18n

- 路由：`/data-quality`（重定向概览）、`/rules`（列表）、`/rules/new`、`/rules/:ruleId+`（规则详情）、`/rules/:ruleId+/edit`、`/suggestions`、`/issues`、`/issues/:issueId+`、`/runs`、`/runs/:runId+`；`/settings/data-quality` 挂在 Settings。每条路由带 permission，侧栏按权限隐藏（不置灰）。父路由 `contentWidth: "full"`。
- 导航：侧栏第 6 组“数据质量”（概览/规则/执行/问题/建议），SettingsNav 增加“数据质量”。规则详情从规则列表标题、执行详情的规则名、问题详情的规则名与资产页质量区进入（`rules.get` 在 member 基线内，只读可见）。
- 页面/组件：`pages/data-quality/`（概览、规则列表、规则详情、规则编辑器、执行列表/详情、问题列表/详情、建议队列）；`components/data-quality/`（状态/严重级别徽标、趋势图、最近执行状态色块条、结果表、规则表单与 filter 编辑器、失败样本、SQL 查看、建议卡、字段分析面板、AI 解释抽屉、草稿对话框、目标选择器、资产页质量区）。复用 shadcn-vue 原语，不新增 tabs 原语。
- 规则列表筛选：标题/描述文本、**目标范围**（实例 → 数据库 → Schema → 表/视图；复用 `TargetPicker` 的 `mode="scope"`，在任一层可停，点表/视图行直接筛到该表）、维度、严重级别、状态；改动后点“应用”才查询。`TargetScopeFilter` 只持有待应用值，请求由 `ListQualityRules` 的 instance/database/schema/table（GUID 前缀）字段承受。
- 规则详情的信息层级：顶部“最近结果”大状态（图标+状态词+相对时间+实际值/阈值/失败行数）→ 最近 20 次状态色块条（旧→新，按状态着色，点击进 run 详情）→ 状态计数与通过率 → 最近执行表 → 规则定义（目标/字段/类型/维度/严重级别/调度/版本/时间/标签）。运行与编辑分别按 `rules.run`、`rules.update` 显示。
- API/工具：`api/data-quality.ts` + `api/client.ts` 的 `dataQualityClient`；纯函数在 `src/utils/dataQuality*.ts`（状态/严重级别映射、表单校验、LLM 提示、概览、设置、建议）；`lib/notificationText.ts` 渲染通知文案；无跨页 Pinia 模块。
- i18n：`dataQuality.*`、`dataQualitySettings.*`、`menu.dataQuality*`、`notifications.dataQuality*`；术语进 `frontend/glossary.json` 的 **Data Quality** 分类；动态 key 已登记 `frontend/scripts/check-vue-i18n.mjs` 的 `DYNAMIC_PREFIXES`。
- 布局门禁：新路由登记 `frontend/scripts/audit-ui-layout.mjs`（含各表 `tableTopMaxY` 预算）；`ERROR` 与 `FAIL` 文案/徽标必须不同；编辑器保存前必须干跑（`PreviewQualityRule`）。

### 3.8 画像、语义标注与建议队列

- 画像（确定性、无 LLM）：每表一条聚合查询（行数/null/distinct/min/max/长度）+ 一条样本查询（`LIMIT 1000`）；Go 内算 Top-K（仅 distinct ≤20 的列）、样式、长度分布；值截断 64 字节、最多 20 个；写 `column_profile`（列级 upsert，`sample_method="limit"`、`engine_version="dq.v1"`）；单次 ≤设置列数、120s、4 MiB 结果。
- 没有原生文本/排序语义的类型（json/jsonb、xml、空间、二进制、数组、用户自定义/enum 等，见 D-34）只取非空计数：distinct 来自样本并标 `distinct_estimated=true`（样本覆盖全表时为精确值），不取 min/max、长度和样式；前端与 L3 提示词对估算值显示 `≈`/`~`。形状判定在 `profileShapeFor`，文本类型白名单按引擎族维护（MSSQL 的 `text`/`ntext` 不可比较，因此不算文本）。
- 标注：`column_semantic_label` 唯一键 `(table_guid, table_type, column_name)`；封闭枚举 `QualitySemanticType`；`source=MANUAL|LLM`、`status`、`confidence`、`evidence`；人工标注可覆盖（Upsert），LLM 建议不自动覆盖。
- 规则候选映射（代码固定）：`ID/BUSINESS_KEY → unique+null_percent`；`EMAIL/PHONE/URL/UUID/IP → regex+null_percent`；`COUNTRY_CODE/CURRENCY/ENUM → accepted_values`；`AMOUNT/QUANTITY → range+null_percent`；`PERCENTAGE → range(0-100)`；`TIMESTAMP/DATE → freshness+range`；`FREE_TEXT → string_length+null_percent`；`UNKNOWN → 不生成规则`。阈值为默认值，不从当前值推导。
- 建议：`quality_suggestion(kind=RULE_DRAFT|LABEL, source=NATURAL_LANGUAGE|PROFILE, payload, status=PENDING, provider/model/prompt_version)`；决策只允许 PENDING→ACCEPTED/REJECTED（并发重复决策返回 `Conflict` → 客户端 `Aborted`），接受 LABEL 即 upsert 标注，接受 RULE_DRAFT 只改状态。

## 4. 配置映射

### 4.1 设置（`WorkspaceProfileSetting.data_quality`，零值=默认）

| 字段 | 默认 | 解析规则 | 生效点 |
| --- | --- | --- | --- |
| `enable_llm_analysis` | false | 按值 | L1/L2/L3 总开关；关闭时规则引擎不受影响 |
| `llm_provider_profile` | 空 | trim；空=允许列表首个 | DQ 分析 provider |
| `store_failure_samples` | true | message 缺席时取默认；显式 false 生效 | 结果是否取失败样本 |
| `failure_sample_limit` | 20 | ≤0→20；>100→100 | 每结果样本行数 |
| `default_schedule_interval` | 1h | ≤0→1h；夹到 15m…7d | 新建规则默认间隔 |
| `daily_execution_limit` | 10000 | ≤0→10000 | runner 日执行上限（触顶跳过扫描） |
| `daily_column_analysis_limit` | 50 | ≤0→50 | L3 日分析次数 |
| `max_columns_per_analysis` | 50 | ≤0→50；写入校验上限 200 | 单次画像/标注列数 |
| `send_sample_values_to_llm` | false | 按值 | **当前无消费者**（§7） |

保留期不在设置里（常量）：结果随 run 90d、关闭问题 90d、画像/LLM 缓存 7d、已决建议 90d —— 见 §7，清理未接线。

### 4.2 规则 config（`proto/store/store/data_quality.proto`，公共字段在 oneof 外）

| 规则 | config 字段 |
| --- | --- |
| `row_count` | `min_rows`、`max_rows`、`exact_rows`、`warn_min_rows`、`warn_max_rows` |
| `row_count_change` | `max_increase_pct`、`max_decrease_pct`、`warn_increase_pct`、`warn_decrease_pct` |
| `null_percent` | `column`、`max_null_count`/`warn_null_count` 或 `max_null_pct`/`warn_null_pct`（二选一）、`null_values`（≤20，自定义缺失值） |
| `unique` | `column`、`count_nulls`、`fail_threshold`、`warn_threshold` |
| `unique_combination` | `columns`、`all_columns`、`count_nulls`、`fail_threshold`、`warn_threshold` |
| `accepted_values` | `column`、`values`（≤200）、`allow_null`、`fail_threshold`、`warn_threshold` |
| `range` | `column`、`min`/`max`（字符串，按列类型解析）、`inclusive`、`fail_threshold`、`warn_threshold` |
| `regex` | `column`、`pattern`、`fail_threshold`、`warn_threshold` |
| `string_length` | `column`、`min_len`、`max_len`、`fail_threshold`、`warn_threshold` |
| `referential_integrity` | `child_column`、`parent_table`（`schema.table`/`database.table`）、`parent_column`、`allow_null`、`fail_threshold`、`warn_threshold` |
| `cross_column` | `left_column`、`right_column`、`op`、`tolerance`、`fail_threshold`、`warn_threshold` |
| `freshness` | `timestamp_column`、`max_lag_seconds`、`warn_lag_seconds` |
| `custom_sql` | `sql`（≤8 KiB）、`fail_threshold`、`warn_threshold` |
| `cel_expression` | `expression`（≤4 KiB）、`columns`、`sample_rows`（默认 10000，上限 100000）、`fail_threshold`、`warn_threshold` |
| `schema_drift` | `expected_columns[{name,type}]`、`check_types`（可选，未设置=true）、`check_order`（默认 false）、`allow_extra_columns`（默认 false）、`fail_threshold`、`warn_threshold` |
| `group_check` | `columns`（1–5）、`min_rows_per_group`/`max_rows_per_group`（至少一个）、`fail_threshold`、`warn_threshold` |
| `aggregate` | `column`、`function`（SUM/AVG/MIN/MAX/STDDEV/PERCENTILE）、`percentile`、`min`/`max`（至少一个）、`warn_min`/`warn_max`、`inclusive` |
| `cross_table` | `right_table`、`left_function`/`right_function`（COUNT/SUM/AVG/MIN/MAX）、`left_column`/`right_column`、`tolerance`、`fail_threshold`、`warn_threshold` |
| 公共 | `threshold_metric`（COUNT/PERCENT）、`filter`（AND 子句）、`on_empty` |

### 4.3 JSONB 列 → store 消息

| 列 | 消息 |
| --- | --- |
| `quality_rule.target_path` / revision | `QualityTargetPath` |
| `quality_rule.config` / revision | `QualityRuleConfig`（oneof + 公共字段） |
| `quality_run.counts` | `QualityRunCounts`（六态 + total） |
| `quality_rule_result.thresholds` | `QualityThresholds`（warn/fail、upper、comparison、on_empty 快照） |
| `quality_rule_result.sample` | `QualityFailureSamples`（`FailureSample{ColumnValue{column,value}}`，值已截断） |
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

### 4.5 错误码（结果行 `error_code`）

`CONNECTION` / `AUTH` / `TIMEOUT` / `PERMISSION` / `UNSUPPORTED` / `SQL_SYNTAX` / `BAD_DATA` / `LIMIT_EXCEEDED` / `ORPHANED` / `QUOTA` / `NO_READ_ONLY_SOURCE` / `EXECUTION`（未归类驱动错误，脱敏截断）。LLM 侧错误以 Connect code 返回，不写结果行。

## 5. 不变量

| # | 不变量 | 守卫 |
| --- | --- | --- |
| I-1 | 目标身份 = `(guid, object_type)` + 路径快照；解析校验 registry 当前行、实例未删、数据库未隐藏 | `dataquality.ResolveTarget` 单测 + store 读路径 |
| I-2 | head config 与最新 revision config 一致；结果记实际执行的 revision + engine version | 更新事务 + `quality_rule_test.go` |
| I-3 | 每规则至多一个未解决 issue | 部分唯一索引 + `OpenQualityIssue`/`PlanIssueTransition` 测试 |
| I-4 | run rollup = 最坏 rank；`ERROR` 与 `FAIL` 不合并 | `Rollup`/`CountResults` 单测 + 前端徽标测试 |
| I-5 | 空数据语义由 `on_empty` 决定并写入阈值快照；默认 NOT_EVALUATED | evaluator 单测 |
| I-6 | 查询型规则只走 READ_ONLY 数据源；SQL 只允许单条 SELECT/WITH | `ReadOnlyQueryer` + ValidateRule/Compile 双层 |
| I-7 | 结果/样本在配置上限内，超限置 `truncated=true`（不是失败） | 扫描器 + 测试 |
| I-8 | 告警按状态迁移；issue 通知 1h 去重；执行失败不可被静音；风暴只抑制逐条消息 | issue 迁移表 + 通知单测 |
| I-9 | 手动 run 只执行 API 选择的规则 id，绝不回落到“该实例全部启用规则” | 进程内队列 `EnqueueQualityRun(runID, ruleIDs)` + runner 测试 |
| I-10 | scheduled run 先推进 `next_run_at` 再执行（失败不回退）；inflight 去重保证同一规则不并发打库 | runner 单测 |
| I-11 | LLM 输出必须通过封闭 schema 校验才落库/缓存；解析失败不缓存 | `llm.ParseJSON` + 各 `Parse*` 单测 |
| I-12 | LLM 不接触目标库、无工具、不自动建规则/启用/执行；L1/L3 只产 PENDING 建议 | handler 流程 + 建议决策只改状态 |
| I-13 | 提示中的数据库内容是不可信数据块（`</` 被中和）；模型无连接/凭据 | prompt 构造单测 |
| I-14 | 结果资源名最后一段是行 id；其余资源名用不可回收 resource_id | convert 单测 + handler 解析 |
| I-15 | 软删除保留 revision/结果/问题；表不物理删除规则 | archive 路径 + store 查询过滤 |
| I-16 | 单副本：inflight/配额/LLM 计数都在进程内；重启收敛、不补跑 | 代码结构 + runner 测试 |
| I-17 | 编译 SQL 的占位符与 `Args` 必须一起下发：`db.ReadOnlyQueryer.QueryReadOnly(ctx, sql, args, opts)`；预览、执行、画像都传各自的 args（画像无参数传 nil） | `execution_test.go` 的参数断言 + 真 server 集成场景（带 filter 的 range 规则 dry run/执行） |

## 6. 失败模式

| 场景 | 行为 |
| --- | --- |
| 实例没有 READ_ONLY 数据源 | 查询型规则**创建即拒绝**（`FailedPrecondition`，文案含配置入口）；创建后数据源被移除则运行期 `ERROR(NO_READ_ONLY_SOURCE)`；预览/执行同样拒绝；不回退 ADMIN |
| 目标库连接失败/凭据失效 | 结果 `ERROR(CONNECTION/AUTH/PERMISSION)`，执行失败通知（admins）；下一次排期自然重试 |
| 查询超时 | `ERROR(TIMEOUT)`；不自动重试 |
| 结果集超上限 | 截断并 `truncated=true`，状态照常判定；UI 显示“已截断” |
| 保存后列被删/类型变了 | 运行期 `Error(UNSUPPORTED)`（ValidateConfig 失败）；预览阶段则拒绝保存 |
| 目标被删/实例被删/数据库隐藏 | 规则显示 `orphaned_reason`；运行期 `SKIPPED(ORPHANED)`；不通知 |
| 同一规则并发触发 | 进程内 inflight 去重 → 后到者 `SKIPPED` |
| 规则编辑中 | 结果永远带实际 revision；历史不变 |
| 服务重启 | 进行中 run → 启动时 `ABANDONED`；规则按 `next_run_at` 收敛；不补跑 |
| 单规则 panic | 该规则 `ERROR(EXECUTION)`，其余规则继续 |
| 结果批量写失败 | run `ABANDONED`，不迁移 issue、不发通知 |
| 通知写失败 | 只记日志；结果与问题状态不受影响 |
| 告警风暴（run FAIL 规则 >10） | 逐条 opened/execution-failed 抑制，发一条 STORM_SUMMARY；issue 照建、恢复照发 |
| 规则/问题静音期内失败或恢复 | 不发送问题通知；执行失败通知不受影响 |
| 每日执行配额触顶 | 跳过本轮 due 扫描（规则保持 due，次日继续）；不写 ERROR、不告警（§7） |
| 手动队列满 | 该 run 标 `ABANDONED`（outcome ERROR）并返回 `ResourceExhausted` |
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
| 1 | 日执行配额触顶 | 跳过本轮 due 扫描并记日志（规则保持 due，次日继续）；**不**写 `ERROR(QUOTA)`、不发告警——与需求文案有差异，避免为一条配额事件伪造规则结果 |
| 2 | 单规则查询超时 | 固定 60s；设计提到的“规则 config 可覆盖到 300s”没有对应字段，未实现 |
| 3 | `send_sample_values_to_llm` | 已存储/展示但无消费者：所有 LLM 路径都不发原始样本行（比开关默认更严格）；接线前该开关无效 |
| 4 | 每 workspace/每表规则数上限 | 已实现（1000 / 100，创建时 `ResourceExhausted`） |
| 5 | 创建期只读源校验 | 已实现：查询型规则创建时若无 READ_ONLY 数据源直接 `FailedPrecondition`；元数据规则不要求 |
| 6 | `row_count_change` 基线窗口 | 已实现：≤7 天且 ≤2×interval，否则视为无基线 → `NOT_EVALUATED` |
| 7 | 保留期清理 | 已实现：maintenance 清理 run(+results)/resolved issue/column_profile/LLM cache/decided suggestion（§8 `backend/runner/maintenance`） |
| 8 | L2 的 `ExplainSQLMetadata` 无 usage/prompt_version | usage 只在缓存行与 debug 日志；未改 proto（有意） |
| 9 | 建议队列无 Get RPC | 编辑器 `?suggestion=` 预填按队列全量查找后匹配（数据量有界；如需可加 GetQualitySuggestion） |
| 10 | 多副本 | 不支持（单副本假设，见 [security-posture.md](../../docs/security-posture.md)）；两个副本会重复执行、LLM 配额各自计数 |
| 11 | 分布漂移/ML 异常、分组内多种检查（每组 null/aggregate）、RuleSet | 非目标（§非目标）；RuleSet 在 v1.1 之后单独评审 |

## 8. Where things live

| 面 | 位置 |
| --- | --- |
| 领域/编译/判定 | `backend/component/dataquality/`：`model.go`、`types.go`、`target.go`、`validate.go`、`compile.go`、`compile_rules.go`、`compile_util.go`、`compile_samples.go`、`dialect.go`、`support.go`、`evaluate.go`、`execution.go`、`executor.go`、`errors.go`、`filter.go`、`issue.go`、`settings.go`、`profile*.go`、`cel.go`、`label.go`、`prompt.go` |
| 只读查询能力 | `backend/plugin/db/query.go`（`ReadOnlyQueryer`/`QueryOptions`/`QueryResult`、扫描上限）；`plugin/db/{mysql,pg,mssql,starrocks}/query.go`；`backend/component/dbfactory/readonly.go`（`ErrNoReadOnlyDataSource`） |
| runner | `backend/runner/qualityrunner/qualityrunner.go`（调度/执行/问题迁移/通知）；`backend/server/server.go` 注册第 5 个 runner；保留期清理在 `backend/runner/maintenance/maintenance.go` |
| API | `backend/api/v1/data_quality_service.go`（CRUD/run/result/issue/profile/preview/overview）、`data_quality_convert.go`、`data_quality_llm_analysis.go`（L1/L2）、`data_quality_label_analysis.go`（L3）、`data_quality_llm_client.go`、`data_quality_llm_quota.go`、`data_quality_overview.go`、`data_quality_cross_table.go` |
| store | `backend/store/quality_{rule,run,result,issue,profile,label,suggestion,llm_cache,overview}.go`（9 张表） |
| proto/迁移 | `proto/store/store/data_quality.proto`、`proto/v1/v1/data_quality_service.proto`、notification/setting 扩展；`backend/migrator/migration/0.1/0024##data_quality.sql` + `LATEST.sql` |
| 前端 | `frontend/src/pages/data-quality/`、`pages/settings/DataQualitySettingsPage.vue`、`components/data-quality/`、`api/data-quality.ts`、`utils/dataQuality*.ts`、`lib/notificationText.ts`；路由 `router/index.ts`，侧栏 `AppSidebar.vue`，`SettingsNav.vue` |
| 单元测试 | `component/dataquality/*_test.go`（18 个测试文件：编译 golden ×4 方言、判定、filter、custom_sql、CEL、画像、标注/prompt、schema_drift、group_check、aggregate、cross_table、null_values、issue、execution、errors）；`store/quality_*_test.go`；`runner/qualityrunner/qualityrunner_test.go`；`api/v1/data_quality_*_test.go`；`component/notification/data_quality_test.go`、`api/v1/notification_service_test.go`；前端 `*.test.ts` 与 `lib/notificationText.test.ts` |
| 集成测试 | `backend/test/integration/runner/data_quality_service_test.go`（规则生命周期、问题/通知、元数据+只读规则、画像/标注、LLM 默认关闭） |
| 权限 | `backend/common/permission/permission.json` + `permission_gen.go`；`store/predefined_roles.go`（member 基线）；前端镜像 `frontend/src/lib/permissions.ts` |
| 启用点 | 迁移 `0024`（随二进制自动执行）；每个实例需配 `READ_ONLY` 数据源；LLM 需设置页开启 `enable_llm_analysis` 且存在允许的 provider profile；无其他开关 |
| 门禁 | Go：`gofmt`、`golangci-lint`、`go test ./component/dataquality/ ./store/ ./runner/qualityrunner/ ./api/v1/`；proto：`buf format/lint/generate`；迁移链测试；前端：`biome:check`、`lint`、`i18n`、`type-check`、`test run`、`audit:ui` |

**非目标**（不做）：RuleSet/contract-like 整体验证（D-20）、模板/示例库、进程内 Python/WASM/任意代码、LLM-as-judge 判定、自由 SQL 生成并执行、自动修复数据/阈值、邮件/webhook/Slack/PagerDuty、质量分/scorecard、流式/行级检查、分布漂移/季节基线、MCP/CLI 暴露、跨副本调度、跨实例/跨库对账（cross_table 限同库）。
