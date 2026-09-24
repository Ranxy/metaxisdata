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

## 0b. 第二批（P1）实施状态：已落地

第二批 5 项已全部处理，全仓 build/test/lint 通过。

| 项 | 状态 | 落地内容 |
| --- | --- | --- |
| **P1-4 / P1-5** 谓词归属 | ✅ 已修复 / 已决策 | `a.predicates` 由"语句级扁平队列"改为 `map[*scope.Scope][]predicateInfluence` + `map[*scope.CTEDefinition][]...`；谓词归属到产生它的 scope，并由**消费该 scope 行集的一方继承**（派生表/表达式子查询 → 外层查询；CTE → 引用它的查询；集合运算分支 → 合并结果）。未被引用的 CTE 的谓词随之丢弃。`emitPredicateInfluences` 改为按 scope 发射。**UPDATE 是否产出影响边：决定维持跨方言既定规则（仅 SELECT 类语句记录谓词影响，只有 DELETE 另有 `__deletion__`），并在 `processUpdateStmt` 上写明这是有意丢弃而非半成品** |
| **P1-3** 命名窗口 | ✅ 已修复 | 新增 `namedWindows`（按查询的 WINDOW 子句建立索引）与 `namedWindowDefinitions`（跟随 `WINDOW w2 AS (w1 …)` 链、带环保护）；`windowClauses` 与列收集都会展开 `OVER w`。顺带补齐命名窗口的**帧边界**列（`ROWS BETWEEN z PRECEDING`），使命名与内联两种写法产出完全一致的列与变换 |
| **P1-7** MERGE 连带失败 | ✅ 已修复 | 新增 `lineage.UnsupportedStatementError`；`AnalyzeRelations` 返回**已算出的边 + 该错误**（解析错误仍是硬失败、无部分结果）；runner 识别该类型后**保留血缘**并把缺口写进 `error_message`（`markAnalyzed` 而非 `markAnalysisFailed`） |
| **P1-2 / P1-1** 目标列命名 | ✅ 已修复 | CTAS 的 `Into.ColNames` 不再只对 MATVIEW 生效；`inferColumnAlias` 改为按 **PostgreSQL 实际命名规则**（对 16 实测 33 个表达式）：函数→函数名、cast→被 cast 表达式名否则目标类型名、`CASE`/`COALESCE`/`GREATEST`/`LEAST`/`NULLIF`/`ARRAY`/`ROW`/`GROUPING`/SQL 值函数→各自名字、其余→`?column?`；标量子查询取首列名 |
| **P1-6 / P1-8** 边身份与 DML 输出 | ✅ 已修复 | 去重键改为结构体 `edgeKey`（含变换的规范渲染，并用标识符值本身而非 `.` 拼接，避免含点标识符碰撞）；`SELECT … INTO` 建模为写入目标表；DML 的 `RETURNING` 列注册为语句输出，**数据修改型 CTE 因此能把返回列传给读取它的查询**（原先整条链断掉） |

**第二批过程中发现并修复的两个附带缺陷**（都是被掩盖/新暴露的，不修就是"修一个露一个"）：

1. **ON CONFLICT 子句的作用域错绑**。原实现让冲突子句的作用域以语句作用域为父，于是 `SET quantity = shipment.quantity` 会解析到 INSERT 的 SELECT 关系上；旧去重键恰好把它与合法边合并，**把错误藏住了**。改为**脱离父作用域**（只含目标表 + EXCLUDED）。真实 PostgreSQL 16 实测：`SET quantity = shipment.quantity` 与 `SET b = c.y`（语句级 CTE）都报 `missing FROM-clause entry`，而 `SET b = EXCLUDED.b + t.b` 合法 —— 与修复后行为一致。
2. **`SELECT … INTO` 根本没走到目标分支**。omni 把 `SELECT … INTO` 解析成带 `IntoClause` 的普通 `SelectStmt`，而原代码只在 `CreateTableAsStmt.IsSelectInto` 上判断，那条分支实际不可达。现在 `processSelectStmt` 直接分派 INTO，并与 CTAS/MATVIEW 共用同一个目标发射路径（`emitOutputColumnsToTarget`，同时消除了三处重复代码）。

**新增语料**

- `34_test_predicate_attribution_table.yaml`（P1-4，7 例）
- `35_test_named_window_lineage_table.yaml`（P1-3，4 例）
- `36_test_output_target_lineage_table.yaml`（P1-2/P1-1/P1-8，8 例）
- `37_test_edge_identity_lineage_table.yaml`（P1-6，1 例）
- 更新既有语料 4 处：`15_test_on_conflict`、`25_test_predicate_influence`（用例名从 "keeps one edge" 改为 "carries both influences"）、`26_test_assignment_resolution`、`32_test_assignment_temp_source`；新增 Go 测试 `TestUnsupportedStatementKeepsOtherStatements`
- `29_test_statement_lineage` 的 MERGE 用例改名并注明新的部分成功语义

**已知残留（记录在案，非本次范围）**

- ~~ON CONFLICT 子句内**子查询**引用语句级 CTE~~ → **已在第四批修复**（见 §0d）。原判断"需要一层只暴露 CTE 的中间作用域"方向对了一半：缺的不是一层新作用域，而是把「关系」与「CTE 定义」两个命名空间分开（`NewScopeWithDefinitions`）。危害也比当时记的更重——不是丢边，而是把 CTE 名注册成基表、产出指向不存在关系的边。
- 顶层 DML 的 `RETURNING` 不再产出 `__result__` 边（有意决策：返回行是面向客户端的结果，语句的血缘是它执行的写入）。数据修改型 CTE 的情形已完整建模。

**第二批之后仍待处理**：§5 的 P2（13 项）、§6 的 D1/D3/D5、§7 剩余测试缺口。

---

## 0c. 第三批（技术债 + P2 + 基准 + UNION ALL）实施状态：已落地

第三批覆盖 §10 的第 9～12 项，加上 §5 的 P2 批量清理与 §7 的注册测试缺口。落地为六个提交：

```
df064aa feat(lineage): distinguish a set operation that keeps duplicate rows
91b9143 test(lineage): give the analyzer an allocation budget and share the benchmarks
1fad8df fix(lineage): clear the low-risk findings from the PostgreSQL review
4e50f61 fix(lineage): fix the defects the sibling dialects kept after PostgreSQL
4dbf876 refactor(lineage): sink the dialect-neutral lineage algorithm
（本文件与 plan/lineage_transformation_model.md 的提交）
```

| 项 | 状态 | 落地内容 |
| --- | --- | --- |
| **D1** 方言无关算法下沉 | ✅ 已落地 | 新增 `lineage/algorithm`：`EdgeSet`（边身份＝两端标识符值 + 逐字段比较的变换列表）、`Influences`（按 scope 归属的影响）、`MergeSetOpColumns` + `ArmChain`（集合运算按分支合并、保留运算链）、`TraceThroughTableLineage*` / `FlattenTempSources*`（查询局部关系展开）。`model.CombineTransformations`、`model.SameTransformations`、`model.ResultTableName`、`scope.NewSchemaLineageEdge`、`scope.SetOutputColumns` 收拢原先各写一份的逻辑；benchmark 夹具与驱动下沉 `testutil`。PostgreSQL 迁入后**输出逐字节不变**（见下方验证） |
| **D1 连带：兄弟方言同类缺陷** | ✅ 已修复 | 实测确认 MySQL/TiDB/MariaDB/StarRocks 至今仍带着 P0-1、P0-2、P1-4、P1-6、P0-4 五类缺陷，随之下沉一并修复（`tempTables`/`markTempTable`/`isTempRelation`/`isTableTempInCurrentScope` 全部删除，目标永远不是查询局部关系） |
| **D3** Transformation 模型决策 | ✅ 已决策 | 新增 `plan/lineage_transformation_model.md`：变换随源走（`scope.ColumnSource`）、一条派生一条边且变换参与身份、列表有序且首项为最外层、集合运算用 `All` 位；并明确模型**不**表达什么（递归不动点、表达式结构、SORT/GROUP_BY 生产者、按列分组） |
| **P0-4 残留**：`UNION ALL` | ✅ 已修复（含 proto 与前端） | `Transformation.All` + `proto/v1` 字段 10 + API 映射 + 前端 `UNION ALL` 展示与「重复行 ALL/DISTINCT」明细 + 语料支持 `all:` 断言并钉住两种写法与混合链（`INTERSECT ALL`/`EXCEPT ALL` 同样覆盖） |
| **D5** 基准预算 | ✅ 已落地 | `TestAnalyzeAllocationBudget`：8 个形状 + 全语料一条**分配次数**预算（约 50% 余量）。用次数而非时间：时间预算在忙的 CI 上失败、在闲的机器上通过，是不会被信任的检测器；语料总量那条是通用回归网（每边多一次分配就会抬升总量） |
| **P2** 13 项 | ✅ 11 项已处理，2 项决策保留 | P2-1/2/3/5/6/7/9/10/11/12/13 见 §5 表格的 ✅；P2-4 与 P2-8 见下方「第三批的决策保留」 |
| **§7** 测试缺口 | ✅ 已补 | 新增 `postgresql/registration_test.go`（引擎注册 + 未注册引擎哨兵）；语料新增 `38_test_set_returning_function`、`24_test_query_local_name`（兄弟方言，含 StarRocks 副本 `23_`）、`all:` 系列用例 |
| **§8** 文档漂移 | ✅ 已同步 | PG-FU-1/PG-FU-5 标注 LANDED；本包 doc comment 交叉引用表达式 plan 与模型 plan；`scope/types.go` 注释重写；`analyzer.go` 那句"只有物化视图采纳列名列表"已在第二批删除 |

### 第三批发现（原报告没有的）

1. **原 §10 第 9 条「把 dupl 加进 `.golangci.yaml`」的前提不成立。** 实测 golangci-lint 的 `dupl` **只在同一个包内两两比较**：`mysql`/`tidb`/`mariadb` 三份逐字节相同的 2000 行它一声不响，PG↔StarRocks 的 `predicate.go` 也看不到；全仓启用后只有 16 处发现，全部落在 `plugin/schema`、`plugin/db/pg`、`store/role.go` 等无关包，lineage 包 **0 处**。因此本批**不启用 dupl**，改用仓库内的跨包哨兵测试 `backend/plugin/lineage/shared_analysis_test.go`：它解析各方言包的非测试文件，断言 `edgeKey`/`EdgeSet`/`Influences`/`predicateInfluence`/`mergeUnionOutputColumns`/`combineTransformations`/`isTempRelation`/`markTempTable` 等**不再出现在方言包里**，同时断言 `algorithm` 仍导出这些机制（哨兵本身做过反向验证：临时加回一个 `markTempTable` 会使其失败）。
2. **兄弟方言的缺陷是实测出来的，不是推断的。** `WITH orders AS (SELECT user_id, total FROM orders …) SELECT … FROM orders` 在 MySQL 家族与 StarRocks 都返回 **0 条边**（P0-1 同级）；`WITH unused AS (SELECT id FROM t WHERE x=1) SELECT 1` 伪造 `t.x → __result__`（P1-4）；`SELECT x+1 AS a, x+2 AS a FROM t` 只留第一条（P1-6）；`A UNION B INTERSECT C` 三条全标 UNION（P0-4）；`UPDATE t JOIN (SELECT x FROM s) q … SET t.a = q.x` 指向不存在的 `q`（P0-2）。这些已随本批修复，并由新增语料钉住。
3. **递归 CTE 的自引用判定从"比名字"改成"注册自身 CTE"。** 原实现（第一批）按 `res.Ref.Table == cteName` 丢弃自引用，只覆盖**源**列，且会连一个真正同名的基础表一起丢掉；而 `WITH RECURSIVE tree AS (… JOIN tree t ON n.parent_id = t.id)` 的 **JOIN 谓词**仍会产出一条指向 CTE 的边。现在递归 CTE 在自己的 body 作用域里以空 lineage 注册自身，于是源与谓词都自然落空。PostgreSQL 的语料与探针输出在此改动前后完全一致。
4. **`RangeFunction`（P2-5）不是清理而是补功能，本批实现了。** `SELECT u.x FROM t, LATERAL unnest(t.arr) AS u(x)` 原先 0 条边，现在报 `t.arr → __result__.x`；`ROWS FROM` 每个函数一列、`WITH ORDINALITY` 增加一列无源的列、参数不含列（如 `generate_series(1,10)`）则不产生边。

### 第三批的决策保留

- **P2-4 / P2-8（没有诊断通道）**：按用户决策，本轮**不动**「部分血缘 vs 完整血缘」的区分，仅记录。因此 `SELECT unknown_alias.* FROM t` 仍静默返回 0 条边，`if err != nil { continue }` 仍静默丢弃。要做需要先决定诊断落点（复用 `UnsupportedStatementError` 写进 `column_lineage_version.error_message`，还是新增 Warnings 概念并落库），属独立一轮。
- ~~**ON CONFLICT 子句内子查询引用语句级 CTE**（第二批残留）仍未建模~~ → **已在第四批修复**（见 §0d），并顺带修掉 MySQL 家族 upsert 值里的子查询丢血缘。
- **顶层 DML 的 `RETURNING` 不产出 `__result__` 边**：仍是有意决策。
- **UPDATE 不产出谓词影响边**：跨方言既定规则，已在代码注释中写明是有意丢弃。

### 验证方式

先把修复前的完整输出抓成快照（5 个方言 × 57 条 review 探针 × 全部语料，5645 行），每批之后逐行 diff：

- 第一批下沉（`4dbf876`）后：**全文件逐字节相同**。
- 兄弟方言修复（`4e50f61`）后：PostgreSQL 两个 section **仍逐字节相同**；MySQL 家族与 StarRocks 的差异逐条核对，全部是 P0-1/P0-2/P1-4/P1-6/P0-4 的修复或由此新增的正确边（`orders.*`、`t.a` 谓词、`r.id` 递归 JOIN 影响、`x+1`/`x+2` 两条、集合运算链）。
- P2 批（`1fad8df`）后：除新增语料用例本身外**无任何输出变化**（说明 EqualFold、删除回退、INSERT arity、CTE 列名规范化、ctx 检查都不改变既有行为）。
- 全仓 `go build ./...`、`go test ./...`、`golangci-lint`（0 issues）、前端 `biome`/`eslint`/`vue-tsc`/`vitest`（85 例）均通过；`go test -bench` 冒烟运行确认基准夹具搬迁无碍。

---

## 0d. 第四批（upsert 子句的作用域与来源收集）实施状态：已落地

第四批处理 §0c 残留清单的第一条：**ON CONFLICT 子句内的子查询读不到语句级 CTE**。顺带按用户决策并入两项同类收尾（集合运算分支的 CTE 拷贝 workaround、MySQL 家族 upsert 值里的子查询血缘）。

```
755ca35 fix(lineage): read a statement's CTEs from inside an upsert clause
（本文件的提交）
```

| 项 | 状态 | 落地内容 |
| --- | --- | --- |
| **ON CONFLICT 子句内子查询引用语句级 CTE** | ✅ 已修复 | `scope` 新增「定义来源」这一维：`Scope.definitions` + `NewScopeWithDefinitions(parent, definitions)`。`FindCTE`（FROM 路径）穿透 definitions，`findCTEQualifier`（列限定符路径）不穿透——这正是 PostgreSQL 的非对称规则。`processOnConflict` 改用 `NewScopeWithDefinitions(nil, a.currentScope())`：关系仍完全断开（保住第一批"冲突子句不能命名 SELECT 的关系"的决策），CTE 定义可达 |
| **集合运算分支的 CTE 拷贝 workaround** | ✅ 已按新链接统一 | 5 个方言的 `scope.NewScope(baseScope.Parent())` + `for _, cte := range baseScope.CTEs() { tempScope.AddCTE(cte) }` 改为 `scope.NewScopeWithDefinitions(baseScope.Parent(), baseScope)`；`Scope.CTEs()` 随之删除（它只为这份拷贝而存在）。快照实测 5 方言逐字节一致 |
| **MySQL 家族：upsert 值里的子查询丢血缘** | ✅ 已修复 | 删掉自由函数 `collectUpsertSources`（其中 `case *nodes.SubqueryExpr: return false` 把子查询整个跳过），与 `collectExprColumns` 合并为 `collectExpressionSources(expr, sp, upsertValues)`：`VALUES(col)` 走 `insertSources`，子查询按自己的作用域展开。tidb/mariadb 为从 mysql 重新生成（`copies_test.go` 钉住逐字节一致） |
| 语料 | ✅ 已补 | PG `32_test_assignment_temp_source` +4 例（基本形、重复读不重复计影响、数据修改型 CTE 内的兄弟 CTE、声明的列名列表）；MySQL `23_test_assignment_resolution` +3 例（子查询取数、子查询读语句级 CTE、子查询内列归属），后三条在 tidb/mariadb 通过 `sharedCorpusDir()` 各跑一遍 |
| 单测 | ✅ 已补 | `scope/scope_test.go` 的 `TestScopeWithDefinitions`：嵌套 FROM 可达定义 / 限定符不可达 / 语句关系不可见 / 以语句为 parent 时关系照旧可见 |
| 文档 | ✅ 已同步 | 本条残留改为 LANDED；附录 B / B2 记录真实引擎实测；附录 C 记录 omni 的 MySQL 解析限制 |

### 第四批发现（原报告与 §0c 都没有的）

1. **PG 的规则是两个方向的，原记录只写了一半。** 16.15 实测：`ON CONFLICT (b) DO UPDATE SET b = c.y`（直接限定符）报 `missing FROM-clause entry for table "c"`（原语料已钉住）；而 `SET b = (SELECT y FROM c)` **执行成功**——子句本身不能命名 CTE，但子句里的子查询是独立查询层级，它的 FROM 可以。根因不是"缺一层中间作用域"，而是 `NewScope(nil)` 把 CTE 定义与语句关系一起切断了；切断的后果不是丢边而是**退化**：`processRangeVar` 找不到 CTE 就注册成基表，产出指向不存在关系的边（实测 `c.y -> t.b`、`c.m -> t.b`、`c.* -> t.b`、`SELECT * FROM c` 的 `c.*`）。**假边比缺边更危险**，因为它会被持久化成一个指向不存在关系的血缘。
2. **PG 里 CTE 只能由 FROM 命名，任何层级都不能作为列限定符。** `WITH c AS (...) SELECT c.a FROM stage`、`SELECT (SELECT c.a) FROM stage`、`ON CONFLICT ... SET a = (SELECT c.a)` 在 16.15 全部报 `missing FROM-clause entry for table "c"`。本仓库 `ResolveColumnRefs` 有一条"CTE 未注册为关系时仍按定义解析"的宽松路径，因此这些**非法**形状在本仓库能解析出血缘（超集）。按用户决策**保持现状**（实测收紧后全语料仍全绿，只有 2 个 `scope` 单测钉住它），记录为已知偏差。
3. **MySQL 家族的 ODKU 子句能命名语句关系，但不能用限定符命名 CTE。** MariaDB 11.8.9 实测：`ON DUPLICATE KEY UPDATE a = stage.a` **接受**（与 PG 相反，所以 MySQL 分析器用语句作用域是对的，不要照搬 PG 的脱离写法）；`a = c.a` 报 `ERROR 1054 (42S22) Unknown column 'c.a' in 'UPDATE'`；`a = (SELECT a FROM c)` **接受**（所以它的子查询也该读到 CTE）。
4. **MySQL 家族 upsert 值的子查询血缘被整条丢弃，且子查询内的列会被错误归属。** `ON DUPLICATE KEY UPDATE a = (SELECT max(x) FROM other)` 修复前**零边**（同一个分析器的 `UPDATE ... SET t.a = (SELECT max(x) FROM other)` 产出 `other.x -> t.a`——同一份代码里自相矛盾）；`a = (a IN (SELECT x FROM other))` 修复前产出 `stage.x -> t.a`，把子查询里的 `x` 归属到外层关系上；`a = (EXISTS (SELECT 1 FROM other WHERE other.id = stage.id))` 修复前产出 `stage.id -> t.a`。修复后子查询按自己的作用域展开（`other.x`），EXISTS 那条不再产出——与 `collectExprColumns`（UPDATE 路径）的既有规则一致：**子查询的过滤列不是血缘**。
5. **冲突子句的 `OnConflictClause.WhereClause`（DO UPDATE 的 WHERE）没有被遍历** —— 后续复查把它从"技术现象"厘清成"策略适用点"，并决定保持不建模。

   **事实三层**。① omni 正确解析它：`OnConflictClause.WhereClause` 是 `*ast.A_Expr`，且与 `Infer.WhereClause`（索引谓词）区分清楚，所以这是我们的选择而不是依赖缺口。② 只"走一遍并收集"（不 emit）实测与现状**逐字节一致**（15 个形状 + 5 方言语料）：冲突子句作用域没有 emitter，`emitPredicateInfluences` 只在根作用域与 INSERT 的源作用域被调用，而后者已经跑完；所以"遍历不产出边"成立，但原因是策略而非巧合。③ 真正的问题是它属于哪一类行集：

   | 语句 | 决定行集的谓词 | 现状（实测） |
   | --- | --- | --- |
   | `INSERT ... SELECT ... WHERE`（源查询谓词） | ✅ 建模 | `stage.flag -> t.` + FILTER |
   | `DELETE ... WHERE` | ✅ 建模 | `t.flag -> t.__deletion__` + DELETE |
   | `UPDATE ... WHERE`（含 FROM/JOIN 条件） | ❌ 不建模 | 只出 SET 的边（P1-5，跨方言既定） |
   | `INSERT ... ON CONFLICT DO UPDATE ... WHERE` | ❌ 不建模 | 只出 SET 的边 |

   **决策（保持不建模 + 语料钉住）**：该子句是**一个 UPDATE 的行集**，按 P1-5 不建模，与 `UPDATE ... WHERE` 同一规则。`15_test_on_conflict` 新增一条用例断言"带 WHERE 与不带 WHERE 的边逐条相同"（含目标列谓词与一个读 `audit` 的子查询），把现状变成有意为之；实测把该子句改成会 emit（把它当作 INSERT 的源谓词那样发 `-> t.` + FILTER）时这条用例会失败，说明钉子有效。**考虑过但未采纳的两条路**：C 把它作为目标行集影响发出来（PG-only，实测 +9 条边），代价是 `-> t.` 这个边形状同时表示"插入选中的行"与"冲突时改写的行"，且 upsert 建模而 UPDATE 不建模更不一致；D 重开 P1-5 让 DML 行集规则统一（UPDATE 的 WHERE/JOIN 一起建模，跨方言、改既有语料，`Transformation.operation` 是字符串故不需改 proto，但前端 `LineageTransformationCell.vue` 的 switch 默认分支走 `expression` 而 DELETE 用 `condition`，要加一个 case）——留作独立议题。
6. **RETURNING 有两个层次的问题，本批原先只看到了一层；后续复查把两层都测清了。两层均已修复（见 §0e）。**

   **6a（结构错误，合法 SQL 可达）**：`processReturning` 把 RETURNING 的列**追加**到当前作用域的 output columns 上，而这个列表正是「本查询对外暴露的列」——对 data-modifying CTE 来说就是该 CTE 的列。于是 `INSERT ... SELECT ... RETURNING id` 的 CTE 暴露的是「SELECT 的列 + RETURNING 的列」，列名撞车时把 INSERT 的源列算成 CTE 返回的列。实测：`WITH ins AS (INSERT INTO t (id, a) SELECT id, a FROM stage RETURNING id) SELECT id FROM ins` 产出 `stage.id -> __result__.id` **和** `t.id -> __result__.id`；而 PG 16.15 里该 CTE 只有一列 `id`（`SELECT a FROM ins` 报 `column "a" does not exist`），正确只有后者。`SELECT * FROM ins` 更明显：本仓库 4 条边，PG 只返回 1 列。可达性：data-modifying CTE 不能出现在视图里（`WITH` 里的数据修改语句必须在顶层），来源只有 MANUAL_SQL——但它是**合法 SQL**，PG 会执行。修法极小：`processReturning` 用 `SetOutputColumns` **替换**而不是追加，没有 RETURNING 时置空（PG 拒绝引用没有 RETURNING 的 data-modifying CTE）。

   **6b（作用域越权，只影响非法 SQL）**：RETURNING 的作用域是语句作用域的子作用域。PG 的规则**按语句种类不同**（16.15 实测）：`INSERT ... SELECT ... RETURNING stage.a` 与 `INSERT ... ON CONFLICT ... RETURNING excluded.a` 都报 `missing FROM-clause entry`（EXCLUDED 只在冲突子句自己的表达式里可见），但 `UPDATE ... FROM stage ... RETURNING stage.id` 与 `DELETE ... USING stage ... RETURNING stage.id` **都被接受**。所以"把 RETURNING 作用域脱离父作用域"这个做法**是错的**：实测脱离后上面那条 UPDATE 的合法边 `stage.id -> __result__.id` 会消失（回归）。正确的模型是「语句自己的关系」——INSERT 只有目标表（它的 SELECT 是嵌套查询层级），UPDATE/DELETE 是目标 + FROM/USING 关系。而且单靠脱离**无效**：`resolveOutputColumns` 在解析失败时原样保留引用，交给后面能看到更多关系的作用域（CTE 的 lineage 构建）二次绑定；实测「脱离」与「不脱离」输出逐字节一致。要让失败成为终局必须改 `resolveOutputColumns` 的失败策略——实测改成严格丢弃后 **5 个方言 3542 行语料零变化**，即没有任何一条现有语料依赖这条推迟解析。

   **两层的可选修法（已决策：用户选 R1′ + R2-ii，已落地，见 §0e）**：R1 = 6a 的 `SetOutputColumns` 替换（一行，实测语料零变化，且 9 个 RETURNING 形状里 5 条假边消失、UPDATE/DELETE 的合法边与 `RETURNING (SELECT y FROM c)` 都不受影响）；R1′ = R1 + 无 RETURNING 时置空；R2 = 6b：仅在 INSERT 上脱离（保留 definitions 链接）+ 把 `resolveOutputColumns` 的失败改成终局丢弃——实测组合后只剩的那条非法 SQL 边也消失、UPDATE/DELETE 不受影响、语料仍全绿，代价是要按语句种类分派作用域构造并决定严格丢弃是否全局；R2 也可以再进一步，把 INSERT 的 SELECT 放进自己的嵌套作用域（结构性对齐 PG，但要改 INSERT 的边生成与影响 emit，面最大）。若两者都做，严格丢弃应当"丢弃 + 上报"，也就是说它与 §0c 保留的 P2-4/P2-8 诊断通道是同一件事的两半。MySQL 家族不涉及此项：MariaDB 11.8.9 不允许 data-modifying CTE（实测 `ERR 1064`），其分析器也完全不读 `Returning`。 **落地结果**：R2-ii 把 INSERT 的源挪进自己的查询层级之后，6b 的假边自然消失，`resolveOutputColumns` 的严格丢弃**不再需要**——失败引用已没有可以二次绑定的作用域。
7. **upsert 常量赋值的建模不对称（新发现，未修）**：PG `ON CONFLICT ... DO UPDATE SET b = 'x'` 产出 `t.* -> t.b`；MySQL `ON DUPLICATE KEY UPDATE a = 1` 产出**零边**（`len(sourceColumns) == 0` 即 `continue`），而 MySQL 自己的 `UPDATE t SET a = 1` 产出 `t.* -> t.a`。三者都是"整行被重写"，只有 MySQL 的 upsert 沉默。需要先决定常量写入是否值得一条 `t.*` 边，再决定是否对齐。

### 验证方式

- **快照**：用 `ZZ_DUMP_OUT` 导出 5 个方言的全部语料（2108 行、含变换/is_temp/relation_type），分步 diff。ON CONFLICT 修复后**逐字节一致**；集合运算统一后**逐字节一致**；MySQL ODKU 修复后**逐字节一致**；新增 7 条语料后 diff **只包含这 7 条**（PG +4 例 219、MySQL +3 例 202）。
- **新语料负向校验**：把 7 个代码文件整体回退到修复前（保留新语料），4 条 PG 新用例与 3 条 MySQL 新用例**全部失败**。
- **探针 diff**：14 条 PG 形状 + 11 条 MySQL 形状 before/after 逐条核对，变化只有"假表 → 真实源"与 MySQL 侧子查询归属的修正；`SET b = c.y`、`(SELECT x FROM other)`（普通表）、`a = a + VALUES(a)`、`VALUES(a) + (SELECT ...)` 的值部分、`REPLACE INTO`、集合运算语料均为**不变**。
- **引擎实测**：PostgreSQL 16.15（附录 B）、MariaDB 11.8.9（附录 B2）；两个容器均为一次性，用完删除。
- `go build ./...`、`go test ./...`、`golangci-lint run ./backend/...`（0 issues）全绿。不涉及 proto 与前端。

---

## 0e. 第五批（RETURNING 的结构对齐）实施状态：已落地

第五批处理 §0d 发现 6 的两层：6a（data-modifying CTE 暴露的列被 INSERT 的源列污染）与 6b（RETURNING 能命名 INSERT 的 SELECT 关系）。用户选择**结构性对齐**而不是"脱离作用域 + 严格丢弃"的补丁式方案。

```
0f4c847 fix(lineage): analyze an insert's source as the subquery it is
（本文件的提交）
```

| 项 | 状态 | 落地内容 |
| --- | --- | --- |
| **R2-ii：INSERT 的源是一个查询层级** | ✅ 已落地 | `processInsertSource` 把 INSERT 的 SELECT/VALUES/TABLE 源推进自己的作用域再弹出；`generateEdgesForDataModification` 与 `insertSourceMap` 改为接收该作用域，不再读 `currentScope()`。语句作用域因此只剩「语句自己的关系」——INSERT 为空，UPDATE/DELETE 是目标 + FROM/USING。PostgreSQL 正是这样分析的（INSERT 的源是子查询），于是冲突子句与 RETURNING 都自然看不到源关系 |
| **R1′：RETURNING 暴露的列 = RETURNING 列表** | ✅ 已落地 | `processReturning` 用 `SetOutputColumns(columns)` **替换**而非追加；没有 RETURNING 时置空（PG 拒绝引用这样的 CTE） |
| 语料 | ✅ 已补 | `36_test_output_target` +7 例：只暴露返回列（含 `SELECT *` 读法）、不能命名源关系、无 RETURNING 不暴露列、UPDATE/DELETE 的 RETURNING 仍能命名自己的 FROM/USING 关系（回归护栏）、链式 data-modifying CTE 读到的是前一个返回的列 |
| 冲突子句的 WHERE（§0d 发现 5） | ✅ 已决策保留 | 不建模——它是 UPDATE 的行集，按 P1-5 处理；`15_test_on_conflict` 补一条钉子，断言带 WHERE 与不带 WHERE 的边逐条相同（改决策必须是一次显式 diff） |
| 注释 | ✅ 已同步 | `processOnConflict` 的"为什么仍要脱离"改写：源关系已结构上不可见，脱离现在只为「CTE 名不能当限定符」这一条服务；`generateEdgesForDataModification` 的文档改为「INSERT 的写边」并说明为何接收源作用域 |

### 第五批发现

1. **结构性对齐之后，那条共享解析策略的改动不再需要。** §0d 里的 R2 方案（脱离 + 把 `resolveOutputColumns` 的失败改成终局丢弃）实测可行，但把 INSERT 的源挪进嵌套作用域之后，`RETURNING stage.id` 解析失败留下的引用**再也没有可以二次绑定的作用域**（语句作用域已无源关系），那条非法 SQL 的边自然消失——不必去改 5 个方言共享的策略。实测 13 条 RETURNING 形状全部与 PostgreSQL 一致。
2. **两个假边源的成因不同，必须分开修。** 6a 的假边来自「暴露列列表被污染」（SELECT 的列被当成 CTE 的返回列），6b 的假边来自「RETURNING 能命名源关系」。实测只做 R1′ 时 `RETURNING stage.id` 仍产出 `stage.id -> __result__.id`；只做 R2-ii 而不做 R1′ 时 6a 的污染仍在。两者都修才是现在的结果。
3. **链式 data-modifying CTE 的源从"写入的源"变成"返回的列"，这是 PG 语义而不是回归。** `WITH a1 AS (INSERT INTO t (id) SELECT id FROM stage RETURNING id), a2 AS (INSERT INTO t2 (id) SELECT id FROM a1 RETURNING id) SELECT id FROM a2` 里 `a1.id` 读到的是目标表自己的列，所以 `t2.id` 的源是 `t.id`；修复前 `a1` 暴露了被污染的列，同一条 `t2.id` 被同时算成 `stage.id` 与 `t.id`（多一条假边）。语料已按正确语义钉住。
4. **INSERT 的谓词影响边不受影响**：影响本来就绑定在源查询的作用域上，本次只是把 emit 的作用域从语句作用域换成源作用域。实测 37 条 INSERT/DML 形状（含 `WHERE`/JOIN/`IN (SELECT …)`/CTE/集合运算/聚合/`VALUES`/`DEFAULT VALUES`/`TABLE`/arity 不匹配/子查询源/多语句脚本/`ON CONFLICT` 各变体/`UPDATE`/`DELETE`）before/after 逐条核对，除假边消失与源被改正外无变化。

### 验证方式

- **快照**：5 个方言 3542 行语料 before/after **逐字节一致**，说明本次改动不改任何既有语料行为——被修的是语料没覆盖到的形状。
- **形状探针**：13 条 RETURNING 形状 + 37 条 INSERT/DML 形状 before/after 逐条核对，变化只有假边消失与源被改正；无新增、无回归。
- **新语料负向校验**：7 条新用例里 5 条在修复前失败；另 2 条（UPDATE/DELETE 的 RETURNING 命名自己关系）是回归护栏，修复前后都应通过。
- `go build ./...`、`go test ./...`、`golangci-lint run ./backend/...`（0 issues）全绿。仍不涉及 proto/前端；MySQL 家族与 StarRocks 不受影响（不建模 RETURNING，MariaDB 也不允许 data-modifying CTE）。

---

## 0f. AST 字段覆盖审计发现（待决策）

第五批之后做了一次结构字段覆盖审计：把 omni 的语句/查询节点字段与"分析器是否读取"对照，用 40 余个形状在真实 PostgreSQL 16 上验证。表达式层面是安全的（`extractColumnsFromNode` 用 `pgast.Inspect` 泛走，任何表达式节点里的列都会被找到），风险集中在**结构性字段**——跨批发现的 P2-4/P2-5、冲突子句的 WHERE、RETURNING 的两层都属于这个模式。本轮审计新发现三条，**均未修复**：

| # | 字段 | 现象 | 实测 | 判定 |
| --- | --- | --- | --- | --- |
| **A1** | `SelectStmt.ValuesLists`（从未读取） | `INSERT INTO t (a) VALUES ((SELECT max(x) FROM other))` **零边** | PG 16 实测 `INSERT 0 1`（真的把 `other.x` 写进 `t.a`）；同样影响 `INSERT ... VALUES (1), ((SELECT ...))`、`SELECT * FROM (VALUES ((SELECT ...))) v(a)`、`INSERT ... SELECT v.a FROM (VALUES ((SELECT ...))) v(a)`；5 方言语料里 `VALUES ((` **零覆盖** | **建议修**：与 P0-2 同类，合法 SQL 上整条源静默丢失 |
| **A2** | `GroupingSet.Content`（`groupByKeys` 只做 `nodeTexts`） | `GROUP BY ROLLUP (t.a, t.b)` / `CUBE` / `GROUPING SETS (...)` → `group_keys: [""]` —— **一个空字符串键** | 普通 `GROUP BY t.a, t.b` 正常给出 `[t.a t.b]`；组集形态给出 `[""]`（`%#v` 确证，不是空列表） | **建议修**：错误元数据，且语料严格注解会把这个空键固定下来 |
| **A3** | `RangeTableFunc`（XMLTABLE） | `SELECT * FROM t, XMLTABLE('/a' PASSING t.doc COLUMNS x int PATH 'x') q` 丢掉 `t.doc` | 同族 `unnest(t.arr)` 正常给出 `t.arr -> __result__.x`（P2-5 已实现） | **建议只记录**：XMLTABLE 在同步视图里极罕见 |

同批确认**无问题**、以免以后重复怀疑的形状：`DISTINCT` / `DISTINCT ON`（与既有语料一致：只算投影，不算行集影响）、`TABLESAMPLE`、`FOR UPDATE`、`WITH RECURSIVE ... SEARCH/CYCLE`、`= ANY (SELECT ...)`（两侧 FILTER 都在）、`ARRAY(SELECT ...)`、`ROW(...)`、`COLLATE`、`greatest(...)`、窗口帧 `ROWS BETWEEN`、`unnest`、`count(*) FILTER (WHERE ...)`。

**审计方法**（可复现）：临时在包内加一个 `_test.go`，对每个形状打印边集合与变换；对照 `pgast` 节点的字段清单逐项检查分析器是否读取；可疑形状用真实引擎确认它合法且可执行。**建议后续把这张"字段 × 是否读取"的对照表补全并归档**，作为收口"某个 clause 没被读"这类缺陷的系统手段。

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

### P1-1　`inferColumnAlias` 没有实现 PostgreSQL 的输出列命名规则（影响 MANUAL_SQL）　— ✅ 已在第二批修复（见 §0b）

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

### P1-2　CTAS 的显式列名列表被忽略（`Into.ColNames` 只在 MATVIEW 生效）　— ✅ 已在第二批修复（见 §0b）

**位置**：`analyzer.go:1202-1211`，注释写"Only a materialized view honors an explicit column list here"

实测 PostgreSQL 完全支持并采纳该列表：

```
CREATE TABLE dst (p, q) AS SELECT a, d FROM s2;   -- 实测列名为 p, q
```
分析器输出目标列为 `dst.a`、`dst.d`。虽然 `runner` 不分析 TABLE 对象，MANUAL_SQL 仍会命中。

### P1-3　命名窗口（`WINDOW w AS (...)` + `OVER w`）丢失窗口子句与其中依赖的列　— ✅ 已在第二批修复（见 §0b）

**位置**：`expr.go:389-407`（`windowClauses` 只处理 `WindowDef` 的 `PartitionClause`/`OrderClause`，不处理 `Refname`）；`SelectStmt.WindowClause` 全程未被处理

```
SELECT sum(x) OVER w AS s FROM t WINDOW w AS (PARTITION BY y ORDER BY z)
```
分析器输出：只有 `t.x → __result__.s`，变换 `WINDOW SUM`，`PartitionBy`/`OrderBy` 为空；**`t.y`、`t.z` 完全没被记录为源**。

对照实测：内联窗口 `SUM(x) OVER (PARTITION BY y ORDER BY z)` 是正确的，会记录 `t.x`/`t.y`/`t.z`。

**生产可达**：实测 `pg_get_viewdef` **原样保留** `WINDOW w AS (PARTITION BY y ORDER BY z)`，所以同步视图会命中此缺陷。

### P1-4　未 emit 的作用域里的谓词会泄漏到外层输出　— ✅ 已在第二批修复（见 §0b）

**位置**：`predicate.go:90-97`（`collectPredicates`）、`predicate.go:137-155`（`emitPredicateInfluences`）

`a.predicates` 是一条语句级扁平列表，只由"最后一个 emit 者"消费。`generateEdges` 在非根作用域会提前 return（不 emit），于是在**被丢弃/未被引用**的作用域里收集到的谓词会挂到外层结果上：

```
WITH unused AS (SELECT id FROM t WHERE x = 1) SELECT 1 AS one
```
分析器输出：`t.x → __result__.` `{FILTER "x = 1"}` —— **`t` 根本没被这条语句读取**，是凭空捏造的边。

同理 `SELECT a FROM t WHERE EXISTS (SELECT 1 FROM t2 WHERE t2.x = 1)` 会把 `t2.x` 挂到结果上（这条语义上尚可接受，但机制是同一个"泄漏"）。

**反向问题**：`UPDATE` 里的谓词被收集后**永远无人 emit**（见 P1-5），直接沉默丢弃。

### P1-5　`UPDATE ... WHERE` / `UPDATE ... FROM ... WHERE` 不产生任何行级影响边（与 DELETE/SELECT 不对称）　— ✅ 已在第二批决策并写明（见 §0b）

**位置**：`analyzer.go:930-954`（`processUpdateStmt` 从不调用 `emitPredicateInfluences`）

- `SELECT` / `INSERT` / `VIEW` / CTAS → `FILTER`、`JOIN` 影响边；
- `DELETE` → `__deletion__` 边；
- `UPDATE` → **什么都没有**，连 `processFromClause` 收集的 JOIN 条件也一起丢失。

```
UPDATE t SET a = s.x FROM s WHERE t.id = s.id AND s.flag = 1
→ 只有 s.x → t.a，t.id / s.id / s.flag 全无
```

**这条是"文档化决策"而非纯疏忽**：MySQL 语料 `09_test_update_lineage_table.yaml` 头部明确写"Predicate influence edges are recorded for SELECT-based statements only. … only DELETE models it"。但它是一个**容易踩坑的不对称规则**，且 `processUpdateStmt` 收集了谓词却无人消费，属于"半成品"状态。建议要么明确补齐 `__update__` 风格的标记，要么在代码里显式丢弃并注释，别让它悬着。

### P1-6　去重键忽略变换，导致第二条不同的变换被静默丢弃　— ✅ 已在第二批修复（见 §0b）

**位置**：`analyzer.go:1502-1505`

```go
signature := fmt.Sprintf("%s.%s.%s->%s.%s.%s", src.Schema, src.Name, src.Column, tgt.Schema, tgt.Name, tgt.Column)
```

- `SELECT x + 1 AS a, x + 2 AS a FROM t` → 只保留第一条，`x + 2` 丢失；
- 同一列同时作为投影源和谓词源且目标列相同时，第二个 `FILTER` 条件（含不同 `Condition` 文本）会丢；
- 键用 `.` 拼接，带点的引用标识符（`"a.b".c`）理论上可构造碰撞。

### P1-7　MERGE 失败会连带丢弃同一语句串里其它合法语句的结果　— ✅ 已在第二批修复（见 §0b）

**位置**：`analyzer.go:154-158`（`a.errors`），`analyzer.go:130-132`（有 error 就整体 `return nil`）

```
SELECT a FROM t1; MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET a = s.a
→ ERR: "analysis errors: MERGE analysis is not implemented yet"，nil
```

`SELECT a FROM t1` 本来能算对，但整串结果是 nil。运行侧 `markAnalysisFailed` 会**清空**该对象已有血缘（`backend/runner/lineageanalyzer/analyzer.go:491-501`）。对 MANUAL_SQL 多语句脚本，"加了一句 MERGE" 会让整份手写 SQL 的血缘归零。

"fail loudly" 本身是既定决策（与 StarRocks 一致），但**失败半径**（丢弃同一输入中其他语句的边）未被评估。建议至少保留能算的语句的边，仅在 `error_message` 中标注未支持语句。

### P1-8　`SELECT ... INTO` 与 `RETURNING` 未建模　— ✅ 已在第二批修复（见 §0b）

- `analyzer.go:1192-1197`：`IsSelectInto` 被当作裸 SELECT，目标是 `__result__` 而非新建的表（注释说明是有意为之，但对 MANUAL_SQL 意味着目标丢失）。
- 各 DML 的 `ReturningList` 全程未处理：`INSERT ... RETURNING`、`DELETE ... RETURNING` 里被读出的列不构成源。

---

## 5. P2 低风险 / 健壮性 / 卫生问题

| ID | 位置 | 问题 |
| --- | --- | --- |
| P2-1 ✅ | `analyzer.go:544-565` | `tempColumnNames` 采纳声明的列名列表时**不校验 arity**，与 `exposedColumnNames`（校验）和 `processCTE`（校验）不一致 |
| P2-2 ✅ | `analyzer.go:1099-1103` | `processDeleteStmt` 解析失败时回退成未解析的 `condCol`，可能产出源表为空的边；与"未解析的源一律丢弃"的既定规则相悖 |
| P2-3 ✅ | `analyzer.go:1280-1284` | `INSERT INTO t (a) SELECT x, y FROM s`（PG 会拒绝的非法 SQL）会外推出目标列 `t.y`，而不是止于声明列表 |
| P2-4 ⏸ | `analyzer.go:768-784` | `processTableStar` 找不到限定符对应的关系时静默 return：`SELECT unknown_alias.* FROM t` 产出 0 条边，既无 wildcard 兜底也无诊断 |
| P2-5 ✅ | `analyzer.go:480-482` | `RangeFunction` 被忽略：`SELECT * FROM unnest(t.arr) u` 产出 0 条边，`t.arr` 丢失 |
| P2-6 ✅ | `analyzer.go:1570-1578` | `combineTransformations` 用 `append(base, additional...)` 不复制。当前各生产者恰好返回 cap==len 所以不可达，但一旦有人返回带余量的 slice，就会写坏被多条边共享的底层数组——**潜在别名污染** |
| P2-7 ✅ | `model/relation.go:33` vs `scope/scope.go:280` | 列名匹配一处用 `==`（`AnsweringLineage`），一处用 `strings.EqualFold`（`resolveInScope`），大小写策略不统一 |
| P2-8 ⏸ | 全局 | **没有诊断通道**：所有 `if err != nil { continue }`（解析失败的列、`ResolveColumnRefs` 失败）都静默丢弃，"部分血缘"与"完整血缘"从外部无法区分；`a.errors` 除了 MERGE 从不被写入 |
| P2-9 ✅ | `analyzer.go:108-135` | 分析过程不检查 `ctx` 取消；`a.ctx` 仅用于 catalog 查询。超大 SQL 无法中断 |
| P2-10 ✅ | `analyzer.go:124-128` | 逐语句重置的状态是手工枚举的（`scopeStack`/`tempTables`/`predicates`）。当前正确，但新增字段时极易漏掉（历史上就出过 CTE 名字/谓词跨语句泄漏，见 118-126 行注释） |
| P2-11 ✅ | `analyzer.go:1548` | `NewLineageEdge` 被导出但**无包外调用者**（`grep` 确认），按仓库导出规则应改为非导出 |
| P2-12 ✅ | `scope/types.go:6-16` | `ColumnRef.Resolved` 的注释写"StarRocks 设置它，其它分析器一律保持 false"——**已过时**：PostgreSQL 在 `resolveOutputColumns`（`expr.go:136-159`）、`wildcardSourceRef`（`analyzer.go:789-796`）、`expandWildcardWithCatalog`（`analyzer.go:1531-1538`）都会设置它 |
| P2-13 ✅ | `analyze_test_helper.go` | 文件名**没有 `_test.go` 后缀**，会被编译进生产包并引入 `testing` + `testutil`（mysql/starrocks 同样问题）。属仓库级模式，建议一并收拾 |

---

## 6. 技术债与架构

### D1　三份近重复的分析器，`predicate.go` 是逐行拷贝（最重的一项债务）

- `mysql/analyzer.go` 2208 行、`starrocks/analyzer.go` 1384 行、`postgresql/{analyzer,expr,predicate}.go` 2255 行。
- `predicate.go`：PG 155 行 vs StarRocks 167 行，`diff` 后**除 AST 类型名外几乎完全一致**（`JoinExpr`→`JoinClause`、`Quals`→`On`、`Larg/Rarg`→`Left/Right` …）。这一层算法只依赖 `scope.ColumnRef` 与 `model.Transformation`，本应与方言无关。
- 同名重复函数（PG ∩ MySQL ∩ StarRocks）：`combineTransformations`、`resolveOutputColumns`、`flattenSetOpArms`、`setOpTransformation`、`tempColumnNames`、`exposedColumnNames`、`attachTempColumnLookup`、`containsGroupAggregate`、`wildcardSourceRef`、`normalizeExpressionText`、`joinSideRefs`、`usingClauseText`、`insertSourceMap`，以及整套 benchmark 夹具（`buildWideSelect` / `buildDeepSubquery` / `buildManyJoins` / `loadCorpusBenchCases` / `BenchmarkAnalyze*`）。
- **后果已经在本次 review 中体现**：P0-2（赋值未展开 temp 源）和 P0-3（集合运算变换归并）都是"同一逻辑写三份、只在其中一份演进"的典型症状。
- `dupl` **未启用**（`.golangci.yaml` 的 `enable` 列表里没有），所以 lint 是 0 issues，重复无人拦。
  **更正（第三批实测）**：启用 `dupl` 也拦不住这里的重复——它只在**同一个包内**两两比较，而 `mysql`/`tidb`/`mariadb`、PG↔StarRocks 都是跨包。见 §0c 发现 1。
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

另外：`mysql`/`starrocks` 都有 `registration_test.go` 断言引擎注册，**postgresql 包没有**（只靠 `init()` 与集成测试间接验证）。✅ 第三批已补 `postgresql/registration_test.go`（注册 + 未注册引擎哨兵）。集成测试 `backend/test/integration/runner/schemasync_lineage_postgres_service_test.go` 有 9 个用例，覆盖 schema sync → lineage 端到端、物化视图列、外部表通配、视图变更后更新、视图删除后清理、MANUAL_SQL、列元数据历史等——**质量不错，但都是"链路通"级别，不校验具体边集合**，因此上面 11 个缺口它一个也抓不到。

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

**第三批（债）— ✅ 已落地（见 §0c）**

9. **D1** ✅：算法下沉比建议更进一步——不只 `predicate.go`/集合运算归并/benchmark 夹具，连边身份、查询局部关系展开、`SetOutputColumn` 语义一并收拢，兄弟方言同步迁入并修掉同类缺陷。~~同时把 `dupl` 加进 `.golangci.yaml`~~ → **该建议作废**：实测 `dupl` 只做包内比较，看不见本次真正的跨包重复；改为 `backend/plugin/lineage/shared_analysis_test.go` 的跨包哨兵测试（详见 §0c 发现 1）。
10. **D3** ✅：`plan/lineage_transformation_model.md`，并据此补上 `Transformation.All`（含 proto/前端，关掉 P0-4 的 ALL 残留）。
11. **P2 批量清理** ✅ + 文档漂移修正（§8）✅：11 项修复、2 项按决策保留（P2-4/P2-8，见 §0c）。
12. **D5** ✅：改为**分配次数**预算而非 `benchstat` 时间基线——次数与机器无关，语料总量那条能抓住"每边多一次分配"的回归；时间阈值在忙的 CI 上不可靠，`benchstat` 基线还需要一套基线产物与 CI 步骤。

**第四批（§0c 残留的第一条）— ✅ 已落地（见 §0d）**

13. **ON CONFLICT 子句的作用域** ✅：不是"加一层中间作用域"，而是把 `scope` 的「关系」与「CTE 定义」两个命名空间分开（`NewScopeWithDefinitions`）；顺带用同一链接替掉集合运算分支的 CTE 拷贝 workaround（并删除 `Scope.CTEs()`），并修掉 MySQL 家族 upsert 值里子查询血缘被整条丢弃、子查询内列被错误归属的缺陷。常量 upsert 的建模不对称（§0d 发现 7）、PG 冲突子句的 `WHERE` 未遍历（发现 5）、RETURNING 的失败引用会被二次绑定（发现 6）、CTE 限定符的宽松路径（发现 2）四项按决策/依赖记录在案，未修。

**第五批（RETURNING 的结构对齐）— ✅ 已落地（见 §0e）**

14. **RETURNING 的两层问题** ✅：INSERT 的源改为自己的查询层级（`processInsertSource`），RETURNING 暴露的列改为 `SetOutputColumns` 替换。选结构性对齐而不是补丁，结果是共享解析策略不必改动，且 6a/6b 一次解决。

**第六批前置（AST 字段覆盖审计）— 发现已记录（见 §0f）**

15. **A1 `ValuesLists` / A2 `GroupingSet` / A3 `RangeTableFunc`**：三条结构字段缺口，均已用真实 PostgreSQL 16 验证并记录；A1、A2 判定为"建议修"，A3 建议只记录。完整对照表待补。

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
| `WITH c AS (SELECT y FROM s) INSERT INTO t (b) SELECT 1 ON CONFLICT (b) DO UPDATE SET b = (SELECT y FROM c)` | **执行成功** → 冲突子句内**子查询的 FROM 可以**命名语句级 CTE（第四批发现 1） |
| `... ON CONFLICT (b) DO UPDATE SET b = c.y`（同上，直接限定符） | `ERROR: missing FROM-clause entry for table "c"` → 冲突子句**本身不能**命名 CTE（第四批发现 1） |
| `... ON CONFLICT (b) DO UPDATE SET b = (SELECT c.a)` | `ERROR: missing FROM-clause entry for table "c"` → 限定符在任何层级都不能命名 CTE（第四批发现 2） |
| `WITH c AS (...) SELECT c.a FROM stage` / `SELECT (SELECT c.a) FROM stage` | 均报 `missing FROM-clause entry for table "c"` → 同上（第四批发现 2） |
| `INSERT INTO t (id, a) SELECT id, a FROM stage ON CONFLICT (id) DO UPDATE SET a = (SELECT stage.a)` | `ERROR: missing FROM-clause entry for table "stage"` → INSERT 的 SELECT 关系在冲突子句内（含子查询）不可见 |
| `INSERT INTO t (id, a) SELECT id, a FROM stage RETURNING stage.a` / `RETURNING (SELECT stage.a)` | 均报 `missing FROM-clause entry for table "stage"` → RETURNING 也不可见（但本仓库仍能解析，见 §0d 发现 6） |
| `WITH c AS (SELECT id, a FROM src), ins AS (INSERT INTO t (id, a) SELECT 1, 1 ON CONFLICT (id) DO UPDATE SET a = (SELECT a FROM c) RETURNING *) SELECT * FROM ins` | **执行成功** → 数据修改型 CTE 内的冲突子句同样读得到兄弟 CTE（第四批语料第 3 例） |
| `... ON CONFLICT (id) DO UPDATE SET a = 1 WHERE (SELECT count(*) FROM c) > 0` | **执行成功** → DO UPDATE 的 WHERE 里也可以有读 CTE 的子查询（本仓库未遍历，见 §0d 发现 5） |
| `... ON CONFLICT (id) WHERE t.id IN (SELECT id FROM c) DO UPDATE SET a = 1` | **报错**（索引推断谓词不允许子查询）→ 该 `InferClause.WhereClause` 不遍历是对的 |
| `... ON CONFLICT (id) DO UPDATE SET a = (SELECT excluded.a)` / `(SELECT t.a)` / `(SELECT max(x.a) FROM src x WHERE x.id = t.id)` | 均**执行成功** → 子查询内 `EXCLUDED` 与目标关系可按相关引用读到 |
| `UPDATE t SET a = 5 FROM stage WHERE t.id = stage.id RETURNING stage.id` | **执行成功** → UPDATE 的 RETURNING **可以**命名 `FROM` 关系（与 INSERT 相反，见 §0d 发现 6b） |
| `DELETE FROM t USING stage WHERE t.id = stage.id RETURNING stage.id` | **执行成功** → DELETE 的 RETURNING **可以**命名 `USING` 关系 |
| `INSERT ... ON CONFLICT (id) DO NOTHING RETURNING excluded.a` | `ERROR: missing FROM-clause entry for table "excluded"` → EXCLUDED 只在冲突子句自己的表达式里可见，RETURNING 里不可见 |
| `WITH ins AS (INSERT INTO t (id, a) SELECT id, a FROM stage RETURNING id) SELECT a FROM ins` | `ERROR: column "a" does not exist` → data-modifying CTE 只暴露 RETURNING 的列（6a 的判据） |
| `WITH ins AS (INSERT ... ON CONFLICT DO NOTHING) SELECT * FROM ins` | `ERROR: WITH query "ins" does not have a RETURNING clause` → 没有 RETURNING 的 data-modifying CTE 不可引用（6a 的 R1′ 判据） |

## 附录 B2：真实 MariaDB 11.8.9 验证记录

```bash
docker run -d --name mxd-mariasem -e MARIADB_ROOT_PASSWORD=dev -e MARIADB_DATABASE=sem -p 53306:3306 mariadb:11
```

以下结论由该实例实测得出（容器为一次性，用完删除）：

| 校验点 | 结果 |
| --- | --- |
| `INSERT INTO t (id, a) SELECT id, a FROM stage ON DUPLICATE KEY UPDATE a = stage.a` | **执行成功** → MySQL 家族的 ODKU 子句能命名 INSERT 的 SELECT 关系（与 PostgreSQL 相反；所以 MySQL 分析器用语句作用域是对的） |
| `INSERT INTO t (id, a) WITH c AS (SELECT id, a FROM src) SELECT id, a FROM stage ON DUPLICATE KEY UPDATE a = c.a` | `ERROR 1054 (42S22): Unknown column 'c.a' in 'UPDATE'` → 限定符不能命名 CTE（与 PG 同向） |
| `INSERT INTO t (id, a) WITH c AS (SELECT id, a FROM src) SELECT id, a FROM stage ON DUPLICATE KEY UPDATE a = (SELECT a FROM c)` | **执行成功** → 子查询的 FROM 可以命名 CTE（与 PG 同向），而修复前的分析器对这条产出**零边** |
| `... ON DUPLICATE KEY UPDATE a = (SELECT stage.a)` / `(SELECT t.a)` | 均**执行成功** → 子查询内可按相关引用读到语句关系与目标关系 |
| `WITH c AS (...) INSERT INTO t ...`（WITH 在 INSERT 之前） | `ERROR 1064 (42000)` 语法错误 → omni 的 MySQL 解析器对同形状报错是**正确**的（见附录 C 第 3 条） |
| `INSERT INTO t (id, a) SELECT id, a FROM stage RETURNING id` / `... ON DUPLICATE KEY UPDATE a = VALUES(a) RETURNING id, a` / `DELETE FROM t WHERE id = 1 RETURNING id` | 均**执行成功** → MariaDB 支持 RETURNING；本仓库 MySQL 家族分析器不读 `Returning`（与「顶层 DML 的 RETURNING 不产出 `__result__` 边」的既定决策一致） |
| `WITH ins AS (INSERT INTO t (id, a) SELECT id, a FROM stage RETURNING id) SELECT * FROM ins` | `ERROR 1064 (42000)` 语法错误 → **MariaDB 不允许 data-modifying CTE**，所以 §0d 发现 6a 那类缺陷在 MySQL 家族不存在 |

---

## 附录 C：外部依赖相关观察（仅内部记录，未对外反馈）

以下不是本仓库代码的缺陷，按要求仅记录在本文件中，**未向任何外部方反馈**：

1. `github.com/bytebase/omni` 的 `pg/ast` 中，命名窗口 `WindowDef.Refname` 只携带窗口名、不携带被引用的窗口定义；`SelectStmt.WindowClause` 也不在 omni 提供的便捷访问器里。本仓库需要自己遍历 `WindowClause` 并在 `Refname` 上做一次解析（P1-3 的修复因此比内联窗口麻烦一点）。这是**依赖的 AST 表达力**问题，不是 bug。
2. omni 的 MySQL 解析器**不支持 `WITH c AS (...) INSERT ...`**（WITH 写在 INSERT 之前）这种写法：分析器对它报 `WITH before INSERT is not supported: the parser drops the CTE, so its sources cannot be resolved`。MariaDB 11.8.9 对同一条 SQL 报 `ERROR 1064` 语法错误（MariaDB 的 CTE 要写在 `INSERT ... WITH ... SELECT` 里），所以本仓库的硬失败与引擎一致，**不是缺陷**，仅记录。
3. `plan/postgresql_omni_parser_migration_plan.md` 的 **PG-FU-2** 已经提出"把 DML/集合运算/窗口保留的 walker 回馈上游 omni"，本次 review 的 P0-2/P0-3 恰好是支持该方向的额外证据——若上游能提供生产级 `analysis` 包，本仓库三份分析器（D1）可显著收缩。是否需要推进属产品决策，本次不推动。
