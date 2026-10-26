# 数据质量（Data Quality）— Reference

> Status: **implemented**（v1 与 v1.1 全部落地；单副本假设）。维护参考，覆盖 `backend/component/dataquality/`、`backend/runner/qualityrunner/`、`backend/api/v1/data_quality_*.go`、`backend/store/quality_*.go`、`frontend/src/pages/data-quality/`。用户侧说明见 [docs/data-quality.md](../../docs/data-quality.md)；刻意决策见 [security-posture.md](../../docs/security-posture.md)。
> 关联：[instance-connections.md](instance-connections.md)（内部批次的连接准入）、[runner.md](runner.md)（后台执行器契约、单副本）、[notifications.md](notifications.md)（站内信与结构化 detail）、[iam-and-permissions.md](iam-and-permissions.md)（权限目录/注解）、[rate-limiting.md](rate-limiting.md)（principal 预算）、[object-ddl-storage.md](object-ddl-storage.md)（DDL 读取）、[lineage-analyzer.md](lineage-analyzer.md)（血缘上下文）、[frontend-ui.md](frontend-ui.md)（骨架/布局门禁）、[i18n-glossary.md](i18n-glossary.md)（术语）、[data-quality-target-centric.md](data-quality-target-centric.md)（目标工作台入口与契约化提案）、[security-open-items.md](security-open-items.md)。规则：[backend/AGENTS.md](../../backend/AGENTS.md)、[proto/AGENTS.md](../../proto/AGENTS.md)、[backend/migrator/AGENTS.md](../../backend/migrator/AGENTS.md)。

## 0. 它是什么

规则引擎负责**确定性判定**“数据是否达标”；LLM 只做三件辅助——把自然语言变成**规则草稿**、把失败结果**解释**成假设、把字段画像变成**语义标注与规则建议**。LLM 不执行判定、不写回数据、不自动建规则/启用/执行。

**对外唯一的执行实体是 `QualityExecution`（一条规则被检查一次）**。`quality_run` 退为**内部派发批次**：它只负责按实例领一份连接预算、聚合一个 tick 的规则、合并风暴通知并级联保留期，没有资源名、RPC 或页面。旧的 `quality_rule_result` 已并入 `quality_execution`。

```
SPA ──ConnectRPC──► DataQualityService (api/v1) ──► component/dataquality（校验/编译/判定/画像/CEL）
                               │                        │
                               │  手动运行/定时扫描：规划 PENDING quality_execution 行，按实例组成内部批次
                               ├──► store（PG：quality_* 等 10 张表）◄── qualityrunner（tick 10s / 手动队列）
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
| D-16 | 保留期：执行随内部批次 90d、关闭问题 90d、画像/LLM 缓存/LLM 调试日志 7d、已决建议 90d、已结束的 AI 起草任务 90d；由 maintenance runner 每 6h 清理 | 删除批次级联删除其执行；只清已解决的问题与已决建议；PENDING/RUNNING 的建议与任务不清理 |
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

| D-36 | 建议队列升级为“建议中心”：AI 起草是一等入口（可选多个目标、一次提交后台任务，单目标可限定列），规则编辑器保留上下文快捷入口；生成结果仍只入队 PENDING 建议 | 具体异步机制见 D-40…D-46（本行早期“前端顺序调用、不新增生成 RPC”的形态已被 D-40/D-42 取代）；队列按目标表分组（GUID 路径分段 + 实例标题展示），不新增“生成批次”实体 |
| D-37 | 批量创建规则走新 `BatchCreateQualityRules`（≤200 项，逐项校验/落库，结果按请求顺序返回，并回显 suggestion/name 便于客户端对齐）；`CreateQualityRuleRequest.suggestion` 让“从建议创建”成为一次请求：目标取自建议（请求 target 为空或与建议一致，不一致直接拒绝）、规则落库后接受建议，决策失败只记日志不回滚 | 建议的接受是创建规则的副作用；批量非原子，单项失败不中止其余；审计一次调用一行；仍受每表 100/每工作区 1000 条上限与 READ_ONLY 源校验；自动接受是非原子“先规则后决策”，并发/重试可能重复建规则（§7 #12） |
| D-38 | 批量决策走新 `BatchUpdateQualitySuggestions`（≤200 项，逐项独立）；接受 LABEL 仍立即写标注；已决/不存在的项作为该项错误返回而非整批失败 | 单条 `UpdateQualitySuggestion` 语义不变（已决仍 `Aborted`）；批量与单条共享决策/标注路径 |
| D-39 | 建议列表新增 instance/database/schema/table GUID 前缀过滤（与规则列表同形），并新增 `GetQualitySuggestion` 供深链；`target_guid` 过滤改为等值或子树匹配 | 复用 `metaGUIDPrefix`/`appendGUIDSubtreeCondition`，无迁移；表/视图目标下与精确匹配等价；编辑器 `?suggestion=` 不再全量扫描队列 |

| D-40 | 多目标 AI 起草从“浏览器顺序循环”改为**持久化后台任务** `QualitySuggestionJob`：一次提交 = 1 个意图 + 1..20 个目标，API 立即返回，runner 异步逐目标执行；编辑器单目标快捷入口仍同步（需要结果回填表单） | 关闭页面不丢任务；目标与进度存 JSONB；建议仍逐条进 `quality_suggestion`，不引入“生成批次”资源 |
| D-41 | 新增 `qualitysuggestionrunner`（tick 5s）：**启动时把所有 RUNNING 任务标 FAILED**（单副本，不存在合法在途任务；RUNNING 目标 FAILED、PENDING 目标 SKIPPED），原子 claim（`FOR UPDATE SKIP LOCKED`），逐目标持久化进度；单任务 panic 被捕获后标 FAILED 并继续循环；崩溃不续跑 | 已入队建议保留；重启后未完成任务不自动重试；runner 复用 `runner.Go` 的生命周期，但任务级 panic 不终止 runner |
| D-42 | L1 编排下沉到 `component/dataquality.Suggester`，同步 RPC 与 runner 共用同一实现；进程级 LLM 配额移入 `component/dataquality.LLMQuota`，由 API 与 runner 共享 | 提示词、校验、建议落库只有一份；同步入口与任务入口的额度/并发限制一致 |
| D-43 | 任务生命周期 `PENDING → RUNNING → COMPLETED / FAILED / CANCELED`；目标状态 `PENDING → RUNNING → DONE / FAILED / SKIPPED`；`COMPLETED` = 所有目标都被尝试（目标失败只记在目标上），`FAILED` = 任务级失败（配置不可用/配额在开始时耗尽/关机） | 客户端可显示“3/5 完成、1 失败、1 跳过”，不伪造部分完成态；SKIPPED ≠ FAILED（模型未被询问） |
| D-44 | 提交时 best-effort 预检当日 L1 配额（不足 → `ResourceExhausted`），不预占；每个目标执行时经 `LLMQuota.Begin` 扣减，任务中途耗尽则当前目标 FAILED、其余 SKIPPED、任务 FAILED | 并发提交可能轻微超发；已执行的调用仍被额度计数 |
| D-45 | 取消是协作式：PENDING 立即 CANCELED（目标全 SKIPPED）；RUNNING 只置 `cancel_requested`，runner 在目标边界停止并结束为 CANCELED；已入队建议保留；对终态任务取消幂等返回原任务 | 不取消进行中的 provider 调用；不回收已产生的建议 |
| D-46 | 任务完成不发通知；建议中心的“AI 起草任务”状态条在存在 PENDING/RUNNING 任务时每 2.5s 轮询，完成后刷新队列并自动选中当前页可见的新建议；任务历史抽屉打开且当前页存在活跃任务时也按 2.5s 刷新当前页 | 无新通知类型；不做任务详情路由/深链；轮询只在有活动任务时发生 |

| D-47 | DQ 的 LLM 输出预算不再由代码固定：取 provider profile 的 `max_output_tokens`（默认 32768，见 D-52）；`finish_reason=length` 归类为 `TRUNCATED`，错误文案直接告诉用户“模型在输出上限停下，草稿被丢弃——缩小意图或减少列后重试”；不做自动重试 | 大多数截断消失；仍截断时用户知道下一步；自动重试会再消耗一次配额且可能再次截断 |
| D-48 | 每个失败的起草目标持久化 **`QualityLLMDiagnostic`**（始终开启、不含正文）：分类码、provider/model/prompt 版本、`max_output_tokens`、`finish_reason`、provider 报告的 prompt/completion/total tokens、prompt 字符数、provider HTTP status、correlation id、`debug_captured` | 存在任务的 targets JSONB 中，随 Get/List 返回；不包含 prompt/response 正文，也不含样本行 |
| D-49 | 每次模型调用生成 **correlation id**；失败以 WARN 记录 feature/provider/model/code/finish_reason/max_tokens/prompt_chars/correlation_id，UI 展示并支持复制该 id | 运维可在服务端日志中按 id 定位；无需打开数据库 |
| D-50 | **原始 request/response 仅在 RuntimeDebug 打开时捕获**（与 ExplainSQL 同一开关）：写入 `llm_debug_log` 的 `kind`/`correlation_id`/`error_code` 列，保留 7 天；`GetQualitySuggestionJobTargetDebug` 按任务目标索引返回正文（`suggest` 权限、审计），未开启/已被清理时返回 `available=false` | DQ 调试行的 `error_code` 可能为空（捕获早于分类），分类码以任务目标诊断为准；正文只含元数据/DDL/意图/模型输出，不含样本行 |
| D-51 | 同步编辑器入口不新增 error-details 协议：服务端返回可执行文案，前端按文案识别截断并展示本地化建议，同时保留服务端原文 | 不改变现有 Connect 错误形状；任务路径才有完整结构化诊断 |

| D-52 | LLM provider profile 新增 `max_output_tokens`（JSONB metadata，无迁移）：0=服务端默认 **32768**，显式值 256–131072；该 profile 下的所有分析（L1/L2/L3、ExplainSQL）默认使用该预算，调用方可请求更小的上限；无 mask 的局部更新只写非零值，显式 `max_output_tokens` mask 才能清回默认 | 输出 token 是计费的实际部分，上限仍是 ceiling 不是预占；provider/模型自身上限更低时首次 400/422 会以 4096 重试一次，仍拒绝则诊断 `PROVIDER_STATUS`，可在 profile 调低 |
| D-53 | DQ 设置新增 `daily_llm_analysis_limit`（L1+L2，默认 **10000**，范围 1–1000000，0=默认）与 `llm_concurrency_limit`（默认 **8**，1–64，0=默认）；L3 的 `daily_column_analysis_limit`（默认 50）保持独立桶 | 设置页可读写，API 返回解析后的有效值；旧部署未保存设置时直接使用新默认 |
| D-54 | `LLMQuota` 的日额度与并发来自每次调用加载的 `LLMLimits`；并发从固定 channel 改为 mutex/cond 动态上限，设置变更对下一次分析生效；后台任务每个目标重新读取设置（provider 配置仍按任务解析一次）；计数仍进程内、UTC 日滚动、单副本 | `Begin(ctx, kind, limits)`；取消的等待者不消耗额度/槽；进程重启计数归零 |
| D-55 | 新增只读 `GetQualityLLMUsage`（`suggest` 权限、不审计）返回两个桶的 used/limit、in-flight/并发与下一个 00:00 UTC 重置时间；设置页“今日 AI 用量”卡片在加载/保存后刷新 | 不轮询；计数进程内，重启后显示 0；重置时间仍是下一个 UTC 零点 |
| D-56 | 删除固定的 200 次/日与并发 2，默认提升为 10000/8；输出预算默认 32768 | 显式写入过旧值的部署保留旧值，其余获得新默认 |
| D-57 | 同步入口与后台任务共用同一 quota 与 limits；后台任务逐目标重读 limits，任务提交时的额度预检仍 best-effort（不预占） | 并发设置变化不追溯已准入分析（Begin 时采样） |
| D-58 | 建议是**关于事实的提案**而非事件流：每条建议有确定性 identity（`label\|<column>` / `rule\|<type>\|<sorted columns>\|<discriminator>`），存 `quality_suggestion.signature`；部分唯一索引 `(target_guid, target_type, signature) WHERE status = 1` 保证同一 identity 至多一条 PENDING。生成 = **对账**（建立缺失、刷新既有、关闭失效、跳过事实），不是追加 | 连点/并发生成收敛到一行；刷新保留行 id 与 created_at，`refreshed_at` 记录刷新时间；数据库而非读后写是并发仲裁者 |
| D-59 | 生成前先与事实和决策对账：列已有标注（LABEL）或已有同 identity 的规则（RULE_DRAFT）→ 不分析/不入队；已有 PENDING → 默认不重复分析（省一次模型调用）；已决 → 默认不再提；`force_regenerate` 强制重析，但仍只刷新不重复。LABEL 只在 REJECTED 时抑制（标注被清除后允许重提），RULE_DRAFT 在 ACCEPTED/REJECTED 时都抑制（接受即视为完成） | 连点「建议标注」不再产生重复，也不再对已标注表重复花 LLM 额度；全部列已处理时直接返回 summary，不扣配额、不调模型 |
| D-60 | 新增建议状态 `SUPERSEDED`：事实出现（写标注、创建规则）时关闭同 identity 的 PENDING，并写 reason 到 `decision_note`；生成 pass 也会兜底关闭事实已存在的残留 PENDING。它不是人工决策：`decided_by` 为空，不计入接受/拒绝历史 | 队列不出现“与事实矛盾”的待审项；接受/拒绝只作用于唯一在审提案，语义确定；SUPERSEDED 随已决建议保留期清理 |
| D-61 | AI 起草任务幂等：`quality_suggestion_job.request_fingerprint` = hash(intent, language, force, 目标快照；目标内列排序去重)，部分唯一索引仅覆盖 PENDING/RUNNING；重复提交返回既有任务（响应 `deduplicated=true`），不启动第二次分析 | 双击/重试提交不会产生重复任务与重复建议；任务结束（终态）后同请求可再次提交 |
| D-62 | 生成结果带 `QualitySuggestionQueueSummary`（created/refreshed/superseded/skipped_existing_fact/skipped_pending/skipped_decided/skipped_columns）：`SuggestQualityRule`、`SuggestColumnLabels` 同步返回，起草任务逐目标持久化到 `targets` JSONB 并在任务面板展示 | 前端能如实说明“新建 N / 刷新 M / 跳过 K 及原因”；不再用“已排队 N 条”暗示追加 |
| D-63 | identity 的粒度只覆盖“同一条规则”的语义：**忽略**阈值类字段（range/string_length/freshness/aggregate 的上下界、cross_column tolerance、row_count 界限）、标题与理由；**参与** identity 的语义字段：filter、referential 的 parent 表/列、cross_column 的左右列与 op、aggregate 的 function/percentile、cross_table 的右表/两侧函数/左右列、unique 的 count_nulls、unique_combination 的 all_columns、accepted_values 的值集合与 allow_null、regex 的 pattern、schema_drift 的期望列快照与三个检查开关、custom_sql/cel 的文本摘要、threshold_metric（PERCENT）。`on_empty` 视为同一条规则的行为变体，不参与 identity | 同一 proposal 的阈值变体不会再各占一行；真正不同的规则（不同父表、不同聚合函数、不同正则/枚举集合、不同 schema 快照等）不会被错误合并 |
| D-64 | 进程内 LLM 分析合并：`analysisGroup` 以“feature + prompt 版本 + provider/model + 输出预算 + system/user prompt”的 SHA-256 为 key，同一 prompt 的在途调用只执行一次，结果（text/usage/diagnostic）分发给所有调用方；每个调用方拿到 usage/diagnostic 的副本（失败诊断可被调用方改写）。leader 的 ctx 取消时，仍存活的 follower 重试并成为新 leader | 双击“建议标注”、两个审阅者同时生成、工作台与编辑器并发提交同一请求，只付一次模型调用；队列仍由 identity upsert 收敛。单副本假设下进程内即可，不引入分布式锁 |
| D-65 | 生成结果里 `skipped_columns` 在 UI 中逐列展示（最多列 6 个，多余以“等 N 列”概括），接在 summary 文案后；仍保留计数原因 | 用户能看到“为什么这次没东西”，而不是只看到“跳过 N”；过长列表不撑爆 toast |
| D-66 | “从建议创建规则”改为**先 claim 再建规则**：`CreateQualityRule(suggestion)` 在通过全部校验后用 `DecideQualitySuggestion(PENDING→ACCEPTED)` 作原子 claim，claim 成功者才写规则；写规则失败时用 `ReopenQualitySuggestion(ACCEPTED→PENDING)` 回滚 claim（best-effort） | 并发/重试同一 suggestion 至多建出一条规则（另一路 `Aborted`）；崩溃窗口内 suggestion 已接受但无规则是合法状态（接受草稿本就不要求立刻建规则）；彻底消除 §7 #12 的并发重复建规则 |
| D-67 | 目标选择器（`TargetPicker`）在实例以下每一层都跟随 `ListMetadata` 的 page token 分页：首页按 meta type 分组返回、每组 token 独立推进，“加载更多”按需取下一页（失败可重试且不丢已载入行）；搜索走 `SearchMetadata`，以当前层父 GUID 前缀 + 每个目标 meta type 各一条请求，合并顺序固定为表/视图/物化视图并保留各组 token；实例层复用全量实例缓存、本地过滤并分批渲染。分页/搜索状态收敛在 `composables/useMetadataChildren`（ticket + `AbortSignal`，旧层级/旧关键词的迟到响应不能覆盖新结果）。规则表单的列清单改用 `listAllMetadata` 走完所有页，不再固定 `page_size=200` | 大 schema/数据库不再被截断到第一页，搜索是全量数据的服务端匹配而不是“只搜已加载的行”；`SearchMetadata` 一次只接受一个 `meta_type`，故表/视图/物化视图是三条共享一次防抖的请求；无后端/协议改动，`TargetPicker` 的三个调用方（规则编辑器、目标范围筛选、多选工作台）自动获得分页与搜索 |
| D-68 | 元数据页数据质量标签页新增**待审建议卡**：按 `status=PENDING`、`target_guid=当前目标`、`page_size=100` 读同一建议队列，**标注建议与规则草稿两种 kind 都显示**；非空才渲染，最多预览 5 条（标注在前、草稿在后），其余以“另有 N 条”指向建议中心（读满一页时计数带 `+`）。每行显示 kind、字段名/标题、语义标注或规则类型+严重级别、置信度、理由与刷新/创建时间；标注行的动作“接受/拒绝”（接受同时刷新字段标注），草稿行在可预填时给“在编辑器中打开”，并保留“拒绝”；卡头链接建议中心；“建议标注” pass 结束后（含部分批次失败）重新拉取，结果因此留在本页可见；队列读取失败时就地提示而不是伪装成空队列；决策失败在卡内提示并重读队列，清掉已被他人处理的陈旧行 | 草稿不在此卡直接建规则：复用编辑器预填，避免把批量创建/确认流程塞进资产页；列表接口属 `suggest` 权限，只读成员不请求也不显示，“在编辑器中打开”另需 `rules.create`（仅有 `suggest` 的审阅者只能拒绝草稿）；卡片只是同一队列的按目标预览（最多 5 条，不引入嵌套滚动容器），不改变队列语义；无后端/协议改动 |

## 2. 工作负载形状

### 2.1 实体与资源名

```
meta_registry_resource（只读）──< quality_rule >──< quality_rule_revision（不可变）
                                        │
                                        ├──< quality_run（内部派发批次，不对外）──< quality_execution（对外执行，一条规则一次）
                                        │                                              │
                                        │                                              └──< quality_issue（每规则至多一个未解决）
                                        ├──< quality_suggestion（待审建议）
                                        └──< quality_suggestion_job（后台 AI 起草任务，目标在 JSONB）──> quality_suggestion（任务产出的建议，按 resource id 引用）
                                                                        column_profile / column_semantic_label（按表 GUID+列名）
```

| 资源 | 名称 | id 来源 |
| --- | --- | --- |
| 规则 | `workspaces/{ws}/qualityRules/{quality_rule}` | `common.RandomString(12)`，不可回收 |
| 执行 | `workspaces/{ws}/qualityExecutions/{quality_execution}` | **数据库行 id**（BIGSERIAL 数字） |
| 内部批次 | 无 API 资源名；`quality_run.resource_id` 仅供内部诊断 | `common.RandomString(12)` |
| 问题 | `workspaces/{ws}/qualityIssues/{quality_issue}` | `common.RandomString(12)` |
| 建议 | `workspaces/{ws}/qualitySuggestions/{quality_suggestion}` | `common.RandomString(12)` |
| 起草任务 | `workspaces/{ws}/qualitySuggestionJobs/{quality_suggestion_job}` | `common.RandomString(12)`，不可回收 |
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

**AI 起草任务生命周期**（`quality_suggestion_job`，单副本、进程内 runner 驱动）：

| 阶段 | 行为 |
| --- | --- |
| `PENDING` | API 校验目标/列与 provider 可用性、best-effort 检查日配额后落库；runner 每 5s 原子 claim 一个 |
| `RUNNING` | runner 解析一次 provider 配置并写入任务；逐目标 `PENDING→RUNNING→DONE/FAILED`，每个目标后持久化 targets JSONB；每个目标开始前重读任务以尊重 `cancel_requested` |
| `COMPLETED` / `FAILED` / `CANCELED` | 终态带 `finished_at`；FAILED 用于配置不可用/配额在开始时耗尽/关机，COMPLETED 表示所有目标都被尝试；取消在目标边界生效并把未访问目标标 `SKIPPED` |

启动时把所有仍在 `RUNNING` 的任务标 FAILED（RUNNING 目标 FAILED、PENDING 目标 SKIPPED）：单副本进程刚启动，不可能存在合法的在途任务，因此不等待任何超时窗口；不续跑，已写入的建议保留。保留期：终态任务 90d 由 maintenance 清理。

**建议状态**：`PENDING` 是唯一在审状态；`ACCEPTED`/`REJECTED` 是人工决策且终局；`SUPERSEDED` 是服务端因事实已存在而关闭（标注已写、规则已建），没有 `decided_by`，只写 `decision_note` 说明原因。同一 identity 任意时刻至多一条 PENDING；同一 identity 之前的历史行（已决/已失效）保留为决策历史，生成 pass 默认不再提议已决项。

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
- `row_count_change`：基线 = 本规则**最近一条** COMPLETED 执行的 `observed_value`，且该执行必须是 PASS/WARN/FAIL 并落在保留窗口内（≤7 天且 ≤2×interval，见 §7）；若最近一条是 `NOT_EVALUATED` 等状态则视为无基线、不向前回溯。`baseline` 为空或 0 → `on_empty`；变化比例与 `max_increase_pct`/`max_decrease_pct` 比较。
- `aggregate`：观测值 = 聚合值，`total_rows = COUNT(*)`；区间判定（默认开区间，`inclusive` 改闭）；NULL 聚合 → `on_empty`；`PERCENTILE` 仅 PG/MSSQL；`COUNT` 不是聚合规则的函数（与 `row_count` 重复），保存期和执行前 `ValidateConfig` 都拒绝，因此存量 COUNT 聚合规则会得到 `ERROR(UNSUPPORTED)`。
- `group_check`：`total_rows` = 组数、`failed_rows` = 越界组数；阈值按 COUNT 或 PERCENT；样本返回违规组（分组列 + 组行数）。
- `cross_table`：左右各一个 COUNT/SUM/AVG/MIN/MAX（不同实例/库不支持）；只应用左表 filter；`observed = |left-right|`，判定用 `max(0, |diff| - tolerance)` 与 fail/warn 阈值（`>`）；任一侧 NULL → `on_empty`。
- `cel_expression`：Go 内求值；SQL 只取 `SELECT <引用列> FROM <t> [WHERE filter] LIMIT sample_rows`（默认 1 万、上限 10 万行，列 ≤20，表达式 ≤4 KiB）；表达式只能引用目标列（名字必须是 CEL 标识符）；`sampled=true`；`matches()` 模式必须是常量；引用 `money`/`smallmoney` 列在保存期拒绝（值以货币文本渲染，无法稳定读成数值）；数字文本先按普通小数解析，货币文本仅在无歧义时接受（`$1,234.50`、`($1.23)`），`1,5`、`1 234,50` 这类歧义写法按 `BAD_DATA` 处理。
- `schema_drift`：把 registry 当前列与保存的快照比对，漂移项 = 缺失列 / 类型或可空性不符（`check_types`，未设置时默认 true；可空性漂移项为 `nullable: NOT NULL -> NULL` 或反向）/ 顺序不符（`check_order`，默认 false）/ 多余列（`allow_extra_columns=false` 时）；可空性只在快照记录了该列的可空性时比较（`optional bool`；未设置 = 不判断，因此旧快照不会产生假漂移），编辑器和 API 的填充路径总会写入；`total_rows = len(expected_columns)`、`failed_rows = 漂移项数`、样本是漂移项列表；无 SQL、无只读源。
- `custom_sql`：人写的单条 SELECT/WITH（≤8 KiB，omni 解析校验）；执行 `SELECT COUNT(*) FROM (<sql>) dq`，样本 `SELECT * FROM (<sql>) dq LIMIT n`；无 `total_rows`，因此拒绝 PERCENT。SQL 本身负责 filter，DQ 不重写。
- **PERCENT 允许集（10 类）**：`unique`、`unique_combination`、`accepted_values`、`range`、`regex`、`string_length`、`referential_integrity`、`cross_column`、`group_check`、`cel_expression`；其余（`custom_sql`、`freshness`、`row_count`、`row_count_change`、`null_percent`、`schema_drift`、`aggregate`、`cross_table`）保存期拒绝。`null_percent` 不读取共享 `threshold_metric`（其 config 用 `max_null_count`/`max_null_pct` 自选单位），提交 PERCENT 会被拒绝并指向自己的字段。
- 对象类型：TABLE/EXTERNAL_TABLE 全类型；VIEW/MATERIALIZED_VIEW 只允许表级规则（`row_count`、`row_count_change`、`freshness`、`custom_sql`、`schema_drift`）。
- 执行 SQL 截断 8 KiB；样本值截断 256 字节；样本行数默认 20、上限 100（工作区设置可下可上，硬上限 100）；单次执行结果内存上限 1 MiB。
- 失败样本 = **失败行本身**：按列检查的样本先取目标的**主键列**，再取被检查列（去重），因此同一列上的多个失败行可以区分。主键来自 registry `TableMetadata.indexes` 里 `primary` 索引的 key，按 key 顺序、用 registry 拼写；引擎的打印形式先归一化——PG `pg_get_indexdef` 会给混合大小写/保留字加引号（`"TeamID"`）并可能带 `DESC` 后缀；MySQL/MSSQL 的同步记录裸列名，归一化也接受反引号/方括号形式（防御性）。去引号后与 registry 列名**精确**匹配（PG 可以有只差大小写的两列）；函数表达式与 `COLLATE`/非默认 opclass 等复合 key 跳过。没有主键（外部表、同步未记录、上述跳过）时只取被检查列。`unique`/`unique_combination`/`group_check` 的样本是分组键（+ 组行数），`referential_integrity` 取全部子表列，`custom_sql` 是用户语句的 `SELECT *`，`cel_expression` 只取表达式引用列（仍无行标识，见 [backend-open-items.md](backend-open-items.md)），`schema_drift` 是漂移项列表。SQL NULL 以 `ColumnValue.is_null=true` 存储（`value` 为空），空字符串是 `value=""` 且 `is_null=false`；旧行/旧服务没有该字段，视为**未知**（无法区分 NULL 与空字符串）。样本 SELECT 比被检查列宽，主键值也计入 1 MiB 上限（极端宽表可能少几行并置 `truncated`）；主键列导致查询失败（元数据过期、只读账号没有列级授权）时回退为只含被检查列的样本再试一次，超时除外（重试只会再花一个查询预算）。

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
- 只读通道：`db.ReadOnlyQueryer`；PG 事务内 `SET LOCAL statement_timeout`，MySQL 会话 `MAX_EXECUTION_TIME`（刻意晚于客户端 deadline 1s 再复位：超时一律由客户端 deadline 报错，服务端上限只是兜底）并复位；其余依赖只读账号 + 语句白名单。无 READ_ONLY 数据源 → `ERROR(NO_READ_ONLY_SOURCE)`。
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
| L1 规则草稿 | `SuggestQualityRule`（同步，编辑器单目标）与 `CreateQualitySuggestionJob`（异步，1..20 目标） | `dq.rule_draft.v1` | 目标 schema/DDL/列注释、已有规则摘要、意图、请求列 | 1–5 条草稿 → 校验/编译 → 按 identity 对账 `quality_suggestion(RULE_DRAFT, NATURAL_LANGUAGE)`：新建或刷新 PENDING、跳过已有规则/已决（除非 `force_regenerate`），返回 summary；任务另记 `quality_suggestion_job` 进度与逐目标 summary |
| L2 失败解释 | `ExplainQualityExecution`（server streaming） | `dq.explain.v1` | 执行快照、rule revision 的 config、最近 10 条执行（不含当前）、schema history ≤5、lineage ≤10、open issue | hypotheses+evidence+next_step → `data_quality_llm_cache`（命中则回放） |
| L3 标注与建议 | `SuggestColumnLabels` | `dq.column_label.v1` | 近 7 天画像摘要（含 Top-K 摘要）、schema、lineage、已有标注；每批 ≤20 列、≤3 批 | 先按事实/队列对账过滤列（已标注/已决/已有 PENDING；`force_regenerate` 重析），再 label 建议 + 确定性映射的规则草稿按 identity 入队/刷新；返回 summary（含被跳过的列） |

- L1 有同步与异步两条路径：同步 `SuggestQualityRule` 只服务规则编辑器（需要结果回填表单）；建议中心的多目标起草提交 `CreateQualitySuggestionJob`，由 `qualitysuggestionrunner` 逐目标调用同一个 `dataquality.Suggester`，任务只负责调度、进度持久化与取消，不改变提示词/校验/落库规则。
- 两条路径共享同一对账实现：草稿按 `RuleSignature` 去重；已有规则覆盖的 identity 不入队并关闭残留 PENDING；已决 identity 默认跳过；其余走 `QueueQualitySuggestion` upsert（created/refreshed 计入 summary）。任务提交按 `request_fingerprint` 幂等：同 intent/language/force/目标快照的 PENDING/RUNNING 任务只存在一个，重复提交返回既有任务（`deduplicated=true`），任务目标持久化本目标的 summary。

- 失败诊断（D-47…D-51）：每次调用带 correlation id；失败写入 `QualityLLMDiagnostic`（分类码/provider/model/prompt 版本/max_output_tokens/finish_reason/token 用量/prompt 字符数/provider status/correlation id/debug_captured）并 WARN 日志同字段。输出预算取 provider profile（默认 32768）；provider 返回 400/422（例如 `max_tokens` 超过模型上限）时会先以 4096 重试一次；`finish_reason=length` → `TRUNCATED`，文案提示“缩小意图或减少列后重试”，不自动重试。分类码：`TRUNCATED`、`STREAM_INCOMPLETE`、`PROVIDER_STATUS`、`PROVIDER_REQUEST`、`RESPONSE_TOO_LARGE`、`UNEXPECTED_FINISH_REASON`、`TIMEOUT`、`MALFORMED_RESPONSE`、`PARSE`、`UNKNOWN`。
- 原始请求/响应只在 `RuntimeDebug` 打开时写入 `llm_debug_log`（`kind`、`correlation_id`、`error_code`，7 天保留），并经 `GetQualitySuggestionJobTargetDebug` 按任务目标按需读取；未开启、未实际捕获或已过期时只有结构化诊断（`debug_captured` 表示捕获回调确实执行过）。该读取 RPC 审计，但审计 payload 会把 `requestBody`/`responseBody` 脱敏，正文只受 7 天调试日志保留期约束；提示词/响应只含元数据、DDL、意图与模型输出，**不含样本行**。
- provider：`llm.ResolveConfig(configs, allowlist, settings.LLMProviderProfile)`；未指定时取允许列表首个；无可用 profile → `FailedPrecondition`。
- 结构化输出：`temperature=0` + JSON mode；`llm.ParseJSON[T]`（剥 code fence、`DisallowUnknownFields`、拒绝多文档/空/null、validator 校验）；解析失败不缓存。L1 草稿词表仅 12 类 v1 规则（`custom_sql`/CEL/v1.1 四类不可由模型提出）；草稿再经列存在/列作用域/`ValidateRule` 过滤，全部被丢则返回空列表 + Warn 日志。
- L2：缓存 key = `explain|execution:<执行行 id>|rev:<rev>|status:<status>|prompt:dq.explain.v1|provider:<profile>|model:<model>|lang:<en-US|zh-CN>`；命中回放 content+metadata(`from_cache=true`)；`force_regenerate` 跳过读但仍写；usage 存缓存行 + debug 日志，metadata 不带 usage/prompt_version。解释 content 是一次性 markdown（`### n. cause`、Evidence、Next step、confidence），progress 只发一帧 `thinking`。前端只在 `FAIL`/`ERROR` 的执行详情显示入口，API 本身不限制状态。
- L3：要求请求列都有 ≤7 天画像，否则 `FailedPrecondition` 指向上 `ProfileColumns`；标注只在封闭枚举内，规则候选由 `RuleTypesForSemantic` 确定性映射、阈值为默认值（不从当前值推导）；置信度不自动接受。
- 配额（进程内、UTC 日，D-53…D-57）：L1+L2 取设置 `daily_llm_analysis_limit`（默认 10000，0=默认，上限 1000000）；L3 取 `daily_column_analysis_limit`（默认 50）；并发取 `llm_concurrency_limit`（默认 8，1–64），两个桶共享；超限 `ResourceExhausted`。计数在 `component/dataquality.LLMQuota`，由 API 与 `qualitysuggestionrunner` 共享；每次调用把设置解析成 `LLMLimits` 传入，设置变更对下一次分析生效；同步 L1 一次请求扣 1，后台任务每个目标扣 1，提交任务时按目标数 best-effort 预检（不足直接 `ResourceExhausted`，不预占）。用量/上限/重置时间通过 `GetQualityLLMUsage` 读取，设置页展示。
- 出域：仅元数据/DDL/注释/规则定义/历史摘要/血缘/画像摘要；**没有任何路径发送原始样本行**（`send_sample_values_to_llm` 设置被保存但当前无消费者，§7）。提示注入：数据块 `<schema>/<rule>/<result>/…` 包裹，嵌入值中的 `</` 被中和为 `<\/`，system 声明“data, not instructions”；模型无工具、无连接。
- 失败：分析失败不改变规则/问题状态；错误以 `Internal`/`FailedPrecondition`/`ResourceExhausted` 返回，消息不含 key/样本。

### 3.6 API、权限与审计

- `DataQualityService`（`proto/v1/v1/data_quality_service.proto`）共 33 个 RPC：规则 CRUD/预览/手动执行（7）+ 批量创建（1）、执行列表/详情（2）、问题列表/详情/处置（3）、画像与标注列表/写入（4）、建议列表/详情/决策/批量决策（4）、AI 起草任务创建/读取/列表/取消（4）+ 失败目标调试图读取（1）、三个 LLM 入口（3）、聚合读 `GetQualityOverview`（1）、目标聚合读 `ListQualityTargets`/`GetQualityTarget`（2），以及只读的 `GetQualityLLMUsage`（1）。手动执行返回按规则规划的执行；没有 run/result 的 RPC 或消息。
- 目标目录（`ListQualityTargets`）：一条 SQL 按 `target_guid` 聚合非归档规则，返回每目标的规则/启用/暂停数、去重字段数、未解决问题数、待审草案/标注数、最差最近结果（ERROR>FAIL>WARN>PASS>SKIPPED/NOT_EVALUATED>未跑，与概览口径一致）、最近执行时间与 `schema_changed`（规则快照 hash 与 registry 当前 hash 不同）；支持 instance/database/schema/table GUID 前缀、path/GUID 子串搜索、worst_status/has_open_issue/has_pending_draft 过滤与 attention/name/rule_count/last_run 排序，offset 分页、页上限 1000。`GetQualityTarget` 按 GUID 读一个目标：规则（含最近结果、`orphaned_reason`、`target_schema_changed`）、未解决问题、与目录同形的 summary；目标已不可解析但仍有规则时仍返回规则与快照路径，无规则且不可解析时 `NotFound`。两者只需 `rules.list`（聚合计数与概览同口径），**不审计、不进 principal 限流**；草案正文不在 Get 里（读取草案需 `suggest` 权限，客户端按权限单独 `ListQualitySuggestions`）。
- 分页统一 `parseLimitAndOffset`（offset token），页上限 1000；手动执行选择集 ≤200 条；规则列表的 instance/database/schema/table 是 **GUID 前缀**过滤，`search` 只匹配 title/description；列表可按 `rule_type`、dimension、severity、state、owner/owner_unassigned 过滤。执行列表可按 rule/target_guid/state/outcome/trigger/instance/initiator/start/end 过滤。建议列表同样按 instance/database/schema/table（GUID 前缀）与 status/kind/source/target_guid 过滤；两个批量 RPC 各 ≤200 项，结果与请求同序、逐项独立。起草任务一次 1..20 个目标（去重、逐目标解析 registry 与列），列表默认 20 条、按 status 过滤；失败目标调试图按 `target_index` 读取（越界 `InvalidArgument`，未捕获/已清理返回 `available=false`）。
- 概览（`GetQualityOverview`）：`rule_count` = ENABLED+PAUSED（不含 ARCHIVED），`monitored_table_count` = 至少一条启用规则覆盖的去重表数，打开问题按严重级别计数；`execution_count` 是最近 24 小时**规划**的执行数（含各生命周期状态），而 `last_day_counts` 与 `trend` 只统计已 `COMPLETED` 的执行（pending/running/abandoned 不进趋势），`failing_rule_count` 只算最近 24 小时有 FAIL 执行的规则（`ERROR` 不算）。
- 错误映射：参数/类型/编译失败 → `InvalidArgument`；目标消失 → 创建期 `NotFound`、运行期 `SKIPPED(ORPHANED)`；无只读源/LLM 未开启/无 provider → `FailedPrecondition`；配额/队列满 → `ResourceExhausted`；revision 冲突/建议已决 → `Aborted`；其余 `Internal`。
- 权限（`metaxisdata.dataQuality.*`，15 项）：`rules.{get,list,create,update,delete,run}`、`executions.{get,list}`、`issues.{get,list,update}`、`explain`、`suggest`、`profile.run`、`labels.update`。`workspaceMember` 基线 = rules.get/list、executions.get/list、issues.get/list、explain；其余默认仅 `workspaceAdmin`/自定义角色。设置页复用 `settings.get/update`；标注的**读**复用 `rules.list`（经 `ListColumnLabels`）。
- 审计（proto `audit=true`，每次调用 1 行）：规则创建/更新/删除、批量创建、手动执行、问题处置、建议决策/批量决策、AI 起草任务创建/取消、失败目标的调试图读取、画像、标注更新、三个 LLM RPC；只读列表/详情与定时执行不审计。审计行只含请求元数据，不含 LLM 正文/样本；调试图读取即使带审计，响应中的 `requestBody`/`responseBody` 也会被脱敏（正文只进 7 天调试日志）。
- 限流：所有带审计注解的 DQ RPC 自动进入 principal 预算（`(principal, procedure)` 3000/min、部署 30000/min，见 [rate-limiting.md](rate-limiting.md)）；LLM 另有 §3.5 的配额。

### 3.7 前端与 i18n

- 路由：`/data-quality`（概览）、`/targets`（目标目录）、`/targets/:targetGuid+`（目标工作台，query `metaType`）、`/rules`（跨目标规则列表，从目标目录的“全部规则”进入）、`/rules/new`、`/rules/:ruleId+`（规则详情）、`/rules/:ruleId+/edit`、`/suggestions`、`/issues`、`/issues/:issueId+`、`/executions`、`/executions/:executionId+`；`/settings/data-quality` 挂在 Settings。每条路由带 permission，侧栏按权限隐藏（不置灰）。父路由 `contentWidth: "full"`。
- 导航：侧栏第 6 组“数据质量”（概览/质量目标/执行/问题/建议），SettingsNav 增加“数据质量”。目标目录是创建/编辑的主入口；规则列表保留为跨目标筛选视图。表、视图、物化视图、外部表的元数据详情都有“数据质量”标签页（`MetadataQualitySection`）。规则详情从目标工作台、执行详情的规则名、问题详情的规则名与资产页质量区进入（`rules.get` 在 member 基线内，只读可见）。
- 页面/组件：`pages/data-quality/`（概览、目标目录、目标工作台、规则列表、规则详情、规则编辑器、执行列表/详情、问题列表/详情、建议中心）；`components/data-quality/`（状态/严重级别徽标、趋势图、最近执行状态色块条、规则表单与 filter 编辑器、失败样本、SQL 查看、建议详情抽屉、字段分析面板、AI 解释抽屉、AI 起草工作台 `SuggestionWorkbench`、AI 起草任务状态条 `SuggestionJobsPanel` 与任务历史抽屉 `SuggestionJobHistoryDrawer`、批量创建规则对话框 `BatchCreateRulesDialog`、批量决策对话框 `BatchDecideSuggestionsDialog`、目标工作台的行/草案/助手/内联新建（`TargetCheckRow`/`TargetDraftCard`/`TargetCopilotPanel`/`TargetRuleComposer`）、目标选择器与范围筛选（`TargetPicker`，实例以下每层分页 + 服务端搜索，状态在 `composables/useMetadataChildren`）、资产页质量区）。复用 shadcn-vue 原语，不新增 tabs 原语。
- 概览趋势图（`QualityTrendChart.vue`，几何在 `utils/dataQualityOverview.ts`）：按容器实测宽度 1:1 绘制、高度固定 232px——缩放 viewBox 会把轴文字一起放大（旧实现 720×200 在卡宽 1071px 时把 10px 文字画成 15px）；柱宽封顶 `maxBarWidth=72`，画不出柱的日子（无执行、或仅有跳过/未评估）画基线短桩；纵轴 `tickCount=5` 走 `niceScale` 取整刻度；横轴用 `formatTrendAxisDate` 的 `10/19` 短日期、标签数由 `plotWidth/64` 决定，最后一天显示“今天”；图例兼作窗口合计（跳过/未评估有值时才出现，故合计等于页脚窗口总量）。悬停整日列显示当日全部状态计数（含跳过/未评估）（悬停面必须叠在柱体之上：透明 rect 放在柱体下面收不到指针），另有 `sr-only` 表格给出同数据。填充色用 `--chart-pass/-warn/-fail/-error` 图形变体（见 [frontend-ui.md](frontend-ui.md) 的令牌表）
- 规则列表筛选：标题/描述文本、**目标范围**（实例 → 数据库 → Schema → 表/视图；复用 `TargetPicker` 的 `mode="scope"`，在任一层可停，点表/视图行直接筛到该表）、**负责人**（复用 `UserSelect`；owner 可空，所以除成员外还有“未指定”一项；成员目录一次加载后同时供选择器与列表的负责人列复用，选择器支持懒加载与预载）、**规则类型**（18 类，复用 `ListQualityRules` 的 `rule_type`；“全部类型”发 UNSPECIFIED 即不筛选）、维度、严重级别、状态；改动后点“应用”才查询。列表还显示负责人列（邮箱，未指定时显示“未指定”）。`TargetScopeFilter` 与负责人控件只持有待应用值，请求由 `ListQualityRules` 的 instance/database/schema/table（GUID 前缀）以及 owner（`users/{user}`；空表示不限）与 owner_unassigned（只取无负责人的规则，与 owner 互斥）字段承受。
- 规则编辑器的规则类型选择：按 4 组（数量与时效 / 取值与关系 / 聚合与对账 / 结构与自定义）分组，选中项下方显示一句说明；目标为 VIEW/MATERIALIZED_VIEW 时只列出后端 `RuleTypeForTarget` 允许的 5 类（行数、行数变化、时效性、自定义 SQL、结构漂移）并显示提示；切换类型会重置 `threshold_metric`（新类型不读该字段时归零为 COUNT）；`on_empty` 仅对 `row_count`/`custom_sql`/`schema_drift` 隐藏（对它们永远无效）。规则类型名称、说明与分组都在 `dataQuality.ruleType*` 文案中维护。结构漂移的 `check_types` 文案说明类型与可空性都参与比较；若存量规则的类型不在当前目标允许集内，编辑器仍显示该类型（带视图提示），不会静默改写。
- 建议中心（`/suggestions`）：顶部“AI 起草”打开 `SuggestionWorkbench`。页模式：目标选择器一次多选多张表（`TargetPicker` 的 `multiple`，仅工作台启用、默认关闭、不影响 scope 模式）、单一目标给列清单、必填意图（≤2000 字符，最多 20 个目标）；提交即创建一个后台起草任务并显示“可关闭页面”的确认，之后由 `SuggestionJobsPanel` 展示进度。工作台有“强制重新生成”复选框（默认关闭；开启后同一批已决/已标注目标会重析，但仍只刷新不重复），任务返回 `deduplicated=true` 时显示“已有相同任务在进行中，未重复创建”。编辑器模式：目标固定、同步生成，结果可“填入编辑器”或忽略。任务区是一个**固定高度状态条**（`SuggestionJobsPanel`：标题 + “N 个进行中 · 最近 M 个已结束” + 当前最相关任务的一行状态/意图/进度/结果，右侧“任务历史”按钮），任务积累多少都不会再撑高页面；完整列表在右侧抽屉 `SuggestionJobHistoryDrawer`（服务端分页，20/50/100 条每页、按状态筛选、单个任务展开逐目标状态/错误/summary，活跃任务支持两步确认取消；失败目标展示分类码+可执行建议+provider/model/停止原因/token/字符数/correlation id（可复制），debug 捕获时提供“查看原始请求/响应”对话框）。仅当存在 PENDING/RUNNING 任务时状态条每 2.5s 轮询（活跃任务的取消也留在状态条上，不必先进抽屉），抽屉只在打开且当前页还有活跃任务时轮询，任务进入终态后刷新队列；COMPLETED 任务会回到队列第一页并选中当前页可见的新建议（未全部可见时给出提示）并 toast。队列按目标表分组（组头显示实例标题/路径、类型、数量、组复选框与折叠），行内可“创建规则”/“接受”/查看详情，已决行只读；多选后批量条按 kind 提供“创建 N 条规则/忽略 N 条草稿”与“接受/拒绝 N 条标注”，混合选择各自只作用于对应 kind；筛选含状态（默认 PENDING）/kind/source/目标范围并显示可移除 chip；三种空状态（无待审、筛选无结果、从未生成）。规则编辑器的“用 AI 起草”复用同一工作台（目标固定），保存时用 `createQualityRule(rule, suggestionName)` 一次请求创建并接受建议；若在编辑器中改了目标，页面先解除建议预填（建议保持 PENDING），避免把规则建到旧目标上而服务端拒绝保存；`?suggestion=` 预填改用 `GetQualitySuggestion`。
- 建议的生成结果展示：资产页“建议标注”是分体按钮——主按钮默认跳过已处理列，下拉“重新生成建议”置 `force_regenerate=true`；两种结果都用 `useSuggestionSummary` 生成本地化一句话（新建/刷新/已跳过/已失效；全部跳过时说明原因：已标注/已生成/已决/已失效），不再显示“已排队 N 条”。建议中心状态筛选含 `SUPERSEDED`（“已失效”，徽标与接受/拒绝不同），被刷新的 PENDING 行在状态徽标下显示“已刷新 · 相对时间”（来自 `refresh_time`，不新增列）；工作台与任务历史抽屉逐目标展示 summary；`SuggestionJobsPanel` 的完成 toast 改为“已可审阅”而不是“已排队”。`skipped_columns` 逐列展示（最多 6 个，多余显示“等 N 列”）。资产页的待审建议卡（D-68）把当前目标的 PENDING 建议（标注建议与规则草稿）直接列在数据质量标签页：最多 5 条、标注在前，行内显示字段名/标题、语义标注或规则类型+严重级别、置信度、理由与时间；标注可“接受/拒绝”（接受同时刷新字段标注），规则草稿可在编辑器中打开，其余由卡头链接建议中心。生成 pass 后（含部分批次失败）立即重新拉取，为空则整卡不渲染；读取失败或决策失败都就地提示，决策失败还会重读队列以清掉陈旧行。
- 执行列表：一行一条执行（state/outcome 徽标、执行 id、规则、目标、观测值、失败/总行数、trigger、耗时、开始时间）；筛选 state/outcome/trigger，跨页链接带来的 `?rule=`/`?target=` 显示为可移除 chip；offset 分页；点击行进入执行详情。
- 执行详情：状态/六态、规则链接、目标路径、观测值/阈值、行数（含 sampled/truncated 提示）、失败样本（每个样本编号；`NULL` 与空字符串分别标记，不再把空值渲染成空白；升级前记录的样本没有 NULL 标记，显示中性的 `""` 并在悬浮提示说明无法区分）、执行 SQL（可复制）、错误码/消息、耗时、trigger/initiator/instance/revision/engine version、开始/结束时间；`PENDING`/`RUNNING` 时轮询到终态；`FAIL`/`ERROR` 显示 AI 解释入口。
- 规则详情的信息层级：顶部“最近结果”大状态（图标+状态词+相对时间+实际值/阈值/失败行数）→ 最近 20 次执行状态色块条（旧→新，按状态着色，点击进执行详情）→ 状态计数与通过率 → 最近执行表 → 规则定义（目标/字段/类型/维度/严重级别/负责人/调度/版本/时间/标签；负责人显示成员目录里的邮箱，未指定时显示“未指定”）。运行与编辑分别按 `rules.run`、`rules.update` 显示；“运行”后单条执行跳执行详情，多条进入按规则筛选的执行列表。
- API/工具：`api/data-quality.ts` + `api/client.ts` 的 `dataQualityClient`（同步 suggest 返回完整 response 以便读取 summary）；纯函数在 `src/utils/dataQuality*.ts`（状态/严重级别映射、执行状态辅助、表单校验、LLM 提示、概览、设置、建议、`suggestionSummaryView`/`suggestionSummaryReason`）；`lib/notificationText.ts` 渲染通知文案（问题/执行失败链到执行详情，风暴摘要回退到执行列表）；`composables/useUserDirectory.ts` 是规则页与详情页共用的成员目录（邮箱解析、失败回退原始 `users/{id}`，仅持 `users.list` 时请求）；无跨页 Pinia 模块。
- i18n：`dataQuality.*`、`dataQualitySettings.*`、`menu.dataQuality*`、`notifications.dataQuality*`；执行状态/触发器等动态 key 前缀已在 `DYNAMIC_PREFIXES`；术语进 `frontend/glossary.json` 的 **Data Quality** 分类。
- 设置页：Settings → Data Quality 的 Limits 卡显示/编辑日分析上限与并发，并有一张只读“今日 AI 用量”卡（used/limit、in-flight/并发、重置时间）；Settings → LLM providers 的 profile 表单有 Max output tokens（0/空=默认 32768，256–131072）。
- 布局门禁：新路由登记 `frontend/scripts/audit-ui-layout.mjs`（含各表 `tableTopMaxY` 预算；`/data-quality/executions` 为 200，`/data-quality/suggestions` 因固定高度的任务状态条为 320）；`ERROR` 与 `FAIL` 文案/徽标必须不同；编辑器保存前必须干跑（`PreviewQualityRule`）。

### 3.8 画像、语义标注与建议队列

- 画像（确定性、无 LLM）：每表一条聚合查询（行数/null/distinct/min/max/长度）+ 一条样本查询（`LIMIT 1000`）；画像打开目标库前先领实例连接预算（[instance-connections.md](instance-connections.md)）；Go 内算 Top-K（仅 distinct ≤20 的列）、样式、长度分布；值截断 64 字节、最多 20 个；写 `column_profile`（列级 upsert，`sample_method="limit"`、`engine_version="dq.v1"`）；单次 ≤设置列数、120s、4 MiB 结果。
- 聚合事实由类型能力表推导（D-34）：`countDistinct` → 精确 distinct，`extrema` → min/max，`length` → 长度上界，`text` → 样式；没有 `countDistinct` 时 distinct 来自样本并标 `distinct_estimated=true`（样本覆盖全表时为精确值），前端与 L3 提示词显示 `≈`/`~`。完全没有能力的列只取非空计数：默认 `COUNT(col)`，MSSQL text/ntext/image 用 `COUNT(CASE WHEN col IS NOT NULL …)`；`binary` 类型的极值/Top 值一律以 `0x…` 展示。推导入口是 `newProfileColumn`，画像与规则校验共用同一张表。
- 标注：`column_semantic_label` 唯一键 `(table_guid, table_type, column_name)`；封闭枚举 `QualitySemanticType`；`source=MANUAL|LLM`、`status`、`confidence`、`evidence`；人工标注可覆盖（Upsert），LLM 建议不自动覆盖。
- 规则候选映射（代码固定）：`ID/BUSINESS_KEY → unique+null_percent`；`EMAIL/PHONE/URL/UUID/IP → regex+null_percent`；`COUNTRY_CODE/CURRENCY/ENUM → accepted_values`；`AMOUNT/QUANTITY → range+null_percent`；`PERCENTAGE → range(0-100)`；`TIMESTAMP/DATE → freshness+range`；`FREE_TEXT → string_length+null_percent`；`UNKNOWN → 不生成规则`。阈值为默认值，不从当前值推导。
- 建议：`quality_suggestion(kind=RULE_DRAFT|LABEL, source=NATURAL_LANGUAGE|PROFILE, payload, signature, status, provider/model/prompt_version, refreshed_at)`；`signature` 是提案身份，部分唯一索引只允许每个 `(target, signature)` 一条 PENDING。写入走 upsert（`QueueQualitySuggestion`）：同一提案再次生成时刷新 payload/provider/`refreshed_at` 并返回 created=false；事实出现时用 `SupersedePendingQualitySuggestions` 把 PENDING 标 `SUPERSEDED`。决策只允许 PENDING→ACCEPTED/REJECTED（并发重复决策返回 `Conflict` → 客户端 `Aborted`），接受 LABEL 即 upsert 标注；从建议创建规则时先以该决策作原子 claim、再写规则，写失败回滚 claim（D-66）。生成 RPC/任务目标返回 `QualitySuggestionQueueSummary`；`force_regenerate` 只重提已决/已标注项，不绕过唯一性。
- 建议中心（D-36…D-46，D-58…D-63）：AI 起草入口在建议中心与规则编辑器共用 `SuggestionWorkbench`；建议中心的多目标起草提交一个后台 `QualitySuggestionJob`（生命周期与不变量见 I-22…I-25、I-34/I-35），API 只做目标/列的 fail-fast 校验、provider 与配额预检后落 PENDING（相同请求指纹的 PENDING/RUNNING 任务直接返回，`deduplicated=true`）；`qualitysuggestionrunner` 异步逐目标调用 `dataquality.Suggester`（与同步 `SuggestQualityRule` 同一实现），每个目标后持久化进度与本目标 summary，建议按 identity 对账入 `quality_suggestion` 并由任务目标的 `suggestion_resource_ids` 引用；批量创建用 `BatchCreateQualityRules` 逐项执行正常校验链，命名 `suggestion` 的项目标取自建议（请求 target 必须为空或一致），规则落库后接受建议（决策失败只记日志），失败项返回错误且建议保持 PENDING；批量决策用 `BatchUpdateQualitySuggestions`，与单条共享标注写入路径；列表按目标表分组（GUID 分段；实例标题来自实例缓存），并支持 GUID 前缀筛选；建议与任务都无“生成批次”资源（任务本身是唯一批次）。批量结果逐项回显 suggestion/name，客户端不依赖下标。

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
| `daily_column_analysis_limit` | 50 | ≤0→50；上限 1000000 | L3 日分析次数 |
| `daily_llm_analysis_limit` | 10000 | ≤0→10000；上限 1000000 | L1+L2 日分析次数 |
| `llm_concurrency_limit` | 8 | ≤0→8；夹到 1–64 | 进程内并发的 DQ LLM 分析数（两个桶共享） |
| `max_columns_per_analysis` | 50 | ≤0→50；写入校验上限 200 | 单次画像/标注列数 |
| `send_sample_values_to_llm` | false | 按值 | **当前无消费者**（§7） |

保留期不在设置里（常量）：执行随内部批次 90d、关闭问题 90d、画像/LLM 缓存/LLM 调试日志 7d、已决建议 90d、已结束的起草任务 90d —— 见 §7，已由 maintenance 接线。

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
| `quality_execution.sample` | `QualityFailureSamples`（`FailureSample{ColumnValue{column,value,is_null}}`，值已截断；`is_null` 是 optional bool，区分 SQL NULL、空字符串与旧行/旧服务的未知） |
| `column_profile.profile` | `ColumnProfile` |
| `column_semantic_label.evidence` | `SemanticLabelEvidence` |
| `quality_suggestion.payload` | `QualitySuggestionPayload`（`rule`/`label` oneof） |
| `quality_suggestion_job.targets` | `QualitySuggestionJobTargets`（逐目标 guid/type/path/columns、state、建议 resource id、错误、起止时间、失败诊断 `QualityLLMDiagnostic`、生成 summary `QualitySuggestionQueueSummary`） |
| `data_quality_llm_cache.result_json` | `DataQualityLLMResult`（`rule_draft`/`explanation` + provider/model/prompt/token 用量） |

JSONB 一律 `protojson.Marshal`，反序列化用 `common.ProtojsonUnmarshaler`；列注释写消息名。

`llm_debug_log` 不是 JSONB：`kind`、`correlation_id`、`error_code` 是普通文本列（迁移 `0027`），正文为原始请求/响应文本，只在 `RuntimeDebug` 打开时写入、保留 7 天。

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
| I-20 | 批量建议/规则 RPC 非原子：每项独立校验与落库，结果与请求同序，单项失败不中止其余；从建议创建规则是“先规则后决策”，决策失败不回滚规则 | `data_quality_batch.go` + `TestDataQualitySuggestionCenterRealServerIntegration` |
| I-21 | 从建议创建规则时目标取自建议的 `(target_guid, target_type)`；请求自带的 target 必须为空或与建议一致，不一致返回 `InvalidArgument`（不静默改写）；建议必须是 PENDING `RULE_DRAFT` | `validateRuleTargetAgainstSuggestion`/`pendingRuleDraftSuggestion` 单测 + 集成场景 |
| I-22 | AI 起草任务由单进程 runner 原子 claim，同一任务不会被并发执行；**启动时所有 RUNNING 任务标 FAILED**（RUNNING 目标 FAILED、PENDING 目标 SKIPPED），已入队建议保留（不续跑） | `ClaimPendingQualitySuggestionJob`/`AbandonStaleQualitySuggestionJobs` 单测 + runner 场景 |
| I-23 | 任务与同步 L1 共用同一 `LLMQuota`：每个请求/目标至多扣 1 次、全局并发 ≤2；提交时只做 best-effort 预检（不预占），中途耗尽 → 当前目标 FAILED、其余 SKIPPED、任务 FAILED | quota/suggester 单测 + runner quota-stop 场景 |
| I-24 | 任务状态机 PENDING→RUNNING→COMPLETED/FAILED/CANCELED；目标状态只在 RUNNING 任务内写 DONE/FAILED/SKIPPED；COMPLETED = 所有目标都被尝试；CANCELED 只在目标边界产生并保留已入队建议；终态取消幂等 | store 生命周期守卫 + runner 测试 |
| I-25 | 任务目标快照（guid/type/path/columns）在提交时写入；执行期重新解析目标，孤儿/列消失 → 该目标 FAILED，其余目标继续 | API 校验测试 + 集成/runner 场景 |
| I-26 | 每个失败的 LLM 目标都带分类码与 correlation id；任务目标的诊断不含 prompt/response 正文；服务端 WARN 日志含同一 correlation id 与 provider/stop reason/max tokens/prompt chars | `suggester_diagnostic_test.go` + runner/集成场景 |
| I-27 | 原始请求/响应只在 `RuntimeDebug` 打开时写入 `llm_debug_log`（7 天保留）；读取走 `GetQualitySuggestionJobTargetDebug`（`suggest` 权限、审计），未捕获/已清理一律 `available=false`；提示词/响应不含样本行 | store/debug 单测 + 集成回读场景 |
| I-28 | LLM 输出预算来自 provider profile（0=32768，256–131072），调用方可请求更小；所有 DQ 分析（含 ExplainSQL）请求的 `max_tokens` 与诊断记录的值一致 | limits/代理请求/诊断测试 |
| I-29 | 配额与并发上限来自每次调用解析的设置（默认 10000/8，上限 1000000/64；L3 列上限 1000000）；等待者取消不消耗额度或槽；设置变更对下一次分析生效（后台任务逐目标重读），进程重启计数归零 | quota 单测 + runner 逐目标 limits 场景 + 集成用量场景 |
| I-30 | 同一 `(target_guid, target_type, signature)` 至多一条 PENDING 建议；并发写入由部分唯一索引收敛为一条，后到者刷新而非新增 | 迁移 `idx_quality_suggestion_pending_signature` + store upsert 语句断言 + `suggestion_test.go` |
| I-31 | 生成 pass 幂等：同一状态重复执行不新建重复提案；规则草稿路径会刷新 payload/provider/refreshed_at，L3 对已有 PENDING 的列默认跳过（不刷新、不调模型）；已存在的事实（标注、规则）不再入队 | `Suggester.Draft` 对账逻辑 + `planQualityLabelPass` 单测（skip/force/count/close）+ 集成场景 |
| I-32 | 事实优先：写人工/接受标注或创建规则后，同 identity 的 PENDING 建议被标 SUPERSEDED（写 reason），不留在队列里；SUPERSEDED 没有 decided_by，不是接受/拒绝 | `SupersedePendingQualitySuggestions` 调用点（`UpdateColumnLabel`、`CreateQualityRule`、两个生成 pass）+ 单测 |
| I-33 | 已决建议默认不再提议：RULE_DRAFT 的 ACCEPTED/REJECTED、LABEL 的 REJECTED 抑制后续生成；`force_regenerate` 可重提；LABEL 的 ACCEPTED 由“标注事实存在”抑制，标注清除后允许重提。抑制记忆依赖已决行，保留期（90d）到期后同一提案可能再次出现 | `SuggestionDecisionSuppresses*` 单测 + 生成路径 + D-16 保留期 |
| I-34 | 相同请求指纹的 PENDING/RUNNING 起草任务至多一个；重复提交返回既有任务且 `deduplicated=true`，不会二次扣额度或重复建建议 | 迁移 `idx_quality_suggestion_job_active_fingerprint` + fingerprint 单测 + 集成场景 |
| I-35 | 每个任务目标持久化本目标的生成 summary（store proto `QualitySuggestionJobTarget.summary`），与同步入口同形；任务历史抽屉可解释每个目标新建/刷新/跳过的数量 | runner 单测（force 透传 + summary 持久化） |
| I-36 | 同一 prompt（feature/prompt 版本/provider/model/输出预算/system+user 全文）的在途分析至多执行一次，所有调用方共享该结果；leader 取消不毒化仍存活的 follower（follower 重试）；leader panic 时向 follower 发布错误、清理在途条目并重新抛出 panic（runner 的 panic 隔离仍生效），不允许留下无人关闭的等待通道 | `analysis_group_test.go`（含 `-race` 与 panic 恢复场景） |
| I-37 | 从建议创建规则时，PENDING→ACCEPTED 的 claim 成功是写规则的前置条件；规则写失败必须把 claim 回滚为 PENDING（best-effort，失败只记日志）。回滚使用 detached 短超时上下文，客户端取消导致的写失败也能回滚 | `reopenQualitySuggestionQuery` 约束 + 集成并发创建场景（恰一成功、恰一 Aborted、规则数=1） |
| I-38 | 生成结果的 `skipped_columns` 在 UI 逐列展示（上限 6 个 + 省略计数），summary 计数与原因仍保留 | `useSuggestionSummary` 单测 + i18n |

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
| DQ LLM 日额度/并发触顶 | LLM 分析返回 `ResourceExhausted`；额度不足时等待并发槽前就快速失败，取消的等待不消耗额度/槽；上限可在 Settings → Data Quality 调整（默认 10000/8） |
| 实例连接预算被占满 | 定时批次等待 ≤2m 后不建批次、规则保持 due、下个 tick 重试；手动批次等待 ≤5m 后其执行与批次 `ABANDONED`；不写 `ERROR(QUOTA)`、不通知（[instance-connections.md](instance-connections.md)） |
| 手动队列满 | 该批次及其执行标 `ABANDONED`（无 outcome）并返回 `ResourceExhausted` |
| LLM 未开启/无 provider | `FailedPrecondition`；规则引擎完全可用 |
| LLM 解析失败/超时/配额耗尽 | 请求失败（Internal/FailedPrecondition/ResourceExhausted）；不缓存、不改规则/问题状态；L3 已入队的建议保持 PENDING |
| 模型在输出上限停下（`finish_reason=length`） | 目标 FAILED，诊断码 `TRUNCATED`，文案提示缩小意图/减少列后重试；输出预算取 provider profile（默认 32768）；本次草稿丢弃，不自动重试 |
| provider 返回非 2xx | 目标 FAILED，诊断码 `PROVIDER_STATUS` + HTTP status；provider 响应体只写服务端日志，不进错误/诊断；按 correlation id 查日志 |
| provider/model 的输出上限低于请求值 | 首次 400/422（例如 `max_tokens` 超过模型上限）会以 `max_tokens=4096` 自动重试一次；仍被拒绝则目标 FAILED，诊断码 `PROVIDER_STATUS`，按 profile 调低预算或换支持该输出上限的模型（D-52） |
| 流中断/超时/响应过大/异常 finish_reason | 目标 FAILED，诊断码 `STREAM_INCOMPLETE`/`TIMEOUT`/`RESPONSE_TOO_LARGE`/`UNEXPECTED_FINISH_REASON`；重试由用户发起 |
| 草稿 JSON 解析失败 | 目标 FAILED，诊断码 `PARSE`（保留 token 用量/停止原因）；不落建议 |
| RuntimeDebug 未开或调试日志已过期 | 结构化诊断仍返回；`GetQualitySuggestionJobTargetDebug` 返回 `available=false`，UI 说明正文未捕获/已清理 |
| AI 起草任务提交时 LLM 未开启/无 provider | `FailedPrecondition`，不落任务 |
| AI 起草任务提交时配额不足（目标数 > 当日剩余） | `ResourceExhausted`，不落任务；预检 best-effort，不预占，两个并发提交可能轻微超发 |
| 任务执行中 provider 不可用/解析失败 | 该目标 FAILED（error 截断 1 KiB），继续下一个目标；全部尝试后任务 COMPLETED |
| 任务执行中配额耗尽 | 当前目标 FAILED、其余 SKIPPED、任务 FAILED；已入队建议保留 |
| 任务执行中服务重启 | 启动时把所有 RUNNING 任务标 FAILED、RUNNING 目标 FAILED、PENDING 目标 SKIPPED；不续跑；已入队建议保留 |
| 任务执行中单任务 panic | runner 捕获并尽力把该任务标 FAILED（detached 短超时），继续处理后续任务，不终止 runner 循环 |
| 取消 PENDING / RUNNING 任务 | PENDING → 立即 CANCELED（目标全 SKIPPED）；RUNNING → 置 `cancel_requested`，当前目标结束后停止（其余 SKIPPED，任务 CANCELED）；已入队建议保留；终态取消幂等返回原任务 |
| 任务目标在提交后被删除/列被删 | 该目标运行期 FAILED（`ResolveTarget`/列校验失败），其余目标继续 |
| 画像失败 | 请求失败并返回可渲染错误；已写入的画像行保留，重试会覆盖 |
| 建议重复决策 | `Conflict` → `Aborted` |
| 同一建议并发创建规则 | 只有 claim（PENDING→ACCEPTED）成功的一路会写规则，另一路 `Aborted`；规则写失败则 claim 回滚为 PENDING，可重试。不会再建出两条规则 |
| 相同 prompt 的并发生成 | 进程内分析合并为一次模型调用（结果共享）；leader 取消而调用方仍存活时由后者重试；队列结果仍由 identity upsert 收敛 |
| 连续点击“建议标注”/“AI 起草” | 不会追加重复建议：同 identity 的 PENDING 被刷新（行 id 不变，`refreshed_at` 更新），已标注列/已有规则覆盖的草稿被跳过或标 SUPERSEDED；summary 报告 created/refreshed/skipped。全部列已处理时不调模型、不扣配额 |
| 同一起草请求重复提交 | 若已有同指纹的 PENDING/RUNNING 任务则返回该任务（`deduplicated=true`），不创建第二个任务、不二次扣额度；任务终态后允许再次提交 |
| 待审建议与事实冲突（他人已写标注/建规则） | 写标注/建规则时把同 identity 的 PENDING 标 SUPERSEDED（reason 入 note）；生成 pass 也会兜底关闭残留 |
| 已拒绝的建议再次生成 | 默认被抑制（不回到队列）；需要重试时用 `force_regenerate`（重新分析并刷新/新建 PENDING，仍不重复） |
| 批量创建/决策单项失败 | 该项 result 带 error（已决/不存在/目标消失/config 非法），其余项继续；创建失败的建议保持 PENDING，创建成功的建议被接受；批量调用本身仍返回成功 |
| 从建议创建规则时请求 target 与建议不一致 | `InvalidArgument`，不创建规则、建议保持 PENDING；编辑器在改目标时必须先解除建议预填 |
| 从建议创建规则时决策写失败（并发已决） | 规则已落库不回滚，只记 warning；建议保持他人的决策，可在队列中看到最终状态。同一 PENDING 建议被并发创建或决策失败后重试时，仍可能建出第二条规则（§7 #12） |
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
| 7 | 保留期清理 | 已实现：maintenance 清理内部批次（级联执行）/resolved issue/column_profile/LLM cache/decided suggestion/已结束的起草任务（§8 `backend/runner/maintenance`） |
| 8 | L2 的 `ExplainSQLMetadata` 无 usage/prompt_version | usage 只在缓存行与 debug 日志；未改 proto（有意） |
| 9 | 多副本 | 不支持（单副本假设，见 [security-posture.md](../../docs/security-posture.md)）；两个副本会重复执行、LLM 配额各自计数 |
| 10 | 分布漂移/ML 异常、分组内多种检查（每组 null/aggregate）、RuleSet | 非目标（§非目标）；RuleSet 在 v1.1 之后单独评审 |
| 11 | 连接等待的可观测性 | 批次的准入/延后/放弃与在途执行数已有指标（见 [metrics.md](metrics.md)）；连接等待时长直方图仍是 P1，尚未实现 |
| 12 | 从建议创建规则的自动接受 | **已修复（D-66）**：改为“先 claim 决策、再写规则”，并发请求只有一路能通过 PENDING→ACCEPTED；写规则失败回滚 claim 为 PENDING。残留风险仅是 claim 成功且进程在写规则前崩溃：建议显示为已接受但无规则，这是接受草稿的合法状态，可在编辑器中手动创建 |
| 13 | AI 起草任务的通知与历史 | 完成不发通知；建议中心任务状态条只在页面打开时轮询（2.5s），任务历史抽屉只在打开且当前页含活跃任务时轮询，关闭页面后需回来查看；抽屉有服务端分页（20/50/100 条每页）与状态筛选，但没有独立任务详情路由/深链，也不能跨页搜索意图 |
| 14 | AI 起草任务的执行语义 | 单副本、单 runner 串行：不并行执行多个任务、不续跑、不取消进行中的 provider 调用；取消/配额只会在目标边界生效；提交时配额预检不预占，可能轻微超发；RUNNING 任务取消后、到目标边界前仍显示 RUNNING（cancel_requested 不在资源上暴露）；单任务 panic 已被捕获并标 FAILED，不终止 runner |
| 15 | 截断调用的 token 用量 | 流在 `finish_reason=length` 处报错时 agent loop 不再读取后续 usage 块，因此该诊断只有 max_tokens/prompt_chars/correlation/finish_reason，没有 prompt/completion tokens；provider/model/stop reason 仍足以复现 |
| 16 | DQ 调试行的 `error_code` | 分类后由 `SetLLMDebugLogErrorCode` 以 correlation id best-effort 回写；调试行是异步写入的，若 UPDATE 先于 INSERT 到达则该行 `error_code` 留空（无重试）。分类码始终以任务目标诊断为准，检索 RPC 返回它 |
| 17 | LLM 失败的自动重试与配置 | 业务失败不做自动重试；输出预算按 profile 可配置（默认 32768），provider 的 400/422（例如 max_tokens 超限）会以 4096 自动重试一次，仍拒绝则得到 `PROVIDER_STATUS`；日额度/并发可配置但计数是进程内、重启归零（单副本），任务提交的额度预检不预占；原始正文只在 RuntimeDebug 下捕获（7 天）、仅 `suggest` 权限可读且审计正文已脱敏；没有通用调试日志浏览器 |
| 18 | 迁移前的建议行 | 迁移 `0028` 给历史行回填 `signature = 'legacy:' \|\| resource_id`（唯一、不参与去重）并对历史 active 任务回填 `request_fingerprint`，因此升级后旧 PENDING 建议仍可见；功能未上线，不做 re-key。旧行不匹配任何新 identity，**不会被新的生成 pass 自动关闭或合并**，只能由审阅者显式接受/拒绝（过期后随保留期清理） |
| 19 | `target_schema_changed` 的引擎覆盖 | 读取路径按 `meta_registry_resource_schema.schema_hash` 比较；只有实现 `db.ObjectDefinitionReader` 的引擎（目前 StarRocks/Doris）写入 DDL 行，MySQL/MariaDB/TiDB/OceanBase/PG/MSSQL 上该 map 为空、`target_schema_changed` 恒为 false（见 [schemasync.md](schemasync.md)）。结构变更在这些引擎上由 `schema_drift` 规则发现；端到端测试断言 drift 规则而不是该布尔值 |
| 20 | `row_count_change` 的默认空行为 | 无基线时按 `on_empty` 判定（默认 `NOT_EVALUATED`），而基线只读该规则**最近一条** COMPLETED 执行（`limit 1`），若它不是 PASS/WARN/FAIL（例如 `NOT_EVALUATED`）就视为无基线，不会继续往前找；默认配置下首次执行即 `NOT_EVALUATED`，之后每次都读到这条 `NOT_EVALUATED`，永远建立不了基线。需要显式设 `on_empty=PASS/FAIL` 才能启动（端到端旅程即如此配置） |

## 8. Where things live

| 面 | 位置 |
| --- | --- |
| 领域/编译/判定 | `backend/component/dataquality/`：`model.go`、`types.go`、`capabilities.go`、`target.go`、`validate.go`、`compile.go`、`compile_rules.go`、`compile_util.go`、`compile_samples.go`、`dialect.go`、`support.go`、`evaluate.go`、`execution.go`、`executor.go`、`errors.go`、`filter.go`、`issue.go`、`settings.go`、`profile*.go`、`cel.go`、`label.go`、`prompt.go`；L1 编排与配额 `suggester.go`、`quota.go`、`settings.go`（同步 RPC 与后台 runner 共用，`suggester.go` 产出失败诊断；quota 的 limits 由调用方按设置传入）；建议 identity/去重助手 `suggestion.go`（`LabelSuggestionSignature`、`RuleSignature`、`RuleConfigColumns`、`RuleSignatures`、决策抑制谓词）；进程内分析合并 `analysis_group.go` |
| 只读查询能力 | `backend/plugin/db/query.go`（`ReadOnlyQueryer`/`QueryOptions`/`QueryResult`、扫描上限）；`plugin/db/{mysql,pg,mssql,starrocks}/query.go`；`backend/component/dbfactory/readonly.go`（`ErrNoReadOnlyDataSource`） |
| runner | `backend/runner/qualityrunner/qualityrunner.go`（调度/规划执行/执行/问题迁移/通知）；`backend/runner/qualitysuggestionrunner/`（后台 AI 起草任务：5s tick、启动清扫、逐目标进度、协作取消）；`backend/server/server.go` 注册第 6 个 runner；保留期清理在 `backend/runner/maintenance/maintenance.go` |
| 连接准入 | `backend/component/state/connection.go`（governor/优先级/等待队列）；`qualityrunner` 的 `admitBatch`；契约与各调用方预算见 [instance-connections.md](instance-connections.md) |
| API | `backend/api/v1/data_quality_service.go`（规则 CRUD/手动执行/执行查询/issue/profile/preview/overview）、`data_quality_batch.go`（批量创建规则/批量决策建议）、`data_quality_suggestion_job.go`（AI 起草任务创建/读取/列表/取消/失败目标调试图读取）、`data_quality_usage.go`（LLM 用量读取）、`data_quality_convert.go`、`data_quality_llm_analysis.go`（L1 同步包装/L2）、`data_quality_label_analysis.go`（L3）、`data_quality_overview.go`、`data_quality_cross_table.go`、`data_quality_target.go`（目标目录/工作台聚合读 `ListQualityTargets`/`GetQualityTarget`） |
| store | `backend/store/quality_{rule,execution,run,issue,profile,label,suggestion,suggestion_job,llm_cache,overview,target}.go`（10 张表 + 目标聚合读；`quality_target.go` 的 `ListQualityTargets` 一条 SQL 聚合，`CountPendingQualitySuggestions` 只数草案；`quality_run` 为内部批次；建议列表的 GUID 前缀过滤复用 `metaGUIDPrefix`/`appendGUIDSubtreeCondition`；起草任务用 `FOR UPDATE SKIP LOCKED` 原子 claim；建议 `QueueQualitySuggestion` upsert / `SupersedePendingQualitySuggestions` / `QualitySuggestionSignatureStates` / `ReopenQualitySuggestion`；任务 `QueueQualitySuggestionJob` 按指纹幂等 + `GetActive`）；`backend/store/explain_sql.go`（`llm_debug_log` 写入/按 correlation id 读取）；`backend/component/llm/limits.go`（输出预算常量/校验） |
| proto/迁移 | `proto/store/store/data_quality.proto`、`proto/v1/v1/data_quality_service.proto`、notification/setting 扩展；`backend/migrator/migration/0.1/0024##data_quality.sql` + `0025##quality_execution.sql`（`quality_rule_result` 改名 `quality_execution` 并补执行列）+ `0026##quality_suggestion_job.sql`（后台起草任务表）+ `0027##llm_debug_log_context.sql`（调试日志 kind/correlation/error_code 列）+ `0028##quality_suggestion_identity.sql`（`signature`/`refreshed_at`、PENDING 唯一索引、任务 `request_fingerprint`/`force_regenerate`）+ `LATEST.sql` |
| 前端 | `frontend/src/pages/data-quality/`、`pages/settings/DataQualitySettingsPage.vue`（含 LLM 用量卡）、`pages/settings/LLMProviderManagementPage.vue`（Max output tokens）、`components/data-quality/`（含 `SuggestionWorkbench.vue`、`SuggestionJobsPanel.vue`、`SuggestionJobHistoryDrawer.vue`、`SuggestionJobTargetDebugDialog.vue`、`BatchCreateRulesDialog.vue`、`BatchDecideSuggestionsDialog.vue`）、`api/data-quality.ts`、`utils/dataQuality*.ts`（`suggestionSummaryView`/`suggestionSummaryReason`）、`composables/useSuggestionSummary.ts`、`lib/notificationText.ts`；路由 `router/index.ts`，侧栏 `AppSidebar.vue`，`SettingsNav.vue` |
| 单元测试 | `component/dataquality/*_test.go`（编译 golden ×4 方言、判定、filter、custom_sql、CEL、画像、标注/prompt、schema_drift、group_check、aggregate、cross_table、null_values、issue、execution、errors、逐项能力矩阵、`suggester_test.go`/`quota_test.go`）；`store/quality_execution_test.go` 等 `store/quality_*_test.go`（含建议 GUID 前缀过滤、起草任务生命周期守卫）；`runner/qualityrunner/qualityrunner_test.go`、`runner/qualitysuggestionrunner/qualitysuggestionrunner_test.go`（claim/逐目标/取消/配额/关机/panic/诊断持久化）、`component/llm/errors_test.go`、`component/dataquality/suggester_diagnostic_test.go`、`store/explain_sql_test.go`、`runner/maintenance`；`api/v1/data_quality_*_test.go`（含批量大小/项父级/建议目标解析/列表过滤/起草任务）；`component/notification/data_quality_test.go`、`api/v1/notification_service_test.go`；前端 `*.test.ts` 与 `lib/notificationText.test.ts` |
| 集成测试 | `backend/test/integration/runner/data_quality_service_test.go`（规则生命周期、问题/通知、元数据+只读规则、画像/标注、jsonb 画像、不可运行列类型在保存期被拒、json 列上的 plain null_percent 真执行、cross_table 右表为视图、LLM 默认关闭、建议中心批量创建/决策与列表前缀过滤、起草任务提交/读取/列表/取消、失败诊断与调试图读取/清理）；`data_quality_journey_service_test.go`（端到端旅程：18 类规则的六态矩阵与失败样本、手动+定时执行、问题 occurrence/恢复、通知、概览、画像/标注、修复数据自动恢复、ERROR(NO_READ_ONLY_SOURCE)、SKIPPED(ORPHANED) 与 `orphaned_reason` 读路径、DDL 变更后的 schema_drift 复核、归档保留历史） |
| 权限 | `backend/common/permission/permission.json` + `permission_gen.go`；`store/predefined_roles.go`（member 基线）；前端镜像 `frontend/src/lib/permissions.ts` |
| 启用点 | 迁移 `0024` + `0025` + `0026` + `0027`（随二进制自动执行）；每个实例需配 `READ_ONLY` 数据源；LLM 需设置页开启 `enable_llm_analysis` 且存在允许的 provider profile；无其他开关 |
| 门禁 | Go：`gofmt`、`golangci-lint`、`go test ./component/llm/ ./component/dataquality/ ./store/ ./runner/qualityrunner/ ./runner/qualitysuggestionrunner/ ./runner/maintenance/ ./api/v1/`；proto：`buf format/lint/generate`；迁移链测试；前端：`biome:check`、`lint`、`i18n`、`type-check`、`test run`、`audit:ui` |

> 引擎覆盖待办：CI 目前只有 PostgreSQL + MySQL 的 testcontainers。MSSQL / StarRocks / Doris 的类型能力矩阵由单测 + 手工容器 probe 固化（`capabilities_test.go`），**待 CI 环境增加这三种引擎后补真实集成测试**。MariaDB/TiDB/OceanBase 继承 MySQL 家族规则但未逐一探测（MariaDB 的 JSON 是 LONGTEXT），列入同一待办。

**非目标**（不做）：RuleSet/contract-like 整体验证（D-20）、模板/示例库、进程内 Python/WASM/任意代码、LLM-as-judge 判定、自由 SQL 生成并执行、自动修复数据/阈值、邮件/webhook/Slack/PagerDuty、质量分/scorecard、流式/行级检查、分布漂移/季节基线、MCP/CLI 暴露、跨副本调度、跨实例/跨库对账（cross_table 限同库）。
