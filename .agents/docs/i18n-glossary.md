# i18n 术语表 — Reference

> Status: **implemented**。术语数据、门禁与生成文档均已落地。本文件是 `frontend/glossary.json` 的维护参考；`frontend/AGENTS.md` 是前端代码规则，本篇不重复。
> Related: [frontend/AGENTS.md](frontend/AGENTS.md)（i18n 工作流与门禁）、[glossary.json](frontend/glossary.json)（唯一事实源）、[check-i18n-glossary.mjs](frontend/scripts/check-i18n-glossary.mjs)（门禁与生成器）。

## 是什么

一份固定专业术语译法的表：每个术语在每种语言里只有一种写法，人和 agent 在**翻译 i18n 文案**、**写文档**、**命名代码与注释**时都参照它。它把"顺手换个近义词"这类漂移变成 CI 失败，而不是留在 review 里靠人眼发现。

已收录的术语分九类：Data / Lineage / OpenLineage / Ingestion / Access / Identity / Integration / Platform / Conventions。最后两类之外，Conventions 也管英文侧的大小写与拼写（`cancelled`→`canceled`、`Signup`→`sign-up`、`Openlineage`→`OpenLineage`），因为英文文案同样是产品语言的一部分。

## 数据与生成物

| 位置 | 角色 | 谁改 |
| --- | --- | --- |
| `frontend/glossary.json` | 唯一事实源：术语、定义、`scope`、`when`、`avoid` | 人（评审过的 PR） |
| `.agents/docs/i18n-glossary.md` 的生成表格 | 人看的表；agent 直接读这一段 | 只由脚本写 |
| `frontend/scripts/check-i18n-glossary.mjs` | 门禁 + 生成器 + 查询工具 | 与术语规则一起改 |

生成表格夹在一对 HTML 注释标记之间（`BEGIN GENERATED GLOSSARY` / `END GENERATED GLOSSARY`），**不要手改表格**：`pnpm --dir frontend i18n` 会在它和 JSON 不一致时失败。术语数据放在 `frontend/` 根目录而不是 `src/locales/`，是因为 `src/locales/*.json` 被 ESLint 的 `no-unused-keys` 规则和 `sort_i18n_keys.mjs` 当作 locale catalog 处理——术语表不是 catalog，混在一起会被当成一堆"未使用的 key"。

## 术语条目

```jsonc
{
  "id": "job",                       // lower-kebab-case，唯一
  "category": "OpenLineage",         // 必须是 categories 里声明过的
  "definition": "…",                 // 一句话，用来消歧（view 名词 vs 动词）
  "translations": { "en-US": "job", "zh-CN": "作业" },
  "scope": ["openlineage.", "openlineageSettings.", "home."],
  "when": { "en-US": "/\\bjobs?\\b/i" },
  "avoid": { "zh-CN": ["任务"] }
}
```

| 字段 | 语义 |
| --- | --- |
| `translations` | 固定译法。`locales` 里的每种语言都必须有非空值 |
| `definition` | 术语的边界：什么算这个术语、什么时候不算。表格里给人和 agent 读 |
| `scope` | i18n key 前缀列表，限定 `avoid` 的检查范围。**缺省 = 全部 key** |
| `when` | 附加条件：该 key 在某些语言里的值必须命中模式，规则才生效。通常写成"英文源文里真的出现了这个术语" |
| `avoid` | 按语言列出的禁用写法。不以 `/` 开头结尾的是字面量子串，`/regex/flags` 是正则 |

`scope` 和 `when` 存在的理由：同一个词常有多个义项（`view` 是视图也是"查看"，`列` 出现在"排列""列出""序列"里）。全局禁用一个写法会误伤；把规则钉在真正适用的 key 前缀上、并且要求英文源文确实用了这个术语，规则才既严格又不吵——`column` 那条就是这么写的。

## 门禁

`pnpm --dir frontend i18n` 依次跑三个脚本，`check-i18n-glossary.mjs` 是最后一个：

1. **完整性** — id 唯一且 lower-kebab-case、category 已声明、每种 locale 都有译法、`scope` 前缀在 catalog 里真实存在、正则可编译。
2. **漂移** — 任何 catalog 值命中 `avoid` 即失败，报错给出 locale、key、命中的模式和该用的译法。
3. **文档同步** — 生成表格与 JSON 必须逐字节一致；标记重复或缺失同样算失败。

漂移报错长这样：

```
  - zh-CN openlineage.searchJobsPlaceholder: contains "任务", but glossary "job" fixes "作业" — "搜索作业名称、…"
```

## 查询

```bash
pnpm --dir frontend i18n:glossary                 # 门禁
pnpm --dir frontend i18n:glossary --show job      # 查一个术语
pnpm --dir frontend i18n:glossary --show lineage  # 中英任一写法都能命中
pnpm --dir frontend i18n:glossary --write-doc     # 重写生成表格
```

`--show` 无参数时打印整张表——agent 在写新文案前的一次调用，比读 JSON 便宜。

## 决定

| 决定 | 理由 |
| --- | --- |
| JSON 是唯一事实源，Markdown 表格生成 | 两份手写清单必然漂移；生成 + 门禁让"表准不准"不再是问题 |
| 只固化术语，不固化整句 | 语序、语气和断句要留给译者；锁死整句会产出翻译腔和无法维护的模板 |
| 规则按 key 前缀 `scope` 收窄，并按 `when` 要求源文命中 | 多义词（`view` / `run` / `column`）全局禁用会误伤，收窄后可以下重手 |
| 英文侧同样受管 | 产品对外只有一套语言习惯；`cancelled`、`signin`、`Openlineage` 已在真实文案里出现过 |
| 中文保留 `Schema`、`SQL`、`API 密钥` 中的拉丁词 | 项目既有习惯（`README_zh.md` 也写 schema），硬译反而降低可读性 |
| `scope` 前缀必须命中真实 key | 术语随功能下线而失效，门禁逼着维护者删掉死规则而不是留着装饰 |
| 术语表放在 `frontend/glossary.json`，不放进 `src/locales/` | 那里每个 `*.json` 都被当成 catalog（ESLint 未使用 key、排序脚本），术语表不是 catalog |
| 允许 `--show` 查询 | 人和 agent 的入口不同：人看文档表格，agent 用一次命令拿到术语边界 |

## 生成表格

<!-- BEGIN GENERATED GLOSSARY -->
| en-US | zh-CN | Category | Definition | Scope | Do not use |
| --- | --- | --- | --- | --- | --- |
| instance | 实例 | Data | A registered connection to one database server whose schemas are synced. | all keys | — |
| database | 数据库 | Data | A database inside an instance. | all keys | — |
| schema | Schema | Data | The namespace holding a database's tables, views and sequences. Kept in Latin script in Chinese copy. | manualSqlManagement., metadataBrowser., openlineage., explainSQL., instanceDetail., oauthConsent., home. | zh-CN: `/(?<!\{)schema/`, `模式` |
| table | 表 | Data | A relation in a schema; its definition is synced and analyzed for lineage. | all keys | — |
| column | 字段 | Data | A field of a table or view, and the unit of column-level lineage. | applicationSql., home., metadataBrowser., lineageGraph., openlineage., oauthConsent., manualSqlManagement., instanceDetail., explainSQL., databaseManagement. | when en-US ~ `/\bcolumns?\b/i`; zh-CN: `/列(?!级\|表)/` |
| sequence | 序列 | Data | A database sequence object the metadata browser tracks. | all keys | — |
| view | 视图 | Data | A database view, as opposed to the verb "view" (查看). | all keys | — |
| materialized view | 物化视图 | Data | A materialized view; its definition is analyzed for lineage like a view's. | all keys | — |
| DDL | DDL | Data | The DDL statement that defines a database object. | all keys | — |
| metadata | 元数据 | Data | The registry's record of an object's structure and attributes. | all keys | — |
| Metadata Browser | 元数据浏览器 | Data | The page that walks instance → database → schema → table → column. | all keys | — |
| Manual SQL | 手动 SQL | Data | A hand-written SQL definition stored with an object so it takes part in lineage analysis. | all keys | — |
| lineage | 血缘 | Lineage | The derivation of one object or field from another. | all keys | — |
| table-level lineage | 表级血缘 | Lineage | Lineage between whole tables or views. | all keys | — |
| column-level lineage | 列级血缘 | Lineage | Lineage between individual columns; the phrase is fixed, whatever the noun used for a column elsewhere. | lineageGraph., openlineage. | en-US: `/field[- ]?(level )?lineage/i`; zh-CN: `字段血缘`, `字段级血缘` |
| lineage graph | 血缘图 | Lineage | The canvas that draws lineage nodes and edges. | lineageGraph., openlineage. | zh-CN: `图谱` |
| upstream | 上游 | Lineage | The side a lineage edge comes from. | all keys | — |
| downstream | 下游 | Lineage | The side a lineage edge points to. | all keys | — |
| field trail | 字段轨迹 | Lineage | The chain of column-level hops followed from one field. | all keys | — |
| node | 节点 | Lineage | One object drawn on the lineage canvas. | all keys | — |
| edge | 边 | Lineage | One lineage relation drawn between two nodes. | all keys | — |
| OpenLineage | OpenLineage | OpenLineage | The OpenLineage event standard, and the feature that ingests its events. One word, capital O and L. | all keys | en-US: `/Openlineage/`; zh-CN: `/Openlineage/` |
| job | 作业 | OpenLineage | A unit of work named by an OpenLineage producer; its runs carry the lineage. | openlineage., openlineageSettings., home. | zh-CN: `任务` |
| run | 运行记录 | OpenLineage | One execution of a job, with the events it reported. In Chinese copy the noun is 运行记录, not the bare English word. | lineageGraph., openlineage., generalSettings. | zh-CN: `/\bruns?\b/i` |
| event | 事件 | OpenLineage | A single reported OpenLineage event. | all keys | — |
| dataset | 数据集 | OpenLineage | An input or output asset named by an OpenLineage event. | all keys | — |
| external dataset | 外部数据集 | OpenLineage | A dataset that resolves to no registered instance. | notifications., openlineage., openlineageSettings. | zh-CN: `external dataset` |
| namespace | 命名空间 | OpenLineage | The OpenLineage namespace that scopes job and dataset names. | notifications., openlineage., openlineageSettings. | zh-CN: `/(?<!\{)namespace(?!\})/i` |
| producer | 生产者 | OpenLineage | The OpenLineage producer URI that identifies the emitting integration. | all keys | — |
| integration | 集成 | OpenLineage | A tool that posts OpenLineage events to the ingestion endpoint. | all keys | — |
| ingest | 接入 | OpenLineage | Receiving OpenLineage events into the workspace. | notifications., generalSettings., openlineageSettings., home. | zh-CN: `摄取` |
| retention | 保留时长 | OpenLineage | How long persisted OpenLineage runs are kept. | all keys | — |
| application | 应用 | Ingestion | A registered application that reports the SQL it runs. | all keys | — |
| statement | 语句 | Ingestion | A sanitized SQL statement an application reported. | all keys | — |
| report | 上报 | Ingestion | What an application does with the SQL it runs. | all keys | — |
| API key | API 密钥 | Ingestion | The credential an application or an OpenLineage producer authenticates with. | all keys | zh-CN: `/API [Kk]ey/`, `/OpenLineage key/`, `/\bKey\b/` |
| fingerprint | 指纹 | Ingestion | The stable hash of a sanitized statement that identifies it across reports. | all keys | — |
| sanitize | 脱敏 | Ingestion | Replacing literals and stripping comments before a statement is stored. | all keys | — |
| workspace | 工作区 | Access | The single tenant a deployment serves; every member sees its data. | all keys | — |
| member | 成员 | Access | An account that can read the workspace. | all keys | — |
| user | 用户 | Access | An account the server knows; every user is a workspace member. | all keys | — |
| group | 用户组 | Access | Several users merged into one IAM subject. | all keys | — |
| role | 角色 | Access | A named set of permissions granted to users and groups. | all keys | — |
| permission | 权限 | Access | One allowed action inside the workspace. | all keys | — |
| audit log | 审计日志 | Access | The append-only ledger of auditable actions. | all keys | — |
| revoke | 撤销 | Access | Ending a credential's or an authorization's validity. The Chinese rendering is 撤销, matching the API key pages; never 吊销. | applicationManagement., connectedApps., openlineageSettings. | when en-US ~ `/\brevok/i`; zh-CN: `吊销`, `回收`, `作废`, `取消授权` |
| SSO | SSO | Identity | Single sign-on through a configured identity provider. | all keys | — |
| identity provider | 身份提供商 | Identity | An OAuth2/OIDC provider configured for browser login. | all keys | — |
| provider | 提供商 | Identity | A vendor that supplies something (an identity, an LLM); 提供商, never 提供方. | identityProviderSettings., generalSettings., llmProvider., explainSQL., home., login., ssoCallback. | zh-CN: `提供方`, `/(?<!\{)[Pp]rovider(?!\})/` |
| sign in | 登录 | Identity | Authenticating. Verb "sign in"; noun and adjective "sign-in". | generalSettings., login., ssoCallback., oauthConsent., deviceLogin., identityProviderSettings. | en-US: `/\bsignin\b/i` |
| sign up | 注册 | Identity | Creating an account. Verb "sign up"; noun and adjective "sign-up". | generalSettings., register., login. | en-US: `/\bsignup\b/i` |
| password | 密码 | Identity | The local credential of a user account. | all keys | — |
| session | 会话 | Identity | A signed-in browser session, and the token it carries. | all keys | — |
| MCP | MCP | Integration | The Model Context Protocol endpoint the server exposes when enabled. | all keys | — |
| OAuth 2.1 | OAuth 2.1 | Integration | The OAuth 2.1 authorization server that backs MCP clients. | all keys | — |
| connected app | 已连接应用 | Integration | An MCP client a user authorized to act on their behalf; one record per user and client authorization. Not a browser session. | connectedApps., menu. | when en-US ~ `/\bconnected apps?\b/i`; zh-CN: `已连接的应用`, `关联应用`, `连接的应用`, `第三方应用` |
| external URL | 外部地址 | Integration | The address this deployment is reached at, which MCP tokens are bound to. | generalSettings., openlineageSettings. | zh-CN: `external URL`, `external url` |
| environment | 环境 | Platform | A catalogue label instances can be assigned to. | all keys | — |
| sync | 同步 | Platform | Pulling a source's schema into the registry. | all keys | — |
| LLM provider | LLM 提供商 | Platform | A configured model endpoint that Explain SQL can call. | all keys | — |
| Explain SQL | 解释 SQL | Platform | The LLM-assisted SQL explanation feature. | all keys | — |
| canceled | 已取消 | Conventions | American English spelling, one l: canceled. | error., common., environment. | en-US: `cancelled` |
| email | 邮箱 | Conventions | One word, no hyphen. | all keys | en-US: `e-mail` |
| filter | 筛选 | Conventions | Narrowing a list. The noun and the verb are 筛选. | manualSqlManagement., metadataBrowser., auditLogs., applicationSql., home. | zh-CN: `过滤` |
| SQL | SQL | Conventions | Always upper case, including inside Chinese copy. | all keys | en-US: `/\bSql\b/`; zh-CN: `/\bSql\b/` |
| create | 创建 | Conventions | A button that opens a blank form is 新建; the imperative and the past tense are 创建 (创建时间). | all keys | — |
| refresh | 刷新 | Conventions | Refresh is 刷新. Reloading a stored definition is 重新加载; Regenerate is 重新生成. | all keys | — |
| data quality | 数据质量 | Data Quality | The correctness and completeness of the data itself, as opposed to the registry's record of it. | dataQuality., menu. | — |
| quality rule | 质量规则 | Data Quality | One typed check over one target table or column, run on a schedule or on demand. | dataQuality., menu. | — |
| rule result | 规则结果 | Data Quality | One rule's outcome in one execution. | dataQuality. | — |
| execution | 执行 | Data Quality | One batch of rule results produced together, manually or on a schedule; the Chinese noun is 执行, never 运行记录 (that is an OpenLineage run). | dataQuality., menu. | zh-CN: `运行记录` |
| issue | 质量问题 | Data Quality | A failed or errored rule result that needs attention. | dataQuality. | — |
| dimension | 维度 | Data Quality | The quality aspect a rule measures, e.g. completeness or timeliness. | dataQuality. | zh-CN: `度量` |
| severity | 严重级别 | Data Quality | How much an issue on a rule matters. | dataQuality. | zh-CN: `严重程度`, `严重性` |
| threshold | 阈值 | Data Quality | The bound an observed value is compared with. | dataQuality. | zh-CN: `阀值`, `门槛` |
| observed value | 实际值 | Data Quality | What a rule measured in one execution. | dataQuality. | zh-CN: `观测值`, `实测值` |
| failure sample | 失败样本 | Data Quality | A failed row kept as evidence, already truncated. | dataQuality. | zh-CN: `失败样例` |
| freshness | 时效性 | Data Quality | How recently the data was updated. | dataQuality. | zh-CN: `新鲜度` |
| uniqueness | 唯一性 | Data Quality | Whether values, or tuples of values, occur only once. | dataQuality. | — |
| referential integrity | 引用完整性 | Data Quality | Whether a child row's value has a matching parent row. | dataQuality. | zh-CN: `参照完整性` |
| dry run | 干跑 | Data Quality | Compiling and probing a rule without saving it or creating a run. | dataQuality. | — |
| sampled | 采样 | Data Quality | Judged from a sample of rows rather than every row. | dataQuality. | zh-CN: `抽样` |
| suggestion | 建议 | Data Quality | A model-proposed rule draft or column label waiting for a person to accept or reject it. | dataQuality., menu. | zh-CN: `提议`, `意见` |
| semantic label | 语义标注 | Data Quality | The accepted meaning of a column, such as email address or business key, chosen from the semantic type catalog. | dataQuality. | zh-CN: `语义标签`, `语意标注` |
| column profile | 字段画像 | Data Quality | The measured facts about a column's values — null rate, distinct count, extremes, lengths — without reading the rows. | dataQuality. | zh-CN: `列画像`, `字段剖析` |
| owner | 负责人 | Data Quality | The workspace member responsible for a rule. | dataQuality. | zh-CN: `所有者`, `拥有人` |
<!-- END GENERATED GLOSSARY -->
