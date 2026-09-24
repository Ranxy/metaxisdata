# 血缘分析器 AST 字段覆盖审计（完整对照表）

本文件是 [PostgreSQL 血缘包 review 记录](./postgresql_lineage_package_review.md) §0f 那条建议的落地：把「**AST 字段 × 分析器是否读取**」补全并归档，作为收口「某个 clause 没被读」这类缺陷的系统手段。review 记录负责「发现了什么、怎么修」，本文件负责「覆盖到什么程度、还有哪些字段没人读、哪些不读是有意的」。

> **修复状态（第十批，`3161563`）**：下面列出的 7 条缺口（A4–A10）连同此前只记录的 A3 已全部修复，语料钉子 30 例、改判 2 处既有期望、改名 1 例；本文件 §1–§7 的记录保持原样（它们描述的是修复前的状态），**§9 之后新增的"修复状态"一节**给出每一项落在哪一处。仍属未修的只有 §8 的待决 1（StarRocks `LoadDataDesc.SetExpr/Where`）与待决 3/4 两处口径选择。

审计对象：`backend/plugin/lineage/{postgresql,mysql,starrocks}` 三个分析器族（`tidb`/`mariadb` 与 `mysql` 逐字节同源——第二期起由 `mysql/gen` 从 `mysql/analyzer.go` 生成，见 `plan/lineage_mysql_family_generation_plan.md`——不重复列出）与 `{scope,algorithm,model,catalog}` 共享层；被审的 AST 是 `github.com/bytebase/omni`（pin `v0.0.0-20260912023254-4574e69bb9f1`）的 `pg/ast`、`mysql/ast`、`starrocks/ast`。

---

## 1. 范围、口径与规模

### 1.1 审计范围

**范围内** = 分析器实际触及的 AST 类型——即在生产代码里至少读过它一个字段的类型（含所有被 `switch` 分派的语句类型）。**这些类型的每一个字段都在下面的对照表里**，一行一个。

**范围外** = 其余类型（PG 179、MySQL 家族 191、StarRocks 163）。它们分成三类，逐类在 §6 说明：DDL/DCL/管理/事务（分析器 `default` 分支忽略）、表达式节点（由泛走覆盖）、omni 里存在但本 pin 的语法产不出来的节点。

### 1.2 三个维度是机器算出来的，不是读代码猜的

| 维度 | 来源 | 说明 |
| --- | --- | --- |
| **读**（✔） | `go/types` 类型检查分析器包，遍历全部 `SelectorExpr`，用 `types.Selection.Index()` 把每个字段读取**精确归属到声明它的结构体** | 不是按字段名 grep（`Name`、`Type` 这类名字在 AST 里到处都是，grep 会给出大量假阳性） |
| **泛走**（◐） | 解析 omni 的 walker（`walk_generated.go` / `walk_children.go`）的 `walkChildren` switch，标记每个被递归访问的字段 | 只说明「如果把这个节点交给 `Inspect`，字段内容会被访问到」；三个分析器的列提取都用 `Inspect`/`Nodes.Inspect`，所以表达式子树里的列总能被找到 |
| **判定** | 本轮人工审计：对每个未读字段给出类别与依据 | 类别代码见 §1.3。`!`（缺口）与 `T`（待决）是结论，其余是「确认无血缘语义」或「有记录的决策」 |

关键区分：**「没被显式读」不等于「没被处理」**。表达式节点的字段几乎都是 ◐（泛走），实测 `CASE`/`ARRAY[…]`/`COALESCE`/`IS NULL`/`CAST`/`BETWEEN`/`LIKE`/`EXTRACT`/`t.a = ANY(ARRAY[…])` 的列全部被提取（§7 有实测清单）。真正的风险只在**结构性字段**：分析器必须显式读取才能向下遍历，漏读就等于整块子树不可达。

### 1.3 类别代码

| 代码 | 含义 | 典型字段 |
| --- | --- | --- |
| ✔ | 生产代码读到了 | — |
| ◐ 泛走覆盖 | 字段本身是表达式/子节点，内容由 `Inspect` 找到 | `BoolExpr.Args`、`BinaryExpr.Left`、`UnaryExpr.Operand`、`A_Indirection.Indirection` |
| 位置/内部 | 位置、OID、typmod、解析器簿记，无列内容 | `Loc`、`*type`、`*collid`、`Rtindex` |
| 派生回填 | 解析后回填的字段，本仓库不消费它们 | `Ctecolnames`、`CreateViewStmt.SelectText`、`IntoClause.ViewQuery` |
| 语法修饰 | 只选择语法变体的开关，不携带列 | `IfNotExists`、`Ignore`、`Priority`、`DistinctKind` |
| 排序/分页/锁 | 影响行集或顺序，不产生列血缘（跨批既定） | `SortClause`、`Limit`、`LockingClause`、`WindowDef.Frame` |
| DDL/装载选项 | 建表/分区/存储/装载格式/安全选项 | `Options`、`Partitions`、`Definer`、`FieldsTerminatedBy` |
| 已验证 | 审计怀疑过并用真实引擎或语料确认「不读也不影响边」 | `DistinctClause`、`RangeTableSample.*`、`JoinExpr.JoinUsing` |
| 决策不建模 | 有显式决策记录（含语料钉子），不是遗漏 | `UpdateStmt.WhereClause`（P1-5）、`OnConflictClause.WhereClause`（§0d）、`*Returning`（MySQL 家族） |
| **!** **缺口** | 本轮审计新发现：结构性字段未读导致边/列名丢失 | 见 §3、§5 |
| T 待决 | 本轮没能定论（需引擎验证或需一次决策） | StarRocks `LoadDataDesc.SetExpr/Where` |

### 1.4 规模

| AST 包 | 结构类型 | 字段 | 范围类型 | 范围字段 | 已读 | 未读 |
| --- | --- | --- | --- | --- | --- | --- |
| `pg/ast` | 220 | 1126 | 41 | 235 | 101 | 134 |
| `mysql/ast` | 219 | 1104 | 28 | 201 | 74 | 127 |
| `tidb/ast` | 220 | 1103 | 28 | 194 | 74 | 120 |
| `mariadb/ast` | 223 | 1150 | 28 | 206 | 74 | 132 |
| `starrocks/ast` | 195 | 898 | 32 | 190 | 79 | 111 |

下表按 PG / MySQL 家族 / StarRocks 三族列出（`tidb`/`mariadb` 的字段与 `mysql` 几乎逐条相同，差异只是 AST 包名与极少数字段，故合并叙述）。三族共 626 行：已读 254，未读 372；未读的类别分布是 位置/内部 120、DDL/装载选项 86、语法修饰 55、排序/分页/锁 42、已验证 31、泛走 14、派生回填 8、决策不建模 8、**缺口 6**、待决 2。

---

## 2. 结论摘要

1. **PG 一侧没有发现新的缺口。** `pg/ast` 范围里 134 个未读字段全部可归入「位置/内部」「泛走」「语法修饰/排序」「已验证」「决策不建模」，其中 7 处是本轮**新验证为无问题**的（`DistinctClause`、`GroupDistinct`、`JoinUsing`、`SEARCH/CYCLE`、`Coldeflist`、`A_Indirection.Indirection`、`CopyStmt.Query`，见 §4）。
2. **缺口全部落在 MySQL 家族与 StarRocks，共 7 条**（表中 6 行 `!`：A4、A5、A6、A7、A8 两个字段；另加 A9 这条非 AST 字段的启发式缺陷与 A10 这个范围外类型的 FROM 项）。都是同一模式：**结构性字段没读，而语料只钉了「安全形状」**。PG 在多数情形已经做对（如命名窗口 P1-3），兄弟方言没有跟上——这与 review 记录跨批反复出现的「方言漂移」是同一现象。
3. **三条是静默丢边**（A4/A5/A7，引擎实测这些语句可执行且把值写进去了），**一条是有诊断的缺口**（A8），**一条丢的是谓词影响而不是列边**（A6），**一条丢的是派生列名**（A9）。
4. 表达式层面是安全的：**所有表达式节点都由泛走覆盖**，本轮对 15 个表达式形状做了实测（§7），没有例外。
5. 本轮**没有改任何代码、没有加语料钉子**（按「发现先记录」的约定）：给缺陷加 `expected_edges` 会把现状固化成预期。修哪几条、以什么顺序，见 §8。

### 2.1 新发现一览

| # | 方言 | 字段 | 现象 | 实测 |
| --- | --- | --- | --- | --- |
| **A4** | MySQL 家族 | `InsertStmt.SetList` | `INSERT INTO t SET a = (SELECT MAX(x) FROM other)` → **零边** | MySQL 8.3 **与** MariaDB 11.8.9 均执行成功并写入 7；同分析器的 `UPDATE t SET a = (SELECT …)` 有边（自相矛盾） |
| **A5** | MySQL 家族 | `ValuesStmt.Rows`、`SelectStmt.ValuesSource` | `VALUES ROW((SELECT MAX(x) FROM other))` 与 `SELECT * FROM (VALUES ROW((SELECT …))) v(a)` → **零边** | MySQL 8.3 两条都执行成功，分别返回 7 与 1,7；`VALUES ROW((SELECT …))` 单行也返回 7（代码注释「VALUES 语句只带字面量」是未验证的假设） |
| **A6** | StarRocks | `SelectStmt.Qualify` | `QUALIFY ROW_NUMBER() OVER (PARTITION BY t.b) = 1` 不产出任何 `t.b` 影响边 | 同分析器 `WHERE (SELECT …)` 产出 `other.x -> __result__.`、`HAVING` 也产出；`QUALIFY` 是 StarRocks 2.5+ 文档特性（[窗口函数文档](https://docs.starrocks.io/docs/sql-reference/sql-functions/Window_function/) QUALIFY 节） |
| **A7** | MySQL 家族 | `SelectStmt.WindowClause` | `SELECT row_number() OVER w AS r FROM t WINDOW w AS (PARTITION BY t.b)` → **零边**（内联 `OVER (PARTITION BY t.b)` 有 `t.b -> __result__.r`） | MySQL 8.3 与 MariaDB 11.8.9 均执行成功；PG 第二批已修 P1-3 并有语料 `35_test_named_window` |
| **A8** | MySQL 家族 | `InsertStmt.RowAlias`、`ColAliases` | 行别名 `VALUES (…) AS new ON DUPLICATE KEY UPDATE b = new.a` 解析不到，ODKU 那条边丢失 | MySQL 8.0.19+ 语法，8.3 实测可执行；**已有诊断**（`unresolved reference in an upsert assignment: new.a`），所以不是静默丢失 |
| **A9** | StarRocks | `inferColumnAlias`（非 AST 字段，由本审计发现） | 未取别名的复杂表达式派生列名被截断：`CASE … ELSE t.b END` → `bEND`、`t.x IS NULL` → `xISNULL`、`[t.a, t.b]` → `b]` | `starrocks/expr.go` 的启发式把「含点且不含括号」的表达式当成限定标识符取末段；MySQL 给的是完整表达式文本、PG 按自己的命名规则 |
| A3 | PG | `RangeTableFunc`（XMLTABLE） | `PASSING t.doc` 丢掉 | 第八批按决策只记录；**第十批已修**（XMLTABLE 注册为查询内关系，文档列名 + `PASSING` 文档表达式为源）|

---

## 3. 对照表

读列：`✔` 已读、`◐` 泛走覆盖、`—` 未读、`**!**` 缺口、`?` 待决。判定列见 §1.3。


### postgresql（`github.com/bytebase/omni/pg/ast`）

| 类型 | 字段 | 读 | 判定 | 说明 |
| --- | --- | --- | --- | --- |
| `A_Expr` | `Kind` | ✔ | — | — |
| `A_Expr` | `Lexpr` | ✔ | — | — |
| `A_Expr` | `Loc` | — | 位置/内部 | 位置 |
| `A_Expr` | `Name` | ✔ | — | — |
| `A_Expr` | `Rexpr` | ✔ | — | — |
| `A_Indirection` | `Arg` | ✔ | — | — |
| `A_Indirection` | `Indirection` | ◐ | 泛走覆盖 | 下标/字段路径；实测 `(t.arr)[t.id]` 两列都被提取 |
| `A_Indirection` | `Loc` | — | 位置/内部 | 位置 |
| `Alias` | `Aliasname` | ✔ | — | — |
| `Alias` | `Colnames` | ✔ | — | — |
| `Alias` | `Loc` | — | 位置/内部 | 位置 |
| `BoolExpr` | `Args` | ◐ | 泛走覆盖 | AND/OR 操作数由 Inspect 取列 |
| `BoolExpr` | `Boolop` | ✔ | — | — |
| `BoolExpr` | `Loc` | — | 位置/内部 | 位置 |
| `BooleanTest` | `Arg` | ◐ | 泛走覆盖 | IS TRUE/FALSE 的操作数由 Inspect 取列 |
| `BooleanTest` | `Booltesttype` | ✔ | — | — |
| `BooleanTest` | `Loc` | — | 位置/内部 | 位置 |
| `CoalesceExpr` | `Args` | ✔ | — | — |
| `CoalesceExpr` | `Coalescecollid` | — | 位置/内部 | 解析后 collation OID |
| `CoalesceExpr` | `Coalescetype` | — | 位置/内部 | 解析后类型 OID |
| `CoalesceExpr` | `Loc` | — | 位置/内部 | 位置 |
| `CollateClause` | `Arg` | ✔ | — | — |
| `CollateClause` | `Collname` | ◐ | 泛走覆盖 | 排序规则名不是列；Arg 已读（实测 `COLLATE "C"` 正常） |
| `CollateClause` | `Loc` | — | 位置/内部 | 位置 |
| `ColumnRef` | `Fields` | ✔ | — | — |
| `ColumnRef` | `Loc` | — | 位置/内部 | 位置 |
| `CommonTableExpr` | `Aliascolnames` | ✔ | — | — |
| `CommonTableExpr` | `Ctecolcollations` | — | 派生回填 | 回填的 collation OID |
| `CommonTableExpr` | `Ctecolnames` | — | 派生回填 | 解析后回填的输出列名；用户书写的 Aliascolnames 已读，实测 `WITH c(x) AS (SELECT a AS y …)` 解析正确 |
| `CommonTableExpr` | `Ctecoltypes` | — | 派生回填 | 回填的类型 OID |
| `CommonTableExpr` | `Ctecoltypmods` | — | 派生回填 | 回填的 typmod |
| `CommonTableExpr` | `Ctematerialized` | — | 语法修饰 | MATERIALIZED 提示 |
| `CommonTableExpr` | `Ctename` | ✔ | — | — |
| `CommonTableExpr` | `Ctequery` | ✔ | — | — |
| `CommonTableExpr` | `Cterecursive` | — | 位置/内部 | 解析后回填的递归标记 |
| `CommonTableExpr` | `Cterefcount` | — | 位置/内部 | 解析后回填的引用计数 |
| `CommonTableExpr` | `CycleClause` | — | 已验证 | CYCLE 子句 §0f 已验证 |
| `CommonTableExpr` | `Loc` | — | 位置/内部 | 位置 |
| `CommonTableExpr` | `SearchClause` | — | 已验证 | SEARCH 子句 §0f 已验证 |
| `CopyStmt` | `Attlist` | ✔ | — | — |
| `CopyStmt` | `Filename` | — | DDL/装载选项 | 文件/程序名 |
| `CopyStmt` | `InlineData` | — | DDL/装载选项 | 内联数据 |
| `CopyStmt` | `IsFrom` | ✔ | — | — |
| `CopyStmt` | `IsProgram` | — | DDL/装载选项 | PROGRAM 标记 |
| `CopyStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CopyStmt` | `Options` | — | DDL/装载选项 | COPY 选项 |
| `CopyStmt` | `Query` | — | 已验证 | `COPY (SELECT …) TO` 的目标不是关系，实证无血缘 |
| `CopyStmt` | `Relation` | ✔ | — | — |
| `CopyStmt` | `WhereClause` | — | 决策不建模 | 装载行过滤，按 P1-5 的 DML 行集规则不建模 |
| `CreateTableAsStmt` | `IfNotExists` | — | 语法修饰 | 语法修饰 |
| `CreateTableAsStmt` | `Into` | ✔ | — | — |
| `CreateTableAsStmt` | `IsSelectInto` | — | 派生回填 | 解析后回填（SELECT INTO 与 CTAS 同形） |
| `CreateTableAsStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CreateTableAsStmt` | `Objtype` | — | 语法修饰 | 对象类型（表/物化视图） |
| `CreateTableAsStmt` | `Query` | ✔ | — | — |
| `DeleteStmt` | `Loc` | — | 位置/内部 | 位置 |
| `DeleteStmt` | `Relation` | ✔ | — | — |
| `DeleteStmt` | `ReturningList` | ✔ | — | — |
| `DeleteStmt` | `UsingClause` | ✔ | — | — |
| `DeleteStmt` | `WhereClause` | ✔ | — | — |
| `DeleteStmt` | `WithClause` | ✔ | — | — |
| `FuncCall` | `AggDistinct` | — | 语法修饰 | DISTINCT 修饰（列来自 Args） |
| `FuncCall` | `AggFilter` | — | 已验证 | FILTER (WHERE …) §0f 已验证 |
| `FuncCall` | `AggOrder` | — | 排序/分页/锁 | 聚合内排序 |
| `FuncCall` | `AggStar` | ✔ | — | — |
| `FuncCall` | `AggWithinGroup` | — | 排序/分页/锁 | WITHIN GROUP 排序 |
| `FuncCall` | `Args` | ✔ | — | — |
| `FuncCall` | `FuncFormat` | — | 语法修饰 | COERCE_EXPLICIT/IMPLICIT 标记 |
| `FuncCall` | `FuncVariadic` | — | 语法修饰 | VARIADIC 标记 |
| `FuncCall` | `Funcname` | ✔ | — | — |
| `FuncCall` | `Loc` | — | 位置/内部 | 位置 |
| `FuncCall` | `Over` | ✔ | — | — |
| `GroupingSet` | `Content` | ✔ | — | — |
| `GroupingSet` | `Kind` | — | 已验证 | 组集种类；键由 Content 展开（本批 A2 已修） |
| `GroupingSet` | `Loc` | — | 位置/内部 | 位置 |
| `InsertStmt` | `Cols` | ✔ | — | — |
| `InsertStmt` | `Loc` | — | 位置/内部 | 位置 |
| `InsertStmt` | `OnConflictClause` | ✔ | — | — |
| `InsertStmt` | `Override` | — | 语法修饰 | OVERRIDING 子句 |
| `InsertStmt` | `Relation` | ✔ | — | — |
| `InsertStmt` | `ReturningList` | ✔ | — | — |
| `InsertStmt` | `SelectStmt` | ✔ | — | — |
| `InsertStmt` | `WithClause` | ✔ | — | — |
| `IntoClause` | `AccessMethod` | — | DDL/装载选项 | 存储访问方法 |
| `IntoClause` | `ColNames` | ✔ | — | — |
| `IntoClause` | `Loc` | — | 位置/内部 | 位置 |
| `IntoClause` | `OnCommit` | — | DDL/装载选项 | ON COMMIT 行为 |
| `IntoClause` | `Options` | — | DDL/装载选项 | WITH 选项 |
| `IntoClause` | `Rel` | ✔ | — | — |
| `IntoClause` | `SkipData` | — | DDL/装载选项 | WITH NO DATA |
| `IntoClause` | `TableSpaceName` | — | DDL/装载选项 | 表空间 |
| `IntoClause` | `ViewQuery` | — | 派生回填 | 解析后回填（CREATE VIEW 的查询经 ViewStmt.Query 处理） |
| `JoinExpr` | `Alias` | — | 语法修饰 | JOIN 别名 |
| `JoinExpr` | `IsNatural` | — | 语法修饰 | NATURAL 修饰 |
| `JoinExpr` | `JoinUsing` | — | 已验证 | USING 的列在 UsingClause（已读）；实测 USING 与 ON 边相同 |
| `JoinExpr` | `Jointype` | — | 已验证 | 连接类型不产生列边；实测 LEFT/INNER 无关 |
| `JoinExpr` | `Larg` | ✔ | — | — |
| `JoinExpr` | `Loc` | — | 位置/内部 | 位置 |
| `JoinExpr` | `Quals` | ✔ | — | — |
| `JoinExpr` | `Rarg` | ✔ | — | — |
| `JoinExpr` | `Rtindex` | — | 位置/内部 | 规划器 RT 索引 |
| `JoinExpr` | `UsingClause` | ✔ | — | — |
| `List` | `Items` | ✔ | — | — |
| `Loc` | `End` | ✔ | — | — |
| `Loc` | `Start` | ✔ | — | — |
| `MinMaxExpr` | `Args` | ✔ | — | — |
| `MinMaxExpr` | `Loc` | — | 位置/内部 | 位置 |
| `MinMaxExpr` | `Minmaxcollid` | — | 位置/内部 | 解析后 collation OID |
| `MinMaxExpr` | `Minmaxtype` | — | 位置/内部 | 解析后类型 OID |
| `MinMaxExpr` | `Op` | ✔ | — | — |
| `MultiAssignRef` | `Colno` | ✔ | — | — |
| `MultiAssignRef` | `Loc` | — | 位置/内部 | 位置 |
| `MultiAssignRef` | `Ncolumns` | — | 位置/内部 | 多列赋值的列数（Colno/Source 已读） |
| `MultiAssignRef` | `Source` | ✔ | — | — |
| `NullIfExpr` | `Args` | ✔ | — | — |
| `NullIfExpr` | `Inputcollid` | — | 位置/内部 | 解析后 collation OID |
| `NullIfExpr` | `Loc` | — | 位置/内部 | 位置 |
| `NullIfExpr` | `Opcollid` | — | 位置/内部 | 解析后 collation OID |
| `NullIfExpr` | `Opfuncid` | — | 位置/内部 | 解析后算子函数 OID |
| `NullIfExpr` | `Opno` | — | 位置/内部 | 解析后算子 OID |
| `NullIfExpr` | `Opresulttype` | — | 位置/内部 | 解析后类型 OID |
| `NullIfExpr` | `Opretset` | — | 位置/内部 | 解析后标记 |
| `NullTest` | `Arg` | ◐ | 泛走覆盖 | IS NULL 的操作数由 Inspect 取列（实测 `x IS NULL` 有边） |
| `NullTest` | `Argisrow` | — | 语法修饰 | 行级 IS NULL 标记（Arg 由泛走取列） |
| `NullTest` | `Loc` | — | 位置/内部 | 位置 |
| `NullTest` | `Nulltesttype` | ✔ | — | — |
| `OnConflictClause` | `Action` | — | 语法修饰 | DO NOTHING / DO UPDATE 的选择 |
| `OnConflictClause` | `Infer` | — | 语法修饰 | 冲突目标（索引推断），无列内容 |
| `OnConflictClause` | `Loc` | — | 位置/内部 | 位置 |
| `OnConflictClause` | `TargetList` | ✔ | — | — |
| `OnConflictClause` | `WhereClause` | — | 决策不建模 | §0d：冲突子句的 WHERE 是 UPDATE 行集，按 P1-5 不建模；语料已钉 |
| `RangeFunction` | `Alias` | ✔ | — | — |
| `RangeFunction` | `Coldeflist` | — | 已验证 | ROWS FROM 的列定义；实测 `ROWS FROM (unnest(t.arr)) AS u(a)` 与 `unnest(t.arr) AS u(a)` 都产出边且列名正确 |
| `RangeFunction` | `Functions` | ✔ | — | — |
| `RangeFunction` | `IsRowsfrom` | — | 语法修饰 | ROWS FROM 修饰 |
| `RangeFunction` | `Lateral` | — | 语法修饰 | LATERAL 修饰 |
| `RangeFunction` | `Loc` | — | 位置/内部 | 位置 |
| `RangeFunction` | `Ordinality` | ✔ | — | — |
| `RangeSubselect` | `Alias` | ✔ | — | — |
| `RangeSubselect` | `Lateral` | — | 语法修饰 | LATERAL 修饰 |
| `RangeSubselect` | `Loc` | — | 位置/内部 | 位置 |
| `RangeSubselect` | `Subquery` | ✔ | — | — |
| `RangeTableSample` | `Args` | — | 已验证 | TABLESAMPLE 参数 §0f 已验证 |
| `RangeTableSample` | `Loc` | — | 位置/内部 | 位置 |
| `RangeTableSample` | `Method` | — | 已验证 | TABLESAMPLE 方法 §0f 已验证 |
| `RangeTableSample` | `Relation` | ✔ | — | — |
| `RangeTableSample` | `Repeatable` | — | 已验证 | TABLESAMPLE 种子 §0f 已验证 |
| `RangeVar` | `Alias` | ✔ | — | — |
| `RangeVar` | `Catalogname` | — | 位置/内部 | 跨库限定，血缘标识符不含 catalog |
| `RangeVar` | `Inh` | — | 已验证 | ONLY / 继承展开不改变列来源 |
| `RangeVar` | `Loc` | — | 位置/内部 | 位置 |
| `RangeVar` | `Relname` | ✔ | — | — |
| `RangeVar` | `Relpersistence` | — | 语法修饰 | 临时/非日志表标记 |
| `RangeVar` | `Schemaname` | ✔ | — | — |
| `ResTarget` | `Indirection` | ◐ | 泛走覆盖 | 字段选择路径；实测 `(t.a).x` 正常 |
| `ResTarget` | `Loc` | — | 位置/内部 | 位置 |
| `ResTarget` | `Name` | ✔ | — | — |
| `ResTarget` | `Val` | ✔ | — | — |
| `RowExpr` | `Args` | ✔ | — | — |
| `RowExpr` | `Colnames` | — | 已验证 | ROW(… AS x) 的名字，列来自 Args |
| `RowExpr` | `Loc` | — | 位置/内部 | 位置 |
| `RowExpr` | `RowFormat` | — | 语法修饰 | 行格式标记 |
| `RowExpr` | `RowTypeid` | — | 位置/内部 | 行类型 OID |
| `SQLValueFunction` | `Loc` | — | 位置/内部 | 位置 |
| `SQLValueFunction` | `Op` | ✔ | — | — |
| `SQLValueFunction` | `Typmod` | — | 位置/内部 | 类型修饰 |
| `SelectStmt` | `All` | ✔ | — | — |
| `SelectStmt` | `DistinctClause` | — | 已验证 | DISTINCT ON 只影响行集，跨批已验证 |
| `SelectStmt` | `FromClause` | ✔ | — | — |
| `SelectStmt` | `GroupClause` | ✔ | — | — |
| `SelectStmt` | `GroupDistinct` | — | 已验证 | GROUP BY DISTINCT 去重分组，键由 GroupClause 提供 |
| `SelectStmt` | `HavingClause` | ✔ | — | — |
| `SelectStmt` | `IntoClause` | ✔ | — | — |
| `SelectStmt` | `Larg` | ✔ | — | — |
| `SelectStmt` | `LimitCount` | — | 排序/分页/锁 | 分页 |
| `SelectStmt` | `LimitOffset` | — | 排序/分页/锁 | 分页 |
| `SelectStmt` | `LimitOption` | — | 语法修饰 | LIMIT 变体 |
| `SelectStmt` | `Loc` | — | 位置/内部 | 位置 |
| `SelectStmt` | `LockingClause` | — | 排序/分页/锁 | FOR UPDATE 只锁行 |
| `SelectStmt` | `Op` | ✔ | — | — |
| `SelectStmt` | `Rarg` | ✔ | — | — |
| `SelectStmt` | `SortClause` | — | 排序/分页/锁 | 排序 |
| `SelectStmt` | `TargetList` | ✔ | — | — |
| `SelectStmt` | `ValuesLists` | ✔ | — | — |
| `SelectStmt` | `WhereClause` | ✔ | — | — |
| `SelectStmt` | `WindowClause` | ✔ | — | — |
| `SelectStmt` | `WithClause` | ✔ | — | — |
| `SortBy` | `Loc` | — | 位置/内部 | 位置 |
| `SortBy` | `Node` | ✔ | — | — |
| `SortBy` | `SortbyDir` | — | 排序/分页/锁 | 排序方向 |
| `SortBy` | `SortbyNulls` | — | 排序/分页/锁 | NULLS 位置 |
| `SortBy` | `UseOp` | — | 排序/分页/锁 | USING 算子排序 |
| `String` | `Str` | ✔ | — | — |
| `SubLink` | `Loc` | — | 位置/内部 | 位置 |
| `SubLink` | `OperName` | — | 已验证 | 算子名只影响文本渲染 |
| `SubLink` | `SubLinkId` | — | 位置/内部 | 解析后标识 |
| `SubLink` | `SubLinkType` | — | 已验证 | = ANY 两侧 FILTER 都在（§0f）；类型差异不改变边 |
| `SubLink` | `Subselect` | ✔ | — | — |
| `SubLink` | `Testexpr` | ✔ | — | — |
| `TypeCast` | `Arg` | ✔ | — | — |
| `TypeCast` | `Loc` | — | 位置/内部 | 位置 |
| `TypeCast` | `TypeName` | ✔ | — | — |
| `TypeName` | `ArrayBounds` | ◐ | 泛走覆盖 | 数组界表达式（非列） |
| `TypeName` | `Loc` | — | 位置/内部 | 位置 |
| `TypeName` | `Names` | ✔ | — | — |
| `TypeName` | `PctType` | — | 语法修饰 | 类型语法变体 |
| `TypeName` | `Setof` | — | 语法修饰 | SETOF 标记 |
| `TypeName` | `TypeOid` | — | 位置/内部 | 类型 OID |
| `TypeName` | `Typemod` | — | 位置/内部 | 类型修饰 |
| `TypeName` | `Typmods` | ◐ | 泛走覆盖 | 类型修饰表达式（非列） |
| `UpdateStmt` | `FromClause` | ✔ | — | — |
| `UpdateStmt` | `Loc` | — | 位置/内部 | 位置 |
| `UpdateStmt` | `Relation` | ✔ | — | — |
| `UpdateStmt` | `ReturningList` | ✔ | — | — |
| `UpdateStmt` | `TargetList` | ✔ | — | — |
| `UpdateStmt` | `WhereClause` | — | 决策不建模 | P1-5：DML 行集不建模（仅 SELECT 记谓词影响，DELETE 另有 __deletion__） |
| `UpdateStmt` | `WithClause` | ✔ | — | — |
| `ViewStmt` | `Aliases` | ✔ | — | — |
| `ViewStmt` | `Loc` | — | 位置/内部 | 位置 |
| `ViewStmt` | `Options` | — | DDL/装载选项 | 视图选项 |
| `ViewStmt` | `Query` | ✔ | — | — |
| `ViewStmt` | `Replace` | — | 语法修饰 | CREATE OR REPLACE |
| `ViewStmt` | `View` | ✔ | — | — |
| `ViewStmt` | `WithCheckOption` | — | DDL/装载选项 | CHECK OPTION |
| `WindowDef` | `EndOffset` | ✔ | — | — |
| `WindowDef` | `FrameOptions` | — | 排序/分页/锁 | 窗口帧选项（PartitionClause/OrderClause 已读） |
| `WindowDef` | `Loc` | — | 位置/内部 | 位置 |
| `WindowDef` | `Name` | ✔ | — | — |
| `WindowDef` | `OrderClause` | ✔ | — | — |
| `WindowDef` | `PartitionClause` | ✔ | — | — |
| `WindowDef` | `Refname` | ✔ | — | — |
| `WindowDef` | `StartOffset` | ✔ | — | — |
| `WithClause` | `Ctes` | ✔ | — | — |
| `WithClause` | `Loc` | — | 位置/内部 | 位置 |
| `WithClause` | `Recursive` | ✔ | — | — |

（postgresql：范围类型 41 个，已读字段 101，未读字段 134）

### mysql（`github.com/bytebase/omni/mysql/ast`）

| 类型 | 字段 | 读 | 判定 | 说明 |
| --- | --- | --- | --- | --- |
| `AlterViewStmt` | `Algorithm` | — | DDL/装载选项 | ALGORITHM |
| `AlterViewStmt` | `CheckOption` | — | DDL/装载选项 | CHECK OPTION |
| `AlterViewStmt` | `Columns` | ✔ | — | — |
| `AlterViewStmt` | `Definer` | — | DDL/装载选项 | DEFINER |
| `AlterViewStmt` | `Loc` | — | 位置/内部 | 位置 |
| `AlterViewStmt` | `Name` | ✔ | — | — |
| `AlterViewStmt` | `Select` | ✔ | — | — |
| `AlterViewStmt` | `SelectText` | — | 派生回填 | 与已解析的 Select 重复 |
| `AlterViewStmt` | `SqlSecurity` | — | DDL/装载选项 | SQL SECURITY |
| `Assignment` | `Column` | ✔ | — | — |
| `Assignment` | `Loc` | — | 位置/内部 | 位置 |
| `Assignment` | `Value` | ✔ | — | — |
| `BinaryExpr` | `Left` | ◐ | 泛走覆盖 | 算子两侧由 Inspect 取列 |
| `BinaryExpr` | `Loc` | — | 位置/内部 | 位置 |
| `BinaryExpr` | `Op` | ✔ | — | — |
| `BinaryExpr` | `OriginalOp` | — | 位置/内部 | 原始算子文本 |
| `BinaryExpr` | `Right` | ◐ | 泛走覆盖 | 同上 |
| `ColumnRef` | `Column` | ✔ | — | — |
| `ColumnRef` | `Loc` | — | 位置/内部 | 位置 |
| `ColumnRef` | `Schema` | ✔ | — | — |
| `ColumnRef` | `Star` | ✔ | — | — |
| `ColumnRef` | `Table` | ✔ | — | — |
| `CommonTableExpr` | `Columns` | ✔ | — | — |
| `CommonTableExpr` | `Loc` | — | 位置/内部 | 位置 |
| `CommonTableExpr` | `Name` | ✔ | — | — |
| `CommonTableExpr` | `Recursive` | ✔ | — | — |
| `CommonTableExpr` | `Select` | ✔ | — | — |
| `CreateTableStmt` | `Columns` | — | DDL/装载选项 | 建表列定义（CTAS 走 AsSelect） |
| `CreateTableStmt` | `Constraints` | — | DDL/装载选项 | 约束 |
| `CreateTableStmt` | `IfNotExists` | — | 语法修饰 | 语法修饰 |
| `CreateTableStmt` | `Ignore` | — | 语法修饰 | 语法修饰 |
| `CreateTableStmt` | `Like` | — | DDL/装载选项 | CREATE TABLE LIKE |
| `CreateTableStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CreateTableStmt` | `Options` | — | DDL/装载选项 | 表选项 |
| `CreateTableStmt` | `Partitions` | — | DDL/装载选项 | 分区 |
| `CreateTableStmt` | `Replace` | — | 语法修饰 | REPLACE |
| `CreateTableStmt` | `Select` | ✔ | — | — |
| `CreateTableStmt` | `Table` | ✔ | — | — |
| `CreateTableStmt` | `Temporary` | — | 语法修饰 | TEMPORARY |
| `CreateViewStmt` | `Algorithm` | — | DDL/装载选项 | ALGORITHM |
| `CreateViewStmt` | `CheckOption` | — | DDL/装载选项 | CHECK OPTION |
| `CreateViewStmt` | `Columns` | ✔ | — | — |
| `CreateViewStmt` | `Definer` | — | DDL/装载选项 | DEFINER |
| `CreateViewStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CreateViewStmt` | `Name` | ✔ | — | — |
| `CreateViewStmt` | `OrReplace` | — | 语法修饰 | OR REPLACE |
| `CreateViewStmt` | `Select` | ✔ | — | — |
| `CreateViewStmt` | `SelectText` | — | 派生回填 | 与已解析的 Select 重复（分析用 Select） |
| `CreateViewStmt` | `SqlSecurity` | — | DDL/装载选项 | SQL SECURITY |
| `DeleteStmt` | `Ignore` | — | 语法修饰 | DELETE IGNORE |
| `DeleteStmt` | `Limit` | — | 排序/分页/锁 | 分页 |
| `DeleteStmt` | `Loc` | — | 位置/内部 | 位置 |
| `DeleteStmt` | `LowPriority` | — | 语法修饰 | LOW_PRIORITY |
| `DeleteStmt` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `DeleteStmt` | `Quick` | — | 语法修饰 | DELETE QUICK |
| `DeleteStmt` | `Tables` | ✔ | — | — |
| `DeleteStmt` | `Using` | ✔ | — | — |
| `DeleteStmt` | `Where` | ✔ | — | — |
| `ExistsExpr` | `Loc` | — | 位置/内部 | 位置 |
| `ExistsExpr` | `Select` | ✔ | — | — |
| `FuncCallExpr` | `Args` | ✔ | — | — |
| `FuncCallExpr` | `Distinct` | — | 语法修饰 | 聚合 DISTINCT（列来自 Args） |
| `FuncCallExpr` | `HasParens` | — | 语法修饰 | 括号形态 |
| `FuncCallExpr` | `Loc` | — | 位置/内部 | 位置 |
| `FuncCallExpr` | `Name` | ✔ | — | — |
| `FuncCallExpr` | `OrderBy` | — | 排序/分页/锁 | 聚合内排序 |
| `FuncCallExpr` | `Over` | ✔ | — | — |
| `FuncCallExpr` | `Schema` | — | 位置/内部 | 函数限定符 |
| `FuncCallExpr` | `Separator` | — | 排序/分页/锁 | GROUP_CONCAT 分隔符 |
| `FuncCallExpr` | `Star` | — | 已验证 | `COUNT(*)` 实测产出 `t.*` |
| `InExpr` | `Expr` | ✔ | — | — |
| `InExpr` | `List` | ✔ | — | — |
| `InExpr` | `Loc` | — | 位置/内部 | 位置 |
| `InExpr` | `Not` | — | 语法修饰 | NOT IN 修饰 |
| `InExpr` | `Select` | ✔ | — | — |
| `InsertStmt` | `ColAliases` | **!** | **缺口** | A8：同上（`AS new(m)` 的列别名） |
| `InsertStmt` | `Columns` | ✔ | — | — |
| `InsertStmt` | `Ignore` | — | 语法修饰 | INSERT IGNORE |
| `InsertStmt` | `IsReplace` | ✔ | — | — |
| `InsertStmt` | `Loc` | — | 位置/内部 | 位置 |
| `InsertStmt` | `OnDuplicateKey` | ✔ | — | — |
| `InsertStmt` | `Partitions` | — | DDL/装载选项 | 分区选择 |
| `InsertStmt` | `Priority` | — | 语法修饰 | INSERT 优先级 |
| `InsertStmt` | `Returning` | — | 决策不建模 | MySQL 家族不建模 RETURNING（既定） |
| `InsertStmt` | `RowAlias` | **!** | **缺口** | A8：行别名 `VALUES (…) AS new` 在 ODKU 里解析不到（有诊断，非静默）；MySQL 8.0.19+ |
| `InsertStmt` | `Select` | ✔ | — | — |
| `InsertStmt` | `SetList` | **!** | **缺口** | A4：`INSERT … SET a = (SELECT …)` 零边（MySQL 8.3 与 MariaDB 11.8.9 实测可执行）；语料只钉了常量形态 |
| `InsertStmt` | `Table` | ✔ | — | — |
| `InsertStmt` | `TableSource` | ✔ | — | — |
| `InsertStmt` | `Values` | ✔ | — | — |
| `JoinClause` | `Condition` | ✔ | — | — |
| `JoinClause` | `Left` | ✔ | — | — |
| `JoinClause` | `Loc` | — | 位置/内部 | 位置 |
| `JoinClause` | `Right` | ✔ | — | — |
| `JoinClause` | `Type` | — | 已验证 | 连接类型不产生列边 |
| `List` | `Items` | ✔ | — | — |
| `LoadDataStmt` | `CharacterSet` | — | DDL/装载选项 | 字符集 |
| `LoadDataStmt` | `Columns` | ✔ | — | — |
| `LoadDataStmt` | `Concurrent` | — | DDL/装载选项 | 并发装载 |
| `LoadDataStmt` | `FieldsEnclosedBy` | — | DDL/装载选项 | 字段包围符 |
| `LoadDataStmt` | `FieldsEscapedBy` | — | DDL/装载选项 | 转义符 |
| `LoadDataStmt` | `FieldsOptionalEncl` | — | DDL/装载选项 | OPTIONALLY |
| `LoadDataStmt` | `FieldsTerminatedBy` | — | DDL/装载选项 | 字段分隔符 |
| `LoadDataStmt` | `FromS3` | — | DDL/装载选项 | S3 装载 |
| `LoadDataStmt` | `Ignore` | — | DDL/装载选项 | IGNORE 标记 |
| `LoadDataStmt` | `IgnoreRows` | — | DDL/装载选项 | 跳过行数 |
| `LoadDataStmt` | `Infile` | — | DDL/装载选项 | 输入文件 |
| `LoadDataStmt` | `IsXML` | — | DDL/装载选项 | XML 装载 |
| `LoadDataStmt` | `LinesStartingBy` | — | DDL/装载选项 | 行前缀 |
| `LoadDataStmt` | `LinesTerminatedBy` | — | DDL/装载选项 | 行分隔符 |
| `LoadDataStmt` | `Loc` | — | 位置/内部 | 位置 |
| `LoadDataStmt` | `Local` | — | DDL/装载选项 | LOCAL 标记 |
| `LoadDataStmt` | `LowPriority` | — | DDL/装载选项 | 优先级 |
| `LoadDataStmt` | `Partitions` | — | DDL/装载选项 | 分区选择 |
| `LoadDataStmt` | `Replace` | — | DDL/装载选项 | REPLACE 标记 |
| `LoadDataStmt` | `RowsIdentifiedBy` | — | DDL/装载选项 | 行标识 |
| `LoadDataStmt` | `S3Kind` | — | DDL/装载选项 | S3 变体 |
| `LoadDataStmt` | `S3URI` | — | DDL/装载选项 | S3 URI |
| `LoadDataStmt` | `SetList` | ✔ | — | — |
| `LoadDataStmt` | `Table` | ✔ | — | — |
| `Loc` | `End` | ✔ | — | — |
| `Loc` | `Start` | ✔ | — | — |
| `OnCondition` | `Expr` | ✔ | — | — |
| `OnCondition` | `Loc` | — | 位置/内部 | 位置 |
| `OrderByItem` | `Desc` | — | 排序/分页/锁 | 排序方向 |
| `OrderByItem` | `Direction` | — | 排序/分页/锁 | 排序方向 |
| `OrderByItem` | `Expr` | ✔ | — | — |
| `OrderByItem` | `Loc` | — | 位置/内部 | 位置 |
| `OrderByItem` | `NullsFirst` | — | 排序/分页/锁 | NULLS 位置 |
| `ParenExpr` | `Expr` | ✔ | — | — |
| `ParenExpr` | `Loc` | — | 位置/内部 | 位置 |
| `ResTarget` | `Loc` | — | 位置/内部 | 位置 |
| `ResTarget` | `Name` | ✔ | — | — |
| `ResTarget` | `Val` | ✔ | — | — |
| `SelectStmt` | `BigResult` | — | 语法修饰 | SQL_BIG_RESULT |
| `SelectStmt` | `BufferResult` | — | 语法修饰 | SQL_BUFFER_RESULT |
| `SelectStmt` | `CTEs` | ✔ | — | — |
| `SelectStmt` | `CalcFoundRows` | — | 语法修饰 | SQL_CALC_FOUND_ROWS |
| `SelectStmt` | `DistinctKind` | — | 已验证 | DISTINCT / DISTINCTROW 只影响行集 |
| `SelectStmt` | `ForUpdate` | — | 排序/分页/锁 | 行锁 |
| `SelectStmt` | `From` | ✔ | — | — |
| `SelectStmt` | `GroupBy` | ✔ | — | — |
| `SelectStmt` | `Having` | ✔ | — | — |
| `SelectStmt` | `HighPriority` | — | 语法修饰 | HIGH_PRIORITY |
| `SelectStmt` | `Into` | — | 已验证 | INTO OUTFILE 的目标不是关系 |
| `SelectStmt` | `Left` | ✔ | — | — |
| `SelectStmt` | `Limit` | — | 排序/分页/锁 | 分页 |
| `SelectStmt` | `Loc` | — | 位置/内部 | 位置 |
| `SelectStmt` | `NoCache` | — | 语法修饰 | SQL_NO_CACHE |
| `SelectStmt` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `SelectStmt` | `OrderByWithRollup` | — | 排序/分页/锁 | 排序 |
| `SelectStmt` | `ParenSource` | ✔ | — | — |
| `SelectStmt` | `Right` | ✔ | — | — |
| `SelectStmt` | `SetAll` | ✔ | — | — |
| `SelectStmt` | `SetOp` | ✔ | — | — |
| `SelectStmt` | `SmallResult` | — | 语法修饰 | SQL_SMALL_RESULT |
| `SelectStmt` | `StraightJoin` | — | 语法修饰 | STRAIGHT_JOIN 提示 |
| `SelectStmt` | `TableSource` | — | 已验证 | 查询原语形态 omni 解析报错（`TABLE t UNION ALL …`）；语句形态经 TableStmt 处理，实测有边 |
| `SelectStmt` | `TargetList` | ✔ | — | — |
| `SelectStmt` | `ValuesSource` | **!** | **缺口** | A5：`VALUES ROW((SELECT …))` 与派生表形态零边（MySQL 8.3 实测可执行） |
| `SelectStmt` | `Where` | ✔ | — | — |
| `SelectStmt` | `WindowClause` | **!** | **缺口** | A7：命名窗口 `WINDOW w AS (PARTITION BY b)` 零边（MySQL 8.3 / MariaDB 11.8.9 实测可执行）；PG 有同名语料 |
| `SelectStmt` | `WithRollup` | — | 已验证 | ROLLUP 只影响分组行集 |
| `SubqueryExpr` | `Alias` | ✔ | — | — |
| `SubqueryExpr` | `Columns` | ✔ | — | — |
| `SubqueryExpr` | `Exists` | — | 已验证 | EXISTS 子查询的过滤列不算血缘（批 4 既定） |
| `SubqueryExpr` | `Lateral` | — | 语法修饰 | LATERAL 修饰 |
| `SubqueryExpr` | `Loc` | — | 位置/内部 | 位置 |
| `SubqueryExpr` | `Quantifier` | — | 已验证 | ANY/ALL 语义；= ANY 两侧 FILTER 已验证 |
| `SubqueryExpr` | `Select` | ✔ | — | — |
| `TableRef` | `Alias` | ✔ | — | — |
| `TableRef` | `IndexHints` | — | 排序/分页/锁 | 索引提示 |
| `TableRef` | `Loc` | — | 位置/内部 | 位置 |
| `TableRef` | `Name` | ✔ | — | — |
| `TableRef` | `Partitions` | — | DDL/装载选项 | 分区选择 |
| `TableRef` | `Schema` | ✔ | — | — |
| `TableStmt` | `Into` | — | 已验证 | INTO OUTFILE 的目标不是关系 |
| `TableStmt` | `Limit` | — | 排序/分页/锁 | 分页 |
| `TableStmt` | `Loc` | — | 位置/内部 | 位置 |
| `TableStmt` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `TableStmt` | `Table` | ✔ | — | — |
| `UnaryExpr` | `Loc` | — | 位置/内部 | 位置 |
| `UnaryExpr` | `Op` | ✔ | — | — |
| `UnaryExpr` | `Operand` | ◐ | 泛走覆盖 | 操作数由 Inspect 取列 |
| `UnaryExpr` | `OriginalOp` | — | 位置/内部 | 原始算子文本 |
| `UpdateStmt` | `Ignore` | — | 语法修饰 | UPDATE IGNORE |
| `UpdateStmt` | `Limit` | — | 排序/分页/锁 | 分页 |
| `UpdateStmt` | `Loc` | — | 位置/内部 | 位置 |
| `UpdateStmt` | `LowPriority` | — | 语法修饰 | LOW_PRIORITY |
| `UpdateStmt` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `UpdateStmt` | `SetList` | ✔ | — | — |
| `UpdateStmt` | `Tables` | ✔ | — | — |
| `UpdateStmt` | `Where` | — | 决策不建模 | P1-5：DML 行集不建模（DELETE 的 WHERE 建模，实测不对称） |
| `UsingCondition` | `Columns` | ✔ | — | — |
| `UsingCondition` | `Loc` | — | 位置/内部 | 位置 |
| `WindowDef` | `Frame` | — | 排序/分页/锁 | 窗口帧 |
| `WindowDef` | `Loc` | — | 位置/内部 | 位置 |
| `WindowDef` | `Name` | — | 语法修饰 | 被定义的窗口名 |
| `WindowDef` | `OrderBy` | ✔ | — | — |
| `WindowDef` | `PartitionBy` | ✔ | — | — |
| `WindowDef` | `RefName` | — | 决策不建模 | 命名窗口引用；同 A7 |

（mysql：范围类型 28 个，已读字段 74，未读字段 127）

### starrocks（`github.com/bytebase/omni/starrocks/ast`）

| 类型 | 字段 | 读 | 判定 | 说明 |
| --- | --- | --- | --- | --- |
| `AlterViewStmt` | `Columns` | ✔ | — | — |
| `AlterViewStmt` | `Loc` | — | 位置/内部 | 位置 |
| `AlterViewStmt` | `Name` | ✔ | — | — |
| `AlterViewStmt` | `Query` | ✔ | — | — |
| `Assignment` | `Column` | ✔ | — | — |
| `Assignment` | `Loc` | — | 位置/内部 | 位置 |
| `Assignment` | `Value` | ✔ | — | — |
| `BinaryExpr` | `Left` | ◐ | 泛走覆盖 | 算子两侧由 Inspect 取列 |
| `BinaryExpr` | `Loc` | — | 位置/内部 | 位置 |
| `BinaryExpr` | `Op` | ✔ | — | — |
| `BinaryExpr` | `Right` | ◐ | 泛走覆盖 | 同上 |
| `CTE` | `Columns` | ✔ | — | — |
| `CTE` | `Loc` | — | 位置/内部 | 位置 |
| `CTE` | `Name` | ✔ | — | — |
| `CTE` | `Query` | ✔ | — | — |
| `ColumnRef` | `Loc` | — | 位置/内部 | 位置 |
| `ColumnRef` | `Name` | ✔ | — | — |
| `CopyIntoStmt` | `Files` | — | DDL/装载选项 | 文件列表 |
| `CopyIntoStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CopyIntoStmt` | `Pattern` | — | DDL/装载选项 | 文件模式 |
| `CopyIntoStmt` | `Properties` | — | DDL/装载选项 | 导入属性 |
| `CopyIntoStmt` | `Source` | — | DDL/装载选项 | 外部 stage 源（目标不是列来源） |
| `CopyIntoStmt` | `Target` | ✔ | — | — |
| `CreateMTMVStmt` | `BuildMode` | — | DDL/装载选项 | 构建模式 |
| `CreateMTMVStmt` | `Columns` | ✔ | — | — |
| `CreateMTMVStmt` | `Comment` | — | DDL/装载选项 | 注释 |
| `CreateMTMVStmt` | `DistributedBy` | — | DDL/装载选项 | 分桶 |
| `CreateMTMVStmt` | `IfNotExists` | — | 语法修饰 | 语法修饰 |
| `CreateMTMVStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CreateMTMVStmt` | `Name` | ✔ | — | — |
| `CreateMTMVStmt` | `PartitionBy` | — | DDL/装载选项 | 分区 |
| `CreateMTMVStmt` | `Properties` | — | DDL/装载选项 | 属性 |
| `CreateMTMVStmt` | `Query` | ✔ | — | — |
| `CreateMTMVStmt` | `RefreshMethod` | — | DDL/装载选项 | 刷新方式 |
| `CreateMTMVStmt` | `RefreshTrigger` | — | DDL/装载选项 | 刷新触发 |
| `CreateTableStmt` | `AsSelect` | ✔ | — | — |
| `CreateTableStmt` | `CTASColumns` | ✔ | — | — |
| `CreateTableStmt` | `Columns` | — | DDL/装载选项 | 建表列定义（CTAS 走 AsSelect） |
| `CreateTableStmt` | `Comment` | — | DDL/装载选项 | 注释 |
| `CreateTableStmt` | `Constraints` | — | DDL/装载选项 | 约束 |
| `CreateTableStmt` | `DistributedBy` | — | DDL/装载选项 | 分桶 |
| `CreateTableStmt` | `Engine` | — | DDL/装载选项 | 引擎 |
| `CreateTableStmt` | `External` | — | DDL/装载选项 | 外部表 |
| `CreateTableStmt` | `IfNotExists` | — | 语法修饰 | 语法修饰 |
| `CreateTableStmt` | `Indexes` | — | DDL/装载选项 | 索引 |
| `CreateTableStmt` | `KeyDesc` | — | DDL/装载选项 | 排序键 |
| `CreateTableStmt` | `Like` | — | DDL/装载选项 | CREATE TABLE LIKE |
| `CreateTableStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CreateTableStmt` | `Name` | ✔ | — | — |
| `CreateTableStmt` | `PartitionBy` | — | DDL/装载选项 | 分区 |
| `CreateTableStmt` | `Properties` | — | DDL/装载选项 | 属性 |
| `CreateTableStmt` | `Rollup` | — | DDL/装载选项 | 物化视图/rollup |
| `CreateTableStmt` | `Temporary` | — | 语法修饰 | TEMPORARY |
| `CreateViewStmt` | `Columns` | ✔ | — | — |
| `CreateViewStmt` | `Comment` | — | DDL/装载选项 | 注释 |
| `CreateViewStmt` | `IfNotExists` | — | 语法修饰 | 语法修饰 |
| `CreateViewStmt` | `Loc` | — | 位置/内部 | 位置 |
| `CreateViewStmt` | `Name` | ✔ | — | — |
| `CreateViewStmt` | `OrReplace` | — | 语法修饰 | OR REPLACE |
| `CreateViewStmt` | `Query` | ✔ | — | — |
| `DeleteStmt` | `Loc` | — | 位置/内部 | 位置 |
| `DeleteStmt` | `Partition` | — | DDL/装载选项 | 分区选择 |
| `DeleteStmt` | `Target` | ✔ | — | — |
| `DeleteStmt` | `TargetAlias` | ✔ | — | — |
| `DeleteStmt` | `Using` | ✔ | — | — |
| `DeleteStmt` | `Where` | ✔ | — | — |
| `DeleteStmt` | `With` | ✔ | — | — |
| `File` | `Loc` | — | 位置/内部 | 位置 |
| `File` | `Stmts` | ✔ | — | — |
| `FuncCallExpr` | `Args` | ✔ | — | — |
| `FuncCallExpr` | `Distinct` | — | 语法修饰 | 聚合 DISTINCT（列来自 Args） |
| `FuncCallExpr` | `IgnoreNulls` | — | 语法修饰 | IGNORE NULLS |
| `FuncCallExpr` | `Loc` | — | 位置/内部 | 位置 |
| `FuncCallExpr` | `Name` | ✔ | — | — |
| `FuncCallExpr` | `OrderBy` | — | 排序/分页/锁 | 聚合内排序 |
| `FuncCallExpr` | `Over` | ✔ | — | — |
| `FuncCallExpr` | `Separator` | — | 排序/分页/锁 | GROUP_CONCAT 分隔符 |
| `FuncCallExpr` | `Star` | — | 已验证 | `COUNT(*)` 实测产出 `t.*` |
| `InsertStmt` | `ByName` | ✔ | — | — |
| `InsertStmt` | `Columns` | ✔ | — | — |
| `InsertStmt` | `FileTarget` | — | 已验证 | INSERT INTO FILES 的目标是文件 |
| `InsertStmt` | `Label` | — | DDL/装载选项 | WITH LABEL |
| `InsertStmt` | `Loc` | — | 位置/内部 | 位置 |
| `InsertStmt` | `Overwrite` | — | 语法修饰 | INSERT OVERWRITE |
| `InsertStmt` | `Partition` | — | DDL/装载选项 | 分区选择 |
| `InsertStmt` | `PartitionStar` | — | DDL/装载选项 | PARTITION(*) |
| `InsertStmt` | `Query` | ✔ | — | — |
| `InsertStmt` | `Target` | ✔ | — | — |
| `InsertStmt` | `TempPartition` | — | DDL/装载选项 | 临时分区 |
| `InsertStmt` | `Values` | ✔ | — | — |
| `JoinClause` | `Hints` | — | 排序/分页/锁 | 执行提示（shuffle/broadcast） |
| `JoinClause` | `Left` | ✔ | — | — |
| `JoinClause` | `Loc` | — | 位置/内部 | 位置 |
| `JoinClause` | `Natural` | — | 语法修饰 | NATURAL 修饰 |
| `JoinClause` | `On` | ✔ | — | — |
| `JoinClause` | `Right` | ✔ | — | — |
| `JoinClause` | `Type` | — | 已验证 | 连接类型不产生列边 |
| `JoinClause` | `Using` | ✔ | — | — |
| `LoadDataDesc` | `ColumnList` | ✔ | — | — |
| `LoadDataDesc` | `ColumnsFromPath` | — | DDL/装载选项 | 从路径取列 |
| `LoadDataDesc` | `Format` | — | DDL/装载选项 | 文件格式 |
| `LoadDataDesc` | `Loc` | — | 位置/内部 | 位置 |
| `LoadDataDesc` | `Negative` | — | DDL/装载选项 | NEGATIVE 修饰 |
| `LoadDataDesc` | `Partition` | — | DDL/装载选项 | 分区选择 |
| `LoadDataDesc` | `SetExpr` | ? | 待决 | SET 以原始文本保存，未回解析；需先确认语法可达性再决定是否建模 |
| `LoadDataDesc` | `SourceFiles` | — | DDL/装载选项 | 数据文件 |
| `LoadDataDesc` | `Target` | ✔ | — | — |
| `LoadDataDesc` | `Where` | ? | 待决 | 同上（原始文本） |
| `LoadDataStmt` | `BrokerName` | — | DDL/装载选项 | broker |
| `LoadDataStmt` | `Comment` | — | DDL/装载选项 | 注释 |
| `LoadDataStmt` | `DataDescs` | ✔ | — | — |
| `LoadDataStmt` | `Label` | — | DDL/装载选项 | 标签 |
| `LoadDataStmt` | `Loc` | — | 位置/内部 | 位置 |
| `LoadDataStmt` | `Properties` | — | DDL/装载选项 | 属性 |
| `Loc` | `End` | ✔ | — | — |
| `Loc` | `Start` | ✔ | — | — |
| `ObjectName` | `Loc` | — | 位置/内部 | 位置 |
| `ObjectName` | `Parts` | ✔ | — | — |
| `OrderByItem` | `Desc` | — | 排序/分页/锁 | 排序方向 |
| `OrderByItem` | `Expr` | ✔ | — | — |
| `OrderByItem` | `Loc` | — | 位置/内部 | 位置 |
| `OrderByItem` | `NullsFirst` | — | 排序/分页/锁 | NULLS 位置 |
| `ParenExpr` | `Expr` | ✔ | — | — |
| `ParenExpr` | `Loc` | — | 位置/内部 | 位置 |
| `ParenSelect` | `Limit` | — | 排序/分页/锁 | 分页 |
| `ParenSelect` | `Loc` | — | 位置/内部 | 位置 |
| `ParenSelect` | `Offset` | — | 排序/分页/锁 | 分页 |
| `ParenSelect` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `ParenSelect` | `Sel` | ✔ | — | — |
| `RawQuery` | `Loc` | — | 位置/内部 | 位置 |
| `RawQuery` | `RawText` | ✔ | — | — |
| `RawQuery` | `TextStart` | — | 位置/内部 | 原文偏移 |
| `SelectItem` | `Alias` | ✔ | — | — |
| `SelectItem` | `Aliased` | ✔ | — | — |
| `SelectItem` | `ExceptColumns` | ✔ | — | — |
| `SelectItem` | `Expr` | ✔ | — | — |
| `SelectItem` | `Loc` | — | 位置/内部 | 位置 |
| `SelectItem` | `Star` | ✔ | — | — |
| `SelectItem` | `TableName` | ✔ | — | — |
| `SelectStmt` | `All` | — | 语法修饰 | ALL 修饰 |
| `SelectStmt` | `Distinct` | — | 已验证 | DISTINCT 只影响行集 |
| `SelectStmt` | `From` | ✔ | — | — |
| `SelectStmt` | `GroupBy` | ✔ | — | — |
| `SelectStmt` | `Having` | ✔ | — | — |
| `SelectStmt` | `Into` | — | 已验证 | INTO OUTFILE 的目标不是关系 |
| `SelectStmt` | `Items` | ✔ | — | — |
| `SelectStmt` | `Limit` | — | 排序/分页/锁 | 分页 |
| `SelectStmt` | `Loc` | — | 位置/内部 | 位置 |
| `SelectStmt` | `Offset` | — | 排序/分页/锁 | 分页 |
| `SelectStmt` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `SelectStmt` | `Qualify` | **!** | **缺口** | A6：QUALIFY 完全不读——谓词影响边与子查询均丢失（StarRocks 2.5+ 文档特性；同分析器 WHERE/HAVING 都建模） |
| `SelectStmt` | `Where` | ✔ | — | — |
| `SelectStmt` | `With` | ✔ | — | — |
| `SetOpStmt` | `All` | ✔ | — | — |
| `SetOpStmt` | `Left` | ✔ | — | — |
| `SetOpStmt` | `Limit` | — | 排序/分页/锁 | 分页 |
| `SetOpStmt` | `Loc` | — | 位置/内部 | 位置 |
| `SetOpStmt` | `Offset` | — | 排序/分页/锁 | 分页 |
| `SetOpStmt` | `Op` | ✔ | — | — |
| `SetOpStmt` | `OrderBy` | — | 排序/分页/锁 | 排序 |
| `SetOpStmt` | `Right` | ✔ | — | — |
| `SubqueryExpr` | `Loc` | — | 位置/内部 | 位置 |
| `SubqueryExpr` | `RawText` | ✔ | — | — |
| `SubqueryExpr` | `TextStart` | — | 位置/内部 | 原文偏移 |
| `TableRef` | `Alias` | ✔ | — | — |
| `TableRef` | `Loc` | — | 位置/内部 | 位置 |
| `TableRef` | `Name` | ✔ | — | — |
| `TableRef` | `Subquery` | ✔ | — | — |
| `TableRef` | `TabletIDs` | — | DDL/装载选项 | 物理 tablet 选择 |
| `UnaryExpr` | `Expr` | ◐ | 泛走覆盖 | 操作数由 Inspect 取列 |
| `UnaryExpr` | `Loc` | — | 位置/内部 | 位置 |
| `UnaryExpr` | `Op` | ✔ | — | — |
| `UpdateStmt` | `Assignments` | ✔ | — | — |
| `UpdateStmt` | `From` | ✔ | — | — |
| `UpdateStmt` | `Loc` | — | 位置/内部 | 位置 |
| `UpdateStmt` | `Target` | ✔ | — | — |
| `UpdateStmt` | `TargetAlias` | ✔ | — | — |
| `UpdateStmt` | `Where` | — | 决策不建模 | P1-5：DML 行集不建模（DELETE 的 WHERE 建模，实测不对称） |
| `UpdateStmt` | `With` | ✔ | — | — |
| `ViewColumn` | `Comment` | — | DDL/装载选项 | 列注释 |
| `ViewColumn` | `Loc` | — | 位置/内部 | 位置 |
| `ViewColumn` | `Name` | ✔ | — | — |
| `WindowSpec` | `Frame` | — | 排序/分页/锁 | 窗口帧 |
| `WindowSpec` | `Loc` | — | 位置/内部 | 位置 |
| `WindowSpec` | `Name` | — | 决策不建模 | 命名窗口引用；omni 的 StarRocks AST 没有 `SelectStmt.WindowClause` 字段，命名窗口无处定义 |
| `WindowSpec` | `OrderBy` | ✔ | — | — |
| `WindowSpec` | `PartitionBy` | ✔ | — | — |
| `WithClause` | `CTEs` | ✔ | — | — |
| `WithClause` | `Loc` | — | 位置/内部 | 位置 |
| `WithClause` | `Recursive` | ✔ | — | — |

（starrocks：范围类型 32 个，已读字段 79，未读字段 111）


---

## 4. 本轮新验证为「无问题」的形状

审计怀疑过、用探针确认**不读也不丢东西**的（记下来，避免以后重复怀疑）：

| 形状 | 结论 |
| --- | --- |
| PG `SELECT DISTINCT ON (t.x) …` / `GROUP BY DISTINCT` | 只影响行集，与既有语料规则一致 |
| PG `JOIN … USING (id)` vs `ON t.id = s.id` | 两者产出**逐条相同**的边（`UsingClause` 已读；`JoinUsing` 只是 USING 连接的别名） |
| PG `WITH RECURSIVE … SEARCH/CYCLE` | `SearchClause`/`CycleClause` 不读，但不影响边（§0f 已记） |
| PG `TABLESAMPLE` | `RangeTableSample.Args/Method/Repeatable` 不读，不影响边（§0f 已记） |
| PG `ROWS FROM (unnest(t.arr)) AS u(a)` / `unnest(t.arr) AS u(a)` | 两者都产出 `t.arr -> __result__.a`；`Coldeflist` 不读但列名经 `Alias.Colnames` 正确定位 |
| PG `SELECT * FROM t, unnest(t.arr) AS u(a)` | 星号展开**包含**范围函数那一列（`t.arr -> __result__.a`），与 StarRocks 的 FROM-VALUES 残余不同 |
| PG `WITH c(x) AS (SELECT a AS y FROM t) SELECT x FROM c` | 显式列名列表被尊重（`Aliascolnames` 已读；`Ctecolnames` 是解析后回填的同名字段） |
| PG `SELECT (t.arr)[t.id] FROM t` | 下标表达式里的列也被提取（`A_Indirection.Indirection` 走泛走） |
| PG `UPDATE t SET (a,b) = (SELECT x,y FROM other)` | 两列都有边（`MultiAssignRef.Colno/Source` 已读，`Ncolumns` 只是计数） |
| PG `COPY (SELECT a FROM t) TO '/tmp/x'` | 目标不是关系，实证零边（`COPY t FROM …` 才是 `__file__.* -> t.*`） |
| PG `count(*)` / `count(DISTINCT t.x)` / 窗口 `PARTITION BY`/`ORDER BY` | 都产边（`AggStar` 已读） |
| 三方言 `CASE`/`COALESCE`/`ARRAY[…]`/`ANY`/`BETWEEN`/`IS NULL`/`LIKE`/`CAST`/`EXTRACT`/`IF`/`COLLATE` | 列全部被提取（详见 §7） |
| MySQL 家族 `TABLE t` / `INSERT INTO t TABLE s` | 有边（语句形态经 `TableStmt`/`InsertStmt.TableSource` 处理） |
| MySQL 家族 `SELECT COUNT(*) FROM t` | `t.* -> __result__.COUNT(*)`（`FuncCallExpr.Star` 不读但结果正确） |
| MySQL 家族 `LOAD DATA … SET (a = …)` | `LoadDataStmt.SetList` 已读（与 StarRocks 的 `LoadDataDesc.SetExpr` 不同） |
| MySQL 家族 `SELECT * FROM (VALUES ROW(1)) v(a) WHERE a = 1` | 纯字面量，零边（正确） |
| StarRocks `INSERT OVERWRITE t SELECT a FROM s` | 有边（`Overwrite` 不读但只是修饰） |
| StarRocks `GROUP BY ROLLUP/CUBE/GROUPING SETS` | 列边正确；只有 `group_keys` 元数据退化成单个键（第八批残余） |

---

## 5. 新发现详述

### A4　MySQL 家族：`INSERT … SET <col> = (子查询)` 零边（静默）

```sql
INSERT INTO t SET a = 1;                                    -- 语料 17_test_regression 钉的形态
INSERT INTO t SET a = (SELECT MAX(x) FROM other);            -- 本审计：零边
INSERT INTO t SET a = (SELECT x FROM other), b = 2;          -- 本审计：零边
REPLACE INTO t SET a = (SELECT MAX(x) FROM other);           -- 本审计：零边
```

实测（本机一次性容器，下同）：

- **MySQL 8.3.0**：三条子查询形态都 `Query OK`；`SELECT a FROM t` → `7`，即 `other.x` 真的写进了 `t.a`。
- **MariaDB 11.8.9**：同样执行成功并写入。

根因在 `mysql/analyzer.go` 的 `processInsertStatement`：`switch` 只覆盖 `Select`/`TableSource`/`Values`，`default` 分支的注释写着「INSERT … SET assigns literals; there is no source to resolve」——**这是一个未验证的假设**，`SetList` 从此再没被读过。矛盾点很直接：同一个分析器的 `UPDATE t SET a = (SELECT …)` 走 `processUpdateList` 产出 `other.x -> t.a`，而 `Assignment` 是同一个类型。

语料只钉了安全形状：`17_test_regression` 的 `INSERT INTO d SET a = 1` 命名为 "INSERT SET constants have no lineage"。

**修法方向**：把 `SetList` 接进 `processUpdateList` 的既有解析（目标列取 `Columns` 或按位置），或把它当成一个 VALUES 行交给第八批新增的 `processValuesRows`。

### A5　MySQL 家族：`VALUES` 查询原语的子查询零边（静默；`mariadb` 侧表现为解析错误）

```sql
VALUES ROW(1), ROW(2);                                        -- 语料 17_test_regression：零边（正确）
VALUES ROW((SELECT MAX(x) FROM other));                        -- 本审计：零边
SELECT * FROM (VALUES ROW(1), ROW((SELECT MAX(x) FROM other))) v(a);   -- 本审计：零边
```

实测：**MySQL 8.3** 上述子查询两条都成功，分别返回 `7` 与 `1,7`。**MariaDB 11.8.9 不支持 `ROW()` 形态**（`ERROR 1064`），它自己的 `VALUES (1),(2)` / `FROM (VALUES (1),(2)) v(a)` 才是合法写法，且子查询形态也能执行（`VALUES ((SELECT MAX(x) FROM other))` → 7）；但 **omni 的 MySQL 系解析器只接受 `VALUES ROW(…)`，对 MariaDB 的写法报语法错误**。所以这条缺口的可达性按方言不同：

- `mysql`/`tidb`：**静默丢边**（引擎合法、解析通过、边为零）。
- `mariadb`：解析阶段就报错（不会静默），但同一个分析器代码仍在，且 omni 接受的 `VALUES ROW(…)` 形态在 MariaDB 上引擎会拒绝——属于「解析通过、引擎不认」的形态。

根因：`ValuesStmt` 的分派分支是**空体**，注释「A VALUES statement carries only literals; it has no lineage」同样是未验证的假设；`SelectStmt.ValuesSource`（查询原语形态）也从未被读。

**修法方向**：`ValuesStmt.Rows` 与 `InsertStmt.Values` 是同一个 `[][]ExprNode` 类型，第八批为 A1 写的 `processValuesRows(rows)` 可以直接复用；`SelectStmt.ValuesSource` 需要接进 `processSelectStatement` 的查询原语分派。

### A6　StarRocks：`QUALIFY` 完全不读（谓词影响丢失）

```sql
SELECT a, row_number() OVER (PARTITION BY t.b) AS r FROM t;                 -- t.b -> __result__.r
SELECT a FROM t QUALIFY ROW_NUMBER() OVER (PARTITION BY t.b) = 1;           -- 本审计：只有 t.a
SELECT a FROM t WHERE (SELECT MAX(x) FROM other) > 1;                       -- other.x -> __result__.
SELECT a FROM t GROUP BY a HAVING (SELECT MAX(x) FROM other) > 1;           -- other.x -> __result__.
```

同一个分析器里 `WHERE`/`HAVING`/`JOIN … ON` 都产出谓词影响边，唯独 `QUALIFY` 一个字都不读——`SelectStmt.Qualify` 是唯一没有任何读取者的顶层子句字段（MySQL 家族的 `Qualify` 不存在于 AST 里）。

`QUALIFY` 是 StarRocks 2.5+ 的特性，且按官方文档**只接受 `ROW_NUMBER()`/`RANK()`/`DENSE_RANK()`** 三种窗口函数（[窗口函数文档](https://docs.starrocks.io/docs/sql-reference/sql-functions/Window_function/) QUALIFY 节，执行顺序在 HAVING 之后）。因此丢失的血缘是**过滤用窗口的 `PARTITION BY`/`ORDER BY` 列**。建模时要在两个形状里选：与内联窗口一致（列当作窗口值的源，`t.b -> __result__.r`）或与 WHERE 一致（行级谓词影响，`t.b -> __result__.`）——本次不预判，记在 §8。由于 `QUALIFY` 的子句体内只能是窗口函数，本轮探针里 `QUALIFY (SELECT …)` 的形态并非合法 StarRocks，不作为证据。

### A7　MySQL 家族：命名窗口（`WINDOW w AS (…)` + `OVER w`）零边

```sql
SELECT row_number() OVER (PARTITION BY t.b) AS r FROM t;                     -- t.b -> __result__.r
SELECT row_number() OVER w AS r FROM t WINDOW w AS (PARTITION BY t.b);      -- 本审计：零边
```

MySQL 8.3 与 MariaDB 11.8.9 都执行成功。根因：`SelectStmt.WindowClause` 与 `WindowDef.RefName` 都没有读取者；而 **PostgreSQL 早在第二批就修过同一缺陷**（review 记录 P1-3：新增 `namedWindows`/`namedWindowDefinitions`，跟随 `WINDOW w2 AS (w1 …)` 链并带环保护），并留下语料 `35_test_named_window`。这是「PG 修了、兄弟方言没跟上」的又一例。

StarRocks 是另一个问题：它的 AST **根本没有 `SelectStmt.WindowClause` 字段**，只有 `WindowSpec.Name`（引用名）——命名窗口无处定义，属于 omni 的 AST 表达力问题（内部记录，见 §9）。

### A8　MySQL 家族：`VALUES … AS new ON DUPLICATE KEY UPDATE` 的行别名解析不到（有诊断）

```sql
INSERT INTO t (a) VALUES ((SELECT MAX(x) FROM other)) AS new ON DUPLICATE KEY UPDATE b = new.a;
```

分析结果：`other.x -> t.a` 正常产出；ODKU 那条报告 `unresolved reference in an upsert assignment: new.a` 并丢弃。`RowAlias`/`ColAliases` 没有读取者。MySQL 8.0.19+ 语法（8.3 实测可执行；MariaDB 不支持行别名，因此只涉及 `mysql`/`tidb` 方言）。

严重性低，且**不是静默的**——第七批的诊断通道把它报出来了。它和 A4/A5/A7 的区别正在这里：同一个「字段没读」的成因，因为有诊断而可解释。

### A9　StarRocks：未取别名的复杂表达式派生列名被截断

| SQL | 本仓库 `to_field` | 期望 |
| --- | --- | --- |
| `SELECT CASE WHEN t.x > 0 THEN t.a ELSE t.b END FROM t` | `bEND` | 完整表达式文本（MySQL 方言给的就是完整文本） |
| `SELECT t.x IS NULL FROM t` | `xISNULL` | 同上 |
| `SELECT [t.a, t.b] FROM t` | `b]` | 同上 |
| `SELECT CAST(t.x AS STRING) FROM t` | `CAST(t.xASSTRING)` | 至少不是无空格拼接 |
| `SELECT t.a + 1 FROM t` | `a+1` | —（这条本来就该去掉限定符，行为正确） |
| `SELECT upper(t.a) FROM t` | `upper(t.a)` | —（含括号，走了另一分支） |

根因是 `starrocks/expr.go` 的 `inferColumnAlias`：只要表达式文本**含 `.` 且不含 `()`**，就当作限定标识符取最后一段。`CASE … ELSE t.b END` 的文本里恰好有一个点、没有括号，于是被切成了 `bEND`。这是**本仓库的启发式**，不是 omni 的问题（omni 给的文本是对的）。

影响面与 PG 的 P1-1 同属「目标列名错误」，主要影响 MANUAL_SQL 与 `__result__` 元数据：视图/物化视图定义通常带列名列表（`CreateViewStmt.Columns`、`CreateMTMVStmt.Columns` 已读），会覆盖这个推导名。语料只覆盖了 `AS band` 这种带别名的形态（`16_test_extended_forms`），所以一直没暴露。

**修法方向**：判定「是不是限定标识符」应当基于 AST（单个 `ColumnRef`/裸标识符）而不是文本启发式；退一步也应要求文本整体匹配标识符路径（`^[A-Za-z_][\w$]*(\.[A-Za-z_][\w$]*)*$`）。

### A10　MySQL 家族：`JSON_TABLE` 作为 FROM 项不建模（星号展开静默漏列）

```sql
SELECT jt.x FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (x INT PATH '$.x')) AS jt;   -- 诊断 unresolved reference in an output column: jt.x
SELECT * FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (x INT PATH '$.x')) AS jt;      -- 只有 t 的列，无诊断（静默）
```

`JSON_TABLE` 与 PG 的 `RangeTableFunc`（XMLTABLE，A3）同类：它不是表达式树里的节点，而是 FROM 项，必须结构性处理才会被看到。MySQL 8.0.4+ 支持该语法；本仓库不注册 `jt` 这个关系，所以 `t.doc` 这条源丢失。与 A3 一样建议**只记录**（同步视图里罕见），但它比 A3 多一条：星号展开那条是静默的。

### 顺带记录（不编号）

- **StarRocks 的表达式文本会丢掉空格**：`exprText` 按 token 拼接（`CASEWHENsalary>100THEN'hi'ELSEnameEND`），而 MySQL 方言保留原文本。语料已经把这种无空格形态当作预期钉住了（`16_test_extended_forms` 的 `expression:`），所以它是既定约定而不是回归；但它同时是 A9 里 `CAST(t.xASSTRING)` 的成因之一。跨方言的元数据文本不一致，值得在某次清理里一并决定。

---

## 6. 范围外类型（未被任何分析器触及）

**第一类·DDL/DCL/管理/事务/复制（绝大多数）**：分析器的语句分派只覆盖 SELECT/INSERT/UPDATE/DELETE/CTAS/CREATE VIEW/ALTER VIEW/LOAD/COPY/MERGE 等，`default` 分支显式忽略其余（「Statement kinds with no column lineage」）。这类节点在本仓库里不可达，无血缘语义。

**第二类·表达式节点**：`CaseExpr`、`CaseWhen`、`A_ArrayExpr`、`XmlExpr`、`JsonValueExpr`、`BetweenExpr`、`IsExpr`、`LikeExpr`、`CastExpr`、`ExtractExpr`、`IntervalExpr`、`ArrayLiteral`、`MapLiteral`、`LambdaExpr`、`Literal`、`TypeName` 等——全部由泛走覆盖（§7 实测）。它们出现在「未被显式读取」列表里是正常的。

**第三类·omni 有、本 pin 的语法产不出来的节点**：PG 的 `FromExpr`、`SetOperationStmt`（集合运算经 `SelectStmt.Op/Larg/Rarg` 建模）、`LockingClause`、`WindowClause`、`InferClause`；MySQL 家族的 `RawStmt`、`BatchStmt`、`FactorOp` 等。不读它们不影响任何可解析输入的边。

**需要留意的例外（都在上面已编号或已记录）**：

- PG `RangeTableFunc`（XMLTABLE）→ A3，第八批决定只记录。
- PG `MergeStmt` → 分析器**拒绝**并上报 `not modelled: MERGE`（第七批诊断），属决策。
- MySQL 家族 `ValuesStmt` → 被分派但分支为空 → A5。
- MySQL 家族 `JsonTableExpr`（`JSON_TABLE`，FROM 项）→ A10。
- PG `JsonTable`（`JSON_TABLE`，PG 17 才有；本仓库验证引擎是 16，未实测）→ 与 A3/A10 同类的候选。
- StarRocks `TableFunctionRef` → 实测 `SELECT t.a FROM t, unnest([1,2]) AS u` 与 `generate_series(1,3)` 不产出额外边，但那两个参数是字面量、本来就无源，**没有证据表明丢东西**；带列参数的形态未验证，记在 §8 待决。
- StarRocks `GroupingSetsExpr`/`RollupDef` → 组集作为整体渲染成一个键（第八批残余，只影响 `group_keys` 元数据）。

---

## 7. 表达式节点的泛走覆盖（实测清单）

三个分析器都用 `Inspect` 提取表达式里的列，所以「表达式节点的字段没被逐个读」不等于丢列。本轮对 15 个形状做了实测，全部符合预期：

| SQL 形状 | postgresql | mysql/tidb/mariadb | starrocks |
| --- | --- | --- | --- |
| `CASE WHEN x > 0 THEN a ELSE b END` | `x`/`a`/`b` 三列 | 三列 | 三列（列名受 A9 影响） |
| `COALESCE(a, b)` | 两列 | —（等价于 `IF`） | — |
| `ARRAY[a, b]` / `[a, b]` | 两列 | — | 两列（列名受 A9 影响） |
| `a = ANY(ARRAY[b])` | 两列（`?column?` 名为 PG 规则） | — | — |
| `GROUPING(a)` + `GROUP BY ROLLUP(a)` | 有边 | — | — |
| `IF(x > 0, a, b)` | — | 三列 | — |
| `CAST(x AS CHAR)` / `CAST(t.x AS STRING)` | 有边 | 有边 | 有边（列名 A9） |
| `x BETWEEN a AND b` | — | 三列 | — |
| `x IS NULL` | 有边 | 有边 | 有边（列名 A9） |
| `x LIKE 'a%'` | — | 有边 | — |
| `EXTRACT(YEAR FROM x)` | — | 有边 | — |
| `COLLATE "C"` | 有边 | — | — |
| `ROW(...)`/`(a, b) = (1, 2)` | 有边（两列，名为 PG 规则的 `?column?`） | — | — |
| 聚合内 `ORDER BY`/`SEPARATOR`（`GROUP_CONCAT(x ORDER BY y)`） | — | 两列都在 | — |
| `PARTITION BY`/`ORDER BY`（内联窗口） | 有边 | 有边（`OVER w` 除外，A7） | 有边（`QUALIFY` 除外，A6） |

---

## 8. 待决事项与建议顺序

**待决（本次没有定论，记录在案）**

1. **StarRocks `LoadDataDesc.SetExpr` / `Where`**（表中 `T`）：这两个字段以**原始文本**保存（`SET(...)`、`WHERE ...` 的字符串），分析器读了 `Target`/`ColumnList` 但没回解析它们。是否需要把装载作业的 SET/WHERE 建模，取决于这类血缘在产品里有没有消费方；若要做，需要一次二次解析（并决定解析失败的语义）。本轮没有引擎验证其语法可达性。
2. **StarRocks `TableFunctionRef`**：带列参数的形态（如 `unnest(t.arr)`）未验证，A6/A10 同类问题的潜在第三条。
3. **A6 的建模形状**：QUALIFY 的窗口列应该发成「窗口值的源」还是「行级谓词影响」——需要一次与 P1-5 同层面的决策。
4. **A9 的判定方式**：改用 AST 判定还是收紧正则，属于实现选择；但一旦改，`16_test_extended_forms` 那类无空格表达式文本的既定约定要一并考虑。

**建议顺序（修的时候）**

1. **A4 / A5 / A7**——同一类「结构性字段没读导致静默丢边」，且修法都已知：A4/A5 复用第八批的 `processValuesRows` 与 `processUpdateList`，A7 复用 PG 第二批的 `namedWindows` 思路。修的时候**必须同时补语料**，且注意 A5 在 `mariadb` 侧的可达性与 MySQL 不同。
2. **A10 / A3**——FROM 项里的表函数（JSON_TABLE/XMLTABLE）：A10 的星号展开是静默的，A3 是记录状态；两者可以一起做，也可以继续只记录。
3. **A9**——列名规则，影响面与 PG 的 P1-1 同级（主要 MANUAL_SQL），但改动局部。
4. **A8**——已有诊断，优先级最低。
5. **§8 待决 1/2**——需要先有产品判断，不建议顺手做。

**不要照搬 PG**：A4/A5/A7 都涉及 MySQL 家族与 PG 的语法差异（`INSERT … SET` 是 MySQL 扩展；MariaDB 不支持 `ROW()` 与行别名；命名窗口是两边都有但 AST 表达不同）。修之前按本仓库既定做法用一次性容器实测。

---

## 9. 与既有记录的关系

| 既有条目 | 关系 |
| --- | --- |
| review §0f A1（`ValuesLists`）/ A2（`GroupingSet`） | 都在第八批修复（三方言族 / PG）。**A4/A5 是同一类、同一分析器里剩下的两个 VALUES 形态**；A1 写的 `processValuesRows` 正是 A4/A5 的复用点 |
| review §0b P1-3（命名窗口） | 只在 PG 修了；MySQL 家族从未实现 → **A7 是它的兄弟方言缺口** |
| review §0b P1-5（UPDATE 行集不建模） | 表中 `UpdateStmt.Where`/`WhereClause` 的 `X` 判定；`CopyStmt.WhereClause` 按同一规则 |
| review §0d 发现 5（ON CONFLICT 的 WHERE） | 表中 `OnConflictClause.WhereClause` 的 `X` 判定，语料 `15_test_on_conflict` 已钉 |
| review §0d 发现 7（upsert 常量赋值不对称） | 与本审计无关但同类（同一子句的两种形态建模不一致），仍未修 |
| 第八批残余：`OutputColumn.IsDerived` 只写不读 | **不是 AST 字段**，不在这张表里；仍然成立 |
| 第八批残余：StarRocks FROM-VALUES 零边 | 与 A5 同族（MySQL 家族侧的 `ValuesSource`），A5 给出的是 MySQL 家族的确证 |
| 附录 C（外部依赖观察） | 本审计新增三条，见下 |

**新增的 omni 观察（仅内部记录，未对外反馈）**

1. omni 的 `starrocks/parser` 只接受 `VALUES ROW(…)` 与 `TABLE t` 的语句形态，`TABLE t UNION ALL TABLE s` 报语法错误；`GROUP BY a WITH ROLLUP` 也报错（StarRocks 用 `GROUP BY ROLLUP(a)`）。
2. omni 的 StarRocks AST **没有 `SelectStmt.WindowClause` 字段**，命名窗口只有 `WindowSpec.Name`（引用名）而无定义处；对照 PG 的 `SelectStmt.WindowClause` + `WindowDef.Refname`。
3. omni 的 MySQL 系解析器接受 `VALUES ROW(…)`，而 MariaDB 11.8.9 引擎拒绝该语法；反过来 MariaDB 的 `VALUES (1),(2)` / `FROM (VALUES (1),(2)) v(a)` 解析器不接受。两边对不上，属于依赖的语法覆盖问题，不是本仓库缺陷。

---

## 10. 复现方法

审计工具是**一次性**的（不留在仓库里，源码见附录 A）：一个 `_` 前缀目录下的 `main.go`（Go 工具忽略 `_` 目录，所以不会进 `go build ./...`），加一个生成表格的 Python 脚本。

```bash
# 1. 抽取：字段清单 × 读取 × 泛走（Go 侧用 export data 做精确的类型检查）
mkdir -p _astaudit && $EDITOR _astaudit/main.go     # 见附录 A
GOCACHE=$PWD/.gocache go run ./_astaudit > _astaudit/audit.json

# 2. 生成对照表：audit.json + 判定规则 → Markdown
python3 _astaudit/gen_tables.py > /dev/null

# 3. 形状探针（与 review 附录 A 同一手法：包内临时 _test.go + 现成的 analyzeSQL）
#    backend/plugin/lineage/{postgresql,mysql,starrocks}/zz_audit_probe_test.go
GOCACHE=$PWD/.gocache go test -count=1 -run TestZZAuditProbe -v ./backend/plugin/lineage/mysql/

# 4. 引擎实测（一次性容器，用完即删；不碰任何既有容器）
docker run -d --name mxd-astaudit -e MARIADB_ROOT_PASSWORD=dev -e MARIADB_DATABASE=sem mariadb:11
docker run -d --name mxd-astaudit-mysql -e MYSQL_ROOT_PASSWORD=dev -e MYSQL_DATABASE=sem mysql:8.3.0
docker exec mxd-astaudit mariadb -uroot -pdev sem -e "INSERT INTO t SET a = (SELECT MAX(x) FROM other)"
docker exec mxd-astaudit-mysql mysql -uroot -pdev sem -e "VALUES ROW((SELECT MAX(x) FROM other))"
docker rm -f mxd-astaudit mxd-astaudit-mysql
```

**机器可复现的部分**是「字段清单 × 是否读取 × 是否泛走」三列（步骤 1）。**判定列是人工结论**，已随表逐行记录依据；重新生成时若要一字不差地还原判定，需要照 §5 的规则表重建（类别代码见 §1.3）。

审计产物（`audit.json`、探针、临时容器）在本文件归档后已删除；`git status` 干净。

---

---

## 修复状态（第十批，`3161563`）

每一项落在哪里，便于把这张表和代码对上：

| # | 修法 | 代码位置 | 语料钉子 |
| --- | --- | --- | --- |
| **A4** | `INSERT ... SET` 的赋值值表达式按赋值列名成为输出列，走 INSERT 既有的发射路径 | mysql/tidb/mariadb `processSetList` | `06_test_insert` 三条 + 常量一条 |
| **A5** | 独立 VALUES 语句与 VALUES 查询原语都产出行的源；无名列占位改 `column_0` | `processValuesStatement`、`processQuerySpecification`、`processValuesRows` | `17_test_regression` 两条（TiDB 解析器拒绝派生表形态，进其 `knownParserGaps`）|
| **A7** | 移植 PG 的 `namedWindows`/`namedWindowDefinitions`，列收集与变换元数据都展开 `OVER w` | `namedWindowsOf`、`namedWindowDefinitions`、`extractWindowClauses` | `12_test_window_function` 两条 + 内联回归一条 |
| **A8** | `VALUES(col)` 与行别名共用一张按名字索引的表 | `upsertSourceMap`、`proposedValueKey` | `06_test_insert` 两条（MariaDB 无该语法，进其 `knownParserGaps`）|
| **A3** | `RangeTableFunc` 注册为查询内关系：COLUMNS 声明列、`PASSING` 文档表达式为源、`FOR ORDINALITY` 无源 | postgresql `processRangeTableFunc` | `38_test_set_returning_function` 五条 |
| **A10** | `JsonTableExpr` 同样注册，含 NESTED PATH 列 | mysql 家族 `processJSONTable`、`jsonTableColumnNames` | `16_test_extended_forms` 三条 |
| **A6** | QUALIFY 接入谓词影响路径（与 WHERE/HAVING 同路），并像 HAVING 一样解析别名 | starrocks `processQuerySpecification` | `22_test_predicate_influence` 两条 + QUALIFY 视图用例补 2 条边 |
| **A9** | 列名判定改为基于 AST；顺带把引号列名对齐 mysql 语料 | starrocks `inferredColumnAlias` | 新建 `25_test_derived_column_name` 六例 |
| §8 待决 2 | StarRocks `TableFunctionRef` 实测为缺口（`u.a` 未解析、星号静默漏列）并一并修 | starrocks `processTableFunction` | `23_test_query_local_name` 三条 |
| §3 表内 `!` 行 | A4/A5/A6/A7/A8 的行已随之消解；表内其余 `!` 标记描述的是**修复前**状态 | — | — |

**仍按决策保留**：§8 待决 1（`LoadDataDesc.SetExpr/Where` 以原始文本保存、未回解析）、待决 3（QUALIFY 的建模形状已定为行级谓词影响，但"是否也解析别名"保留为容差）、待决 4（A9 不改表达式文本的空格约定，语料 `expression:` 的既有约定因此不变）。

**新增的 omni 观察**（仅内部记录，未对外反馈，明细在 `docs/omni_upstream_defects.md`）：TiDB 的 `SelectStmt` 缺 VALUES 原语字段；MariaDB 的 `InsertStmt` 缺 `RowAlias`/`ColAliases` 字段；MariaDB 解析器与 MariaDB 引擎在 `VALUES` 语法上互不覆盖。

**另附一条引擎证据**：StarRocks 的 FROM 侧 VALUES（`InlineTable`）带子查询被 StarRocks 4.1 直接拒绝（规划器内部错误），字面量形态无源——所以本文件 §6 记的"StarRocks FROM-VALUES 残余"**不是分析器丢边**，而是引擎不支持的形状。

## 附录 A：抽取器源码

`_astaudit/main.go`（原样保留；运行时用仓库自己的 module 与 `GOCACHE`，无新依赖）：

```go
// Command _astaudit extracts an exact cross-reference between omni AST struct
// fields (read coverage, walker coverage) and the fields the lineage analyzers
// read. It is a throwaway audit tool; the underscore directory keeps the Go
// tool from building it as part of the repository.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type readSite struct {
	Consumer string `json:"consumer"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Func     string `json:"func"`
}

type output struct {
	Fields map[string]map[string]map[string]bool   `json:"fields"` // pkg -> type -> field -> walker
	Types  map[string]map[string]map[string]string `json:"types"`  // pkg -> type -> field -> field type
	Reads  map[string][]readSite                   `json:"reads"`  // pkg|type|field -> sites
	Errors map[string]int                          `json:"errors"`
}

var astPkgs = []string{
	"github.com/bytebase/omni/pg/ast",
	"github.com/bytebase/omni/mysql/ast",
	"github.com/bytebase/omni/tidb/ast",
	"github.com/bytebase/omni/mariadb/ast",
	"github.com/bytebase/omni/starrocks/ast",
}

var targetPkgs = []string{
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/postgresql",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/tidb",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/mariadb",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/starrocks",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model",
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog",
}

func main() {
	fset := token.NewFileSet()
	out := output{
		Fields: map[string]map[string]map[string]bool{},
		Types:  map[string]map[string]map[string]string{},
		Reads:  map[string][]readSite{},
		Errors: map[string]int{},
	}

	exports := listExports()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		p, ok := exports[path]
		if !ok || p == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(p)
	})

	// 1. AST packages: struct types and their fields (from export data).
	for _, path := range astPkgs {
		pkg, err := imp.Import(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "import %s: %v\n", path, err)
			continue
		}
		out.Fields[path] = map[string]map[string]bool{}
		out.Types[path] = map[string]map[string]string{}
		for _, name := range pkg.Scope().Names() {
			obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || obj.IsAlias() {
				continue
			}
			st, ok := obj.Type().Underlying().(*types.Struct)
			if !ok {
				continue
			}
			out.Fields[path][name] = map[string]bool{}
			out.Types[path][name] = map[string]string{}
			for i := 0; i < st.NumFields(); i++ {
				out.Fields[path][name][st.Field(i).Name()] = false
				out.Types[path][name][st.Field(i).Name()] = st.Field(i).Type().String()
			}
		}
	}

	// 2. Walker coverage: parse the AST package's walker source and mark every
	// field the generated walker recurses into.
	for _, path := range astPkgs {
		dir := pkgDir(path)
		for _, f := range []string{"walk_generated.go", "walk_children.go", "walk.go"} {
			markWalker(filepath.Join(dir, f), out.Fields[path])
		}
	}

	// 3. Analyzer packages: type-check from source and collect field reads.
	for _, path := range targetPkgs {
		dir, files := targetFiles(path)
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "no files for %s\n", path)
			continue
		}
		conf := types.Config{
			Importer: imp,
			Error:    func(error) {},
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}
		var parsed []*ast.File
		for _, f := range files {
			file, err := parser.ParseFile(fset, filepath.Join(dir, f), nil, parser.SkipObjectResolution)
			if err != nil {
				fmt.Fprintf(os.Stderr, "parse %s: %v\n", f, err)
				continue
			}
			parsed = append(parsed, file)
		}
		_, err := conf.Check(path, fset, parsed, info)
		if err != nil {
			out.Errors[path]++
		}
		for _, file := range parsed {
			rel := fset.Position(file.Pos()).Filename
			walkFuncs(file, func(fn string, node ast.Node) {
				ast.Inspect(node, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					s := info.Selections[sel]
					if s == nil {
						return true
					}
					v, ok := s.Obj().(*types.Var)
					if !ok || !v.IsField() {
						return true
					}
					key, ok := declaringKey(s)
					if !ok {
						return true
					}
					pos := fset.Position(sel.Pos())
					out.Reads[key] = append(out.Reads[key], readSite{
						Consumer: path,
						File:     filepath.Base(rel),
						Line:     pos.Line,
						Func:     fn,
					})
					return true
				})
			})
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func declaringKey(s *types.Selection) (string, bool) {
	idx := s.Index()
	if len(idx) == 0 {
		return "", false
	}
	base := deref(s.Recv())
	name := ""
	pkgPath := ""
	for i, field := range idx {
		if named, ok := base.(*types.Named); ok {
			obj := named.Obj()
			if obj.Pkg() != nil {
				pkgPath = obj.Pkg().Path()
			}
			name = obj.Name()
		}
		st, ok := base.Underlying().(*types.Struct)
		if !ok || field >= st.NumFields() {
			return "", false
		}
		if i == len(idx)-1 {
			if pkgPath == "" {
				return "", false
			}
			return pkgPath + "|" + name + "|" + st.Field(field).Name(), true
		}
		base = deref(st.Field(field).Type())
	}
	return "", false
}

func deref(t types.Type) types.Type {
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			return t
		}
		t = p.Elem()
	}
}

// walkFuncs calls fn for every function body in the file, with the enclosing
// function name.
func walkFuncs(file *ast.File, fn func(string, ast.Node)) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = recvName(d.Recv.List[0].Type) + "." + name
			}
			if d.Body != nil {
				fn(name, d.Body)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					fn("var "+vs.Names[0].Name, v)
				}
			}
		}
	}
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	}
	return "?"
}

// markWalker parses a walker source file and marks fields visited in the
// walkChildren switch as walked.
func markWalker(file string, fields map[string]map[string]bool) {
	if fields == nil {
		return
	}
	src, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		return
	}
	current := ""
	ast.Inspect(src, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CaseClause:
			current = ""
			for _, e := range node.List {
				if se, ok := e.(*ast.StarExpr); ok {
					if id, ok := se.X.(*ast.Ident); ok {
						current = id.Name
					}
				}
				if id, ok := e.(*ast.Ident); ok {
					current = id.Name
				}
			}
			for _, stmt := range node.Body {
				ast.Inspect(stmt, func(m ast.Node) bool {
					sel, ok := m.(*ast.SelectorExpr)
					if !ok || current == "" {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "n" {
						if f, ok := fields[current]; ok {
							f[sel.Sel.Name] = true
						}
					}
					return true
				})
			}
			return false
		}
		return true
	})
}

func listExports() map[string]string {
	args := append([]string{"list", "-deps", "-export", "-f", "{{.ImportPath}}\t{{.Export}}"}, targetPkgs...)
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func pkgDir(path string) string {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", path)
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func targetFiles(path string) (string, []string) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}\t{{join .GoFiles \"|\"}}", path)
	data, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	parts := strings.SplitN(strings.TrimSpace(string(data)), "\t", 2)
	if len(parts) != 2 {
		return parts[0], nil
	}
	var files []string
	for _, f := range strings.Split(parts[1], "|") {
		if f == "" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		files = append(files, f)
	}
	sort.Strings(files)
	return parts[0], files
}
```

---

## 附录 B：本次实测命令与结果（引擎侧）

| 语句 | MySQL 8.3.0 | MariaDB 11.8.9 |
| --- | --- | --- |
| `INSERT INTO t SET a = (SELECT MAX(x) FROM other)` | ✅ 写入 7 | ✅ 写入 7 |
| `REPLACE INTO t SET a = (SELECT MAX(x) FROM other)` | ✅ | ✅ |
| `INSERT INTO t (a) VALUES ((SELECT MAX(x) FROM other))` | ✅ | ✅ |
| `VALUES ROW(1), ROW(2)` | ✅ 返回 1,2 | ❌ `ERROR 1064`（不支持 `ROW()`） |
| `VALUES ROW((SELECT MAX(x) FROM other))` | ✅ 返回 7 | ❌ `ERROR 1064` |
| `VALUES ((SELECT MAX(x) FROM other))` | —（MySQL 不接受该写法） | ✅ 返回 7 |
| `SELECT * FROM (VALUES ROW(1), ROW((SELECT MAX(x) FROM other))) v(a)` | ✅ 返回 1,7 | ❌ `ERROR 1064` |
| `SELECT * FROM (VALUES ((SELECT MAX(x) FROM other))) v(a)` | — | ✅ 返回 7 |
| `SELECT row_number() OVER w AS r FROM t WINDOW w AS (PARTITION BY b)` | ✅ | ✅ |
| `INSERT INTO t (a) VALUES ((SELECT MAX(x) FROM other)) AS new(a) ON DUPLICATE KEY UPDATE b = new.a` | ✅（`b` 为 NULL） | —（不支持行别名） |
| `SELECT jt.x FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (x INT PATH '$.x')) AS jt` | ✅ 语法通过（本例列类型报错属数据问题） | — |
| `TABLE t` | ✅ | ✅ |
