# Plan: MySQL 家族分析器单一真相源化 + 方言无关机制的继续下沉

> **Status: 已实施(见下方"实施结果"与附录 A)。** 本计划是 `plan/lineage_package_architecture_review.md` §4 第二期的实施展开,覆盖该报告的 T1(MySQL 家族三份字节级拷贝)与 T2(StarRocks/PG 移植变体的无人看守漂移)。
>
> **前一阶段的原始决策**(一式三份 + 人肉同步)与理由记录在 `plan/mysql_family_dialect_lineage_plan.md`,本计划不推翻它——omni 三个方言 AST 依然无法用 Go 无 IR 地参数化——而是把"同步"从人肉约定变成机器强制。

## TL;DR

| 改造点 | 现状 | 目标 |
| --- | --- | --- |
| MySQL 家族遍历正文 | `mysql`/`tidb`/`mariadb` 各持一份 2,365 行 `analyzer.go`,`TestDialectCopiesStayInSync` 只**检测**漂移,同步靠人肉拷贝 | `mysql/analyzer.go` 为唯一可编辑真相源;tidb/mariadb 的正文为 `go generate` 产物,漂移在**生成期**被消灭而非事后被检测 |
| StarRocks/PG 同名函数 | 66(mysql∩starrocks)/ 50(mysql∩pg)个同名函数,仅靠语料兜行为,无人看守实现分叉 | 把签名不含 AST 类型的函数审计后下沉 `algorithm`/`scope`,并把名单扩进 `shared_analysis_test.go` 的守卫清单 |
| 可编辑代码面 | MySQL 家族 7,094 行 | ~2,365(正文)+ 2×~90(方言头)+ ~250(生成器)≈ **2,900 行 ↓59%** |

**零行为变更约束**:本阶段是重构,不改任何一条血缘边的产出。验收以"逐字节等价 + 全语料绿"为准,不接受"行为看起来差不多"。

## 实施结果(已落地)

### Part 1:单一真相源化

| 项 | 结果 |
| --- | --- |
| 真相源 | `mysql/analyzer.go` 的 `// == MYSQL-FAMILY SHARED BODY ==` 哨兵之下是唯一可编辑正文 |
| 生成物 | `tidb/analyzer_body_gen.go`、`mariadb/analyzer_body_gen.go`(各 2,220 行),带标准 `Code generated ... DO NOT EDIT.` 头 |
| 方言头 | `tidb/dialect.go`(50 行)、`mariadb/dialect.go`(49 行):包文档、omni import、四枚常量、`init()` 注册、两个钩子 |
| 生成器 | `mysql/gen`:301 行核心(`Split`/`Render`/`Generate`/接缝自检)+ 62 行 IO 壳(`//go:generate go run ./gen`,支持 `-check`),另 141 行测试 |
| 守卫 | `TestGeneratedBodiesAreFresh`(重生成并逐字节比对,失败信息直接给出生成命令)、`TestGeneratedBodiesCopyTheSourceVerbatim`(证明生成物含正文的逐字节拷贝)、`TestSeamSelfCheck`、`TestSplitRejectsMalformedSource`;`mysql/copies_test.go` 与 `TestDialectCopiesStayInSync` 一并删除 |
| import 块 | 生成器按"固定映射表 + 正文选择符扫描"计算,只输出正文真正限定的包。`nodes`/`mysqlparser` 两个别名无法由 goimports 推导,因此不走通用工具 |

**可编辑代码面**:MySQL 家族 7,112 行 → **2,728 行**(mysql 正文 2,266 + 两个方言头 99 + 生成器 363),**↓62%**;含生成器测试为 2,869 行。计划里"~2,365 正文 + 2×~90 方言头 + ~250 生成器 ≈ 2,900"的估算由实测取代。

**零行为变更的机器证明**:生成前后三份正文逐字节相同(`diff` 无输出,mysql 对 tidb 与 mariadb 各一次);全语料用例数与结果不变。

**与计划草图的两处偏离**(实施中得出):

1. 生成器核心是 `mysql/gen` 包内的纯函数,而不是另建一个可导入包。`package main` 的 `_test.go` 能直接测试它们,新鲜度测试因此与生成器物理共用同一份渲染逻辑——为可测试性再造一个包没有必要。
2. 生成物的 import 块**不含** `storepb`:它只被方言头的 `init()` 用来注册引擎,正文用不到。整文件复制会把三份用不到的 import 带进生成物,这正是"引用扫描"要解决的问题。

### Part 2:方言无关机制的继续下沉

按"签名不含 `nodes.`/`pgast.` 类型"机械筛出 26 个候选(不含 `init`),逐个以正文哈希分类,完整处置见**附录 A**。本轮下沉 6 个机制,并登记进 `shared_analysis_test.go` 的双向清单:

| 下沉项 | 目标 | 共享范围 | 等价性依据 |
| --- | --- | --- | --- |
| `tempColumnNames` | `scope.TempColumnNames` | mysql / starrocks / pg(+ 生成的两个方言) | 三方正文逐字节相同 |
| `attachTempColumnLookup` | `scope.AttachTempColumnLookup` | 同上 | 同上 |
| `exposedColumnNames` | `scope.ExposedColumnNames` | mysql 家族 + pg | mysql == pg 逐字节相同(starrocks 无此函数,属行为差异,见附录 A) |
| `wildcardSourceRef` | `scope.WildcardSourceRef` | mysql 家族 + pg | mysql == pg 逐字节相同;starrocks 的分叉版改名 `queryLocalWildcardSourceRef` 留在本地 |
| `normalizeIdentifier` | `model.NormalizeIdentifier` | mysql 家族 + starrocks | mysql == starrocks 逐字节相同 |
| `resolveOutputColumns` | `algorithm.ResolveOutputColumns` | mysql 家族 + starrocks + pg | mysql == starrocks 逐字节相同;pg 仅差一层 `flattenTempSources` 包装,而该包装的函数体就是 `algorithm.FlattenTempSources`,包装随后删除 |

`shared_analysis_test.go` 从"只查 `algorithm` 是否仍声明共享机制"扩为**多包 + 双向**:`requiredSharedDeclarations` 变成 `包 → 名单`(algorithm / scope / model),`forbiddenDeclarations` 增加上述 6 个方言侧旧名——下一个想写第三份拷贝的人会被测试直接拦下,这是该守卫既有的工作方式。

**未下沉的分叉项**按"只下沉可证等价"的判据保留在方言本地并写进附录 A,其中值得下一轮决策的两条:`flattenTempSourceLineage`(mysql 7 参 vs starrocks/pg 8 参)与 `expandWildcardWithCatalog`/`processStar`(starrocks 的 `* EXCEPT` 支持是真方言特性,pg 另有差异)。

## 范围与非目标

**范围内**:`backend/plugin/lineage/{mysql,tidb,mariadb,starrocks,postgresql,algorithm,scope,testutil}`、一个新生成器包、`shared_analysis_test.go` 守卫清单、`AGENTS.md` 与相关 plan 文档的同步。

**非目标**:

- 不改变任何 analyzer 的输出(边的端点、变换、RelationType、错误分类一律不动);
- 不抽象 omni AST 适配层(那是第三期候选,成本高、收益不确定);
- 不动 OceanBase/Doris 的未注册决策、不碰 omni 依赖政策(`docs/omni_upstream_defects.md`);
- 不合并三个 parser——方言语法差异(MariaDB `FOR SYSTEM_TIME`、TiDB 无 VALUES primary)决定了一方言一 parser,见 `mysql_family_dialect_lineage_plan.md`。

## 现状量化(评审复核数据)

- `mysql/analyzer.go`、`tidb/analyzer.go`、`mariadb/analyzer.go` 均 ~2,365 行;两两 `diff` 48–49 行,全部集中在第 1–63 行的头部区(包文档、包名、两个 omni import、引擎注册、`valuesQueryPrimary`/`rowAliasNames` 两个方言钩子)。
- 第 64 行(`// Analyzer performs direct lineage analysis on MySQL queries.`)起正文逐字节相同,由 `mysql/copies_test.go` 的 `TestDialectCopiesStayInSync` 强制。
- mysql∩tidb 102/112、mysql∩mariadb 102/112 函数同名同体;mysql∩starrocks 66 个同名函数,其中抽查的 `wildcardSourceRef` 两个版本**语义已分叉**(mysql 版写 `Schema/Table/Resolved`,starrocks 版对 query-local 关系走别名分支)——证明 T2 的漂移不是假设,已经在发生。
- 仓库已有生成惯例:`backend/common/permission/permission.go` 的 `//go:generate go run ./gen` + `gen/main.go` 产出带 `// Code generated ...; DO NOT EDIT.` 头的 `*_gen.go`。`.golangci.yaml` 对标准生成标记文件 `generated: lax`(linter 与 formatter 两处),生成物自动获得 lint 宽免。

## Part 1:MySQL 家族单一真相源化

### 设计:身体生成,头部手写

把每个方言包分成两个文件:

```
mysql/
  analyzer.go          ← 唯一真相源:头部 + 正文(现状原样,仅加一个哨兵行)
tidb/
  dialect.go           ← 手写(~90 行):包文档、包名、常量、init 注册、两个方言钩子
  analyzer_body_gen.go ← 生成(~2,300 行,DO NOT EDIT):从 mysql 正文逐字节复制
mariadb/
  dialect.go           ← 同上
  analyzer_body_gen.go ← 同上
```

选择"生成正文、保留手写头部"而不是"生成整文件"的原因:

1. **变异点天然就是函数**:`valuesQueryPrimary`/`rowAliasNames` 已是函数级钩子(tidb 的 VALUES primary 返回 nil,mariadb 的 row alias 返回空)。正文按名字调用它们,生成物与手写文件同包,编译器自然把它们缝在一起——不需要模板参数替换。
2. **头部承载方言知识**:包文档里记录着"omni 的 TiDB AST 没有 ValuesSource 字段,见 docs/omni_upstream_defects.md"这类决策,应该手写、应该留在方言包里。
3. 生成器因此只做**纯机械动作**:读真相源 → 切出正文 → 换包名与两个 omni import 路径 → 输出。没有模板语言,没有字符串插值逻辑,出错面最小。

### 哨兵行

头部/正文边界由自然语言注释改为显式机器哨兵,放进 `mysql/analyzer.go` 第 64 行位置:

```go
// == MYSQL-FAMILY SHARED BODY: everything below is generated into tidb/ and
// mariadb/ as analyzer_body_gen.go. Do not edit their copies; edit here and run
// go generate ./backend/plugin/lineage/mysql. ==
```

哨兵之上的一切(常量 `resultTableName`/`deletionFieldName`/`wildcardColumn`/`fileSourceMarker`、`init()`、两个钩子、`Analyze`/`NewAnalyzer`……以实际切分为准)留在各方言自己的 `dialect.go`;哨兵之下逐字节复制。

注意:`Analyze`/`NewAnalyzer` 目前在哨兵线之下(mysql/analyzer.go:107/112),正文引用 `lineage.GetCatalogProvide()` 与 `catalog.Provide`,这些 import 由生成物的 import 块提供,见下。

### 生成器

镜像 `backend/common/permission/gen` 的形态:

```
backend/plugin/lineage/mysql/gen/main.go
```

`mysql/analyzer.go` 顶部加 `//go:generate go run ./gen`(与 permission 包同款)。生成器职责:

1. 读 `../mysql/analyzer.go`,按哨兵行切成头部/正文;找不到哨兵或哨兵不唯一即失败。
2. 对每个目标方言(tidb、mariadb;配置内嵌在生成器内,每个 ~15 行:包名、omni ast/parser 两个 import 路径):
   - 计算生成物的 import 块:**固定映射表 + 引用扫描**。即:生成器内置一张"本包可能引用的 import 全集"表(context、fmt、reflect、slices、strings、cmp、sync、errors、storepb、lineage、algorithm、catalog、model、scope、nodes→方言 omni ast、mysqlparser→方言 omni parser),用 `go/ast` 解析正文,收集实际出现的包级选择符前缀,只输出被引用的项。不用 `goimports`——它无法推导出 `nodes`/`mysqlparser` 这两个别名路径。
   - 输出 `// Code generated by go generate ./backend/plugin/lineage/mysql; DO NOT EDIT.` 头 + `package <dialect>` + import 块 + 正文逐字节。
   - 经 `go/format` 落盘为 `../<dialect>/analyzer_body_gen.go`。`go/format` 只排版,不重排字节级内容;生成物是否 gofmt-稳定由新鲜度测试兜底。
3. **方言头与生成物的接缝清单**由生成器自检:正文引用了哪些"应定义于 dialect.go"的标识符(常量四枚 + 钩子两个),生成器断言 tidb/mariadb 的 dialect.go 确实定义了它们(文本级检查即可,编译期是最终裁判)。这个自检让"新增变异点忘加钩子"在生成期爆炸,而不是编译期才报错。

### 测试与守卫改造

| 现状 | 改为 |
| --- | --- |
| `mysql/copies_test.go` `TestDialectCopiesStayInSync`(读三个文件按旧自然语言标记比对) | `TestGeneratedBodyFresh`:调用生成器的纯函数形态(见下),对 tidb/mariadb 重新生成并比对落盘文件,不一致时报 `run go generate ./backend/plugin/lineage/mysql` |
| tidb/mariadb 语料测试(`TestAnalyzeYAML`,共享语料 + `knownParserGaps`) | 原样不动——它们是行为证据,与代码组织无关 |
| 各方言 `registration_test.go` | 原样不动 |
| `shared_analysis_test.go` 方言清单 | 不动(mysql/tidb/mariadb 仍是三个包) |

生成器核心写成可测的纯函数(`Split(src)`/`Render(dialect, body)`),`main.go` 只是一层文件 IO 壳——新鲜度测试与生成器共用同一份渲染逻辑,避免"测试一套、生成一套"。

### 实施步骤(可按 commit 切分;以下为实际执行顺序,全部已完成)

1. **接缝前置统一**:把 tidb/mariadb 头部的 `resultTableName = "__result__"` 改为 `model.ResultTableName`(与 mysql 对齐),消除一个无谓差异。(第一期的模型卫生已顺带完成)
2. mysql/analyzer.go 加哨兵行 + `//go:generate` 指令,copies_test 相应改为按哨兵切分(语义不变,仍是字节一致性检测)——此时三个包仍是整文件形态,测试应绿。
3. 新增 `mysql/gen/main.go`(含 tidb/mariadb 配置、import 映射表、接缝自检),支持 `-check` 模式。
4. 拆分 tidb:手工从现有 `analyzer.go` 切出 `dialect.go`(头 63 行等价物),运行生成器产出 `analyzer_body_gen.go`,删除旧整文件。**验收点**:拆分后的 tidb 包 `go build` 通过、`TestAnalyzeYAML` 全绿、且 `analyzer_body_gen.go` 与 mysql 正文除包名/import 外逐字节相同(用 diff 验证,这就是"零行为变更"的机器证明)。
5. mariadb 同 4。
6. copies_test → 新鲜度测试(见上表)。
7. 文档同步:`AGENTS.md` 的 lineage 行加一句"tidb/mariadb 的 analyzer 正文是生成物,只改 mysql";`plan/mysql_family_dialect_lineage_plan.md` 决策一节追加本计划的链接与状态;评审报告 §4 引用本计划。
8. lint 复核:`golangci-lint run --allow-parallel-runners` 对生成物应自动走 `generated: lax` 宽免;若仍有个别 linter 命中,在 `.golangci.yaml` 的 exclusions 为该路径补规则(与 `proto/generated-go` 同手法)。

### 风险与对策

| 风险 | 对策 |
| --- | --- |
| 有人直接编辑 `analyzer_body_gen.go` | DO NOT EDIT 头 + 新鲜度测试(测试失败信息直接给出生成命令) |
| omni 升级使某方言 AST 获得/失去字段(如 mariadb 长出 ValuesSource) | 只需要改该方言手写的 `dialect.go` 钩子,不触正文;这正是头/身分离的红利 |
| 某日正文真需要按方言分叉(超出钩子能覆盖的差异) | **毕业条款**:该方言退出家族,恢复自持整文件(StarRocks 现状即此形态)。生成器配置删掉它即可,无历史包袱 |
| 生成器自身漂移(渲染逻辑与新鲜度检测逻辑分叉) | 二者共用同一纯函数实现,物理上不可能分叉 |

## Part 2:方言无关机制的继续下沉(StarRocks/PG)

### 审计方法(机械、可复核)

对 66(mysql∩starrocks)与 50(mysql∩pg)个同名函数逐一分类:**签名(参数/返回值)不含 `nodes.`/`pgast.` 类型的函数**才是候选——含 AST 类型的天然属于方言适配层,不动。抽查结论先行:

- **确认可下沉**:`resolveOutputColumns`(只依赖 `scope`/`algorithm`,mysql 与 starrocks 版本待逐行 diff 确认等价)。
- **确认已漂移,先对齐再谈下沉**:`wildcardSourceRef`(mysql 写 `Schema/Table/Resolved`;starrocks 对 query-local 关系走别名分支)。这类函数要先用语料用例定胜负——补一条能区分两种语义的 corpus 用例,确认哪个正确(或都是上下文正确的特化),再决定下沉形态;**不允许"顺手统一"**,那是行为变更。
- 其余候选按同法过一遍,产出一张"函数 × 方言 × 处置(下沉/保留/先对齐)"清单,作为本 part 的附件落地。

### 下沉与守卫

- 下沉目标包与现有一致:作用于边的进 `algorithm`,作用于引用的进 `scope`。
- 每下沉一个,在 `shared_analysis_test.go` 做双向登记:`requiredSharedDeclarations` 加共享侧名字,`forbiddenDeclarations` 加方言侧旧名字——让"下一个想写第三份拷贝的人"被测试拦下,这是该守卫既有的工作方式,直接复用。
- StarRocks 的语句遍历骨架(`processSelect*`/`processCTE`/`processInsert*` 等)**不在本阶段动**——它们 AST 绑定且与 mysql 结构差异大(集合运算是独立节点、select list 是 `[]*SelectItem`),强行合并是第三期"适配层"的事,现在做只会得到一个满是 type switch 的坏抽象。

## 验收标准(全部机器可判)

1. ✅ `go build ./...`、`go test ./...` 全绿,语料用例数不变(MySQL 家族共享 225 + MariaDB 方言若干 + TiDB 方言若干 + PG 252 + StarRocks 185,外加各自 skip 表原样)。
2. ✅ `tidb/analyzer_body_gen.go`、`mariadb/analyzer_body_gen.go` 与 mysql 正文逐字节等价(除包名/import);手改生成物后 `TestGeneratedBodiesAreFresh` 红(已实测)。
3. ✅ `golangci-lint run --allow-parallel-runners` 0 issues;生成物享受且仅享受 `generated: lax` 宽免,`.golangci.yaml` 无需新增规则。
4. ✅ 可编辑代码面:MySQL 家族由 7,112 行降到 **2,728 行**(生成器测试另 141 行;见"实施结果")。
5. ✅ Part 2 的"函数 × 方言 × 处置"清单见附录 A,6 个下沉项全部登记进 `shared_analysis_test.go`。
6. ⚠️ `make test-integration` 未执行:本机没有 Docker/PostgreSQL/MySQL,集成套件无法起。已用 `go vet -tags integration ./backend/test/integration/...` 验证集成测试**编译**通过,且本阶段不改任何产出的边,预期无需改动集成测试——这一条需要在有容器环境的机器上补跑。

## 回滚

两部分相互独立,可分别回滚:Part 1 回滚 = `git revert` 到整文件形态(生成物与生成器一并删除即可,真相源 mysql/analyzer.go 始终是完整可读的单文件,不存在"只有生成物没有源"的状态);Part 2 回滚 = 逐函数 revert,语料与守卫清单会指出每处的归属。

## 工作量预估

| 项 | 预估 |
| --- | --- |
| Part 1 生成器 + 拆分 + 测试改造 | 1–2 天(大头是 import 映射表与接缝自检的打磨) |
| Part 2 审计 + 逐个下沉 | 2–3 天(取决于"先对齐"类函数的语料补充量) |

## 与后续阶段的关系

- Part 1 完成后,MySQL 家族的"修复只落一个方言"风险被结构性消除;第三期若要进一步合并(单包多方言注册 / IR 层),本阶段的头/身切分是前置条件。
- catalog 降级进 diagnostics、方言契约对齐等**第一期**事项(评审报告 §4)可与本计划并行,互不依赖。

## 附录 A:Part 2 审计清单(函数 × 方言 × 处置)

**方法(机械、可复核)**:用 `go/parser` 取出每个包非测试文件里的全部函数与方法,正文归一化接收者名后取哈希;凡**签名(参数/返回值/接收者)不含 `nodes.` 或 `pgast.` 类型**即为候选。MySQL 家族三方因头/身分离而天然同源,故按"mysql ↔ starrocks ↔ postgresql"三方比对,mariadb/tidb 视同 mysql。筛出候选 26 个(不含 `init`)。

| 函数 | mysql | starrocks | pg | 正文一致性 | 处置 |
| --- | --- | --- | --- | --- | --- |
| `tempColumnNames` | ✓ | ✓ | ✓ | 三方逐字节相同 | **下沉 `scope.TempColumnNames`** |
| `attachTempColumnLookup` | ✓ | ✓ | ✓ | 三方逐字节相同 | **下沉 `scope.AttachTempColumnLookup`** |
| `exposedColumnNames` | ✓ | — | ✓ | mysql == pg | **下沉 `scope.ExposedColumnNames`**(starrocks 无此函数,见下"行为差异") |
| `wildcardSourceRef` | ✓ | ✓ | ✓ | mysql == pg;starrocks 分叉 | **下沉公共版 `scope.WildcardSourceRef`**;starrocks 保留本地 `queryLocalWildcardSourceRef` |
| `normalizeIdentifier` | ✓ | ✓ | — | mysql == starrocks | **下沉 `model.NormalizeIdentifier`**(pg 用的是另一个函数,见下) |
| `resolveOutputColumns` | ✓ | ✓ | ✓ | mysql == starrocks;pg 差一层包装 | **下沉 `algorithm.ResolveOutputColumns`**,pg 的 `flattenTempSources` 包装删除 |
| `addRelation` | ✓ | ✓ | ✓ | 三方逐字节相同 | 保留:`EdgeSet.Add` 的 3 行回调适配(`Emitter.AddEdge` 需要方法值) |
| `currentScope` | ✓ | ✓ | ✓ | 三方逐字节相同 | 保留:读取接收者的 `scopeStack`,4 行;下沉需改 30+ 调用点 |
| `pushScope` / `popScope` | ✓ | ✓ | ✓ | 语义等价(仅局部变量命名不同) | 保留:同上,接收者状态胶水 |
| `attachColumnLookup` | ✓ | ✓ | ✓ | 语义等价(仅 nil 分支写法不同) | 保留:绑定 `a.catalog` 与 `a.catalogTable` |
| `catalogTable` | ✓ | ✓ | 分叉 | pg 填 `Schema`,MySQL 族填 `Database` | 保留:标识符层级是方言事实,合并即行为变更 |
| `normalizeExpressionText` | — | ✓ | ✓ | sr == pg | 保留:一行 `strings.Join(strings.Fields(...))`,下沉无收益 |
| `generateEdgesForDataModification` | ✓ | — | 分叉 | mysql == mariadb | 保留:pg 版多一个 `*scope.Scope` 参数 |
| `traceThroughTableLineage` / `…ToTarget` | 分叉 | 分叉 | 分叉 | 分叉 | 保留:`algorithm` 两个函数的回调适配层 |
| `emitPredicateInfluences` | 分叉 | 分叉 | 分叉 | 分叉 | 保留:`Influences.Emit` 的回调装配 |
| `generateEdges` | 分叉 | 分叉 | 分叉 | 分叉 | 保留:绑定 `a.realTarget`/`a.inSetOpArm`/`a.emitSources` |
| `flattenTempSourceLineage` | 分叉(7 参) | 分叉(8 参) | 分叉(8 参) | 分叉 | 保留(待先对齐):starrocks/pg 多一个 output alias 参数 |
| `processStar` / `expandWildcardWithCatalog` | 分叉 | 分叉 | 分叉 | 分叉 | 保留:starrocks 的 `except []string` 是 `SELECT * EXCEPT` 支持,真方言特性;pg 另有差异 |
| `Analyze` / `NewAnalyzer` / `AnalyzeRelations` / `splitStatements` | 各自 | 各自 | 各自 | 分叉 | 保留:入口与方言注册,本就该各方言自持 |

### 未下沉项的"先对齐"发现(供下一轮决策)

1. **`wildcardSourceRef` 的分叉是真实的**。starrocks 对 CTE/派生表返回**未标记 `Resolved`** 的别名引用(`{Table: alias 或 table, Column: "*"}`),mysql/pg 返回**标记 `Resolved`** 的真实表名引用。用临时探测程序跑 `SELECT * FROM (SELECT a, b FROM t1) x`、两个派生表 JOIN、`WITH c AS (...) SELECT * FROM c` 三种形态,四种方言产出的边完全一致——即该分叉在常见上下文中不可观测。但无法证明它在**所有**上下文等价,按"只下沉可证等价"的判据保留 starrocks 本地实现,并改名 `queryLocalWildcardSourceRef` 让分叉在调用点可见。
2. **`expandWildcardWithCatalog` / `processStar` 在 starrocks 上的差异不是漂移**:多出的 `except []string` 支撑 `SELECT * EXCEPT (a, b)`。pg 的版本另有差异(尚未定位到具体规则),本轮不合并。
3. **`catalogTable` 的差异不是漂移**:pg 用 `model.ObjectIdentifier{Schema: …}`,MySQL 族用 `{Database: …}`——目录里 schema/database 的层级不同,统一会让一方的目录查找全部落空。
4. **`exposedColumnNames` 在 starrocks 缺席是行为差异**:starrocks 的 CTE 列重命名走内联的位置映射,其注释自述"MySQL 分析器忽略该列表且完全不出边,映射它严格更正确"。这属于应当保留的方言改进,不属本阶段的对齐范围。
5. **`normalizeIdentifier` 与 pg 的 `normalizeExpressionText` 不是同一个函数**:前者去引号,后者折叠空白。名字相近但用途不同,不构成重复。
6. **`attachColumnLookup` / `pushScope` / `popScope` 三方语义相同、只是写法不同**。它们没被下沉纯粹是因为绑定接收者状态(`a.catalog`、`a.scopeStack`),而不是因为语义有分歧;若第三期给 `Analyzer` 提出共享的基状态类型,这三项可以立即跟进。

## 附录 B:复核命令

```bash
# 正文新鲜度(失败即给出生成命令;生成物被手改时红)
go test ./backend/plugin/lineage/mysql/gen/ -run TestGeneratedBodies

# 只检查、不落盘
cd backend/plugin/lineage/mysql && go run ./gen -check

# 生成物正文与真相源正文逐字节比对(应无输出)
diff <(awk '/^\/\/ Analyzer performs direct lineage analysis on MySQL queries\.$/{f=1} f' \
        backend/plugin/lineage/mysql/analyzer.go) \
     <(awk '/^\/\/ Analyzer performs direct lineage analysis on MySQL queries\.$/{f=1} f' \
        backend/plugin/lineage/tidb/analyzer_body_gen.go)

# 共享机制守门(多包 + 双向)
go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared
```
