# 应用 SQL 摄入与血缘(Application SQL Ingestion)— 设计方案

> 状态:**设计稿,未实现**。本文件描述的是待建子系统,不是已交付行为。实现落地后应把本文件改写成维护参考,并在 [README.md](README.md) 的索引表里加一行——现在不加,以免索引表宣称一件并不存在的东西。
>
> 关联:[lineage-analyzer.md](lineage-analyzer.md)(分析器装配与 runner 生命周期)、[lineage-semantics.md](lineage-semantics.md)(边与 transformation 的语义)、[openlineage-lineage.md](openlineage-lineage.md)(另一条外部摄入路径)、[rate-limiting.md](rate-limiting.md)(本仓库对机器摄入的既有做法)。

## 0. 结论

想法成立,而且与仓库里已有的两条路径同构:manual SQL 提供"SQL 文本 → 血缘",OpenLineage 提供"外部系统 → 摄入 → 血缘"。应用 SQL 是这两者的交集:像 OpenLineage 一样由外部系统主动推送,像 manual SQL 一样以 SQL 文本为血缘来源。改动集中在新增一条摄入链路,不需要改分析器语义,也不需要改图的读取模型。

但有三个必须先正面处理的事实,它们决定了方案的形状。三条都是实测或读码得出,不是推测;其中第 1、3 条已经结案,第 2 条引出对 D1 的修订。

### 事实 1:`?` 是 omni MySQL 语法的缺口 —— 但不需要改 omni,在服务端替换掉即可

**先定位责任**:报错来自 omni 的 MySQL **parser**,不是我们的血缘分析器。我们的代码只包装了它(`backend/plugin/lineage/mysql/analyzer.go:165`:`errors.Wrap(err, "failed to parse MySQL SQL")`)。

直接调上游、绕过我们的分析器,证据是:

| | 词法 | 语法 |
| --- | --- | --- |
| MySQL | `SELECT ? FROM t` 的 lexer 产出 **token type=63, str="?"** —— `?` 是合法 token,不是非法字符 | `Parse` 报 `syntax error at or near "?"`;`mysql/ast` 里**没有任何占位符节点类型**,`mysql/parser` 里**没有任何接受该 token 的产生式** |
| PostgreSQL | `SELECT $1` 的 lexer 产出 token type=815 | `$1` 是 `pg/ast.ParamRef`,有产生式,分析器把它当叶子 |
| StarRocks | 自己的 parser | 接受 `?` |

所以这是上游 MySQL 语法的缺口:**token 有,产生式没有**。按仓库既定政策我们不 patch、不 fork、不 vendor,也不向上游反馈([omni-upstream-defects.md](omni-upstream-defects.md));这里同样不联系上游。

**但这条不需要上游修。** 血缘只关心列的派生,而占位符是一个"值",值从不参与列级派生。把 `?` 在送分析前换成一个中性字面量,结果与真实字面量一致。

第一次抽查(20 个常见占位符位置)时三种字面量看不出差别,`NULL` 全部通过;但那批形状里没有几个"语法强制要求字符串"的位置。P0 把 642 条语料跑过一遍之后,缺口立刻就露出来了——所以下面给的是**补全后的结论**,不是那次抽查的结论:

| 位置 | `NULL` | `0` | `$n` | 字符串 |
| --- | --- | --- | --- | --- |
| MySQL `LOAD DATA INFILE …` / `FIELDS TERMINATED BY …` | ✗ | ✗ | — | ✓ |
| MySQL `JSON_TABLE(… , '<path>' COLUMNS (x INT PATH '<p>'))` | ✗ | ✗ | — | ✓ |
| PostgreSQL `COPY … FROM '…'` | ✓ | ✗ | ✗ | ✓ |
| PostgreSQL `… - INTERVAL '1 year'` | ✗ | ✗ | ✗ | ✓ |
| StarRocks `COPY INTO t FROM '…'` | ✗ | ✗ | — | ✓ |
| 其余全部位置(20 个抽查形状 + 语料里的其余部分) | ✓ | ✓ | ✓ | ✓ |

**任何字符串字面量都通过,`NULL` / `0` / `$n` 各有硬缺口。** 于是中性字面量取 `'?'`:它在语法上就是"一个字符串字面量",通吃所有位置;`'?'` 而不是 `''`,是为了让派生出来的列名读起来像参数(§6.2、§13.3)。

由此得到三条设计结论:

- **无需任何 omni 改动**,也无需 `replace` / `vendor` / 上游沟通。替换是服务端自己的事。
- 存储文本与送分析文本是两种形态(§4.2),替换是必需环节而不是优化——不替换,MySQL 家族的应用 SQL 会被"解析失败不落库"整类丢弃。
- token type 63 的存在反而是好事:`?` 是一个**独立 token**,所以"字符串字面量里的 `?`"与"注释里的 `?`"天然不会被误替换——由上游 lexer 区分,不靠我们猜。

**P0 把这个"中性字面量"实测到一个字**:任何**字符串字面量**都能解析,而 `NULL` 与 `0` 不能。失败位置都是语法强制要求字符串的地方,换成字符串就好:

| 位置 | `NULL` | `0` | `$n` | 字符串 |
| --- | --- | --- | --- | --- |
| MySQL `LOAD DATA INFILE … ` / `FIELDS TERMINATED BY …` | ✗ | ✗ | — | ✓ |
| MySQL `JSON_TABLE(… , '<path>' COLUMNS (x INT PATH '<p>'))` | ✗ | ✗ | — | ✓ |
| PostgreSQL `COPY … FROM '…'` | ✗ | ✗ | ✗ | ✓ |
| PostgreSQL `… - INTERVAL '1 year'` | ✗ | ✗ | ✗ | ✓ |
| StarRocks `COPY INTO t FROM '…'` | ✗ | ✗ | — | ✓ |
| 其余全部位置 | ✓ | ✓ | ✓ | ✓ |

于是**三套方言派生塌成一条规则**:每个 `?` token 换成同一个字符串字面量。取 `'?'` 而不是 `''`,是为了让派生出来的列名读起来像参数(`price * '?'`)而不是像 bug(`price * ''`)——这一层影响见 §6.2 的命名说明。

顺带纠正一个此前的说法:`$n` 对 PostgreSQL **并非**更优。它在 `INTERVAL` 与 `COPY` 两个位置被拒,而 `'?'` 通吃,所以 §4.2 里"PG 换成 `$1…$n`"的做法被这条实测推翻了。

### 事实 2:零关系是设计,不是缺陷;要分开处理"关系落在临时目标上"与"根本没有关系"

上面那版的框定不准:列级派生是分析器的设计目标,不是它的短板。真正要区分的是两类形状。

| 形状 | relations | 目标 | 现有 runner 的处理 |
| --- | --- | --- | --- |
| `SELECT a FROM t` | 1 | `__result__.a`(临时) | 丢弃(`if rel.IsTemp { continue }`) |
| `INSERT INTO t … SELECT x FROM s` | 2 | `t.x`(真实) | 保留 |
| `UPDATE t SET a = 1` | 1 | `t.a`(真实) | 保留 |
| `DELETE FROM t WHERE a = 1` | 1 | `t.__deletion__`(真实表) | 保留 |
| `INSERT INTO t (a, b) VALUES (?, ?)` | **0** | —— | 无从处理 |

**(a) 关系落在 `__result__` 上(读语句)——可以拿到,而且机制已经存在。** 就在 manual SQL 那条分支里(`backend/runner/lineageanalyzer/analyzer.go:334-370`):它把每条关系的 target 从临时的 `__result__` **重定向到语句自己的 registry GUID**:

```go
SourceGUID:   srcGUID,          // 真实来源表
TargetGUID:   metaGUID,         // 语句自身 —— 一个非临时的 registry 对象
TargetColumn: rel.Target.Name,  // 原本是 __result__ 的列名
```

于是 manual SQL 里一条 `SELECT a FROM t` 会写出 `t.a -> <manual_sql>.a`,manual SQL 因此作为节点出现在图上——[lineage-analyzer.md](lineage-analyzer.md) 把这条特例记作"让'这段 SQL 输出什么'在图上可见"。`__result__` 只是在**分析器模型里**临时;runner 把它换成了语句节点这个非临时对象。把这套分支原样用在 `APPLICATION_SQL` 上,读语句就拿到了你要的血缘。

**但这与 D1 冲突**:D1 是"只读语句不产生边",而"读语句也要建节点"正是这条特例的效果。已修订为 **D1'**(§2)并确认。

**(b) 零关系(常量写入)——没有 target 可重定向。** 分析器按设计不产出任何东西,所以要回答"`INSERT INTO t (a,b) VALUES (?,?)` 写了哪张表",只能另找来源。见 §6.4。

### 事实 3:这个端点不进 `audit_log`

已确认:`audit_log` 永久保留(根 [AGENTS.md](../../AGENTS.md)),而现有 OpenLineage 摄入每个请求写一条审计行(`auditIngestion`,`backend/api/v1/openlineage_handler.go:690`;见 [rate-limiting.md](rate-limiting.md) 第 80 行)。应用上报是高频机器流量,且这类自动化请求不是审计要回答的对象——**逐请求审计会让 `audit_log` 的行数由请求次数决定,直接毁掉"落库不会很多"这条性质**。

因此这个端点**不逐请求审计**(**D8**),改为在 key 行上累计聚合计数(§5.5);人的动作(建应用、签发/撤销 key)照旧走 ConnectRPC 的 `audit` 注解。

## 1. 与现有两条路径的关系

| | manual SQL | OpenLineage | 应用 SQL(本方案) |
| --- | --- | --- | --- |
| 谁写入 | 人在界面上录入 | 外部调度器推送事件 | 应用自动上报参数化 SQL |
| SQL 来源 | 用户输入 | 事件的 `sql` facet | 上报 payload |
| 是否分析 SQL | 是 | 是(不信 producer 的列级 facet) | 是 |
| 表级实体 | `manual_sql` 表 + registry 镜像 | `openlineage_run` + 数据集 | `application_sql` 表 + registry 镜像 |
| 图上形态 | **节点**(表→语句→表) | **不是节点**,边落表↔表,靠 origin 区分 | **节点**,与 manual SQL 同形(读:`表→语句`;写:`表→语句→表`) |
| 频率类元数据 | 无 | run 的 event_time | **first/last seen + 执行次数**(两级) |
| 凭据 | 人登录,ConnectRPC | API key(`openlineage_api_key`) | **API key,key ↔ 应用一对一**(见 §2) |

三条路径共用同一个进程级 `*lineage.Analyzer`(`backend/server/server.go:107`),不做第二个实例——这条不变量照旧。

## 2. 已对齐的决策

本次问答得到的九条,连同我据此推出的推论。每条都标注了它的代价,便于日后回溯。D1' 是对 D1 的修订(见 §0 事实 2a)。

| # | 决策 | 推论与代价 |
| --- | --- | --- |
| D1' | **每条语句都在图上建节点**(修订自 D1):读语句写 `来源表 → 语句`,写入型再加 `语句 → 目标表`——即 manual SQL 那条分支的形状,含把 `__result__` 重定向到语句自身(§0 事实 2a) | 这是你要的"捕获非临时 `__result__`"。代价:应用语句数即图上节点数,可能上千;缓解手段是第三类 origin 可以一键从图例里关掉(§6.5),且语句是去重后的(同一指纹只一个节点)。仍需要 `kind` 列给列表页筛"读/写" |
| D2 | **双保险脱敏**:应用侧按契约替换字面量,服务端再跑一次参数化兜底,只保存服务端结果 | 原始 SQL 永不落库。接口对应用是契约,对服务端是强制——不依赖应用守约 |
| D3 | **两级聚合**:语句表按(实例, 库, schema, 指纹)唯一;观测表按(语句, 应用)记 first/last seen 与次数 | 观测表不需要再带实例/库/schema——那些已经在语句行上。见 §3.4 |
| D4 | **专用 REST+JSON 批量端点**,按应用签发的 API key 认证 | 不复用 OpenLineage 通道(会把两个概念混在一起),也不做 ConnectRPC 服务(非 Go 应用接入成本高)。HTTP 状态码与逐项结果见 §5.3 |
| D5 | **指纹尽量激进**:字面量→占位符、`IN` 列表折叠、常量表达式折叠、空白/注释归一 | 这是 D6"不设上限"能成立的前提。规则必须写死并带版本号,见 §4.3 / §4.4 |
| D6 | **不设存储上限、不做限流**;解析失败的 SQL 不落库,只 `WARN` 记录 | 前提是 D5 的归一化足够激进,且失败路径永不落库(§5.5)。残余风险见 §11 |
| D7 | **key 必须人工创建签发**,key 与应用一对一;没有 key 的请求直接拒绝 | 创建 key 即注册应用,不做"首报自动注册"。**应用身份取自 key,不取自 payload**——payload 里的应用名被忽略,一个 key 无法冒充另一个应用 |
| D8 | **这个端点不逐请求写 `audit_log`**;改为 key 上的聚合计数 | 应用上报是高频机器流量,逐请求审计会让永久保留的 `audit_log` 行数由请求次数决定,毁掉"落库不会很多"这条性质(§0 事实 3)。人的动作(建应用、签发/撤销 key)照旧审计 |
| D9 | **零关系写入的目标表由服务端自己取**(在那条窄分支上用 omni 读 DML 目标关系,写一条表级出边) | 不给应用加负担,不动分析器契约。P0 实测:真实触发面小得多——只需 INSERT 与不带列的 DELETE,三个方言族合计约几十行。见 §6.4 / §13 |

## 3. 数据模型

四张表。前两张是"谁在发",后两张是"发了什么、发了多少次"。

### 3.1 `application` — 应用实体

| 列 | 说明 |
| --- | --- |
| `id BIGSERIAL PK` | |
| `application_id TEXT UNIQUE` | 服务端生成的可读 id,作为 AIP 资源名末段 |
| `name TEXT NOT NULL` | 展示名 |
| `description TEXT` | |
| `disabled BOOLEAN NOT NULL DEFAULT FALSE` | 停用即拒绝其 key,但保留历史 |
| `created_by TEXT` / `created_at` / `updated_at` | |

**应用不是 meta registry 对象,没有 MetaType,没有 GUID。** 它是纯粹的归属标签,像 OpenLineage 的 namespace mapping 一样只是普通表行:它没有 schema、没有列、没有血缘,把它塞进 `meta_registry_resource` 只会污染那张表的语义。

### 3.2 `application_sql_api_key` — 摄取凭据

照抄 `openlineage_api_key` 的形状(`backend/migrator/migration/LATEST.sql:518`),把 `scope_namespace` 换成 `application_id`,并加上 §5.5 的聚合计数(**P1 已落地**,见 §3.7)：

| 列 | 说明 |
| --- | --- |
| `id BIGSERIAL PK` | |
| `application_id TEXT NOT NULL REFERENCES application(application_id) ON DELETE CASCADE` | 每个 key 只绑定一个应用(D7)。引用**公开 id** 而不是 `application(id)`,这样 store 的每一处都按同一个 id 寻址应用。**不加 `UNIQUE`**:轮换期需要新旧 key 并存,所以是"多 key → 一应用" |
| `key_hash TEXT NOT NULL` | bcrypt,真正决定接受与否 |
| `key_digest TEXT NOT NULL` | 明文 key 的 SHA-256,带唯一索引,只用于**查行** |
| `masked_key TEXT NOT NULL` | 展示用,`xxxx…last4` |
| `description`, `created_by`, `created_at`, `revoked_at` | |
| `last_used_at TIMESTAMPTZ` | |
| `last_report_id TEXT NOT NULL DEFAULT ''` | 见 §5.3 的重试幂等;空串表示还没有接受过任何上报 |
| `request_count`, `statement_accepted_count`, `statement_rejected_count BIGINT NOT NULL DEFAULT 0` | §5.5 的聚合计数,替代逐请求审计行 |

**`key_digest` 是 P1 写实现时改回来的**(原设计说不要它):当初的理由只想到限流器,漏了**查行**。没有这一列,每个请求都要对**所有**未撤销的 key 逐个跑 bcrypt——而 bcrypt 是故意慢的。所以它回来,职责和 `openlineage_api_key` 那一列一样:摘要负责找到唯一那一行,bcrypt 只对那一行做一次比较。它不是密码哈希(SHA-256 over 256 位随机 key,不可爆破),也不扩大泄漏面——库里本来就有 bcrypt 哈希。

凭据与 OpenLineage 的**不复用、不互通**:key 表不同、校验路径不同,两个凭据空间互不相交——一个泄漏的 OpenLineage key 写不了应用 SQL,反之亦然。HTTP 头沿用标准的 `Authorization: Bearer`(§5.1),分离由凭据本身保证,不靠改头名。

### 3.3 `application_sql` — 语句(第一级)

| 列 | 说明 |
| --- | --- |
| `id BIGSERIAL PK` | |
| `guid TEXT` | meta registry GUID,`BuildMetaGUID(instance, database, schema, "__application_sql__/"+application_sql_id)` |
| `application_sql_id TEXT NOT NULL` | 服务端生成的不透明 id,资源名末段 |
| `instance_resource_id TEXT NOT NULL` | |
| `database_name TEXT NOT NULL` | |
| `schema_name TEXT NOT NULL DEFAULT ''` | MySQL 家族可为空;PG 类必填(§5.2) |
| `engine INT2 NOT NULL` | 冗余自实例,便于按引擎筛与取分析器;实例引擎变化时刷新 |
| `fingerprint TEXT NOT NULL` | sha256 hex,§4.4 |
| `fingerprint_version INT2 NOT NULL` | 归一化规则版本;规则变更时靠它做重算 pass(§9.3) |
| `sql_text TEXT NOT NULL` | **参数化后的展示文本**,`?` 占位符形态。库里永远只有这一种文本 |
| `kind INT2 NOT NULL DEFAULT 0` | `UNKNOWN` / `READ` / `WRITE`,由 runner 分析后回写(§6.1);只是给列表页筛选用的一份缓存,不是判定来源 |
| `execution_count BIGINT NOT NULL DEFAULT 0` | 所有应用求和(可由观测表重算) |
| `first_seen_at TIMESTAMPTZ NOT NULL` | 所有应用中最早的 |
| `last_seen_at TIMESTAMPTZ NOT NULL` | 所有应用中最晚的 |
| `content_search TEXT NOT NULL DEFAULT ''` | 检索文本(就是 `sql_text`),供 §7.1 的文本检索 |
| `search_vector TSVECTOR NOT NULL DEFAULT ''::tsvector` | `to_tsvector('simple', content_search)`,带 GIN 索引;照 `manual_sql` 的形状 |
| `created_at`, `updated_at` | |

唯一键:`UNIQUE (instance_resource_id, database_name, schema_name, fingerprint)`;另有 `guid`、`application_sql_id` 两个唯一索引。

**外键只到实例,不到库**:`instance_resource_id` 引用 `instance(resource_id)`(实例与库都是软删,不会真的级联),但 `database_name` **不**引用 `db`。理由是应用经常在元数据同步覆盖到它的库之前就开始上报,要求 `db` 行已存在会让上线期的上报整批失败。

**为什么语句身份里带实例**:血缘 GUID 是 `instance;db;schema;object` 的形状,同一个 `sales.orders` 在两个实例上是两个对象,分析作用域(instance/database/schema)也必须逐实例确定。把实例拿掉只会在图上把两个实例的对象错并成一条。

**为什么语句身份里带 schema**:PG 下 `public.orders` 与 `archive.orders` 是不同对象。MySQL 家族 schema 恒为空,不影响。

### 3.4 `application_sql_observation` — 观测(第二级)

| 列 | 说明 |
| --- | --- |
| `application_sql_id BIGINT NOT NULL REFERENCES application_sql(id) ON DELETE CASCADE` | 语句是内部对象,按行 id 关联 |
| `application_id TEXT NOT NULL REFERENCES application(application_id) ON DELETE CASCADE` | 应用按公开 id 关联,于是删应用会级联带走它的观测 |
| `first_seen_at`, `last_seen_at TIMESTAMPTZ NOT NULL` | 该应用自己的首末时间 |
| `execution_count BIGINT NOT NULL DEFAULT 0` | 该应用自己的次数 |
| `updated_at` | |

唯一键:`UNIQUE (application_sql_id, application_id)`。

这里相对 D3 的原话做了一处**简化并明确说明**:观测表只按(语句, 应用)两级,不再重复实例/库/schema,因为语句行已经唯一确定了那三者。若日后要支持"同一指纹在不同作用域出现",那是语句行多一行,而不是观测表多一维。

`application_sql.execution_count` / `first_seen_at` / `last_seen_at` 是观测表的**汇总**,在同一事务里维护;一条语句被删或观测被级联删除时重算。汇总冗余的理由:列表页默认按"热度/最近"排序并分页,每次都去 `SUM` 观测表会在最热的查询上做聚合。

### 3.5 与 `meta_registry_resource` 的关系

- **每一条语句都有 registry 镜像行**(`MetaType_APPLICATION_SQL`),不论读还是写。理由:镜像行是"这个对象在系统里存在"的唯一凭证,GUID 寻址的读取(`GetMetadata`、`ListMetadataHistory`、历史 diff、Explain SQL)全部依赖它;只给写入型建镜像会让只读语句在除列表页以外的每个入口都变成"不存在的对象",并逼出一套按资源名寻址的平行 API。
- **写入与只读的区别只在于有没有出边**(D1')。读语句写 `来源表 → 语句`;写入型再多一条 `语句 → 目标表`。两者都有节点——区分读写不靠"建不建镜像",也不靠"建不建节点"。
- 因此 `meta_registry_resource` 的行数等于**不同语句数**,和 `application_sql` 同阶;它不会随执行次数或请求次数增长。
- `getNextLevelObjectType`(`backend/store/meta_resource.go:563`)**不添加** `APPLICATION_SQL`——镜像行存在不等于要在元数据浏览器树上列出来,否则每个 schema 下会挂上成百上千个语句条目。浏览入口是专用的"应用 SQL"页面。
- `GetMetadataSchema`(`backend/api/v1/database_metadata.go:129` 的 MANUAL_SQL 分支)需要为 `APPLICATION_SQL` 加一个分支,返回 `sql_text`,让"看定义"和"Explain SQL"照旧可用。

### 3.6 元数据类型与枚举

- `proto/store/store/meta.proto` 的 `MetaType` 加 `APPLICATION_SQL = 19`(13/14/15 是 reserved,16/17 已给 EXTERNAL_TABLE / EXTERNAL_DATASET,18 是 MANUAL_SQL,100 是 OPENLINEAGE,19 空闲)。
- `proto/v1/v1/database_service.proto:1495` 的 v1 侧枚举同步加一项;`proto/store` 的 `StoredMetadata` 加 `ApplicationSQLMetadata{application_sql_id, name, sql_text, instance_resource, database_name, schema_name}` —— 镜像 manual SQL 的 `ManualSQLMetadata`。
- 生成物按 `cd proto && buf generate` 重新生成,不手改。

## 4. 脱敏与参数化:`backend/plugin/sqlsanitize`

新包。它是本方案唯一真正需要新算法的部分,单独成包、单独成语料测试,不让血缘包承担它。

### 4.1 线上契约:占位符统一写成 `?`

上报 payload 不论方言,占位符一律是 `?`。理由:MySQL 用户熟悉它,ORM 与各种驱动都能产出它,不需要应用知道"我们后端在 PG 上用 `$n`"。方言差异由服务端在派生送分析文本时补上。

### 4.2 一条语句的三种形态

| 形态 | 内容 | 存在哪 / 用在哪 |
| --- | --- | --- |
| **请求文本**(应用发出) | 应用产出的、字面量已换成 `?` 的文本 | 只在请求里,不落库 |
| **存储文本** `sql_text` | 服务端 `Sanitize` 的输出:`?` 占位符、注释已删、**其余字节原样保留** | **落库的唯一文本**,面向人展示、Explain SQL、指纹的输入 |
| **送分析文本** | 把存储文本里的每个 `?` token 换成一个字符串字面量 `'?'`;**一条规则,不分方言** | 不落库,只在 runner 分析时现算(§6.2) |

第三种的必要性来自 §0 事实 1。它在 `buildSQL`(`backend/runner/lineageanalyzer/analyzer.go:421`)的 `APPLICATION_SQL` 分支现算——runner 已经在按 GUID 取语句时知道 `engine`,加一次 `sqlsanitize.Analyzable(engine, sqlText)` 即可,**不落冗余列**,避免两份文本各自过期。

**存储文本保留原始空白**(P0 修正):`sql_text` 只在字面量处做定点替换、只删注释,不再做空白归一。原因是分析器把"未加别名的表达式列"命名为其源文本,而那条名字会进 `column_lineage`——空白归一让 `CONCAT(name, ' x')` 变成 `CONCAT ( name , '?' )` 这种带空格的名字(§6.2)。空白归一改为只作用于 `canonical`(指纹输入),它本来就不给人看。

### 4.3 规范化规则

产出 `sql_text`(展示/存储)与 `canonical`(指纹输入)两串。规则编号固定,便于写测试与日后回溯。

| # | 规则 | 作用于 |
| --- | --- | --- |
| N1 | 每个字面量(整数、小数、字符串、十六进制、位串、布尔、日期时间字面量)→ 一个 `?` | 两者 |
| N2 | 注释删除 | 存储文本只删注释、其余空白**原样保留**;`canonical` 另把 token 之间归并为单个空格 |
| N3 | 标识符**原样保留**(含大小写与引号形态),不折叠、不改引号 | 两者 |
| N4 | `IN` / `NOT IN` 列表中 ≥1 个占位符 → 折叠成单个 `?` | 仅 canonical |
| N5 | `VALUES` 里重复的值组折叠成一组(消掉批量大小差异) | 仅 canonical |
| N6 | 只含占位符、运算符、括号的最大表达式片段 → 折叠成单个 `?`(使 `1+1` ≡ `2`)。**逗号切开片段**,所以 `f(?, ?)` 不会塌成 `f(?)`;`IN (…)` 的逗号由 N4 单独处理 | 仅 canonical |
| N7 | PG 的参数编号 `$n` 在 canonical 里写成 `?`(使 `$1`/`$2` 不分裂指纹) | 仅 canonical |

**明确不做**:不改写标识符与函数名(不做大小写折叠、不加引号);不做真正的常量求值(不折叠 `CONCAT('a','b')` 与 `'ab'`,那需要求值器,收益不抵风险);不改语句顺序;不合并语义不同但文本相同的语句。关键字大小写归一(N8)可选,**建议推迟**:应用对同一条语句每次发的文本是固定的,关键字大小写只在跨应用去重时有影响,而收益不足以抵消"依赖各方言关键字表"的耦合。记录在 §11 待决。

N4–N6 正是字面量之外唯一会随执行批次漂移的差异(IN 元素个数、批量行数、常量算式写法),也就是 D5"激进"的实际内容。

### 4.4 指纹与版本

- `canonical` 经 sha256 得到 `fingerprint`(hex)。
- `fingerprint_version` 从 1 开始,随 N 规则变更递增。
- **规范化规则是破坏性变更**:改了 N4–N6,同一个语句的指纹就变了,同一行会分裂成两行。因此必须同时提供一次性的重算 pass(§9.3)。这条写进包文档与 `fingerprint_version` 的列注释里,不靠人记。

### 4.5 实现方式:上游 lexer 驱动,不是自己写词法

omni 已经把各方言的 lexer 导出了,带字节偏移:

| 方言 | 构造 | token |
| --- | --- | --- |
| MySQL 家族 | `mysql/parser.NewLexer(input)` | `Token{Type, Str, Ival, Loc, End}` |
| PostgreSQL | `pg/parser.NewLexer(input)` | 同上 |
| StarRocks | `starrocks/parser.NewLexer(input)` | 同上 |

用 `Loc`/`End` 做**源文本区间替换**,不碰 AST、不依赖 `nodeLoc` 那套反射(分析器里 `mysql/analyzer.go:264` 的注释已经说明 omni 没有位置接口,`Loc` 是具名字段,零值意味着"解析器没设")。选 lexer 而不是 AST 的理由:

- 需要精确的方言级容错——单双引号与反引号、反斜杠转义、`--` / `#` / `/* */` 注释、PG 的 dollar-quoting(`$$ … $$`)。这些规则上游已经实现且被上游测试覆盖,自己重写一遍就是重新引入一批分歧。
- 不需要 AST 改写,也就不需要依赖"字面量节点是否都设了 `Loc`"这个未经检验的前提。

**不需要上游的 token 类型常量**(P0 已解决)。判断"这个 token 是不是字面量"原本打算读 `tokSCONST` / `tokICONST` 一类的常量,但那些常量在 `mysql/parser` 与 `pg/parser` 里都是**未导出**的——依赖它们等于把魔法数字写进我们的代码,而上游一改编号就会静默失效。

改用**token 的源文本切片**判定:`Token` 带 `Loc`/`End` 字节偏移,`sql[Loc:End]` 就是原始文本,与上游怎么解码 `Str` 无关。于是规则变成纯形状判断:首字符是 `'` → 字符串;首字符是 `"` → MySQL 家族与 StarRocks 是字符串、PostgreSQL 是引用标识符;首字符是 `` ` `` → 引用标识符;首字符是数字 → 数值;首字符是 `$` → `$n` 是参数、`$$…$$` 是字符串;其余含 `'` 的字母开头 token → `x'..'`/`b'..'` 一类前缀字面量。

两个方言族的 lexer 行为已实测过:**注释根本不产生 token**,所以按 span 重建天然就把注释丢掉了,不需要我们识别注释语法。

StarRocks 略有不同:它的 `Token` 用 `Kind` + `ast.Loc{Start,End}`,并提供了 `Tokenize` 一次性入口与 `Errors()`;它是三者中唯一自己报词法错误的。

### 4.6 幂等性与不变量

- `Sanitize(Sanitize(x)) == Sanitize(x)`:语料测试逐例断言。它是"双保险"的前提——应用侧做过一次,服务端再做一次必须不变形。
- **落库文本永远是 `Sanitize` 的输出**,而不是请求里的文本。这条是安全不变量,在 handler 里只有一个写入口。
- `Sanitize` 失败 ⇒ 该上报项不落库(§5.5)。

## 5. 上报接口

### 5.1 端点与认证

```
POST /v1/applicationSql:report
Authorization: Bearer <application-sql-key>
Content-Type: application/json
```

- 普通 Echo 路由(与 OpenLineage 摄入同层,`backend/server/` 注册),不进 ConnectRPC 拦截器链。
- 认证:**只认这个头发。**缺头、头的格式不对、key 已撤销、密钥无效、应用已 `disabled` —— 一律 401,不进入处理。
- **应用身份取自 key,不取自 payload**。payload 里即便带了应用名也被忽略,响应里回显 key 所属的应用。这样 key 与应用的对应关系是服务端事实,不靠应用自觉。

### 5.2 请求

```json
{
  "instance": "instances/inst-abc",
  "database": "sales",
  "schema": "public",
  "reportId": "optional-opaque-id-for-retry-dedup",
  "statements": [
    {
      "sql": "SELECT o.id, o.total FROM orders o WHERE o.id = ? AND o.status = ?",
      "count": 128,
      "firstSeenAt": "2025-01-01T00:00:00Z",
      "lastSeenAt": "2025-01-02T00:00:00Z"
    }
  ]
}
```

| 字段 | 规则 |
| --- | --- |
| `instance` | 必填,`instances/{id}` 形状。必须是注册表里已存在的实例,否则整请求 400 |
| `database` | 必填 |
| `schema` | PG 类引擎必填;MySQL 家族可空(空即"无默认 schema") |
| `reportId` | 可选;见 §5.3 的重试去重 |
| `statements` | 必填,1..1000 项 |
| `sql` | 必填,单条语句;字面量应已替换为 `?` |
| `count` | 可选,默认 1;**表示本次上报窗口内的增量次数** |
| `firstSeenAt` / `lastSeenAt` | 可选,默认服务端收件时间;应用比服务端更清楚何时第一次执行 |

**`engine` 不由应用声明**,服务端从实例取。理由:血缘分析器按引擎注册,实例的引擎是注册表的事实;让应用声明就会引入"应用说的是 MySQL、实例是 TiDB"这类不一致。

**多语句脚本**:`sql` 里若含多条语句,服务端用根包的切分器(`lineage.go:126-141`)切开,**每条各自成为一行语句**,`count` 施加到每一条。接口文档要写明这一点,避免应用以为"一个上报项 = 一行记录"。

**请求体上限**:8 MiB,与 `maxOpenLineageBodySize` 一致;**每请求语句数上限 1000**,与 `maxOpenLineageBatchEvents` 一致。这两个是**请求形状**的边界,不是存储上限——它们只约束单次请求的内存与事务规模,不与你选的 D6 冲突。

### 5.3 响应

```json
{
  "application": "applications/app-1",
  "accepted": 12,
  "rejected": 2,
  "results": [
    { "index": 0, "status": "ACCEPTED", "guid": "inst-abc;sales;public;__application_sql__/01H…" },
    { "index": 1, "status": "REJECTED", "reason": "PARSE_ERROR", "detail": "syntax error at or near \"SELECT\"" }
  ]
}
```

- 逐项结果,而不是整请求成败:一个应用的批次里混着一条坏 SQL 时,好语句不该被连坐。
- `reason` 是枚举字符串:`PARSE_ERROR` / `EMPTY_STATEMENT` / `ENGINE_UNSUPPORTED` / `SCOPE_NOT_FOUND`。应用可以据此定位自己发错了什么。
- 整请求级别失败用 HTTP 4xx:401 凭据缺失/无效/已撤销/应用停用、400 payload 形状不合法、404 实例不存在、413 超体、400 超批。
- **重试幂等**:`reportId` 与 `application_sql_api_key.last_report_id` 比较,相同则直接返回 200 且不再累计计数(只有最近一次,所以只挡"紧接的一次重试",挡不住乱序补发)。不带 `reportId` 的上报是**增量语义**,重试会重复计数。接口文档必须写明这一点。v1 接受这个近似:频率是元数据不是账单,少几次数不影响它要回答的问题。

### 5.4 服务端处理流程

```
认证 → 取实例与引擎 → 校验作用域
对每个 statement:
  1. 切分多语句(lineage.go:126-141 的根包切分器)→ 逐条走 2..6
  2. Sanitize(engine, 单条语句)        ← 词法失败 → REJECTED(PARSE_ERROR),WARN 日志
  2b. 闸门:Analyzable 后的文本必须被分析器接受,否则 REJECTED,WARN(见下)
  3. 计算 fingerprint
  4. upsert application_sql by (实例,库,schema,fingerprint)
     - 新行 → 生成 application_sql_id / guid
     - 已有行 → 更新 last_seen_at、累加 execution_count
  5. upsert application_sql_observation by (语句,应用)
     - first_seen_at 取最早,last_seen_at 取最晚,execution_count 累加
  6. 取汇总回写 application_sql;upsert meta registry 镜像
     —— 镜像与语句行同一事务,照 CreateManualSQL 的写法
事务提交后:
  7. QueueLineageAnalysis(guid, APPLICATION_SQL)
     —— 不在这里判读写:判定归 runner(§6.1),摄入只负责"语句存在"这件事
```

**摄入不判类型,但必须跑一次分析当闸门。** 类型判定只该有一处实现(runner),摄入不重复它;但 D6"解析失败不落库"要求摄入端自己知道这条语句能不能解析,所以摄入要跑一次分析并把结果丢掉。

这里有个 P0 跑出来的陷阱:**闸门不能用裸 parser。** StarRocks 的 `ALTER VIEW … SELECT … UNION ALL …` 被 `srparser.Parse` 拒绝,却被分析器**接受**——分析器有 `extractViewDDL` 兜底(`starrocks/analyzer.go:147`)。闸门若用裸 parser,会把一条能产出完整血缘的语句误判成"解析失败"而拒收。闸门与分析器必须同口径,也就是同一个 `*lineage.Analyzer.Analyze`。细节见 §13.6。

切分在 `Sanitize` 之前,和 `Analyzer.Analyze` 的次序一致——切分是词法扫描,不依赖解析,让它在最外层就不必为"注释里的分号"操两次心。

入队放在事务提交之后,是因为计数与元数据是"应用发了什么"的事实,不该因为分析器抖动而回滚。分析失败只影响图,不影响频率统计——与 [lineage-analyzer.md](lineage-analyzer.md) 的"解析失败清血缘、未建模留血缘 + 缺口"是同一套处理哲学。

### 5.5 不落库的分支,以及为什么这里不逐请求审计

不落库的分支穷举:

| 分支 | 处理 |
| --- | --- |
| 无 key / key 无效 / 已撤销 / 应用停用 | 401,不落库,`slog.Warn` |
| payload 形状不合法、实例不存在、作用域不合法 | 4xx,不落库,`slog.Warn` |
| 超体 / 超批 | 413 / 400,不落库 |
| 单条 SQL `Sanitize` 失败(解析不了) | 该条不落库,逐项 `REJECTED`,**`slog.Warn`**(D6 明确要求) |
| 单条 SQL 解析成了但没建模 | 正常落库;缺口交给血缘侧按既有机制处理 |

**审计**(与 [openlineage-lineage.md](openlineage-lineage.md) 的 OpenLineage 路径有意分歧):

- 审计**人的动作**:创建/停用应用、签发/撤销 key —— 走 ConnectRPC 的 `audit` 注解,与其余管理接口一致。
- 摄入请求**不逐请求写审计行**,改为在 `application_sql_api_key` 上累计:`last_used_at`、`request_count`、`statement_accepted_count`、`statement_rejected_count`。

理由就是 §0 事实 3:`audit_log` 永久保留,逐请求审计会让这张表的行数由请求次数决定,直接否掉 D6 想保住的性质。代价是失去了"某次具体请求发生了什么"的逐条记录——保留的是"这个 key 一共发过多少、被拒多少、最后何时用"。这是 **D8**,已确认。

代价的另一面:每个请求会多一次对 key 行的 `UPDATE`。它不增加行数,也不进永久账本,是刻意的交易。若日后这一步在写负载里显现出来,把它合并成"最多每分钟落一次"即可,不影响任何读路径。

## 6. 血缘接入

### 6.1 分类规则:读还是写(决定写不写 `语句 → 目标表` 那条边)

D1' 之后,分类**不再决定"有没有节点"**——每条语句都有节点。它决定的是要不要多写一条出边到真实目标表。

```
relations := analyzer.Analyze(engine, analyzableText)

写入型 ⟺ ∃ rel ∈ relations : !rel.IsTemp          // 有真实目标表
       或 (relations 为空 ∧ 能取出目标表)          // 常量写入的补丁,§6.4
只读型 ⟺ 其余
```

- 读 `SELECT` 的 target 是 `__result__`(`model.ResultTableName`),`IsTemp` 为真 ⇒ 只读型,**仍然建节点**,只是没有指向真实表的出边(实测:`SELECT a FROM t WHERE c = 1` 有 1 条关系、非临时 target 数 0)。
- `UPDATE` / `DELETE` 的 target 是真实表 ⇒ 写入型(实测各 1 条非临时 target)。
- `INSERT … SELECT` ⇒ 写入型(实测 2 条非临时 target)。
- `INSERT … VALUES` ⇒ 0 条关系,需要 §6.4 才知道写的是哪张表,否则被误判成只读。

**一条语句一条边都没有,就不会出现在图上**——图的节点集合由边推导。所以:

| 形状 | 靠哪条边出现在图上 |
| --- | --- |
| 读语句(含只读 SELECT) | 把 `__result__` 重定向到语句 GUID 的那条边(§0 事实 2a) |
| `INSERT … SELECT` / `UPDATE` / `DELETE` | 上面那条 + `语句 → 目标表` |
| 常量 `INSERT … VALUES` | 只能靠 §6.4 取到目标表后写的那条表级出边 |
| `SELECT 1`、`SET @x = ?` 等两端都拿不到 | **不进画布**:入库、列表页可见、`kind` 有值,但没有边 |

最后一行不是本方案新增的空洞:manual SQL 上一条 `SELECT 1` 同样不产生任何边、同样不进图。

**这条规则跑在 runner 里,不在摄入路径上**(§5.4)。`backend/runner/lineageanalyzer/analyzer.go` 的 `APPLICATION_SQL` 分支在 `Analyze` 之后按上面的判据决定要不要写 `语句 → 目标表`,并把 `READ`/`WRITE` 回写 `application_sql.kind`。于是:

- 判定只有一处实现,摄入路径不重复它,也不因为要判定而多跑一次分析器。
- 分类只决定"有没有出边",不决定"落不落库"——两类都落库、都有 `application_sql` 行与频率。
- 判定翻转(例如目标表被删后不再有非临时 target)时,分支按既有机制重写该对象的 `column_lineage`(照"解析失败清血缘"那条路径),`kind` 跟着回写。

### 6.2 边形状

所有语句沿用 MANUAL_SQL 的边形状(`backend/runner/lineageanalyzer/analyzer.go:334-370` 那段分支),只把 `MetaType_MANUAL_SQL` 换成 `MetaType_APPLICATION_SQL`:

| 边 | 何时写 | 含义 |
| --- | --- | --- |
| `源表.列 → 语句.列` | 每条关系都写 | 语句读到的列;`__result__` 这个临时目标在这里被换成语句自身 |
| `语句.列 → 目标表.列` | 仅当 `!rel.IsTemp` | 语句写下的列 |

于是图上出现 `t1 → [应用: <语句摘要>] → t2` 的链,与 manual SQL 节点同构;而一条只有读的语句表现为 `t1 → [应用: <语句摘要>]`。`guidLookupSet` 里额外补 `MetaType_APPLICATION_SQL` 的类型解析,和现有分支一致。

### 6.3 节点密度:这条修订最大的代价

D1' 的直接后果是**画布上多出等于"不同语句数"的节点**。三点让它不至于失控,但都要在 P5 落地:

- **去重**:同一指纹只有一个节点,不是每次执行一个。
- **origin 过滤器**:第三类 origin(`applicationsql`)在图例里可以一键关掉(§6.5),这是现成的机制,不是新造的。
- **默认值可以讨论**:默认画出还是默认隐藏,是一个 UI 默认值,与数据模型无关;`GetLineageGraphRequest` 已有 `lineage_type`、`depth` 这类参数,扩展一个来源过滤不需要改存储。

**语句详情页的"读写关系"从哪来。** D1' 之后,读语句在图上已经有边,所以详情页不再承担"补图的空缺"这个职责;它要的是**逐列**展开。做法是 `GetApplicationSQL` 在打开详情时**服务端现算一次**该语句的分析结果(用 `sqlsanitize.Analyzable` 译成送分析文本再调 `lineage.Analyzer.Analyze`),把列级关系列出来;一次人打开的抽屉 = 一次解析,代价可以忽略。

注意这里**不能直接复用 `AnalyzeSQL`**:那个 RPC 把 `sql_text` 原样交给分析器(`backend/api/v1/lineage_service_analyze.go`),而 MySQL 家族的存储文本带 `?`,原样送进去必然解析失败(§0 事实 1)。要么让 `GetApplicationSQL` 自己走 `Analyzable`,要么给 `AnalyzeSQL` 加一个"文本已参数化,请按方言翻译"的开关——前者不动既有 RPC 的语义,推荐前者。表详情页要列"读它的应用语句"则直接查 `column_lineage` 即可,不需要额外索引。

### 6.4 常量写入的目标表:服务端自己取(D9)

`INSERT INTO t (a, b) VALUES (?, ?)` 一条关系都没有(§0 事实 2b),所以既判不出它是不是写入,也画不出它指向目标表的出边。**已定:服务端自己取,不由应用声明。**

**什么时候跑。** 只在"分析器对这条语句返回 **0 条关系**"这一条窄分支上跑。其他形状分析器已经给出了目标表,不需要第二遍解析——不把解析变成主路径的一部分。

**取什么。** 用 omni 的 AST 读这条语句的目标关系:`INSERT [IGNORE] INTO <relation>`、`UPDATE <relation>`、`DELETE FROM <relation>`、`MERGE INTO <relation>`。拿到的名字通常是不限定的,交给既有的 `catalog.AnalysisContext.Complete` 补全成 GUID——与 `AnalyzeSQL`、runner 用的是同一条补全规则(`backend/plugin/lineage/catalog/provide.go`)。

**写成什么边。** 一条**表级边** `语句.guid → 目标表 guid`:

- 列名为空(`source_column` / `target_column` 都是 `''`);
- `Transformation: []`,于是 `RelationType` 按 `model.RelationTypeOf` 算出来是 `direct`(`model/relation.go:91`)。

这不是新形状,而是仓库里唯一的另一种表级边的写法——OpenLineage 的表级路径就是这么落库的(`backend/plugin/openlineage/processor.go:390`):

```go
buildColumnLineage(meta, sourceResolved, targetResolved, "", "", []model.Transformation{})
```

而它确实能被画出来,这一点已经**读码确认**,不再是待验项。`buildLineageGraph`(`backend/api/v1/lineage_service_graph.go:123`)的邻居发现只用两端 GUID:

```go
case edge.TargetGUID == node.guid && edge.SourceGUID != node.guid:
    peer, distance = edge.SourceGUID, node.distance-1
case edge.SourceGUID == node.guid && edge.TargetGUID != node.guid:
    peer, distance = edge.TargetGUID, node.distance+1
default:
    continue                       // 唯一被丢掉的是自环
```

列名只在 `columnLineageKey`(`:205`)里参与去重,从不参与"这条边算不算一个邻居"。`语句 → 目标表` 两端 GUID 不同,不是自环,两个节点都会被收进结果。

**范围比原先估的小得多**(P0 实测)。真实触发面只有两类,因为它们才真的是零关系:

| 形状 | 是否触发 |
| --- | --- |
| 常量 `INSERT`(各种形态)、`INSERT … SELECT <常量>`、PG `DEFAULT VALUES` | **是** |
| 不引用任何列的 `DELETE FROM t` / `DELETE … WHERE <常量>` | **是** |
| 任何 `UPDATE`(哪怕 `SET a = 1`、多表 JOIN 上的常量赋值) | 否——分析器给出 `t.* -> t.a` |
| 任何 DDL(`CREATE TABLE AS SELECT`、`CREATE VIEW`、`ALTER VIEW`) | 否——有真实目标表 |
| 多表 `UPDATE … JOIN` / `DELETE o FROM t o …` | 否——`nonTemp ≥ 1` |
| `TRUNCATE` | 是,但 D9 不认它是写入(它没有列血缘,也没有"写了哪些列"可言) |

实测 25 个触发面的形状 **25/25 全部取对**(见 §13)。所以这个助手只需要读 `InsertStmt` 与 `DeleteStmt` 的目标关系,不需要处理 JOIN 树、别名回解、DDL——**三个方言族合计几十行,不是各 60 行**。

**代码落点(已定):`backend/plugin/dmltarget/`**,与 `sqlsanitize` 并列的一个小包,不塞进 `lineage/` 那棵树——那里有一套针对分析器的规则(生成物、语料门禁),而这个助手不是分析器。不改分析器契约,不改语料契约,也不碰 omni。

一个 MySQL 特有的坑,已在实现时堵住:`DELETE o FROM orders o JOIN users u …` 这种形式把**别名**放进 delete 目标列表,`o` 不是表。规则是"带 `USING` 子句的 DELETE 一律拒答",而不是猜——这类语句引用列,分析器本来就给了答案,不会问到我们。

**考虑过但不采用:**

| 方案 | 为什么不选 |
| --- | --- |
| 由应用在上报里声明 `operation` + `target` | 服务端零解析,但把"我知道我写哪张表"变成应用侧义务,与"应用侧尽量少做实现"的取向相反 |
| 不处理,接受这个缺口 | 实现最省,但 ORM 整行插入——应用写入里最普遍的形态——在图上没有出边,`kind` 也会被标成只读 |

### 6.5 新增第三类 origin

前端 `frontend/src/lib/lineageOrigin.ts` 现在把关系分成两类:`sql`(定义型)与 `openlineage`(摄入型),判据是 `relation.metaType === MetaType.OPENLINEAGE`。加第三类:

| 改动 | 位置 |
| --- | --- |
| `LineageOrigin` 联合类型加 `"applicationsql"` | `lineageOrigin.ts:16` |
| 色板加 `--lineage-appsql` 三元组与虚线样式(`sql` 实线、`openlineage` `7 5`;应用 SQL 取第三种,例如 `1 4`) | `lineageOrigin.ts:22,39` + `lineageTokens`/CSS 变量 |
| `relationOrigin()` 按三种 meta type 分派 | `lineageOrigin.ts:49` |
| `LINEAGE_ORIGINS` 与 `countRelationsByOrigin` 加一个键 | `lineageOrigin.ts:46,74` |
| 图例与提示的 i18n 键 | `lineageGraph.originApplicationSql` / `…Hint`,两个 locale 都加 |
| `lineageOrigin.test.ts`、`lineageTokens.test.ts`(后者断言"每种 origin 在两个主题都有颜色")同步 | 测试 |

注意 `mixed` 的语义现在是"两个写入者的边被折叠到一起",加第三类后它自然变成三者任意组合——`mixed` 的实现是"不在 `LINEAGE_ORIGINS` 里"的兜底,不需要改逻辑,但测试要补一条三者混合的用例。

## 7. 服务端 API 与权限

### 7.1 ConnectRPC 服务

新增 `ApplicationService`(`proto/v1/v1/application_service.proto`),照 AIP 与 `openlineage_service.proto` 的形状:

| RPC | HTTP | 权限 | 说明 |
| --- | --- | --- | --- |
| `CreateApplication` | `POST /v1/applications` | `metaxisdata.applications.create` | 创建应用,**同时签发第一个 key**(D7:创建 key 即注册应用) |
| `ListApplications` | `GET /v1/applications` | `metaxisdata.applications.list` | |
| `UpdateApplication` | `PATCH /v1/{application.name=applications/*}` | `metaxisdata.applications.update` | 改名、描述、`disabled` |
| `DeleteApplication` | `DELETE /v1/{name=applications/*}` | `metaxisdata.applications.delete` | 级联删观测;孤语句随之清理(§9.2) |
| `CreateApplicationAPIKey` | `POST /v1/{parent=applications/*}/apiKeys` | `metaxisdata.applications.apiKeys.create` | 轮换用;明文 key 只在响应里出现一次 |
| `ListApplicationAPIKeys` | `GET /v1/{parent=applications/*}/apiKeys` | `metaxisdata.applications.apiKeys.list` | 只回 masked |
| `RevokeApplicationAPIKey` | `DELETE /v1/{name=applications/*/apiKeys/*}` | `metaxisdata.applications.apiKeys.delete` | |
| `ListApplicationSQLs` | `GET /v1/{parent=instances/*/databases/*}/applicationSqls` | `metaxisdata.applicationSqls.list` | 过滤:应用 / schema / 类型 / 文本检索 / 时间窗(`parent` 已定实例与库);排序:最近出现、出现次数 |
| `GetApplicationSQL` | `GET /v1/applicationSqls:get?guid=…` | `metaxisdata.applicationSqls.get` | 单条详情:SQL 文本、`kind`、逐应用首末时间与次数、按需现算的读写关系 |
| `DeleteApplicationSQL` | `POST /v1/applicationSqls:delete`(body 带 `guid`) | `metaxisdata.applicationSqls.delete` | 人工清理误报;清镜像 + 血缘 + 版本行;`audit` |

语句是**对象级一律按 guid 寻址**的服务(带 body 的 POST 有先例:`GetLineageCounts` 是 `POST /v1/lineages:counts`),理由是拿到语句的入口几乎都给的是 GUID:图上的节点、Explain SQL 的历史入口、`GetMetadata` 的返回。GUID 里含 `;` 和 `/`(`__application_sql__/<id>`),塞进 URL 路径段需要额外转义,而 `GetLineage` 早已采用 guid 查询参数的形式(`/v1/lineages?guid=…`)。只有 `ListApplicationSQLs` 需要一个 `parent` 来做作用域收敛,沿用 manual SQL 的嵌套形状(`database_service.proto:74,506`)。于是 `application_sql_id` 完全不进 API 表面,只是内部构造资源名与 GUID 的原料。

**不新增"读语句"的 RPC**:语句已经是 registry 对象,`GetMetadata`(`GET /v1/metadata?guid=…`)与 `ListMetadataHistory` 对 `APPLICATION_SQL` 直接可用,只要 §3.6 的 `StoredMetadata` 分支补上。

### 7.2 权限与预定义角色

- 新增权限常量加到 `backend/common/permission/permission_gen.go`(生成物,按该包的方式生成)。
- `memberBaselinePermissions`(`backend/store/predefined_roles.go:39`)**只加读**:`ApplicationsList`、`ApplicationSQLsList`、`ApplicationSQLsGet`。理由:工作区是单租户、读是所有人的基线(见根 [AGENTS.md](../../AGENTS.md) 的"授权按工作区划定"),但**签发 key 是能写入数据的凭据**,必须留在 `workspaceAdmin` 或自定义角色里,和 OpenLineage 的写权限同一档。
- `ApplicationsApiKeysCreate` 等高危项**不加进 member 基线**。
- `predefined_roles_test.go` 会因权限集变化而需要同步。

## 8. 前端

| 新增 | 说明 |
| --- | --- |
| `pages/ApplicationManagementPage.vue` | 应用列表 + 创建(创建时展示一次明文 key)+ 停用 + key 轮换 |
| `pages/ApplicationSQLPage.vue` | 语句列表:筛选(应用、实例、库、schema、类型、时间)、排序(最近出现、出现次数)、分页;详情抽屉含只读 Monaco 展示参数化 SQL、逐应用首末时间与次数、读写关系 |
| `api/application.ts` | 每服务一模块,照 [frontend/AGENTS.md](../../frontend/AGENTS.md) 的分层 |
| 侧边栏 | `AppSidebar.vue` 加两个入口,绑到对应权限 |
| 血缘图 | §6.5 的 origin 第三类;节点渲染按 `MetaType.APPLICATION_SQL` 与 manual SQL 节点共用一个形状(它是"一个 SQL 节点",不是表) |
| i18n | `en-US.json` / `zh-CN.json` 两份都加,跑 `pnpm --dir frontend i18n:sort` |

参数化 SQL 的展示**必须只读**且不提供"编辑后保存"——语句是机器事实,不是人工资产。与 manual SQL 的差别正在于此,页面上要说清。

## 9. 迁移与生命周期

### 9.1 迁移

按 [backend/migrator/AGENTS.md](../../backend/migrator/AGENTS.md) 的两文件规则:

- `backend/migrator/migration/LATEST.sql` 追加四张表的幂等 DDL。
- `backend/migrator/migration/0.1/0019##application_sql.sql` 增量(当前目录最高是 `0018##notification_seen.sql`)。
- `backend/store` 的查询同批完成。

### 9.2 生命周期与清理

| 触发 | 行为 |
| --- | --- |
| 上报了新语句 | 插 `application_sql` + 观测行 + registry 镜像(总是),`QueueLineageAnalysis` |
| 语句的写入型判定翻转 | runner 按当前分析结果重写该对象的边(`BatchReplaceColumnLineage` 本来就是替换语义,只多/少那条出边),并回写 `kind`(§6.1);镜像行始终保留 |
| 应用被删(硬删,不是停用) | key 与观测行随 FK 级联删除;**孤语句**(观测数为 0)连 `application_sql` 行、registry 镜像、血缘、版本行一起删除,形状照 `DeleteManualSQL`(`backend/store/manual_sql.go:408`) |
| 应用被停用 | 只拒 key,不删任何历史 |
| 语句长期无上报 | **不自动删除**。`last_seen_at` 已经表达了它;页面上按时间筛即可 |

### 9.3 归一化规则变更时的重算

`fingerprint_version` 变化时,`application_sql` 里旧版本的行需要重算指纹,并按 (实例, 库, schema, 新指纹) 合并重复行:合并时观测行按 `application_id` 去重,`first_seen_at` 取最早、`last_seen_at` 取最晚、`execution_count` 取和;被合并掉的行连同其 registry 镜像与血缘一起删除(血缘由保留行的 `QueueLineageAnalysis` 重建)。

这是一个 maintenance runner 里的独立 pass(`backend/runner/maintenance/`),不是迁移的一部分——因为它要跑分析器、要重写血缘,而迁移只该做 DDL。规则变更时手动触发一次。**这条不做的话,改一次 N 规则就永久留下两套语句行**,所以它必须和规则变更同批交付。

## 10. 实施阶段

每个阶段都独立可验证、独立可回滚。P0 是 spike,结论不成立就改方案而不是硬推。

| 阶段 | 交付 | 验证 |
| --- | --- | --- |
| ~~**P0 spike**~~ **已完成** | 三件事全部结案,见 §13:①字面量判定改用 token 源文本切片,不依赖上游未导出的类型常量;②在仓库自带语料(642 条可分析语句)上跑完脱敏幂等性与血缘等价性;③取表助手在真实触发面上 25/25 | 实测报告见 §13;表级边能被图读取已加为常驻测试 `TestBuildLineageGraphDrawsTableLevelEdges` |
| ~~**P2 脱敏与指纹**~~ **已完成** | `backend/plugin/sqlsanitize/`:`Sanitize` / `Analyzable` / `Statement{Text, Canonical, Fingerprint}` / `Version`,含语料等价性、幂等性、字面量分类与中性字面量矩阵四组测试 | `go test ./backend/plugin/sqlsanitize/`;`golangci-lint` 0 issues |
| ~~**D9 取表助手**~~ **已完成** | `backend/plugin/dmltarget/`:`Targets` / `ErrNoTarget`,含触发面 25 例、边界与拒答三组测试 | `go test ./backend/plugin/dmltarget/`;`golangci-lint` 0 issues |
| ~~**P1 数据模型**~~ **已完成** | 四张表 + `0019##application_sql.sql` 增量 + `backend/store/{application,application_api_key,application_sql}.go`;见 §13.9 | `go test ./backend/store/`、`./backend/migrator/`;`golangci-lint` 0 issues;两份 schema 已在真 PostgreSQL 上跑过并验幂等 |
| **P3 摄入与管理 API** | 上报端点(含 §5.4 的分析器闸门)+ key 认证 + `ApplicationService` 的 CRUD 与 key 管理 + 权限常量与角色 | handler 单测(无 key / 坏 key / 停用 / 逐项 REJECTED / 超体超批 / `reportId` 去重);`go test ./backend/api/v1/` |
| **P4 血缘接入** | `MetaType_APPLICATION_SQL` + registry 镜像 + runner 的 `buildSQL` 分支 + 分析器分支 + 写入/只读分类(消费 P2 与 D9 两个包) | `go test ./backend/plugin/lineage/...`、`go test ./backend/runner/lineageanalyzer/`;集成:`make test-integration`(上报 → 图上可见边) |
| **P5 前端** | 应用管理页、应用 SQL 页、origin 第三类、图例、i18n | `pnpm --dir frontend biome:check && lint && i18n && type-check && test run` |
| **P6 收尾** | 把本文件改写成维护参考并在 `.agents/docs/README.md` 加行;`docs/` 里补一份面向接入方的接口文档(上报格式、key 申请、重试语义、占位符约定) | 文档 review |

P2 与 D9 先于 P1 落地是有意的:两者都是纯函数包,不依赖数据模型,先把它们做成可测的实体,后面接摄入路径时就只剩接线。

## 11. 风险与未决问题

### 已确认的取舍

本次对齐定下的四条,全部已经写进上面的设计,这里只留索引与代价:

| 取舍 | 代价 |
| --- | --- |
| **§0 事实 1:`?` 由服务端替换,不动 omni** | 上游 MySQL 语法缺占位符产生式;我们不需要它——替换是服务端的事(20 个位置 × 3 种中性字面量零失败) |
| **D1':每条语句都在图上建节点** | 节点数 = 不同语句数,可能上千;靠"去重 + origin 过滤器"控制(§6.3) |
| **D8:摄入请求不写审计行** | 无法事后追查某一次摄取请求发了什么,只留 key 级聚合计数(§5.5) |
| **D9:零关系写入的目标表由服务端取** | 只需 INSERT 与不带列的 DELETE,三个方言族合计几十行,只在零关系分支上跑;实测 25/25(§6.4 / §13) |

### 与规则变更绑定的前置条件

**§9.3 的重算 pass 与 N 规则同批交付**。不做的话,改一次规则就永久留下两套行。

### 残余风险,接受但不隐瞒

| 风险 | 说明 | 可用的缓解 |
| --- | --- | --- |
| **无请求预算** | 你选了不限流。落库行数确实受控,但**每个请求都要落一次事务,并让每一条新语句进一次分析队列**,这条路径目前没有任何请求预算。一个写错循环或 key 泄漏的应用可以把 CPU 花在解析与写库上 | 现成的 `openLineageIngestionMiddleware`(`backend/server/openlineage_ingestion.go`)可以照搬成一个独立实例,只需换预算值。日后要加,不改数据模型 |
| **指纹漂移导致行数增长** | "去重后不会很多"依赖 N 规则覆盖了应用的文本差异。N4–N6 覆盖的是批量大小、IN 长度、常量算式;若某应用用字符串拼 SQL 拼出**列名/表名**(如动态排序字段),指纹会随维度漂移 | 应用 SQL 列表页要有"新增语句数 / 天"的观测入口,让这类应用能被发现。不预设上限,但要有度量 |
| **未加别名的表达式会带上中性字面量作为列名** | 分析器把未加别名的表达式列命名为其源文本,而源文本里的字面量已被换成 `'?'`,于是列名形如 `price * '?'`。这是**归一化而非丢失**(同一条语句每次执行得到同一个名字),但应用若想让人读到原始表达式,应当在 SQL 里写别名 | 无——接受。ORM 生成的 SQL 基本都给列起别名 |
| **重试重复计数** | 不带 `reportId` 的重试会重复计数 | `reportId` 只挡紧接的一次;频率是元数据不是账单,v1 接受 |

### 待决(不影响动工,但要在 P0 定)

- **N8 关键字大小写归一**是否做(§4.3)。建议推迟。
- **N4–N6 何时加**:折叠规则改的是"什么算同一条语句",要等 §13.7 那份真实应用语料才能量出过并的代价。加之前不动 `sqlsanitize.Version`。

## 12. 验收与测试

| 层 | 断言 |
| --- | --- |
| `sqlsanitize`(**已实现**) | `Sanitize` 幂等;字符串里的 `?` 与注释里的字面量不被误替换;`'`/`"`/`` ` ``/`$$`/`x'..'` 各形态的字面量判定逐例;存储文本保留原始空白、只删注释;`Analyzable` 产出的文本能被三个方言族的分析器接受。N4–N6 未实现,见 §13.7 |
| 指纹 | 同义语句(IN 长度不同、批量行数不同、`1+1` vs `2`、字面量不同)得到同一指纹;不同表/不同列名**不**合并;`fingerprint_version` 变更后重算 pass 能合并并保住计数等于各自之和 |
| 摄入 handler | 无 key / 坏 key / 撤销 / 停用 → 401 且不落库;逐项 REJECTED 不连坐;超体 413、超批 400;`reportId` 重放不重复计数;`WARN` 日志含语句摘要与原因 |
| store | 唯一键冲突走 upsert 而不是插重复;观测首末时间取极值、次数累加;汇总与观测表一致;删应用级联后孤语句及其 registry/血缘/版本行一并消失 |
| 分析器 | 读/写分类逐例(`INSERT VALUES` 为写入型依赖 §6.4 的服务端取表);读语句产生 `源表 → 语句` 且**没有**出边,写入型另有 `语句 → 目标表`;`SELECT 1` 这类两端都拿不到的语句产生 0 行;**表级边(空列名)能被 `GetLineageGraph` 画成节点** |
| 只读详情 | `GetApplicationSQL` 对 MySQL 家族的 `?` 文本能算出一致的读写关系(即它确实走了 `Analyzable`,没有把 `?` 原样喂给分析器);`kind` 在 runner 分析后被回写 |
| 端到端 | 集成用例:签发 key → 上报一批含读/写/坏 SQL → 断言库内计数、断言图上新增 origin=`applicationsql` 的节点与边、断言坏 SQL 未落库且响应里可见 |
| 前端 | `lineageOrigin` 三类分派与 `mixed` 三者混合;色板在两个主题下齐全;i18n 两份键齐;i18n:sort 后无 diff |

## 13. P0 spike 结论(已完成)

> 本节记录的每个数字都来自实跑。探针已提升为 `backend/plugin/sqlsanitize/` 与 `backend/plugin/dmltarget/` 两个包(§13.8),外加一条常驻测试 `TestBuildLineageGraphDrawsTableLevelEdges`(`backend/api/v1/lineage_service_graph_test.go`)。

### 13.1 语料

用仓库自带的血缘语料当"真实应用 SQL"的替身:86 个套件、695 个用例,覆盖 MySQL / PostgreSQL / StarRocks 三个 parser 族;其中 **642 条**能被分析器接受(其余是分析器自身的缺口,与本方案无关)。

这是离线可得的最接近真实应用的语料,但它以视图定义为主,不是 ORM 产出——见 §13.7。

### 13.2 三个设计问题的答案

| 问题 | 结论 |
| --- | --- |
| 字面量判定要不要依赖上游的 token 类型常量? | **不要。** `Token` 的 `Loc`/`End` 给出原始文本切片,按首字符形状判定即可,与上游常量、与 `Str` 的解码方式都无关。上游改编号不会再静默失效——§11 原先那条残余风险因此消失 |
| 注释要自己识别吗? | **不要。** 两个方言族的 lexer 都不把注释产出成 token,按 span 重建天然丢弃 |
| 中性字面量选什么? | **任何字符串字面量**,取 `'?'`。`NULL` 与 `0` 在 MySQL `LOAD DATA`、`JSON_TABLE PATH`、PostgreSQL `INTERVAL`、StarRocks `COPY INTO` 上被拒(`NULL` 唯独在 PostgreSQL `COPY` 上侥幸通过);`$n` 在 PostgreSQL 的 `COPY` / `INTERVAL` 上被拒——于是"PostgreSQL 用 `$n`"被推翻,三套方言派生塌成一条规则。完整矩阵已固化在 `TestNeutralLiteralIsTheOnlySubstituteThatParsesEverywhere` |

### 13.3 脱敏的质量(642 条)

| 指标 | 结果 |
| --- | --- |
| 脱敏拒绝的语句 | **0** |
| 文本被改动的语句 | 209(其余 433 条本就没有字面量) |
| 幂等 `Sanitize(Sanitize(x)) == Sanitize(x)` | **642 / 642** |
| 血缘逐边完全相同 | **636 / 642 = 99.06%** |
| 有差异 | 6 |

6 条差异**没有一条是结构性丢失**:

| 类别 | 条数 | 说明 |
| --- | --- | --- |
| 未加别名的表达式列名 | 3 | 列名 = 源文本,字面量被换成 `'?'`:`v.x - 1` → `v.x - '?'`。边结构不变,只是名字被归一化 |
| 同端点、不同常量的边被合并 | 3 | `SELECT x + 1 AS a, x + 2 AS a` 的两条边变成一条——这正是 D5"激进归一"想要的;两端相同,所以图上没有差别 |

### 13.4 D9 取表助手

真实触发面只有"零关系写入"两类:

- 常量 `INSERT` 的各种形态:多行 VALUES、`INSERT IGNORE`、`REPLACE`、`INSERT … SET`、`ON DUPLICATE KEY UPDATE`、限定名与引号名、PG `ON CONFLICT` 与 `DEFAULT VALUES`、`INSERT … SELECT <常量>`;
- **不引用任何列的 `DELETE`**:`DELETE FROM t`、`DELETE … WHERE <常量>`。

实测 **25 / 25 全部取对**。三类形状**不会**触发,**因此不需要在助手里处理**:`UPDATE`(哪怕 `SET a = 1`、多表 JOIN 上的常量赋值)有 `t.* -> t.a`;多表 `DELETE o FROM t o JOIN …` 有 `nonTemp ≥ 1`;所有 DDL 有真实目标表。

一个已知小空白:`TRUNCATE` 也是零关系,而 D9 不认它是写入,所以它既没有出边,`kind` 也会被标成只读。

### 13.5 表级边能被画出来(已常驻为测试)

`TestBuildLineageGraphDrawsTableLevelEdges` 喂一条列名为空的边给遍历,断言两个节点都出现。这是 D9 那条表级边的生存条件:如果它不成立,语句会被存下来却从不出现在画布上。

### 13.6 摄入闸门必须与分析器同口径(新增发现)

这条原设计漏了,是跑语料才暴露的:闸门**不能调裸 parser**。

`ALTER VIEW v AS SELECT id FROM t1 UNION ALL SELECT id FROM t2` 在 StarRocks 上被 `srparser.Parse` 拒绝,却被分析器**接受**——分析器有 `extractViewDDL` 兜底(`starrocks/analyzer.go:147`)。闸门若用裸 parser,这条语句会被误判成"解析失败"而拒收,而它本可以产出完整的血缘。

结论:摄入的闸门就是 `*lineage.Analyzer.Analyze` 本身,结果丢弃(§5.4)。代价是每条新语句在摄入端多解析一次,落在 §11 的"无请求预算"残余风险上。

### 13.7 未覆盖,留给 P2

- **语料以视图定义为主**,不是真实 ORM 产出。真实应用的命名参数、批量 VALUES、动态 `IN` 展开要在 P2 补一份语料再跑一次。
- **N4 / N5 / N6 未实现**:`IN` 列表折叠、`VALUES` 组折叠、常量表达式折叠只影响**指纹粒度**,不影响血缘等价性;本轮实现的是 N1–N3,折叠规则的收益要用真实语料量。
- **TiDB / MariaDB 未单独跑**:它们与 MySQL 共享同一份分析器正文(生成物),行为一致,但两条独立语料仍应各跑一遍。

### 13.8 探针已提升为仓库包

一次性探针(`backend/spike/`)已删除,内容落成两个包。它们都是纯函数、不依赖数据模型,所以先于 P1 落地。

**`backend/plugin/sqlsanitize/`**

| 文件 | 内容 / 钉住什么 |
| --- | --- |
| `sanitize.go` | `Sanitize(engine, sql) (Statement, error)`、`Analyzable(engine, text) string`、`Statement{Text, Canonical, Fingerprint}`、`Version`、`ErrUnlexable` |
| `token.go` | `tokenSpans`(三方言的字节偏移)、`isLiteral`(按源文本切片判定)、`stripComments` |
| `sanitize_test.go` | 字面量分类逐形态、字面量替换与留白、注释删除、幂等、引号标识符里的 `?`、指纹忽略空白 |
| `corpus_test.go` | §13.3 的验收:642 条语料上 0 拒绝、100% 幂等、≥98% 逐边相同,且**绝对要求**"脱敏前能分析、脱敏后仍能分析" |
| `neutral_test.go` | §13.2 的完整替换矩阵(含 `NULL` 唯独在 PostgreSQL `COPY` 上通过、`$n` 在 `COPY`/`INTERVAL` 上失败的细节) |

**`backend/plugin/dmltarget/`**

| 文件 | 内容 / 钉住什么 |
| --- | --- |
| `target.go` | `Targets(engine, sql) ([]model.ObjectIdentifier, error)`、`ErrNoTarget`;各方言的限定名规则与 MySQL 的 `USING` 拒答规则 |
| `target_test.go` | 触发面 25 例逐条取对;边界侧断言"助手拒答的这些形状,分析器本来就给了答案";拒答侧防"发明一张表" |

两条实现经验,顺手记下:

- **`strings.Builder.WriteString` 在本仓库会触发 `revive` 的 `unhandled-error`。** 既有写法是 `_, _ = b.WriteString(...)`(`backend/plugin/lineage/starrocks/rawsql.go:84`)。两个包都已经是这个写法,`golangci-lint` 0 issues。
- **`rawsql.go` 是"按 token span 重建源文本"的现成先例**,与 `sqlsanitize` 完全同构(`exprText` 从 `Loc.Start:Loc.End` 拼回文本)。改这个包之前先读它。

### 13.9 P1 数据模型落地记录

四张表、一份增量迁移、三个 store 文件。两份 schema 文件(`LATEST.sql` 与 `0019##application_sql.sql`)都在 PostgreSQL 16 上实跑过,增量在已建好的库上重跑只有 `NOTICE ... already exists`,没有报错。

三个只有**真跑**才会暴露的问题,记下来免得再犯:

| 问题 | 症状 | 处理 |
| --- | --- | --- |
| `last_report_id` 可空 | `Scan error: converting NULL to string is unsupported` | 改成 `NOT NULL DEFAULT ''`,空串即"还没接受过上报" |
| `Authenticate` 的 JOIN 用错键 | `operator does not exist: bigint = text` | `application_id` 改成公开 id 之后,JOIN 要写 `a.application_id = k.application_id` |
| `INSERT ... RETURNING s.guid` 没有别名可绑 | `missing FROM-clause entry for table "s"` | 列列表统一用表名限定而不是别名——`INSERT ... RETURNING` 没有 `FROM`,`s` 绑不上 |

store 的守门测试照仓库惯例放在纯逻辑上(`application_sql_pure_test.go`):冲突目标是语句身份、**冲突时不重写 `guid`/`application_sql_id`/`sql_text`**、观测是累加而不是覆盖、汇总从观测算出、孤语句清理以"没有观测"为条件。这五条都被断言,而不是靠读代码。

端到端行为(建应用 → 发 key → 认证 → 上报 → 跨应用去重 → 汇总 → 镜像 → 读回 → 撤销 key → 删应用保语句 → 删最后一个应用清理孤语句与镜像)本轮用一个连真库的一次性探针跑通过,**但那个探针已删除**;它要由 P3 的集成测试补成常驻用例,否则 P1 之后的 SQL 改动没有回归保护。

## 附:一句话总览

应用 SQL 走的是"OpenLineage 的管道形态 + manual SQL 的血缘形态",新增的是**脱敏与指纹**这一段算法和**应用/key** 这一层归属;图与运行时的改动是加一个 origin、加一个 MetaType、加一个分析器分支,不动语义。
