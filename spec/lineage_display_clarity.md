# Lineage 展示可读性：实例归属与血缘来源

血缘页面要回答两个问题，改动前都答不出来：

1. **这条关系两端到底是哪个实例的哪个对象？** GUID 是 `instance;database;schema;object`，而展示只取了末尾几段（`slice(-3)`），PostgreSQL 对象因此丢掉实例段、MySQL 对象反而保留——同一套 UI 上两种拼法，且两个实例下 `db.table` 同名的对象**渲染完全一致**。`dwd_customer_360` 在 `testPG` 和 `t-star` 里各有一份，图上并排出现却看不出区别。
2. **这条关系是谁写进图里的？** 存储层同时被两个写者填充：实例内分析器读对象自身的 SQL 定义（视图 / 物化视图 / 手工 SQL）并整体替换该对象的边；OpenLineage 摄入按 run 整体替换该 run 的边。二者在图上原本同色同线型。

## 一、两个编码信号（都不需要改后端）

| 问题 | 信号 | 来源 |
| --- | --- | --- |
| 实例归属 | GUID 第一段 = 实例 resource id | `LineageRelation.sourceGuid/targetGuid` |
| 血缘来源 | `LineageRelation.metaType`：`OPENLINEAGE`(100) 表示 OpenLineage 写入，其余（TABLE/VIEW/MATERIALIZED_VIEW/MANUAL_SQL…）表示分析器写入 | `lineageRelations.metaType`，即该关系的 `meta_guid` 自身的类型 |

实例名（`testPG` 而不是 `test-pg-1`）来自 `ListInstances`，前端 `useInstanceStore` 已有全量缓存，页面 `ensureLoaded()` 即可，无需在血缘响应里重复下发。

## 二、视觉编码

- **节点 = 实例**：左侧 1px 色条 + 表头色点 + 表头实例名，同一实例的节点同色；节点标题只放对象名，第二行放 `database.schema` 限定路径，完整名 `实例 · 路径.对象` 放在 tooltip。MiniMap 沿用同一套颜色。
- **边 = 来源**：`SQL 解析` 为蓝色实线，`OpenLineage` 为紫色虚线，同一个对象对同时被两者写入时为第三色点线（`mixed`）。颜色之外还有线型，灰度/色盲下同样可辨；所有边补了箭头，方向不再只靠流动动画表达。
- **图例即过滤器**：画布左上角的图例列出当前图中的实例（名称 + 节点数）和两种来源（带线型样例 + 边数），点击来源行即隐藏该来源的边。节点位置、度数、选中态不受影响。
- **调色板**：`--lineage-scope-1..6` 与 `--lineage-sql/-openlineage/-mixed` 定义在 `main.css` 的 `:root` / `.dark`。实例颜色按"实例 id 排序 → 外部 namespace 排序"分配，因此展开图里出现外部数据集时已上屏的实例不会换色；排序用 `compareScopeKeys`（码点序而非 locale 序），避免不同语言的浏览器给出不同配色。

## 三、代码落点

| 文件 | 职责 |
| --- | --- |
| `src/utils/lineageAsset.ts` | 纯函数：GUID/外部数据集 → `{scopeKey, scopeLabel, name, qualifier, fullLabel}`；scope 调色板分配 |
| `src/lib/lineageOrigin.ts` | 纯函数：`relationOrigin(rel)` 由 `metaType` 判定来源；来源色/线型/文案键；按来源统计关系数 |
| `src/lib/openlineageRun.ts` | 解析 `openlineage:run:<jobType>:<namespace>:<job>:<runId>`，给出 job/run 标签（原先两个页面各有一份副本） |
| `src/lib/lineageGraph.ts` | 建边：按对象对聚合来源、着色/线型/箭头、`originFilter` 过滤 |
| `src/components/lineage/LineageNode.vue` | 节点：实例色条 / 实例名 / 对象名 / 限定路径 / 类型徽标 / 度数 / 折叠字段列表 |
| `src/components/lineage/LineageLegend.vue` | 图例 + 来源过滤（至少保留一个来源，否则画布无剩余行可点回） |
| `src/pages/LineageGraphPage.vue` | 组装：实例缓存、调色板、图例数据、详情面板（实例 / 完整名 / 来源分布）、MiniMap 配色 |
| `src/components/metadata/TableLineageSection.vue` | 关系表：Related Object 加实例色点 + `实例 · 路径` 副行；新增 `Lineage source` 列（来源徽标 + job 名，tooltip 给出完整 `job · run`）；总数改为与表格同一范围 |
| `src/pages/openlineage/OpenLineageColumnLineagePage.vue` | 字段血缘：标题与关系卡片改用实例限定名，卡片补来源徽标 |

## 四、顺带修掉的既有问题

- 关系表的 `Total Relations` 取的是**未按字段过滤**的全量（223），而旁边的 Upstream/Downstream 取的是过滤后（1 / 16），三个数字互相矛盾；现在都以表格列出的集合为准（157 = 88 + 69）。
- 关系计数徽标在无搜索时显示 `17 / 17`，等价于一个分数形式的自己；现在只显示 `17`，只有搜索隐藏了行才显示 `17 / 25`。
- `formatOpenLineageRunLabel` / GUID 末段取名等逻辑在三个文件里各有一份，已收敛到 `lib/` 与 `utils/`。
- 节点类型原先按 GUID "非空段数"猜测，MySQL 的空 schema 段会被算成 `schema`；改为按位置计数，并优先采用关系上报的真实 `MetaType`。

## 五、验证

- 单测：`lineageAsset` / `lineageOrigin` / `openlineageRun` / `lineageGraph` 覆盖新分支（`pnpm test:coverage` 全绿，`utils/`、`lib/` 达到 95% 行 / 85% 分支门槛）。
- 视觉：`pnpm dev` + 后端 8083，用 chrome-devtools 在 `localhost:3000` 上核对 `dwd_customer_360`（同名跨实例）、`v_customer_360`（SQL 与 OpenLineage 混排）、字段血缘页、关系表，浅色/深色与中英文均确认。
