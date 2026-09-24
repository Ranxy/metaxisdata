# 数据血缘分析包(`backend/plugin/lineage`)架构评审报告

> **文档性质:架构审计报告 + 演进建议,不是实现计划。**
> 审查对象:`backend/plugin/lineage/` 全部子包(非测试代码 ~11,800 行,测试代码 ~3,500 行 + YAML 语料 ~17,300 行),及其直接调用方 `backend/runner/lineageanalyzer/analyzer.go` 与 `backend/api/v1/lineage_service_analyze.go`。
> 方法:全部核心文件精读 + 三方代码 diff 量化 + git 历史演进分析 + 与既有文档(`plan/postgresql_lineage_package_review.md`、`plan/mysql_family_dialect_lineage_plan.md` 等)的对照核实。
> 结论:**工程质量高于一般水平(机制设计 A-),但背负全仓库最重的一块结构性技术债(代码组织 C+),综合 B+。**

---

## 0. 第一期实施状态:已落地

§4 第一期的三项已全部落地并通过验证。§3/§4 保留原始发现(作为问题记录与验收标准),此处只记结果。

| 项 | 状态 | 落地内容 |
| --- | --- | --- |
| **T3-1 多语句策略** | ✅ 已统一 | 方言注册改为 `RegisterAnalyzeRelation(engine, analyzer, splitter)`;切分上移到根包 `lineage.GetAnalyzeRelation`,逐语句送入单语句分析器,边用 `algorithm.EdgeSet` 合并。PostgreSQL 分析器改为拒绝多语句,原"共享分析器状态遍历脚本"的循环、`resetStatement` 与随之无用的 `Influences.Reset` 一并删除;MySQL 家族的脚本从"整体硬失败(并触发清血缘)"变为逐语句分析,与 PG 行为一致 |
| **T3-2 未建模语句** | ✅ 已统一 | StarRocks 的 `MERGE` 与未建模 `query expression` 由硬失败改为 `diagnostics.NotModelled`(部分分析),`unsupported()` 删除;MySQL 家族 leading `WITH`-before-DML(omni 解析后丢弃 CTE)由硬失败改为"跳过该语句 + 记缺口"。**清血缘现在只由解析失败触发**,根包新增契约测试固定该边界 |
| **T3-3 catalog 缓存** | ✅ 已统一 | 新增 `catalog.Cache` 作分析器级缓存:PG/StarRocks 由"每个引用查一次库"改为"每个标识符查一次",MySQL 家族的私有 `tableCache` 与三处 `GetTable` 调用点一并收敛到共享 `catalogTable` 助手 |
| **T4 catalog 降级** | ✅ 已可见 | 新增 `algorithm.Diagnostics.CatalogUnavailable`;三个方言的 `catalogTable` 在 `GetTable` 报错时记录缺口并继续(通配符回退),不再 `if err != nil { meta = nil }` 静默吞错。runner 侧注释同步修正:降级以缺口形式落在 version 行,而不是伪装成完整分析 |
| **T7 模型卫生** | ✅ 已修正(部分) | `ColumnRelation.IsTemp` 注释改为真实语义(等价于 `target == model.ResultTableName`);PostgreSQL 与 TiDB/MariaDB 改用 `model.ResultTableName`(API 层 `tempResultTable` 同步)。`__deletion__`/`__file__` 的单点定义仍留待第三期 |
| **T8 测试基建** | ✅ 已补齐(部分) | `catalog/memory_test_provide.go` 移入 `testutil`(`go list -deps ./backend/bin/server` 已确认不再进生产二进制),并按需增加 `Calls()` 计数与失败 provider;MySQL/StarRocks 补 `TestAnalyzeAllocationBudget`(逐形状预算 + 全语料预算)。`shared_analysis_test.go` 的相对路径前提未动 |

**新增/修改的测试**

- 新增根包 `lineage_test.go`:多语句逐条分析(5 个引擎)、语句间 scope/谓词/CTE 不泄漏、未建模语句只记缺口且保留兄弟语句的边、解析失败整体失败
- 新增 `catalog/cache_test.go`(同标识符只读一次、miss 与失败同样只读一次、nil 语义)
- 新增 `mysql`/`postgresql`/`starrocks` 的 `catalog_lookup_test.go`(同一关系只查一次 + 查询失败记为缺口)
- `postgresql/testdata/analyze/29_test_statement_lineage_table.yaml` 的两条多语句用例移入根包契约测试;`mysql/testdata/analyze/17_test_regression_lineage_table.yaml` 的三条 leading-WITH 用例改为断言缺口消息与零边
- StarRocks `TestUnimplementedStatementsFailLoudly` 改为 `TestUnmodelledStatementsAreGaps`;PG `hardfail_test.go` 的 `TestMultiStatementAnalyzed`/`TestUnsupportedStatementKeepsOtherStatements`/`TestParseErrorFailsWholeInput` 分别由 `TestRejectsMultiStatement` 与根包测试承接
- 集成测试 `TestMySQLManualSQLUnsupportedStatementRealServerIntegration` 断言 version 行携带 `not modelled: WITH before INSERT`

**有意引入的行为变化**

1. MySQL 家族接受多语句脚本并逐条分析,`MANUAL_SQL` 不再因其中一条语句而清空整份血缘(此前"清血缘还是存缺口取决于引擎")。
2. leading `WITH`-before-DML 由 `markAnalysisFailed`(清血缘 + "lineage analysis failed")改为部分分析(存缺口 + "analysis errors: not modelled: ..."),版本行仍然携带当前 hash,因此不会被小时扫描反复重排。
3. PostgreSQL 分析器拒绝多语句输入;多语句能力由根包统一提供,且不再跨语句共享 scope(此前 `SELECT t1.a FROM t1; SELECT t2.b FROM t2` 的注解说共享 root scope,代码实际已按语句 reset,现已由结构保证)。
4. catalog 查询失败不再静默降级:`runner` 存下边并把缺口写进 version 行,`AnalyzeSQL` 则按既有策略把"带缺口的分析"映射为请求错误(`InvalidArgument` + 消息,关系被丢弃)——这条映射早于本期(PG MERGE 等缺口一直如此),把缺口渲染为响应 `warnings` 而不是错误,需要第三期的结构化 diagnostics;`ExplainSQL` 路径不受影响(它记录错误后继续使用已得到的边)。

**仍待处理**:T1/T2(第二期,见 `plan/lineage_mysql_family_generation_plan.md`)、T5 的 omni 依赖策略与 T6 的全局可变状态、T7 的哨兵单点定义(第三期)。

---

## 1. 总体结论

这个包的工程质量明显高于一般项目:模型层干净、语料测试严格(662 个 golden 用例、精确边匹配)、错误分类学经过设计(hard-fail vs partial vs skip)、且有自建"架构守门测试"。历史上最危险的一批正确性缺陷(名字遮蔽丢血缘、谓词泄漏、集合运算变换伪造)已通过"下沉共享算法"的架构重构修复(见 `4dbf876 refactor(lineage): sink the dialect-neutral lineage algorithm`)。

但它现在背负全仓库最重的一块结构性技术债:**三个 2,365 行、~98% 逐字节相同的方言分析器**(`mysql`/`tidb`/`mariadb`),再加一个 2,194 行的"移植版"(starrocks)。当前的守门机制(`TestDialectCopiesStayInSync`、共享语料)只是**把债务冻结住,而不是偿还它**——每一次 MySQL 家族的修复仍然要"改一份、重新生成三份",而 StarRocks 上 66 个同名函数已经处于无人强制约束的漂移状态。此外存在若干方言间契约不一致和一处 runner 与分析器之间的隐含假设裂缝。

---

## 2. 架构现状

### 2.1 分层与依赖

```
model      (叶子: ColumnRelation / Transformation / ObjectIdentifier)
  ↑
scope      (词法作用域 + 名称解析,CTE/派生表以 Lineage 边回溯)
  ↑
algorithm  (方言无关机制: EdgeSet 去重 / Influences 谓词归属 /
           setop 合并 / temp 展开 / Diagnostics 诊断)
  ↑
catalog    (Provide 接口,单方法 GetTable; AnalysisContext 经 ctx 传默认限定符)
  ↑
lineage    (根包: 注册表 getAnalyzes + 全局 CatalogProvide + UnsupportedStatementError)
  ↑
mysql · tidb · mariadb · postgresql · starrocks  (init() 自注册,各有独立 AST 遍历)
  ↑
runner/lineageanalyzer (周期扫描 + 退避重试) / api/v1 LineageService(无状态 AnalyzeSQL)
```

分层方向正确、无环;`algorithm` 不依赖 `store`,可纯函数测试——这是好设计。`algorithm/edge.go` 的包注释本身就是一份事故复盘,说明了为什么抽这层("同一机制写三份会各自漂移"),说明团队对问题成因有清醒认识。

### 2.2 代码量分布

| 包 | 非测试代码 | 测试代码 | YAML 语料 |
| --- | --- | --- | --- |
| 根包 lineage | 81 | 117 | — |
| scope | 597 | 618 | — |
| model | 392 | 129 | — |
| algorithm | 581 | 341 | — |
| catalog | 169 | 101 | — |
| testutil | 1,001 | 333 | — |
| mysql | 2,365 | 193 | 6,099(24 文件 / 225 用例) |
| tidb | 2,365 | 118 | 221 |
| mariadb | 2,364 | 160 | 275 |
| postgresql | 2,746 | 546 | 6,483(40 文件 / 252 用例) |
| starrocks | 2,194 | 370 | 4,196(20 文件 / 185 用例) |

语料合计 662 个用例;TiDB/MariaDB 除各自方言文件外复用 MySQL 的 225 例共享语料(MariaDB 有 4 例 parser gap 跳过,TiDB 有 1 例)。

### 2.3 关键机制评估

| 机制 | 评估 |
| --- | --- |
| **EdgeSet 去重**(端点结构体 + 逐字段变换比较) | 正确,且注释解释了"为何不能用字符串拼接当 key" |
| **Influences 谓词归属**(按 scope 收集、消费方继承) | 设计严密,修正了未引用 CTE 的谓词泄漏这类深层问题 |
| **Scope 解析器**(未限定列走字典序 + catalog 佐证) | 有意偏离 SQL 真实解析规则以保确定性——**正确性依赖 catalog 新鲜度**:schema 未及时同步时血缘会静默猜错 |
| **错误分类**(`UnsupportedStatementError` 部分分析 / 硬失败 / 引擎未注册三档) | 语义清晰,runner 对三档分别有"存边+记缺口 / 清边+记失败 / 记跳过"的配套动作 |
| **Golden 语料**(YAML、KnownFields 拒拼写、`RequireFullEdgeAnnotations` 强制全字段断言) | 全仓库最强的测试资产 |
| **架构守门测试**(`shared_analysis_test.go`、`mysql/copies_test.go`) | 用 AST 解析禁止方言包重新声明共享机制、强制三份拷贝字节一致——少见但有效 |
| **性能预算**(PG 的 `TestAnalyzeAllocationBudget`,分配次数而非墙钟) | 思路正确;**但只有 PG 有** |

---

## 3. 技术债务清单(按严重性排序)

### T1【最重】MySQL 家族三份字节级拷贝:7,094 行中 ~4,700 行是重复

量化事实:`mysql/analyzer.go`、`tidb/analyzer.go`、`mariadb/analyzer.go` 三者均 ~2,365 行;两两 `diff` 只有 **48–49 行**(包名、两行 omni import、注册引擎、`valuesQueryPrimary`/`rowAliasNames` 两个方言钩子)。从第 64 行的 `// Analyzer performs...` 标记行起,**正文被 `TestDialectCopiesStayInSync` 强制要求逐字节相同**;102/112 个函数完全同名同体。

该决策在 `plan/mysql_family_dialect_lineage_plan.md` 中是有意为之(理由:omni 三个方言 AST 无法在无 IR/适配层的情况下参数化,Go 表达不了)。理由成立,但成本被低估:

1. **同步靠人肉**:"改 mysql → 重新生成 tidb/mariadb"是无工具化的约定——仓库里没有生成脚本,只有一个事后检测的测试。
2. **守门测试防止漂移,但也锁死了形态**:`TestDialectCopiesStayInSync` 让任何"只改一个方言"的局部优化变得不可能——要么改全体,要么破坏测试。这是把债务制度化,而不是消除它。
3. **编译与测试的三倍开销**:三份 82KB 源码进同一份二进制,全量测试把相同的 225 例语料跑三遍。

### T2 StarRocks 是"移植"而非拷贝——无人看守的第四种变体

`starrocks/analyzer.go` 头注释自称 "a port rather than a dialect copy"。它与 mysql **共享 66 个同名函数**(占 mysql 函数数的 59%),PG 也有 50 个。这些函数**不受** `TestDialectCopiesStayInSync` 约束,只靠共享语料兜底行为漂移,而语料只能发现"行为差异",发现不了"机制实现逐渐分叉"。git 历史已经演示过后果:`4e50f61 fix(lineage): fix the defects the sibling dialects kept after PostgreSQL`——PG 修好的缺陷在兄弟方言里继续存在。`algorithm` 包把"机制"收编了,但**语句遍历骨架**(CTE/INSERT/UPDATE/DELETE 的处理流程)仍然一式四份。

### T3 方言间契约不一致——`analyze(ctx, sql)` 同签名不同语义

`lineage.go:60` 把所有方言抽象成 `func(ctx, sql) ([]ColumnRelation, error)`,但实际契约各不相同:

| 维度 | MySQL/TiDB/MariaDB | StarRocks | PostgreSQL |
| --- | --- | --- | --- |
| 多语句输入 | **硬拒绝**(`expected exactly 1 statement`) | 硬拒绝 | **逐语句分析**(语句间共享分析器、ctx 可取消) |
| 同类缺口 MERGE | —(无此语法) | **硬失败**(`a.errors`,丢弃全部边) | **部分分析**(`diagnostics.NotModelled`,保留边) |
| catalog 缓存 | 分析器级 `tableCache` map | 每 TableRef 闭包 | 每 TableRef 闭包 |

后果是具体的:

- **MANUAL_SQL 多语句脚本在 MySQL 家族上整体分析失败,在 PG 上正常**。runner(`analyzer.go:428`)把 manual SQL 原文直传,不做切分。同一产品功能,引擎不同则行为不同;且失败路径触发 `markAnalysisFailed`,**清空该对象已有血缘**——一次编辑失误就能抹掉历史结果。
- 同名缺口(MERGE)落在不同错误桶里,导致 runner 一侧"清血缘"与"存血缘+记缺口"两种后果随机取决于方言。这不是领域差异,是实现漂移。
- 缓存策略不一致:PG/StarRocks 对 `FROM t a JOIN t b` 会查两次库,MySQL 只查一次。

### T4 runner 的"纯函数、失败确定性"假设与分析器实际行为之间有裂缝

`runner/lineageanalyzer/analyzer.go:292-300` 的注释明确写道:"analyzers are pure functions of (engine, statement)…fails identically on every retry",并据此把失败直接置为已分析(不再重试)。但实际上:

- 三个方言的 catalog 查找都**吞掉错误**(`if err != nil { meta = nil }`,见 `mysql/analyzer.go:2336-2343`)。一次瞬时 DB 抖动会把结果静默降级为"未知列"→ 通配符回退边,**且不产生任何 diagnostics 记录**。
- 于是分析结果事实上是 `(engine, statement, catalog 状态, catalog 错误)` 的函数;runner 的确定性推理不成立,而降级结果还被打上当前 hash 记为"已分析",schema 未变就不会再算。

修复成本低(catalog 错误转成一条 diagnostics 记录,或至少让降级可见),但现状是**静默不准**。

### T5 对 `github.com/bytebase/omni` 的单点依赖 + 自我束缚的规避策略

`go.mod` 把全部五个解析器钉在 omni 的一个伪版本上(`v0.0.0-20260912023254-4574e69bb9f1`)。项目同时给自己立了规矩(`docs/omni_upstream_defects.md`):**不向上游反馈、不 patch、不 fork、不 vendor**。这个组合把上游缺陷的成本全部转嫁给分析器层,规避代码正在累积:

- `mysql/analyzer.go:245` `nodeLoc` 用**反射**读每个 AST 节点的 `Loc` 字段(靠 `sync.Map` 缓存字段下标兜底性能);
- `mysql/analyzer.go:207` `hasLeadingWith` 手工扫注释和前缀,只为识别"omni 会丢掉的 WITH 子句";
- `starrocks/viewbody.go` 在解析失败后**用 tokenizer 手工切割** `CREATE VIEW ... AS` 再二次解析 body;
- `starrocks/rawsql.go` 维护一整套"源码/token 栈",因为 omni 把派生表/CTAS/表达式子查询只暴露为原始文本,需要重新解析并自行对齐偏移;
- MariaDB 有 4 个生产会撞到的解析缺口(mysqldump 视图的三层括号 join)只能登记在 `knownParserGaps` 里等上游。

每一条单独看都合理(注释也写得极好),但它们共同构成一个**脆弱适配层**:omni 任何一次 AST/Loc 语义微调,反射和字符串手术的破坏面是编译器抓不到的,只能靠语料事后发现。**依赖策略与不修改策略至少需要松动一个**;若都维持,应给 omni pinning 配一套升级验收流程(全语料 + 输出 diff)。

### T6 根包的全局可变状态

`lineage.go:41-58` 用包级 `CatalogProvide` + `init()` 自注册。在单 server 二进制下能用,但:

- `Analyze()` 这个导出入口隐藏了对全局 catalog 的依赖(测试里都走 `NewAnalyzer` 显式注入,说明作者也知道哪个更好);
- 根包既是注册表、又是错误类型的家、又是全局状态持有者;未来要做"多 store/多目录实例"就得动根包;
- panic-on-double-register 在 init 阶段可接受,但注册表本质上是"编译期可知的固定集合",用 `map + RWMutex` 的运行时注册是为插件化付出的不需要的复杂度。

建议方向:`Analyze(ctx, engine, sql, catalog)` 显式传参,根包只留错误类型与注册表;全局 getter 保留为薄包装。

### T7 模型层的细节瑕疵

- **RelationType 双真值**:`model.ColumnRelation.RelationType` 是存储字段,但 `RelationTypeOf(transform)` 可从变换推导(`buildLineageEdge` 自动回填),openlineage 处理器另行推导。字段值与推导值可能不一致而没有校验。
- **`IsTemp` 注释漂移**:`model/relation.go:15` 仍写 "if the target table is not a real table",而第一批修复后的真实语义是 `target == __result__`(PG review §0 P0-1 已确认)。
- **`StrToObjectIdentifier`**(`model/identifier.go:35`):按 `.` 切分 1–4 段,quoted 标识符内含点(`"a.b".c`)会解析错;目前只被 explain_sql_service 使用,但它是公开形态,迟早被误用到 SQL 派生名上。
- **字符串哨兵协议**:`WildcardColumn = "*"` / `__result__` / `__deletion__` / `__file__` 横跨 analyzer→runner→store→openlineage→前端,没有单点定义。mysql 家族已常量化 `model.ResultTableName`,PG 却自定义 `"__result__"`(`postgresql/analyzer.go:40`)。

### T8 测试基建残留

- `catalog/memory_test_provide.go` 是普通源文件(靠文件名里的 `_test_` 暗示而非 `_test.go` 约束),会被编进生产二进制;
- 分配预算测试只有 PG 有,mysql/starrocks 只有 benchmark 没有预算——PG review 的 D5 只修了一半;
- 根包 `shared_analysis_test.go` 用相对路径读兄弟目录源码作为 lint——可用,但依赖"在包目录下运行 `go test`"的隐含前提。

---

## 4. 建议的演进路径

### 第一期(低风险,直接还息)

1. **方言契约对齐**:统一多语句策略(建议 runner/api 层切分语句后逐条送入,方言保持单语句);统一"未建模语句"归入 diagnostics 桶(部分分析),让"清血缘"只属于解析失败;统一 catalog 缓存为分析器级 map。
2. catalog 错误/降级进入 diagnostics——堵住 T4 的静默不准。
3. 模型层卫生:修正 `IsTemp` 注释;PG 换用 `model.ResultTableName`;`memory_test_provide.go` 改名 `_test.go` 或移到 `testutil`;给 mysql/starrocks 补分配预算。

### 第二期(T1/T2,真正的债务)

> **实施计划已展开为独立文档:`plan/lineage_mysql_family_generation_plan.md`。**

给 MySQL 家族三份拷贝做减法。诚实的评估:**完整的 AST 适配接口层(~100 个访问器)成本高**,不建议一步到位。务实路径:

- 首选:把 `mysql/analyzer.go` 变成**单一真相源 + `go generate` 生成另两份**(已有的 `valuesQueryPrimary`/`rowAliasNames` 钩子正好就是变异点,提成头部模板参数即可)。源码树里只剩一份可编辑文件,copies 测试改成校验"生成物新鲜度"。这一步消灭"人肉同步",且不需要抽象 omni AST。
- StarRocks 次之:把 66 个同名函数中与 AST 无关的部分(语句骨架状态机、emit/edge 流程)继续下沉 `algorithm`,方言侧只留 AST 遍历。

### 第三期(结构性)

- omni 依赖策略重新决策:fork/vendor/上游反馈至少放开一个,或建立带验收门槛的升级流程;
- 注册表改编译期装配(server 显式传入 engine→analyzer map),去掉全局单例,让 `lineage` 根包回归纯类型/接口定义;
- `UnsupportedStatementError` 结构化(分类枚举 + 引用列表),API 层才能把"缺口"渲染给前端而不是塞字符串。

---

## 5. 不该动的地方

评审中应尊重已有的深思熟虑决策:硬失败解析策略(部分结果会伪装成"没有血缘")、OceanBase/Doris 不注册(`ErrorEngineNotSupported` 是显式 skip 而非缺陷)、黄金语料的严格匹配哲学、用分配次数而非墙钟做性能门槛、`dupl` 拦不住跨包重复的实证记录。这些都已在代码注释和 plan 文档中固化为决策,不构成债务。

---

## 附录:复核入口

- 三方重复度:`diff backend/plugin/lineage/{mysql,mariadb}/analyzer.go | wc -l` → 49;`{mysql,tidb}` → 48
- 字节级正文同步守卫:`backend/plugin/lineage/mysql/copies_test.go`(`TestDialectCopiesStayInSync`)
- 跨方言同名函数统计:mysql∩tidb=102/112,mysql∩mariadb=102/112,mysql∩starrocks=66/112,mysql∩postgresql=50/112,pg∩starrocks=42
- 共享机制守门:`backend/plugin/lineage/shared_analysis_test.go`(`TestSharedAnalysisStaysShared`)
- 既有相关文档:`plan/postgresql_lineage_package_review.md`(P0/P1 批次已全部落地,D1/D3 即本文 T1/哨兵模型的前身)、`plan/mysql_family_dialect_lineage_plan.md`(拷贝决策的原始理由)、`docs/omni_upstream_defects.md`(上游缺陷登记与不修改政策)、`plan/lineage_ast_field_coverage_audit.md`
