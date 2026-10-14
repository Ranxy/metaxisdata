# MSSQL(T-SQL)血缘分析器 — Reference

> Status: **implemented**。Maintenance reference for `backend/plugin/lineage/mssql/`。
> Related: [lineage-analyzer.md](lineage-analyzer.md)(装配、runner 生命周期、生成器、AST 审计方法)、[lineage-semantics.md](lineage-semantics.md)(transformation 模型与各引擎语义)、[omni-upstream-defects.md](omni-upstream-defects.md)(上游缺陷与不修改政策)、[backend/plugin/lineage/AGENTS.md](../../backend/plugin/lineage/AGENTS.md)(改代码的规则)。

## What it is

`backend/plugin/lineage/mssql` 把一条 T-SQL 语句解析成 `[]model.ColumnRelation`,基于 `github.com/bytebase/omni/mssql` 的解析器与类型化 AST。它注册 `storepb.Engine_MSSQL`,是第六个方言包;`engines.Registrations()` 在编译期列全六个,进程启动时组成一个 `lineage.Analyzer`。

三条输入路径都已接通,无需 engine 分支:

| 来源 | 文本形态 | 说明 |
| --- | --- | --- |
| VIEW | `sys.sql_modules.definition` 的**完整 `CREATE VIEW … AS …`** | runner 的 `buildSQL` 用 `isCompleteStatement` 原样放行(不会二次包裹);`ALTER VIEW` 也被 omni 解析成 `CreateViewStmt`,而 SQL Server 本身在存储时把它归一成 `CREATE VIEW` |
| MANUAL_SQL | 用户粘贴的脚本 | 支持 `GO` 批分隔符;见 §切分 |
| APPLICATION_SQL | 参数化语句 | 走 `sqlsanitize.Analyzable`,该函数对 MSSQL 用 **T-SQL 词法器**(`mssql/parser`),字面量与引号标识符的边界由它决定 |

MSSQL 没有物化视图,`MetaType_MATERIALIZED_VIEW` 这条路径与它无关。

## 方言事实(与其它方言的差异)

| 事实 | 后果 | 代码 |
| --- | --- | --- |
| 集合运算不是独立节点,而是 `SelectStmt` 上的 `Op/All/Larg/Rarg` | 一个 `SelectStmt` 既是查询又是集合运算,`processSelectStatement` 先看 `Op`;`flattenSetOpArms` 递归 `Larg/Rarg` | `analyzer.go` `flattenSetOpArms` |
| `SelectStmt.All` 与 `SELECT ALL` 复用同一个字段 | **只在 `Op != SetOpNone` 时**把它当"保留重复行"读,否则 `SELECT ALL` 会被误判成 UNION ALL | `setOpTransformation` |
| 限定符是 schema(`TableRef.Schema/Object`,列的末段叫 `Column`) | 所有边用 `scope.NewSchemaLineageEdge`,与 PostgreSQL 同类;`catalogTable` 也按 `{Schema, Name}` 查 | `expr.go`、`analyzer.go` |
| 支持 `db.schema.table` 三部分名,而 `scope.ColumnRef` 只带 schema | 数据库限定符与 analysis context 的 database 比对:相同(或没有 context)则按 schema 限定名正常分析;不同则**报告缺口**而不是把它读成本库同名对象 | `databaseMatches` |
| 视图定义是完整语句,不是裸查询体 | 不需要 StarRocks 式的 viewbody 提取;唯一例外见 §缺口 | `processCreateView` |
| `[bracketed]` 标识符由解析器去括号 | 边的字段名是去括号后的名字;`Loc` 仍能取回带括号的原文 | `identifierOf` |
| 若干节点的 `Loc.End` 为 -1:标量子查询、`AliasedTableRef`、所有名字列表(`*List`/`*String`)、`SetExpr.Column` | 表达式文本取不到源切片时按节点重建(见 `rebuiltText`);名字列表一律读 `.Str`,绝不读文本;`SetExpr.Column` 读字段 | `expr.go` |
| 解析器偶尔返回**类型化 nil**(`AST` 是 `*(*SelectStmt)(nil)`) | `x == nil` 判不出来,而 omni 的 walker 会解引用它:所有入口先过 `nodeOrNil` | `expr.go` `nodeOrNil` |
| 两个关键字会被当成表别名:NATURAL(join)、USING(MERGE 目标) | `relationAlias` 用源文本复核别名,是子句就丢掉;写出来的 `AS [USING]` 保留 | `analyzer.go` |

## 能力矩阵

**建模**:SELECT(投影、别名、`*`/`t.*`、DISTINCT、TOP、CASE/IIF/COALESCE/NULLIF/CAST/CONVERT、运算符、窗口与命名窗口 `WINDOW w AS`、GROUP BY + HAVING、谓词)、FROM(表、别名、schema/三段名、INNER/LEFT/RIGHT/FULL/CROSS JOIN、CROSS/OUTER APPLY、派生表(含别名列清单)、CTE(含列清单、链式、递归自身)、`VALUES` 内联表、表值函数、PIVOT/UNPIVOT、表提示、`FOR JSON`/`OPTION`)、集合运算(UNION/UNION ALL/EXCEPT/INTERSECT,含嵌套)、CREATE VIEW / CREATE OR ALTER VIEW / ALTER VIEW(含列清单、SCHEMABINDING、括号名)、SELECT … INTO、INSERT … SELECT / VALUES / DEFAULT VALUES、UPDATE … SET(含 FROM)、DELETE(WHERE → `__deletion__`)、MERGE(UPDATE SET / INSERT VALUES / DELETE)、BULK INSERT(`__file__ → target`)。

**不建模(有意,均已在 corpus 或此处钉住)**:

| 形状 | 行为 |
| --- | --- |
| `OUTPUT` / `OUTPUT … INTO` | 不产出边,与 PostgreSQL 的 `RETURNING` 同一决策:返回给客户端的结果集不是血缘,写才是 |
| `SELECT @v = expr` | 变量赋值不是结果列,不产出行;它读到的列也不记为影响 |
| UPDATE 的 `WHERE` / `FROM` join 谓词 | 不记影响边(与 PostgreSQL / MySQL 一致);DELETE 有 WHERE 时记 `__deletion__`,没有 WHERE 时把 FROM 的 join 谓词记为 DELETE 目标行的影响 |
| 表变量(`@tv`)、`SEMANTIC` 模型表 | 无注册表身份 → `not modelled` 缺口;引用它的列报"未解析" |
| `INSERT … EXEC` | 源是存储过程的结果集 → 缺口 |
| MERGE 的 WHEN 条件、`PIVOT` 源表的其余分组列 | 不记录;PIVOT 只记聚合函数与 FOR 列的 group key |
| 行集函数(`OPENJSON … WITH (…)`、`OPENROWSET`)的自身列清单 | 按函数名建列,别名列清单不建模 |
| 递归 CTE 的不动点 | 与其他方言相同,单遍分析 |

**上游语法缺口(硬失败,不建模)**:`JOIN … USING (…)`、`NATURAL JOIN`(静默误读为别名,已在本地复核)、`OPENROWSET(BULK …)`、表值函数的带类型列定义、`CREATE VIEW v AS (SELECT …)` 的括号体。明细见 [omni-upstream-defects.md](omni-upstream-defects.md) §10。

**`WITH` 在 DML 之前**:omni 读到 CTE 列表后若下一个词不是 SELECT,就丢掉整段 `WITH` 并留下一个空语句,紧随的 INSERT/UPDATE/DELETE/MERGE 其文本跨度也会变成空。CTE 定义在树里不可达,任何引用都可能指向其中一个,所以方言包在分析前**从源文本恢复 CTE 列表**(`withclause.go`:切出第一个顶层 DML 关键字之前的前缀,补一个 `SELECT 1` 解析出 `WithClause`,再挂回该语句)。前缀是原文本的前缀,因此 CTE 体内的偏移量仍然有效。恢复失败才硬失败——文本已被丢弃一部分时不能拿错误的边冒充部分结果。做法与 StarRocks 的 `viewbody.go` 同类。

## 切分(ScriptSplitter)

`SplitStatements` 以**解析为主、词法扫描兜底**,与 PG / StarRocks 的纯词法切分不同:

1. `mssql.Parse` 整段解析后按 `Statement.Text` 取语句。这是唯一能正确处理 `GO` 批分隔符与 `CREATE PROCEDURE … BEGIN … ; … END` 内部 `;` 的办法(词法切分会把过程体切碎)。`GoStmt` 项被丢弃——它不是语句。
2. 某个保留语句的 `Text` 为空(因为前一条语句的 `Loc.End` 无效,把整段文本都吃掉了)时,放弃解析结果,回退到词法扫描。
3. 词法扫描按顶层 `;` 切分,理解字符串字面量(`''` 转义)、`[bracketed]`(`]]` 转义)、双引号标识符、行/块注释(T-SQL 块注释可嵌套);空白与注释构成的片段不作为语句。它只处理解析器已经拒绝的文本,唯一职责是让"哪一条语句解析不了"仍然可被报告,而不是静默变成"这段脚本没有血缘"。

## 诊断与失败边界

| 场景 | 行为 |
| --- | --- |
| omni 解析失败(含括号视图体、OPENROWSET(BULK)) | 硬失败:无部分结果;runner 清该对象血缘 + `error_message` |
| `WITH` + DML(上游丢 CTE) | 从源文本恢复 CTE 列表后正常分析;恢复失败才硬失败(文本已被丢弃一部分,不可信) |
| 某子句没建模(表变量、INSERT EXEC、SEMANTIC 表) | 部分分析:兄弟边照存,缺口写 version 行 / RPC diagnostics |
| 引用解析不到(未解析列 / 通配符限定符 / 跨库名) | 部分分析 + 对应 `model.Diagnostic` |
| catalog 查询失败 | `DiagnosticCatalogUnavailable` + 通配符回退,分析继续 |
| 只有注释/空白 | 硬失败(没有语句可分析),而不是"没有血缘" |

## 不变量

1. **`All` 只在 `Op != SetOpNone` 时读**;否则 `SELECT ALL` 会被当成保留重复行。
2. **边带 schema 不带 database**:`NewSchemaLineageEdge`;三部分名的 database 只用于与 analysis context 比对。
3. **查询本地关系永不作为边的端点**:CTE / 派生表 / 表值函数 / PIVOT 关系先经 `tempTableLineage` 展平。
4. **别名必须与源文本复核**:`relationAlias` 是防两个上游误读的唯一闸门。
5. **表达式的 governing 节点决定 transformation**(不是树里第一个函数调用),与 PostgreSQL 的结构化分类同一原则;运算符词汇表用 MySQL 族的词(ADDITION 而非 `+`),因为 AST 是枚举驱动。
6. **`nodeOrNil` 是每个入口的第一行**:类型化 nil 会让 omni 的 walker 崩溃。
7. **切分器宁可回退到词法扫描,也不返回空列表或空字符串**。
8. **任何输出变更必须过黄金语料**,语料用精确边匹配(`subset` 未使用)。

## Open items

1. **标识符大小写** — 边的字段名原样保留 SQL 的写法(与 MySQL 相同),而 MSSQL 引擎按数据库排序规则大小写不敏感、注册表的 meta GUID 大小写敏感。因此 `SELECT id FROM Users` 会产出列名 `id`,与注册表里 `ID` 不是同一个名字。要修需要注册表级别的归一化(或按实例排序规则折叠),不在本方言范围内。
2. ~~**APPLICATION_SQL 的词法器**~~ — **已落地**。`sqlsanitize` 现在对 MSSQL 走 `mssql/parser` 的 lexer 分支(`tokenSpans` + `isLiteral` 的 `[`/`#`/`"` 规则 + `stripComments` 不再把 `#` 当注释),并用 `testdata/10_mssql.yaml` 与 MSSQL 语料回归钉住;细节见 [application-sql-ingestion.md](application-sql-ingestion.md)。
3. **MERGE 的 WHEN 条件** — `WHEN MATCHED AND …` / `WHEN NOT MATCHED AND …` 的条件不记为影响边;目前只有 ON 条件记 join 影响。
4. **PIVOT 的隐式分组列** — PIVOT 会保留源表中未参与聚合/FOR 的列,只有 catalog 能说出它们是谁;现在这些列在 PIVOT 关系里不存在,`SELECT *` 走通配符回退。
5. **命名窗口跨语句不可见** — `WINDOW w AS …` 只在处理该查询期间有效(`namedWindows`),这是 SQL 的作用域,不是限制。
6. ~~**上游 `WITH` + DML 缺陷的本地绕行**~~ — **已落地**。`withclause.go` 恢复 CTE 列表(StarRocks `viewbody.go` 的同类做法),四种 DML 与"CTE 体变换透传"都由语料钉住;`withclause_test.go` 钉住边界判定与只挂 DML 两半。上游缺陷记录见 [omni-upstream-defects.md](omni-upstream-defects.md) §10.1。

## Where things live

| 关注点 | 代码 | 测试 / 门禁 |
| --- | --- | --- |
| 方言包 | `backend/plugin/lineage/mssql/{analyzer,expr,predicate,split,withclause}.go` | `go test ./backend/plugin/lineage/mssql/` |
| 装配 | `backend/plugin/lineage/engines/engines.go`、`engines_test.go` | `go test ./backend/plugin/lineage/engines/` |
| 共享机制守门 | `backend/plugin/lineage/shared_analysis_test.go`(`dialectPackages` 含 `mssql`) | `go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared` |
| 黄金语料 | `backend/plugin/lineage/mssql/testdata/analyze/`(14 套 / 101 例) | `TestAnalyzeYAML`、`TestCorpusIsFullyAnnotated` |
| 切分与别名复核 | `analyzer_test.go`(`TestSplitStatements`、`TestKeywordAliasIsNotAnAlias`) | 同上 |
| 跨库限定符 | `analyzer_test.go`(`TestCrossDatabaseQualifierIsReported`) | 同上 |
| 上游缺陷记录 | `.agents/docs/omni-upstream-defects.md` §10 | — |
| 端到端 | — | `make test-integration`(真实 PostgreSQL + MySQL;MSSQL 不在集成测试矩阵内) |

不在本篇范围:transformation 模型的字段含义见 [lineage-semantics.md](lineage-semantics.md);装配与 runner 生命周期见 [lineage-analyzer.md](lineage-analyzer.md);上游缺陷与不修改政策见 [omni-upstream-defects.md](omni-upstream-defects.md)。
