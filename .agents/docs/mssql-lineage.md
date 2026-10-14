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
| APPLICATION_SQL | 参数化语句 | 走 `sqlsanitize.Analyzable`,该函数对 MSSQL 用 **T-SQL 词法器**(`mssql/parser`)决定字面量与引号标识符的边界;词法器报出"没走完"的语句(未闭合的字符串、引号标识符、`[` 括号名、块注释)一律拒绝,不写入库 |

MSSQL 没有物化视图,`MetaType_MATERIALIZED_VIEW` 这条路径与它无关。

## 方言事实(与其它方言的差异)

| 事实 | 后果 | 代码 |
| --- | --- | --- |
| 集合运算不是独立节点,而是 `SelectStmt` 上的 `Op/All/Larg/Rarg` | 一个 `SelectStmt` 既是查询又是集合运算,`processSelectStatement` 先看 `Op`;`flattenSetOpArms` 递归 `Larg/Rarg` | `analyzer.go` `flattenSetOpArms` |
| `SelectStmt.All` 与 `SELECT ALL` 复用同一个字段 | **只在 `Op != SetOpNone` 时**把它当"保留重复行"读,否则 `SELECT ALL` 会被误判成 UNION ALL | `setOpTransformation` |
| 限定符是 schema(`TableRef.Schema/Object`,列的末段叫 `Column`) | 所有边用 `scope.NewSchemaLineageEdge`,与 PostgreSQL 同类;`catalogTable` 也按 `{Schema, Name}` 查 | `expr.go`、`analyzer.go` |
| 支持 `db.schema.table` 三部分名,而 `scope.ColumnRef` 只带 schema | 数据库限定符与 analysis context 的 database 比对:相同(或没有 context)则按 schema 限定名正常分析;不同则**报告缺口**而不是把它读成本库同名对象 | `databaseMatches` |
| 标识符大小写:引擎默认排序规则下**不敏感**,而注册表按 meta GUID **精确**匹配(逐字节、`COLLATE "C"`) | 三层都按引擎规则落地:①**语句内**解析由 `scope.SetFoldNames` 折叠,`SELECT T.a FROM t`、`SELECT X.a FROM t AS x`、CTE 名大小写不同都能解析;②**列名**按注册表拼写记录(精确拼写优先),`CREATE VIEW` / `SELECT INTO` 的列名是语句自己定义的、保持原样;③**关系名/模式名与注册表之间的那一步也折叠**——`catalog.Provide.GetTable` 精确未命中时,只在方言声明了折叠的引擎上按 `GUIDFolded` 再查一次(`lower(guid COLLATE "default") = lower($1)`,由 `idx_meta_registry_resource_guid_folded` 服务),并把**注册表的身份**回给分析器;mssql 在两个存储关系注册点(`addBaseTable`、`targetAddress`)用它改写关系地址,于是边记的是注册表里的对象而不是语句写下的拼写。折叠只决定指向哪个对象,不决定列拼写;注册表不知道的名字保持语句原样 | `scope.SetFoldNames`、`columnSpelling`、`canonicalRelation`、`catalog.WithIdentifierFolding`、`FindMetaRegistryResourceMessage.GUIDFolded` |
| 视图定义是完整语句,不是裸查询体 | 不需要 StarRocks 式的 viewbody 提取;唯一例外见 §缺口 | `processCreateView` |
| `[bracketed]` 标识符由解析器去括号 | 边的字段名是去括号后的名字;`Loc` 仍能取回带括号的原文 | `identifierOf` |
| 若干节点的 `Loc.End` 为 -1:标量子查询、`AliasedTableRef`、所有名字列表(`*List`/`*String`)、`SetExpr.Column` | 表达式文本取不到源切片时按节点重建(见 `rebuiltText`);名字列表一律读 `.Str`,绝不读文本;`SetExpr.Column` 读字段 | `expr.go` |
| 解析器偶尔返回**类型化 nil**(`AST` 是 `*(*SelectStmt)(nil)`) | `x == nil` 判不出来,而 omni 的 walker 会解引用它:所有入口先过 `nodeOrNil` | `expr.go` `nodeOrNil` |
| 两个关键字会被当成表别名:NATURAL(`t1 NATURAL JOIN t2`)、USING(MERGE 目标) | 按**关键字之后的 token** 判定别名是不是子句:NATURAL 后跟 `JOIN`、USING 后跟行源(名字/括号/函数)才是子句;注释不是 token、`WITH (…)` 表提示整组跳过,所以带注释或表提示的 MERGE 也能识别。NATURAL/USING 只是 ODBC/未来保留字,**不是** T-SQL 保留字,因此 `FROM t NATURAL`、`FROM t USING` 是合法别名,必须保留;写出来的 `AS [USING]` 同理 | `analyzer.go` `aliasIsFollowingClause` |

## 能力矩阵

**建模**:SELECT(投影、别名、`*`/`t.*`、DISTINCT、TOP、CASE/IIF/COALESCE/NULLIF/CAST/CONVERT、运算符、窗口与命名窗口 `WINDOW w AS`、GROUP BY + HAVING、谓词)、FROM(表、别名、schema/三段名、INNER/LEFT/RIGHT/FULL/CROSS JOIN、CROSS/OUTER APPLY、派生表(含别名列清单)、CTE(含列清单、链式、递归自身)、`VALUES` 内联表、表值函数、PIVOT/UNPIVOT、表提示、`FOR JSON`/`OPTION`)、集合运算(UNION/UNION ALL/EXCEPT/INTERSECT,含嵌套)、CREATE VIEW / CREATE OR ALTER VIEW / ALTER VIEW(含列清单、SCHEMABINDING、括号名)、SELECT … INTO、INSERT … SELECT / VALUES / DEFAULT VALUES、UPDATE … SET(含 FROM)、DELETE(WHERE → `__deletion__`)、MERGE(UPDATE SET / INSERT VALUES / DELETE)、BULK INSERT(`__file__ → target`)。

**不建模(有意,均已在 corpus 或此处钉住)**:

| 形状 | 行为 |
| --- | --- |
| `OUTPUT` / `OUTPUT … INTO` | 不产出边,与 PostgreSQL 的 `RETURNING` 同一决策:返回给客户端的结果集不是血缘,写才是 |
| `SELECT @v = expr` | 变量赋值不是结果列,不产出行;它读到的列也不记为影响 |
| UPDATE 的 `WHERE` / `FROM` join 谓词 | 不记影响边(与 PostgreSQL / MySQL 一致);DELETE 有 WHERE 时记 `__deletion__`,没有 WHERE 时把 FROM 的 join 谓词记为 DELETE 目标行的影响 |
| 表变量(`@tv`)、`#temp` 临时表、`SEMANTIC` 模型表 | 无注册表身份 → `not modelled` 缺口;引用它的列报"未解析"。**DML 目标不可寻址时不产出任何边**:INSERT/UPDATE/DELETE/MERGE/SELECT INTO 走同一条规则——MERGE 的每个 WHEN 子句都写目标,过去会写出目标 schema 与名字都是空串的边,而 runner 照单入库 |
| `INSERT … EXEC` | 源是存储过程的结果集 → 缺口 |
| 行集函数(`OPENJSON … WITH (…)`、`OPENROWSET`)的自身列清单 | 按函数名建列,别名列清单不建模 |
| 表提示 + 别名(`FROM t WITH (NOLOCK) x`) | 上游语法缺口:带表提示的表不接受别名(`t NOLOCK` 反而把 NOLOCK 读成别名),解析直接失败 → 硬失败;单有表提示正常 |
| 递归 CTE 的不动点 | 与其他方言相同,单遍分析 |

**两处按 catalog 决定的行为**:PIVOT 会保留源表中"聚合函数与 FOR 列都没消费"的列作为分组列,但语句从不写出它们 —— 源关系的列清单已知(catalog 命中 / CTE / 内联表)时按 `源列 → PIVOT 列`(direct,值原样带过)记录,未知时只暴露 IN 值列(不猜);MERGE 的 `WHEN … AND <条件>` 记为该目标行的 FILTER 影响,与 ON 条件同一机制。PIVOT/UNPIVOT、表值函数与内联 VALUES 的源在**独立子作用域**里分析——它是关系内部的行源,留在外层会让 `SELECT *` 把它当成第二个关系展开;手写的内部 lineage 也必须像 `tempTableLineage` 一样**展平查询本地来源**(CTE/派生表),否则 CTE 名会变成落库的边端点(语料 `12_test_pivot_lineage_table.yaml` / `16_test_extended_forms_lineage_table.yaml` 各四例钉住,含派生表叠 CTE)。

**上游语法缺口(硬失败,不建模)**:`JOIN … USING (…)`、`OPENROWSET(BULK …)`、表值函数的带类型列定义、`CREATE VIEW v AS (SELECT …)` 的括号体、`GROUP BY ALL`(omni 直接报 `syntax error at end of input`)。`NATURAL JOIN` 不在此列:它不是 T-SQL 语法,解析器把 NATURAL 读成别名,由 §别名复核那一条按后随 token 处理(`t1 NATURAL JOIN t2` 仍能把 `t1.a` 解析出来)。明细见 [omni-upstream-defects.md](omni-upstream-defects.md) §10。

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
9. **标识符折叠是引擎事实,不是方言偏好**:`scope.SetFoldNames` 默认关,只有标识符比较不敏感的引擎(T-SQL,默认排序规则下不敏感)可以打开;同一个事实由 `EngineRegistration.FoldIdentifierNames` 声明一次,方言在构造时把它标到 context 上,共享的 catalog 层据此决定要不要做折叠回退。折叠只影响"引用指向哪个对象",绝不改写记录下来的列拼写——MySQL 在大小写敏感文件系统上的表名、PostgreSQL 的带引号别名都真的把 `t` 与 `T` 当成两个对象;而关系地址取注册表身份,因为注册表才是对象的事实来源。
10. **目标不可寻址 ⇒ 不产出边**:表变量、`#temp`、跨库目标一律以 `not modelled` 或 unresolved 缺口说明,绝不以空标识符落边;也不把语句的读改挂到 `__result__` 上(这些语句不返回结果集)。

## Open items

1. ~~**标识符大小写**~~ — **已落地**。三层齐了:①语句内解析折叠(`scope.SetFoldNames`);②列名取注册表拼写(`columnSpelling`、`registeredColumn`);③关系身份取注册表身份(`canonicalRelation` 的两个注册点 + `catalog` 折叠回退 + `lower(guid COLLATE "default")` 函数索引迁移 `0.1/0020##meta_registry_guid_folded.sql`)。钉住它的有 `identifier_case_test.go` 的六个单测(含"注册表不知道就保持原样"、"一次折叠命中两个对象就报缺口而不挑一个"、DML 目标同样改写)、`02_test_join_lineage_table.yaml` 与 `06_test_insert_lineage_table.yaml` 的三条语料,以及集成测试 `TestMSSQLLineageRecordsTheRegisteredRelationRealServerIntegration`(真实 PostgreSQL + 真实 runner,不需要真实 SQL Server——分析不连实例)。**有意保留的边界**:①折叠跟随**数据库默认排序规则**,库默认排序规则是 C/C.UTF-8 时只折 ASCII(未命中退化为今天的行为,不会产生错误边);②引擎级近似——用 CS 排序规则的 SQL Server 库,只在精确未命中时才回退,且一次折叠命中多个对象时报缺口而不猜;③`AnalyzeSQL` 的 scope 由调用方给出,scope **自身**拼写的大小写不在折叠范围内(与语句无关)。
2. ~~**APPLICATION_SQL 的词法器**~~ — **已落地**。`sqlsanitize` 现在对 MSSQL 走 `mssql/parser` 的 lexer 分支(`tokenSpans` + `isLiteral` 的 `[`/`#`/`"` 规则 + `stripComments` 不再把 `#` 当注释),并用 `testdata/10_mssql.yaml` 与 MSSQL 语料回归钉住;细节见 [application-sql-ingestion.md](application-sql-ingestion.md)。
3. ~~**MERGE 的 WHEN 条件**~~ — **已落地**。`WHEN MATCHED AND …` / `WHEN NOT MATCHED AND …` 的条件现在以 FILTER 影响记在目标行上,由两条语料钉住。
4. ~~**PIVOT 的隐式分组列**~~ — **已落地**(按 catalog 能力分级):源关系列清单已知时列出未消费列并按 direct 带过,未知时只暴露 IN 值列。顺带修掉一个真实作用域缺陷:PIVOT/UNPIVOT 的源曾留在外层作用域,`SELECT *` 会把源表当第二个关系展开(语料 `12_test_pivot_lineage_table.yaml` 的 star expansion 两例钉住)。
5. **命名窗口跨语句不可见** — `WINDOW w AS …` 只在处理该查询期间有效(`namedWindows`),这是 SQL 的作用域,不是限制。
6. ~~**上游 `WITH` + DML 缺陷的本地绕行**~~ — **已落地**。`withclause.go` 恢复 CTE 列表(StarRocks `viewbody.go` 的同类做法),四种 DML 与"CTE 体变换透传"都由语料钉住;`withclause_test.go` 钉住边界判定与只挂 DML 两半。上游缺陷记录见 [omni-upstream-defects.md](omni-upstream-defects.md) §10.1。

## Where things live

| 关注点 | 代码 | 测试 / 门禁 |
| --- | --- | --- |
| 方言包 | `backend/plugin/lineage/mssql/{analyzer,expr,predicate,split,withclause}.go` | `go test ./backend/plugin/lineage/mssql/` |
| 装配 | `backend/plugin/lineage/engines/engines.go`、`engines_test.go` | `go test ./backend/plugin/lineage/engines/` |
| 共享机制守门 | `backend/plugin/lineage/shared_analysis_test.go`(`dialectPackages` 含 `mssql`) | `go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared` |
| 黄金语料 | `backend/plugin/lineage/mssql/testdata/analyze/`(14 套 / 136 例) | `TestAnalyzeYAML`、`TestCorpusIsFullyAnnotated` |
| 切分与别名复核 | `analyzer_test.go`(`TestSplitStatements`、`TestClauseKeywordAliasGuard`) | 同上 |
| APPLICATION_SQL 的写目标分类 | `backend/plugin/dmltarget/target.go`(MSSQL 分支:INSERT/UPDATE/DELETE/MERGE/BULK INSERT;表变量与 `#temp` 拒绝) | `go test ./backend/plugin/dmltarget/` |
| 跨库限定符 | `analyzer_test.go`(`TestCrossDatabaseQualifierIsReported`) | 同上 |
| 上游缺陷记录 | `.agents/docs/omni-upstream-defects.md` §10 | — |
| 端到端 | `backend/test/integration/runner/mssql_lineage_identifier_case_test.go`(手工注册 MSSQL 实例、`db` 行与注册表行;分析不连实例,所以不需要真实 SQL Server) | `make test-integration` 或 `go test -tags integration -run TestMSSQLLineageRecordsTheRegisteredRelationRealServerIntegration ./backend/test/integration/runner` |

不在本篇范围:transformation 模型的字段含义见 [lineage-semantics.md](lineage-semantics.md);装配与 runner 生命周期见 [lineage-analyzer.md](lineage-analyzer.md);上游缺陷与不修改政策见 [omni-upstream-defects.md](omni-upstream-defects.md)。
