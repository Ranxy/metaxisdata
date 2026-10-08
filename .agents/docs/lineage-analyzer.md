# 血缘分析器 — Reference

> Status: **implemented**。Maintenance reference for `backend/plugin/lineage/`、`backend/runner/lineageanalyzer/`、`backend/api/v1/lineage_service_analyze.go`。
> Related: [lineage-semantics.md](lineage-semantics.md)(各引擎语义决策与 transformation 模型)、[lineage-graph.md](lineage-graph.md)(图上字段级血缘)、[omni-upstream-defects.md](omni-upstream-defects.md)(上游缺陷与不修改政策)、[backend/plugin/lineage/AGENTS.md](../../backend/plugin/lineage/AGENTS.md)(改代码的规则)。

## What it is

从 SQL 定义推导表级 / 字段级血缘的三段链路,外加一条 OpenLineage 摄入路径:

- **分析器包** `backend/plugin/lineage/`:把一条 SQL 解析成 `[]model.ColumnRelation`。
- **runner** `backend/runner/lineageanalyzer/`:周期 + 事件驱动地取 VIEW / MATERIALIZED_VIEW / MANUAL_SQL 的定义,调用分析器,把边写进 `column_lineage`。
- **API** `backend/api/v1/lineage_service_analyze.go`:无状态 `AnalyzeSQL`,同一套分析与 GUID 补全逻辑,供图页面/CLI 手工分析。

契约是 `lineage.Analyzer.Analyze(ctx, engine, sql) ([]model.ColumnRelation, error)`(`backend/plugin/lineage/lineage.go:118`)。`sql` 可以是多语句脚本:根包在方言之上切分,逐条交给**单语句**方言分析器(切分循环 `lineage.go:126-141`;方言契约见 `lineage.go:56-60` 的 `AnalyzeRelationFunc` 注释),因此跨语句不共享 scope。

结果有三档,这是整个子系统的核心区分:

| 档 | 触发 | 边 | 错误 | 消费者动作 |
| --- | --- | --- | --- | --- |
| 完整 | 全部语句被建模 | 全部 | nil | 存边 |
| 部分分析 | 某语句/子句只是**没被建模** | 已解析的边保留 | `*lineage.UnsupportedStatementError{Diagnostics, Omitted}` | 存边 + 缺口写进 version 行 / RPC diagnostics |
| 硬失败 | 语句**解析不了** | 无 | 普通 error | 清掉该对象已有血缘 + `error_message` |

## 包结构与装配

装配是**编译期列表 + 值传递**,没有任何 `init()` 自注册、没有包级注册表。方言导出 `Registration()`,新包 `engines` 在编译期列全五个方言,server 构造一个 `*lineage.Analyzer` 并显式注入每个消费者。

| 文件 | 角色 |
| --- | --- |
| `backend/plugin/lineage/lineage.go:90` | `NewAnalyzer(cat catalog.Provide, registrations ...EngineRegistration) *Analyzer`;重复 engine panic |
| `backend/plugin/lineage/lineage.go:76-83` | `Analyzer{catalog, engines map[storepb.Engine]engineAnalyzer}` —— 值是调用方拥有的,不是包状态 |
| `backend/plugin/lineage/lineage.go:16` | `ErrorEngineNotSupported` |
| `backend/plugin/lineage/engines/engines.go:26-33` | `Registrations()` 列出 MySQL / TiDB / MariaDB / PostgreSQL / StarRocks |
| `backend/server/server.go:107` | `lineageEngines := lineage.NewAnalyzer(catalog.NewCatalogProvide(stores), engines.Registrations()...)` |
| `backend/server/server.go:109,121,147` | 同一个值交给 `lineageanalyzer.NewAnalyzer`、OpenLineage processor、gRPC routers(LineageService / ExplainSQLService) |
| `backend/plugin/lineage/catalog/provide.go:15,23,29,77` | `AnalysisContext{InstanceID, Database, Schema}` + `WithAnalysisContext`/`GetAnalysisContext`;`GetTable` 用 `Complete` 补全未限定名 |
| `backend/server/ultimate.go` | 只 blank-import 驱动与 schema 插件;**已无** lineage blank import |

OceanBase 与 Doris 刻意不注册(`engines.go:10-11`),`Analyze` 对它们返回 `ErrorEngineNotSupported`(`lineage.go:121`)。

## 运行生命周期与变更检测

| 机制 | 代码 | 行为 |
| --- | --- | --- |
| 周期全扫 | `backend/runner/lineageanalyzer/analyzer.go:26,124` | `lineageAnalysisInterval = 1h`,启动时先扫一次 |
| 队列消费 | `analyzer.go:27,138` | `analyzeCheckerInterval = 10s`,并发池 `MaxGoroutines = 10` |
| 事件驱动 | `analyzer.go:84`;`backend/runner/schemasync/syncer.go:417-422,783-787` | schema sync 提交**之后**把变更的 VIEW / MV 入队;MANUAL_SQL 由 `backend/api/v1/database_manual_sql.go:72,269` 入队 |
| 变更检测 | `analyzer.go:156-180` | 比较 `meta_registry_resource.metahash` 与 `column_lineage_version.meta_hash`;相同则跳过 |
| 分析写入 | `analyzer.go:238-417` | 包成 `CREATE VIEW name AS <definition>` → `Analyze` → 过滤 `IsTemp` → `BatchReplaceColumnLineage` → `UpsertColumnLineageVersion` |
| 失败(解析) | `analyzer.go:513-519` | `markAnalysisFailed`:清空该对象血缘 + 写 `error_message`,并**存当前 meta_hash**(否则小时扫描会反复重排) |
| 失败(瞬时) | `analyzer.go:63-73,93-112` | 有界退避重试 30s / 2m / 5m,最多 3 次,之后等下一轮全扫 |
| 引擎不支持 | `analyzer.go:296-301` | 记 `markAnalyzed` + "engine %s has no lineage analyzer; analysis skipped",不清边 |
| 删除清理 | `syncer.go:1076-1090`,调用点 `:757-762` | 删除对象时按 `meta_guid`、`source_guid`、`target_guid` 三向清 `column_lineage`,并清 `column_lineage_version` |
| MANUAL_SQL 特例 | `analyzer.go:334-370` | 除真实 target 边外,额外写一条以自身为源、目标同名的边,让"这段 SQL 输出什么"在图上可见 |

store 侧:`backend/store/column_lineage.go` 的 `BatchReplaceColumnLineage:64`、`ListColumnLineage:252`、`UpsertColumnLineageVersion:383`、`ListColumnLineageVersions:399`、`GetColumnLineageVersion:425`;表定义 `backend/migrator/migration/LATEST.sql:295,316`。

## 诊断与缺口

缺口是**结构化数据**,不是一句话:`model.Diagnostic{Category, Subject, Reference, Detail}`(`backend/plugin/lineage/model/diagnostic.go:33`),四类枚举在 `:12-27`(NotModelled / Unresolved / Ambiguous / CatalogUnavailable)。`model.FormatDiagnostics:72` 统一渲染,`Omitted` 计数用 "N more not listed" 表示被上限截断,避免一条满是未解析引用的语句把文本撑爆。

| 层 | 代码 | 行为 |
| --- | --- | --- |
| proto | `proto/v1/v1/lineage_service.proto:281,285,289,310` | `AnalyzeSQLResult.diagnostics` / `omitted_diagnostic_count`,以及 `AnalyzeSQLDiagnostic`、`DiagnosticCategory` |
| API | `backend/api/v1/lineage_service_analyze.go:120-137` | 拿到 `*UnsupportedStatementError` 时**保留 relations**,把缺口放进 `result.Diagnostics`;不再返回 `InvalidArgument` |
| API 仍然失败的三种 | `lineage_service_analyze.go:130-134` | 无分析器 → `FailedPrecondition`;scope 无效 / 解析失败 → `InvalidArgument` |
| CLI | `cli/cmd/lineage.go:182,185,238` | JSON 输出 `diagnostics` / `omittedDiagnosticCount`;table 用 `diagnosticText` 渲染同措辞 |
| CLI 文档 | `cli/skill/SKILL.md:134-138` | "Partial analyses":缺口不等于"这条语句没有血缘" |

边界只有一条:**解析失败清血缘,没建模只记缺口**。前者是"文本不可信",后者是"覆盖率不足,但已解析的边可信"。catalog 查询失败属于后者(降级为 `DiagnosticCatalogUnavailable`,通配符回退继续分析)。

## MySQL 家族的单一真相源

TiDB 和 MariaDB **没有自己的遍历**。`mysql/analyzer.go` 哨兵之下的正文是唯一可编辑副本,生成器把它逐字节写进两个兄弟包的 `analyzer_body_gen.go`。

| 文件 | 角色 | 现状(现场 `wc -l`) |
| --- | --- | --- |
| `backend/plugin/lineage/mysql/analyzer.go` | 唯一真相源:头部(1-80)+ 哨兵 `:81` + 正文 | 2297 行(`//go:generate go run ./gen` 在 `:12`) |
| `backend/plugin/lineage/tidb/analyzer_body_gen.go` | TiDB 生成物,`DO NOT EDIT` | 2243 行 |
| `backend/plugin/lineage/mariadb/analyzer_body_gen.go` | MariaDB 生成物,`DO NOT EDIT` | 2243 行 |
| `backend/plugin/lineage/tidb/dialect.go` | 手写方言头:包文档、omni import、四枚常量、`Registration()`、两个钩子 | 55 行 |
| `backend/plugin/lineage/mariadb/dialect.go` | 同上 | 54 行 |
| `backend/plugin/lineage/mysql/gen/generate.go` | 生成器纯函数:`Sentinel:18`、`dialects:40`、`Split:100`、`Generate:115`、`Render:147`、`checkSeam:209` | 301 行 |
| `backend/plugin/lineage/mysql/gen/main.go` | IO 壳,支持 `-check` | — |
| `backend/plugin/lineage/mysql/gen/generate_test.go:15,39,64,110` | `TestGeneratedBodiesAreFresh` / `…CopyTheSourceVerbatim` / `TestSplitRejectsMalformedSource` / `TestSeamSelfCheck` | — |

改代码时改哪一层:

- 改**遍历行为** → 只改 `mysql/analyzer.go` 哨兵之下,然后 `go generate ./backend/plugin/lineage/mysql`(`AGENTS.md:13-20`)。
- 改**方言事实** → 只改 `dialect.go`:四枚哨兵常量(`resultTableName`/`deletionFieldName`/`wildcardColumn`/`fileSourceMarker`)与两个钩子(`valuesQueryPrimary`、`rowAliasNames`)。
- 接缝是**双向**自检的(`generate.go:82` 的 `seamIdentifiers` + `checkSeam:209-223`):正文引用了某钩子而方言头没声明 → 生成失败;方言头声明了而正文不再引用 → 也失败。所以要新增变异点,必须同时改正文与两个方言头。

## AST 覆盖审计方法

审计回答的是"omni 的每个 AST 字段,分析器读到没有"。结论靠三个维度机器算出,不是读代码猜:

| 维度 | 来源 | 含义 |
| --- | --- | --- |
| **读** | `go/types` 类型检查分析器包,遍历 `SelectorExpr`,用 `types.Selection.Index()` 把字段读取精确归属到声明它的结构体 | 避免按名字 grep 的假阳性 |
| **泛走** | 解析 omni walker 的 `walkChildren` switch,标记被递归访问的字段 | 只说明"交给 `Inspect` 时字段内容会被访问" |
| **判定** | 人工给每个未读字段类别与依据 | 类别代码见下 |

关键区分:**"没被显式读" ≠ "没被处理"**。表达式子树由 `Inspect` 泛走覆盖,实测 15 种形状全部提取到列。风险只在**结构性字段**——分析器必须显式读才能向下遍历,漏读等于整块子树不可达。

| 代码 | 含义 | 典型 |
| --- | --- | --- |
| ✔ | 生产代码读到了 | — |
| ◐ 泛走覆盖 | 表达式/子节点,内容由 `Inspect` 找到 | `BoolExpr.Args`、`BinaryExpr.Left` |
| 位置/内部 | `Loc`、OID、typmod、解析器簿记 | 无列内容 |
| 派生回填 | 解析后回填,本仓库不消费 | `Ctecolnames`、`CreateViewStmt.SelectText` |
| 语法修饰 | 只选语法变体 | `IfNotExists`、`Ignore` |
| 排序/分页/锁 | 影响行集,不产生列血缘(跨批既定) | `SortClause`、`Limit` |
| DDL/装载选项 | 建表/分区/存储/装载格式 | `Options`、`FieldsTerminatedBy` |
| 已验证 | 怀疑过并用真实引擎/语料确认无影响 | `JoinExpr.JoinUsing` |
| 决策不建模 | 有显式决策记录,不是遗漏 | `UpdateStmt.WhereClause`(仅 SELECT 记谓词影响) |
| **!** 缺口 | 结构性字段未读导致边/列名丢失 | 见 §Open items |
| T 待决 | 需引擎验证或一次决策 | StarRocks `LoadDataDesc.SetExpr/Where` |

**范围外三类**(未被任何分析器触及的类型):① DDL/DCL/管理/事务/复制——`default` 分支显式忽略;② 表达式节点——由泛走覆盖;③ omni 里存在但本 pin 的语法产不出来的节点(如 PG `FromExpr`、`SetOperationStmt`)。三类都不读不影响可解析输入的边。

**表达式泛走实测清单**(15 种形状,按原审计实测表逐行复核;探针在审计归档后已删除):`CASE`、`COALESCE`、`ARRAY[…]`、`= ANY(ARRAY[…])`、`GROUPING`+`ROLLUP`、`IF`、`CAST`、`BETWEEN`、`IS NULL`、`LIKE`、`EXTRACT`、`COLLATE`、`ROW(…)`/行比较、聚合内 `ORDER BY`/`SEPARATOR`、内联窗口 `PARTITION BY`/`ORDER BY`。除各引擎语法差异(如 PG 无 `IF`、MySQL 无 `COALESCE` 形态)外,列都提取到了。

复现方法(工具是一次性的,已删):`go/types` 抽取 → 生成"字段 × 读取 × 泛走"表 → 包内临时 `_test.go` 探针 → 一次性容器做引擎实测。要重建须按此流程重跑,不能指望仓库里还有脚本。

## 上游依赖政策

五个解析器同属一个模块,钉在 `go.mod:10` `github.com/bytebase/omni v0.0.0-20260912023254-4574e69bb9f1`。政策是**不 patch、不 fork、不 vendor、不向上游反馈**(见 [omni-upstream-defects.md](omni-upstream-defects.md));代价是适配层脆弱,omni 任何 AST/Loc 微调只能靠语料事后发现。

- **升级流程缺失**:没有"带验收门槛的升级流程"(全语料 + 输出 diff)是唯一未决的依赖策略项(T5),go.mod 里没有任何 `replace`,仓库没有 `vendor/`。
- **ANTLR 已移除**:`go.mod:49` 只剩 `github.com/antlr4-go/antlr/v4 // indirect`,仓库里已无 `github.com/bytebase/parser` 依赖,`postgresqlantlr/` 目录不存在。
- **解析失败策略**:解析不了就是硬失败,错误进 `column_lineage_version.error_message`;不再有"部分分析被当成错误并丢边"的路径。
- **PG-FU-3**(未加引号标识符折小写,与 `pg_catalog`/registry 一致)已按 document-only 结案,由 `backend/plugin/lineage/postgresql/identifier_case_test.go:11-17` 钉住;细节属语义决策,见 [lineage-semantics.md](lineage-semantics.md)。

## 不变量

1. **方言只分析单语句**(`lineage.go:56-60`);脚本切分只在根包(`lineage.go:126-141`),跨语句不共享 scope。
2. **注册表是编译期的值**:`engines.Registrations()` + `NewAnalyzer(...)`;不得恢复 `init()` 自注册、`RegisterAnalyzeRelation`、根包包级可变量或 lineage blank import。重复 engine 是组装错误,直接 panic。
3. **`mysql/analyzer.go` 哨兵之下是唯一可编辑的家族正文**;绝不手改 `analyzer_body_gen.go`。改完必须重新生成,`TestGeneratedBodiesAreFresh` 会以红色失败信息给出命令。
4. **解析失败 = 清血缘;未建模 = 留血缘 + 缺口**。不要把缺口变成静默丢弃,也不要把缺口变成硬失败。
5. **缺口是数据不是句子**:一律经 `model.Diagnostic`,API 映射成 proto enum,CLI 渲染同一措辞;`error_message` 里存的是同一渲染文本。
6. **清血缘只由解析失败或对象删除触发**;引擎未注册是显式 skip(记 `markAnalyzed`),不构成失败。
7. **metahash 未变不重排**:成功、跳过、失败三条路径都写当前 hash,否则小时扫描会无限重试。
8. **哨兵字面量单点**:`__result__` / `__deletion__` / `__file__` / `*` 只在 `backend/plugin/lineage/model/relation.go:25,30,36,41` 定义;非测试 Go 代码里不得再出现字面量。
9. **catalog 查询失败不静默降级**:记 `DiagnosticCatalogUnavailable` 后用通配符继续,让降级结果可区分于完整结果。
10. **任何输出变更(边、变换、错误分类)必须过 golden 语料**,语料用精确边匹配而非"差不多"。

## Failure modes

| 场景 | 行为 |
| --- | --- |
| omni 解析不了某语句(含 MariaDB 三层括号 join) | 硬失败 → runner 清该对象血缘 + `error_message`,存 hash,等元数据变化再试 |
| 某子句只是没建模(如 PG `MERGE`) | 部分分析 → 兄弟语句的边照存,缺口写 version 行 / RPC diagnostics |
| 引擎没有分析器(OceanBase / Doris / MSSQL) | `ErrorEngineNotSupported` → 记 skip 与原因,不清边 |
| catalog `GetTable` 失败 | `DiagnosticCatalogUnavailable` + 通配符回退,分析继续 |
| 瞬时失败(DB 抖动等) | 30s/2m/5m 退避重试 ≤3 次,之后等小时全扫 |
| 数据库定义被删/改 | sync 提交后三向清边(`meta_guid`/`source_guid`/`target_guid`);变更的 VIEW/MV 重新入队 |
| 手工生成物被手改 | `go test ./backend/plugin/lineage/mysql/gen/` 变红并打印重新生成命令 |
| MariaDB 独有语法(row alias) | 生产硬失败;语料里登记在 `knownParserGaps`(`mariadb/analyze_test.go:15`) |

## Open items

1. **T5 omni 依赖策略与升级流程** — 仍开放。证据:`go.mod:10` pin,无 `replace`、无 `vendor/`;政策见 [omni-upstream-defects.md](omni-upstream-defects.md)。缺的是"带验收门槛(全语料 + 输出 diff)的升级流程"。
2. **PG-FU-2 上游回馈** — 仍开放。把"保留 DML/集合运算/窗口的 walker"作为生产级 `analysis` 包回馈 `github.com/bytebase/omni`,以缩小仓库内 walker;原先只登记在 PostgreSQL omni 迁移计划里(该计划文档随本次合并归档),现以本条为唯一跟踪点。
3. **`error_message` 是纯文本** — 仍开放。列本身 `TEXT`(`LATEST.sql:321`),store 侧 `*string`(`backend/store/column_lineage.go:52`),写入的是 `Diagnostic.String()` 渲染后的文本(`:391`);结构化只到 RPC 边界,前端若想按类别过滤历史缺口需要一次 schema 变更。
4. **跨包共享分析器基状态未做** — 仍开放。`pushScope` / `popScope` / `currentScope` / `attachColumnLookup` 在每个方言各一份(`mysql/analyzer.go:2205-2245`、`starrocks/analyzer.go:1190-1210`、`postgresql/analyzer.go:1759-1780`),只因绑定接收者状态(`a.catalog`、`a.scopeStack`)而未下沉;另有各包私有的 `locFieldCache sync.Map`(`mysql/analyzer.go:262`)。抽出共享基状态类型才能继续收敛。
5. **StarRocks `LOAD` 的 SET/WHERE 无列级血缘** — 仍开放。`starrocks/analyzer.go:892-896` 明确:omni 只把 SET 子句当原文保存,分析器只读 target 与 column list,不二次解析 SET/WHERE。
6. **MariaDB 三层以上括号 join 树缺口** — 仍开放。`mariadb/analyze_test.go:15-22` 的 `knownParserGaps` 共 5 条,其中 4 条是该缺口(mysqldump 视图会撞上);需上游修 omni 的 MariaDB parser。
7. **函数 / 存储过程血缘未覆盖** — 仍开放。`analyzer.go:156-159` 只扫 VIEW、MATERIALIZED_VIEW、MANUAL_SQL;FUNCTION / PROCEDURE 不在任何队列里。
8. **`OutputColumn.IsDerived` 无消费者** — 仍开放。定义 `backend/plugin/lineage/scope/types.go:163`;只被写入或自或合并(`mysql/analyzer.go:1012,1221`、`starrocks/analyzer.go:720,1047`、`postgresql/analyzer.go:999` 等),全仓没有任何读取它做决策的地方。

已复核后**删除**的原登记项:PG-FU-1(结构化表达式分类,已落地)、PG-FU-3(未加引号标识符折叠,已 document-only 结案)、PG-FU-5(`ON CONFLICT … EXCLUDED.col`,已落地)、ANTLR 迁移(依赖已移除)。

## Where things live

| 关注点 | 代码 | 测试 / 门禁 |
| --- | --- | --- |
| 装配 | `backend/plugin/lineage/lineage.go`、`engines/engines.go`、`backend/server/server.go:107` | `go test ./backend/plugin/lineage/engines/` |
| 缺口模型 | `backend/plugin/lineage/model/diagnostic.go` | `go test ./backend/plugin/lineage/model/ -run TestDiagnostic` |
| API / proto / CLI | `backend/api/v1/lineage_service_analyze.go`、`proto/v1/v1/lineage_service.proto:281-310`、`cli/cmd/lineage.go` | `go test ./cli/cmd/ -run TestDiagnosticText`;`cd proto && buf lint` |
| runner | `backend/runner/lineageanalyzer/analyzer.go` | `go test ./backend/runner/lineageanalyzer/` |
| store / schema | `backend/store/column_lineage.go`、`backend/migrator/migration/LATEST.sql:295,316` | `go test ./backend/store/`(schema 改动须补增量迁移) |
| 清理 | `backend/runner/schemasync/syncer.go:757-762,1076-1090` | `go test ./backend/runner/schemasync/` |
| MySQL 家族生成 | `backend/plugin/lineage/mysql/analyzer.go` + `mysql/gen/` | `go generate ./backend/plugin/lineage/mysql`;`go test ./backend/plugin/lineage/mysql/gen/` |
| 黄金语料 | `backend/plugin/lineage/{mysql,tidb,mariadb,postgresql,starrocks}/testdata/analyze/` | `go test ./backend/plugin/lineage/...` |
| 共享机制守门 | `backend/plugin/lineage/shared_analysis_test.go` | `go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared` |
| 端到端 | — | `make test-integration`(真实 PostgreSQL + MySQL) |

语料计数取法(2026-09 复核):`for d in backend/plugin/lineage/*/testdata/analyze; do grep -hcE '^\s*- name:' $d/*.yaml; done` → mysql 225、tidb 自有 15、mariadb 自有 20、postgresql 250、starrocks 185;MySQL 族共享语料 225 例在 TiDB / MariaDB 各跳过 1 / 5 例。非测试代码 15,370 行、测试 3,994 行取法:`find backend/plugin/lineage -name '*.go' ! -name '*_test.go' | xargs wc -l`(及去掉 `!` 的测试侧)。

不在本篇范围:各引擎"什么 clause 可见 / 什么必须保持产出"属语义决策,见 [lineage-semantics.md](lineage-semantics.md);图上字段级血缘的读取与展开见 [lineage-graph.md](lineage-graph.md);omni 缺陷明细与不修改政策见 [omni-upstream-defects.md](omni-upstream-defects.md)。
