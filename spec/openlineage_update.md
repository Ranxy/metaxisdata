## Plan: OpenLineage Page Design

建议把 OpenLineage 产品面拆成 6 个一级页面和 2 类辅助详情视图：Dashboard、Jobs、Datasets、Events、Table Lineage、Column Lineage，以及 Job Detail / Run Detail。MVP 不强制先做 Dashboard，优先保证 Jobs -> Table Lineage -> Run Evidence 和 Datasets -> Column Lineage -> Run Evidence 两条调查链路完整。整体设计遵循“目录层负责选择对象，关系层负责解释依赖，证据层负责建立信任”的原则。

**Steps**
1. Phase 1 - Navigation and IA: 将 OpenLineage 升级为主导航一级入口，二级导航包含 Overview、Jobs、Datasets、Events；Table Lineage 和 Column Lineage 作为上下文页，不必常驻主导航；Ingestion Settings 保留在 Settings 内。这个阶段先解决用户“从哪里进入调查”的问题。
2. Phase 1 - Shared design system decisions: 全部 OpenLineage 页面共用统一的时间范围控件、namespace/source 筛选、状态颜色语义、空状态和错误状态；页面顶部统一保留面包屑或返回上下文，避免调查链路断裂。这个阶段是所有页面的视觉和交互基线。
3. Phase 2 - Jobs page PRD: Jobs 页承接当前 task 聚合能力，作为主要目录页。列表列建议包括 Job Name、Namespace、Integration、Processing Type、Latest Event Time、Latest Run Status、Run Count、Lineage Run Count。支持搜索、namespace 筛选、时间范围、仅看有 lineage、按最近活跃排序。每行进入 Job Detail，也应直接提供“表级血缘”和“最近事件”快捷入口。
4. Phase 2 - Job detail PRD: Job Detail 页承接当前 OpenLineageTaskDetail 的能力，但产品定位改为调查页而非纯明细页。页面分为 Summary、Recent Runs、Related Datasets、Related Graph Entry 四区。Summary 展示 namespace、jobName、jobType、integration、processingType、parent/root job、run counters。Recent Runs 展示最近事件和状态。Related Datasets 列出最近作为输入输出出现的数据集。Graph Entry 区提供“打开表级血缘”和“打开最近一次有 lineage 的 run”两个显著 CTA。
5. Phase 2 - Events page PRD: Events 页承接当前 runs 列表和 run detail，是证据页。列表列建议包括 Event Time、Event Type、Job Name、Namespace、Run ID、Has Lineage、Input Count、Output Count、Producer、Source。支持时间范围、job、namespace、event type、has lineage 过滤。行点击展开轻量摘要，进一步点击进入 Run Detail。Run Detail 保留原始 JSON、原始 facets、Airflow 链接、相关 job、相关 datasets、相关图谱入口。
6. Phase 3 - Datasets page PRD: Datasets 页是新增的核心目录页，用于回答“我该看哪个资产”。列表列建议包括 Dataset Name、Namespace、Dataset Type、Resolved Target、Last Seen、Source Jobs Count、Target Jobs Count、Supports Column Lineage、Internal/External。支持搜索、namespace/source/integration 过滤、internal/external 切换、只看支持列级血缘。每行至少提供“表级血缘”和“字段级血缘”两个入口；若是 internal dataset，再提供跳转 metadata 详情。
7. Phase 3 - Dataset detail PRD: Dataset Detail 建议优先做成右侧抽屉而不是新整页，以保留目录页上下文。抽屉内容包括基础信息、schema、最近相关 jobs、最近 events、internal/external 身份、namespace resolution 信息、字段级入口列表。若后续内容明显增多，再升级为完整页面。
8. Phase 3 - Table Lineage PRD: Table Lineage 复用现有 graph 页。页面结构分为顶部控制栏、中心图谱、右侧详情抽屉、底部或次级证据区。顶部控制栏包含当前焦点资产、上/下游方向、展开深度、重置、居中、返回来源页。图谱节点只展示导航必要信息：对象名、类型、是否 external、是否存在字段级信息。点击节点打开右侧抽屉，不强制整页跳转。抽屉展示对象摘要、最近相关 jobs、最近相关 OpenLineage runs、进入 Column Lineage 的按钮、进入 metadata 的按钮。证据区继续复用现有 openlineage sources 能力，但应升级成“Related Runs”。
9. Phase 3 - Column Lineage PRD: Column Lineage 建议先做独立页面，URL 允许携带 dataset guid 和 column 名称。布局采用三段式：顶部调查上下文，主体字段关系区域，右侧详情/证据面板。字段关系区优先用“按 Dataset 分组的字段关系列表或分组图”实现，不要求一开始就是复杂图谱。用户进入页面后，应默认高亮当前字段，并明确列出上游字段、下游字段、关系类型、转换表达式、来源 run。右侧面板展示选中字段的 schema 信息、最近相关 run、原始 columnLineage facet 片段、跳转到完整事件 JSON 的链接。
10. Phase 4 - Overview page PRD: Overview 是可选页。若要做，首页只承担发现异常和进入调查的职责，不展示过多实体详情。模块建议包括 Event Trend、Lineage Coverage、Recent Failures、Recently Active Jobs、Recently Seen Datasets、External Dataset Growth。每张卡片必须能点击进入 Jobs / Datasets / Events / Table Lineage 中的具体调查页。
11. Phase 4 - Cross-page linking contract: Jobs 行 -> Job Detail；Job Detail -> Table Lineage / Events；Datasets 行 -> Dataset Drawer / Table Lineage / Column Lineage；Table Lineage Node Drawer -> Metadata / Column Lineage / Related Runs；Events 行 -> Run Detail；Run Detail -> Job Detail / Dataset / Table Lineage。要求所有跳转尽量保留来源页和筛选条件，例如用 query 参数记录 from、timeRange、namespace、selectedColumn。
12. Phase 5 - Data contracts and backend implications: 为支持上述页面，需要补齐四类接口：Overview summary；Dataset aggregates；Events filters；Job/Dataset related entities。现有 task/run API 可继续服务 Jobs 和 Run Detail，但 Datasets 页和 Overview 页必须新增聚合 API；Table/Column Lineage 尽量复用现有 lineage service，避免重复建图接口。

**Relevant files**
- /home/ran/gocode/metaxisdata/frontend/src/router/index.ts — 需要重新设计 OpenLineage 路由层级，区分目录层、关系层、证据层。
- /home/ran/gocode/metaxisdata/frontend/src/components/layout/AppSidebar.vue — 需要将 OpenLineage 改成主导航入口，并增加二级导航结构。
- /home/ran/gocode/metaxisdata/frontend/src/pages/settings/OpenLineageSettingsPage.vue — 仅保留接入配置和 API key，不再承担主要浏览入口。
- /home/ran/gocode/metaxisdata/frontend/src/pages/openlineage/OpenLineageRunsPage.vue — 作为 Jobs 页的直接演进基础。
- /home/ran/gocode/metaxisdata/frontend/src/pages/openlineage/OpenLineageTaskDetailPage.vue — 作为 Job Detail 的实现基础。
- /home/ran/gocode/metaxisdata/frontend/src/pages/openlineage/OpenLineageRunDetailPage.vue — 作为 Run Detail / Evidence Detail 的实现基础。
- /home/ran/gocode/metaxisdata/frontend/src/pages/LineageGraphPage.vue — 作为 Table Lineage 的实现基础。
- /home/ran/gocode/metaxisdata/frontend/src/components/metadata/TableLineageSection.vue — 作为 Column Lineage 页面核心内容的实现参考。
- /home/ran/gocode/metaxisdata/frontend/src/pages/HomePage.vue — 如果做 Overview，可参考现有首页卡片结构，但需完全替换为真实 OpenLineage 数据。
- /home/ran/gocode/metaxisdata/frontend/src/api/openlineage.ts — 需要补足页面级 API 封装。
- /home/ran/gocode/metaxisdata/proto/v1/v1/openlineage_service.proto — 需要新增 Overview、Dataset aggregates、Events filters 相关 RPC。
- /home/ran/gocode/metaxisdata/backend/api/v1/openlineage_service.go — 需要实现页面所需聚合查询和详情接口。
- /home/ran/gocode/metaxisdata/backend/store/openlineage_task.go — 继续作为 Jobs 聚合层。
- /home/ran/gocode/metaxisdata/backend/store/openlineage_run.go — 继续作为 Events 证据层。
- /home/ran/gocode/metaxisdata/backend/plugin/openlineage/resolver.go — 作为 Datasets 页 internal/external 标识与跳转映射的基础。
- /home/ran/gocode/metaxisdata/backend/plugin/openlineage/processor.go — 作为 Table / Column Lineage 数据来源的基础。
- /home/ran/gocode/metaxisdata/backend/api/v1/lineage_service.go — 继续复用以承接图谱和字段关系页面。

**Verification**
1. 页面架构验收：用户能明确区分总览页、目录页、关系页、证据页，不再通过 Settings 进入主要调查路径。
2. Jobs 旅程验收：从 Jobs 列表进入 Job Detail，再进入 Table Lineage，再进入 Related Runs，再进入 Run Detail，全程不迷失上下文。
3. Datasets 旅程验收：从 Datasets 列表进入 Dataset Drawer，再进入 Column Lineage，再进入相关 Event JSON，全程保持 dataset 和 column 上下文。
4. Events 旅程验收：用户可通过 filters 快速定位某段时间内有 lineage 的失败或异常事件，并查看原始 payload。
5. 图谱验收：关闭节点抽屉不应清空图谱深度、筛选、字段高亮或来源页状态。
6. 状态一致性验收：normal / failed / unknown / external / internal 的颜色和徽标在 Jobs、Datasets、Events、Graph 中语义一致。
7. 分享性验收：关键页面 URL 可编码当前对象、筛选和来源信息，便于分享和返回。

**Decisions**
- 页面命名：产品层统一使用 Jobs、Datasets、Events、Overview，内部仍可沿用 task/run 字段命名，避免后端大规模重命名。
- Details 策略：目录页中的资产详情优先用抽屉保留上下文；Run 详情保留整页，因为它承担证据页职责。
- Column Lineage 策略：MVP 先做分组字段关系页，不强求重型图谱；当字段规模和交互复杂度上升后再迭代成图谱。
- Dashboard 策略：不阻塞主调查链路，优先级低于 Jobs、Datasets、Events、Table Lineage、Column Lineage。

**Further Considerations**
1. Jobs 页是否显示运行状态。建议显示 latest event type 或归一化状态徽标，否则用户很难从目录页快速决定先点哪个 job。
2. Dataset Detail 是否一开始就做整页。建议先抽屉，因为当前更重要的是从目录页快速继续调查，而不是沉浸式 dataset 详情。
3. Overview 是否替换现有 Home。建议不要直接替换，先作为 OpenLineage 子路由独立验证价值，再决定是否上升为产品首页。