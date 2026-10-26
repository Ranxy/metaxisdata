# 数据质量：从「规则目录」到「目标工作台」— 目标中心化重构

> Status: **P0、P1a、P1b 与 P1 小项已实现**（分支 `dq-target-centric-refactor`，尚未合并）。P0 是前端目标工作台；P1a 是服务端聚合读、工作台内联新建规则、视图/物化视图/外部表入口与详情页反向锚点；P1b 是薄契约（目标级 filter、作者默认值、发布/版本/差异）；P1 小项补齐内联编辑、字段覆盖、反向高亮、列表 URL 筛选与每规则最近执行。剩余仅 §4 的 P2（可选）。
> 关联：[data-quality.md](data-quality.md)（现有实现与不变量）、[frontend-ui.md](frontend-ui.md)（布局门禁）、[i18n-glossary.md](i18n-glossary.md)（术语）、[instance-connections.md](instance-connections.md)（执行侧连接准入）、[security-posture.md](../../docs/security-posture.md)（单副本假设）。

## 0. 结论摘要

1. **问题不在规则引擎，而在入口**。现有 DQ 模块把 `QualityRule` 当作唯一的一等公民：规则目录、规则编辑器、规则详情、执行、问题、建议各自成页，没有「一张表的质量状况」这个视图。给一张表配 N 条规则 ≈ 2N 次整页跳转 + (N−1) 次从实例级重选目标（证据见 §1.1）。AI 草案落在另一个页面的队列里，而不是「这张表」的语境里。
2. **目标（表/视图）才是读者维护的单位**。外部产品（Soda/Great Expectations/dbt/Monte Carlo/dbt）几乎都把「表/数据资产」作为聚合单位；Soda 把它命名为 contract，但真正产生价值的不是这个名字，而是：目标级默认值、检查分层、就地编辑、健康度汇总、AI 草案就地审阅。
3. **P0 已落地「目标工作台」读模型，不引入新实体**：`/data-quality/targets`（目标列表）与 `/data-quality/targets/:targetGuid`（目标工作台：表级/跨字段/字段三段 + 右栏 AI 助手 + 内联草案一键建规则 + 运行全部）。规则仍是唯一可变实体；后端零改动，完全复用 `ListQualityRules`（已批量回填 `last_result`）、`ListQualitySuggestions`、`ListQualityIssues`、`RunQualityRules({target_guid})`、`SuggestQualityRule`。
4. **P1 才引入「薄契约」**：`Contract = 目标级默认值（filter/schedule/severity/owner/tags/attributes）+ 有序规则清单 + 显式发布产生的版本`。契约不接管执行、问题、通知与配额——它们仍按单规则；契约也不成为第二套编辑器。发布才产生不可变版本（对齐 Soda「版本由 publish 产生」），不是每次编辑都生成版本。
5. **明确不做的重器**：每目标一个不可变版本、独立契约编辑器、Soda 式 proposal/request/冲突 sync 三层协作、AI 直接写生效规则或自动发布。
6. **后续必须补的读接口**：`ListQualityTargets`（服务端 GROUP BY + 排序/分页/计数）与 `GetQualityTarget`（目标全貌 + 每规则最近 N 次执行）。P0 用客户端聚合是因为工作区规则有上限（每表 100 / 每工作区 1000，见 [data-quality.md](data-quality.md) §7 #4），超过这个量级就该搬到服务端。

## 1. 现状诊断

### 1.1 信息架构：所有入口都以「一条规则」为单位

`/data-quality` 下现有 10 条路由、侧栏 5 项（`frontend/src/router/index.ts:220-344`、`components/layout/AppSidebar.vue:362-403`），全部以 `QualityRule` 为主体。唯一「按表」的界面是元数据表详情里的一个标签页（`components/metadata/TableMetadataDetail.vue:62`），而且**只对 TABLE 挂载**：视图、物化视图、外部表支持规则（`utils/dataQualityForm.ts:197-213`）却没有入口。

「给一张表配一套规则」的真实路径：

1. 元数据表页 → DQ 标签（本地 `ref`，不进 URL）→「创建规则」→ `/data-quality/rules/new?table_guid=…`；
2. 选类型、填参数、dry run、保存 → 跳规则详情；
3. 想加第 2 条：规则详情**没有**「同一目标再加一条」入口，只能回列表 → 新建，且 `TargetPicker` 每次都从实例级 `reset()`（`TargetPicker.vue:490-506`）。

即 **2N 次路由跳转 + (N−1) 次 4 级目标重选**，且每次都被丢回「单条规则」的详情页，失去「这张表现在到底被检查成什么样」的视野。

### 1.2 具体摩擦（均有代码证据）

| # | 症状 | 证据 | 目标中心化后 |
| --- | --- | --- | --- |
| 1 | 规则平铺，目标只是标题下的副行；同表 6 条规则分散在不同页 | `DataQualityRulesPage.vue:260-325` | 目标列表一行一表；工作台内按表级/字段分组 |
| 2 | 改阈值要离开语境、加载整页、再跳回详情 | `DataQualityRuleEditorPage.vue:1046-1092,1251-1306` | 工作台内联草案/规则行就地操作（P0 先覆盖草案；内联编辑器 P1） |
| 3 | AI 草案在建议中心/资产卡（≤5 条）/Dialog 三个表面 | `SuggestionWorkbench.vue:2-6`、`MetadataQualitySection.vue:758,769` | 草案作为「尚未创建的目标行」就地渲染并高亮 |
| 4 | 执行/问题/建议四处分居；规则详情没有问题区 | `DataQualityRuleDetailPage.vue` 全文无 issue 区；`DataQualityIssuesPage.vue:292-302` 无 rule/target 筛选 | 目标页汇总未解决问题，问题详情反向锚回目标 |
| 5 | `RunQualityRules({target_guid})` 后端支持、前端从未调用 | `api/data-quality.ts:238-252`；全仓无 target 调用 | 工作台「运行全部规则」 |
| 6 | 概览「受监控表数」是死指标（无出口） | `DataQualityOverviewPage.vue:79-84` | 链到目标列表 |
| 7 | 列表筛选不写 URL，返回即丢 | `DataQualityRulesPage.vue:637-654` | 目标列表的搜索/筛选先写 URL（P1 再统一） |
| 8 | 18 类规则冷启动选择负担 | `utils/dataQualityForm.ts:101-190` | P1：从字段/语义候选反向给「建议槽位」，18 类降级为高级分支 |

### 1.3 后端：目标的过滤能力齐全，聚合能力缺失

- **模型是规则中心**：`quality_rule` 存 `target_guid/target_type/target_path(JSONB 快照)/target_meta_hash`，没有目标表；目标身份 = `(guid, object_type)`（`LATEST.sql:903-942`，registry 成对唯一 `:194-207`）。
- **按目标过滤全部具备**：rules/suggestions/issues/executions 四个 List RPC 都有 `target_guid`（proto `:1361-1362,1518-1521,1566-1569,1625-1626`）；`RunQualityRules` 支持 `target_guid`。
- **每规则最近结果不缺**：`ListQualityRules` 批量回填 `last_result`（`backend/api/v1/data_quality_service.go:246-257,1267-1277`），不是 N+1。
- **缺的是目标级聚合**：没有 `ListQualityTargets`/`GetQualityTarget`；`CountMonitoredTables` 只能给全工作区一个数（`store/quality_overview.go:16-54`）。「一个目标的全貌」今天靠资产页 6 个并行 RPC 拼（`MetadataQualitySection.vue:932-940`）。
- **写路径仍以单规则为粒度**：配额（每表 100、每工作区 1000，仅 Create 校验）、owner、调度 `next_run_at`、mute、issue 唯一性（`(rule_id) WHERE status<>3`）都挂在规则上。**契约不能接管这些**，否则等于重写执行与告警模型。

### 1.4 目标身份的两个硬事实

- **GUID 由名字派生**（`backend/common/guid.go:36-52`）：表改名 = 换 GUID = 旧规则变孤儿。以 GUID 为键的目标/契约会继承这个脆弱性；工作台必须把 `orphaned_reason` 显示为「目标不可用」，而不是假装目标还在。
- **GUID 不携带 meta type**：工作台路由因此把 `metaType` 放 query（与 `/metadata/:guid?metaType=` 一致），并从规则/建议兜底推导；这也让「还没配规则的表」可以进入工作台。

## 2. 外部调研：Soda 与其他产品做对了什么

### 2.1 Soda：contract 是「单个数据集上的检查集合 + 元数据 + 发布状态」

- **数据模型**：一份 YAML = 一个数据集；顶层 `checks`（数据集级）+ `columns`（列级检查、类型/长度/精度）+ `filter`（全局行过滤）+ `variables` + `check_attributes`（可批量打标签、可选择性执行）+ `scopes`（命名切片，带独立 filter/属性和 cron）+ `checks_schedule`。
- **检查身份** = 路径 + qualifier + scope；改 qualifier 或用 scope 移动会被视为**新检查并丢历史**——这是历史曲线与 AI 去重的地基，也是必须在 UI 里显式警告的语义。
- **版本由发布产生**：草稿点 Publish 才成为数据集的 source of truth；Contract history 全量保留、可 diff。不是每次编辑都生成版本。
- **协作**用 request/proposal：A/M/R 差异标记 + 父级蓝点、Sync to latest；复杂度来自跨团队生产者-消费者。
- **Contract Copilot**（用户截图的功能）：自然语言 → 取 schema/列元数据/数据源类型 + 现有契约 → 模型**在已有契约上提出改动** → 人类 approve the diff；官方措辞是「Within the chat, you will see exactly what changed」。只处理已有契约（首版用 Autopilot）；不发原始行数据；需 Manage contract 权限。**未证实**：截图里「左对话 / 右实时标注」的精确布局与逐条 accept/reject——公开文档只到「diff 标记 + approve the diff」粒度。

### 2.2 竞品共识

| 产品 | 聚合单位 | 检查组织 | 多检查操作 | 结果/健康度 | AI |
| --- | --- | --- | --- | --- | --- |
| Soda | dataset（contract） | 数据集级 / 列级 / scope | 按列分组编辑、按 filter 批量打属性、proposal 批量审阅 | 数据集列表健康列 + 检查历史 | Copilot 只改现有契约、出 diff 待批 |
| Monte Carlo | table / field | 5 类 monitor | Assets 页 Tab、alert 批量改 owner/severity | 资产健康列 + 异常时间线 | Agent 增删 monitor |
| Great Expectations | data asset | Suite（一个 asset 的断言集合） | Suite 内按列/主题组织 | Data Docs | 自动生成 suite 仍需人审 |
| dbt / Elementary | model | 通用/单例 test 挂在 model/column YAML | 一份 YAML 给多列加 test | 按 model 汇总 | 代理写 YAML，不入审阅 UI |
| Anomalo / Bigeye | table / metric | 自动检查 + 人工规则 | 表级一键开启 | 表状态 + 根因样例 | 异常检测为主 |

**共同点**：聚合单位是「表/数据资产」；契约只是「检查集合 + 元数据 + 发布状态」的命名；AI 一律产建议、人工批准后才生效。

### 2.3 可复用模式（采用）

1. 目标工作台作为二级枢纽：目标页承载检查、结果、问题与历史；检查固定三段（数据集级 / 列级 / 跨列）。
2. 健康度两层：列表页给失败检查数/草稿数/覆盖列数/上次执行；目标页顶部同指标 + 结果条带。
3. 草稿层与生效层分离：编辑不改现行契约；发布才产生不可变快照。
4. 内联草案状态机：proposed → edited → applied/accepted / rejected / superseded；可批量接受但逐条可撤销；AI 永不直接发布。
5. 稳定检查身份 + 目标级默认值 + 检查级覆盖（filter/属性/owner/调度）。
6. 反向导航：issue/执行/建议 → 检查 → 目标锚点。

### 2.4 反模式（拒绝）

- 每次编辑都生成不可变目标版本（历史爆炸、diff 无意义、发布语义消失）。
- 为契约再造一个编辑器（与规则编辑器形成两个事实来源）。
- 照搬 proposal/request/冲突 sync 三层流程（单租户先做「草稿/发布 + 审计」）。
- 目标页堆叠全部内容（规则 + 全量执行 + 画像 + 建议 + 历史一屏滚到底）。
- 聚合后仍以全局规则列表为主入口（创建/编辑必须发生在目标页）。
- AI 直接写生效规则，或「全部接受」而无逐条差异。
- 目标级与检查级调度/过滤并存但优先级不明。

## 3. 目标架构

### 3.1 概念模型

```
meta_registry_resource（只读）
  └── Target = (guid, object_type)        ← 目标身份，来自 registry；工作台/契约的聚合键
        ├── Contract（P1，薄投影）        ← 目标级默认值 + 有序规则清单 + 发布版本
        ├── QualityRule（唯一可变判定实体，已有 revision）
        │     └── quality_rule_revision（不可变）
        ├── Draft = quality_suggestion（已有 identity/signature）
        ├── QualityExecution（规则一次检查，已有）
        └── QualityIssue（规则失败问题，已有）
```

关键边界：

- **Contract 不接管**执行、调度、问题、通知、配额；这些仍按单规则。Contract 只回答「这个目标应该有哪些检查、默认怎么跑、当前发布的是哪一版」。
- **Target 不是新资源**：它没有自己的 resource name，只是 `(guid, meta_type)` 的投影；`Contract` 才可能落表（P1）。
- **Draft 不自动生效**：仍是 PENDING 建议，接受=创建规则；规则状态/执行/问题都不因草案改变。

### 3.2 信息架构（P0 已实现）

| 层 | 路由 | 页面 | 职责 |
| --- | --- | --- | --- |
| 概览 | `/data-quality` | `DataQualityOverviewPage.vue` | 工作区健康度；「受监控表数」链到目标列表 |
| **目标目录** | `/data-quality/targets` | `DataQualityTargetsPage.vue`（新） | 一行一个目标：规则数、覆盖字段、最近结果、未解决问题、待审草案、最近执行；搜索/排序/三个关注开关 |
| **目标工作台** | `/data-quality/targets/:targetGuid+?metaType=` | `DataQualityTargetPage.vue`（新） | 表级/跨字段/字段三段检查 + 右栏 AI 助手 + 内联草案 + 运行全部 + 未解决问题 |
| 跨目标视图 | `/data-quality/rules` | `DataQualityRulesPage.vue`（保留） | 「全部规则」高级筛选视图，不再是创建/编辑主入口 |
| 证据 | `/data-quality/executions`、`/issues` | 现有页 | 不变；问题/执行详情反向链回目标工作台（P1） |
| 建议 | `/data-quality/suggestions` | 现有页 | 跨目标队列保留；目标页内联草案是新的主路径 |

侧栏「质量规则」被「质量目标」替换；`menu.dataQualityRules` 删除，规则页从目标列表的「全部规则」和概览统计卡进入。

### 3.3 目标工作台布局（P0）

```
┌ PageHeader ─ 全部目标 / 质量目标 / orders ───────────────────────────────┐
│ orders   [表]   instance-1 · sales · public        [运行全部] [新建规则] [AI 起草] │
│ 3 条规则 · 2 通过 · 1 失败 · 2 条草案 · 1 个未解决问题 · 最近执行 2 分钟前      │
├──────────────────────────────────────────────┬───────────────────────────┤
│ 表级规则 (1)                                  │ AI 助手                    │
│  [PASS] 订单量不为空   row_count   [运行][编辑][详情] │  描述你想要的规则…         │
│                                              │  [示例 chips]              │
│ 跨字段规则 (1)                                │  [输入框]           [起草] │
│  [FAIL] 金额 = 单价 × 数量  cross_column       │  上次结果：新建 2 条草案     │
│                                              ├───────────────────────────┤
│ 字段 email (2)                                │ 未解决问题 (1)             │
│  [AI 草案] 邮箱不能为空  null_percent  [创建][忽略] │  ...                    │
│  [PASS] 邮箱唯一  unique                      │                           │
└──────────────────────────────────────────────┴───────────────────────────┘
```

布局约束（来自 [frontend-ui.md](frontend-ui.md) 与 `audit-ui-layout.mjs`）：

- **单一滚动容器**：页面只有 `main` 一个 scroller；右栏 `lg:sticky lg:top-0` 且**不**设 `overflow-y-auto`，内容随页面滚。结果/问题不做独立滚动栏；将来「执行历史/失败样本」用右侧 Dialog（teleport 到 body，审计看不见，与 `QualityExplainDrawer` 同形）。
- 桌面无顶栏、页面标题走 `PageHeader`、状态徽标用语义令牌；不新造颜色。
- 目标列表登记进审计（`/data-quality/targets`，`tableTopMaxY: 260`）；工作台需要真实 GUID，与规则详情一样**不登记**，由页面测试覆盖布局不变量。

### 3.4 AI 助手与内联草案（P0）

交互：

1. 右栏输入意图（如「确保数据集不为空」「为 bus_no 增加重复值检查」），提交调用现有同步 `SuggestQualityRule`（单目标；多目标后台任务仍留给建议中心）。
2. 返回的 `QualitySuggestion[]` 与 `summary`：草案按所属 section 就地插入（在对应字段段落内），新增项高亮；面板显示一句话 summary（复用 `useSuggestionSummary`，包含「已存在规则而跳过」「已决」「已刷新」等真实原因），而不是「已排队 N 条」。
3. 草案卡操作：
   - **创建规则**：`buildRuleFromSuggestion(suggestion)` → `createQualityRule(rule, suggestion.name)`，服务端在同一请求里「先 claim 决策、再写规则」；成功后草案变成正式规则行并启用。
   - **在编辑器中调整**：`?suggestion=<name>` 打开现有编辑器（唯一编辑器，不新建第二套表单）。
   - **忽略**：`UpdateQualitySuggestion(REJECTED)`；草案消失。
4. 生成失败按 Connect code 就地解释：`FailedPrecondition`（模型辅助未开）给出设置入口，`ResourceExhausted`（日额度/并发）说明重置，`PermissionDenied` 说明权限；不丢输入、不自动重试。

不变量（沿用现有 DQ 决策）：

- AI 只产 PENDING 建议，不建/不启用/不执行规则（D-30）。
- 草案身份仍是 `signature`（D-58），事实出现即 `SUPERSEDED`（D-60）；「接受」= 创建规则，不是自动发布。
- 不发送原始样本行；提示注入防护不变。

### 3.5 P1b：薄契约（Contract，已实现）

**为什么需要**：P0/P1a 解决了「看得见、就地改」，但每条规则仍要各配一遍 filter；「这张表的约定」没有可发布、可评审、可比对的载体。契约的收益来自**目标级 filter + 作者默认值 + 发布语义**，而不是新编辑器。

**已实现模型**（`0029##quality_contract.sql`，`proto/store/store/data_quality.proto`）：

```
quality_contract
  target_guid, target_type          -- 目标身份（与 rule 同口径）
  settings JSONB                    -- QualityContractSettings: filter,
                                    --   default_schedule_interval_seconds, default_severity
  published_version INT4            -- 最新的已发布版本；0 = 从未发布
quality_contract_revision
  contract_id, version              -- 每个 contract 自增
  snapshot JSONB                    -- QualityContractSnapshot: settings + 有序 checks
                                    --   (rule_resource_id, rule_revision, title, type, severity, column)
                                    --   + fingerprint(sha256)
  check_count, added_count, modified_count, removed_count, note
  published_by, published_at        -- 版本元数据
```

- **规则仍是唯一可变实体**：契约不存规则正文，只存引用 + 默认值；编辑规则 = 改 rule（已有 revision），契约只投影。
- **发布才定版**：发布写一条 revision（快照 + 计数 + 发布人 + note），并推进 `published_version`；未发布时 UI 显示「未发布变更」。**发布不阻塞执行**——规则保存后照常运行；发布是承诺/审计，不是 draft/publish 门禁。这与 Soda 的 publish 语义不同（我们没有跨团队 review 流程）。
- **目标级 filter 在编译期合成**：`有效 filter = 规则自身 filter 子句 + 契约 filter 子句`（规则在前），在 `dataquality.whereClause` 一次编译，因此占位符不重编号；预览、手动/定时执行、失败样本都带同一 filter。各自仍受 10 条上限，合计最多 20。`custom_sql` 不重写用户 SQL，因此**不受契约 filter 约束**；`cross_table` 只作用于左表（与规则 filter 同语义）；`schema_drift` 无 SQL、filter 不参与判定。
- **默认调度/严重级别只作作者默认值**：`TargetRuleComposer` 打开时用它们初始化表单；**不回写已有规则、执行规划不重算调度**。owner/tags 默认值未做（规则级字段，留待需要时再加）。
- **权限**：读 `rules.list`；改设置 `rules.update`；发布新增 `metaxisdata.dataQuality.contracts.publish`（workspaceAdmin 自动持有，自定义角色可授予）。设置更新与发布都审计；读取不审计。
- **版本差异**：按 `rule_resource_id` 对比快照：新增 / 移除 / 修改（revision、title、type、severity、column 任一不同）。历史列表只读版本元数据；差异按需拉两个版本快照，客户端计算。
- **幂等发布**：相同 settings + 相同 checks 的 fingerprint 相同；发布事务在行锁内比较最新 revision 的 fingerprint，相同则返回既有版本（`no_changes=true`），不产生重复版本。
- **不接管**：执行、问题、通知、配额、issue 唯一性全部保持规则粒度；契约不新增执行实体，也没有独立的契约编辑器。

**实现位置**：`backend/store/quality_contract.go`（CRUD/发布事务/历史）、`backend/api/v1/data_quality_contract.go`（5 个 RPC、快照/差异/转换）、`backend/component/dataquality/contract.go`（filter 校验与合并）、`backend/runner/qualityrunner/qualityrunner.go`（执行前读契约 filter）、`frontend/src/components/data-quality/{TargetContractCard,ContractSettingsDialog,ContractPublishDialog,ContractHistoryDrawer}.vue`。

**测试**：`dataquality/contract_test.go` + 执行/校验测试、`api/v1/data_quality_contract_test.go`、集成 `TestDataQualityContractRealServerIntegration`（真实 PG+MySQL：settings 校验、发布/幂等/历史/差异、契约 filter 进入执行 SQL）。

### 3.6 读接口（P1a，已实现）

P0 的客户端聚合在工作区上限内成立（每表 100 / 每工作区 1000 条规则），但目录没有服务端排序/计数，工作台要发 3 个请求。P1a 加的两个聚合读已落地：

```proto
rpc ListQualityTargets(ListQualityTargetsRequest) returns (ListQualityTargetsResponse) {
  // get: /v1/{parent=workspaces/*}/qualityTargets
}
message ListQualityTargetsRequest {
  string parent = 1;                    // workspaces/-
  int32 page_size = 2; string page_token = 3;
  string instance = 4; string database = 5; string schema = 6; string table = 7;  // GUID 前缀
  string search = 8;                    // path/GUID 子串
  QualityStatus worst_status = 9;       // 只匹配该最差最近结果
  bool has_open_issue = 10; bool has_pending_draft = 11;
  QualityTargetOrder order_by = 12;     // ATTENTION | NAME | RULE_COUNT | LAST_RUN
}
message QualityTargetSummary {
  QualityTarget target = 1;             // meta_guid, meta_type, 路径快照
  int32 rule_count = 2; enabled_rule_count = 3; paused_rule_count = 4; column_count = 5;
  int32 open_issue_count = 6; pending_draft_count = 7; pending_label_count = 8;
  QualityStatus worst_status = 9; google.protobuf.Timestamp last_run_time = 10;
  bool schema_changed = 11;
}

rpc GetQualityTarget(GetQualityTargetRequest) returns (GetQualityTargetResponse) {
  // get: /v1/{parent=workspaces/*}/qualityTargets:get
}
message GetQualityTargetResponse {
  QualityTargetSummary summary = 1;
  repeated QualityRule quality_rules = 2;     // 含 last_result / orphaned_reason / target_schema_changed
  repeated QualityIssue open_issues = 3;
  string orphaned_reason = 4;
}
```

实现要点（`backend/store/quality_target.go` + `backend/api/v1/data_quality_target.go`）：

- `ListQualityTargets` 一条 SQL：`live`（非归档规则）→ `targets`（计数、去重字段数、路径快照、`schema_changed` 来自 `meta_registry_resource_schema` join）→ 每规则最新执行（`DISTINCT ON (rule_id)`）→ 最差结果与最近执行时间 → 未解决问题/待审草案/标注计数；排序与过滤都在 SQL，offset 分页沿用 `parseLimitAndOffset`。
- `GetQualityTarget` 复用现有 store 读：规则 + `ListLatestQualityExecutions` + `ListMetaRegistrySchemaHashes` + `targetOrphanReasons` + 未解决问题；目标已被删除但规则还在时仍返回规则与快照路径（`orphaned_reason` 非空），无规则且 registry 也没有时 `NotFound`。
- 权限复用 `rules.list`（与 `GetQualityOverview` 同口径），**不加 audit 注解**，不进 principal 限流。
- **草案不在 Get 响应里**：`ListQualitySuggestions` 需要 `suggest` 权限，而目标读只需 `rules.list`；客户端有 `suggest` 时单独读草案。同理 summary 只给聚合计数，草案正文/样本都不出域。
- 测试：store 侧对 `targetWhere`（GUID 子树、LIKE 转义、attention 过滤、占位符基数）与排序/rank 做无数据库守卫测试；`api/v1` 侧覆盖顺序映射、worst-status 优先级、summary 转换；集成测试 `TestDataQualityTargetDirectoryRealServerIntegration` 在真实 PostgreSQL + MySQL 上验证聚合、搜索、分页与 Get。

### 3.7 后端/前端改造面（P1 摘要）

| 层 | 改动 |
| --- | --- |
| proto | **已实现**：`ListQualityTargets`/`GetQualityTarget` 两个 RPC 与消息；契约 CRUD/publish 未做。`buf format/lint/generate`，生成物随 proto 提交 |
| 迁移 | 聚合读不需要迁移；契约的 `quality_contract` + `quality_contract_revision` 未做 |
| store | **已实现**：`quality_target.go` 的单查询聚合 + `CountPendingQualitySuggestions`；`quality_contract.go` 未做 |
| api | **已实现**：聚合读 handler、顺序/过滤映射、summary 转换；发布事务未做 |
| runner | 未做：规划执行时合成目标级默认值（filter/schedule/severity） |
| 前端 | **已实现**：目标页接 `GetQualityTarget`/`ListQualityTargets`；工作台内联新建规则（`TargetRuleComposer`，复用 `RuleConfigForm` + `PreviewQualityRule`）；视图/物化视图/外部表的 DQ 标签页；规则/问题/执行详情 → 目标工作台的反向链接。未做：目标级设置面板、发布/变更入口、URL 化筛选、字段覆盖视图 |
| 文档 | `data-quality.md` §3.6/§3.7 已同步聚合读与路由；契约落地后需再同步 |

## 4. 阶段计划

### P0（已实现，本分支）

- 目标列表 + 目标工作台两页；表级/跨字段/字段分组；右栏 copilot；内联草案高亮、创建、忽略、去编辑器调整；运行全部规则；未解决问题汇总；侧栏与概览入口；元数据表页「目标工作台」入口。
- 零后端改动：复用 5 个既有 RPC；目标列表用 `ListQualityRules` 全量分页 + 纯函数聚合。
- 测试：`utils/dataQualityTarget.test.ts`（分组/健康度/排序/搜索）、目标列表页测试、copilot 与工作台测试。

验收：从任意一张有规则的表，2 次点击内到达「它的全部规则」；在该页可以用自然语言加一条草案并一键建成规则；不需要经过规则列表或目标选择器；页面不产生嵌套滚动、不破坏布局审计。

### P1a（已实现，本分支）

1. ✅ `ListQualityTargets` / `GetQualityTarget`：目标目录改服务端聚合；工作台一次读规则 + 摘要 + 未解决问题。**尚未做**每规则最近 N 次执行（`GetQualityTarget` 只带每条规则的 `last_result`）。
4. ✅ 工作台内联新建规则（`TargetRuleComposer`）：目标固定、类型按对象能力裁剪、`RuleConfigForm` 复用、先预览再创建；保存后就地插入分组，不离开页面。编辑仍走完整编辑器。
5. ✅（部分）视图/物化视图/外部表的元数据详情接入 `MetadataQualitySection`；规则/问题/执行详情页增加“目标工作台”按钮。未做：字段覆盖视图（未覆盖列 + 语义标注候选槽位）、问题/执行反向锚回目标并**高亮**具体规则。

### P1b（已实现，本分支）

1. ✅ 契约表与目标级 filter（编译期 AND 到每条规则）；作者默认调度/严重级别（只用于新规则表单）。
2. ✅ 发布/版本/差异 + `contracts.publish` 权限 + 审计；幂等发布；历史与按需 diff。
3. 未做：owner/tags 目标级默认值；目标级 filter 对 `custom_sql` 的支持（自定义 SQL 不被改写，属有意边界）。
4. ✅ 工作台内联「编辑规则」（composer 编辑模式 + update mask）；字段覆盖卡；问题/执行/规则详情反向锚回目标并**高亮**具体规则行；规则/问题/建议列表筛选写 URL；`GetQualityTarget.rule_history` 每规则最近 20 次执行（工作台行内色块条）。
5. ✅（此前）视图/物化视图/外部表入口、详情页反向锚点。

粗估：P1a 聚合读约 3–5 人日（已实现）；P1b 契约约 8–12 人日（已实现）；剩余小项约 3–5 人日。

### P2（可选）

- 契约 YAML 只读导出（供 CI/评审）；导入不做。
- 目标级趋势/画像覆盖度/scorecard；AI 批量起草契约（Autopilot 式），仍只产 PENDING 草案。
- 跨目标批量运行/暂停（复用 `RunQualityRules` 批量选择集）。

## 5. 不变量与风险

| # | 不变量/风险 | 处理 |
| --- | --- | --- |
| 1 | 规则是唯一可变判定实体，契约不成为第二个事实来源 | 契约只存引用 + 默认值；编辑永远走规则编辑器/规则 API |
| 2 | AI 只产 PENDING 建议，接受=创建规则，不自动发布/执行 | 沿用 D-30/D-58/D-60；UI 文案区分「创建规则」与「发布契约」 |
| 3 | 目标身份 = GUID+meta type；表改名 → GUID 变 → 旧规则孤儿 | 工作台显示 `目标不可用`；P1 契约沿用同一身份，不承诺改名跟随；如未来需要稳定身份，另立设计（registry 主键问题） |
| 4 | `metaType` 不在 GUID 里 | 路由 query + 规则/建议兜底；无规则目标也能打开工作台（AI 起草需 metaType，按钮按可用性禁用） |
| 5 | 归档规则默认不出现在列表 | 目标列表只统计非归档；「含归档」视图 P1 再给 |
| 6 | 单滚动容器 | 右栏 sticky 不滚动；历史/证据用 Dialog；`audit:ui` 登记目标列表 |
| 7 | 客户端聚合有上限 | 每表 100/工作区 1000 内成立；P1 改服务端聚合，前端只换数据源 |
| 8 | 权限混合（`rules.list` 是基线，`suggest`/`run`/`create`/`issues.list` 各自独立） | 按块 `v-if`，页面不整页一个 `canView` |
| 9 | 草案高亮不能替代审阅 | 卡片显示标题/类型/严重级别/置信度/理由；创建可撤销（忽略/删除规则），不提供「全部接受」 |
| 10 | 目标页过载 | 默认只列「有规则的字段」；未覆盖列/历史/画像放 P1 折叠或抽屉；列表默认 attention 排序 |

## 6. 验证与门禁

- 前端：`pnpm --dir frontend biome:check && pnpm --dir frontend lint && pnpm --dir frontend i18n && pnpm --dir frontend type-check && pnpm --dir frontend test run`；`utils/` 新文件在逐文件 95% 行/85% 分支门槛内，必须带测试。
- 页面：新增两页与三个组件测试；重点断言分组、草案创建/忽略、错误码文案、空态与加载降级。
- 布局：目标列表登记 `audit:ui`（预算 260）；工作台不登记（需要真实 GUID），靠页面测试与人工检查。
- 后端：P0 零改动；P1 按 `backend/AGENTS.md` 跑 gofmt/golangci-lint/相关 store/api/runner 测试与迁移链测试，proto 走 `buf format/lint/generate`。

## 7. Where things live（P0 + P1a）

| 面 | 位置 |
| --- | --- |
| 聚合读 | `backend/store/quality_target.go`（`ListQualityTargets` 的单查询聚合、`CountPendingQualitySuggestions`、排序/rank 守卫）、`backend/api/v1/data_quality_target.go`（两个 handler、顺序/过滤映射、summary 转换）、`proto/v1/v1/data_quality_service.proto` |
| 契约 | `backend/migrator/migration/0.1/0029##quality_contract.sql`（+ `LATEST.sql`）、`proto/store/store/data_quality.proto`（`QualityContractSettings/Snapshot/Check`）、`backend/store/quality_contract.go`（设置 upsert、发布事务、历史）、`backend/api/v1/data_quality_contract.go`（5 RPC、快照/差异/转换）、`backend/component/dataquality/contract.go`（filter 校验/合并）、`qualityrunner` 执行前读取、前端 `TargetContractCard`/`ContractSettingsDialog`/`ContractPublishDialog`/`ContractHistoryDrawer` |
| 目标列表 | `frontend/src/pages/data-quality/DataQualityTargetsPage.vue`（测试同名 `.test.ts`） |
| 目标工作台 | `frontend/src/pages/data-quality/DataQualityTargetPage.vue` + `components/data-quality/TargetCheckRow.vue`、`TargetDraftCard.vue`、`TargetCopilotPanel.vue`、`TargetRuleComposer.vue`、`TargetCoverageCard.vue`、`TargetRuleHistoryStrip.vue` |
| 纯投影 | `frontend/src/utils/dataQualityTarget.ts`（按列/跨字段分组、v1 summary 的排序/搜索/展示辅助） |
| 路由/导航 | `frontend/src/router/index.ts`（`DataQualityTargets`、`DataQualityTarget`）、`components/layout/AppSidebar.vue`；元数据四个详情页的 DQ 标签页复用 `MetadataQualitySection.vue` |
| 数据源 | `ListQualityTargets`、`GetQualityTarget`；写路径仍为 `RunQualityRules`、`SuggestQualityRule`、`CreateQualityRule`、`UpdateQualitySuggestion`；草案正文按 `suggest` 权限单独 `ListQualitySuggestions` |
| 复用 | `TargetPicker`、`RuleConfigForm`（P1b 内联编辑）、`SuggestionDetailDrawer`、`QualityExplainDrawer`（历史/解释）、`useSuggestionSummary`、`buildRuleFromSuggestion`、`suggestionToEditorPrefill` |
| 入口 | 概览统计卡、元数据四个详情页的 DQ 标签页、侧栏「质量目标」 |
| 设计参考 | 本文 §2 的 Soda/竞品结论与 URL |

## 8. 参考

- Soda 契约语言：https://docs.soda.io/reference/contract-language-reference
- Soda 契约编辑/发布/历史：https://docs.soda.io/data-testing/cloud-managed-data-contracts/author-a-contract-in-soda-cloud
- Soda 契约协作：https://docs.soda.io/data-testing/contract-collaboration
- Soda Contract Copilot：https://docs.soda.io/soda-ai/contract-copilot
- Soda Contract Autopilot：https://docs.soda.io/soda-ai/contract-autopilot
- Soda 数据集健康列表：https://docs.soda.io/manage-issues/browse-datasets
- Soda 全局检查列表：https://docs.soda.io/manage-issues/browse-checks
- Monte Carlo Assets：https://docs.getmontecarlo.com/docs/using-assets
- Great Expectations 术语：https://docs.greatexpectations.io/docs/reference/learn/glossary
- dbt data tests：https://docs.getdbt.com/docs/build/data-tests
