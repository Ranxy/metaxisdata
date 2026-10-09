# 前端布局与血缘展示 — Reference

> Status: **implemented**。骨架、设置 section、OpenLineage 页面、血缘视觉编码均已落地；唯一未做项是全局 ⌘K 命令面板。本文件是 `frontend/src/{layouts,components/layout,pages/settings,pages/openlineage,components/lineage,lib,utils}/` 的布局与信息架构参考（`frontend/AGENTS.md` 是代码规则，本篇不重复）。
> Related: [frontend/AGENTS.md](frontend/AGENTS.md)（前端代码规则，本篇不重复）、[scripts/audit-ui-layout.mjs](frontend/scripts/audit-ui-layout.mjs)（布局门禁）。

## What it is

三个合成一个"内容优先"契约：**骨架**上桌面无顶栏、侧栏 5 个顶层分组、设置降级为页内二级导航、页面标题统一走 `PageHeader`；**OpenLineage 信息架构**分目录层（选对象）/ 关系层（解释依赖）/ 证据层（建立信任）；**血缘图编码**节点按实例、边按来源。

## Decisions

| 决定 | 理由 |
| --- | --- |
| 桌面端完全移除顶栏（品牌/折叠进侧栏顶部，用户/语言/主题进侧栏底部）；移动端留 48px 细栏 + drawer | 56px 顶栏只为品牌与两控件，收益低于每页固定损耗 |
| 侧栏顶层收为 5 项：首页 / Explain SQL / 数据源 / OpenLineage / 设置；设置降级为"入口 + 页内二级导航"（9 项） | 19 项全展开时导航压过内容；低频配置不该占侧栏可见行 |
| 统一 `PageHeader` 原语（面包屑可选 / 标题 / 描述 / `#actions`）；详情态不渲染页面级 H1，实体名即标题，面包屑定位 | 全站 h1 字号与操作按钮位置一致；消除 H1 + 面包屑 + 卡片标题三层重复 |
| 单一 tab 体系，禁止 tab 套 tab | 消除 `Table Details\|History` 与内层 `History\|Version Diff` 的重复 |
| 全站单一滚动容器 + sticky 页头/tabs；禁用 `max-h-[calc(100vh-16rem)]` | 原先是卡片内层 + `main` + `window` 三层滚动 |
| 宽度：列表/表单页限宽居中（1400px），详情与宽表页 `contentWidth: "full"`；设置导航**不居中**，钉死在内容区左缘，只有阅读列（860px）设上限 | 原先全站零 `max-w-*`；居中列的 x 取决于滚动容器内容盒宽度，窗口变宽/滚动条出现都会推动导航 |
| 语言/主题收进用户菜单二级项（`Language ▸` / `Theme ▸`），`Profile` 因无实现移除 | set-once 偏好不该占顶栏主要视觉权重 |
| 页面级元数据搜索条改为全局 ⌘K 命令面板（**未实现**） | 搜索是跨页能力，不该在详情页占 42px 并触发无谓请求 |
| OpenLineage 命名统一为 Jobs / Datasets / Events / Overview（内部沿用 task/run）；目录页资产详情优先用抽屉保留上下文，Run 详情保留整页 | 避免后端大规模重命名；Run 承担证据页职责 |
| Column Lineage 先做分组字段关系页，Dashboard 不阻塞主链路 | 字段规模上升后再迭代图谱 |

## 骨架与布局机制

| 机制 | 实现 |
| --- | --- |
| 单一滚动容器 | `<main class="min-h-0 flex-1 overflow-y-auto [scrollbar-gutter:stable]">` 是壳里唯一滚动区；页面不得嵌套自己的 scroller；`DefaultLayout` 监听 `route.path`（不认 query）把 `main` 滚回顶部（[DefaultLayout.vue:32](frontend/src/layouts/DefaultLayout.vue#L32)） |
| 宽度 | `route.meta.contentWidth !== "full"` 时才包 `mx-auto w-full max-w-[1400px]`；全宽页不加 wrapper 以保住 `h-full` 工作台（[DefaultLayout.vue:61](frontend/src/layouts/DefaultLayout.vue#L61)） |
| 桌面无顶栏 | `AppHeader` 只在 `lg:hidden` 渲染（48px，drawer 触发 + 品牌）（[AppHeader.vue:6](frontend/src/components/layout/AppHeader.vue#L6)） |
| 设置 section | [`SettingsLayout.vue`](frontend/src/pages/settings/SettingsLayout.vue)：导航 `md:sticky md:top-0 md:w-52 md:self-start`（`self-start` 必需，否则被 stretch 的 flex item 无可粘滞空间），内容列 `max-w-[860px]`，`settingsContentWidth: "full"` 的页 opt out；[`SettingsNav.vue`](frontend/src/components/layout/SettingsNav.vue) 9 项 |
| 侧栏 | [`AppSidebar.vue:248-341`](frontend/src/components/layout/AppSidebar.vue#L248-L341) 5 个顶层项，底部 `NotificationBell` + `UserMenu`（[:174-175](frontend/src/components/layout/AppSidebar.vue#L174-L175)）；折叠 rail 下每组前用分隔线代替标题 |
| 页头 | [`PageHeader.vue`](frontend/src/components/layout/PageHeader.vue)：`breadcrumb` slot + `h1.text-xl` + 描述 + `actions` slot；被 19 个 `.vue` 引用 |
| 路由 | 设置二级导航挂在 `/settings` children（[router/index.ts:52-142](frontend/src/router/index.ts#L52-L142)），`/settings` redirect 到 GeneralSettings；OpenLineage 父路由统一 `contentWidth: "full"`（[:166-217](frontend/src/router/index.ts#L166-L217)） |

## OpenLineage 页面

| 层 | 路由 → 页面文件 | 说明 |
| --- | --- | --- |
| 总览 | `/openlineage/overview` → `OpenLineageOverviewPage.vue` | 真实数据：指标卡（Jobs / Datasets / 最近 Runs / 最近事件）+ 最近 Runs 表 + 最近活跃作业表；零数据退化为接入引导空态。**无新增后端 RPC**，用已有 `ListOpenLineageRuns` / `Tasks` / `Datasets` 客户端聚合；卡片标注"可见"数量而非总数（列表 RPC 无总数） |
| 目录 | `/openlineage/jobs` → `OpenLineageRunsPage.vue`（页面名 `OpenLineageTasks`）；`/openlineage/datasets` → `OpenLineageDatasetsPage.vue`（详情用 `OpenLineageDatasetDetailDrawer.vue`，Dialog 形态抽屉，保留目录页上下文） | 承接 task/dataset 聚合 |
| 调查 | `/openlineage/jobs/:guid` → `OpenLineageTaskDetailPage.vue` | Job Detail |
| 证据 / 关系 | `/openlineage/events` → `OpenLineageEventsPage.vue`；`/events/:guid` → `OpenLineageRunDetailPage.vue`（保留原始 JSON、facets、Airflow 链接、相关 job/dataset）；`/column-lineage/:guid+` → `OpenLineageColumnLineagePage.vue`（URL 携带 `column`；`:guid+` 重复段让含 `/` 的对象名存活） | 证据层 + 字段关系 |

共享头 `components/openlineage/OpenLineageSectionHeader.vue` 并入 `PageHeader`。**跨页上下文已实现**：`from` query 在 Job Detail / Run Detail / Column Lineage 之间往返（`goBack()` 优先回 `from`），列表筛选写进 URL 并可读回（`queryFilters()` ← `route.query[key]`，`router.replace({ query: nextQuery })`），`lineageOnly` / `columnLineageOnly` / `search` 同样持久化。

## 血缘展示的视觉编码

| 问题 | 信号 | 表现 |
| --- | --- | --- |
| 实例归属 | GUID 第一段 = 实例 resource id；实例名来自 `ListInstances`（`useInstanceStore` 已缓存，血缘响应不重复下发） | 节点左侧 4px 色条 + 表头色点 + 表头实例名，同实例同色；标题只放对象名，第二行 `database.schema`，完整名 `实例 · 路径.对象` 在 tooltip；MiniMap 同色 |
| 血缘来源 | `LineageRelation.metaType`：`OPENLINEAGE`(100) = 摄入写入，其余 = 分析器读 SQL 定义写入 | `SQL 解析` 蓝色实线、`OpenLineage` 紫色虚线、同一对象对两者都写 = `mixed` 第三色点线；所有边带箭头，方向不只靠动画 |

- **色条用 `border-l-4` 不用绝对定位色块**：节点不能 `overflow-hidden`，否则骑在边框上的连接点 Handle 会被裁一半、拖拽热区减半（[LineageNode.vue:9-11](frontend/src/components/lineage/LineageNode.vue#L9-L11)）。
- **线型是颜色的备份**：`LINEAGE_ORIGIN_DASH = { sql: undefined, openlineage: "7 5", mixed: "2 3" }`（[lineageOrigin.ts:38-42](frontend/src/lib/lineageOrigin.ts#L38-L42)），灰度/色盲下可辨。
- **对比度**：浅色主题所有 accent 对画布 ≥3:1（WCAG 图形下限），三个来源色额外 ≥4.5:1（来源徽标当 12px 文字用）。未高亮的边用 **0.85** 透明度，可见性由 token 承担而非靠透明度兜底（[lineageGraph.ts:268-275](frontend/src/lib/lineageGraph.ts#L268-L275)）。
- **图例即过滤器、默认折叠**（`collapsed = ref(true)`，[LineageLegend.vue:154](frontend/src/components/lineage/LineageLegend.vue#L154)）：展开占画布 8.8% 会盖住节点，折叠只占 1.4% 且仍回答"哪种颜色对应哪种来源"。展开列出实例（名称 + 对象数）与两种来源（线型样例 + 边数），点来源行即隐藏该来源的边；`mixed` 是合体、无独立过滤语义，渲染成不可点击的一行；切换对象或 Reset 都恢复全选，否则新对象会看起来"没有血缘"。
- **调色板**：`--lineage-scope-1..6` 与 `--lineage-sql`/`-openlineage`/`-mixed` 定义在 [`main.css`](frontend/src/assets/styles/main.css)。实例颜色按"实例 id 排序 → 外部 namespace 排序"分配，展开出外部数据集时已上屏的实例不换色；排序用 `compareScopeKeys`（**码点序**而非 locale 序），避免不同语言浏览器给出不同配色（[lineageAsset.ts:155](frontend/src/utils/lineageAsset.ts#L155)、[:202-205](frontend/src/utils/lineageAsset.ts#L202-L205)）。

文件职责：

| 文件 | 职责 |
| --- | --- |
| `src/utils/lineageAsset.ts` | 纯函数：GUID/外部数据集 → `{scopeKey, scopeLabel, name, qualifier, fullLabel}`；`buildLineageScopeColors()` 同时服务画布与关系表 |
| `src/lib/lineageOrigin.ts` | 纯函数：`relationOrigin(rel)` 由 `metaType` 判来源；来源色/线型/文案键；按来源统计关系数 |
| `src/lib/openlineageRun.ts` | 解析 `openlineage:run:<jobType>:<namespace>:<job>:<runId>`，给出 job/run 标签 |
| `src/lib/lineageGraph.ts` | 建边：按对象对聚合来源、着色/线型/箭头、`originFilter` 过滤；`nodeHeight()` 只是首猜 |
| `src/components/lineage/LineageNode.vue` | 节点：实例色条/实例名/对象名/限定路径/类型徽标/度数/折叠字段列表 |
| `src/components/lineage/LineageLegend.vue` | 图例 + 来源过滤（至少保留一个来源，否则画布无剩余行可点回） |
| `src/pages/LineageGraphPage.vue` | 组装：实例缓存、调色板、图例数据、详情面板、MiniMap 配色；节点高度实测与重叠 |
| `src/components/metadata/TableLineageSection.vue` | 关系表：Related Object 加实例色点 + `实例 · 路径` 副行；`Lineage source` 列（来源徽标 + job 名）；总数与表格同一范围 |
| `src/pages/openlineage/OpenLineageColumnLineagePage.vue` | 字段血缘：标题与关系卡片用实例限定名，卡片补来源徽标 |

**节点高度：先猜后实测（反直觉，必须保留）。** `nodeHeight()` 只能猜——操作行是否折行取决于语言和该节点提供哪些操作（实测带三个操作的节点 164px，而布局一直按 120px 排，每节点叠约 20px 正好吃掉行间隙，表现为"下面节点压住上一个底部"）。实现：`rebuildGraph()` 先按猜测排一次，再 `settleNodeHeights()` 用渲染出的 `.vue-flow__node[data-id]` 的 **`offsetHeight`**（布局尺寸，不受画布缩放影响）重叠；实测值存进 `renderedHeights` 供下次直接用；展开字段列表时该节点实测值失效、退回猜测再重测；循环最多 **4 轮**，位置不影响高度，一至两轮即收敛。**Vue Flow 的 `node.dimensions` 不能作布局依据**：卡片原地长高后它不更新（[LineageGraphPage.vue:1305-1404](frontend/src/pages/LineageGraphPage.vue#L1305-L1404)）。

## Invariants

| 规则 | 破坏后果 |
| --- | --- |
| 桌面端（`lg:` 及以上）不得出现顶栏；页面标题一律经 `PageHeader`，heading 不得跳级（h1→h2→h3） | 每页损失 48–56px；`audit:ui` 判 `h1Count !== 1` 或 heading skip（`CardTitle` 为此改为 `<h2>`，偏离上游 shadcn，组件内注明） |
| 全站只能有一个滚动容器，页面不得嵌套 scroller；禁用 `max-h-[calc(100vh-16rem)]` 一类硬编码视口预留 | 三层滚动上下文；`audit:ui` 的 `nestedScrollers !== 0` 判失败（当前全站 0 处 calc） |
| 滚动容器始终铺满剩余宽度、由内层 wrapper 约束并居中；`contentWidth: "full"` 只给宽表页与全高工作台 | 滚动条停在居中列右缘、宽屏右侧留死区；反之阅读行长度失控或把宽表列推入横向滚动条 |
| 设置导航钉在内容区左缘、只给阅读列设上限；滚动容器保留 `scrollbar-gutter: stable`；sticky 导航必须 `md:top-0 md:self-start` | 窗口变宽或滚动条出现会推动导航（"设置页列表位置变化"的根因）；被 stretch 的 flex item 无可粘滞空间，`top-6` 会把导航压到 y=48 而标题在 y=24 |
| 节点颜色按实例、边颜色按来源；颜色之外必须有线型；未高亮边不得低于 0.85 透明度 | 同名跨实例对象渲染一致、两种写者同色；跌破 WCAG 图形下限后灰度下不可辨 |
| 图例默认折叠；实例颜色排序必须用码点序（`compareScopeKeys`） | 展开占画布 8.8% 会盖住节点使其既看不见也点不到；不同语言浏览器给出不同配色 |
| 节点高度必须"先猜后实测"，实测值随展开字段失效；不得改用 Vue Flow `dimensions` | 节点互相压盖（20px 行间隙被吃掉） |
| 跨页跳转尽量保留来源与筛选（`from` + 列表筛选写入 URL） | 调查链路断裂，返回后丢筛选条件 |

## Failure modes

| 场景 | 行为 |
| --- | --- |
| 浏览器画完前第一次测量 | `measureDrawnNodes` 最多 10 次重试（`nextTick` + 25ms），拿不到返回 `null`，`settleNodeHeights` 保持猜测布局（不阻塞渲染） |
| 展开/收起某节点字段列表 | 该节点实测高度失效 → 退回猜测 → 再次实测重叠；其余节点沿用缓存实测值 |
| 血缘响应没有实例信息 | 实例名走 `useInstanceStore` 缓存 + `ensureLoaded()`，不在血缘响应里重复下发 |
| 图例把某来源全关掉 | 组件保证至少保留一个来源，否则画布无剩余行可点回 |
| Overview 无数据 / OpenLineage 列表 RPC 无总数 | 退化为接入引导空态；卡片标注"可见"数量而非总数，不谎报总量 |

## Open items

1. **全局 ⌘K 命令面板未实现**：详情页搜索条已移除（列表页保留），替代品未落地。最小版本只复用元数据搜索；覆盖"跳页面 + 跳实例 + 执行动作"需独立设计数据来源与权限过滤。
2. 品牌名（`MetaxisData`）是否走 i18n：`AppHeader` 里硬编码，移入侧栏顶部时未纳入 locale；建议保持硬编码并在 i18n 检查脚本显式豁免，避免 `brand.name` 这类无意义键。
3. 侧栏折叠按钮位置（品牌行右侧 vs 底部）取决于是否需要"侧栏固定/悬浮"；当前在品牌行。
4. 移动端 drawer 交互细节（手势、路由变更后自动关闭、遮罩与焦点管理）未单独确认。
5. 血缘图 100+ 节点性能是外推而非实测（开发库单图最大约 44~49 节点）。

## Where things live

- 骨架与布局：[DefaultLayout.vue](frontend/src/layouts/DefaultLayout.vue)、[PageHeader.vue](frontend/src/components/layout/PageHeader.vue)、[AppSidebar.vue](frontend/src/components/layout/AppSidebar.vue)、[AppHeader.vue](frontend/src/components/layout/AppHeader.vue)、[UserMenu.vue](frontend/src/components/layout/UserMenu.vue)、[SettingsNav.vue](frontend/src/components/layout/SettingsNav.vue)、[SettingsLayout.vue](frontend/src/pages/settings/SettingsLayout.vue)；路由 [router/index.ts](frontend/src/router/index.ts)（设置 `:52-142`，OpenLineage `:166-217`）。
- OpenLineage：[pages/openlineage/](frontend/src/pages/openlineage/)、[components/openlineage/](frontend/src/components/openlineage/)。血缘：[utils/lineageAsset.ts](frontend/src/utils/lineageAsset.ts)、[lib/lineageOrigin.ts](frontend/src/lib/lineageOrigin.ts)、[lib/lineageGraph.ts](frontend/src/lib/lineageGraph.ts)、[lib/openlineageRun.ts](frontend/src/lib/openlineageRun.ts)、[components/lineage/](frontend/src/components/lineage/)、[pages/LineageGraphPage.vue](frontend/src/pages/LineageGraphPage.vue)、[components/metadata/TableLineageSection.vue](frontend/src/components/metadata/TableLineageSection.vue)。
- 令牌与主题：[assets/styles/main.css](frontend/src/assets/styles/main.css)、[store/modules/app.ts](frontend/src/store/modules/app.ts)（`theme` / `collapsedSections` / `locale`）。无功能开关；布局由 `route.meta.contentWidth` / `settingsContentWidth` 驱动，主题由 `applyTheme` 在首屏挂载前应用。
- 门禁：[scripts/audit-ui-layout.mjs](frontend/scripts/audit-ui-layout.mjs) → `pnpm --dir frontend audit:ui`（[package.json:21](frontend/package.json#L21)）。CDP 直连浏览器，不引入 puppeteer/playwright；量 `tableTopY`、`firstDataRowY`、`h1Count`、`nestedScrollers`、`clippedTableCells`、横向溢出、heading 跳级、console 错误；退出码 **0** 全部达标 / **1** 有路由越界 / **2** 无法运行。当前 **15** 条路由，预算按 `tableTopY`（表格顶部；表头属数据呈现，不算 chrome）。
- 测试：[utils/lineageAsset.test.ts](frontend/src/utils/lineageAsset.test.ts)、[utils/lineageTokens.test.ts](frontend/src/utils/lineageTokens.test.ts)（钉住 `main.css` 令牌名，防止 `hsl()` 静默失效）、[lib/lineageOrigin.test.ts](frontend/src/lib/lineageOrigin.test.ts)、[lib/lineageGraph.test.ts](frontend/src/lib/lineageGraph.test.ts)、[lib/openlineageRun.test.ts](frontend/src/lib/openlineageRun.test.ts)、[lib/lineageTrail.test.ts](frontend/src/lib/lineageTrail.test.ts)、[components/layout/AppSidebar.test.ts](frontend/src/components/layout/AppSidebar.test.ts)、[pages/openlineage/](frontend/src/pages/openlineage/) 下 `*.test.ts`；门禁命令 `pnpm --dir frontend biome:check && pnpm --dir frontend lint && pnpm --dir frontend i18n && pnpm --dir frontend type-check && pnpm --dir frontend test run`（`src/lib/`、`src/utils/` 有 95% 行 / 85% 分支门槛，`test:coverage` 检查）。
