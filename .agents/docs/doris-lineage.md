# Doris 血缘分析器 — Reference

> Status: **implemented**。Maintenance reference for `backend/plugin/lineage/doris/`。
> Related: [lineage-analyzer.md](lineage-analyzer.md)(装配、runner 生命周期、生成器、AST 审计方法)、[lineage-semantics.md](lineage-semantics.md)(transformation 模型与各引擎语义)、[starrocks-doris-driver.md](starrocks-doris-driver.md)(`mv_infos()` 与 `SHOW CREATE` 的来源)、[omni-upstream-defects.md](omni-upstream-defects.md)(上游缺陷与不修改政策)、[backend/plugin/lineage/AGENTS.md](../../backend/plugin/lineage/AGENTS.md)(改代码的规则)。

## What it is

`backend/plugin/lineage/doris` 把一条 Doris SQL 解析成 `[]model.ColumnRelation`,基于 `github.com/bytebase/omni/doris` 的解析器与类型化 AST(`doris/ast` + `doris/parser`,约 4.8 万行)。它注册 `storepb.Engine_DORIS`,是第七个方言包;`engines.Registrations()` 在编译期列全七个,进程启动时组成一个 `lineage.Analyzer`(见 [lineage-analyzer.md](lineage-analyzer.md))。

它是**独立包、不是生成物**,与 StarRocks / MSSQL 同一先例;MySQL / TiDB / MariaDB 仍是唯一的生成家族(哨兵 `// == MYSQL-FAMILY SHARED BODY ==`)。

omni 的 Doris 与 StarRocks 极其接近:关键字集合只差 3 个(StarRocks 独有的 `FIELD` / `PERCENTILE` / `PERCENTILE_UNION`);AST 结构体集合只差 4 个 StarRocks 独有(`ParenSelect`、`TableFunctionRef`、`IntoOutfileClause`、`InlineTable`)与 5 个 Doris 独有(`GroupedQuery`、`LateralView`、`TableSample`、`ArraySliceExpr`、`StructLiteral`)。把 173 条两边都能解析的 StarRocks 语料逐条对比规范 AST 形状:171 条一致,2 条例外正是 §方言事实的前两行。Doris 遍历因此是 StarRocks 遍历的移植,而不是第三个独立实现。

四条输入路径都已接通,无需 engine 分支:

| 来源 | 文本形态 | 说明 |
| --- | --- | --- |
| VIEW | `information_schema.VIEWS.VIEW_DEFINITION` 的**裸查询体** | runner 的 `buildSQL` 包成 `CREATE VIEW <name> AS <body>`;裸查询体不以 `CREATE` 开头,不走 `isCompleteStatement` 的放行分支 |
| MATERIALIZED_VIEW | Doris `mv_infos("database"=?)` 的 `QuerySql` 列,**裸查询体** | `backend/plugin/db/starrocks/sync.go` 的 Doris 分支读 `Name, QuerySql`,存进 `MaterializedViewMetadata.Definition`;runner 包成 `CREATE MATERIALIZED VIEW <name> AS <body>`。StarRocks 的同一列存的是完整 `CREATE MATERIALIZED VIEW`,Doris 的不是——这是 [starrocks-doris-driver.md](starrocks-doris-driver.md) 里两引擎共用字段、含义不同的一处 |
| MANUAL_SQL | 用户粘贴的脚本 | 根包切分后逐条分析;`SplitStatements` 走 `dorisparser.Split` |
| APPLICATION_SQL | 参数化语句 | 与其它方言同一条路径(见 [application-sql-ingestion.md](application-sql-ingestion.md));`sqlsanitize` 的 DORIS 分支复用 omni Doris 的一次性 `Tokenize`,lex 错误一律拒绝不写入库——Doris 词法器与 StarRocks 的逐字节相同(只差 ast import),所以两个分支做的是同一件事 |

`CREATE VIEW` / `CREATE MATERIALIZED VIEW` 的体是 WITH、集合运算或括号查询时,omni 的 CREATE VIEW 语法只接受裸 SELECT,于是 `viewbody.go` 用 `extractViewDDL` 抽出顶层 `AS` 之后的体,再按顶层查询解析。兜底只接管**以 CREATE / ALTER 开头**的语句,且目标名只取紧邻 `VIEW`、用 `.` 分隔的那一段标识符:两者缺一不可——`LATERAL VIEW` 会让普通 SELECT / DML 也含 `VIEW` 关键字(接管它会把真实语法错误换成假的 "view query" 报错),而 `REFRESH ASYNC START(...)`、`DEFINER = 'u'` 这类未建模的头部子句里含**标识符**(`ASYNC` 不是关键字),把名字扫描一路做下去就会把目标写成 `mv.ASYNC`。命中任一种情况即放弃兜底、保留 omni 的原始解析错误。

## 真实引擎核对

用本地 Apache Doris FE **4.1.4** 容器逐条执行(临时库,已删)得到的裁决,是本篇其余结论的事实基础:

| 语句 | 引擎 |
| --- | --- |
| `SELECT * FROM (SELECT arr FROM ev) d LATERAL VIEW explode(d.arr) u AS elem` | **接受**(返回行,与 `WITH d AS (…) SELECT * FROM d LATERAL VIEW …` 同结果同列名)——omni 拒绝,已由恢复垫片取回血缘,见 §方言事实与 open item 7 |
| `LATERAL VIEW OUTER …` / `LATERAL VIEW … AS (c1)` | 拒绝(`extraneous input 'OUTER'` / `'('`)——与 omni 一致 |
| `LATERAL VIEW posexplode(ev.arr) u AS pos, elem` | 接受(多列 `AS` 可用) |
| `SELECT … INTO OUTFILE 's3://…'`(含 `WITH` 前缀、`FORMAT AS CSV`) | **语法接受**(planning 阶段因 `enable_outfile_to_local` / BROKER 配置报错)——omni 拒绝,已由恢复垫片取回投影血缘,见 §不新增血缘语义的语法与 open item 8 |
| `TABLESAMPLE(10 PERCENT)` / `(100 ROWS)` / `(100 ROWS) REPEATABLE 1` | 接受;`REPEATABLE(1)` 拒绝 |
| `QUALIFY rn = 1`(select-list 别名)与 `QUALIFY ROW_NUMBER() OVER (…) = 1` | 都接受 |
| `INSERT OVERWRITE ev …` | 拒绝;必须写 `INSERT OVERWRITE TABLE ev …` |
| `CREATE TABLE t (a INT, b INT) AS SELECT …` | 拒绝(`Should not define the entire column in CTAS`)——CTAS 不许声明列清单 |
| `CREATE TABLE t (k BIGINT, v VARIANT<'id': INT, properties(…)> …)` | 接受——omni 拒绝,见 open item 6 |
| `CREATE TABLE t (k BIGINT, v ARRAY<VARIANT>) …` | 拒绝(`ARRAY unsupported sub-type: variant`) |
| `SELECT [ev.id, ev.id] FROM ev` | 拒绝(数组字面量只收常量) |
| `SELECT v['user']['name'], v.user.name FROM ev` | 都接受且**返回同一个值**——点号路径合法,已建模(见 §方言事实) |
| `SELECT v['user'].name FROM ev` / `v['user'].name.deep` | 接受,**列名就是源文本** `v['user'].name`;`v['user'].name` 与 `v['user']['name']` 同值。混合写法 omni 拒绝,已由恢复垫片取回(见 §方言事实与 open item 11) |
| `SELECT a[1].b FROM t` | 接受(数组元素上的字段),但**括号写法不是同一个意思**,因此不重写、保留硬失败 |
| `WITH c AS (…)` 在 INSERT / UPDATE / DELETE 之前 | **三种都接受并生效**(4.1.4 逐条读回:UPDATE 改到了值、INSERT 插入了行、DELETE 删掉了行;`SET b = (SELECT m FROM c)` 这种引用也生效)。omni 在语句关键字处报错,已按 `withdml.go` 的规则恢复(见 §方言事实) |
| `numbers("number"="2")` 的列名 | `number`(按参数名),不是函数名;`u.numbers` 报 `Unknown column`。已按参数 key 建模 |
| FROM 表值函数的实参 | 只能是**字面量**:`explode_numbers("number" = tv.id)` 报 `mismatched input '.' expecting {')', ','}` → 合法 SQL 上 FROM TVF 不携带列级血缘(列值实参形态 omni 能解析、引擎拒绝) |
| 限定列 `t.c` 在输出列 / CTAS / `INSERT … SELECT` / `DELETE … WHERE` / 谓词各处 | 都拒绝:`Unknown column 'nope' in 'ev2'`(报在 `PROJECT` / `FILTER` 子句上);`UPDATE u SET name = u.nope`(UNIQUE 表)同样拒绝 → **限定列按 registry 校验**已落地(见 §方言事实) |
| `SELECT ev2.NAME AS n FROM ev2` | **接受**(列名不区分大小写)→ 校验按 `EqualFold` 比对,不是按字节 |
| `FROM ev7, ev8 v; SELECT v.name` | 拒绝:`v.name is ambiguous: v#1, v#3`(槽位号随关系数变化)→ 缺口 |
| StarRocks 的 VARIANT 访问 | 是**函数式**(`variant_query(v,'$.user.name')`/`get_variant`);SR 4.1 引擎拒绝点号(`Column 'v.user.name' cannot be resolved`)与括号下标(`cannot subscript type VARIANT`)→ Doris 的路径读法**不镜像**到 SR |
| `CREATE MATERIALIZED VIEW mv REFRESH AUTO ON SCHEDULE EVERY 1 DAY STARTS '…' AS SELECT …` | 语法接受;`REFRESH ASYNC`(2.1 拼写)被 4.x 拒绝 |
| `mv_infos("database"="…")` | 可查询 |

## 方言事实(与 StarRocks 的差异)

| 事实 | 后果 | 代码 |
| --- | --- | --- |
| 括号查询、以及"同一条查询上重复出现的 ORDER BY/LIMIT 组"由 `GroupedQuery{Query, OrderBy, Limit, Offset}` 承载,不是 `ParenSelect{Sel}` | `dispatch` 与 `processQueryNode` 都按 `*nodes.GroupedQuery` 递归进 `Query`;它同时是作用域边界(带 WITH 的组不把 CTE 名提升到组外) | `analyzer.go`(`dispatch`、`processQueryNode`) |
| 表值函数是 `TableRef.Func`(一个 `*FuncCallExpr`),不是独立 `TableFunctionRef` | `processTableExpr` 先看 `t.Func`:有则注册成查询本地关系(不写注册表、不发明对象),别名缺省用函数名。Doris 不给它列清单,所以它只暴露"函数名"这一列:`FROM t, explode(t.arr) u` 把 `t.arr` 记为 `u.explode` 的来源,`SELECT u.explode …` 能解析;纯字面量调用(`numbers("number" = "10")`)没有可命名的来源,不产边也不报缺口 | `processTableExpr`、`processTableFunction` |
| `InsertStmt` 没有 `ByName` | 目标列清单永远是位置式:INSERT 的 `Columns` 按序映射到查询的输出列;没有"按名字匹配"可读 | `processInsertStatement` |
| CTAS 列名来自 `CreateTableStmt.Columns`,Doris 没有 `CTASColumns` | Doris 的 CTAS 列清单每列都带类型,名字从类型化定义里取并**盖过查询自己的输出别名**:`CREATE TABLE t3 (k INT, n VARCHAR(50)) AS SELECT id, name FROM users` 记 `users.id → t3.k`、`users.name → t3.n`;未声明时用查询输出别名。两处按位置对不齐的情形**有意不报诊断**:声明 N 列而查询多出输出时,多出的按查询别名落边;重名列落成同目标的两条边——真实引擎本就拒绝这两种语句(`Should not define the entire column in CTAS`),不会静默读错一个合法语句 | `ctasColumnNames` |
| 混合写法 `v['a'].b` 由**改写成纯括号形式**后解析,并按**原文**命名表达式 | omni 在 `]` 后的 `.` 处报错,而引擎接受并给出同一值(`v['a'].b` ≡ `v['a']['b']`,4.1.4 实测);`mixedpath.go` 把跟在**字符串下标**后的 `.段` 改写成 `['段']`,于是语句的血缘被取回。改写会改变文本,所以改写后的 source 同时保留**原文**与映射(`source.original`/`edits`)——引擎是按查询文本给列命名的(`SELECT v['user'].name FROM ev` 报出的列名就是 `v['user'].name`),改写后的拼写不能变成我们自己的名字,`exprText` 因此回读原文。**只改字符串下标**后的点:`a[1].b` 是数组元素上的字段访问,括号形式不是同一个意思,保持硬失败 | `mixedpath.go`、`source.original`/`edits`、`exprText` |
| 通配符落到**声明目标清单**时按位置填充:查询本地关系的 `*` 是**一个**输出列,展开成该关系每列一条边,所以关系自己的列按序占用清单从该位置起的条目 | `INSERT INTO dst (k, name) SELECT * FROM (SELECT k, name FROM src) d` 原先每条边都落在清单第一列(`src.name → dst.k`,且 `dst.name` 无上游);现在 `src.k → dst.k`、`src.name → dst.name`。基表不受影响(`*` 早已按 catalog 展开成 N 个输出列,位置映射本来就成立);**暴露列未知**的查询本地关系保留旧读法(通配符代表整条 lineage,第一条清单项命名它),因为无从判断哪列是哪列。`wildcard_target_test.go` 与语料两例(派生表 + 基表对照)钉住 | `generateEdgesForTarget`、`traceWildcardThroughTableLineageToTarget` |
| `LATERAL VIEW` 作用于**派生表**时,把派生表提升成同名 CTE 后再解析 | omni 的 Doris 语法在 `LATERAL` 处报错(§12.2),而引擎接受该语句;提升不改变语义(派生表读一次 = CTE 读一次,4.1.4 上两式同结果同列名),因此血缘被完整取回:`SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag` 记 `ev.v → __result__.v` 与 `ev.v → __result__.tag`;视图体走同一条垫片(定义整体解析失败 → 抽出 body → 重写)。提升让该名字获得 CTE 优先级,所以**语句里还有另一个关系叫这个名字时拒绝改写**;一次只处理一个、只在语句自身层级;裸别名与 `AS d` 都支持 | `lateral.go`、`rewriteQuery` |
| CTE 的暴露列 = 声明清单**从左往右**改名,清单够不到的输出保留查询别名 | 引用 CTE 的列按 body 真正暴露的名字解析,并且**刻意不用** `scope.ExposedColumnNames`——那条"ary 不符就整条作废"是 MySQL 家族的规则,而 Doris 4.1.4 实测短清单可用:`WITH c (a) AS (SELECT k, name FROM t) SELECT a FROM c` 与 `SELECT name FROM c` 都能执行(前者是 `a`=k,后者是未改名的 `name`)。暴露列写回 `definition.Columns`(PostgreSQL 同一做法),所以常量输出列没有来源、不产边,但它**是**该关系的列;此前拿 lineage target 当清单,`WITH c AS (SELECT 1 AS x, k FROM ev2) SELECT x FROM c, ev2` 这条引擎可执行的语句被报成缺口。**边的目标名映射**与暴露列共用同一份名字 | `processCTE` |
| `WITH` 可以写在 **INSERT / UPDATE / DELETE 之前**(引擎如此,omni 不是) | 4.1.4 实测三种都能执行并生效(`WITH c AS (SELECT 1 AS a) UPDATE t SET b = 9 WHERE a = 1` 真的把 b 改成 9、INSERT 真的插入、DELETE 真的删除;被引用的 CTE 也生效),而 omni 在 `INSERT`/`UPDATE`/`DELETE` 处报语法错误 → 过去是**整条硬失败清血缘**。`withdml.go` 把 INSERT 的 WITH 挪到它写入的那个查询之前(引擎与 omni 都在那里读它:`INSERT INTO t (a) WITH c AS (…) SELECT a FROM c` 解析通过且 CTE 参与分析),UPDATE/DELETE 与 **VALUES 源**的 INSERT 没有可放的位置(omni 的 WITH 只认 SELECT),所以只在语句确实没读到它定义的任何名字时删除(未使用的 CTE 不写任何东西,而"读到"按**每个 token** 判断:CTE 可以用词法上是关键字的词命名,`WITH `type` AS (…) … FROM type` 引擎真的会读);**确实被读到的仍保留硬失败**,见 open item 14。`INSERT INTO t PARTITION (p) (a) WITH c AS (…) SELECT …` 是引擎与 omni 都接受的落点,允许;`WITH LABEL` 是唯一交给解析器的形状(引擎接受、omni 拒绝) | `withdml.go`、`rewriteQuery` |
| UPDATE 的 SET 目标**可以带限定符**(`UPDATE t1 SET t1.name = t1.other`),StarRocks 直接拒绝这个形状 | 写入目标仍取语句的表(`targetSchema`/`targetTable`),限定符只用来检查该列属于作用域里的关系;`UPDATE t1 AS x SET x.name = x.other` 同样接受,表达式保留别名的拼写。两条语料钉住 | `processUpdateList` |
| 限定列(`t.c` / `db.t.c`)由 registry 回答,而不是"限定符绑上关系就信列存在" | Doris 引擎对限定列与无限定列给同一个答案(`Unknown column 'nope' in 'ev2'`,实测见 §真实引擎核对),所以根作用域打开 `scope.SetVerifyQualifiedColumns`:某关系被 registry 描述(列清单非空)却不拥有该列时,引用解析失败,写它的子句各自报缺口——输出列 / 输出目标(CTAS、`INSERT … SELECT`)/ 赋值 / DELETE 条件 / 谓词 / CTE 体 / 生成器实参。**仍按旧读法**的情形:关系未被 registry 描述、列清单为空(未重扫)、引用已 resolved、列名大小写不同视为同一列(`EqualFold`);**查询本地关系(CTE / 派生表 / 表值函数 / LATERAL VIEW 生成器)一律不校验**:排除按键是"关系从哪来"而不是"清单有多准"——CTE 的清单取自 lineage target(漏掉常量输出列),表值函数是参数 key 占位(引擎实测 `SELECT m.name FROM mv_infos("database"="demo") m` 可用,而占位声称的那个 `database` 列反被引擎拒绝),LATERAL VIEW 的 `AS` 列表与派生表的输出别名却是语句自己的字面;会错的清单绝不能拿来报缺口,而名不副实的"权威"标记会造成更坏的假缺口,所以查询本地关系一律不问(代价见 [lineage-semantics.md](lineage-semantics.md) open item 4)。无限定名的单关系回退是既有刻意行为(见 [lineage-semantics.md](lineage-semantics.md)),不在本条内。**已知代价**:registry 列清单落后于引擎时,限定写法会丢边而无限定写法保留(`SELECT t3.name FROM t3` vs `SELECT name FROM t3`),这是"registry 是限定列的唯一权威"的代价,`qualified_column_test.go` 两侧都钉住 | `NewAnalyzer`(根作用域一行开关)、`scope.SetVerifyQualifiedColumns` / `ownsColumn` |
| VARIANT 子列路径:点号 `v.user.name` 与括号 `v['user']['name']` 同义,但 omni 把点号读成普通限定列名 | 分析器的顺序与引擎一致:① 限定读法优先,但**仅在 catalog 未否证该列时**(由上一行的限定列校验实现)——`SELECT v.name FROM ev7 v`(别名 v 指向 ev7,而 ev7 没有 `name` 列)原来会产出指向不存在列的边,现在按引擎改读 `ev7.v` 的路径;② 关系前缀必须**按语句写法寻址**该关系(前缀末段须等于关系的表名或别名,多段前缀还须匹配其 database),否则作用域的宽松限定匹配会把 `ev.v` 绑到同名别名 `v` 上;③ 基列类型由 catalog 作证是 `variant` 才成立(`FROM ev, ev2 v` 的 `v.name` 仍读 ev2.name,前缀列是别的类型时按引擎一样不退回路径),两处同名 VARIANT 列则不猜;④ 以上都不成立时,该引用保持未解析并由写它的子句报缺口(`FROM ev7, ev8 v; SELECT v.name`,引擎同样报 `v.name is ambiguous`)——不再回退成指向不存在列的边。输出列名取整条路径原文(引擎行为,不是最后一段) | `collectColumnSources`、`variantPathBase`、`variantBase` |
| `TableRef.LateralViews` 是**新增血缘** | `LATERAL VIEW gen(args) alias AS c1[, c2]` 把生成器实参读到的每一列映射到 AS 声明的每一列,注册成一个查询本地关系;链式生成器左到右,后者能读前者声明的列。`u.elem` 因此能解析,而不是"未知名字";`generator_lineage_test.go` 钉住单列、多列、通配符与链式四种 | `processLateralViews`、`addGeneratedRelation` |

## 不新增血缘语义的语法

本节列的是 Doris 独有(或两引擎共有)但不改变血缘语义的语法形态;`26_test_doris_forms_table.yaml` 的用例同时钉住这些形状与 VARIANT 子列路径(含"关系前缀优先"、"catalog 否证的限定列让位路径"、"前缀撞名别名"与"无证据仍 unresolved"),其余由通用表达式走查覆盖。会新增血缘的 Doris 独有形状是 LATERAL VIEW 与 VARIANT 子列路径,见 §方言事实。

| 形状 | 读取方式 |
| --- | --- |
| `TABLESAMPLE(1000 ROWS)` / `TABLESAMPLE(20 PERCENT)` / `REPEATABLE <n>`(`TableRef.Sample`) | 只决定取哪些行,不产生列血缘。omni 与真实 4.1.4 一致:接受无括号的 `REPEATABLE <n>`,拒绝 `REPEATABLE(<n>)` |
| `GROUP BY … WITH ROLLUP`(`SelectStmt.GroupByWithRollup`) | 分组键与普通 GROUP BY 同一批;ROLLUP 只多出小计行,不引入新列 |
| 函数式分组集 `ROLLUP(a,b)` / `CUBE(a,b)`(`FuncCallExpr`)与 `GROUPING SETS ((a,b),(a))`(`GroupingSetsExpr`) | 分组集是**一组分组**,不是单个键,而模型只能记一串键,所以记它分组的**每一列**——与 `GROUP BY a, b WITH ROLLUP` 同结果,也是消费方能用的那部分。此前 `groupByKeys` 直接渲染节点原文,聚合边记的是分组表达式本身(`ROLLUP(a,b)`)。**键的拼写仍取对象定义原文**:Doris 存下来的视图体把每列写成 `` `internal`.`db`.`t`.`c` ``,所以键也是这样(Doris 的 `v_customer_segment_rollup` 实测),与 PG / StarRocks 记 `l.order_date` 同类 |
| 数组切片 `a[1:2]`(`ArraySliceExpr`)、元素访问 `value[index]`(`ElementAtExpr`)、`STRUCT(…)` / Map 字面量(`StructLiteral`) | 通用表达式走查覆盖;切片的来源是 `a` |
| `CONVERT(x USING charset)` | 通用表达式走查覆盖 |
| `VARIANT` 列(含 `NULL` / `NOT NULL`) | **类型不参与血缘**:分析器只记列级边,从不读列类型;建表、`INSERT … SELECT`、CTAS 一视同仁 |
| `VARIANT` 子列路径 `v['key']` / `v['a']['b']` / `v['arr'][1]`(`ElementAtExpr` 链)与点号 `v.user.name`(见 §方言事实) | 路径选择发生在**列内部**,所以整列是来源:`SELECT v['user']['name'] AS n FROM events` 与 `SELECT v.user.name AS n FROM ev` 都记 `… .v → __result__.n`(indirect + PROJECT);子列不是注册表对象,绝不做边端点 |
| `COPY INTO <table> FROM <stage>` | 文件整体是来源:`__file__.* → target.*` |
| `LOAD LABEL …(DATA INFILE … INTO TABLE …)` | 每个 data description 各有目标与可选列清单;omni 只把 SET 子句当原文,列级 SET/WHERE 不建模(与 StarRocks 同一条决策) |
| 目录限定名 `catalog.db.table` | 目录段被丢弃,按 `db.table` 分析(注册表没有 catalog 维度) |
| `SELECT … INTO OUTFILE '…' [FORMAT AS …]` | 结果 sink,**不记血缘**:投影的边就是整条语句的血缘(`SELECT k FROM t INTO OUTFILE 'x'` 记 `t.k → __result__.k`)。omni 的 Doris 语法拒绝该子句(§12.3),所以这里先剥掉它再解析其前面的查询;与 StarRocks 分析器对同一语句的结果一致,也**不报诊断**(sink 不携带列级血缘,文件端不是注册表对象)。`outfile.go` 负责剥离,`outfile_test.go` 与语料两例钉住 |

## 能力矩阵

**建模**:SELECT(投影、别名、`*` / `t.*`、DISTINCT、CASE / CAST / 运算符、窗口、GROUP BY + HAVING + QUALIFY + ROLLUP、谓词、VARIANT 子列路径(括号与点号两种写法))、FROM(表、别名、db 限定名、INNER / LEFT / RIGHT / FULL / CROSS / SEMI / ANTI JOIN、派生表、CTE(含列清单、链式、递归自身)、表值函数、LATERAL VIEW)、集合运算(UNION / INTERSECT / EXCEPT,含 ALL 与嵌套)、CREATE VIEW / ALTER VIEW / CREATE MATERIALIZED VIEW(`CreateMTMVStmt`,含列清单)、CREATE TABLE … AS SELECT、INSERT [OVERWRITE] INTO … SELECT / VALUES(位置式列清单)、UPDATE … SET(含 FROM)、DELETE … USING … WHERE(→ `__deletion__`)、COPY INTO、LOAD、查询本地关系的展平与谓词影响。

**报告为缺口而不是失败**:

| 形状 | 行为 |
| --- | --- |
| `MERGE INTO` | `not modelled:` 缺口,兄弟语句的边保留(与 StarRocks、PostgreSQL 对 MERGE 的同一决策);`analyzer_test.go` 的 `TestUnmodelledStatementsAreGaps` 钉住 |
| 未建模的查询表达式 | `not modelled: query expression` 缺口,语句其余部分照常分析 |
| 引用解析不到(未解析列、registry 否证的限定列、通配符限定符) | `unresolved` 缺口 |
| 查询本地关系(CTE / 派生表)上的 VARIANT 路径基列 | 引擎合法产值,但查询本地关系没有声明类型,类型证据不可用 → `unresolved` 缺口(非静默丢;中期可沿 temp lineage 追溯类型) |
| catalog 查询失败 | `DiagnosticCatalogUnavailable` + 通配符回退,分析继续 |

**不建模(有意)**:`LOAD` 的 SET/WHERE 列级血缘;目录维度(注册表里没有)。

## 诊断与失败边界

| 场景 | 行为 |
| --- | --- |
| omni 解析失败(含**被读到的** `WITH … UPDATE/DELETE`、被抽出的视图体自身不合法) | 硬失败:无部分结果;runner 清该对象血缘 + `error_message` |
| 多语句输入 | 硬失败:分析器只定义在单条语句上,切分由根包负责 |
| 某形状没建模(MERGE、未建模查询表达式) | 部分分析:兄弟边照存,缺口写 version 行 / RPC diagnostics |
| 引用解析不到(未解析列、registry 否证的限定列、通配符限定符) | 部分分析 + 对应 `model.Diagnostic` |
| catalog 查询失败 | `DiagnosticCatalogUnavailable` + 通配符继续 |
| 只有注释 / 空白 | 硬失败(没有语句可分析),而不是"没有血缘" |

## 不变量

1. **`GroupedQuery` 只是括号/重复子句组的壳**:递归进 `Query`,绝不把它当成关系或边的端点。
2. **`TableRef.Func` 与 LATERAL VIEW 关系是查询本地关系,永不作为边的端点**:先经 `flattenTempSourceLineage` 展平;**生成器实参里没有可解析列(纯字面量调用)时不产边**——它没有这个分析能命名的来源。实参里的表达式子查询按 select-list 的同一套 `expressionSubquerySources` 取来源,不静默丢。
3. **INSERT 的目标列清单永远位置式**:Doris 没有 `BY NAME`,`Columns[i]` 对查询第 i 个输出列。
4. **CTAS 列名取类型化 `ColumnDef`**:Doris 没有名字清单;声明了就用声明,没声明就用查询输出别名。
5. **`WITH` 在 INSERT / UPDATE / DELETE 之前是引擎合法、omni 不认的形状,能救的必须救**:先按 `withdml.go` 的规则改写(INSERT 挪到查询前;UPDATE/DELETE 只在语句没读到它时删除),改写不了的才硬失败——**绝不因为"语法层应该不存在"就假设它不存在**(4.1.4 三种都能执行,见 `doris-lineage.md` §真实引擎核对)。
6. **MERGE 与未建模查询表达式是缺口,不是硬失败**:已解析的边必须留下 `model.Diagnostic`;只有解析失败才清血缘。
7. **视图兜底只接管 `CREATE` / `ALTER` 开头、且目标名是紧邻 `VIEW` 的点分标识符段**:命中未建模头部子句(含标识符)即放弃兜底并保留 omni 的原始解析错误——绝不把头部子句里的词当对象名,否则会向注册表里不存在的对象写边(曾产出 `mv.ASYNC`)。
8. **任何输出变更(边、变换、错误分类)必须过黄金语料**,语料用精确边匹配(`subset` 未使用)。
9. **限定列按 registry 校验,但只问 registry 描述得出来的关系**:只有"关系被描述(列清单非空)且不拥有该列"才让限定读法失败;未注册关系、空列清单、已 resolved 的引用、以及**一切查询本地关系**(CTE / 派生表 / 表值函数 / LATERAL VIEW 生成器,它们的清单由分析器或语句自行给出,不经 registry)都保持旧读法。被否证的读法必须**有替代读法**才替换,没有替代读法时按引擎报缺口,而不是产一条指向不存在列的边;反过来,不经 registry 的清单绝不用来报缺口——这是同一条规则的两面。

## Open items

1. **MERGE 未建模** — 与 StarRocks 同:报 `not modelled` 缺口而不是产边。要建模得先决定 MERGE 的动作语义,不能自行猜。
2. **`LOAD` 的 SET/WHERE 无列级血缘** — omni 只把 SET 当原文,分析器只读 target 与 column list(与 StarRocks 同;[lineage-analyzer.md](lineage-analyzer.md) 已登记)。
3. **APPLICATION_SQL 里的结构性整数会被 ingest gate 拒绝** — `sqlsanitize` 的 N1 规则把每个整数字面量改写成 `?`,不看语法位置,于是 `CREATE TABLE t3 (k INT, n VARCHAR(50)) AS SELECT …` 变成 `VARCHAR(?)`、`SELECT * FROM t TABLESAMPLE(10 PERCENT)` 变成 `TABLESAMPLE(? PERCENT)`;Doris 解析器要求这两处是整数,所以 `backend/api/v1/application_sql_handler.go` 以解析失败拒绝上报——即使分析器读的是原始语句。重写输出在 MYSQL / POSTGRES / STARROCKS / DORIS / MSSQL 上逐字节相同(含 canonical text 与 fingerprint),这是方言中性的位置盲重写,不是 Doris 缺陷(PostgreSQL 只是恰好接受 `TABLESAMPLE BERNOULLI (?)`)。注意 gate 由分析器决定:注册 Doris 之前 `Analyze(DORIS, …)` 返回 `ErrorEngineNotSupported`(不是 `UnsupportedStatementError`),**所有** Doris 应用语句都被拒;现在只剩这些结构性整数形态。修法是位置感知的字面量分类,属跨方言设计变更。已登记在 [backend-open-items.md](backend-open-items.md)。
4. ~~**真实引擎的语法裁决已完成,端到端仍未验证**~~ — **端到端已由 `test/e2e_etl` 的 Doris hop 验证**(`dags/e2e_07_pg_to_doris.py`、`dags/e2e_08_doris_transform.py`;跑法与证据见 `test/e2e_etl/README.md`,发现见 `FINDINGS.md` F10/F11)。实例 `doris-dev-1`(127.0.0.1:9031)登记后,`doris/10_ods_ddl.sql` + `20_ads_ddl.sql` 建出的对象经 schema sync 进入注册表,六个视图 + 一个异步 MV **全部分析成功、0 错误**(`verify_chain.sql` §26);PostgreSQL → Doris 的 DataX hop 产 37 条列边,Doris → StarRocks 的 hop 落在分析器自己拥有的视图 GUID 上(§23、§25)。**实测确认的存储形态**:视图(`SHOW CREATE VIEW` / `information_schema.VIEWS.VIEW_DEFINITION`)与 MV(`mv_infos()` 的 `QuerySql`)都把每列写成 `` `internal`.`db`.`t`.`c` `` 三段限定,分析器丢弃 catalog 段、按同一 database 读回;MV 在 `information_schema.tables` 里是 `BASE TABLE`,与 `sync.go` 的 Doris 分支一致。语法层裁决仍见 §真实引擎核对(方法:临时库 + 逐条 `mysql -P9030` 执行,库已删)。
5. **函数 / 存储过程血缘未覆盖** — 与所有方言同:队列只扫 VIEW / MATERIALIZED_VIEW / MANUAL_SQL / APPLICATION_SQL。
6. **`VARIANT<…>` schema template 是 omni 解析缺口** — `CREATE TABLE t (k BIGINT, v VARIANT<'id': INT>)`、其 CTAS 形式与 `ALTER TABLE … ADD COLUMN v VARIANT<…>` 都是合法 Doris DDL(4.1.4 实测接受),但 omni 在 `<` 处报语法错误(裸 `VARIANT` 与 `VARIANT NULL` 正常)。视图 / MV 体不含建表语句,所以这两条路径不受影响;MANUAL_SQL / APPLICATION_SQL 携带这种 DDL 会硬失败(清血缘 + `error_message`),CTAS 形式会整条丢边。明细见 [omni-upstream-defects.md](omni-upstream-defects.md) §12;按不修改政策不 patch 上游。
7. ~~**`LATERAL VIEW` 作用于派生表是 omni 缺口**~~ — **omni 缺口仍在,分析器已恢复**。`SELECT * FROM (SELECT arr FROM ev) d LATERAL VIEW explode(d.arr) u AS elem` 在真实 4.1.4 上**可执行**(与改写后的 `WITH d AS (…) SELECT * FROM d LATERAL VIEW …` 返回同样的行、同样的列名 `arr` / `elem`),omni 却在 `LATERAL` 处报语法错误(§12.2)。`lateral.go` 把这个派生表提升成同名 CTE 后重解析——派生表读一次与 CTE 读一次是同一个关系。提升会让该名字获得 CTE 的优先级(高于同名基表),所以只要语句里还有另一个关系叫这个名字就**拒绝改写**、保留硬失败;一次也只处理一个、且只在语句自身层级。裸别名与 `AS d` 两种写法都支持。`lateral_test.go` 与语料两例(普通 SELECT 与视图体)钉住,变异验证(禁用垫片 → 语料两例 + 单测全红)。反面:`LATERAL VIEW OUTER` 与 `AS (c1)` 括号列清单被引擎拒绝(`extraneous input 'OUTER'` / `'('`),omni 的拒绝与引擎一致,不是缺陷。
8. ~~**`SELECT … INTO OUTFILE` 是 omni 缺口**~~ — **omni 缺口仍在,分析器已恢复**。真实 4.1.4 **能解析**该语句(随后在 planning 阶段因 `enable_outfile_to_local` / BROKER 配置失败),omni 在 `INTO` 处报错(§12.3)。`outfile.go` 的垫片在解析失败后剥掉这个 sink 再解析其前面的查询:该子句不携带列级血缘,投影的边就是整条语句的血缘——**与 StarRocks 分析器对同一语句给出的结果一致**(它的 AST 有 `IntoOutfileClause` 而遍历忽略它,且不报诊断)。只有形如"深度 0 的 `INTO` 紧跟 `OUTFILE` 关键字与字符串字面量"才剥;`INSERT INTO` / `COPY INTO`、缺字面量、括号内的 INTO 一律不剥,剥完仍解析不通过就保留原始硬失败。`outfile_test.go` 与语料两例钉住,变异验证(禁用垫片 → 语料两例 + 单测全红)。
9. ~~**VARIANT 点号子列路径未建模**~~ — **已落地**。真实 4.1.4 上两种写法返回同一个值,现在由 catalog 的列类型作证后按路径建模,见 §方言事实;`variant_path_test.go`(13 + 6 + 1 例)与 `26_test_doris_forms_table.yaml` 的八例钉住,包括"无证据时仍是 unresolved"、"catalog 否证的限定列让位路径"与"前缀撞名别名"。StarRocks 侧**刻意不镜像**:SR 4.1 引擎不认点号(`Column 'v.user.name' cannot be resolved`),文档里的子列访问是函数式(`variant_query`/`get_variant` 取 JSONPath),而该函数形式已经通过通用表达式走查把整列记为来源(`starrocks/variant_access_test.go` 钉住);镜像会给 SR 引擎拒绝的语句产边。
10. ~~**限定读法被 catalog 否证、但没有任何可用路径时,仍保留旧边**~~ — **已落地**:限定列改为按 registry 校验(见 §方言事实第 1 行与不变量 9),`FROM ev7, ev8 v; SELECT v.name` 现在报 `unresolved reference in an output column: v.name`(引擎:`v.name is ambiguous: v#1, v#3`),同一规则也覆盖没有 VARIANT 参与的同族形态(输出列 / 输出目标 / 赋值 / DELETE 条件 / 谓词 / CTE 体 / 生成器实参,引擎均报 `Unknown column 'nope' in 'ev2'`)。`qualifiedColumnIsAbsent` 随之删除,判断下沉到 `scope.ResolveColumnRefs`;`qualified_column_test.go` 钉住十种上下文、边界四种放行情形、查询本地关系不校验与该残留用例。
11. ~~**两种混合写法是 omni 缺口**~~ — **omni 缺口仍在,分析器已恢复**。`v['user'].name` 与 `v['user']['name'].x`:真实 4.1.4 都接受(前者与 `v['user']['name']` 同值),omni 在 `.` 处报语法错误(§12.4)。`mixedpath.go` 把字符串下标后的 `.段` 改写成 `['段']` 后重解析;改写保留原文并据此命名表达式,所以 `SELECT v['user'].name FROM ev` 产出的目标列名与引擎一致(`__result__.v['user'].name`)。**只改字符串下标**:`a[1].b`(数组元素上的字段)括号形式语义不同,保持硬失败并已用 `expect_error` 的语料钉住。`mixedpath_test.go` 与语料三例(名字、INSERT 目标、拒绝)钉住;变异验证(禁用改写 / 禁用原文命名 → 对应用例分别变红)。
12. ~~**表值函数暴露的列名是启发式**~~ — **已落地**:单个命名实参的 key 就是暴露的列名(`numbers("number"="2")` → `number`,引擎实测 `u.number` 可用、`u.numbers` 报 Unknown column),其余形态保留函数名占位(`tableFunctionColumn`,由 `table_function_test.go` 钉住)。**范围说明**:Doris 的 FROM TVF 实参只能是字面量,所以这个列名对合法 SQL 没有可观测的血缘影响;真正读列值的是 LATERAL VIEW(它自己声明列名)。该启发式只用于**分析器自己的**占位与无限定名解析,查询本地关系不参与限定列校验(见 §方言事实第 1 行),所以它猜错也不会制造假缺口。
13. ~~**CTE 的暴露列取自 lineage target,而不是查询的输出别名**~~ — **已落地**:`processCTE` 现在把校验后的暴露列写回 `definition.Columns`(`scope.ExposedColumnNames(cte.Columns, bodyOutputs)`,与 PostgreSQL 同一写法),于是引用 CTE 的列按 body 真正暴露的名字解析,`WITH c AS (SELECT 1 AS x, k FROM ev2) SELECT x FROM c, ev2`(4.1.4 实测可执行)不再报 `unresolved reference`;真正不存在的列仍是缺口,声明清单仍按位置改名。`cte_exposure_test.go` 钉住四种形态,变异验证(去掉那一行 → 常量列用例变红)。**仍未做**:收窄仍把一切查询本地关系排除在校验外,所以 CTE 上引擎拒绝的列(`SELECT c.nope FROM c`)继续静默;清单已经可靠了,剩下的不可靠来源是表值函数占位与 `outputColumnAliases` 的完整性守卫,要让 CTE 参与校验得先给 `SetColumnLookup` 加"语句声明 / 分析器推导"的出处标记(标错会造成更坏的假缺口)。**该出处标记方案已放弃**:它的唯一收益是为"引擎拒绝的语句"报缺口,而本产品的输入(库内视图 / MV、ETL 语句)几乎总是可执行,对合法 SQL 它既不多给一条边、也只增加假缺口风险(研判见 [lineage-semantics.md](lineage-semantics.md) open item 4)。其他方言同一缺陷登记在该篇 open item 8,**按决定延后**。另:同一收窄也让 LATERAL VIEW 的 `AS` 列表不再参与校验——它的清单本属语句字面、引擎也认,所以引擎拒绝的引用(`SELECT u.nope FROM ev LATERAL VIEW explode(arr) u AS e`)现在静默;这是"不询问查询本地关系"的代价,同样登记在 open item 4。

14. **被读到的 `WITH … UPDATE / DELETE` 仍是硬失败** — 引擎执行它(`WITH c AS (SELECT max(k) AS m FROM ev2) UPDATE t2 SET b = (SELECT m FROM c)` 4.1.4 上真的把 b 改成 max(k)),omni 在 `UPDATE` 处报错,而 WITH 在 UPDATE/DELETE 之前没有可放的位置(INSERT 没有这个问题:查询自己带着 WITH)。当前只在语句**没读到**该名字时删除子句(安全、可证),读到的保留硬失败。要覆盖剩下的情形得把被引用的 CTE **内联**成子查询——那需要判断"哪些 token 是把 CTE 当关系或限定符用",而不是把同名标识符误替换(列名、别名都可能撞名),属独立改动;语料用 `expect_error` 钉住当前边界。

## Where things live

| 关注点 | 代码 | 测试 / 门禁 |
| --- | --- | --- |
| 方言包 | `backend/plugin/lineage/doris/{analyzer,expr,predicate,rawsql,viewbody,outfile,lateral,mixedpath,withdml,tokenname}.go` | `go test ./backend/plugin/lineage/doris/` |
| 装配 | `backend/plugin/lineage/engines/engines.go`、`engines_test.go` | `go test ./backend/plugin/lineage/engines/` |
| 共享机制守门 | `backend/plugin/lineage/shared_analysis_test.go`(`dialectPackages` 含 `doris`) | `go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared` |
| 黄金语料 | `backend/plugin/lineage/doris/testdata/analyze/`(21 套 / 224 例 / 451 条预期边 —— 含 34 条以 `from_database` 开头的跨库边;其中 6 例是 `expect_error`) | `TestAnalyzeYAML`、`TestCorpusIsFullyAnnotated` |
| 单语句契约与硬失败 | `hardfail_test.go`(`TestHardFailOnUnparseableSQL`、`TestRejectsMultiStatement`) | 同上 |
| 缺口边界 | `analyzer_test.go`(`TestUnmodelledStatementsAreGaps`) | 同上 |
| catalog 读取 | `catalog_lookup_test.go`(`TestCatalogIsReadOncePerRelation`、`TestCatalogFailureIsReportedAsAGap`) | 同上 |
| LATERAL VIEW 与表值函数的映射 | `analyzer.go`(`processLateralViews`、`addGeneratedRelation`、`processTableFunction`) | `generator_lineage_test.go`(`TestLateralViewGeneratorLineage`、`TestTableValuedFunctionLineage`);`testdata/analyze/23_test_query_local_name_lineage_table.yaml` 三例 |
| CTAS 目标列名 | `analyzer.go`(`ctasColumnNames`) | `generator_lineage_test.go`(`TestCTASTargetColumnsAreTheDeclaredNames`) |
| VARIANT 子列路径 | `expr.go`(`collectColumnSources`、`variantPathBase`、`variantBase`、`isVariantType`) | `variant_path_test.go`(13 + 6 + 1 例);`26_test_doris_forms_table.yaml` 八例(含 typed catalog fixture) |
| Doris 独有语法 | 通用表达式走查 + `SelectStmt.GroupByWithRollup` + `groupByKeys`(分组集展平成列、重复键去重) | `testdata/analyze/26_test_doris_forms_table.yaml` 二十八例(含 `ROLLUP` / `CUBE` / `GROUPING SETS` 三例与重复分组键一例) |
| 视图体的抽取 | `viewbody.go`(`extractViewDDL`)、`viewbody_test.go` | `go test ./backend/plugin/lineage/doris/` |
| MV 定义来源 | `backend/plugin/db/starrocks/sync.go`(`mv_infos("database"=?)`)、`backend/runner/lineageanalyzer/analyzer.go`(`buildSQL`) | [starrocks-doris-driver.md](starrocks-doris-driver.md) |
| 端到端 | `test/e2e_etl/doris/*.sql`、`test/e2e_etl/dags/e2e_07_pg_to_doris.py` / `e2e_08_doris_transform.py`、`test/e2e_etl/tools/datax_lineage_doris.py` | `test/e2e_etl/run_chain.sh` + `verify_chain.sql` §23-32;证据见 `test/e2e_etl/FINDINGS.md` |
| 上游 AST 差异记录 | 本篇 §方言事实 | — |

不在本篇范围:transformation 模型的字段含义见 [lineage-semantics.md](lineage-semantics.md);装配与 runner 生命周期见 [lineage-analyzer.md](lineage-analyzer.md);上游缺陷与不修改政策见 [omni-upstream-defects.md](omni-upstream-defects.md)。
