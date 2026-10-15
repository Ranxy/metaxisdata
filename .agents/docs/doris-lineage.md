# Doris 血缘分析器 — Reference

> Status: **implemented**。Maintenance reference for `backend/plugin/lineage/doris/`。
> Related: [lineage-analyzer.md](lineage-analyzer.md)(装配、runner 生命周期、生成器、AST 审计方法)、[lineage-semantics.md](lineage-semantics.md)(transformation 模型与各引擎语义)、[starrocks-doris-driver.md](starrocks-doris-driver.md)(`mv_infos()` 与 `SHOW CREATE` 的来源)、[omni-upstream-defects.md](omni-upstream-defects.md)(上游缺陷与不修改政策)、[backend/plugin/lineage/AGENTS.md](../../backend/plugin/lineage/AGENTS.md)(改代码的规则)。

## What it is

`backend/plugin/lineage/doris` 把一条 Doris SQL 解析成 `[]model.ColumnRelation`,基于 `github.com/bytebase/omni/doris` 的解析器与类型化 AST(`doris/ast` + `doris/parser`,约 4.8 万行)。它注册 `storepb.Engine_DORIS`,是第七个方言包;`engines.Registrations()` 在编译期列全七个,进程启动时组成一个 `lineage.Analyzer`(见 [lineage-analyzer.md](lineage-analyzer.md))。

它是**独立包、不是生成物**,与 StarRocks / MSSQL 同一先例;MySQL / TiDB / MariaDB 仍是唯一的生成家族(哨兵 `// == MYSQL-FAMILY SHARED BODY ==`)。

omni 的 Doris 与 StarRocks 极其接近:关键字集合只差 3 个(StarRocks 独有的 `FIELD` / `PERCENTILE` / `PERCENTILE_UNION`);AST 结构体集合只差 4 个 StarRocks 独有(`ParenSelect`、`TableFunctionRef`、`IntoOutfileClause`、`InlineTable`)与 5 个 Doris 独有(`GroupedQuery`、`LateralView`、`TableSample`、`ArraySliceExpr`、`StructLiteral`)。把 173 条两边都能解析的 StarRocks 语料逐条对比规范 AST 形状:171 条一致,2 条例外正是 §方言事实的前两行。Doris 遍历因此是 StarRocks 遍历的移植,而不是第三个独立实现。

三条输入路径都已接通,无需 engine 分支:

| 来源 | 文本形态 | 说明 |
| --- | --- | --- |
| VIEW | `information_schema.VIEWS.VIEW_DEFINITION` 的**裸查询体** | runner 的 `buildSQL` 包成 `CREATE VIEW <name> AS <body>`;裸查询体不以 `CREATE` 开头,不走 `isCompleteStatement` 的放行分支 |
| MATERIALIZED_VIEW | Doris `mv_infos("database"=?)` 的 `QuerySql` 列,**裸查询体** | `backend/plugin/db/starrocks/sync.go` 的 Doris 分支读 `Name, QuerySql`,存进 `MaterializedViewMetadata.Definition`;runner 包成 `CREATE MATERIALIZED VIEW <name> AS <body>`。StarRocks 的同一列存的是完整 `CREATE MATERIALIZED VIEW`,Doris 的不是——这是 [starrocks-doris-driver.md](starrocks-doris-driver.md) 里两引擎共用字段、含义不同的一处 |
| MANUAL_SQL | 用户粘贴的脚本 | 根包切分后逐条分析;`SplitStatements` 走 `dorisparser.Split` |
| APPLICATION_SQL | 参数化语句 | 与其它方言同一条路径(见 [application-sql-ingestion.md](application-sql-ingestion.md));`sqlsanitize` 的 DORIS 分支复用 omni Doris 的一次性 `Tokenize`,lex 错误一律拒绝不写入库——Doris 词法器与 StarRocks 的逐字节相同(只差 ast import),所以两个分支做的是同一件事 |

`CREATE VIEW` / `CREATE MATERIALIZED VIEW` 的体是 WITH、集合运算或括号查询时,omni 的 CREATE VIEW 语法只接受裸 SELECT,于是 `viewbody.go` 用 `extractViewDDL` 抽出顶层 `AS` 之后的体,再按顶层查询解析。

## 方言事实(与 StarRocks 的差异)

| 事实 | 后果 | 代码 |
| --- | --- | --- |
| 括号查询、以及"同一条查询上重复出现的 ORDER BY/LIMIT 组"由 `GroupedQuery{Query, OrderBy, Limit, Offset}` 承载,不是 `ParenSelect{Sel}` | `dispatch` 与 `processQueryNode` 都按 `*nodes.GroupedQuery` 递归进 `Query`;它同时是作用域边界(带 WITH 的组不把 CTE 名提升到组外) | `analyzer.go`(`dispatch`、`processQueryNode`) |
| 表值函数是 `TableRef.Func`(一个 `*FuncCallExpr`),不是独立 `TableFunctionRef` | `processTableExpr` 先看 `t.Func`:有则注册成查询本地关系(不写注册表、不发明对象),别名缺省用函数名。Doris 不给它列清单,所以它只暴露"函数名"这一列:`FROM t, explode(t.arr) u` 把 `t.arr` 记为 `u.explode` 的来源,`SELECT u.explode …` 能解析;纯字面量调用(`numbers("number" = "10")`)没有可命名的来源,不产边也不报缺口 | `processTableExpr`、`processTableFunction` |
| `InsertStmt` 没有 `ByName` | 目标列清单永远是位置式:INSERT 的 `Columns` 按序映射到查询的输出列;没有"按名字匹配"可读 | `processInsertStatement` |
| CTAS 列名来自 `CreateTableStmt.Columns`,Doris 没有 `CTASColumns` | Doris 的 CTAS 列清单每列都带类型,名字从类型化定义里取并**盖过查询自己的输出别名**:`CREATE TABLE t3 (k INT, n VARCHAR(50)) AS SELECT id, name FROM users` 记 `users.id → t3.k`、`users.name → t3.n`;未声明时用查询输出别名 | `ctasColumnNames` |
| `UpdateStmt` / `DeleteStmt` 没有 `With` | Doris 没有 `WITH … UPDATE` / `WITH … DELETE`;这种输入是**硬解析失败**,不是缺口,也不是"没有血缘" | 语法层(omni `doris/parser`) |
| UPDATE 的 SET 目标**可以带限定符**(`UPDATE t1 SET t1.name = t1.other`),StarRocks 直接拒绝这个形状 | 写入目标仍取语句的表(`targetSchema`/`targetTable`),限定符只用来检查该列属于作用域里的关系;`UPDATE t1 AS x SET x.name = x.other` 同样接受,表达式保留别名的拼写。两条语料钉住 | `processUpdateList` |
| `TableRef.LateralViews` 是**新增血缘** | `LATERAL VIEW gen(args) alias AS c1[, c2]` 把生成器实参读到的每一列映射到 AS 声明的每一列,注册成一个查询本地关系;链式生成器左到右,后者能读前者声明的列。`u.elem` 因此能解析,而不是"未知名字";`generator_lineage_test.go` 钉住单列、多列、通配符与链式四种 | `processLateralViews`、`addGeneratedRelation` |

## 不新增血缘语义的语法

本节列的是 Doris 独有(或两引擎共有)但不改变血缘语义的语法形态;`26_test_doris_forms_table.yaml` 的 7 例(ROLLUP 分组键、数组切片、`CONVERT USING`、`TABLESAMPLE`、异步 MV 体、VARIANT 子列路径两例)钉住其中的语法形状,其余由通用表达式走查覆盖。会新增血缘的 Doris 独有形状只有 LATERAL VIEW,见 §方言事实。

| 形状 | 读取方式 |
| --- | --- |
| `TABLESAMPLE(1000 ROWS)` / `TABLESAMPLE(20 PERCENT)` / `REPEATABLE seed`(`TableRef.Sample`) | 只决定取哪些行,不产生列血缘 |
| `GROUP BY … WITH ROLLUP`(`SelectStmt.GroupByWithRollup`) | 分组键与普通 GROUP BY 同一批;ROLLUP 只多出小计行,不引入新列 |
| 数组切片 `a[1:2]`(`ArraySliceExpr`)、元素访问 `value[index]`(`ElementAtExpr`)、`STRUCT(…)` / Map 字面量(`StructLiteral`) | 通用表达式走查覆盖;切片的来源是 `a` |
| `CONVERT(x USING charset)` | 通用表达式走查覆盖 |
| `VARIANT` 列(含 `NULL` / `NOT NULL`) | **类型不参与血缘**:分析器只记列级边,从不读列类型;建表、`INSERT … SELECT`、CTAS 一视同仁 |
| `VARIANT` 子列路径 `v['key']` / `v['a']['b']` / `v['arr'][1]`(`ElementAtExpr` 链) | 官方的子列访问语法。路径选择发生在**列内部**,所以整列是来源:`SELECT v['user']['name'] AS n FROM events` 记 `events.v → __result__.n`(PROJECT);子列不是注册表对象,绝不做边端点。点号形式 `v.a`(官方访问语法里没有)由 omni 读成普通限定列名,分析器报 `unresolved` 缺口,与 StarRocks 相同 |
| `COPY INTO <table> FROM <stage>` | 文件整体是来源:`__file__.* → target.*` |
| `LOAD LABEL …(DATA INFILE … INTO TABLE …)` | 每个 data description 各有目标与可选列清单;omni 只把 SET 子句当原文,列级 SET/WHERE 不建模(与 StarRocks 同一条决策) |
| 目录限定名 `catalog.db.table` | 目录段被丢弃,按 `db.table` 分析(注册表没有 catalog 维度) |

## 能力矩阵

**建模**:SELECT(投影、别名、`*` / `t.*`、DISTINCT、CASE / CAST / 运算符、窗口、GROUP BY + HAVING + QUALIFY + ROLLUP、谓词、VARIANT 子列路径)、FROM(表、别名、db 限定名、INNER / LEFT / RIGHT / FULL / CROSS / SEMI / ANTI JOIN、派生表、CTE(含列清单、链式、递归自身)、表值函数、LATERAL VIEW)、集合运算(UNION / INTERSECT / EXCEPT,含 ALL 与嵌套)、CREATE VIEW / ALTER VIEW / CREATE MATERIALIZED VIEW(`CreateMTMVStmt`,含列清单)、CREATE TABLE … AS SELECT、INSERT [OVERWRITE] INTO … SELECT / VALUES(位置式列清单)、UPDATE … SET(含 FROM)、DELETE … USING … WHERE(→ `__deletion__`)、COPY INTO、LOAD、查询本地关系的展平与谓词影响。

**报告为缺口而不是失败**:

| 形状 | 行为 |
| --- | --- |
| `MERGE INTO` | `not modelled:` 缺口,兄弟语句的边保留(与 StarRocks、PostgreSQL 对 MERGE 的同一决策);`analyzer_test.go` 的 `TestUnmodelledStatementsAreGaps` 钉住 |
| 未建模的查询表达式 | `not modelled: query expression` 缺口,语句其余部分照常分析 |
| 引用解析不到(未解析列、通配符限定符) | `unresolved` 缺口 |
| catalog 查询失败 | `DiagnosticCatalogUnavailable` + 通配符回退,分析继续 |

**不建模(有意)**:`LOAD` 的 SET/WHERE 列级血缘;目录维度(注册表里没有)。

## 诊断与失败边界

| 场景 | 行为 |
| --- | --- |
| omni 解析失败(含 `WITH … UPDATE/DELETE`、被抽出的视图体自身不合法) | 硬失败:无部分结果;runner 清该对象血缘 + `error_message` |
| 多语句输入 | 硬失败:分析器只定义在单条语句上,切分由根包负责 |
| 某形状没建模(MERGE、未建模查询表达式) | 部分分析:兄弟边照存,缺口写 version 行 / RPC diagnostics |
| 引用解析不到 | 部分分析 + 对应 `model.Diagnostic` |
| catalog 查询失败 | `DiagnosticCatalogUnavailable` + 通配符继续 |
| 只有注释 / 空白 | 硬失败(没有语句可分析),而不是"没有血缘" |

## 不变量

1. **`GroupedQuery` 只是括号/重复子句组的壳**:递归进 `Query`,绝不把它当成关系或边的端点。
2. **`TableRef.Func` 与 LATERAL VIEW 关系是查询本地关系,永不作为边的端点**:先经 `flattenTempSourceLineage` 展平;**生成器实参里没有可解析列(纯字面量调用)时不产边**——它没有这个分析能命名的来源。
3. **INSERT 的目标列清单永远位置式**:Doris 没有 `BY NAME`,`Columns[i]` 对查询第 i 个输出列。
4. **CTAS 列名取类型化 `ColumnDef`**:Doris 没有名字清单;声明了就用声明,没声明就用查询输出别名。
5. **`WITH` 在 UPDATE / DELETE 之前是解析失败,不是缺口**:语法层不存在这个形状。
6. **MERGE 与未建模查询表达式是缺口,不是硬失败**:已解析的边必须留下 `model.Diagnostic`;只有解析失败才清血缘。
7. **任何输出变更(边、变换、错误分类)必须过黄金语料**,语料用精确边匹配(`subset` 未使用)。

## Open items

1. **MERGE 未建模** — 与 StarRocks 同:报 `not modelled` 缺口而不是产边。要建模得先决定 MERGE 的动作语义,不能自行猜。
2. **`LOAD` 的 SET/WHERE 无列级血缘** — omni 只把 SET 当原文,分析器只读 target 与 column list(与 StarRocks 同;[lineage-analyzer.md](lineage-analyzer.md) 已登记)。
3. **APPLICATION_SQL 里的结构性整数会被 ingest gate 拒绝** — `sqlsanitize` 的 N1 规则把每个整数字面量改写成 `?`,不看语法位置,于是 `CREATE TABLE t3 (k INT, n VARCHAR(50)) AS SELECT …` 变成 `VARCHAR(?)`、`SELECT * FROM t TABLESAMPLE(10 PERCENT)` 变成 `TABLESAMPLE(? PERCENT)`;Doris 解析器要求这两处是整数,所以 `backend/api/v1/application_sql_handler.go` 以解析失败拒绝上报——即使分析器读的是原始语句。重写输出在 MYSQL / POSTGRES / STARROCKS / DORIS / MSSQL 上逐字节相同(含 canonical text 与 fingerprint),这是方言中性的位置盲重写,不是 Doris 缺陷(PostgreSQL 只是恰好接受 `TABLESAMPLE BERNOULLI (?)`)。注意 gate 由分析器决定:注册 Doris 之前 `Analyze(DORIS, …)` 返回 `ErrorEngineNotSupported`(不是 `UnsupportedStatementError`),**所有** Doris 应用语句都被拒;现在只剩这些结构性整数形态。修法是位置感知的字面量分类,属跨方言设计变更。已登记在 [backend-open-items.md](backend-open-items.md)。
4. **没有端到端覆盖** — 集成 harness 只有 PostgreSQL + MySQL;Doris 语料是从 StarRocks 移植并在两边解析器上对比过的,不是在真实 Doris 实例上跑出来的。首次真实运行要确认 `mv_infos()` 的 `QuerySql` 形态与视图体。
5. **函数 / 存储过程血缘未覆盖** — 与所有方言同:队列只扫 VIEW / MATERIALIZED_VIEW / MANUAL_SQL / APPLICATION_SQL。
6. **`VARIANT<…>` schema template 是 omni 解析缺口** — `CREATE TABLE t (k BIGINT, v VARIANT<'id': INT>)`、其 CTAS 形式与 `ALTER TABLE … ADD COLUMN v VARIANT<…>` 都是合法 Doris DDL,但 omni 在 `<` 处报语法错误(裸 `VARIANT` 与 `VARIANT NULL` 正常)。视图 / MV 体不含建表语句,所以这两条路径不受影响;MANUAL_SQL / APPLICATION_SQL 携带这种 DDL 会硬失败(清血缘 + `error_message`),CTAS 形式会整条丢边。明细见 [omni-upstream-defects.md](omni-upstream-defects.md) §12;按不修改政策不 patch 上游。

## Where things live

| 关注点 | 代码 | 测试 / 门禁 |
| --- | --- | --- |
| 方言包 | `backend/plugin/lineage/doris/{analyzer,expr,predicate,rawsql,viewbody}.go` | `go test ./backend/plugin/lineage/doris/` |
| 装配 | `backend/plugin/lineage/engines/engines.go`、`engines_test.go` | `go test ./backend/plugin/lineage/engines/` |
| 共享机制守门 | `backend/plugin/lineage/shared_analysis_test.go`(`dialectPackages` 含 `doris`) | `go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared` |
| 黄金语料 | `backend/plugin/lineage/doris/testdata/analyze/`(21 套 / 198 例 / 410 条边) | `TestAnalyzeYAML`、`TestCorpusIsFullyAnnotated` |
| 单语句契约与硬失败 | `hardfail_test.go`(`TestHardFailOnUnparseableSQL`、`TestRejectsMultiStatement`) | 同上 |
| 缺口边界 | `analyzer_test.go`(`TestUnmodelledStatementsAreGaps`) | 同上 |
| catalog 读取 | `catalog_lookup_test.go`(`TestCatalogIsReadOncePerRelation`、`TestCatalogFailureIsReportedAsAGap`) | 同上 |
| LATERAL VIEW 与表值函数的映射 | `analyzer.go`(`processLateralViews`、`addGeneratedRelation`、`processTableFunction`) | `generator_lineage_test.go`(`TestLateralViewGeneratorLineage`、`TestTableValuedFunctionLineage`);`testdata/analyze/23_test_query_local_name_lineage_table.yaml` 三例 |
| CTAS 目标列名 | `analyzer.go`(`ctasColumnNames`) | `generator_lineage_test.go`(`TestCTASTargetColumnsAreTheDeclaredNames`) |
| Doris 独有语法 | 通用表达式走查 + `SelectStmt.GroupByWithRollup` | `testdata/analyze/26_test_doris_forms_table.yaml` 七例(含 VARIANT 子列路径两例) |
| 视图体的抽取 | `viewbody.go`(`extractViewDDL`)、`viewbody_test.go` | `go test ./backend/plugin/lineage/doris/` |
| MV 定义来源 | `backend/plugin/db/starrocks/sync.go`(`mv_infos("database"=?)`)、`backend/runner/lineageanalyzer/analyzer.go`(`buildSQL`) | [starrocks-doris-driver.md](starrocks-doris-driver.md) |
| 上游 AST 差异记录 | 本篇 §方言事实 | — |

不在本篇范围:transformation 模型的字段含义见 [lineage-semantics.md](lineage-semantics.md);装配与 runner 生命周期见 [lineage-analyzer.md](lineage-analyzer.md);上游缺陷与不修改政策见 [omni-upstream-defects.md](omni-upstream-defects.md)。
