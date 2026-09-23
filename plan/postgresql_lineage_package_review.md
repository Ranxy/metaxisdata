# PostgreSQL 血缘分析包深度 Review

> **文档性质：审计报告 + 修复 backlog，不是实现计划。**
> 审查对象：`backend/plugin/lineage/postgresql/`（+ 其共享依赖 `lineage/scope`、`lineage/model`、`lineage/catalog`）。
> 审查方式：全文精读 + 临时探针测试（已删除）+ 真实 PostgreSQL 16 语义验证 + 与 MySQL/StarRocks 同级分析器对比 + lint/测试复核。
> 结论：**整体工程质量偏上（测试语料严格、注释质量高、无 panic 路径），但存在 4 个会静默丢失/伪造血缘的确定性缺陷，以及一处结构性技术债。**

---

## 0. 第一批（P0）实施状态：已落地

第一批 4 项 P0 已全部修复并验证。下方 §3 保留原始发现（作为问题记录与验收标准），此处只记结果。

| 项 | 状态 | 落地内容 |
| --- | --- | --- |
| **P0-1** CTE/派生表别名与真实表同名丢血缘 | ✅ 已修复 | 删除 `tempTables` 名字集合与 `markTempTable`/`isTempRelation`/`isTableTempInCurrentScope`；`addRelation` 不再按名字过滤；temp 判定改为由 `ResolvedColumn.Relation` 派生。`isTemp` 字段改为 `target == __result__`（目标永远是真实对象） |
| **P0-2** UPDATE/ON CONFLICT 赋值源落在 CTE/派生表上丢血缘 | ✅ 已修复 | `processAssignments` 补上 `res.Relation.IsCTE/IsSubquery` → `traceThroughTableLineageToTarget` 分支，与其他 emitter 对齐 |
| **P0-3** 集合运算把第一个分支的变换伪造给所有分支 | ✅ 已修复 | 引入 `scope.ColumnSource`（源与变换绑定），`mergeSetOpOutputColumns` 按分支保留各自变换 |
| **P0-4** 嵌套集合运算丢失内层运算 | ✅ 部分修复 | `flattenSetOpArms` 改为携带"运算链"（外层在前，连续同值去重），`a UNION b INTERSECT c` 的 b/c 分支现同时记录 `UNION`+`INTERSECT`。**`UNION ALL` 与 `UNION` 仍不可区分**：`Transformation` 没有 `All` 字段，需要 proto/model 改动，按原计划单独排期 |

**修复过程中连带处理的问题**（都是上述修复暴露或必需的前置）：

1. **`Resolved` 引用会被重新绑定到同名的无关真实表**：去掉名字过滤后，`SELECT * FROM t WHERE a IN (SELECT x FROM (SELECT y AS x FROM s) t)` 会把谓词源错记为 `t.x`（外层同名真实表），而不是 `s.y`。原因是 `subquerySources` 丢弃了 temp 关系信息，只留下别名 + `Resolved`。新增 `Analyzer.flattenTempSources`，并让 `resolveOutputColumns` 在解析时就把 temp 源展开为真实表——错误边因此被修正为 `s.y`（见 `31_test_relation_shadowing_lineage_table.yaml`）。
2. **递归 CTE 自引用**：原来靠名字过滤顺带丢弃 `WITH RECURSIVE r AS (... FROM r)` 的自引用边。`processCTE` 现在读 `WithClause.Recursive`，显式丢弃自引用（不引入不可解析的关系），并新增语料固定该行为。
3. **共享类型迁移**：`scope.OutputColumn` 由 `SourceColumns []ColumnRef` + `Transform` 改为 `Sources []ColumnSource`。这是 PG 修复的必需项，因此 MySQL/TiDB/MariaDB/StarRocks 五个方言同步做了机械迁移；四个兄弟方言的 `mergeSetOpOutputColumns` 也一并修掉了同一个 P0-3 缺陷（它们此前有完全相同的代码）。
4. **`BenchmarkAnalyzeCorpus`/`BenchmarkAnalyzeCase` 此前根本无法运行**：benchmark 夹具忽略了语料里的 `expect_error` 用例（`29_test_statement_lineage_table.yaml` 的 `merge fails loudly`），一跑就 `b.Fatalf` 退出。已让夹具跳过错误用例。

**新增/修改的语料**

- 新增 `postgresql/testdata/analyze/31_test_relation_shadowing_lineage_table.yaml`（P0-1，3 例）
- 新增 `postgresql/testdata/analyze/32_test_assignment_temp_source_lineage_table.yaml`（P0-2，3 例）
- 新增 `postgresql/testdata/analyze/33_test_set_operation_transformation_lineage_table.yaml`（P0-3/P0-4/递归 CTE，4 例）
- `mysql/testdata/analyze/05_test_union_lineage_table.yaml` 与 `starrocks/.../05_test_union_lineage_table.yaml` 各加 2 例（覆盖兄弟方言的 P0-3 修复；TiDB/MariaDB 复用 MySQL 语料，自动覆盖）

**验证方式**：先抓取修复前对 49 条探针 SQL 的完整输出快照，修复后逐行 diff——**20 处删除全部是伪造的 `a + 1` 变换，42 处新增全部是上述四项修复产生的边**，无一处意外变更。另加 100+ 条嵌套结构的泄漏探针，确认再无查询局部名泄漏成边。全仓 `go build ./...`、`go test ./...`、`golangci-lint`（0 issues）均通过。

**第一批未覆盖、仍待处理**：§4 的 P1-1～P1-8、§5 的 P2、§6 的 D1（三方言重复）与 D3（Transformation 模型表达力）、§7 剩余测试缺口。

---

## 1. 结论摘要

| 级别 | 数量 | 说明 |
| --- | --- | --- |
| **P0 严重** | 4 | 静默丢失全部血缘 / 伪造血缘元数据。均可复现，部分可通过真实 view 定义在生产触发 |
| **P1 中等** | 8 | 命名/目标列名错误、语义缺口、去重丢信息、MERGE 连带失败等 |
| **P2 低风险/健壮性** | 13 | 边界处理、防御性缺陷、注释漂移、可见性等 |
| **技术债** | 5 项 | 三份近重复分析器、双份"临时表"判定、Transformation 模型表达力不足等 |
| **测试缺口** | 11 个方向 | 语料未覆盖上述大部分缺陷（语料机制本身很优秀，是覆盖范围问题） |

**最需要立即处理的 4 件事：**

1. `WITH orders AS (SELECT ... FROM orders ...)` 这类 **CTE/派生表别名与真实表同名**的语句，血缘被**整体清空**（P0-1）。
2. `UPDATE t SET a = c.x FROM c`（源是 CTE/派生表）血缘**整体丢失**（P0-2）。
3. 集合运算（UNION/INTERSECT/EXCEPT）把**第一个分支的表达式变换伪造给所有分支**（P0-3）。
4. 嵌套集合运算把 `INTERSECT`/`EXCEPT` 分支标成 `UNION`，且 `UNION ALL` 无法区分（P0-4）。

---

## 2. 审查范围与方法

### 2.1 范围内

- `postgresql/analyzer.go`（1582 行）、`postgresql/expr.go`（518 行）、`postgresql/predicate.go`（155 行）
- 测试：`analyze_test.go`、`analyze_test_helper.go`、`hardfail_test.go`、`onconflict_test.go`、`expr_classify_test.go`、`identifier_case_test.go`、`benchmark_test.go`、`testdata/analyze/*.yaml`（30 个文件）
- 依赖：`lineage/scope/{scope,types}.go`、`lineage/model/{relation,transformation,identifier}.go`、`lineage/catalog/provide.go`、`lineage/testutil/`
- 调用方：`backend/runner/lineageanalyzer/analyzer.go`（决定真实输入形态与失败后的行为）

### 2.2 方法

1. 全文精读三个源文件与共享依赖。
2. 阅读 `plan/postgresql_omni_parser_migration_plan.md`、`plan/postgresql_expression_transformation_plan.md`，区分**已决策行为**与**缺陷**。
3. 写临时探针测试（`zz_probe*_test.go`，共 100+ 条 SQL）直接观察分析器输出；**探针已全部删除，工作区干净**。
4. 起 `postgres:16-alpine` 容器，用真实 PostgreSQL 校验语义判定（CTE 自引用、`pg_get_viewdef` 别名、CTAS 列名、未具名表达式列名、命名窗口）。**容器已删除**。
5. `go test ./backend/plugin/lineage/...` 全绿；`golangci-lint run` 该包 **0 issues**。
6. 用函数清单对比 PG/MySQL/StarRocks 三个分析器，量化重复。

### 2.3 生产输入形态（影响严重性判定）

`backend/runner/lineageanalyzer/analyzer.go:154` 只分析三类对象：`VIEW`、`MATERIALIZED_VIEW`、`MANUAL_SQL`（**不含 TABLE，所以 CTAS 目标命名只在 MANUAL_SQL 下可见**）。

- **View / Matview**：定义来自 `pg_views.definition` / `pg_matviews.definition`，即 `pg_get_viewdef` 输出。**实测 `pg_get_viewdef` 总是补齐 `AS` 别名**，因此"目标列名"类缺陷（P1-1）对同步视图无影响；但 CTE、UNION、窗口、递归 CTE 都合法，因此 P0-1/P0-3/P0-4/P1-3/P1-5 **对生产视图直接影响**。
- **MANUAL_SQL**：用户手写 SQL，全部缺陷均可能触及；`UPDATE`/`MERGE` 只在此路径出现。

---

## 3. P0 严重问题

### P0-1　CTE / 派生表别名与真实表同名时，该真实表的血缘被整体丢弃　— ✅ 已在第一批修复（见 §0）

**位置**：`analyzer.go:1450-1455`（`markTempTable`）、`analyzer.go:1460-1467`（`isTempRelation`）、`analyzer.go:1492-1513`（`addRelation`）、`analyzer.go:1471-1484`（`isTableTempInCurrentScope`）

**根因**：存在两套互不一致的"临时关系"判定：

- `tempTables map[string]struct{}` 是**语句级、仅按裸名字**记录的集合，`markTempTable` 把每个 CTE 名（`processCTE`）和每个派生表别名（`processRangeSubselect`）写进去；
- `addRelation` 用 `isTempRelation`（查 `tempTables`）决定是否丢弃边；
- 而 `isTableTempInCurrentScope` 走的是**作用域查找**（`FindCTE` / `FindTable`）。

裸名字全局集合无法区分"作用域内的查询局部名"与"同名的真实表"。只要同一语句里出现同名，真实表的边就会被 `addRelation` 静默丢光。

**证据（分析器输出，`nil` catalog）**

| SQL | 分析器结果 | 真实 PostgreSQL 16 |
| --- | --- | --- |
| `WITH orders AS (SELECT user_id, total FROM orders WHERE total > 100) SELECT user_id, total FROM orders` | **0 条边** | 正常执行；内层 `orders` 解析为**基表**（非递归 CTE 名在自身 body 内不可见） |
| `WITH t AS (SELECT id FROM t) SELECT id FROM t` | **0 条边** | 正常执行（实测 `WITH t2 AS (SELECT aa FROM t2) SELECT * FROM t2` 返回基表行） |
| `SELECT * FROM t WHERE a IN (SELECT x FROM (SELECT y AS x FROM s) t)` | **0 条边**（外层真实 `t` 被误判） | 正常执行 |
| `WITH t AS (SELECT id FROM s) INSERT INTO t (a) SELECT id FROM t` | **0 条边**（目标表也被误判） | 正常执行 |

**注意**：`processRangeVar` / `processCTE` 对 CTE 名的解析本身是**正确**的（CTE 名在自身 body 内不可见 → 走基表分支，与 PG 一致）。缺陷纯粹在最后一步的 `tempTables` 过滤把已经算对的血缘扔掉了。

**修复方向**：删除 `tempTables` 名集合，改为**从解析结果派生 temp 性**——即 `res.Relation.IsCTE` / `res.Relation.IsSubquery`（`ResolvedColumn.Relation` 已经携带该信息，各 `traceThroughTableLineage*` 路径正是这么用的）。若必须保留名字集合，至少要按 (qualifier, name, scope 身份) 建索引并与产出该边的 scope 对应。

**建议新增语料**：别名与真实表同名的 CTE / 派生表 / 目标表各一例。

---

### P0-2　`UPDATE` / `ON CONFLICT` 的赋值源若落在 CTE 或派生表上，血缘不会展开而是被丢弃　— ✅ 已在第一批修复（见 §0）

**位置**：`analyzer.go:965-1025`（`processAssignments`），对照 `analyzer.go:1295-1323`（`generateEdgeFromSource`，正确实现了展开）

`processAssignments` 解析出 `res` 后**直接** `a.addRelation(NewLineageEdge(res.Ref...))`，从未判断 `res.Relation.IsCTE || res.Relation.IsSubquery` 并调用 `traceThroughTableLineageToTarget`。而 `generateEdgeFromSource`、`processViewStmt`、`processCreateTableAsStmt`、`multiAssignSources` 都做了这件事。于是 temp 源被写成一条指向临时关系的边，紧接着被 P0-1 的过滤器丢掉。

**证据**

| SQL | 分析器结果 | 对照 |
| --- | --- | --- |
| `UPDATE t SET a = q.x FROM (SELECT x FROM s) q WHERE t.id = q.id` | **0 条边** | — |
| `WITH c AS (SELECT x FROM s) UPDATE t SET a = c.x FROM c WHERE t.id = c.id` | **0 条边** | — |
| `WITH c AS (SELECT y FROM s) INSERT INTO t (b) SELECT y FROM c ON CONFLICT (b) DO UPDATE SET b = c.y` | ON CONFLICT 部分**无源**边 | — |
| `INSERT INTO t (a) SELECT x FROM (SELECT x FROM s) q`（对照） | ✅ `s.x → t.a` | 正确展开 |
| `CREATE VIEW v AS WITH c AS (SELECT x FROM s) SELECT x FROM c`（对照） | ✅ `s.x → v.x` | 正确展开 |

**影响**：MANUAL_SQL 里"用 CTE/派生表回填列"是非常常见的写法，这类语句目前血缘为 0。

**修复方向**：在 `processAssignments` 的 `for _, res := range resolutions` 循环里补上与 `generateEdgeFromSource` 相同的分支判断（`IsCTE || IsSubquery` → `traceThroughTableLineageToTarget`），或把两处合并成一个公共函数，避免再次分叉。

**语料缺口**：现有 `08_test_update` 的 "UPDATE with CTE" 用例赋值是常量，源只出现在 WHERE 里，恰好绕过了这个缺陷。

---

### P0-3　集合运算把第一个分支的表达式变换伪造给所有分支　— ✅ 已在第一批修复（见 §0）

**位置**：`analyzer.go:314-338`（`mergeUnionOutputColumns`）

```go
firstCol.SourceColumns = mergedSources          // 合并所有分支的源
firstCol.Transform = append([]Transformation{transform}, firstCol.Transform...)
baseScope.SetOutputColumn(colIdx, firstCol)     // 但只保留分支 0 的 Transform
```

每个输出列在合并时把**所有分支的 source** 合并进来，却只保留**分支 0 自身的变换**，然后让所有源共用它。分支 1..n 的表达式信息被丢弃，且分支 0 的表达式被错误地安到别的分支的列上。

**证据**

```
SELECT a + 1 AS x FROM t1 UNION ALL SELECT b + 2 AS x FROM t2 UNION ALL SELECT c + 3 AS x FROM t3
```
实际输出：
```
t1.a → __result__.x  [UNION, OPERATOR "+" expr="a + 1"]   ✅
t2.b → __result__.x  [UNION, OPERATOR "+" expr="a + 1"]   ❌ 应为 "b + 2"
t3.c → __result__.x  [UNION, OPERATOR "+" expr="a + 1"]   ❌ 应为 "c + 3"
```

```
SELECT a AS x FROM t1 UNION SELECT b + 2 AS x FROM t2
```
实际输出：两条边都只有 `[UNION]`，**`b + 2` 完全消失**。

`Transformation` 是**持久化并渲染到前端**的（`plan/postgresql_expression_transformation_plan.md` 已确认：`LineageTransformationCell.vue` 展示 `expression`/`functionName`/`arguments` 等）。所以这是**用户可见的错误数据**，不是内部实现细节。

**为什么语料没抓到**：`05_test_union_lineage_table.yaml` 的全部用例分支都是裸列引用，变换列表里只有 `[UNION]`，正好掩盖了合并逻辑。

**修复方向**：合并时按分支保留各自的 source→transform 配对（即不要先把 source 拍平成一个列表），或在 `ColumnRelation` 层面允许"同一目标的多个源各自携带各自变换"。后者属于模型问题（见 D3）。

---

### P0-4　嵌套集合运算丢失内层运算类型；`UNION ALL` 与 `UNION` 无法区分　— ✅ 部分修复（见 §0）

**位置**：`analyzer.go:252-260`（`flattenSetOpArms`）、`analyzer.go:341-352`（`setOpTransformation`）

`flattenSetOpArms` 把整个集合运算树**无条件拍平**成叶子 SELECT 列表，`mergeUnionOutputColumns` 只把**根节点**的 `stmt.Op` 作为前导变换写给所有列。因此：

```
SELECT a FROM t1 UNION SELECT b FROM t2 INTERSECT SELECT c FROM t3
```
实际输出：三条边全部 `relation_type=union`、变换 `{UNION}`。`t2.b`/`t3.c` 之间的 `INTERSECT` 消失。

另外 `Transformation` 没有 `All`/`Distinct` 字段，`UNION` 与 `UNION ALL` 产生完全相同的元数据：

```
SELECT a FROM t1 UNION ALL SELECT a FROM t2   →  {UNION}
SELECT a FROM t1 UNION     SELECT a FROM t2   →  {UNION}
```

**说明**：`model.RelationTypeOf` 的既定规则是"多变换时取最外层"，所以把外层 UNION 标给所有列**部分**符合现有约定；但**内层运算完全丢失**仍是信息缺陷，且与 P0-3 叠加后元数据已不可信。

---

## 4. P1 中等问题

### P1-1　`inferColumnAlias` 没有实现 PostgreSQL 的输出列命名规则（影响 MANUAL_SQL）

**位置**：`expr.go:510-518`

对没有 `AS` 别名的表达式，回退为**表达式原文**。PostgreSQL 的规则完全不同：

| 表达式 | PostgreSQL 列名（实测 16） | 分析器给出 |
| --- | --- | --- |
| `aa + bb` | `?column?` | `aa + bb` |
| `created_at::date` | `created_at` | `created_at::date` |
| `length(name)` | `length` | `length(name)` |
| `count(*)` | `count` | `count(*)` |

**严重性受限的原因**：实测 `pg_get_viewdef` **总会补齐别名**（`aa + bb AS "?column?"`、`created_at::date AS created_at`、`length(name) AS length`、`sum(a) OVER w AS sum`），所以同步来的 VIEW/MATVIEW 不受影响；只有 `MANUAL_SQL` 会产出与注册表列名不一致的目标列名。

**注**：MySQL 的 `inferredColumnAlias` 有明确注释说"表达式原文就是引擎给的名字"——这在 MySQL 下成立，**复制到 PostgreSQL 就不成立了**。属于跨方言移植时未适配的典型。

### P1-2　CTAS 的显式列名列表被忽略（`Into.ColNames` 只在 MATVIEW 生效）

**位置**：`analyzer.go:1202-1211`，注释写"Only a materialized view honors an explicit column list here"

实测 PostgreSQL 完全支持并采纳该列表：

```
CREATE TABLE dst (p, q) AS SELECT a, d FROM s2;   -- 实测列名为 p, q
```
分析器输出目标列为 `dst.a`、`dst.d`。虽然 `runner` 不分析 TABLE 对象，MANUAL_SQL 仍会命中。

### P1-3　命名窗口（`WINDOW w AS (...)` + `OVER w`）丢失窗口子句与其中依赖的列

**位置**：`expr.go:389-407`（`windowClauses` 只处理 `WindowDef` 的 `PartitionClause`/`OrderClause`，不处理 `Refname`）；`SelectStmt.WindowClause` 全程未被处理

```
SELECT sum(x) OVER w AS s FROM t WINDOW w AS (PARTITION BY y ORDER BY z)
```
分析器输出：只有 `t.x → __result__.s`，变换 `WINDOW SUM`，`PartitionBy`/`OrderBy` 为空；**`t.y`、`t.z` 完全没被记录为源**。

对照实测：内联窗口 `SUM(x) OVER (PARTITION BY y ORDER BY z)` 是正确的，会记录 `t.x`/`t.y`/`t.z`。

**生产可达**：实测 `pg_get_viewdef` **原样保留** `WINDOW w AS (PARTITION BY y ORDER BY z)`，所以同步视图会命中此缺陷。

### P1-4　未 emit 的作用域里的谓词会泄漏到外层输出

**位置**：`predicate.go:90-97`（`collectPredicates`）、`predicate.go:137-155`（`emitPredicateInfluences`）

`a.predicates` 是一条语句级扁平列表，只由"最后一个 emit 者"消费。`generateEdges` 在非根作用域会提前 return（不 emit），于是在**被丢弃/未被引用**的作用域里收集到的谓词会挂到外层结果上：

```
WITH unused AS (SELECT id FROM t WHERE x = 1) SELECT 1 AS one
```
分析器输出：`t.x → __result__.` `{FILTER "x = 1"}` —— **`t` 根本没被这条语句读取**，是凭空捏造的边。

同理 `SELECT a FROM t WHERE EXISTS (SELECT 1 FROM t2 WHERE t2.x = 1)` 会把 `t2.x` 挂到结果上（这条语义上尚可接受，但机制是同一个"泄漏"）。

**反向问题**：`UPDATE` 里的谓词被收集后**永远无人 emit**（见 P1-5），直接沉默丢弃。

### P1-5　`UPDATE ... WHERE` / `UPDATE ... FROM ... WHERE` 不产生任何行级影响边（与 DELETE/SELECT 不对称）

**位置**：`analyzer.go:930-954`（`processUpdateStmt` 从不调用 `emitPredicateInfluences`）

- `SELECT` / `INSERT` / `VIEW` / CTAS → `FILTER`、`JOIN` 影响边；
- `DELETE` → `__deletion__` 边；
- `UPDATE` → **什么都没有**，连 `processFromClause` 收集的 JOIN 条件也一起丢失。

```
UPDATE t SET a = s.x FROM s WHERE t.id = s.id AND s.flag = 1
→ 只有 s.x → t.a，t.id / s.id / s.flag 全无
```

**这条是"文档化决策"而非纯疏忽**：MySQL 语料 `09_test_update_lineage_table.yaml` 头部明确写"Predicate influence edges are recorded for SELECT-based statements only. … only DELETE models it"。但它是一个**容易踩坑的不对称规则**，且 `processUpdateStmt` 收集了谓词却无人消费，属于"半成品"状态。建议要么明确补齐 `__update__` 风格的标记，要么在代码里显式丢弃并注释，别让它悬着。

### P1-6　去重键忽略变换，导致第二条不同的变换被静默丢弃

**位置**：`analyzer.go:1502-1505`

```go
signature := fmt.Sprintf("%s.%s.%s->%s.%s.%s", src.Schema, src.Name, src.Column, tgt.Schema, tgt.Name, tgt.Column)
```

- `SELECT x + 1 AS a, x + 2 AS a FROM t` → 只保留第一条，`x + 2` 丢失；
- 同一列同时作为投影源和谓词源且目标列相同时，第二个 `FILTER` 条件（含不同 `Condition` 文本）会丢；
- 键用 `.` 拼接，带点的引用标识符（`"a.b".c`）理论上可构造碰撞。

### P1-7　MERGE 失败会连带丢弃同一语句串里其它合法语句的结果

**位置**：`analyzer.go:154-158`（`a.errors`），`analyzer.go:130-132`（有 error 就整体 `return nil`）

```
SELECT a FROM t1; MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET a = s.a
→ ERR: "analysis errors: MERGE analysis is not implemented yet"，nil
```

`SELECT a FROM t1` 本来能算对，但整串结果是 nil。运行侧 `markAnalysisFailed` 会**清空**该对象已有血缘（`backend/runner/lineageanalyzer/analyzer.go:491-501`）。对 MANUAL_SQL 多语句脚本，"加了一句 MERGE" 会让整份手写 SQL 的血缘归零。

"fail loudly" 本身是既定决策（与 StarRocks 一致），但**失败半径**（丢弃同一输入中其他语句的边）未被评估。建议至少保留能算的语句的边，仅在 `error_message` 中标注未支持语句。

### P1-8　`SELECT ... INTO` 与 `RETURNING` 未建模

- `analyzer.go:1192-1197`：`IsSelectInto` 被当作裸 SELECT，目标是 `__result__` 而非新建的表（注释说明是有意为之，但对 MANUAL_SQL 意味着目标丢失）。
- 各 DML 的 `ReturningList` 全程未处理：`INSERT ... RETURNING`、`DELETE ... RETURNING` 里被读出的列不构成源。

---

## 5. P2 低风险 / 健壮性 / 卫生问题

| ID | 位置 | 问题 |
| --- | --- | --- |
| P2-1 | `analyzer.go:544-565` | `tempColumnNames` 采纳声明的列名列表时**不校验 arity**，与 `exposedColumnNames`（校验）和 `processCTE`（校验）不一致 |
| P2-2 | `analyzer.go:1099-1103` | `processDeleteStmt` 解析失败时回退成未解析的 `condCol`，可能产出源表为空的边；与"未解析的源一律丢弃"的既定规则相悖 |
| P2-3 | `analyzer.go:1280-1284` | `INSERT INTO t (a) SELECT x, y FROM s`（PG 会拒绝的非法 SQL）会外推出目标列 `t.y`，而不是止于声明列表 |
| P2-4 | `analyzer.go:768-784` | `processTableStar` 找不到限定符对应的关系时静默 return：`SELECT unknown_alias.* FROM t` 产出 0 条边，既无 wildcard 兜底也无诊断 |
| P2-5 | `analyzer.go:480-482` | `RangeFunction` 被忽略：`SELECT * FROM unnest(t.arr) u` 产出 0 条边，`t.arr` 丢失 |
| P2-6 | `analyzer.go:1570-1578` | `combineTransformations` 用 `append(base, additional...)` 不复制。当前各生产者恰好返回 cap==len 所以不可达，但一旦有人返回带余量的 slice，就会写坏被多条边共享的底层数组——**潜在别名污染** |
| P2-7 | `model/relation.go:33` vs `scope/scope.go:280` | 列名匹配一处用 `==`（`AnsweringLineage`），一处用 `strings.EqualFold`（`resolveInScope`），大小写策略不统一 |
| P2-8 | 全局 | **没有诊断通道**：所有 `if err != nil { continue }`（解析失败的列、`ResolveColumnRefs` 失败）都静默丢弃，"部分血缘"与"完整血缘"从外部无法区分；`a.errors` 除了 MERGE 从不被写入 |
| P2-9 | `analyzer.go:108-135` | 分析过程不检查 `ctx` 取消；`a.ctx` 仅用于 catalog 查询。超大 SQL 无法中断 |
| P2-10 | `analyzer.go:124-128` | 逐语句重置的状态是手工枚举的（`scopeStack`/`tempTables`/`predicates`）。当前正确，但新增字段时极易漏掉（历史上就出过 CTE 名字/谓词跨语句泄漏，见 118-126 行注释） |
| P2-11 | `analyzer.go:1548` | `NewLineageEdge` 被导出但**无包外调用者**（`grep` 确认），按仓库导出规则应改为非导出 |
| P2-12 | `scope/types.go:6-16` | `ColumnRef.Resolved` 的注释写"StarRocks 设置它，其它分析器一律保持 false"——**已过时**：PostgreSQL 在 `resolveOutputColumns`（`expr.go:136-159`）、`wildcardSourceRef`（`analyzer.go:789-796`）、`expandWildcardWithCatalog`（`analyzer.go:1531-1538`）都会设置它 |
| P2-13 | `analyze_test_helper.go` | 文件名**没有 `_test.go` 后缀**，会被编译进生产包并引入 `testing` + `testutil`（mysql/starrocks 同样问题）。属仓库级模式，建议一并收拾 |

---

## 6. 技术债与架构

### D1　三份近重复的分析器，`predicate.go` 是逐行拷贝（最重的一项债务）

- `mysql/analyzer.go` 2208 行、`starrocks/analyzer.go` 1384 行、`postgresql/{analyzer,expr,predicate}.go` 2255 行。
- `predicate.go`：PG 155 行 vs StarRocks 167 行，`diff` 后**除 AST 类型名外几乎完全一致**（`JoinExpr`→`JoinClause`、`Quals`→`On`、`Larg/Rarg`→`Left/Right` …）。这一层算法只依赖 `scope.ColumnRef` 与 `model.Transformation`，本应与方言无关。
- 同名重复函数（PG ∩ MySQL ∩ StarRocks）：`combineTransformations`、`resolveOutputColumns`、`flattenSetOpArms`、`setOpTransformation`、`tempColumnNames`、`exposedColumnNames`、`attachTempColumnLookup`、`containsGroupAggregate`、`wildcardSourceRef`、`normalizeExpressionText`、`joinSideRefs`、`usingClauseText`、`insertSourceMap`，以及整套 benchmark 夹具（`buildWideSelect` / `buildDeepSubquery` / `buildManyJoins` / `loadCorpusBenchCases` / `BenchmarkAnalyze*`）。
- **后果已经在本次 review 中体现**：P0-2（赋值未展开 temp 源）和 P0-3（集合运算变换归并）都是"同一逻辑写三份、只在其中一份演进"的典型症状。
- `dupl` **未启用**（`.golangci.yaml` 的 `enable` 列表里没有），所以 lint 是 0 issues，重复无人拦。
- **建议**：把与方言无关的部分（谓词影响、temp 展开、集合运算归并、边去重、benchmark 夹具、语料加载）下沉到 `lineage/` 下的公共层，方言侧只保留一个薄 AST 适配器。收益不只是行数，更是**修复不会只落在一种方言上**。

### D2　两套"临时关系"判定并存（P0-1 的结构性根因）

`tempTables`（语句级名字集合）与作用域查找（`FindCTE`/`FindTable`）是两套真值来源，`addRelation` 用前者、`isTemp` 字段用后者。`ResolvedColumn.Relation` 已经携带了权威的 `IsCTE`/`IsSubquery` 信息，名字集合纯属冗余且危险。

### D3　`Transformation` 模型无法表达"同一目标的各源由不同表达式产生"

`ColumnRelation` 是 (source, target, []Transformation) 三元组，而算法是**按输出列**聚合的（一个输出列一个 `Transform`，多个 source 共享）。集合运算合并把这个矛盾放大成 P0-3。需要一次模型层决策：要么允许每条边独立变换，要么在合并时按分支拆边。

### D4　语料严格但覆盖有盲区

机制本身很好：默认**精确边匹配**（多一条边即失败）、`KnownFields(true)` 拒绝拼写错误、`RequireFullEdgeAnnotations` 强制每个期望都断言 `relation_type`/`is_temp`/两端列名、禁止 `expected_edges:` 空值写法。问题在**用例选取**（见 §7）。

### D5　性能只有基准、没有预算

`benchmark_test.go` 覆盖语料整体 / 单例 / 纯解析 / 多种形状 / catalog 通配展开，做得不错；但没有断言阈值，回归不可见。另外 `addRelation` 每条边一次 `fmt.Sprintf` 建签名，是可省掉的分配。

---

## 7. 测试覆盖缺口

以下方向**当前语料完全没有覆盖**，且大多正是本次发现的缺陷所在：

| # | 缺口 | 对应缺陷 |
| --- | --- | --- |
| 1 | 表达式分支的 UNION/UNION ALL（现有 05 全是裸列） | P0-3、P0-4 |
| 2 | 嵌套集合运算 `A UNION B INTERSECT C` | P0-4 |
| 3 | CTE 名 / 派生表别名与真实表同名 | P0-1 |
| 4 | 目标表名与 CTE 名相同的 INSERT/UPDATE | P0-1 |
| 5 | UPDATE/ON CONFLICT 的赋值源是 CTE/派生表 | P0-2 |
| 6 | 命名窗口 `WINDOW w AS (...)` + `OVER w` | P1-3 |
| 7 | 递归 CTE（`WITH RECURSIVE`） | P1-5 之外的 M5 |
| 8 | 未被引用的 CTE / 子查询谓词泄漏 | P1-4 |
| 9 | CTAS 显式列名列表 | P1-2 |
| 10 | 未具名表达式的目标列名 | P1-1 |
| 11 | MERGE 与同串合法语句混用 | P1-7 |

另外：`mysql`/`starrocks` 都有 `registration_test.go` 断言引擎注册，**postgresql 包没有**（只靠 `init()` 与集成测试间接验证）。集成测试 `backend/test/integration/runner/schemasync_lineage_postgres_service_test.go` 有 9 个用例，覆盖 schema sync → lineage 端到端、物化视图列、外部表通配、视图变更后更新、视图删除后清理、MANUAL_SQL、列元数据历史等——**质量不错，但都是"链路通"级别，不校验具体边集合**，因此上面 11 个缺口它一个也抓不到。

---

## 8. 文档与注释漂移

| 位置 | 问题 |
| --- | --- |
| `plan/postgresql_omni_parser_migration_plan.md` Follow-ups 表 | **PG-FU-5**（`EXCLUDED.col` 解析到 INSERT 源）仍列为待办，但 `analyzer.go:1027-1041` 的 `replaceExcluded` 已经实现，`onconflict_test.go` 也已在断言。文档未同步 |
| `plan/postgresql_omni_parser_migration_plan.md` 状态行 | 顶部仍写 "PG-FU-1 是 deferred/tracked"，而 PG-FU-1 已在 `plan/postgresql_expression_transformation_plan.md` 落地 |
| `analyzer.go:1-6` 包注释 | 只指向迁移 plan；结构化表达式分类（PG-FU-1）的真正落点是另一个 plan，未交叉引用 |
| `scope/types.go:6-16` | 见 P2-12，已过时 |
| `analyzer.go:1207` | "Only a materialized view honors an explicit column list here" —— 描述的是**当前实现的限制**，但读起来像 PG 的语义，容易误导（实际 PG 支持 CTAS 列名列表，是我们没处理，见 P1-2） |

---

## 9. 值得肯定的部分

- **无 panic 路径**：`currentScope()` 理论上可返回 nil，但 push/pop 在所有路径上平衡（含 CTE、子查询、集合运算、multi-assign），逐条走过未发现可达空栈。
- **`Loc` 字节偏移处理正确**：`exprText` 的边界守卫有效；含中文表达式（`SELECT name || '中文' AS x FROM t`）切片正确，未出现 rune/byte 混淆。
- **标识符大小写与注册表对齐**：依赖 omni 的 PG 折叠规则，`identifier_case_test.go` 用 GUID 等价性把这件事钉死了——设计是对的。
- **解析策略清晰**：解析错误整体硬失败、多语句逐条分析、错误写入 `column_lineage_version.error_message`；`markAnalysisFailed` 同时清空旧血缘，语义自洽。
- **确定性**：`scope.sortedRelations` 排序 + `tempTables` 重置 + 显式去重，输出顺序稳定（历史上"未限定列随机解析"的缺陷已被 `scope_test.go` 钉住）。
- **注释质量高**：几乎每个非显然分支都写了"为什么"，并且指出了对应的生产事故（例如 `21_test_parenthesized_join` 对应的 `pg_get_viewdef` 括号化 JOIN 缺陷）。本次 review 能快速区分"决策"与"缺陷"，主要归功于这些注释。
- **语料机制严格**（见 D4 前半）。
- **lint 干净**：该包 `golangci-lint run` 0 issues（`GOCACHE`/`GOLANGCI_LINT_CACHE` 指向可写目录后实测）。

---

## 10. 建议的修复顺序（backlog）

**第一批（P0，建议一次 PR 内一起做，因为彼此耦合）**

1. **P0-1**：删除 `tempTables` 名集合，temp 判定改为依据 `ResolvedColumn.Relation.IsCTE/IsSubquery`（或按 scope 身份索引）。补 4 条语料。
2. **P0-2**：`processAssignments` 补 temp 展开分支（与 `generateEdgeFromSource` 对齐，最好抽公共函数）。补 2 条语料。
3. **P0-3 + P0-4**：重做集合运算归并，按分支保留各自的变换；顺带决定 `UNION ALL` 是否需要在 `Transformation` 上加字段（涉及 proto/前端则单独排期）。

**第二批（P1）**

4. **P1-5 / P1-4**：先修谓词的**归属**（谓词应当绑到产生它的 emit 目标，而不是语句级扁平队列），再决定 UPDATE 是否要产出影响边。这两个同一处机制。
5. **P1-3**：处理 `WindowDef.Refname` 与 `SelectStmt.WindowClause`。
6. **P1-7**：MERGE 改为"记录错误 + 保留其余语句边"，避免 MANUAL_SQL 血缘归零。
7. **P1-2 / P1-1**：CTAS 列名列表；`inferColumnAlias` 按 PG 规则（函数→函数名、cast→内层列名、其它→`?column?`）——**注意这是 MANUAL_SQL 专用**，改前先确认不会破坏现有语料里刻意保留的 MySQL 式行为。
8. **P1-6 / P1-8**：去重键加入变换维度；`RETURNING` / `SELECT INTO` 目标建模。

**第三批（债）**

9. **D1**：启动"方言无关算法下沉"重构（建议先只下沉 `predicate.go` + 集合运算归并 + benchmark 夹具，小步走）；同时把 `dupl` 加进 `.golangci.yaml` 防止回潮。
10. **D3**：`Transformation` 与按列聚合的模型冲突做一次显式设计决策并写入 `plan/`。
11. **P2 批量清理** + 文档漂移修正（§8）。
12. **D5**：给基准加阈值（可放进 CI，用 `-benchtime` 小样本 + `benchstat` 阈值）。

---

## 附录 A：复现方法

本次 review 的探针已删除，复现只需在包内加一个临时 `_test.go`，用现成的 `analyzeSQL` 辅助函数：

```go
package postgresql

import "testing"

func TestRepro(t *testing.T) {
    for _, sql := range []string{
        // P0-1
        "WITH orders AS (SELECT user_id, total FROM orders WHERE total > 100) SELECT user_id, total FROM orders",
        "SELECT * FROM t WHERE a IN (SELECT x FROM (SELECT y AS x FROM s) t)",
        "WITH t AS (SELECT id FROM s) INSERT INTO t (a) SELECT id FROM t",
        // P0-2
        "WITH c AS (SELECT x FROM s) UPDATE t SET a = c.x FROM c WHERE t.id = c.id",
        "UPDATE t SET a = q.x FROM (SELECT x FROM s) q WHERE t.id = q.id",
        // P0-3 / P0-4
        "SELECT a + 1 AS x FROM t1 UNION ALL SELECT b + 2 AS x FROM t2",
        "SELECT a FROM t1 UNION SELECT b FROM t2 INTERSECT SELECT c FROM t3",
        // P1
        "WITH unused AS (SELECT id FROM t WHERE x = 1) SELECT 1 AS one",
        "SELECT sum(x) OVER w AS s FROM t WINDOW w AS (PARTITION BY y ORDER BY z)",
        "UPDATE t SET a = s.x FROM s WHERE t.id = s.id AND s.flag = 1",
        "SELECT x + 1 AS a, x + 2 AS a FROM t",
    } {
        rels, err := analyzeSQL(sql, nil)
        t.Logf("SQL: %s\nERR: %v", sql, err)
        for _, r := range rels {
            t.Logf("   %s.%s -> %s.%s [%v temp=%v] %+v",
                r.Source.Table.Name, r.Source.Name,
                r.Target.Table.Name, r.Target.Name,
                r.RelationType, r.IsTemp, r.Transformation)
        }
    }
}
```

```bash
GOCACHE=/tmp/gocache-metaxis go test ./backend/plugin/lineage/postgresql/ -run TestRepro -v -count=1
```

## 附录 B：真实 PostgreSQL 16 验证记录

```bash
docker run -d --name pg-review -e POSTGRES_PASSWORD=review -p 55432:5432 postgres:16-alpine
```

以下结论由该实例实测得出（容器已删除）：

| 校验点 | 结果 |
| --- | --- |
| `WITH t AS (SELECT id FROM t) SELECT id FROM t;` | **执行成功**；内层 `t` 解析为基表（`WITH t2 AS (SELECT aa FROM t2) SELECT * FROM t2` 返回基表行）→ 证实 P0-1 |
| `SELECT * FROM t WHERE a IN (SELECT x FROM (SELECT y AS x FROM s) t);` | **执行成功**，外层读真实表 `t` → 证实 P0-1 |
| `pg_get_viewdef('v_count')` 其中 `CREATE VIEW v_count AS SELECT count(*) FROM t` | `SELECT count(*) AS count FROM t;` |
| `pg_get_viewdef` 其中 `CREATE VIEW v_expr2 AS SELECT aa + bb, created_at::date, length(name) FROM t2` | `aa + bb AS "?column?"`、`created_at::date AS created_at`、`length(name) AS length` → 确认视图定义**总带别名**，P1-1 主要影响 MANUAL_SQL |
| `CREATE TABLE dst (p, q) AS SELECT a, d FROM s2;` | `information_schema.columns` 列名为 **p, q** → 证实 P1-2 |
| `CREATE TABLE dst2 AS SELECT a + d, count(*) FROM s2 GROUP BY a + d;` | 列名为 `?column?`, `count` → 佐证 P1-1 |
| `pg_get_viewdef` 其中 `CREATE VIEW vw AS SELECT sum(a) OVER w FROM wt WINDOW w AS (PARTITION BY y ORDER BY z)` | 定义**原样保留** `WINDOW w AS (...)` → 证实 P1-3 对同步视图可达 |

---

## 附录 C：外部依赖相关观察（仅内部记录，未对外反馈）

以下不是本仓库代码的缺陷，按要求仅记录在本文件中，**未向任何外部方反馈**：

1. `github.com/bytebase/omni` 的 `pg/ast` 中，命名窗口 `WindowDef.Refname` 只携带窗口名、不携带被引用的窗口定义；`SelectStmt.WindowClause` 也不在 omni 提供的便捷访问器里。本仓库需要自己遍历 `WindowClause` 并在 `Refname` 上做一次解析（P1-3 的修复因此比内联窗口麻烦一点）。这是**依赖的 AST 表达力**问题，不是 bug。
2. `plan/postgresql_omni_parser_migration_plan.md` 的 **PG-FU-2** 已经提出"把 DML/集合运算/窗口保留的 walker 回馈上游 omni"，本次 review 的 P0-2/P0-3 恰好是支持该方向的额外证据——若上游能提供生产级 `analysis` 包，本仓库三份分析器（D1）可显著收缩。是否需要推进属产品决策，本次不推动。
