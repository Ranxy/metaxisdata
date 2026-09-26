# 前端架构 Review 报告

**范围**: `frontend/src`(Vue 3.5 + TypeScript SPA:Vite 7 / Pinia 3 / vue-router 4 / Tailwind 4 / shadcn-vue(radix-vue)/ ConnectRPC(web))
**规模**: 138 个 `.vue` + 64 个手写 `.ts`(生成代码 `src/types/proto-es/` 除外),约 27k 行 Vue + 5k 行 TS
**审查方式**: 全量静态阅读(8 个最大页面 script 通读)+ 全仓 grep 交叉验证;所有结论均附 `文件:行号` 证据,未改动任何代码。

---

## 0. 执行摘要

前端的**工程纪律是一流的**:API 调用 100% 收敛在 ConnectRPC client(无一处 ad-hoc fetch/EventSource/WebSocket)、零 TODO/console.log 残留、Biome+ESLint+自研 i18n 审计脚本三重守门、生成代码纪律严格、`tsconfig` 全开 strict。这些都是应该保持的资产。

但从架构演进的角度看,前端正处于**"每个页面自携全部基础设施"**的阶段:共享层(`PageState`、`AdvancedSearchBar`、`useErrorHandler`、`dashboard.ts`、`lib/openlineage.ts`)已经存在且质量不错,但只有一部分页面采用;其余页面各自重写了分页、错误处理、筛选条、删除确认、时间格式化、GUID 编解码。同一关注点存在 2~6 份实现的现象贯穿了整个 pages 层。

**四个最值得优先处理的结构性问题**:

| # | 问题 | 严重度 |
|---|------|--------|
| 1 | **横向关切缺位**:ConnectRPC transport 无 interceptor,cookie 会话中途失效时无全局 401 → 登出跳转;错误码无 i18n 映射,服务端英文原文直达 toast | 高 |
| 2 | **异步数据获取无护栏**:全仓 0 处 `AbortController`/序号防护,路由快速切换、翻页连点时旧响应覆盖新状态;分页逻辑有 4 种互不相同的实现 | 高 |
| 3 | **巨型页面承担过多职责**:`LineageGraphPage`(1130 行 script,含整套图布局引擎)与 `MetadataBrowserPage`(1460 行逻辑,8 个 leaf ref + 8 个逐字重复的 fetch + 7 步串行类型探测)应下沉为 composable/子组件 | 高 |
| 4 | **组件双轨制**:手写的 `AppModal/AppInput/AppButton`(仍在 20+ 处使用)与 shadcn-vue `ui/` 原语并存,与 AGENTS.md"优先 shadcn-vue"的既定方向相违;并存期间还积累了死代码与死 props | 高 |

**ROI 最高的偿还顺序**(详见 §7 路线图):① `utils/guid.ts` + `utils/datetime.ts` 收敛散布的路由编码与时间格式化 → ② `usePagedFetch`(带序号/Abort)统一 4 种分页 → ③ 统一错误处理入口(修 `handleError(e, t(key))` 的双重翻译误用)→ ④ `ConfirmDeleteDialog` 收敛 6 份复制 → ⑤ 拆 `MetadataBrowserPage` 与 `LineageGraphPage`。

---

## 1. 架构现状总览

### 1.1 分层

```
┌─ pages/        19 个页面(settings/ 10 个、openlineage/ 7 个)
│   └─ 直接调 api/,本地 ref 拼状态(主流模式)
├─ layouts/      DefaultLayout(单一滚动容器 + 宽度契约)/ AuthLayout
├─ components/
│   ├─ ui/          73 文件,shadcn-vue 原语(17 族)
│   ├─ metadata/    34 文件,扁平堆放(10 list + 9 detail + 2 history + 工具件)
│   ├─ monaco-editor/ 12 文件,lazy 单例 + composables 分责(全目录质量最高)
│   ├─ common/      11 文件,新旧两代并存(AppModal 等旧件 + PageState/EmptyState 等新件)
│   ├─ layout/      AppSidebar / AppHeader / UserMenu / …
│   └─ lineage/(1 文件)/ openlineage/(2 文件)  ← 业务组件实际沉在 pages 里
├─ api/          17 个服务封装 + client.ts(ConnectRPC transport 单例)
├─ store/modules/ auth / app / environment / toast(Pinia options 风格)
├─ composables/  仅 2 个:dashboard.ts(范例级质量)、useErrorHandler.ts
├─ lib/          cn、permissions(权限目录镜像)、openlineage、relationType
├─ utils/        engine、environment、error、llmProvider
└─ locales/      en-US/zh-CN 各 1134 个 key + 自研审计脚本
```

### 1.2 量化画像

| 指标 | 数值 | 说明 |
|------|------|------|
| 最大页面 | MetadataBrowserPage 1931 行(script ≈1460)、LineageGraphPage 1353(script 1130)、AuditLogsPage 1227 | TOP 8 页面均 ≥770 行 |
| 页面内 `ref<>` | MetadataBrowserPage 25 个、catch 16 个 | 无状态分层,全部平铺 |
| 测试 | 17 个测试文件 / ~200 源文件;仅 5 个组件挂载测试;无 e2e | 集中在 utils/lib/store 纯函数 |
| 构建产物 | `dist/assets` 17MB;monaco chunk 3.7MB + `ts.worker` 6.7MB | 实际只用 SQL/JSON(详见 §5.1) |
| i18n | 双 locale 各 1134 key,缺失/冗余/占位符/排序四项全检 | 工具链是项目亮点 |

---

## 2. 做得好的方面(应明确保留)

以下决策经审查确认是正确资产,重构时不要破坏:

1. **API 接入纪律**:`api/client.ts:20-23` 单一 transport 注入 `credentials: "include"`;全仓 grep `fetch(`/`EventSource`/`XMLHttpRequest`/`WebSocket` 仅命中该文件。`api/*.ts` 统一 `create(Schema, …)` 薄封装(AIP 风格),如 `api/database.ts:51-64`。
2. **路由层**:全量路由懒加载(`router/index.ts`),`beforeEach` 在 `permissionsLoaded` 后零开销短路(`router/index.ts:276-278` + `auth.ts:108-113`),权限 meta 与服务端强制对齐的注释写得很清楚。
3. **共享状态范例**:`store/modules/environment.ts`(缓存 + `loaded/loading` 双标志 + 写后强制刷新)与 `composables/dashboard.ts`(权限分段加载 224-240、section 级容错、`truncated` 标记)是后续重构应该对标的模板。`HomePage.vue` 借 dashboard.ts 做到了"页面基本只声明式组装"。
4. **monaco-editor 封装**:`lazy-editor.ts` 动态 import 单例 + `composables/`(useContent/useOptions/useFormatContent)分责,是组件封装的最佳实践样板。
5. **i18n 工程化**:`scripts/check-vue-i18n.mjs` 覆盖缺失/冗余/跨 locale parity/占位符 parity/`{{}}` 误用五类检查,动态 key 走 `DYNAMIC_PREFIXES` 明示豁免;脚本自身还带测试。
6. **布局契约**:`DefaultLayout.vue:14-28` 的单一滚动容器契约、`SettingsLayout` 统一 settings 宽度(router 中 `contentWidth` 分组的注释 55-58、155-158)都是深思熟虑过的设计,注释解释了"为什么"而非"是什么"。
7. **权限镜像防漂移**:`lib/permissions.ts` 与后端 `permission.json` 的一致性由 `permissions.test.ts` 强制,权限目录永远不失步。
8. **类型纪律**:`strict: true` + `noUnusedLocals/Parameters` 全开;`as any` 真实命中全仓仅 2 处。

---

## 3. 核心架构问题

### 3.1 会话与错误处理:横向关切缺位 【高】

**(a) 无 401 全局处理** — `api/client.ts:20-23` 的 transport 没有传 `interceptors`;全仓无任何对 `Code.Unauthenticated` 的运行时处理。cookie 会话中途失效时,每个页面各自 toast 一条服务端原文,用户停留在失效页面上继续操作,要等到下一次路由导航触发 `fetchCurrentUser` 失败才被送回登录页。

**(b) `fetchCurrentUser` 把网络抖动当登出** — `auth.ts:93-99` 的 `catch {}` 不区分错误类型:任何失败(含 5xx、断网)都置 `isAuthenticated=false`,下一次导航即被 `router/index.ts:280-281` 重定向到 Login。应只对 `Code.Unauthenticated` 清会话。

**(c) 错误码 → 用户文案无映射** — `utils/error.ts:6-23` 只剥 code 前缀,`useErrorHandler.ts:17-23` 把 `rawMessage` 直接弹给用户;`permission_denied`/`failed_precondition` 等没有集中 i18n 文案。

**(d) 错误处理三种姿势并存,且存在误用**:
- 姿势一(正确):`handleError(err, "auditLogs.fetchError")` — 传 **i18n key**(`LLMProviderManagementPage.vue:393`)。
- 姿势二(误用):`handleError(err, t("…"))` — 把**已翻译字符串**当 key 再翻一次,触发 missing-key 警告并绕过 eslint i18n 检查:`InstanceManagementPage.vue:847,860`、`InstanceDetailPage.vue:796,935`。
- 姿势三(绕行):直接 `toastStore.error(e.message || t(key))`:`ManualSQLManagementPage.vue:686,750,787,855,874`、`DatabaseManagementPage.vue:289,334`(后者还是手写 `e instanceof Error` 分支,ConnectError 会带 `[code]` 前缀弹出)。
- 另有内联 `errorMessage` ref 派:`MetadataBrowserPage` 一处文件 11 处。

**建议**:在 `client.ts` 为所有 client 注册一个 response interceptor,捕获 `Unauthenticated` → 清 auth store + 跳登录;`useErrorHandler` 增加 code → i18n key 映射层;修正姿势二的所有调用点。

### 3.2 异步数据获取:分页碎片化 + 零竞态防护 【高】

**(a) 分页有 4 种实现**:
1. `api/lineage.ts:36-58` — 唯一的"拉全部页"封装,且带 stuck-token 保护;
2. token-stack 上一页/下一页,**逐字复制 3 份**:`AuditLogsPage.vue:534-536+1204-1218`、`ManualSQLManagementPage.vue:424-426+793-807`、`DatabaseManagementPage.vue:219-221+304-316`;
3. `AuditLogsPage.vue:1144-1158` CSV 导出 do-while(**没有** stuck-token 保护,与 lineage.ts 不一致);
4. `MetadataBrowserPage.vue:679-685+1456-1481` per-metaType `Map` 维护 nextPageToken。

**(b) 静默截断**:7 处 `listInstances({pageSize: 1000|100})` 不检查 `nextPageToken`(`InstanceManagementPage.vue:790,809`、`DatabaseManagementPage.vue:297`、`MetadataBrowserPage.vue:913`、`ExplainSQLPage.vue:701`、`OpenLineageSettingsPage.vue:549` 等),数据超上限时悄悄缺行;`environment.ts:56-57` 同样截断。对照正面范例:`dashboard.ts:176-178` 把截断暴露为 `truncated` UI 标记。

**(c) 全仓 0 处 `AbortController`/序号防护**。具体后果:
- `MetadataBrowserPage.vue:1618-1669`:`watch(route, {immediate:true})` 内裸 await,路由快速切换时旧详情响应覆盖新状态(8 个 fetchXxxDetail 之间也无互斥);
- `LineageGraphPage.vue:1346`:`watch(currentGuid) → initializeGraph` 无取消;`fetchLineageForGuid`(707-787)无 in-flight 去重,同一 guid+direction 并发 expand 重复请求;
- `AuditLogsPage.vue:1175-1197`:翻页/刷新连点即乱序。
- 项目其实**知道**这个问题,但没有共享方案:`ManualSQLManagementPage.vue:435-436,652,665` 手写 sequence 计数器、`AuditLogsPage.vue:548,1106-1142` 手写 `pendingAuditUsers` Set 去重 —— 两份各不相同的本地防护正好是"缺一个 `usePagedFetch`"的证据。

**(d) instances 列表未 store 化**:至少 6 个页面各自发起同一 RPC(对照 environments 已有缓存 store)。

**建议**:实现 `usePagedFetch(fetcher, {pageSize})` composable(内建序号戳/Abort、`previousPageTokens` 栈、`truncated` 暴露)+ `api/` 层 `listAll(listFn)`;instances 比照 environments 建缓存 store。

### 3.3 巨型页面:职责未下沉 【高】

**`MetadataBrowserPage.vue`(1931 行)—— 实际是 9 个页面塞进一个文件**:
- 8 个同构 leaf ref(`leafTable`…`leafManualSQL`,690-697)+ 对应的 `isXxxDetailView` computed 族(716-784);
- 8 个 `fetchXxxDetail` 函数体仅 metaType 与目标 ref 不同,**逐字重复约 240 行**(1178-1419);
- `fetchMetadataGroups` 内嵌 **7 步串行** leaf-type 探测链(976-1128):无 route hint 时最多 7 次串行 RTT 才能渲染一个 leaf —— 这是**性能问题**而不仅是可维护性问题;
- 级联 scope 筛选器(instance→database→schema 三级)整段内联在模板 79-267(约 190 行);
- MySQL 家族空 schema 的 GUID 修正规则散布在 5 处(870-879、889-900、1558-1589)。

**`LineageGraphPage.vue`(1353 行,script 1130)—— 图引擎沉在页面里**:BFS 分层布局(823-941)、边构建、列高亮规则全部内联;`rebuildGraph`(947-1037)与 `updateGraphState`(1053-1134)之间约 90 行"columnEdgeIds 收集 + 双边构建"**逐字重复两遍**;`components/lineage/` 只有一个 145 行的展示节点 `LineageNode.vue`——组件目录与 pages 的职责划分在这里是倒挂的。

**其他页面**:`AuditLogsPage.vue`(1227)内联整棵 RangeCalendar 模板(173-232)+ 90 行 CSV 导出逻辑(1033-1104)+ `dateRangeChip`/`draftDateRangeChip` computed 逐字重复(618-652);`InstanceManagementPage` 创建表单(模板 ~300 行 + 逻辑 ~180 行)与 `InstanceDetailPage` 编辑表单是"复制后改改"的两份。

**建议**:`LineageGraphPage` 抽 `useLineageGraph`(图 store+布局);`MetadataBrowserPage` 按 leaf 视图拆子组件/子路由分发,8 个 fetch 合并为 `Map<MetaType, fetcher>` 驱动,串行探测改并行或让后端 `ListMetadata` 返回 leaf 类型提示;表单抽 `InstanceFormDialog` 复用。

### 3.4 组件双轨制与死代码 【高】

新一代 shadcn-vue `ui/` 原语与上一代手写 `App*` 组件并存,且旧件仍占多数:

| 关注点 | 旧件(使用中) | 新件(使用中) |
|--------|--------------|--------------|
| 对话框 | `AppModal` ×11 处 | `ui/dialog` ×4 处 |
| 输入框 | `AppInput` ×12 处 | `ui/input` 为辅 |
| 按钮 | `AppButton` ×3 处 | `ui/button` ×44 处(已切换) |

附带损伤:
- `common/AppDropdown.vue` **零引用**,纯死代码;
- `AppModal.vue:41-42` 声明的 `closable`/`closeOnBackdrop` props **从未被实现消费** —— 所有删除/表单弹窗都无法控制这两个行为,且其手动 body-overflow watch(74-83)与 radix 机制重复且卸载不复位;
- `MetadataList.vue:30` 的必选 `isMysql` prop 透传 10 个子列表,唯一声明它的 `SchemaList.vue:54` **从不使用** —— 全链路死 prop;
- `AdvancedSearchBar.vue:53-56` 声明的 `search` emit 从未触发、4 个使用页面也不监听 —— 死契约;
- `ManualSQLManagementPage.vue:163-205` 用原生 `<select>`,`AuditLogsPage.vue:64-133` 手写 input+panel —— 与 AGENTS.md"优先 shadcn-vue"冲突的破窗;
- `MonacoEditor.vue:7` "Loading editor..."、"LineageNode.vue:26` "External" 硬编码英文漏网。

**建议**:定一条迁移路径(AppModal→ui/dialog、AppInput→ui/input、原生 select→ui/select),按页面逐个替换;迁移前先删除死代码与死 props。

### 3.5 状态管理:缺口与冗余并存 【中】

**(a) locale 双源头**:`locales/index.ts:10-23` 自己读 localStorage(默认 **en-US**),`app.ts:94` 的 store state 默认 **zh-CN**;切换逻辑 `appStore.setLocale(...) + locale.value = ...` 在 `UserMenu.vue:172-175` 和 `LoginPage.vue:368-371` **重复两份**;`locales/index.ts:35` 导出的 `setLocale()` 无人调用(死代码)。应收敛为一个 action。

**(b) toast store 是冗余转发层**:`toast.ts` 的 Pinia store 只被 `AppToast.vue:41-52` deep-watch 后转发给 vue-sonner,转发后立即 `removeToast`;store 里的 `setTimeout` 自动移除(41-44)**永远摸不到已被删除的条目**,是纯死代码。整条 Pinia→watch→sonner 链路相比直接 `import { toast } from "vue-sonner"` 多了一跳,无收益。

**(c) `EnvironmentSelect` 放错了层**:位于 `common/` 却直接 import `useAuthStore`/`useEnvironmentStore`(`EnvironmentSelect.vue:164-165,196-197`)并内嵌环境创建弹窗 —— 不是通用组件,应移到业务组件目录或拆解。

### 3.6 跨页面微重复(已确认的最小全集) 【中】

| 模式 | 份数 | 位置(代表) |
|------|------|------------|
| `toGuidPath`(GUID→路由路径,`;`→`/`、空段→`~`) | **共享版存在却有 4 份本地复制** | 共享:`lib/openlineage.ts:10`;本地:`MetadataBrowserPage.vue:1488`、`LineageGraphPage.vue:669`、`ManualSQLManagementPage.vue:580`(改名 guidToRoutePath)、`OpenLineageColumnLineagePage.vue:436` |
| GUID 路由解码(含 `~`→空段) | 3 套并存 + 2 套不解码 | 解码:`MetadataBrowserPage.vue:810-821`、`LineageGraphPage.vue:330-339`、`OpenLineageColumnLineagePage.vue:204-213`(逐字);不解码:`OpenLineageTaskDetailPage.vue:304-307`、`OpenLineageRunDetailPage.vue:198` |
| 时间格式化 `Intl.DateTimeFormat(locale…)` | 13+ 处,**3 种 locale 写法** | `locale.value`(多数)、字面量 `"default"`(`LineageGraphPage.vue:553`、`OpenLineageColumnLineagePage.vue:414` —— 忽略应用语言设置,跟随 OS)、`undefined`(`OpenLineageOverviewPage.vue:348`);`InstanceManagementPage.vue:771-785` 与 `InstanceDetailPage.vue:1038-1052` 逐字相同 |
| 删除确认对话框(AppModal sm + 图标 + 文案 + 双按钮) | **6 份逐字** | `InstanceManagementPage.vue:222-253`、`ManualSQLManagementPage.vue:325-354`、`UserManagementPage.vue:355-382`、`EnvironmentSettingsPage.vue:159-186`、`RoleManagementPage.vue:182-215`、`GroupManagementPage.vue:182-215` |
| 手写 debounce(`let timer; clearTimeout; setTimeout(fn,300)`) | 4+ 处 | `MetadataBrowserPage.vue:570,1913`、`InstanceManagementPage.vue:823`、`UserManagementPage.vue:616`、`ExplainSQLPage.vue:625` —— 而 `@vueuse/core` 已是依赖(仅 4 个文件在用) |
| OpenLineage related-runs 聚合 + 3 个关联格式化函数 | ~120 行逐字 ×2 | `LineageGraphPage.vue:434-480,530-573` ↔ `OpenLineageColumnLineagePage.vue:294-329,391-434` |
| `formatBytes`/`formatNumber` | 逐字 ×2 | `TableList.vue:106-…` ↔ `TableMetadataDetail.vue:490-…` |
| metadata 列表/详情脚手架 | 8 List(772 行)+ 8 Detail(1562 行)近同构 | `components/metadata/*List.vue`、`*MetadataDetail.vue`;其中 ColumnsSection 过滤逻辑已**行为漂移**(TableDetail 含 comment 匹配,其余不含) |
| 标签页按钮组 | 4 份逐字节相同 + 2 种变体 | `View/MaterializedView/ExternalTable/ManualSQL` 各 detail L28-65;`ui/` 无 tabs 原语是根因 |
| `eventVariant` 映射 | 逐字 ×2 | `HomePage.vue:801-815` ↔ `OpenLineageRunsPage.vue:262-276` |
| 实例工具函数(getHostInfo/getEnvironmentLabel/getInstanceId) | 逐字 ×2 | `InstanceManagementPage.vue:747-769` ↔ `InstanceDetailPage.vue:632-634,1015-1027` |
| `WORKSPACE_PARENT = "workspaces/-"` | 5 处 | `dashboard.ts:30`、`AuditLogsPage.vue:525`、内联字面量 ×3 |
| metaType 魔法数 | 多处 | `"100"`=OpenLineage(`OpenLineageTaskDetailPage.vue:380` 等)、`"18"`(`ManualSQLManagementPage.vue:884`)、`3 as MetaType`(同文件:663) |

**`toGuidPath` 特别提示**:这不仅是重复问题,而是**正确性风险**。GUID 路由编码是跨页面契约,`ExplainSQLPage.vue:580-584` 用第三套约定(`/`-joined 拼接后下游按 `;` split,587/642)—— 经 `/explain-sql/:guid+` 路由(`router/index.ts:145`)进入、且 GUID 含空 schema(`~` 段)的对象会**解析错误**;另外三处页面对 vue-router 已 decode 的 params 再 `decodeURIComponent`,对象名含 `%` 时双重解码。**必须收敛为 `utils/guid.ts` 单一来源**,并补解析测试。

### 3.7 i18n / a11y 破窗 【中】

- `LineageGraphPage.vue:575-595` `formatMetaTypeLabel` 硬编码英文,且 578-581 两个 case 是**永不命中的死分支**(`guidToMetaType` 509-516 只可能返回 external/instance/database/schema/table;按 segment 数猜类型对 MySQL 空 schema GUID 本就不可靠);
- `ExplainSQLPage.vue:659-674` `metaTypeLabel` 硬编码英文 + 魔法数字 key,**且参数命名 `t` 遮蔽了 useI18n 的 `t`(324)**,函数体内无法翻译;
- `LLMProviderManagementPage.vue:265-294` BUILTIN_DEFS 的 label/description 硬编码英文;`InstanceDetailPage.vue:324` 模板 `ds.type === 1 ? "ADMIN" : "READ_ONLY"`(魔法数 1,而 `DataSourceType.ADMIN` 已 import);多处 `|| "Unknown"` 兜底;
- `ExplainSQLPage.vue:346-348` `route.params.guid ? "metadata" : "metadata"` —— 死三元,两分支相同;
- a11y:icon-only 按钮多数无 `aria-label`(如 `MetadataBrowserPage.vue:36-42,51-57`),仅 `ManualSQLManagementPage.vue:261` 加了 —— 无统一约定;click-outside 三套实现,其中 `MetadataBrowserPage.vue:1906-1911` 用 `target.closest(".relative")` 脆弱选择器(布局一改就坏),`AuditLogsPage.vue:687` 的 vueuse `onClickOutside` 是唯一正确姿势。

---

## 4. 具体实现问题清单(节选,均有证据)

| # | 位置 | 问题 | 修法 |
|---|------|------|------|
| 1 | `ExplainSQLPage.vue:640` | `(resp as any).results` —— `SearchMetadataResponse` 本有完整类型,`as any` 关掉后续所有检查 | 删断言 |
| 2 | `LineageGraphPage.vue:553` / `OpenLineageColumnLineagePage.vue:414` | `Intl.DateTimeFormat("default")` 字面量 locale | 统一进 `utils/datetime.ts` |
| 3 | `ExplainSQLPage.vue:580-584` | `/`-拼接 guid 与下游 `;` split 不一致,含 `~` 段的 guid 解析错误 | 统一 `utils/guid.ts` 编解码 |
| 4 | `MetadataBrowserPage.vue:817` 等 3 处 | 对 vue-router 已 decode 的 params 二次 `decodeURIComponent` | 同上 |
| 5 | `InstanceDetailPage.vue:1054-1077` | 用 `listInstances(filter)` 当 `getInstance` 用再取 `[0]`;且"网络错误"与"不存在"都被 redirect 掩盖 | 改用已有的 `getInstance`,区分 NotFound |
| 6 | `OpenLineageColumnLineagePage.vue:220` / `MetadataBrowserPage.vue:711` | `Number` → `MetaType` 断言不校验枚举成员资格 | 枚举 includes 校验 |
| 7 | `utils/environment.ts:40` | `as EnvironmentColorKey` 在 `includes` 校验**之前**断言 | 换类型谓词 |
| 8 | `AuditLogsPage.vue:547` | `draftDateRange = ref<any>` | 用 RangeCalendar 的实际类型 |
| 9 | `auth.ts:86-100` | `fetchCurrentUser` 吞所有错误;`ensurePermissionsLoaded` 无 in-flight Promise 去重,快速双导航会重复 RPC | 按 code 区分;缓存 in-flight |
| 10 | `api/database.ts:251-261` | 手工拼 `{seconds, nanos}` Timestamp | 用 `timestampFromDate`(wkt 已提供) |
| 11 | `AppToast.vue:41-52` | deep-watch 数组最后一项 + watch 内修改被 watch 的 store,链路脆弱 | 直接用 vue-sonner 的 `toast` |
| 12 | `components.json` | 残留 shadcn init 占位 `"@acme": "https://acme.com/r/{name}.json"` registry | 删除 |

---

## 5. 构建与工程化

### 5.1 Monaco 打包:17MB dist 的罪魁祸首 【中优先级,高收益】

`lazy-editor.ts:19` `import("monaco-editor")` 引入**全量包**(所有内置语言),`vite.config.ts:19-21` 的 `manualChunks: { "monaco-editor": ["monaco-editor"] }` 进一步把整包钉进一个 chunk:

```
dist/assets/ts.worker-*.js        6.7M   ← 实际 worker  wiring 只用 editor+json
dist/assets/monaco-editor-*.js    3.7M   ← 全语言
dist/assets/css.worker/html.worker/json.worker/editor.worker …
```

`monaco-workers.ts:3-4` 只注册了 `editor.worker` 与 `json.worker`,且产品只编辑 SQL。改法:从 `monaco-editor/esm/vs/editor/editor.api` + 按需 contribution(`basic-languages/sql`、`language/json`)导入,或用插件式裁剪,主 chunk 可从 3.7MB 降到 ~1MB 级;`manualChunks` 同时移除。`ts.worker` 等不再 emit,dist 体积 17MB → 预计 <8MB。

另外 `editor.ts:33` 硬编码 `theme: "vs"`,不跟随应用的暗色主题(`app.ts` 的 theme 状态)—— 编辑器在 dark mode 下是浅色。

### 5.2 依赖与配置卫生 【低】

- `frontend/package-lock.json`(npm)与 `pnpm-lock.yaml` **同时被 git 提交**;`package.json:80` 声明 `packageManager: pnpm@10.24.0` —— npm lock 是纯残留,会误导贡献者;
- `package.json:10` 的 `type-check` 需要 `NODE_OPTIONS=--max_old_space_size=8000` —— 138 个 vue 需要 8GB 堆是 vue-tsc 偏重的信号(生成代码 `.d.ts`+`.js` 双份也参与解析);可考虑把 `proto-es` 排除出 vue-tsc 的输入或升级关注 vue-tsc 的增量构建;
- `components.json` 残留 `@acme` 占位 registry(见 §4-12)。

### 5.3 测试 【中】

- 17 个测试文件 / 约 200 个手写源文件;组件挂载测试仅 5 个(`AppSidebar`、`HomePage`、`MetadataList`、`ExternalTableList`、`ExternalTableMetadataDetail`),全部集中在"最近重构过"的角落;
- 已有的测试质量并不差(drift 测试、store 测试、guard 测试),说明不是不会写,而是没有底线要求;
- `vitest.config.ts` 无 coverage 阈值;无 e2e(playwright 之类)—— 对一个 19 页的单体管理台,先补"共享 composable/utils 的单测"(重构的护航网)比组件快照测试 ROI 更高;
- 建议顺序:为将要抽取的 `utils/guid.ts`、`utils/datetime.ts`、`usePagedFetch` **先写测试再迁移**;`vitest.config.ts` 加 `coverage.thresholds` 防倒退。

---

## 6. 技术债务清单(汇总)

> **状态(2026-09-26):下表 5 条 P0 已全部完成并验证,落地内容见 §6.1;P1 及以下尚未开始。**

| 优先级 | 债务 | 位置/证据 | 预估工作量 |
|--------|------|-----------|-----------|
| P0 | GUID 编解码收敛(toGuidPath×4、解码×3、ExplainSQL 解析 bug、双重解码) | §3.6 表 | 0.5~1 天(含测试) |
| P0 | `utils/datetime.ts` 收敛 13+ 处格式化(顺带修 "default" locale bug) | §3.6 表 | 0.5 天 |
| P0 | 删死代码:AppDropdown、AppModal 死 props、isMysql 死 prop 链、search 死 emit、setLocale 死导出、locale 重复 changeLocale | §3.4/§3.5 | 0.5 天 |
| P0 | 删 `package-lock.json`、`components.json` 占位 registry | §5.2 | 10 分钟 |
| P0 | 修 `handleError(e, t(key))` 双重翻译误用全部调用点 | §3.1(d) | 0.5 天 |
| P1 | ConnectRPC interceptor:401 → 登出;`fetchCurrentUser` 按 code 区分;in-flight 去重 | §3.1(a)(b) | 1 天 |
| P1 | 错误处理统一入口 + code→i18n 文案映射 | §3.1(c)(d) | 1~2 天 |
| P1 | `usePagedFetch`(序号/Abort)+ `api listAll`;替换 3+2 处分页与 7 处截断拉取 | §3.2 | 2~3 天 |
| P1 | `ConfirmDeleteDialog` 收敛 6 份;monaco 打包裁剪 | §3.6/§5.1 | 各 1 天 |
| P1 | instance store(比照 environment);EnvironmentSelect 移出 common | §3.2(d)/§3.5(c) | 1 天 |
| P2 | 拆 `MetadataBrowserPage`(leaf 分发 Map + 搜索栏组件 + 并行探测/后端 hint) | §3.3 | 3~5 天 |
| P2 | `useLineageGraph` 抽取,消除 90 行双边重复;AuditLogsPage 抽 useDateRangePicker/CSV 模块 | §3.3 | 2~3 天 |
| P2 | App*→ui/ 迁移(AppModal×11、AppInput×12、原生 select 破窗);toast store 简化为直接 vue-sonner | §3.4/§3.5(b) | 2~3 天 |
| P2 | 筛选条四分天下收敛到 AdvancedSearchBar;metadata List/Detail 组件参数化合并 | §3.6 表 | 2~3 天 |
| P2 | i18n 破窗修复(硬编码英文簇、死分支、遮蔽) | §3.7 | 1 天 |
| P3 | 共享层单测补齐 + coverage 阈值;组件测试逐步扩到高价值交互 | §5.3 | 持续 |

### 6.1 P0 修复记录(已完成)

实施者补写。5 条 P0 全部落地;每条的决策、证据与残留如下。全量门禁(`pnpm biome:check`、`pnpm lint`、`pnpm i18n`、`pnpm type-check`、`pnpm test run`)通过:`src` 测试文件 17 → 20(`scripts/` 的 2 个另计),用例 85 → 126(新增 guid 13、datetime 11、guidRoutes 13、app store locale 4)。本轮只动前端,未触碰 Go/proto。

> **① GUID 编解码收敛(决策:单一工具 + 重复路由参数,废弃 `/`-join 字符串形态)**
>
> 新增 `frontend/src/utils/guid.ts`(`guidToRouteParams` / `guidSegmentsToRouteParams` / `routeParamToGuid`)与 `guid.test.ts`;删除 5 份本地复制(`MetadataBrowserPage`、`LineageGraphPage`、`ManualSQLManagementPage`、`OpenLineageColumnLineagePage`、`TableLineageSection`——最后一份是原文漏掉的),并移除 `lib/openlineage.ts` 的同名导出;全部 push 点改走共享工具。
>
> 关键决策:§3.6 的"收敛"不足以消掉正确性风险,因为 `:guid(.+)` 单个参数里 `/` 既可能是分隔符也可能是名字的一部分。故把 `/metadata/:guid+`、`/lineage/:guid+`、`openlineage/column-lineage/:guid+`(以及原本就是 `+` 的 `/explain-sql/:guid+`)统一改成**重复参数**,push 时传"一段一个数组元素"(`guidToRouteParams`),元素边界由 matcher 保证——含 `/` 或 `;` 的对象名因此能正确往返,这是原实现根本做不到的。
>
> 同时修掉 §4-3/§4-4:`ExplainSQLPage` 的第三套约定(`/`-join 后按 `;` split)改为同一编解码;6 个 metadata 详情页的 `/explain-sql/<;guid>` 链接改为 path 形态;对**路由参数**的 `decodeURIComponent` 二次解码全部删除(含 `%` 的名字不再抛 `URIError`)。`formatOpenLineageRunLabel` 里对 OpenLineage GUID 分段的解码不是路由参数解码,未动。
>
> 兼容性:`frontend/src/router/guidRoutes.test.ts` 用**真实路由表**(去掉守卫与页面组件)钉住契约,覆盖数组往返、含 `/` 名字、MySQL 空 schema `~`、含 `%` 名字,以及三种历史 URL 形态——手输 `;` URL、手输 `/` 形态、以及旧字符串 push 产生的单段 `%2F` URL(`/metadata/inst%2Fdb%2F~%2Ftbl`)全部仍可解析,书签不受影响。`openlineage:job/run` GUID 由服务端 `url.PathEscape` 逐段转义(`backend/plugin/openlineage/metadata.go:100-113`),其解码路径未改。
>
> 残留(有意的契约边界,已写进 `guid.ts` 注释):`:guid` 路径形态无法表达"名字里含 `/` 的单段 GUID";单段 GUID 只可能是实例 ID,而 OpenLineage GUID 的 `/` 在服务端已转义,故实际不可达。

> **② `utils/datetime.ts` 收敛(决策:纯函数 + 显式 locale,顺带修字面 locale)**
>
> 新增 `frontend/src/utils/datetime.ts`(`formatDateTime` / `formatDate` / `formatTime` / `formatRelativeTime`,选项含 `month` / `seconds` / `fallback`)与 `datetime.test.ts`;21 个文件、23 处内联 `Intl.DateTimeFormat` / `toLocaleString` 全部替换,仓库内已无内联日期格式化(CSV 导出的 ISO 8601 输出按设计保留)。
>
> 顺带修掉的两个真实缺陷:`LineageGraphPage`、`OpenLineageColumnLineagePage` 的 `Intl.DateTimeFormat("default")` 与 `OpenLineageOverviewPage` 的 `undefined` 不再跟随 OS 语言,统一用 `locale.value`;`ManualSQLManagementPage` 在 `seconds` 缺失时会 `new Date(NaN)` 抛 `RangeError`(且该页与全站 12/24 小时制不一致),现统一为 24 小时并回落 `-`。

> **③ 死代码清理**
>
> 删除 `AppDropdown.vue`(零引用)、`AppModal` 的 `closable` / `closeOnBackdrop`(声明但从未消费)、`isMysql` 整条链(`MetadataBrowserPage` → `MetadataList` → `SchemaList`,含测试)与 `AdvancedSearchBar` 的 `search` 死 emit。
>
> locale 收敛(决策:store 持有单一来源 + 默认值统一 en-US,理由与原文的"双源头"一致,见 §7 第一步注):`locales/index.ts` 的死导出 `setLocale` 改为被 store 调用的 `applyLocale`;`store/modules/app.ts` 的 `setLocale` 一处同时更新 state、localStorage 与 vue-i18n;`UserMenu.vue` / `LoginPage.vue` 两份重复 `changeLocale` 删除。`app.test.ts` 新增"挂载探针"用例,证明组件里 `useI18n()` 读到的 locale 会随 store 切换(这是"单一 action"能不能真的切换界面的证据)。

> **④ 工程卫生**:删除 `frontend/package-lock.json`(npm 残留,`packageManager` 已声明 pnpm);移除 `components.json` 的 `@acme` 占位 registry。

> **⑤ `handleError(e, t(key))` 双重翻译**
>
> 16 处调用点改为传 i18n key(InstanceDetail 6、InstanceManagement 4、UserManagement 4、GeneralSettings 2、Iam 1);`LLMProviderManagementPage.vue:497` 的 `handleError(...) ?? String(e)`(对 void 做空合并的死代码)改为 `extractErrorMessage(e)`。
>
> 配套:自研 `scripts/check-vue-i18n.mjs` 本就有 `HANDLE_ERROR_RE` 能识别 `handleError(err, "key")`,但 ESLint 的 `no-unused-keys` 看不到,故按该文件既有约定把 14 个 key 补进 `frontend/eslint.config.mjs` 的 `ignores`(注释仍写"via showSuccess / handleError composables")。

---

## 7. 改进路线图建议

**第一步:止血(P0,约 2 天)** — ~~不碰架构,只收敛契约与死代码:`utils/guid.ts`(含编解码测试)→ 替换 4+3 处实现并修掉 ExplainSQL 的解析 bug;`utils/datetime.ts`;清死代码;修错误处理误用。~~ **已完成,见 §6.1。** 与原文的差异:GUID 部分没有止步于"收敛为单一工具",而是同时把 4 条路由改成重复参数,否则含 `/` 的名字仍然无法往返;locale 默认值从 `zh-CN` 统一为 `en-US`(`locales/index.ts` 与 `app.ts` 原先不一致)。

**第二步:横向护栏(P1,约 1~1.5 周)** — interceptor 全局 401;统一错误入口;`usePagedFetch` + `listAll` 落地并替换全部手写分页/截断拉取(**这一步直接消掉竞态与静默截断两类正确性问题**);`ConfirmDeleteDialog`;monaco 裁剪。

**第三步:拆巨人(P2,约 2 周)** — `MetadataBrowserPage`、`LineageGraphPage`、`AuditLogsPage` 按 §3.3 方案拆分;筛选条收敛;App*→ui/ 迁移;toast 简化。每次拆分都先落 composable 测试。

**第四步:防倒退(P3,持续)** — `vitest.config.ts` 加 coverage 阈值;把"`pnpm type-check` 内存"、"dist 体积"、"测试数"做成 CI 观察项;在 AGENTS.md 写清 `utils/` vs `lib/` 的分界标准(当前靠惯例,新代码归属靠猜)。

---

## 8. 结语

这套前端的"地基"(API 纪律、i18n 工程化、布局契约、类型严格度、monaco 封装)在这个规模的项目里属于上游水平,问题集中在**"腰部"**:可复用抽象的存在与采用率不匹配 —— 共享层已经长出来了(environment store、dashboard composable、PageState、AdvancedSearchBar、lib/openlineage),但旧页面没有回迁,新页面又各自手搓。这不是能力问题而是**缺少一次有计划的收敛**。按上面 P0→P1 的路线走,大约两周内可以把所有"正确性级"的债(竞态、截断、GUID 解析、401)清掉,P2 的拆分可以随后按页面排期,每拆一个文件都是独立可上线的改进。
