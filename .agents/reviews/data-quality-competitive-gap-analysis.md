# 数据质量能力：竞品对标与差距分析

> 对象：Metaxisdata 数据质量模块（`/data-quality`，代码 `backend/component/dataquality`、`backend/runner/qualityrunner`、`backend/runner/qualitysuggestionrunner`、`frontend/src/pages|components/data-quality`）
> 方法：仓库代码 + `docs/data-quality.md` / `.agents/docs/data-quality.md` / `.agents/docs/data-quality-target-centric.md` 逐项核对；竞品以公开文档为主，区分"文档确认"与"宣传口径"；2026-10 调研，网络经 `http://172.21.112.1:7897`。
> 结论口径：只讨论**会改变用户选择/验收结果**的差距，不追求功能清单的对称。

---

## 0. 结论摘要

**一句话：我们输的不是"检测能力"，是"检测之后那一段"。**

- **规则引擎在第一梯队（强于绝大多数竞品）**：18 类有类型的检查、按引擎族的逐项能力矩阵（`capabilities.go` + D-35）、保存期就拒绝不可执行的规则、方言编译 golden、不可变 revision、六态结果（`ERROR` 与 `FAIL` 分离）、`on_empty` 语义、失败样本带主键、preview dry-run、目标级契约 filter 编译期合并。GX 50+ expectation 靠的是"广"，我们 18 类靠的是"每一类在每种引擎上都能真的跑"——**OpenMetadata/DataHub/dbt 这类目录内嵌能力达不到，商业产品也很少把能力矩阵做到类型×引擎级**。
- **"检测之后"基本为零**：缺**告警送达**（只有站内信）、缺**报表/导出/质量分**、缺**异常检测/动态阈值**、缺**自动 schema 变更监控**（仅 StarRocks/Doris 有效）、缺**CI/调度生态集成**、缺**血缘在 DQ 中的使用**、缺**问题协作与整改闭环**。这些恰好是采购、运维和验收真正看的部分。

对照用户三个问题，答案分别是：

| 问题 | 结论 |
| --- | --- |
| **缺失了哪些功能？** | **21 项**，见 §5：**P0 四项**（告警渠道与路由、报表与导出、覆盖率/健康度/质量分、资产页信任信号）；**P1 九项**（接入即推荐检查、观测趋势与经验阈值、全引擎 schema 变更监控、分区/变量/抽样/超时、目标级 cron 与频率档、CI 门禁与代码化、外部断言摄取与 OL facet、血缘影响面与根因、MCP 质量工具）；**P2 八项**（批量操作、工单闭环与事故归并、码表/校验位规则与模板、问题协作、跨实例对账、检查类型补强、评分卡权重与等级、多副本） |
| **哪些功能设计得不够好？** | 11 项，见 §6：`row_count_change` 默认永远建不起基线（功能级缺陷）、schema 变更监控只在 StarRocks/Doris、契约缺消费者、目标级调度一刀切、CEL 被当成行级断言实为采样、失败只通知一次、画像 7 天保留+同步执行、死配置与遗留、检查目录只读无批量、问题台账协作不足、概览"受监控表数"无分母 |
| **哪些可以改善？** | 见 §7/§8：把已有资产（血缘、OpenLineage、MCP、执行历史里的 `observed_value`）接进 DQ；三个 release 约 75–105 人日把"运维闭环 → 监控有效性 → 生态集成"补齐 |

**三个最要紧的缺口（按"丢单/丢用户"排序）：**

| # | 缺口 | 为什么致命 | 竞品现状 |
| --- | --- | --- | --- |
| 1 | **告警只能落在站内信**（`.agents/docs/data-quality.md` D-09、非目标清单；全仓无 SMTP/Webhook/IM 代码） | 自托管部署里没人盯铃铛。故障在无人看页面时无法送达值班，DQ 的运维价值等于零 | Monte Carlo 9 种渠道 + SLO/升级；OpenMetadata 邮件/Slack/Teams/Google Chat/Webhook；DataWorks 含电话/短信/钉钉/企微/飞书 |
| 2 | **没有报表、导出与健康度汇总**（`QualityOverview` 只有计数与 7 天趋势；DQ 页面无任何 CSV/PDF 出口） | 周会、验收、审计、跨团队通报都拿不到东西；国内验收里"评价报告（总评分/二级指标/问题分布）"是**必选交付物** | GX Data Docs、Elementary `edr report`、MC/DataArts 报告与导出、DB3205/T 附录 C 六章报告 |
| 3 | **没有异常检测，schema 变更监控在主力引擎上是空的** | 静态阈值要求人先知道"正常值是多少"，覆盖率上不去；`target_schema_changed` 在 MySQL/PG/MSSQL 恒为 false（gap #19），而这三类恰是主力引擎 | MC/Sifflet/Bigeye 的自动阈值 + 反馈闭环；DataHub/Snowflake/Databricks 的窄口径异常检测；所有目录都有自动 schema 变更信号 |

**三个最值得放大的优势：**

1. **目标（表/视图）中心的产品模型 + 契约**：与 Soda contract、Monte Carlo asset 的聚合单位一致，且我们已经把"表级/跨字段/字段"分区、覆盖卡、就地编辑、AI 草案就地审阅做完了（`data-quality-target-centric.md` 已论证）。
2. **元数据与血缘原生，但 DQ 还没用起来**：这是竞品最难复制的一层——我们已经有表级/列级血缘、OpenLineage 摄取、MCP、`mxd` CLI。把"失败检查 → 下游影响面""上游根因""按血缘推荐检查"做出来，比追 ML 异常检测更便宜、更像我们。调研结论也印证：**只有 Databricks/Snowflake/Monte Carlo 把血缘用到了 DQ 上，且没人展示"下游消费者正在读一张失败的表"**。
3. **自托管 + 单二进制 + 确定性引擎 + LLM 安全边界**：数据不出域是 Monte Carlo/Anomalo 的硬伤；GX/Soda 要一套 Python 基础设施；DataHub 的评估/告警是 Cloud 专属。我们的 LLM 路径不发原始样本行、只产 PENDING 建议（D-30、I-12/I-13），"能用 AI 又不失控"是超前设计。


**一个结构性提醒（投资结构）：** DQ 后端（非测试）合计约 **2.96 万行**——`component/dataquality` 12.6k、`store/quality_*` 7.1k、`api/v1/data_quality_*` 6.9k、两个 runner 3.0k；其中直接服务 LLM 的（建议/解释/标注/任务/配额/诊断 + 对应 handler）约 **5.0k 行，约 17%**，若把共享的 `component/llm`（2.0k）也算上约 22%。前端 `data-quality` 的 42 个 `.vue`（10 页 + 32 组件，约 2.29 万行）里，建议中心/解释相关约 **3.8k，约 17%**。而"告警送达、报表导出、异常检测"三块是 **0 行**。这不是说 LLM 做错了，而是说：**默认关闭的 AI 子系统与生产必需的运营闭环投入相当，而后者为零**——下一阶段的投入比例应该反过来。

---

## 1. 我方能力快照

### 1.1 判定能力

| 维度 | 现状 |
| --- | --- |
| 检查类型 | 18 类：`row_count`、`row_count_change`、`null_percent`、`unique`、`unique_combination`、`accepted_values`、`range`、`regex`、`string_length`、`referential_integrity`、`cross_column`、`freshness`、`custom_sql`、`cel_expression`、`schema_drift`、`group_check`、`aggregate`（SUM/AVG/MIN/MAX/STDDEV/PERCENTILE）、`cross_table`（同库对账） |
| 阈值 | `fail`/`warn` 双阈值；`threshold_metric` = COUNT/PERCENT（10 类可选 PERCENT）；严格比较（等于通过）；`on_empty ∈ {NOT_EVALUATED, PASS, FAIL}` 并写入阈值快照 |
| 过滤 | 结构化 AND 子句（10 种算子、≤10 条、IN ≤100），目标契约 filter 在编译期 AND 合并（合计 ≤20）；`custom_sql` 不受契约 filter 约束 |
| 证据 | 失败样本（默认 20、上限 100 行），列检查优先带主键列；SQL `NULL` 与空串分列；值截断 256B、结果 1MiB、SQL 8KiB |
| 预检 | `PreviewQualityRule` 编译 + `LIMIT 0` 探测（10s），不落库 |
| 版本 | head + 不可变 revision，执行记 `rule_revision` 与 `rule_engine_version` |
| 方言/能力 | MySQL 家族/TiDB/MariaDB/OceanBase、StarRocks/Doris、PG、MSSQL；`regex` 不支持 MSSQL；`PERCENTILE` 仅 PG/MSSQL；`cross_table` 限同库；逐项能力（comparison/grouping/countDistinct/regex/length/extrema/text + 两个否定例外）在**保存期**校验 |
| 结果 | 六态 `PASS/WARN/FAIL/ERROR/SKIPPED/NOT_EVALUATED`，rank `ERROR > FAIL > WARN > NOT_EVALUATED > SKIPPED > PASS`；执行生命周期 `PENDING/RUNNING/COMPLETED/ABANDONED` |

### 1.2 调度与执行

| 维度 | 现状 |
| --- | --- |
| 调度单位 | **目标**（表/视图）一个 schedule，interval 15 分钟–7 天（`dataquality/settings.go`），到期跑该目标全部启用检查；新建检查加入下一轮 |
| 触发 | 定时（target `next_run_at`）+ 手动（选择集 ≤200）；手动按目标拆批 |
| 批量 | 一目标一批次（`quality_run`）；tick 预算 ≤200 规则 / ≤50 目标；单批 ≤100 检查；scheduled worker 2 个、同实例串行 |
| 重试 | 目标周期内未完成的 scheduled 检查补跑，`attempt ≤ 3`；手动不补跑 |
| 资源保护 | 每实例连接预算（scheduled 等 ≤2m、manual ≤5m）、手动队列 512、批次超时 15m、单次查询超时 **固定 60s**、日执行上限 10000（UTC 日，触顶只跳过定时扫描） |
| 执行通道 | 强制 `READ_ONLY` 数据源（无则不回退 ADMIN）；PG `SET LOCAL statement_timeout`、MySQL `MAX_EXECUTION_TIME` 兜底 |
| 错误分类 | `CONNECTION/AUTH/TIMEOUT/PERMISSION/UNSUPPORTED/SQL_SYNTAX/BAD_DATA/LIMIT_EXCEEDED/ORPHANED/NO_READ_ONLY_SOURCE/EXECUTION`（`QUOTA` 为历史值） |
| 并行度 | **单副本**：inflight、配额、scheduled 队列、实例槽位都在进程内 |

### 1.3 问题、通知与协作

| 维度 | 现状 |
| --- | --- |
| 问题模型 | 每检查**至多一个**未解决问题（部分唯一索引，D-08）；`first/last_seen` + `occurrence_count`；状态 OPEN/ACKNOWLEDGED/RESOLVED，自动恢复 `AUTO_RECOVERED` |
| 处置 | 确认（记录人/时间）、解决（原因 + note）、静音（规则级 + 问题级 `muted_until`） |
| 通知 | **仅站内信**（`NOTIFICATION_TYPE_DATA_QUALITY`，三种 DQ 通知类型之一）；收件人 = 规则 owner → 无 owner 时 admins；问题开启/恢复按 issue 资源名 **1h 去重**；`EXECUTION_FAILED` 只发 admins 且不可静音；单批次 FAIL > 10 合并 `STORM_SUMMARY` |
| 缺失 | 邮件/Webhook/IM/Slack/PagerDuty、升级策略、失败持续提醒、维护窗口、问题评论/@人、指派给他人、SLA/到期时间、关联工单、影响面 |

### 1.4 契约、画像、AI

| 维度 | 现状 |
| --- | --- |
| 契约 | 每目标一行：目标级 filter（编译期 AND 到全部规则）+ 默认严重级别；**显式发布**产生不可变 revision（fingerprint + added/modified/removed 计数 + note + 发布人）；幂等发布；权限 `dataQuality.contracts.publish`；审计 |
| 契约边界 | 发布**不阻塞执行**（I-40）；没有 CI/外部消费者；目标级 owner/tags 默认值未做；契约 YAML 导出列为 P2 |
| 画像 | 每表 1 条聚合 SQL + 1 条样本 SQL；null 计数、distinct（能力不足时按样本估算并标 `distinct_estimated`）、min/max、长度、Top-K（distinct ≤20）、样式；同步在 API 请求内（120s、≤列数上限 50、4 MiB）；**保留 7 天** |
| 语义标注 | 封闭枚举 `QualitySemanticType`；`MANUAL` / `LLM`；唯一键 (table_guid, table_type, column_name)；LLM 不自动覆盖人工 |
| AI 三能力 | L1 规则草稿（同步 + 后台任务，1..20 目标）、L2 失败解释（streaming + 缓存）、L3 标注/规则建议；只产 PENDING 建议，人工接受才生效；`signature` 身份去重 + `SUPERSEDED` 事实关闭；配额 10000/日（L1+L2）/50（L3）、并发 8、进程内 UTC 计数；provider profile 输出预算 32768；失败有分类码/correlation id/debug 日志（7d） |
| AI 边界 | 不发原始样本行（`send_sample_values_to_llm` **无消费者**，docs §7 #3）；提示注入中和；模型无工具无连接 |

### 1.5 接口、权限、前端

| 维度 | 现状 |
| --- | --- |
| RPC | `DataQualityService` **42 个**；批次只读 `ListQualityRuns`/`GetQualityRun`；批量创建（≤200）、批量决策（≤200）；目标目录/工作台聚合读；目标调度读写 |
| 权限 | `metaxisdata.dataQuality.*` 16 项；member 基线 7 项 |
| 审计 | 写操作 + LLM + 画像/标注 + 契约发布均审计；列表/详情/定时执行不审计 |
| 限流 | 审计类 RPC 进 principal 预算（3000/min） |
| 前端 | 6 个侧栏入口（概览/质量目标/检查/问题/执行/建议），11 条 route（含 2 条旧 URL 重定向）→ 9 个页面；`data-quality` 目录下 32 个 `.vue` 组件 + 10 个页面（约 2.29 万行）；术语统一为 check/检查 |
| 门禁 | 布局审计（`audit-ui-layout.mjs` 预算）、保存前必须 dry run、`ERROR` 与 `FAIL` 文案徽标必须不同 |

### 1.6 生态与集成（当前为空）

| 能力 | 现状 |
| --- | --- |
| 消息渠道 | 无 |
| 报表/导出 | 无（DQ 页面无 CSV/PDF；执行/问题列表无导出） |
| CI/CD | 无（`mxd` CLI 只有 auth/config/instance/database/meta/lineage，无 quality 子命令） |
| 编排 | 无 Airflow/Dagster/dbt 集成（OpenLineage 只**摄取**，不**发出** DQ 结果） |
| 外部断言摄取 | 无（GX/Soda/dbt 结果不能进入问题台账） |
| MCP | 9 个工具（instances/databases/metadata/search/DDL/analyze_sql/lineage/whoami），**无 DQ 工具** |
| 变更监控 | `target_schema_changed` 只对实现 `ObjectDefinitionReader` 的引擎有效（当前仅 StarRocks/Doris，`backend/runner/schemasync/object_definition.go`） |

### 1.7 既有竞品调研的继承

`.agents/docs/data-quality-target-centric.md` §2 已经做过一轮对标（Soda contract 语言、Monte Carlo 5 类 monitor、GX suite、dbt/Elementary、Anomalo/Bigeye 的异常检测定位），结论是"聚合单位=表/数据资产、契约=检查集合+元数据+发布状态、AI 只产建议"。**那一轮解决的是"入口与信息架构"；本报告解决的是"监控闭环与运营能力"**，两者不重复。

---

## 2. 对标方法与口径

四类竞品，各回答不同问题：

| 类别 | 代表 | 回答的问题 |
| --- | --- | --- |
| 开源代码优先 | Great Expectations、Soda Core、dbt tests + Elementary、Deequ、Pandera | **"检查表达力与工程化"** 的上限在哪，成本是什么 |
| 商业可观测性 | Monte Carlo、Anomalo、Bigeye、Sifflet、Metaplane、Collibra DQ、Ataccama、Informatica | **"客户愿意付费的闭环"** 长什么样 |
| 目录/数仓原生 | OpenMetadata、DataHub、Databricks、Snowflake、AWS Glue、Dataplex、Purview、Atlan、Alation | **"目录内嵌 DQ 的及格线"** 与资产页集成方式 |
| 国内云与验收口径 | DataWorks DQC、WeData、DataArts、DataLeap；袋鼠云/亿信/星环等；DAMA/ISO 8000/GB/T 36344 | **"项目验收的功能清单与评分/报告形态"** |

口径约定：
- 只把"文档能证明"的能力写成事实，宣传页话术标注 **（宣传口径）**，读不到细节的写 **未证实**。
- 能力对比按**是否可用于生产**判断，不按"是否有一个 demo"。例如"支持邮件告警"要求有渠道配置、收件人解析、失败重试。
- 我们的"不做"清单（`.agents/docs/data-quality.md` 末尾非目标）视为**已决策**，只在市场已把它变成及格线时才建议推翻，并说明理由。

---

## 3. 竞品全景

### 3.1 开源/代码优先

> 详情见 `temp/dq-research/01-oss-codefirst.md`（含逐产品 URL）。

| 产品 | 定位 | 值得注意的事实（对我们要紧的部分） |
| --- | --- | --- |
| **Great Expectations**（Apache-2.0，1.24） | "数据的单元测试" Python 框架 | 50+ 内置 expectation，覆盖面最广；Checkpoint → Action（Slack/邮件/Teams/自定义）；Data Docs 是**静态 HTML**；**1.x 删掉了 profiler 与 Data Assistant**（自动建议能力倒退），无原生异常检测、无调度、无问题台账；UI/调度/告警在 GX Cloud |
| **Soda Core / Soda Cloud** | SodaCL YAML → SQL 下推 | 25+ metric + 7 类"独有"检查（freshness/anomaly/reconciliation/reference/cross/schema/distribution）+ `failed rows`；**异常检测用 Prophet，已标记废弃**，改为 Cloud 专有检测器；契约是独立 YAML 且只在 programmatic scan 生效；**`soda-core` 3.3.5 是 Apache-2.0，4.29 在 PyPI 上标 Proprietary** |
| **dbt tests / dbt-expectations / model contracts** | 与模型同仓的断言 | 内置只有 4 个（unique/not_null/accepted_values/relationships）；dbt-expectations 提供 ~62 个宏但 README 首行写"不再积极维护"；model contracts 只管**类型/结构**、在 build 前 fail；dbt 官方明确"测试不属于契约"；测试只在 dbt 跑时跑，`store_failures` 覆盖上一次失败 → **无历史** |
| **Elementary**（Apache-2.0） | dbt 之上的可观测性 | OSS 是 **Z-score（默认 3σ）+ 季节性/训练窗口/维度异常/volume/freshness 异常**；`edr report` 是**静态 HTML**；官方自己承认 OSS 是统计方法、Cloud 才是 ML；健康分/维度/环境/PagerDuty/Jira 等在 Cloud |
| **Deequ / PyDeequ**（Apache-2.0，活跃） | Spark 上的约束计算库 | **MetricsRepository（文件/JDBC）**是这套工具里最好的"历史指标"抽象；4 种异常策略（相对变化率、绝对变化、online/batch normal mean±3σ）；`ConstraintSuggestionRunner`（profile → 启发式约束建议）；需要 Spark + Java 11，无 UI/告警/调度 |
| **Qualifire**（Oracle AIDP 示例，许可未确认） | 最"像平台"的开源设计 | 4 种策略（SLO/阈值/Prophet 预测带/Isolation Forest + SHAP）、WAP、system table 四种后端、`partition_ts` 一等列、严重级别路由 + 去重 + 跨数据集分组、单文件交互式 HTML 看板、回填 |
| **DataKitchen TestGen**（Apache-2.0，自托管） | 架构上最接近我们的开源产品 | **profile 每列 → 自动生成测试**、内置调度在库内跑 SQL、freshness/volume/schema 监控、Web UI + REST API + **MCP server**、14 个连接器 |
| **Pandera / data-diff** | 边界参考 | Pandera（MIT，~30 检查）只做 dataframe 断言；data-diff（MIT）**2024-05 停止维护**，证明"跨库对账"是独立且难的能力 |
| **OpenLineage facet** | 互操作标准 | `dataQualityAssertions`（assertion/success/column/severity/expected/actual/content+contentType=sql）与 `dataQualityMetrics`（nullCount/distinctCount/min/max/quantiles）**几乎 1:1 对应我们的结果模型**；我们已经在摄取 OL，发出质量断言是最便宜的标准化出口 |

**这一类的共同结论**：开源工具把"断言表达力"做到了极限，但**历史、告警、UI、调度、问题闭环几乎全部被切到商业版**（GX/Soda/Elementary 都是）。这恰好是我们的机会——我们是"这些被切走的部分 + 一个够用的规则引擎"的组合，只是**被切走的那部分我们也没做**。

### 3.2 商业可观测性（Monte Carlo / Anomalo / Bigeye / Sifflet / Metaplane / Collibra DQ / Ataccama / Informatica）

这一类回答"**客户愿意为闭环付多少钱**"。它们卖的不是规则，而是"**发现 → 通知 → 分诊 → 归因 → 度量**"的运营链路。

| 能力 | Monte Carlo | Bigeye | Sifflet | Metaplane | Anomalo |
| --- | --- | --- | --- | --- | --- |
| 检测方式 | ML 异常检测（5 类 monitor：Table/Metric/Validation/Job/Agent）+ 敏感度/排除窗口/节假日/自动学习复发异常；6 周窗口 + 周季节性 | **Autothresholds** 自学习阈值 + 用户对告警打分（强化学习）；Deltas 跨数据集/跨源比对；Join Rules 跨源一致性 | 25 个模板（动态/静态体积、新鲜度、schema change、重复、分布变化、空值、唯一、值域、格式、关联完整性、correlated metrics、custom SQL）+ ML 阈值 + 反馈闭环 + 特殊日期 | 14 类 monitor（行数/新鲜度/均值/标准差等）+ 历史观测导入 + **forecast** + "新常态"标注 + 模型反馈 | 以列 profile 为基线的自动异常检测；**无公开文档**（官方 FAQ 称文档仅客户可见），"40+ 无代码规则"仅为营销口径 |
| 开箱/规模化 | 表级 monitor **零配置**即开始；Key Assets/Domains/Data Products | **Autometrics**：每天索引时给新表/列自动生成指标 | **Auto-Coverage** 按表建议 5 类规则；Automatic Monitoring | **Rules**：按库/模式条件自动加 monitor；dbt `meta` 即代码 | 自动表/列监控（口径未公开） |
| 告警渠道 | Slack/Teams/Email/**PagerDuty/OpsGenie**/Jira/ServiceNow/Linear/Google Chat/Datadog/Webhook | Slack/Email/**Webhook**/Teams/Jira/ServiceNow/**PagerDuty**/Azure Boards | Email/Teams/Slack/**PagerDuty**/Jira/ServiceNow/Webhook/Statuspage/Google Chat | Slack/Teams/**Webhook**/Alert routing/Alert mentions（PagerDuty/API 为 Enterprise 档） | Slack/PagerDuty 等（未公开细目） |
| 降噪/分诊 | 告警=事件流 + **names audiences** + 按优先级路由（含 "Not triaged" 桶）；排除窗口、静音 | 可对告警评分；Mute；**Unified Issue Inbox** | **自动 Incident 分组** + 通知规则（按状态/范围过滤） | Alert routing + 标签优先级 + dbt 节点级告警 | 阈值/敏感度 |
| 事故与归因 | Alert → ACK → **mark incident**（SEV-1..4，±15 天归并）；RCA insights；SLO（响应时限）；Circuit Breaker（阻断下游） | **Declaring an Incident**：多个 issue 合成一个（共同根因）+ Impact Analysis + 血缘节点 overlay | Incident 生命周期 + **Sage 根因分析** + 受影响看板数 | Incidents + 端到端血缘 | 根因/影响分析（营销） |
| 度量/报表 | Table Health / Data Quality / Data Ops、**可组合看板**（监控覆盖率 `coverage_rate`、未覆盖关键资产、资产重要度、MTTA/MTTR）、周报邮件、导出 | **Scorecard**（按维度历史、诚实灰显"未部署指标"）+ Dashboard + Evidence Reports | Overview/Operations Dashboard、资产健康、数据产品上游告警 | Dashboards、Chrome 扩展、Jira | 看板（未公开） |
| 样本证据 | 采样行（可客户侧存储） | 失败样例 + Debug Query | Failing rows | — | **good vs bad 行对比**（营销） |
| CI/CD 与代码化 | Monitors/Notifications as Code（YAML/CLI/namespace/dry-run）、Terraform、MCP、**MC Prevent PR & CI Agent** | bigConfig 配置即代码、CLI、Python SDK、MCP | **Monitors as Code（YAML + 模板 + for 循环）**、Terraform、CLI、**Flow Stopper**（流水线门禁）、MCP | Monitors as Code、GitHub/GitLab **Data Test/Impact Previews**、CLI | API（营销） |
| 定价/部署 | SaaS + Agent（PrivateLink/自托管凭据）；按 credit，第三方估算 ≈$115–179/表/年 | SaaS + Agentless/Agent；未公开价 | SaaS + **Self-Hosted** + Agent + PrivateLink；第三方可见 $48k/12 月 credit 包 | SaaS；Free 10 表 / 按表 Pro / Enterprise（渠道分层） | SaaS；第三方称中位合同 ~$115k |

补充说明（都是影响结论的细节）：

- **Anomalo 没有公开文档**（官方 FAQ 明确说文档只给客户/试用方），所以"Anomalo 有 40+ 无代码检查"这类说法**只能当营销口径**；本表 Anomalo 列只作方向性判断。其营销页还有 5/9 个 "agent" 标注 coming soon。
- **Sifflet 是独立公司**（2025 年 EQT Ventures/Mangrove/Capmont 轮），没有被收购；**Datafold 已转向迁移 agent + Data Knowledge Graph**（Data Diff 只剩 MCP 工具形态）；`telmai.com` 是待售域名（实际站点为 telmai.cc）。这几个"竞品"在采购清单里的威胁度需要按此修正。
- **Collibra DQ&O / Ataccama / Informatica / IBM 的定位**：都是治理套件里的 DQ 模块，卖的是"规则库 + 维度分 + 自托管/本地化"。Collibra 有 11 个开箱 monitor、6 维度、可按方言的模板库、**可自托管**；Ataccama 是集中规则库（去重/版本）+ Data Trust Index + 真实整改/清洗（on-prem/edge）；Informatica 用 CLAIRE 从 insight 生成 rule spec + scorecard；**IBM 是唯一给出明确评分公式的一家**（`∏(1−frequency)` + 列/资产加权），但 DQ 规则要求 DataStage Enterprise。它们的通病是"必须买整套平台"。
- **定价结构解释了覆盖率看板为什么存在**：MC 的文档自己说"被监控标记就是 Table Monitor 的范围，也用于计费"。所以**按表计费的产品必须证明覆盖率**；而自托管平坦定价可以"免费给覆盖率"——这是细分市场优势，但要主动讲出来（见 §7）。

**这一类的共同结论**：

1. **告警不是"渠道列表"，是"路由 + 升级 + 去重 + 运营指标"**：Monte Carlo 有 audiences/优先级/SLO/MTTA-MTTR，Sifflet 有通知规则 + 事故自动分组，Metaplane 有 alert routing + mentions，Bigeye 有 Unified Issue Inbox。**我们只有 1 小时去重 + 站内信**，连"谁该被叫醒"都没有建模。
2. **异常检测是"窄而深"**：每家只覆盖少数核心指标（体积/新鲜度/schema + 若干列统计），靠**反馈闭环**（人工标注"这不是问题"）和**排除窗口/节假日/特殊日期**把误报压下去。这正是我们最容易补的一块：数据已在我们库里（`quality_execution.observed_value` 是天然的时间序列）。
3. **"代码化 + CI 门禁"是标配**：Monitors as Code、Terraform、GitHub Action、Metaplane 的 PR 预览、Sifflet 的 Flow Stopper、MC 的 Circuit Breaker。**我们只有 RPC，没有文件格式、没有 `apply`/dry-run、没有 CLI**——这一块是"看起来有 API、实际用不上"。
4. **"接入即监控"是最大的体验差**：MC 表级 monitor 零配置、Bigeye 每天索引自动生成指标、Sifflet Auto-Coverage、Metaplane 从 dbt meta 生成。**我们要求用户逐条手配 18 类检查**——这是我们规则引擎强、但覆盖率上不去的根因。
5. **"发现问题"和"事故"是两层**：所有商业产品都有 check 之上的 incident 对象（严重级别、owner、状态、活动流、TTA/TTR、归并多检查）；**我们的 issue 是"发现台账"，不是"事故"**。



### 3.3 目录与数仓原生（OpenMetadata / DataHub / Databricks / Snowflake / Glue / Dataplex / Purview / Atlan / Alation）

> 详情见 `temp/dq-research/03-catalog-warehouse-native.md`（698 行，逐平台 + 12 行对照表 + 来源）。

这一类的价值是回答"**目录内嵌 DQ 的及格线**"。八家的形态已经收敛成同一句话：

> **类型化规则 + 调度 + 运行历史 + 事件/事故 + 资产页上的健康/评分信号 + 至少一个外部告警渠道 + 能摄取别人家的结果。**

| 平台 | 规则能力 | 异常/漂移 | 调度 | 告警 | 事故 | 健康/评分 | 资产页集成 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **OpenMetadata** | ~25 内置（9 表级 + 16 列级）+ 维度级校验；自定义测试（REST/Python） | ✘（只有严重级别） | 每 suite 一条 pipeline（小时→月/custom，可多条 + 手动） | 邮件/Slack/Teams/Google Chat/通用 Webhook | **自动开单** + ack/指派/根因/解决 + 自动严重级别 | 按实体/suite/case 的质量页；无覆盖率 | 表/列 DQ 标签页；无徽标/评分 |
| **DataHub** | 6 类 assertion（freshness/volume/column metric/column value/custom SQL/schema）；dbt/GX/DMF/SQLMesh/Monte Carlo/ODCS 摄取 | freshness+volume GA，column metric/custom SQL beta，14 天训练，按 bucket 处理季节性 | Cloud 调度 / 摄取驱动 / VPC executor | Cloud 订阅（邮件/Slack/Teams）+ EventBridge/Kafka | ACTIVE/RESOLVED + 阶段 + 优先级 0–3 | Data Health Dashboard（按 assertion / 表健康点 / 事故）；无评分 | 资产页质量与健康状态 + 契约标签页；下游影响定严重级别 |
| **Databricks** | profile 指标表（time series/inference/snapshot）+ expectations（warn/drop/fail）+ 异常检测 | **分布漂移**：chi²/KS/TV/L∞/JS/Wasserstein/PSI；按 schema 的完整性/新鲜度异常 + 切片 | 表更新时自动扫描（intelligent scanning，按热度/下游用量排优先级） | schema 告警 + SQL 告警 | 事故含根因（血缘）、影响 H/M/L、指派 7 天、自动解决 | **Catalog Explorer + 血缘节点上的健康指示** | **这一类里最好的**：资产与血缘节点徽标、质量页、影响面 |
| **Snowflake** | 185+ 系统 DMF（10 类）+ 用户 DMF + expectation | 只有 ROW_COUNT / FRESHNESS（最少 2 周、建议 60 天） | interval ≤1 天 / cron / **DML 触发** | ✘ DQ 原生（需自己配 ALERT + notification integration） | 看板里算"事故"，无 ack/指派 | **健康表占比 + 监控覆盖率**；按对象 DQ 标签页 | 表页 + 覆盖率 + 血缘**受影响资产** |
| **AWS Glue** | DQDL **33 条规则**（规则+analyzer）、动态阈值、WHERE、labels、constants | 统计式 ML，≥3 个点，Linear/Fixed；**规则推荐** | Glue 触发器/Step Functions/手动；增量 | EventBridge + CloudWatch（无原生分页/事故） | ✘ | **DQ score = 通过规则占比**；观察值 | 控制台 ruleset 标签页；无徽标/owner 路由 |
| **Dataplex** | 6 内置 expectation + 3 种自定义 SQL；规则模板 | ✘ | interval/手动；全量/增量；行过滤；抽样 | Cloud Logging + 邮件报告 | ✘ | **`data-quality-scorecard` aspect 写回目录条目**；7 个固定维度 | BigQuery/目录页质量标签 + scorecard 元数据 |
| **Purview** | 8 个开箱规则 + 自定义表达式 | ✘ | 小时→月扫描 | **仅邮件** | "Actions" 中心 + 诊断查询，无完整事故生命周期 | 最强**层级汇总**：规则/列 → 资产 → 数据产品 → 治理域，6 维度 | 数据产品/域页 + 规则标签页 |
| **Atlan** | ~20 模板（仅 BQ/Databricks/Snowflake）+ 对账规则 + schema 变更后**自动重挂** | 依赖底层引擎 | 内部或 SDK 外部 | Webhook + 路由/升级 | ✘ | DQ 维度分类 + 资产 DQ 面板 | 资产页 + 契约 |
| **Alation** | Monitors + 无代码检查，库薄 | ✘ | 内部或 SDK 驱动 | 可配置通知 | 资产页"事故通知"，无分诊模型 | 对象级 DQ 评分 + monitor 摘要 + **信任标记** | 目录页评分/监控/信任标记 |
| **Metaxisdata（我方）** | **18 类型化检查 + CEL + custom SQL**；能力矩阵保存期校验；跨库 | ✘（明确非目标） | 按目标 15m–7d + 手动；无 cron/DML 触发 | **仅站内信** | 问题台账 ACK/解决/静音；六态 | 无（无评分/覆盖率） | 目标工作台 + 资产 DQ 标签页；**无徽标/覆盖率** |

三个可以直接拿来用的判断：

1. **我们输在"信任面"而不是"规则面"**：八家里七家有资产级健康信号（徽标/评分/覆盖率），我们一个都没有；而规则表达力我们是最强的一档（尤其跨库）。
2. **异常检测比宣传窄得多**：Snowflake 只有 2 个指标，Databricks 只有完整性/新鲜度，DataHub 3 类 GA/2 类 beta，Glue 是统计式 + 最少 3 个点。**"对行数/新鲜度/null 率做可解释的期望区间"就能覆盖 80% 的价值**，不需要 ML 平台。
3. **自托管正在变成稀缺**：DataHub 的评估/看板/告警是 Cloud 专属，Databricks 要 UC+serverless，Snowflake 要 Enterprise，Purview/Dataplex 是 SaaS/单云。**"一个 Go 二进制 + Postgres、可完全离线"是一个真实细分市场**，不是脚注。

### 3.4 国内云厂商与标准/验收口径（DataWorks / DataArts / WeData + DB3205/T、GB/T 36344）

> 详情见 `temp/dq-research/04-cn-market-and-standards.md`；一手标准样本为苏州市地方标准 **DB3205/T《数字政府 市场监管数据质量监测规范》**（报批稿，2024，正文见 `temp/dq-research/sz.txt`）与 **GB/T 36344—2018**。

国内这一类与欧美产品有两处根本差异，**直接决定"能不能过验收"**：

**(0) 先说三个容易踩的认知错误。**
- **"对齐 GB/T 36344 的算法"不存在**：GB/T 36344—2018 只给六个维度的**定义**，没有权重、没有公式；给算法的是地方标准（DB3205/T 附录 A/C）与厂商自研模型。所以对外只能说"按 DB3205/T 口径实现"，不能说"符合国标算法"。
- **"六维"不是统一口径**：华为与腾讯一致地用「完整性/有效性/及时性/一致性/准确性/**唯一性**」，把国标的"规范性""可访问性"换掉了；DB3205/T 才是严格贴国标的那个。引用时必须写明是哪一套。
- **"时效性"在国标与地标里不是一回事**：国标指"数据随时间推移仍然正确"（值会过期），DB3205/T 转写成"及时有效"（到得及时）——决定规则写"距今阈值"还是"状态有效窗口"，两者不能混用。

**(1) 质量分 + 权重 + 评价报告是硬要求。** DB3205/T 给了完整口径：
- 一级指标两个：**数据质量指标 70**（规范性 20 / 完整性 20 / 准确性 30 / 一致性 15 / 时效性 10 / 可访问性 5）+ **工作质量指标 30**（工单受理及时性 10 / 处理及时性 20 / 处理有效性 40 / 整改完整性 30）；
- 计算口径是**行级通过率**：`符合规则的数据量 / 全部数据量`（不是"通过规则数/总规则数"）；
- 评价方法（C.2）：**加权平均**——规则权重 × 评测数据量 × 问题数 → 二级指标得分 → 一级指标得分 → 总分；
- 评价报告（C.1）必选章节：**报告范围、评价结果（总评分 + 二级指标评分 + 问题按指标分布 + 问题按责任机构分布）、规则清单、问题案例清单（数据标识/规则/问题描述/错误原因/严重程度/修复建议）、监测依据**；
- 监测方式（5.2）：**全量 / 定量 / 抽样**三种，报告须声明；
- 流程（7.1）：方案制定 → 数据准备 → 规则制定 → **问题发现 → 问题处置 → 复测核验** → 综合评价；问题工单状态 = 已修复 / 关闭 / 暂缓修复 + **复测核验通过才自动关闭**。

**(2) "监测之后"的闭环（工单/整改/复测/责任机构）被当成一等功能。** 上面 30 分的"工作质量指标"全部依赖工单状态机：没有受理/处理时限、没有复测核验、没有按责任机构聚合，**这 30 分直接算不出来**。值得注意的是：**三家云厂商（阿里/华为/腾讯）都没有工单与整改核验**，只有国内治理厂商（亿信华辰）把"发起整改 → 整改流程 → 直至达标"做进产品——所以这块自研不是追平，而是**对云厂商的差异点**；但**"按责任机构聚合"是标准明文要求（C.1 第 2 项）而四家全空**，加一个组织维度就能吃下，是本次调研里性价比最高的一项（见 §5 P2-2）。

云厂商的实际做法（文档确认）：

| | 阿里云 DataWorks DQC | 华为云 DataArts DQC | 腾讯云 WeData |
| --- | --- | --- | --- |
| 核心抽象 | **规则模板 → 监控规则 → 质量监控（执行计划）**，四层；模板是一等公民（内置 + 自定义 + **自建模板库**） | 规则模板 + 质量作业 + **对账作业**（跨表对账是独立作业类型）+ 运维 + 报告 | 规则模板（**56+ 系统内置、只读** + 自定义）+ 质量作业 |
| 维度 | —（以规则模板分类为主） | 完整性/有效性/及时性/一致性/准确性/**唯一性**（与 GB/T 36344 不同） | 同华为六维 |
| 质量分 | **无总分**：走"大盘 + TOP 问题表/责任人" | **唯一有完整公式**：规则评分 × 权重 → 表分 → 库分 → 连接分；业务侧 主题域分组→主题域→业务对象→表；5/10/100 分制 + 等级映射（优秀/良好/中等/及格/不及格） | 有六维"综合质量度/维度分/明细" |
| 告警渠道 | **7 种**：邮件/短信/电话/钉钉/企微/飞书/Webhook（Webhook 仅企业版）；告警是**订阅关系**（一条规则可被多人/多群订阅） | 告警条件表达式 + 云监控 | **7 种**（含个人微信） |
| 阻塞调度 | **强规则 + 红色阈值 → 任务实例置失败、阻塞下游**（借用调度失败传播，不另建通道） | **阻塞生产链路**，总览首屏即"报警和阻塞情况" | 仅"关联生产任务"时有效 |
| 触发方式 | **挂在调度节点成功之后**触发（天然拿到"这一批数据"），虚节点/空跑不触发 | 同上 | 同上 |
| 规则模板/批量 | 按表配置 + **按模板批量给多表建规则** + **自建模板库** | **由数据标准自动生成规则**（码表/编码规则 → 规则） | **56+ 内置只读** + 自定义 + 批量上传 |
| 报告 | **报告模板 + 周期订阅推送**（可自定义指标组合） | 技术/业务双报告；保留 90 天、单次查询 ≤30 天、**当天评分次日生成**、有"立即刷新"临时数据；导出 OBS/本地，单文件 ≤2000 条；评分三级下钻 | 六维报告 + 导出执行结果/日志 |
| 责任主体/机构 | "质量问题责任人" | 主题域/业务对象（业务维度，非组织维度） | 未证实 |
| 工单/整改 | ✘ | ✘ | ✘ |
| 弱点 | 无综合质量分、无工单闭环、强绑阿里生态 | 评分只覆盖"比率型"规则（布尔型不参与）、**文档说加权而公式是简单平均**、强依赖"数据标准"前置模块 | 细节未核实；内置模板只读 |

**对我们的直接含义**（详见 §5、§6）：

- 我们缺的**不是检测类型**（18 类已覆盖 DB3205/T 附录 B 的全部技术形态），而是**"检测之后"那一段**：质量分/权重、评价报告导出、工单闭环 + 复测核验 + 责任机构、监测方式声明。
- 国内口径与欧美不同：**"阻塞下游"在政务旁路监测里是反模式**（DB3205/T 5.1 明确要求"避免对业务系统产生影响"），不要抄——这是最容易被"竞品都有"带偏的一项；**"质量分 + 报告"必须抄**，因为它是验收物。
- **动态阈值/波动率在国内四家（阿里/华为/腾讯/亿信）抓到的文档里都没有明文**（只有阈值分级），所以"没有异常检测"在国内不是短板；它是对商业可观测产品（MC/Bigeye/Sifflet）的短板。两条产品线要用不同话术。
- 一个很便宜的差异化：**由"数据标准/码表"派生规则**（统一社会信用代码、组织机构代码、行政区划、行业分类、身份证号校验位）——这正是 DB3205/T 附录 B 和 DataArts "标准驱动规则"共同指向的能力，而我们只有通用 `regex`，没有校验位/码表检查。
- 另一个可借鉴的设计（亿信华辰）：**评分卡挂多套权重**——同一个质检方案可切换"政务 70/30 口径"与"通用六维口径"，投标时不用改代码就能换解释框架。



---

## 4. 对比矩阵

图例：✅ 一等公民 · ◐ 有但受限/付费版 · ✘ 无 · 「我们」列按仓库现状。竞品列只写**能给出文档证据**的形态，细节见 §3 与附录。

| 能力域 | 我们 | 开源（GX / Soda / dbt+Elementary） | 商业可观测（MC / Bigeye / Sifflet / Metaplane） | 目录与数仓（OM / DataHub / Databricks / Snowflake） | 国内云（DataWorks / DataArts / WeData） |
| --- | --- | --- | --- | --- | --- |
| 检查表达力 | ✅ 18 类 + CEL + custom SQL，**按引擎类型能力保存期校验** | ✅ GX 50+ / Soda 25+ metric / dbt 4+62 宏 | ◐ 模板化 + 自定义 SQL（规则不是主卖点） | ◐ 6–33 类（DataHub 6 / Glue 33 / OM ~25） | ✅ 模板库 + 自定义（DataWorks 自建模板库、WeData 56+ 内置） |
| 跨库/跨源检查 | ✅ **同库跨表对账 + 引用完整性**（跨实例仍为非目标） | ✘（dbt relationships 同库） | ◐ MC Comparison monitors 可跨两个数据源；Bigeye Join Rules/Deltas 可跨源；Sifflet/Metaplane 源到目标 | ✘（各引擎单平台） | ◐ DataArts 明确"跨源"；DB3205/T 附录 B 要求与外部基础库跨库比对 |
| 阈值与空数据语义 | ✅ fail/warn、COUNT/PERCENT、`on_empty`、六态 | ◐ mostly/severity（GX 有 severity + `notify_on`） | ✅ 自动阈值 + 手动阈值 | ✅ 自动/手动 | ✅ 阈值表达式 + 强弱规则 |
| 自动 profiling | ✅ 字段画像（同步 120s、7 天保留） | ✅ GX Profiler（1.x 已删）/ Deequ | ✅ 自动 profile | ✅ Databricks profile metrics / Snowflake | ✅ |
| **接入即推荐（开箱覆盖）** | ✘（要求逐条手配；有 LLM 起草与语义标注映射） | ◐ Deequ 约束建议 / TestGen profile→生成测试 | ✅ MC 表级零配置、Bigeye Autometrics、Sifflet Auto-Coverage、Metaplane dbt meta | ◐ OM 从 dbt 导入；Databricks 自动 anomaly | ✅ 按模板批量套多表 / 由数据标准派生 |
| **异常检测 / 动态阈值** | ✘（明确非目标） | ◐ Deequ 4 策略、Elementary Z-score、Soda Prophet（废弃） | ✅✅ 核心卖点（ML + 反馈闭环 + 排除窗口/节假日） | ◐ 窄：DataHub 3 类 GA、Databricks 完整性/新鲜度、Snowflake 仅 2 指标 | ? **四家文档均未证实**（只有阈值分级） |
| **自动 schema 变更监控** | ✘ 仅 StarRocks/Doris 有效（见 §6.2） | ◐ Elementary schema 变更/OCC；dbt 无 | ✅ 全部（schema monitor） | ✅ 全部 | ✅ |
| **告警送达** | ✘ 仅站内信 | ◐ GX Actions / Elementary OSS Slack；其余在 Cloud | ✅ 邮件/IM/PagerDuty/OpsGenie/Jira/ServiceNow/Webhook | ✅ OM：邮件/Slack/Teams/Google Chat/Webhook；其余受限 | ✅ DataWorks 最全（含电话/短信/钉钉/企微/飞书） |
| 告警路由/升级/静默 | ◐ owner→admins、1h 去重、风暴抑制 | ✘ | ✅ 路由 + 升级 + SLO + 排除窗口 + 事故合并 | ◐ 部分（OM auto-open+指派；DataHub 优先级） | ◐ 订阅 + 责任人；无升级矩阵 |
| **事故/问题生命周期** | ◐ 每检查一个问题，ACK/解决/静音；无评论/指派/SLA | ✘ | ✅ 事故状态流 + 影响分析 + 根因 + MTTA/MTTR | ✅ 事故 + 阶段 + 优先级 + 指派 | ◐ 告警 + 责任人；无工单闭环 |
| **评分/健康度/覆盖率** | ✘（明确非目标） | ◐ GX Data Docs；Elementary Cloud 健康分 | ✅ Table Health / Scorecard / **监控覆盖率** / 数据产品 scorecard | ✅ Snowflake 健康表%+覆盖率；Dataplex scorecard；Purview 层级汇总 | ✅ DataArts 完整评分模型 + 报告；WeData 六维分；**DataWorks 无总分**（大盘+TOP） |
| **报表与导出** | ✘ | ◐ Data Docs / `edr report`（静态 HTML） | ✅ 看板 + 数据导出 + 周报邮件 | ✅ 报告/scorecard；Purview 诊断查询 | ✅ DataArts 技术/业务报告 + 导出；DataWorks 报告模板订阅 |
| **CI/CD 与契约门禁** | ◐ 契约发布快照（不门禁）；无文件格式/CLI/CI | ✅ dbt build 失败、Soda/GX 退出码 | ✅ 配置即代码 + PR 预览 + 门禁（Flow Stopper/Circuit Breaker） | ✅ DataHub 契约失败；OM 无 | ✅ 阻塞调度（强规则）；非 CI 语义 |
| 代码化/API | ◐ 42 RPC + MCP（读）；无 DQ CLI | ✅ YAML/Python | ✅ Monitors as Code/Terraform/SDK/MCP | ✅ API/SDK | ◐ OpenAPI |
| **第三方结果摄取** | ✘（只摄 OpenLineage 事件，不摄断言） | —（它们就是被摄取的源） | ◐ 部分（Bigeye dbt；MC dbt 失败） | ✅ DataHub（dbt/GX/DMF/SQLMesh/ODCS）、OM（dbt/GX） | ◐（生态内） |
| **血缘在 DQ 中的使用** | ✘ 有列级血缘但 DQ 不用 | ✘ | ✅ 根因/影响面/沿血缘推荐 | ✅ Databricks 血缘节点健康 + 根因；Snowflake 受影响资产 | ◐ TOP 问题/责任人 |
| **问题闭环的"整改"语义** | ◐ 解决原因 note；无复测/责任机构/时限 | ✘ | ◐ 事故 + 状态；无复测 | ◐ ACK/解决（OM）；无复测 | ✘ **三家云都没有**工单/整改/复测（亿信华辰有） |
| 调度灵活性 | ◐ 目标级 15m–7d；无 cron/触发 | ◐ 靠外部编排 | ✅ cron/触发/自动扫描 | ✅ Snowflake cron/DML 触发；Databricks 更新即扫 | ✅ 挂在调度节点成功之后触发 |
| 分区/增量/抽样/宏变量 | ✘ 无变量、无分区、无抽样（CEL 采样除外） | ◐ GX batch/partitions；Soda variables | ✅ 分区过滤/窗口/采样 | ✅ 增量/过滤/采样/intelligent scanning | ✅ 全量/定量/抽样 |
| 部署形态 | ✅ 自托管单二进制 + Postgres | ✅ 库/CLI（需自建基础设施） | ✘ SaaS（部分有 Agent/PrivateLink） | ◐ 自托管但要 Python 栈/仅 Cloud 可用 | ✘ 云服务/私有化重 |
| 大规模运维（批量、模板、HA） | ◐ 每表 100/每工作区 1000；无批量编辑；单副本 | ◐ | ✅ 自动铺开 + 批量 | ✅ predicate-based 自动铺开 | ✅ 按模板批量建规则 |

**一句话读法**：规则与执行质量我们**第一梯队**；"检测之后"（异常→告警→事故→评分→报告→门禁）我们**基本为零**；生态（摄取/发出/CI）我们**只有协议没有产品**。

---

## 5. 缺口清单

> 排序 = 对"能不能被生产使用 / 能不能过验收 / 会不会被竞品比下去"的复合影响。**工作量以 1 名熟悉该模块的资深工程师计**，含前后端与测试，不含文档与灰度。

### P0 — 不做就"不可运维 / 不可验收"

| # | 缺口 | 为什么必须做 | 竞品证据 | 最小实现 | 估时 |
| --- | --- | --- | --- | --- | --- |
| **P0-1** | **外部告警渠道 + 路由/升级** | 自托管部署没人盯站内信；"3 点谁被叫醒"是所有采购的必问项 | GX Action、OM 邮件/Slack/Teams/Google Chat/Webhook、DataHub Cloud 订阅、MC/Bigeye/Sifflet/Metaplane 全渠道、DataWorks 含电话/短信/钉钉/企微/飞书 | ① 新表 `notification_delivery`（channel/target/status/attempts/next_retry_at）+ 独立 dispatcher worker：写入与投递解耦，失败重试，投递结果可在 UI 查；② 渠道：SMTP、通用 Webhook、钉钉/企微/飞书机器人（三个适配器各 ~1 天）；③ 路由：规则 owner → 团队/频道，severity 决定渠道，未确认 N 小时升级；④ 设置页：渠道 CRUD + 发送测试 + 静默窗口 | 8–12 |
| **P0-2** | **报表与导出** | 周会/验收/审计拿不到东西；"数据质量评价报告"在国内项目里是**必选交付物** | GX Data Docs、Elementary `edr report`、MC 看板/导出、DataArts 技术/业务报告 + 导出、DB3205/T C.1 六章报告 | ① 执行/问题/目标列表 CSV 导出（服务端流式，尊重筛选与权限，≤10 万行）；② 单目标"质量报告"：一个自包含 HTML（当前状态/趋势/规则清单/未解决问题/失败样本摘要/监测依据），可直接转发；③ 周报 digest（邮件/Webhook，按 owner/团队聚合） | 6–9 |
| **P0-3** | **覆盖率 + 健康度 + 维度汇总** | 只有"受监控表数"没有分母，读者无法判断"该信任多少数据"；管理层要一个数 | Snowflake **健康表 % + 监控覆盖率 %**、MC 监控覆盖率/data product scorecard、DataHub By Table 覆盖、DataArts 评分模型 | ① `GetQualityOverview` 增加 `total_table_count`/`monitored_table_count`/`covered_column_count`/`total_column_count` 与按实例/环境/owner/维度的分解；② 目标行加健康态（worst_status + 近 N 次通过率 + 未解决问题数），并把它做成**元数据树/搜索/资产页的徽标**；③ 质量分：**优先行级**（有 `failed_rows/total_rows` 的检查用 `1 - failed_rows/total_rows`，符合 DB3205/T 的"符合规则的数据量/全部数据量"口径），无行数语义的检查（行数/时效/聚合）退回规则通过率；两项都按检查权重加权，**公式写进文档、可被审计**；并明确"没有检查 ≠ 100 分" | 8–12 |
| **P0-4** | **资产页信任信号** | 消费者在目录里判断"这张表能不能用"，竞品都把信号放在资产页而不是 DQ 工具里 | Databricks Catalog Explorer + 血缘节点健康、Alation trust flags、Dataplex scorecard aspect、Atlan 资产 DQ 面板 | 复用 P0-3 的聚合：元数据列表/搜索/资产页面板显示状态点 + 覆盖率 + 最近一次运行；MCP/API 暴露同一字段 | 2–3 |

### P1 — 决定"监控是否有用"

| # | 缺口 | 为什么必须做 | 竞品证据 | 最小实现 | 估时 |
| --- | --- | --- | --- | --- | --- |
| **P1-0** | **接入即推荐检查（确定性，不靠 LLM）** | 我们要求用户逐条手配 18 类检查，这是"规则引擎强但覆盖率上不去"的根因；"接上一张表就能被监控"是市场默认体验 | MC 表级 monitor 零配置、Bigeye 每天索引自动生成指标、Sifflet Auto-Coverage、Metaplane 从 dbt meta 生成、Deequ `ConstraintSuggestionRunner`、TestGen profile→生成测试 | sync/画像完成后跑一个**确定性 pass**：按列类型 + 画像统计 + 语义标注映射出候选检查与阈值（如 null 率 < 观测值×1.5、min/max 取观测极值、Top-K 生成 accepted_values、主键 → unique、时间列 → freshness），全部落为 **PENDING 建议**（复用现有建议队列与批量创建），人工一键接受 | 6–9 |
| **P1-1** | **观测趋势 + 经验阈值（异常检测的最小可信版）** | 静态阈值要求用户已经知道"正常值"；我们的 `observed_value` 本来就是时间序列，只差展示与基线 | Deequ MetricsRepository（4 策略）、Elementary Z-score + 季节性、MC/Sifflet ML + 反馈闭环、Snowflake 2 指标异常 | ① API 返回单检查最近 N 次 `observed_value/total_rows/failed_rows`（数据已在库里）；② 前端折线 + 阈值带；③ "从历史建议阈值"（p95/p99 或 mean±3σ，排除异常点，**只作为建议值**填入表单）；④ 之后可选：`row_count_change` 改为滚动中位数基线（见 §6.1） | 5–7 |
| **P1-2** | **全引擎自动 schema 变更监控** | 现在只有 StarRocks/Doris 有信号，主力引擎没有（§6.2）；这是竞品默认能力 | 所有目录/数仓产品都有；Elementary schema 变更；Bigeye/MC schema monitor | 在 schema sync 时为所有引擎计算 schema 指纹（列名/类型/可空/顺序/主键），写 `meta_registry_resource_schema`（或新表），驱动 `schema_changed` + 自动快照 + "结构变更"通知 | 4–6 |
| **P1-3** | **执行语义：宏变量 / 分区 / 抽样 / 超时覆盖** | 分区表只能全表扫或写死 SQL；大表无法限成本；60s 固定超时不能按检查放宽 | Soda variables、GX batch/partition、Snowflake/Databricks 增量 + 采样、DataWorks 全量/定量/抽样 | ① 变量：`${today}`/`${interval_start}`/`${partition}` 等有限白名单，在 SQL 与 filter 值里渲染（**不引入通用模板引擎**）；② 分区：检查 config 增加"分区列 + 窗口"生成 WHERE，或直接让变量承担；③ 抽样：`sample_percent`/`max_rows`（`TABLESAMPLE` 或 `WHERE hash %`，**阈值按样本口径标注 sampled**）；④ per-check `timeout_seconds`（上限 300s） | 8–12 |
| **P1-4** | **检查节奏分层 + 目标级 cron/时区/维护窗口** | 目标级一刀切是用户会撞到的限制（§6.4）；企业需要"每天 2 点跑"而不是"每 24 小时" | Soda scopes（独立 cron）、MC/Sifflet/Metaplane 每 monitor cron、Snowflake cron/DML 触发。**注意反面**：Per-check cron 会碎片化批次、放大查询成本 | ① 目标 schedule 增加 **cron 表达式 + 时区**（interval 保留为默认，**不引入每检查 cron**）；② 检查只声明**频率档**（每小时/每天/每周，默认跟随目标），目标 tick 时按档过滤——保住"一目标一批次"的执行语义；③ 工作区级维护窗口（期间只跑不告警） | 6–10 |
| **P1-5** | **CI 门禁 + 代码化** | 契约缺"消费者"：没有文件格式、没有 `apply`/dry-run、没有退出码（§6.3）。注意：**没有厂商用"发布即阻断运行"做门禁**，门禁都在 CI 层 | dbt preflight、Soda 契约 + 退出码、MC Prevent PR Agent、Metaplane Data Test Previews、Sifflet Flow Stopper | ① `mxd quality run --target … --wait --fail-on fail`（CLI 已有 `mxd` 骨架）；② `RunQualityRules` 增加"等待完成"变体或 `QualityGate` RPC 返回 pass/fail + 证据链接；③ 检查/契约 YAML 导出 + `apply`（幂等、dry-run）；④ GitHub Action 文档 | 6–10 |
| **P1-6** | **第三方结果摄取 + OpenLineage 质量 facet** | 客户已经跑 dbt/GX/Soda；摄取比自建连接器便宜，且能立刻扩覆盖面。**失败关联**（dbt/Airflow 失败 → 同一事故）比拓扑更值钱 | DataHub（dbt/GX/DMF/SQLMesh/ODCS）、OM（dbt/GX）、Bigeye（dbt）、MC 导入 dbt test | ① 新端点 `IngestQualityAssertions`（签名密钥、幂等 key、映射到目标/检查/执行/问题）；② 发出：把执行结果编码为 OpenLineage `dataQualityAssertions` facet（字段几乎 1:1），复用现有 OL 端点；③ dbt `run_results.json` 的失败与检查失败关联 | 6–10 |
| **P1-7** | **血缘影响面与根因** | 我们**已经有列级血缘**，这是最便宜的差异化；竞品里也只有 Databricks/Snowflake/MC 做好了 | Databricks 事故根因走血缘 + 影响 H/M/L、Snowflake 受影响资产、MC impact analysis + RCA、Sifflet impacted dashboards | ① 执行详情/问题页显示"下游受影响资产"（复用 lineage 查询，限制 N 层/M 个节点）；② 失败时在同一时间窗内查上游检查失败/上游 schema 变更，作为"根因候选"；③ 按消费关系算覆盖率（有下游的表必须有检查） | 5–8 |
| **P1-8** | **MCP 质量工具** | 我们已有 MCP 与读服务，暴露 DQ 几乎零成本，且是"agent 可运维"的差异化 | MC/Metaplane/Sifflet/Bigeye 都有 MCP server | 新增 4 个只读工具：`list_quality_issues`、`get_quality_target`、`list_quality_executions`、`explain_quality_execution`（复用权限与审计） | 2–3 |

### P2 — 规模化与治理（按客户/场景触发）

| # | 缺口 | 说明 | 估时 |
| --- | --- | --- | --- |
| **P2-1** | 多选批量操作 | 检查目录多选批量改 severity/dimension/owner/on_empty/阈值，逐项返回结果（形态同 `BatchCreateQualityRules`） | 3–5 |
| **P2-2** | 工单闭环 + 事故归并（政务/验收） | ① 问题 → 工单：派发 / 受理 / 处理（已修复/关闭/暂缓修复）/ **复测核验** / 自动关闭；责任人/责任机构维度 + 受理/处理时限 → 直接产出 DB3205/T 的 30 分工作质量指标（**国内云厂商都没有，是差异点**）；② check 之上的 **Incident 对象**：把同一目标/同一批次的多个失败检查归并成一个事故（严重级别、owner、活动流、TTA/TTR）——我们的 issue 是"发现台账"，竞品（MC/Bigeye/Sifflet/OM/DataHub）都有事故对象 | 12–18 |
| **P2-3** | 码表/校验位规则 + 模板 | 新检查类型"标准编码"（统一社会信用代码、组织机构代码、行政区划、行业分类、身份证号等：正则 + 校验位 + 码表存在性），与"由标准派生规则"配套；规则模板库做成内置 + 自定义两层（腾讯内置只读，我们私有化应可改） | 4–6 |
| **P2-4** | 问题协作 | issue 增加 assignee / 评论 / due；与 Webhook/Jira/ServiceNow 打通（复用 P0-1 的投递层） | 4–6 |
| **P2-5** | 跨实例/跨库对账 | 现在 `cross_table` 限同库；DB3205/T 附录 B 第 13 条要求与外部基础库**跨库比对**，MC Comparison monitors/Bigeye Deltas 也能跨源。放宽为"同实例跨库 + 显式配置的跨实例"（仍受连接预算约束） | 6–10 |
| **P2-6** | 检查类型补强 | ① `accepted_values` 支持**动态值集**（来自另一列或子查询；MC 支持 list/field/query 三种）；② 正常样本对比（good vs bad rows，Anomalo 的卖点）；③ 分片/维度异常追踪（"某个 segment 悄悄消失"） | 5–8 |
| **P2-7** | 质量分权重编辑 + 等级映射 | 在 P0-3 的分数之上加：维度权重配置、等级文案（优秀/良好/中等/及格/不及格）、**多套评分卡并存**（政务 70/30 与通用六维，投标友好） | 3–5 |
| **P2-8** | 多副本/HA | 与"单副本"接受决策冲突，只在企业客户明确要求时做（调度租约/分布式锁/计数外置） | 15–25 |


---

## 6. 设计得不够好的地方

> "缺失"是没做，"不够好"是做拧了。以下每条都能落到代码或文档，按用户可感知的伤害排序。

### 6.1 `row_count_change` 默认永远建不起基线（P0，功能级缺陷）

- **现象**：默认配置新建一条"行数变化"检查，第一次执行 `NOT_EVALUATED`，第二次、第三次……永远 `NOT_EVALUATED`。
- **根因**：基线只取该规则**最近一条** PASS/WARN/FAIL 执行的 `observed_value`（`backend/runner/qualityrunner/plan.go` 的 `baselineFromExecutions`），`NOT_EVALUATED` 被显式跳过；而无基线时又按 `on_empty`（默认 `NOT_EVALUATED`）判定。于是第一次的观测值即使已经写进执行行，也永远不会被当作基线。
- **证据**：`.agents/docs/data-quality.md` §7 #20 已记录该现象，但作为"已知缺口"而不是缺陷；`docs/data-quality.md` 未提示用户必须改 `on_empty`。
- **影响**：这是 18 类检查里唯一"默认不可用"的类型，而它是最常用的异常发现手段之一。
- **建议**：`row_count_change` 的基线改为"最近一条 COMPLETED 且 `total_rows > 0` 的执行（不论判定）"，并把 `on_empty` 默认改为 `PASS`（首次观测视为建立基线）；同时输出 `baseline_value`/`baseline_time` 到执行证据里，让用户看到"在和什么比"。进一步用滚动窗口（近 N 次中位数）替代单点基线，见 §5 的动态阈值。

### 6.2 自动 schema 变更监控在主力引擎上是空的（P0，优先级倒挂）

- **现象**：目标目录/工作台的 `schema_changed` 徽标只在 StarRocks/Doris 上可能为真；MySQL/MariaDB/TiDB/OceanBase/PG/MSSQL 恒为 false（`.agents/docs/data-quality.md` §7 #19）。
- **根因**：`schema_hash` 来自 `meta_registry_resource_schema`，而该表只由实现 `db.ObjectDefinitionReader` 的驱动写入，当前只有 `backend/plugin/db/starrocks/definition.go`。这三个主力引擎不取 DDL，于是没有 hash。
- **影响**：竞品把"表结构变了"当作 DQ 的默认告警之一；我们却只在最少用的两个引擎上有这个信号，且 `schema_drift` 规则需要用户逐检查保存快照。
- **建议**：在 schema sync 阶段用**已有列元数据**计算 schema 指纹（列名 + 类型 + 可空 + 顺序 + 索引），对所有引擎写入 `meta_registry_resource_schema`（或新表 `meta_registry_schema_fingerprint`），不依赖 DDL 抓取；再据此驱动 `schema_changed`、`schema_drift` 的自动快照、以及"结构变更"通知。

### 6.3 "契约"缺消费者，而不是缺门禁（P1）

- **现象**：契约有设置、有发布、有版本、有 diff，但没有任何外部消费者——没有文件格式、没有 `apply`/dry-run、没有 CLI、没有退出码（YAML 导出还排在 P2）。
- **先纠正一个常见误解**：**没有厂商把"发布契约"做成运行时门禁**。dbt 的 model contract 是在 build 前校验列名/类型（shape，不是数据）；Soda 的契约靠 CI 里的 scan 退出码；MC/IBM 把契约当资产类型或 monitor 标签。所以"发布不阻塞执行"（I-40）本身是**市场一致的**，不是缺陷。
- **真正的问题**：我们的契约只活在 UI/DB 里，**无法进入版本库与流水线**。用户按"契约"二字期待的是"承诺可被校验"，实际拿到的是"打了一个快照"。
- **建议**：补消费者而不是改语义——① 检查/契约 YAML 导出 + `apply`（幂等 + dry-run）；② `mxd quality gate --target … --fail-on fail` 或 `QualityGate` RPC（等待 + 退出码 + 证据链接）；③ 在 UI 上把"发布 = 快照承诺，门禁在 CI"写清楚。这样契约从"标签"变成"可在 PR 里被破坏的东西"，同时不违背执行模型。


### 6.4 目标级调度一刀切（P1，取舍可再议）

- **现象**：调度只能挂在目标上，一张表的所有检查同一节奏，15 分钟–7 天，没有 cron、没有时区、没有"按检查覆盖"。
- **代价**：`docs/data-quality.md` 的 Troubleshooting 里有一条就是"我不能改单条检查的调度"——这是用户会撞到的显式限制。对一张大表：便宜检查（行数）想天天跑，贵的检查（跨表对账）想每周跑，只能拆表或接受浪费。
- **判断**：当初的理由（一张表不能被检查们拉扯）成立，但解决方式过粗。Soda 用 `scopes`（命名切片 + 独立 filter/属性/cron）解决同一问题；Monte Carlo 是每 monitor 独立节奏。
- **建议**：保留目标级默认节奏，增加**检查频率档（每小时/每天/每周，只允许更慢、不允许更密）**：`next_run_at` 仍按目标算，目标到期时按每检查的档位过滤（见 §5 P1-4）。这样不破坏"目标一次扫描"的批次语义，也解决贵检查浪费；**不要**引入 per-check cron。

### 6.5 CEL 被当成"行级断言"，但它是采样的（P1）

- **现象**：`cel_expression` 从目标表取 `LIMIT sample_rows`（默认 1 万、上限 10 万）到 Go 里逐行求值，执行结果标 `sampled=true`，阈值仍按 PERCENT 判定。
- **风险**：用户会把它当"逐行业务规则校验"，但它在 1 亿行表上只看了 1 万行；PERCENT 阈值作用在样本上，与全表百分比不是一回事。竞品里"行级/表级断言"通常意味着**下推 SQL 全量计算**。
- **建议**：能下推的表达式编译成 `WHERE NOT (expr)` 的 COUNT（精确、与其它检查同语义）；不能下推的保留 Go 求值，但在 UI 上明确"抽样判定"并**禁止** PERCENT 阈值（只允许样本内计数阈值），避免"看着像全量"。

### 6.6 失败只通知一次，之后沉默（P1）

- **现象**：通知只在状态迁移时发（打开/恢复），同一问题 1h 内去重；问题持续失败 3 周也只有 1 条站内信。执行失败（`ERROR`）只发 admins；静音只在规则/问题级。
- **问题**：真实运维需要"还没修好"的持续可见性——日报/周报、升级（N 小时未处理 → 升级）、维护窗口（发版期间静音）。竞品的标准做法是 **alert → escalation policy → digest**。
- **建议**：① 日报/周报 digest（按 owner/团队聚合未解决问题与失败检查数）；② 升级规则（severity + 未确认时长）；③ 工作区级维护窗口；④ 长期失败问题在概览"需关注"里持续置顶（现在概览有，但不提醒）。

### 6.7 画像的保留期与同步执行，让画像无法当基线（P2）

- **现象**：画像同步在 API 请求里跑（120s 上限、≤50 列），保留 **7 天**；L3 标注要求画像 ≤7 天。
- **矛盾**：画像最有价值的用法是"长期基线/历史对比"（Deequ 的 metrics repository、GX 的 profiling 历史），7 天后就没了，等于只能当"一次性快照"；而同步执行让它无法在大表上定时跑。
- **建议**：画像改后台任务（复用 `qualitysuggestionrunner` 的形态），保留期延长到 90 天（与执行一致），并按"表"存储历史点，供阈值建议、趋势、分布漂移使用。这一步同时是 §5 动态阈值的数据地基。

### 6.8 死配置与遗留物（P2，卫生问题）

| 项 | 位置 | 问题 |
| --- | --- | --- |
| `send_sample_values_to_llm` | `DataQualitySetting` 字段 9 | 设置页可见可存，**无消费者**（docs §7 #3） |
| `QUOTA` 错误码 | `dataquality/errors.go` 注释 | 执行侧不再产生，前端仍本地化，读代码/看数据都会困惑 |
| `daily_execution_limit` 触顶 | runner | 只跳过一次扫描 + 日志，**不告警**：监控自己停了，用户不知道（§7 #1） |
| `default_schedule_interval` | 设置页 | 只影响新目标初始化，容易被理解为"全局节奏" |

### 6.9 检查目录只读，批量操作缺失（P2）

- **现象**：`/data-quality/checks` 明确只读，创建/编辑只能在目标工作台；批量只有"批量建议决策""批量创建规则""按范围 Run now"。
- **代价**：1000 条规则的存量治理做不了——批量改严重级别/改阈值/暂停一批/改 owner/改 on_empty，都要逐条进工作台。
- **建议**：目录上加多选 + 批量改 `severity/dimension/owner/on_empty/threshold`（走 `update_mask`，逐条校验，结果按项返回，形态与 `BatchCreateQualityRules` 一致）；这不破坏"创建在目标内"的设计。

### 6.10 问题台账的协作能力不足（P2）

- **现状**：`QualityIssue` 有 status/severity/first-last seen/occurrence/ack/resolution/mute，**没有** assignee、评论、due/SLA、关联工单、影响面。
- **问题**：owner 只能来自规则（想换值班人得改每条规则）；"我确认了但不是我修"没有地方写；验收要的"整改闭环"拿不出记录。
- **建议**：issue 增加 `assignee` + 评论流（复用现有审计/权限模型）+ 可选 `due_at`；这比自己造工单系统便宜，且能直接对接 Jira/ServiceNow 的 webhook（与 §5 的告警渠道同一套投递层）。

### 6.11 概览的"受监控表数"是死指标（P2）

- **现状**：`QualityOverview` 只有 `rule_count`、`monitored_table_count`、打开问题按严重级别、24h 计数、7 天（可请求至 30 天）趋势。没有分母（工作区总表数）、没有按实例/环境/负责人/维度拆分。
- **问题**：竞品普遍强调 **coverage**（"你监控了 12% 的表"），因为它直接回答"我该信任多少数据"。只有分子没有分母，读者无法判断工作做得好不好。
- **建议**：`monitored_table_count / total_table_count`、`covered_columns / total_columns`、按实例/环境/owner 的分解；目标目录已可按 worst_status 过滤，概览加一个"未监控的高价值表"入口（结合血缘/下游消费数）就形成闭环。

---

## 7. 优势与差异化（建议放大）

| 优势 | 现状 | 怎么放大 |
| --- | --- | --- |
| 规则引擎的"可执行性保证" | 能力矩阵 + 保存期拒绝 + 方言 golden + preview | 把"保存前就知道能不能跑"作为卖点写进文档与 UI；对比 GX/Soda 的"运行时才发现表达式不被引擎支持" |
| **竞品明确没有的检查语义**（调研确认） | `cel_expression`（**没有任何厂商文档化行级表达式**）、`string_length`、`on_empty`（空数据语义）、不可变 revision、契约指纹快照（fingerprint + added/modified/removed） | 写成对照页：这五项是"确定性判定"的具体证据，不是功能堆料 |
| 执行/调度模型 | 一目标一批次 + 连接预算 + 周期重试 + 1h 去重 + 风暴抑制 | 竞品是"一检查一任务/一查询"，成本更高、告警更吵；把"批次"和"降噪"从实现细节提升为卖点 |
| 目标中心 + 契约 | 已实现（`data-quality-target-centric.md`） | 继续把健康度/覆盖率做到目标行上；契约接 YAML/CI 后成为"承诺"而非"标签" |
| 血缘原生 | 有表级/列级血缘、OpenLineage 摄取，但 DQ 未使用 | **blast radius**（失败检查 → 下游资产列表）、**根因候选**（同窗口失败的上游检查）、**按血缘推荐检查**（source 表严、派生表对账）、**基于消费的覆盖率**（有下游的表必须有检查）。调研显示**没有厂商向消费者展示"下游正在读一张失败的表"**，这是空位 |
| OpenLineage | 只摄取 | 以 `DataQualityAssertions` / `DataQualityMetrics` facet 发出执行结果（字段与我们的结果模型几乎 1:1）；反向摄取外部断言（GX/Soda/dbt）进同一问题台账 |
| 自托管 + 单二进制 + 确定性 | 已实现 | 竞品里只有 Collibra（自托管）、Ataccama（on-prem/edge）、IBM（on-prem）能在客户机房跑，四个纯 SaaS（MC/Anomalo/Bigeye/Metaplane）都不能；**"数据不出域 + 按表不额外收费"要主动写成对比页**，否则客户不知道 |
| LLM 安全边界 | 不发样本行、只产 PENDING、注入中和、诊断完备 | 把它变成"AI 可审计"的差异化：每次建议可追溯到 prompt 版本/模型/token/关联 id；这是 SaaS 产品也不一定做全的 |
| 平价的覆盖率 | 自托管、无按表计费 | 竞品的覆盖率看板部分是为了证明计费（MC 文档自己说监控标记同时用于计费）；**"覆盖率不收费"是结构优势** |

**低成本立即改善（不改架构，1–3 天内可做）：**

1. 执行详情把已有的 `sampled` / `truncated` / `attempt` 三个标志做成显眼徽标——数据已经在，只是没展示。
2. `row_count_change` 表单默认 `on_empty = PASS`（首次执行即建立基线），并在表单里写一句"首次运行只记录基线，不判定"。
3. 设置页隐藏或标注 `send_sample_values_to_llm`（当前无消费者）；清理 `QUOTA` 遗留错误码文案。
4. 目标目录行加"字段覆盖 X/Y"列（`TargetCoverageCard` 的逻辑已存在，搬到目录）。
5. 概览加一个只读入口"有下游但未监控的表"（用血缘下游数排前 20，不做评分），立刻能回答"还该监控谁"。
6. 检查目录支持行内"运行/暂停"（不新增编辑入口），减少进出工作台的次数。
7. LLM 解释/建议里引用被执行观测值、阈值与 revision，让"AI 说了什么"可追溯到证据。
8. 文档/官网补一页"与 GX/Soda/MC 的差异"：保存期可执行性校验、行级表达式（CEL，标注采样语义）、`on_empty`、不发原始样本行、单二进制自托管、覆盖率不按表计费——**我们最强的几条现在只存在于代码里**。

---

## 8. 路线图建议

三个 release，按"先能被运维使用，再让监控有用，最后接进生态"排序。工作量合计约 **75–105 人日**（P0+P1），P2 按客户触发；如果只为"能卖 / 能过验收"，R1 + 工单闭环（约 40–55 人日）是最小集。

### R1（约 5–7 周）：让 DQ 能被运维和汇报

- P0-1 告警送达与路由（邮件 + Webhook + 钉钉/企微/飞书 + 失败重试 + 升级）
- P0-2 报表与导出（CSV + 单目标自包含 HTML 报告 + 周报 digest）
- P0-3/P0-4 覆盖率/健康度/维度汇总 + 资产页徽标
- 顺手修 §6.1 `row_count_change` 基线缺陷、§6.8 死配置清理

**验收**：把一台测试环境的所有检查失败 → 值班人在邮件/IM 里收到、点开能看到趋势与失败样本、周报能发给领导、在元数据里一眼看到哪张表有问题。

### R2（约 6–8 周）：让监控"自己会看"

- P1-0 接入即推荐检查（确定性 profile→建议，落 PENDING 队列）
- P1-1 观测趋势 + 经验阈值建议
- P1-2 全引擎 schema 变更监控
- P1-3 执行语义（宏变量 / 分区 / 抽样 / 超时覆盖）
- P1-4 目标级 cron/时区 + 检查频率档 + 维护窗口

**验收**：接上一张新表就能在建议队列里看到 5–10 条待审检查（而不是从 18 类里手挑）；分区表能只查最新分区；大表能抽样；用户点"参考历史值"就能得到合理阈值；MySQL/PG 表结构变了会在 15 分钟内被发现。

### R3（约 7–9 周）：接进生态，做差异化

- P1-5 CI 门禁 + 检查/契约 YAML 代码化（`mxd quality`）
- P1-6 外部断言摄取 + OpenLineage 质量 facet 发出
- P1-7 血缘影响面与根因候选
- P1-8 MCP 质量工具

**验收**：dbt/GX 的结果能进同一问题台账；PR 里能挡住"会让检查失败"的变更；失败检查能回答"哪些下游资产受影响"。

### 与验收场景并行（有政务/大客户时）

- P2-2 工单闭环 + 事故归并 + P2-7 评分卡多套权重 + P0-2 报告章节对齐 DB3205/T 附录 C
- P2-3 码表/校验位规则（统一社会信用代码等）
- 这两块**不要等产品化完美**：先按一个标准（DB3205/T 或客户的地方标准）把"评分公式 + 报告模板 + 工单状态机"跑通，再抽象。**这是国内项目里 30 分的硬指标，也是云厂商全空的差异点。**

---

## 9. 不建议抄的（避免形式主义）

| 能力 | 为什么不做/缓做 | 证据 / 备注 |
| --- | --- | --- |
| 通用 Python/任意代码检查沙箱 | GX/Soda 的复杂度与运维成本主要来自这里；我们的 `custom_sql` + `cel_expression` + 18 类已覆盖绝大多数需求；引入运行时会把单二进制变成"要管解释器"的产品 | GX 需要 Python 环境；Soda 4.x 许可证变更（Apache-2.0 → Proprietary） |
| "AI 自动建规则并启用" | 会立刻产生误报与信任崩塌；我们"只产 PENDING 建议"是对的（D-30/I-12） | Deequ 的 constraint suggestion 会一次吐几百条需要人工剪裁；GX Data Assistant 已被移除 |
| **任意层级、任意精度的权重编辑器** | 权重是政治问题、口径难统一；DB3205/T 只给整数权重且注明"可调整"，DataArts 的评分文档自身都前后矛盾（文字说加权、公式是简单平均） | 先做**一个公式、公开可审计**的简单分数（P0-3），权重可配但不做复杂编辑器（P2-7） |
| **"全量 ML 异常检测"当主卖点** | 泛洪是这门生意的核心难题；市场实际只覆盖少数指标（Snowflake 2 个、Databricks 完整性/新鲜度、DataHub 3 类 GA） | 只对行数/新鲜度/null 率做"可解释期望区间"，并把结果作为**疑似问题**进问题台账等人工确认 |
| **强规则阻塞下游调度** | 我们是旁路监测架构（只读数据源 + 独立调度）；DB3205/T 5.1 也明确要求"避免对业务系统产生影响"。CI 门禁（P1-5）是对的替代：在发布前拦，而不是在产出后堵 | DataWorks/DataArts 的强规则阻塞属于数据中台语境，且需要与调度系统同体 |
| **自建通用 BPM 工单/审批流** | 我们只需要一条主线：派发 → 受理 → 修复/关闭/暂缓 → 复测 → 自动关闭 + 时限；引入会签/多级审批会让 4 个工作质量指标算不清 | DB3205/T 也是这条单线 |
| **为"全量/定量/抽样"建三套执行引擎** | 三者本质是同一条 SQL 的不同 WHERE 与采样子句 | 做成执行参数（范围 + 采样率）并在报告里回填声明即可 |
| 花大力气做"可访问性""问题四分类"等弱指标 | 可访问性权重 5、规则 1 条，实操几乎总是满分；信息/流程/技术/人员四分类是报告里的**可选**项，容易退化成填表 | DB3205/T C.1 把"问题分类"列为可选 |
| 静态报告当主界面 | 报告无法 ACK/指派/重跑，只能当导出物 | GX Data Docs、Elementary `edr report` 都是静态 HTML；它们也都把主界面放进 Cloud |
| 改名/删概念式的"重构" | 每次改名都会让客户脚本与文档失效 | GX 0.18→1.0 删掉 profiler/batch request/template，社区反弹 |
| 依赖 JVM/Spark/Python 旁路做统计 | 小表检查不该付 Spark 启动成本 | Deequ 必须 Java 11 + Spark；我们"单 Go 二进制 + Postgres"是优势 |
| **无审计的自动调阈值** | MC 在异常复发时**悄悄放宽阈值**，Bigeye 关闭问题时"适配阈值"；这与我们的不可变 revision 治理立场直接冲突（等于让系统自己改判定标准） | 若要自动调，只能**产出待审的 revision 建议**，由人确认后生效 |
| **抑制后回溯改历史分** | Collibra 的 suppress 会重算抑制日期起的历史评分 | 我们的不可变修订立场应延伸到评分：**向后抑制，绝不回溯** |
| **按"任务数/表数"算覆盖率** | Collibra 把"一次作业覆盖整张表"当覆盖率，与真正检查了多少列无关；MC 明确不测列级覆盖率 | 我们按**检查/列粒度**定义覆盖率，这本身就是一个可辩护的改进点 |
| **每个检查一条 cron** | 市面上普遍 per-monitor cron，结果批次碎片化、查询成本上升 | 保留目标级批次 + 检查"频率档"（每小时/每天/每周），不暴露 per-check cron |
| **国家级证件号格式的长尾** | MC 内置 CA SIN/FR INSEE/PL PESEL/US SSN、Bigeye 内置 SEDOL/CUSIP/LEI/FIGI/ISIN；维护成本高、使用率低、格式变更即负债 | 只做"码表 + 校验位 + regex"的少量高频项（统一社会信用代码/身份证/组织机构代码/行政区划），其余给 `regex` 逃生口 |
| **"路线图当功能"式营销** | Anomalo 官网 9 个 agent 里 5 个标 coming soon | 只列已交付能力；我们的诊断/审计是实测可查的，不必学这个 |
| **用告警路由矩阵掩盖告警量** | MC 的 audience × priority（含 "Not triaged" 桶）是对自家 ML 产生噪声的补偿 | 从源头降噪（去重/归并/`on_empty`/风暴抑制），而不是建路由 DSL |
| **"数据停机时间"当内部 KPI** | MC 的 `incidents × (MTTD + MTTR)` 在幻灯片上很好，作为内部 KPI 会奖励"不报事故" | 如果展示，必须与覆盖率同时展示，防止通过降低发现率刷分 |

---

## 10. 附录

### 10.1 代码位置索引

| 主题 | 位置 |
| --- | --- |
| 规则类型单一真相 | `backend/component/dataquality/rule_type_spec.go`（`ruleTypeSpecs`） |
| 引擎能力矩阵 | `backend/component/dataquality/capabilities.go` + `capabilities_test.go` |
| 判定 | `backend/component/dataquality/evaluate.go` |
| 基线（`row_count_change`） | `backend/runner/qualityrunner/plan.go`（`baselineFromExecutions`、`baselineIsRecent`） |
| 调度常量 | `backend/component/dataquality/settings.go`（`Default/Min/MaxScheduleInterval`） |
| 通知去重 | `backend/component/notification/data_quality.go`（`QualityNotificationWindow = 1h`） |
| 通知投递（仅站内） | `backend/component/notification/{service,hub}.go` |
| 画像超时 | `backend/api/v1/data_quality_service.go:55`（`qualityProfileQueryTimeout = 120s`） |
| schema hash 写入 | `backend/runner/schemasync/object_definition.go`；驱动侧仅 `backend/plugin/db/starrocks/definition.go` |
| 批次/目标读模型 | `backend/store/quality_run.go`、`backend/store/quality_target.go` |
| 前端页面/组件 | `frontend/src/pages/data-quality/`、`frontend/src/components/data-quality/` |

### 10.2 调研方法与局限

- 竞品以**官方文档**为主（`docs.*` / `learn.microsoft.com` / `help.aliyun.com` / `support.huaweicloud.com`），营销页只在标注「宣传口径」时使用；读不到细节的写「未证实」，不做推断。
- 四个子调研分别产出：`01-oss-codefirst.md`（398 行）、`02-commercial-observability.md`（375 行，每条断言标 [D] 文档/[M] 营销/[T] 第三方）、`03-catalog-warehouse-native.md`（698 行）、`04-cn-market-and-standards.md`（580 行）。
- **Anomalo 没有公开文档**（其 FAQ 明确说文档只给客户/试用方），因此 §3.2 中 Anomalo 一列只能作为方向性判断，"40+ 无代码检查"属营销口径。
- **Qualifire 是错位的竞品**：商业 Qualifire（qualifire.ai）是 AI agent 的 guardrail/eval 产品；作为 DQ 参考的是 Oracle AIDP 的示例代码库（本文按后者引用）。
- DB3205/T 是**报批稿**（标准号占位 `DB3205/T XXX—XXXX`），正式发布版条款号可能微调；其 C.2 公式为根据文字描述重构。GB/T 36344—2018 **只给六维定义，没有权重/公式**，未取到正文，仅经 DB3205/T 转引。
- 竞品的规则/限额/价格数量随版本变化，均以 2026-10-31 的文档为准；价格类数字来自第三方，仅供量级参考。
- 本次未做**用户访谈/真实试用**，因此"竞品体验更好/更差"的判断来自文档与产品结构，而非亲自操作。

### 10.3 来源

**开源 / 代码优先**
- Great Expectations 概览 · <https://docs.greatexpectations.io/docs/core/introduction/gx_overview>
- GX Checkpoint Actions（Slack/邮件/Teams + `notify_on`） · <https://docs.greatexpectations.io/docs/core/trigger_actions_based_on_results/create_a_checkpoint_with_actions>
- GX 0.18→1.0 迁移（删除 profilers/Data Assistant） · <https://docs.greatexpectations.io/docs/0.18/reference/learn/migration_guide>
- GX PyPI（Apache-2.0，1.24） · <https://pypi.org/project/great-expectations/>
- Soda SodaCL metrics & checks · <https://docs.soda.io/soda-documentation/soda-v3/sodacl-reference/metrics-and-checks>
- Soda 异常检测（Prophet，已废弃） · <https://docs.soda.io/soda-documentation/soda-v3/sodacl-reference/anomaly-detection>
- Soda 数据契约（YAML，programmatic only） · <https://docs.soda.io/soda-documentation/soda-v3/data-contracts>
- Soda 集成（Slack/Teams/Jira/ServiceNow/PagerDuty/Webhook） · <https://docs.soda.io/soda-documentation/soda-v3/integrate-soda>
- soda-core PyPI（4.x = Proprietary） · <https://pypi.org/project/soda-core/>
- dbt data tests · <https://docs.getdbt.com/docs/build/data-tests>；model contracts · <https://docs.getdbt.com/docs/collaborate/govern/model-contracts>
- dbt-expectations（62 宏，"no longer actively supported"） · <https://github.com/calogica/dbt-expectations>
- Elementary 异常检测（Z-score + 季节性） · <https://docs.elementary-data.com/data-tests/data-anomaly-detection>；`edr report` · <https://docs.elementary-data.com/oss/guides/generate-report-ui>
- Deequ（MetricsRepository / 异常策略 / 约束建议） · <https://github.com/awslabs/deequ>
- Pandera · <https://github.com/unionai-oss/pandera>；data-diff（2024-05 停止维护） · <https://github.com/datafold/data-diff>
- Qualifire（Oracle AIDP 示例） · <https://github.com/oracle-samples/oracle-aidp-samples/blob/main/data-engineering/data_quality/README.md>
- DataKitchen TestGen（自托管 + profile→生成测试 + MCP） · <https://github.com/DataKitchen/dataops-testgen>
- OpenLineage 质量 facet · <https://openlineage.io/docs/spec/facets/dataset-facets/data_quality_assertions> · <https://openlineage.io/docs/spec/facets/dataset-facets/data_quality_metrics>

**目录 / 数仓原生**（完整列表见 `temp/dq-research/03-catalog-warehouse-native.md` §12）
- OpenMetadata 质量概览 / 测试定义 / 自定义测试 / 告警 · <https://docs.open-metadata.org/v2.0.x/how-to-guides/data-quality-observability/quality> · <https://docs.open-metadata.org/v2.0.x/how-to-guides/data-quality-observability/quality/custom-tests> · <https://docs.open-metadata.org/v2.0.x/how-to-guides/data-quality-observability/alerts-notifications>
- DataHub Assertions（类型 × 能力矩阵） · <https://docs.datahub.com/docs/managed-datahub/observe/assertions>；Anomaly detection · <https://docs.datahub.com/docs/managed-datahub/observe/anomaly-detection>；Data Health Dashboard · <https://docs.datahub.com/docs/managed-datahub/observe/data-health-dashboard>
- Databricks 异常检测（健康指示、事故、血缘根因、intelligent scanning） · <https://learn.microsoft.com/en-us/azure/databricks/data-governance/unity-catalog/data-quality-monitoring/anomaly-detection>；漂移指标（chi²/KS/PSI…） · <https://learn.microsoft.com/en-us/azure/databricks/data-governance/unity-catalog/data-quality-monitoring/data-profiling/monitor-output>
- Snowflake DQ 简介 / 系统 DMF 185+ / 异常检测 / 看板（健康表%、覆盖率%） · <https://docs.snowflake.com/en/user-guide/data-quality-intro> · <https://docs.snowflake.com/en/user-guide/data-quality-system-dmfs> · <https://docs.snowflake.com/en/user-guide/data-quality-anomaly> · <https://docs.snowflake.com/en/user-guide/data-quality-centralized-dashboard>
- AWS Glue DQDL 规则类型（33）/ 异常检测（≥3 点）/ DQ score · <https://docs.aws.amazon.com/glue/latest/dg/dqdl-rule-types.html> · <https://docs.aws.amazon.com/glue/latest/dg/data-quality-anomaly-detection.html> · <https://docs.aws.amazon.com/glue/latest/dg/glue-data-quality.html>
- Dataplex auto data quality（scorecard aspect）/ Purview DQ · <https://docs.cloud.google.com/knowledge-catalog/docs/auto-data-quality-overview> · <https://learn.microsoft.com/en-us/purview/unified-catalog-data-quality>

**商业可观测**（完整条目见 `temp/dq-research/02-commercial-observability.md` §12）
- Monte Carlo monitors 总览 · <https://docs.getmontecarlo.com/docs/monitors-overview>；异常检测 · <https://docs.getmontecarlo.com/docs/anomaly-detection-overview>；通知（9 渠道 + audiences） · <https://docs.getmontecarlo.com/docs/notifications-v2>；SLO · <https://docs.getmontecarlo.com/docs/service-level-objectives-slos>；Circuit Breakers · <https://docs.getmontecarlo.com/docs/circuit-breakers>；监控覆盖率看板 · <https://docs.getmontecarlo.com/docs/composable-dashboards-example-monitoring-coverage>；Monitors as Code · <https://docs.getmontecarlo.com/docs/mcu-monitors-as-code>
- Bigeye 监控（Metrics/Autothresholds/Deltas/Join Rules） · <https://docs.bigeye.com/docs/metrics> · <https://docs.bigeye.com/docs/autothresholds>；Scorecard · <https://docs.bigeye.com/docs/scorecard>；事故与通知 · <https://docs.bigeye.com/docs/incidents> · <https://docs.bigeye.com/docs/collaboration-tools>；bigConfig · <https://docs.bigeye.com/docs/bigconfig>
- Sifflet monitor 模板（含 ML 阈值/反馈闭环/特殊日期） · <https://docs.siffletdata.com/docs/monitor-templates> · <https://docs.siffletdata.com/docs/threshold>；事故自动分组 · <https://docs.siffletdata.com/docs/incident-auto-grouping-v2>；通知渠道 · <https://docs.siffletdata.com/docs/alerts-destinations>；Monitors as Code / Flow Stopper · <https://docs.siffletdata.com/docs/monitors-as-code> · <https://docs.siffletdata.com/docs/flow-stopper-integration>
- Metaplane monitor 类型 / 告警路由 / 事故 / CI·PR 预览 / Rules · <https://docs.metaplane.dev/docs/monitor-types> · <https://docs.metaplane.dev/docs/alert-routing> · <https://docs.metaplane.dev/docs/incidents> · <https://docs.metaplane.dev/docs/data-ci-cd> · <https://docs.metaplane.dev/docs/rules>
- Anomalo（**无公开文档**，仅方向性） · <https://docs.anomalo.com/>

**国内云与标准**
- 阿里云 DataWorks 数据质量 · <https://help.aliyun.com/zh/dataworks/user-guide/data-quality>
- 华为云 DataArts Studio 数据质量 · <https://support.huaweicloud.com/usermanual-dataartsstudio/dataartsstudio_01_0709.html> · 质量报告 · <https://support.huaweicloud.com/usermanual-dataartsstudio/dataartsstudio_01_4505.html>
- 腾讯云 WeData 数据质量 · <https://cloud.tencent.cn/document/product/1267/72261>
- 苏州市地方标准 DB3205/T《数字政府 市场监管数据质量监测规范》（报批稿，2024，本地正文 `temp/dq-research/sz.txt`）
- GB/T 36344—2018《信息技术 数据质量评价指标》（经 DB3205/T 转引；**只有六维定义，无评分算法**）

### 10.4 调研原始笔记

- `temp/dq-research/01-oss-codefirst.md` — 开源/代码优先（12 行 × 9 产品对照表 + 逐产品定位 + 来源）
- `temp/dq-research/02-commercial-observability.md` — 商业可观测性（12 节，逐条标注 [D]/[M]/[T]，18 行检查类型 crosswalk，陷阱清单）
- `temp/dq-research/03-catalog-warehouse-native.md` — 目录与数仓原生（698 行，逐平台 + 12 行对照表 + 缺口映射 + 来源）
- `temp/dq-research/04-cn-market-and-standards.md` — 国内标准与云厂商（DB3205/T 指标/权重/公式/流程/报告 + DataWorks/DataArts/WeData/亿信）
- `temp/dq-research/sz.txt`、`sz.docx` — DB3205/T 正文提取与原件
- `temp/dq-research/raw/` — 抓取到的官方文档原文片段（OpenMetadata / DataHub / Databricks / Snowflake / Glue / Dataplex / Purview / Atlan / Select Star）


