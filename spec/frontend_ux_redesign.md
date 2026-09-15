## Plan: Frontend UI/UX Restructure — Content-First Layout

当前前端的核心矛盾是「导航、标题、空白」压倒了「用户真正关心的元数据」。以 `http://localhost:3000/metadata/ms-sql-1%2Fmetaxis_demo%2Fsales%2Fcustomer?metaType=4` 为例，在 1440×900 视口下，首条 column 数据出现在 **y=669（视口高度的 74.3%）**，视口内真正可见的元数据表格面积仅约 **19.6%**；永久常驻的顶栏 + 侧栏已占视口面积 **24.1%**。本方案的目标是把「内容优先」确立为前端布局的第一原则：全局骨架只保留必要导航，页面级 chrome 收敛为一条 sticky 页头，其余纵向空间全部让给数据。

**Steps**

1. Phase 0 — Skeleton: desktop drops the top bar (Option A). 删除桌面端 `AppHeader`（`lg:` 及以上完全不渲染），回收 56px 垂直空间。品牌与折叠按钮移入侧栏顶部一行；用户菜单（含 Language / Theme）移入侧栏底部。`lg:` 以下保留一条 48px 细栏，只放 drawer 触发按钮与当前页标题，侧栏在该断点变为 overlay drawer（现状 820px 宽时侧栏仍死占 256px = 31% 宽度，Columns 表横向溢出、Lineage 标题折成 4 行，响应式实际是坏的）。`AuthLayout` 不受影响。

2. Phase 0 — Sidebar information architecture: 19 items → 5. 侧栏顶层只保留「首页 / Explain SQL / 数据源 / OpenLineage / 设置」五项。`collapsedSections` 初始化时把**非当前所在分组**全部收起，替换现在的默认全展开（`store/modules/app.ts` 中 `collapsedSections: []`）。「设置」由全局导航降级为局部导航：侧栏中直接链到 `/settings`，进入后由页面内二级导航承载 General / Environments / IAM / Roles / Groups / Users / Audit Logs / LLM Providers / Ingestion Settings 九项。把侧栏中 "Metadata" 一项改名为与面包屑、卡片标题一致的统一称呼，消除当前「同一目的地三个名字」（侧栏 `Metadata`、H1 `Metadata Browser`、CardTitle `Database Instances`）的问题。

3. Phase 0 — Shared `PageHeader` primitive. 新增 `components/layout/PageHeader.vue`，结构为 `面包屑(可选) / 标题 / 描述(可选) / #actions slot`，并统一替换当前 8 处手写 `<h1 class="text-2xl font-bold tracking-tight">`（`HomePage`、`MetadataBrowserPage`、`DatabaseManagementPage`、`InstanceManagementPage`、`ExplainSQLPage`、`GeneralSettingsPage`、`OpenLineageSectionHeader`）。标题降为 `text-xl`，`PageHeader` 与页面 tabs 一起在一个滚动容器内 sticky。列表与表单类页面加 `max-w-[1400px] mx-auto`，详情/表格类页面允许全宽——当前全站**零 `max-w-*`**，超宽屏下行长度失控。

4. Phase 1 — Metadata detail page restructure (the reported page). 目标：首条 column 行从 y=669 提到 **y ≤ 280**。具体改动：
   - 详情态**不再渲染 H1 "Metadata Browser"**；实体名即标题，面包屑说明位置。
   - **移除详情态的页面级全宽元数据搜索条**（含 Add Filter popover），改为全局 ⌘K 命令面板挂到侧栏顶部；该搜索条目前在所有 `/metadata` 路由（含详情页）常驻占用 42px + 一次数据请求。
   - **面包屑去卡片化**：`<Card><CardContent class="py-3 px-4">` 改为无边框一行并入 `PageHeader`；同时修掉根路径下渲染出一个只有 home 图标的空面包屑卡片（58px 纯浪费）。
   - **合并双层 tab**：`TableMetadataDetail` 的 `Table Details | History` 与 `MetadataHistorySection` 内部的 `History | Version Diff` 合成一套 tab（`Overview | Columns | Indexes | Lineage | History`），版本对比作为 History 面板内的二级切换，取消重复的 "History" 标题。
   - **Table Info 芯片降级**：`Row Count / Data Size / Index Size` 在全为 0 时压缩为标题下一行 `7 列 · 3 索引 · 0 B`；仅在存在非零值且列数较多时才展开为 chip 组。
   - **空态压缩**：Lineage 的「标题 + Graph View 链接 + 搜索框 + 0/0 徽章 + "No lineage relations"」五件套压成一行 `无血缘关系 · 查看图谱 →`。
   - **删除内层滚动**：去掉 `CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto"`（8 个详情分支都在用）。实测该容器 `scrollHeight=1276 / clientHeight=644`，632px 内容被藏在卡片内部滚动里，而 `main` 本身不可滚动、`window` 可滚动，形成三层滚动上下文。改为全页单一滚动区 + sticky header/tabs。
   - Columns / Indexes / Foreign Keys / Partitions 由纵向堆叠的 6 段 `heading + Badge + Table` 改为 tab 内切换，避免把 Columns 推到首屏之外。

5. Phase 1 — User menu, i18n and theme relocation. `UserMenu` 移入侧栏底部，展开态显示头像 + 名称 + `ChevronUp`，折叠态（`w-16` icon rail）退化为纯头像按钮，点击仍弹出完整菜单。`LanguageSwitcher` 不再作为独立常驻按钮，改为用户菜单内的二级菜单 `Language ▸`（简体中文 / English）；同理增加 `Theme ▸`（Light / Dark / System）——`store/modules/app.ts` 已有 `theme` 状态与 `setTheme`，但当前**没有任何 UI 调用它，深色模式实际不可达**。顺手清理 `UserMenu.handleProfile()` 这个点击无反应的 TODO 空函数。

6. Phase 2 — Fill the navigation-only pages. `OpenLineageOverviewPage` 当前是 4 张纯跳转卡片，100% 重复侧栏 OpenLineage 分组，且 "Ingestion Settings" 在页头按钮 + 卡片中重复出现；页面无任何数据（`mainTextLen=639`），首屏下方 60% 全空。将其替换为真实内容：近期 runs 表、job / dataset 计数、摄入健康度、最近事件时间线，并删除与侧栏重复的导航卡。

7. Phase 2 — Settings page consolidation. `GeneralSettingsPage` 的 4 张全宽 Card 各自带 `CardTitle + CardDescription + p-8 loader + Save 按钮 + readOnly 提示`，而前三张的数据来自同一次 `fetchSetting()`。合并为单一页面 + 分组表单，一个 loader，Save 收敛到 sticky 页脚。

8. Phase 2 — Shared state components and dead code removal. 抽取 `<PageState>`（loading / error / empty）与 `<EmptyState>`，替换当前 15+ 处复制粘贴的 `p-8 text-center` 与 6 处 `h-12 w-12 mx-auto mb-4 text-muted-foreground/50` 图标空态。清理死代码：`InstanceManagementPage.vue` 未被任何模板引用的 `.slide-*` scoped CSS、`DatabaseManagementPage.vue` 只有一个子元素却保留的 `justify-between`、`MetadataList.vue` 声明但从未转发使用的 `currentGuid` prop、`AppHeader.vue` 未走 i18n 的硬编码品牌名。

9. Phase 3 — Design tokens and heading semantics. 统一页面留白为内容区 `gap-4`，消除 `space-y-4` / `space-y-6` / `gap-6` / `p-5` 混用。禁止新增 `max-h-[calc(100vh-16rem)]`、`min-h-[42px]`、`w-[220px]`、`grid-cols-[380px_minmax(0,1fr)]` 这类硬编码尺寸，容器一律用 flex/grid 自适应。section 标题改用 `<h2>/<h3>`——当前 section 标题全是 `<div class="text-sm font-medium">`，无障碍树里 section 结构完全消失。

10. Phase 3 — Verification harness. 把本方案的量化指标沉淀为一个可复跑的检查脚本：对关键路由用 Chrome DevTools 协议量取「首条数据行 Y 坐标 / 视口高度」与「可见数据区面积占比」，防止后续改动导致 chrome 回涨。

**Relevant files**

- /home/ran/gocode/metaxisdata/frontend/src/layouts/DefaultLayout.vue — 去掉桌面顶栏、理顺单一滚动容器（当前 `main.flex-1 overflow-auto p-6` 与内层滚动冲突）。
- /home/ran/gocode/metaxisdata/frontend/src/components/layout/AppHeader.vue — 桌面端不再渲染；`lg:` 以下退化为 drawer 触发细栏。
- /home/ran/gocode/metaxisdata/frontend/src/components/layout/AppSidebar.vue — 顶层收为 5 项、默认只展开当前分组、顶部放品牌 + 折叠按钮、底部挂 `UserMenu`。
- /home/ran/gocode/metaxisdata/frontend/src/components/layout/UserMenu.vue — 移入侧栏底部，新增 Language / Theme 二级菜单，支持折叠态头像按钮，处理 `handleProfile` TODO。
- /home/ran/gocode/metaxisdata/frontend/src/components/layout/LanguageSwitcher.vue — 从常驻按钮改为菜单项（可直接内联进 UserMenu 或保留为子组件）。
- /home/ran/gocode/metaxisdata/frontend/src/components/layout/PageHeader.vue — 新增，统一页面标题 / 面包屑 / 描述 / 操作区。
- /home/ran/gocode/metaxisdata/frontend/src/components/metadata/MetadataBreadcrumb.vue — 去卡片化，支持根路径不渲染。
- /home/ran/gocode/metaxisdata/frontend/src/components/metadata/MetadataTabNav.vue — 并入统一 tab 体系，消除与详情组件内 tab 的重复。
- /home/ran/gocode/metaxisdata/frontend/src/components/metadata/TableMetadataDetail.vue — 详情页主体重排：合并 tab、压缩 Table Info、删除内层 `max-h-[calc(100vh-16rem)]` 滚动。
- /home/ran/gocode/metaxisdata/frontend/src/components/metadata/MetadataHistorySection.vue — 去掉自带 tab 行与重复标题，改为父级 tab 下的面板。
- /home/ran/gocode/metaxisdata/frontend/src/components/metadata/TableLineageSection.vue — 空态压缩为一行，`w-80` 搜索框改自适应。
- /home/ran/gocode/metaxisdata/frontend/src/pages/MetadataBrowserPage.vue — 拆分详情态与列表态的 chrome，移除详情态 H1 与页面级搜索条，修掉根路径空面包屑，去掉 `max-h-[calc(100vh-16rem)]`。
- /home/ran/gocode/metaxisdata/frontend/src/pages/HomePage.vue — 接 `PageHeader`，清理空态 `p-16` / `p-8` 与重复的统计卡。
- /home/ran/gocode/metaxisdata/frontend/src/pages/InstanceManagementPage.vue — 接 `PageHeader`、抽 `<EmptyState>`、删 `.slide-*` 死 CSS。
- /home/ran/gocode/metaxisdata/frontend/src/pages/DatabaseManagementPage.vue — 接 `PageHeader`、删 vestigial `justify-between`、修实例名重复渲染。
- /home/ran/gocode/metaxisdata/frontend/src/pages/ExplainSQLPage.vue — 硬编码 380px 左栏改可折叠/可拖拽，右侧空态收敛。
- /home/ran/gocode/metaxisdata/frontend/src/pages/openlineage/OpenLineageOverviewPage.vue — 用真实数据替换 4 张重复侧栏的导航卡。
- /home/ran/gocode/metaxisdata/frontend/src/components/openlineage/OpenLineageSectionHeader.vue — 并入统一 `PageHeader`，去掉重复的 Ingestion Settings 按钮。
- /home/ran/gocode/metaxisdata/frontend/src/pages/settings/GeneralSettingsPage.vue — 4 张 Card 合并为分组表单，收敛 loader / Save / readOnly 提示。
- /home/ran/gocode/metaxisdata/frontend/src/pages/settings/ — 需要一个设置页二级导航（新增或复用布局）。
- /home/ran/gocode/metaxisdata/frontend/src/store/modules/app.ts — `collapsedSections` 默认策略改为只展开当前分组；接上 `theme`。
- /home/ran/gocode/metaxisdata/frontend/src/router/index.ts — `/settings` 的二级路由结构；`/metadata` 详情态与列表态的 chrome 区分。
- /home/ran/gocode/metaxisdata/frontend/src/components/common/ — 新增 `PageState.vue` 与 `EmptyState.vue`。
- /home/ran/gocode/metaxisdata/frontend/src/locales/{en-US,zh-CN}.json — 所有新增/改名文案，改完跑 `pnpm --dir frontend i18n:sort`。

**Verification**

1. 内容占比验收：在 1440×900 视口下，`/metadata/<table>?metaType=4` 的首条 column 行 Y 坐标 ≤ 280（当前 669），视口内可见元数据区面积占比 ≥ 55%（当前 19.6%）。
2. 列表页验收：`/metadata`、`/metadata/<instance>`、`/metadata/<db>`、`/metadata/<schema>` 首条数据行 Y 坐标 ≤ 220（当前 333–346）。
3. 骨架验收：桌面端（`lg:` 及以上）不存在顶栏，全部纵向空间从 y=0 起算；侧栏可见顶层项目 ≤ 5，默认只展开当前所在分组。
4. 滚动验收：详情页只有**一个**滚动容器，`document.querySelectorAll('main *')` 中不存在 `scrollHeight > clientHeight` 的嵌套滚动元素。
5. 用户菜单验收：侧栏展开态与折叠态（icon rail）下，用户菜单都能打开并包含 Profile / Language / Theme / Logout 四项；语言切换后界面文案与 `app.ts` locale 同步持久化；Theme 切换实际生效。
6. 响应式验收：820px 宽下侧栏为 overlay drawer 而非常驻，主内容区可用宽度 ≥ 780px；Columns 表不产生横向溢出，Lineage 标题不折行超过 2 行。
7. 空态验收：根路径 `/metadata` 不再渲染空面包屑卡片；Lineage 无关系时渲染单行空态而非五件套。
8. 一致性验收：全站无手写页面级 `<h1>`（一律经 `PageHeader`）；全站无 `max-h-[calc(100vh-16rem)]`；section 标题使用 `<h2>/<h3>`。
9. 导航冗余验收：`/openlineage/overview` 不含与侧栏重复的纯跳转卡片，页面存在真实数据行。
10. 回归验收：`pnpm --dir frontend biome:check`、`pnpm --dir frontend lint`、`pnpm --dir frontend i18n`、`pnpm --dir frontend type-check`、`pnpm --dir frontend test run` 全部通过。

**Decisions**

- 顶栏策略：桌面端完全移除顶栏（Option A）。品牌与折叠按钮进侧栏顶部，用户 / i18n / 主题进侧栏底部；移动端保留细栏 + drawer。理由：顶栏 56px 只为承载品牌与两个控件，收益远低于其在每一页造成的固定损耗。
- i18n 策略：语言切换是「设置一次就不再动」的低频操作，当前以常驻文字按钮形式占据顶栏主要视觉权重，与其使用频率不匹配，因此收进用户菜单二级项。
- 用户菜单位置：放侧栏底部而非顶栏右侧。折叠态必须退化为头像按钮 + 完整 popover，不能因折叠丢失功能。
- 搜索策略：页面级全宽元数据搜索条改为全局 ⌘K 命令面板。搜索是跨页能力，不应在详情页继续占用 42px 并触发无谓请求。
- 设置导航策略：设置由全局导航降级为「入口 + 页内二级导航」。9 项低频配置不应在每一次页面访问时都占用侧栏可见行。
- 详情页标题策略：详情态不渲染页面级 H1，实体名即标题；面包屑承担定位职责。当前 H1 + 面包屑 + 卡片标题三层重复同一实体。
- 详情页 tab 策略：单一 tab 体系（Overview / Columns / Indexes / Lineage / History），禁止 tab 套 tab。
- 滚动策略：全站单一滚动容器 + sticky page header/tabs。禁用 `max-h-[calc(100vh-16rem)]` 这类硬编码视口预留。
- 布局宽度策略：列表与表单类页面 `max-w-[1400px] mx-auto`，详情与宽表页面允许全宽。当前全站无宽度约束。
- 分阶段策略：Phase 0（骨架）与 Phase 1（元数据详情页）优先，因为二者直接解决本次报告的核心问题；Phase 2/3 为一致性与可维护性收益。

**Implementation Status — Phase 0 + Phase 1 (done)**

Phase 0 与 Phase 1 已实现，实测结果（1440×900）：

| 指标 | 改造前 | 目标 | 实测 |
| --- | --- | --- | --- |
| 详情页首条 column 行 | y=669 (74.3%) | ≤280 | **y=264 (29.3%)** |
| `/metadata` 根首条数据行 | y=333 | ≤220 | **y=200 (22.2%)** |
| `/metadata/<db>` 列表首条数据行 | y=346 | ≤220 | **y=217 (24.1%)** |
| 详情页嵌套滚动容器 | 1 个（632px 被折叠） | 0 | **0** |
| 侧栏可见顶层项 | 19 行 / 3 分组全展开 | ≤5 | **5 行 / 分组默认收起** |
| 永久全局 chrome 面积 | 24.1% | ~15% | **~17.8%（仅侧栏）** |
| 820px 宽主内容可用宽度 | 564px | ≥780px | **820px** |

实现中的偏离与理由：

- **详情页 tab 集合**：实际实现为 `Columns | Indexes | Lineage | [Foreign Keys] | [Check Constraints] | [Partitions] | History`，**默认落在 Columns**，而非本方案原定的 `Overview | Columns | Indexes | Lineage | History`。原因：Table Info 的非零值已并入标题下的 meta 行，Overview 作为独立 tab 已无内容；且若 Columns 不是默认 tab，用户最关心的内容反而要多点一次，与本方案的初衷相悖。条件性 tab（外键 / 检查约束 / 分区）只在数据存在时出现。
- **用户菜单项**：去掉了 `Profile`（原 `handleProfile()` 是无实现的 TODO），菜单实际为「身份信息 + Language ▸ + Theme ▸ + Logout」。相应地 Verification 第 5 条的 "Profile" 未保留。
- **Theme**：按三态 Light / Dark / System 实现。`html.dark` 令牌在 `main.css` 中早已存在但此前无任何入口，现已接通并在首屏挂载前应用，避免主题闪烁。
- **移动端细栏高度**：实现为 48px（不是 56px），只放 drawer 触发按钮与品牌。
- **设置二级导航形态**：`md` 及以上为左侧竖排（`md:w-52`），`md` 以下退化为自动换行的横向标签。`/settings` 父路由 redirect 到 `GeneralSettings`。
- **`PageState` / `EmptyState`**：原列为 Phase 2，但因 Lineage 空态与元数据列表空态需要而提前落地；其余 15+ 处 `p-8` 复制粘贴块仍待 Phase 2 统一替换。
- **Phase 2 未做的部分**：`OpenLineageOverviewPage` 仍是纯导航卡（需先确定聚合查询的数据来源）；`GeneralSettingsPage` 的 4 张 Card 尚未合并；`AppSidebar` 之外的页面（`HomePage`、`InstanceManagementPage`、`DatabaseManagementPage`、`ExplainSQLPage`）尚未接入 `PageHeader`。
- **全局 ⌘K 搜索面板未实现**：详情页的搜索条已按方案移除（列表页保留），但替代的全局命令面板尚未落地，属 Phase 2。

**Follow-up fixes — 大屏与折叠态布局 (done)**

首轮实现后暴露了三个与宽度约束相关的缺陷，均已修复：

1. **滚动条被关在内容列里**。原实现把 `max-w-[1400px] mx-auto` 直接加在滚动容器 `<main>` 上，导致在 2560px 宽屏下 `main` 只有 1400px 宽、滚动条停在 x=2012，其右侧留下 548px 死区。改为：**滚动容器始终铺满剩余宽度（滚动条落在窗口右缘），由内层 wrapper 约束并居中内容**；`contentWidth: "full"` 的页面完全不加 wrapper，从而 `h-full` 语义与改造前逐字一致（`ExplainSQLPage` 实测仍为 852px 满高）。修复后 `gutterRightOfScrollbar = 0`（2560 / 1440、展开 / 折叠四种组合均验证）。
2. **数据表页面被误判为窄栏**。`/instances`、`/instances/:id`、`/databases`、`/manual-sql` 是宽表页面，按本方案自己的 Decision（「详情与宽表页面允许全宽」）本应全宽，首轮漏标而落到了 1400px 上限。已补标 `contentWidth: "full"`。
3. **折叠态缺少分组线索**。icon rail 隐藏了分组标题后，两个分组的成员混成一条扁平的图标流。现在 rail 模式下每个分组前用一条 `w-6` 分隔线代替标题（首组不加，因其上方已有顶层项分隔）。同时给 `Connections` / `Databases` / `Metadata` 换成 Server / Database / Table2 三个不同图标（此前三者共用 `Database`，在 rail 下无法区分）。

修复过程中另外发现并修掉一个 Phase 0 引入的回归：`SettingsLayout` 的 `<div class="min-w-0 flex-1">` 是自动高度，使 `LLMProviderManagementPage` 根节点的 `h-full` 失效（工作区高度塌成 340px）。`SettingsLayout` 改为 `min-h-full` + 内容列 `flex min-h-0 flex-1 flex-col`，该页根节点改用 `flex-1`，实测工作区恢复到 800px 满高，而 `GeneralSettingsPage` 仍随内容增长到 1446px 正常滚动。

**Follow-up fixes — 设置 section 的导航漂移与表单宽度 (done)**

用户两次反馈"设置页左侧列表位置会变化"。第一次只治了标，第二次才找到真正的根因：**横向居中（`mx-auto`）本身**。导航栏是钉在一个"居中列"的左缘上的，所以它的 x 取决于这个列的宽度，而列的宽度又取决于滚动容器当前的**内容盒宽度**——于是任何能让滚动容器宽度变化的事情都会推动导航。实测的三个症状其实是同一个根因：

1. **窗口变宽就漂移，1405px 起明显**（用户报告的原话）。`main` 内容宽 > 上限 + padding 时居中生效；此后每多一个像素，导航就右移半个像素。实测 `navX`：1400→**280**、1420→**288**、1600→**378**、1920→**538**、2560→**858**。1405 = 256（侧栏）+ 48（`p-6`）+ 1100（当时的上限）+ 1。
2. **`/settings/audit-logs` 加载时自己移动**。该页内容 900 → 2988px，加载过程中垂直滚动条出现，滚动容器 `clientWidth` 少掉一个滚动条宽度，居中列随之左移半个滚动条宽度——纯加载引起的横向跳动，与用户操作无关。
3. **切换设置页时位置不同**。各页内容高度不同（environments 900px 无滚动条；general 1446、audit-logs 2988 有滚动条），所以每次切换都相当于第 2 条的复现。

第一次修复（统一宽度到 1100px）只消除了"逐路由宽度不一致"这一层，没有触及居中本身，所以用户仍能在 1405px 以上复现——这是当时判断失误。

**最终修复**：不再居中。导航是 chrome，钉死在内容区左缘；只有阅读列设上限。

```vue
<div class="flex min-h-full w-full flex-col gap-4 md:flex-row md:gap-6">
  <SettingsNav class="md:sticky md:top-0 md:w-52 md:shrink-0 md:self-start" />
  <div class="flex min-h-0 w-full max-w-[860px] min-w-0 flex-1 flex-col">
    <router-view />
  </div>
</div>
```

同时在滚动容器上加 `[scrollbar-gutter:stable]`：无论滚动条是否出现都预留轨道，`clientWidth` 恒定，从根上消除"内容变高导致横向位移"。这一条不只保护设置section，也保护仍然居中的通用页面（`/`）和全宽表格页。

验证：

- `navX` 在 1200 / 1400 / 1405 / 1420 / 1500 / 1600 / 1920 / 2200 / 2560 / 3200 **全部为 280**（此前从 1405 起单调右移）。
- 9 个设置页在 1920px 下 `navX=280, navY=24` 完全一致，且其中滚动高度 900 / 1446 / 2988 三种并存——证明滚动条的有无不影响位置。
- 逐帧追踪 `/settings/audit-logs` 的加载过程：142 帧、内容高度走过 1446→900→2988，`distinctNavX=[280]`、`distinctNavY=[24]`，**零位移**。

同期的另两项：

- **纵向漂移**：窗口本身不滚动，滚动的是 `<main>`，vue-router 的 scroll restoration 够不到它。此前"有时会归零"只是因为异步组件切换瞬间内容塌缩把 `scrollTop` 夹到 0——依赖时序，不可靠。修复：`DefaultLayout` 显式监听 `route.path` 把 `main` 滚回顶部（只认 path 不认 query，避免切 tab、定位字段时把视图拽走）。
- **设置导航 sticky**：长表单（Audit Logs 2988px）此前会把导航整个滚出视野。`md:sticky md:top-0 md:self-start`——`self-start` 必需（被 stretch 的 flex item 没有可粘滞空间），且必须 `top-0`：滚动容器的粘滞基准是内容边，`top-6` 会把导航压到 y=48 而标题在 y=24。
- **表单宽度收紧**：阅读列上限 860px（卡片实测 860、输入框 810，此前 1400px 下输入框 1168）。

**Follow-up fix — 折叠态侧栏的对齐 (done)**

用户反馈折叠后"展开侧栏"图标与其它图标不对齐。实测：侧栏中线在 x=32，品牌方块与所有导航图标都在 **31.5**，而折叠/展开按钮在 **39**，偏右 7.5px。根因是展开态用来把按钮推到右侧的 `ml-auto`：rail 模式下这一行变成 `flex-col`，而**在列向 flex 容器里 auto 外边距会吸收剩余空间并覆盖 `items-center`**，于是按钮被顶到右缘。修复：`ml-auto` 改为仅非 rail 时生效（`rail ? '' : 'ml-auto'`）；同时把 rail 下的按钮图标从 16px 提到 20px，与导航图标同尺寸。修复后四个中心点全部为 31.5，`marginLeft` 计算值 0px；展开态（`marginLeft` 仍为 auto、图标 16px）与移动端抽屉（行向 + auto）行为均未变。

**Implementation Status — Phase 2 (done)**

| Step | 内容 | 状态 |
| --- | --- | --- |
| 6 | `OpenLineageOverviewPage` 填充真实数据 | 完成 |
| 7 | `GeneralSettingsPage` 合并分组表单 | 完成 |
| 8 | `PageState` / `EmptyState` 铺开 + 死代码清理 | 完成 |
| 3 | `PageHeader` 全站铺开 | 完成 |
| — | 全局 ⌘K 命令面板 | **未做**（见 Further Considerations） |

**6. OpenLineage Overview — 从"整页导航"变成真实总览。** 原页面的 4 张卡片全部是跳转按钮，100% 重复侧栏且零数据。现在只用已有 RPC（`ListOpenLineageRuns` / `ListOpenLineageTasks` / `ListOpenLineageDatasets`，均为 `event_time DESC`）在客户端聚合成四张指标卡（Jobs / Datasets / 最近 Runs / 最近事件）+ 最近 Runs 表（10 行，含事件类型状态徽章）+ 最近活跃作业表（5 行），并各自链到完整目录。**没有新增后端 RPC**——这是当时把该页推迟到 Phase 2 的主要未知项，实测现有 RPC 足够。零数据时退化为一个接入引导空态（这正是该页原先的全部内容）。两种状态均已实测：空态走真实 dev 库（无 OL 数据）；有数据态通过临时 stub 三个 RPC 响应验证（未污染数据库）。指标口径保持诚实：卡片标注为"可见"数量而非总数，因为列表 RPC 只返回 `next_page_token` 而无数值总数。

**7. GeneralSettings — 一个表单、一个 loader、一次保存。** 三个独立 Save 按钮分别提交同一资源的不同字段掩码，已合并为一次 `updateWorkspaceProfileSetting`（7 个字段的并集掩码）。加载合并为一个 `PageState`；Save 收敛到 sticky 页脚并新增未保存提示。实测：保存按钮 3 → **1**，scrollHeight 1446 → **1323**，滚动 423px 后 Save 仍固定在视口底部（top=824），改动任一字段即出现"有未保存的更改"。Debug mode 刻意不并入：它走独立 RPC 且立即生效。Profile 列表单独失败不再拖垮整页（调用方可能没有 `llm.profiles.list`），降级为卡片内提示。

**8 + 3. 原语铺开与死代码。** `PageHeader` 覆盖全部 15 个路由页（含原先无标题的 `/settings/llm-providers`，它是唯一连 h1 都没有的页面）；h1 字号全站统一为 20px（原 24px），操作按钮位置统一。`PageState`/`EmptyState` 替换了 13 个页面 + 10 个列表组件里复制粘贴的 loading/error/empty 块。死代码清理：`InstanceManagementPage` 与 `UserManagementPage` 各有一整块未被引用的 `.slide-*` scoped CSS（后者是执行时新发现的）；`DatabaseManagementPage` 单子元素 `justify-between`；`MetadataList` 未使用的 `currentGuid` prop；`getEngineBadgeVariant` 空转参数；实例名在 title 缺失时的重复渲染。

**顺带修掉一个同类的路由缺陷。** Phase 2 梳理时发现 `/openlineage/overview` 是唯一没有 `contentWidth: "full"` 的 OpenLineage 非重定向路由——即"落地页比它链向的每一页都窄"，与用户先前报告的设置页漂移是同一类问题。已把 `/openlineage` 改为带 children 的父路由并让标志只在父级声明一次，与其他 6 个子页彻底同步（实测全部 `capped=false`）。至此全站只有 `/` 仍是受限宽度的页面。

**Further Considerations**

1. 品牌名是否走 i18n。目前 `MetaxisData` 硬编码在 `AppHeader`，移入侧栏顶部时可一并纳入 locale catalog；但产品名通常不翻译，建议保持硬编码并在 i18n 检查脚本中显式豁免，避免产生 `brand.name` 这类无意义键。
2. 侧栏折叠按钮的位置。放品牌行右侧（顶部）还是侧栏最底部与用户菜单相邻，取决于后续是否需要「侧栏固定 / 悬浮」模式；MVP 建议放品牌行右侧。
3. ⌘K 命令面板的落地范围。最小可用版本只复用现有 `MetadataBrowserPage` 的元数据搜索能力；若后续要覆盖「跳页面 + 跳实例 + 执行动作」，需要独立设计其数据来源与权限过滤。
4. 设置页二级导航形态。左内嵌导航（200px）还是顶部 tab，取决于设置项未来的增长；若维持在 9 项且分组清晰，顶部 tab 更省纵向空间。
5. `OpenLineageOverviewPage` 的数据来源。填充真实内容需要 run / job / dataset 的聚合查询，需确认复用现有 `openlineage` store 查询还是新增聚合 RPC；这会影响该页是否落在 Phase 2 内完成。
6. 移动端 drawer 的交互细节。是否需要手势（右滑）、是否在路由变更后自动关闭、遮罩层与焦点管理，建议在设计实现时单独确认。
7. 本方案与 `spec/openlineage_update.md` 的关系。后者已提出「顶部统一保留面包屑或返回上下文」与「详情优先用抽屉保留上下文」；本方案在骨架与页头层面与之对齐，OpenLineage 各页的具体 PRD 仍以后者为准。
