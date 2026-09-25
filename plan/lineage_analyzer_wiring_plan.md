# Plan:血缘分析器的编译期装配 + 结构化 diagnostics + 哨兵单点定义

> **Status: 已实施(见下方"实施结果")。** 本计划是 `plan/lineage_package_architecture_review.md` §4 第三期的实施展开,覆盖该报告的 **T6**(根包全局可变状态)、**T3/T4 的最后一段契约不一致**(“部分分析”在 API 层仍被映射为错误并丢边)与 **T7 的哨兵单点定义**。
>
> **非目标:omni 依赖策略(T5)。** 本期不 fork、不 vendor、不改 `docs/omni_upstream_defects.md` 的"不向上游反馈、不 patch"政策,也不建立带验收门槛的升级流程——那是一条需要单独决策的线,与本期的三项改造互不阻塞。

## TL;DR

| 改造点 | 改造前 | 目标 |
| --- | --- | --- |
| 引擎注册 | 五个方言各自 `init()` 调 `lineage.RegisterAnalyzeRelation`,写进根包的包级 `map` + `RWMutex`;catalog 也是根包包级变量(`InitCatalogProvide`) | 方言各自导出 `Registration()`,新包 `engines.Registrations()` 在编译期列全;`lineage.NewAnalyzer(catalog, regs...)` 构造一个**值**,server 把它显式交给 runner、`LineageService`、`ExplainSQLService` |
| 缺口表达 | `UnsupportedStatementError{Message string}`,API 层只能把整条 `AnalyzeSQL` 请求判为 `InvalidArgument` 并**丢掉已解析出的边** | `UnsupportedStatementError{Diagnostics []model.Diagnostic, Omitted int}`,分类枚举 + 引用;API 层保留边并把缺口作为 `AnalyzeSQLResult.diagnostics` 返回,CLI 渲染同一措辞 |
| 哨兵定义 | `__deletion__`/`__file__` 在 5 个方言包各写一遍字面量 | 收敛到 `model.DeletionColumnName`/`model.FileSourceName`,仓库里只剩一处定义 |

**行为变更约束**:analyzer 对同一条 SQL 产出的边、变换、RelationType、错误分类**一律不变**;变的是"缺口如何被表达与呈现"。验收以全语料绿 + 缺口文本不变为准。

## 范围内 / 非目标

**范围内**:`backend/plugin/lineage/{,model,algorithm,catalog,engines,mysql,tidb,mariadb,postgresql,starrocks}`、新包 `backend/plugin/lineage/engines`、`backend/server`(装配与注入)、`backend/runner/lineageanalyzer`、`backend/api/v1`(`LineageService`/`ExplainSQLService`/`AnalyzeSQL`)、`proto/v1/v1/lineage_service.proto` 与其生成物、`cli/cmd/lineage.go` 与 `cli/skill/SKILL.md`、相关 plan 文档。

**非目标**:

- 不改任何 analyzer 的产出(第二期的"零行为变更"约束延续,只对缺口表达方式做增强);
- 不动 omni 依赖政策与上游缺陷登记;
- 不动 OceanBase/Doris 的未注册决策(它们仍是显式 skip,只是从"没人注册"变成"装配列表里没有");
- 不把 `column_lineage_version.error_message` 改成结构化存储——列里存的仍是渲染后的文本,结构化只落在 RPC 响应上。

## Part 1:注册表从 `init()` 自注册改为编译期装配

### 设计

改造前,根包同时是四样东西:错误类型的家、注册表、全局 catalog 持有者、以及 `Analyze` 入口。`init()` 自注册的后果是"注册是副作用":一个包只要被链接就自动生效,而谁被链接由 blank import 决定(`backend/server/ultimate.go`),测试又各自 blank import。这既让 `Analyze()` 隐式依赖全局 catalog,也让"注册了什么"无法被静态检查。

改造后三层各自收敛:

```go
// 方言包(dialect.go / analyzer.go 头部)
func Registration() lineage.EngineRegistration {
    return lineage.EngineRegistration{Engine: storepb.Engine_MYSQL, Analyze: Analyze, Split: SplitStatements}
}

// 根包:只留类型/接口与错误类型,不再有包级可变量
type EngineRegistration struct { Engine storepb.Engine; Analyze AnalyzeRelationFunc; Split ScriptSplitter }
type Analyzer struct { catalog catalog.Provide; engines map[storepb.Engine]engineAnalyzer }
func NewAnalyzer(cat catalog.Provide, registrations ...EngineRegistration) *Analyzer
func (a *Analyzer) Analyze(ctx context.Context, engine storepb.Engine, sql string) ([]model.ColumnRelation, error)

// 新包 backend/plugin/lineage/engines:编译期列全五个引擎
func Registrations() []lineage.EngineRegistration
```

连带的效果:

- `Analyze(ctx, sql)` 变成 `Analyze(ctx, sql, cat catalog.Provide)`——catalog 从全局变量变成调用参数,方言不再需要 `lineage.GetCatalogProvide()`;
- 根包不再 import `backend/store`(它此前只为 `InitCatalogProvide` 而 import),分层更干净;
- `RegisterAnalyzeRelation` / `getAnalyzes` / `getSplits` / `CatalogProvide` / `InitCatalogProvide` / `GetCatalogProvide` / 根包 `mux` 全部删除;重复注册从"init 期 panic"变成 `NewAnalyzer` 对装配列表的 panic(同一条约束,但现在是一份静态列表的错,而不是运行期副作用的错);
- `backend/server/ultimate.go` 的 lineage blank import 删除:引擎集合由 `engines` 包显式列举,不再靠 blank import 的副作用。

### 结果

- 根包 `lineage.go` 从 166 行(含注册表 + 全局状态)缩到"类型 + 装配 + 分句合并",无任何包级可变状态;
- server 侧一处构造、三处注入:

```go
lineageEngines := lineage.NewAnalyzer(catalog.NewCatalogProvide(stores), engines.Registrations()...)
s.lineageAnalyzer = lineageanalyzer.NewAnalyzer(stores, lineageEngines)
// → configureGrpcRouters(..., lineageEngines) → NewLineageService / NewExplainSQLService
```

- `mysql/tidb/mariadb` 的正文(生成物)随之改了 `Analyze` 签名与 `splitStatements` → `SplitStatements`,由 `go generate ./backend/plugin/lineage/mysql` 重新生成,生成物与真相源仍逐字节来自 `mysql/analyzer.go` 哨兵之下的正文。

## Part 2:`UnsupportedStatementError` 结构化(分类枚举 + 引用)

### 设计

改造前的链路是"字符串一路到底":`algorithm.Diagnostics` 收 `[]string` 前缀句 → 方言拼成 `"analysis errors: …"` → 根包跨语句再拼一次 → runner 存文本、API 层拿到字符串后**无法区分**"分析器没建模"与"catalog 挂了",只能整条请求判失败并丢边(第一期 §0 已把这处记为遗留)。

改造后:

- `model.Diagnostic{Category, Subject, Reference, Detail}` + `model.DiagnosticCategory` 四类(`NotModelled` / `Unresolved` / `Ambiguous` / `CatalogUnavailable`);
- `algorithm.Diagnostics` 存结构体而不是句子,`Notes() ([]model.Diagnostic, int)` 一次性给出"保留的 note + 被上限截掉的条数";
- `lineage.UnsupportedStatementError{Diagnostics, Omitted}`;`Error()` 仍渲染 `analysis errors: <note>; <note>; N more not listed`,**与改造前逐字相同**(全语料 `error_contains` 与集成测试的 `not modelled: WITH before INSERT` 断言因此不动);
- 渲染集中在 `model.Diagnostic.String()` / `model.FormatDiagnostics()`,方言与根包不再各自拼句。

### API/CLI 落地(本期唯一的行为变化)

- `AnalyzeSQL`:遇到 `*UnsupportedStatementError` 时**不再**返回 `InvalidArgument`,而是保留 relations、把缺口映射成 `AnalyzeSQLResult.diagnostics`,并用 `omitted_diagnostic_count` 报告被上限截掉的条数。"整条 scope 失败"仍只有解析失败、无分析器、scope 无效三种;
- proto 新增 `AnalyzeSQLDiagnostic` 与 `DiagnosticCategory` 枚举(全前缀命名,与 `LLMProviderType`/`OpenLineageDatasetScope` 一致);
- CLI(`mxd lineage sql`):JSON 增加 `diagnostics`(结构化,enum 名)与 `omittedDiagnosticCount`,table 格式把缺口渲染成与服务器同措辞的一行。CLI 不能 import `backend/` 下的分析器(depguard),因此 `diagnosticText` 是客户端侧的第二份渲染——由 `TestDiagnosticTextMatchesTheAnalyzersWording` 与 `model` 侧的同名表用例钉在一起。

## Part 3:哨兵单点定义(T7 残留)

`__deletion__`(删除行时边的 target 列)与 `__file__`(装载语句的 source 表)此前在 mysql/tidb/mariadb/starrocks/postgresql 各写一遍字面量。收敛为:

```go
// model/relation.go
const DeletionColumnName = "__deletion__"
const FileSourceName = "__file__"
```

方言侧保留短名别名(`deletionFieldName`/`fileSourceMarker`/`fileSourceName`),但值改为指向 `model` 常量——与既有的 `resultTableName = model.ResultTableName`、`wildcardColumn = model.WildcardColumn` 同形,因此 `mysql/gen` 的接缝自检(要求方言头声明四枚常量)无需改动。复核:`grep -rn '"__deletion__"\|"__file__"\|"__result__"' --include=*.go backend/ cli/`(排除测试)只剩 `model/relation.go` 三行。

## 验收记录

| 项 | 命令 | 结果 |
| --- | --- | --- |
| 全量单测(免依赖) | `go test ./...` | 通过 |
| 全语料(662 例 + 各包守卫) | `go test ./backend/plugin/lineage/...` | 通过,用例数与断言未改 |
| 装配完整性 | `go test ./backend/plugin/lineage/engines/` | 五个引擎各自可分析、注册唯一且两半接缝齐全、OceanBase/Doris/MSSQL 报 `ErrorEngineNotSupported`、重复注册 panic |
| 生成物新鲜度 | `go test ./backend/plugin/lineage/mysql/gen/` | 通过(正文改动已重新生成) |
| 结构化缺口(单测) | `go test ./backend/plugin/lineage/ ./backend/plugin/lineage/model/ ./backend/api/v1/ ./cli/cmd/` | 通过(分类/引用/上限计数/措辞) |
| 真实服务器 + MySQL | `make test-integration` | 通过,含 `TestAnalyzeSQLRealServerIntegration` 新增子用例"部分分析保留边并报告 diagnostics" |
| proto | `cd proto && buf format -w . && buf lint && buf generate` | 通过,生成物与 proto 一并提交 |
| 构建 | `go build ./...` | 通过 |

## 未做与遗留

- **T5 omni 依赖策略**:按本期范围约定不动。反射读 `Loc`、`hasLeadingWith` 手工扫描、`viewbody.go`/`rawsql.go` 的 token 手术、MariaDB 的 `knownParserGaps` 全部原样保留;
- **`column_lineage_version.error_message` 仍是文本**:结构化只到 RPC 边界;若将来前端要按类别过滤历史缺口,需要一次 schema 变更;
- **`algorithm` 内 `Analyzer` 共享基状态**(第二期附录 A 第 6 条)仍留待后续:那是把 `pushScope`/`popScope`/`attachColumnLookup` 提成共享状态类型的更大动作,与本期三项无耦合。
