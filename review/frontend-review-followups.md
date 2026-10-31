# 前端评审剩余项落地记录（第一批：S 项）

> 基线：`da15e45`（第二批评审修正栈顶）· 分支：`arch-review/frontend-followups` · 每项独立提交。FD7 已在本文件追加，FD2/FD3 继续叠在同一分支上。

## 结论

评审报告 03-frontend 的 6 项 S/小改以及上一轮评审留下的两个 follow-up 已完成，共 7 个提交、36 个文件、+1,228/−204 行（含本记录）。剩余 FD7（`RuleConfigForm` 拆分）、FD2/FD3（god view 拆分）与 FD12（随下沉补测试）尚未开始。

| # | 事项 | 对应条目 | 提交 |
| --- | --- | --- | --- |
| 1 | 轮询轮次可取消在途读；流式生命周期抽成 `useStreaming` | FD5 续 / FD10 续 | `875cd55` |
| 2 | markdown 渲染器改为按需加载 | FD11 | `132aacf` |
| 3 | `utils` 不再反向依赖 `api`，并加分层守卫测试 | FD13 | `68b1979` |
| 4 | `<html lang>` 跟随语言；移动抽屉 ESC/焦点陷阱/焦点归还 | FD14 | `9224508` |
| 5 | 品牌名单一常量、`OPENLINEAGE` 用枚举、RPC 默认超时 | FD15 | `6bc8d29` |
| 6 | 轮询读新增 signal 参数后的测试适配 | — | `d7bf597` |
| 7 | 抽屉焦点边界分支测试 | — | `01f6887` |
| 8 | **FD7**：`RuleConfigForm` 按规则族拆分 + `useSchemaDriftColumns` | FD7 | `ef870fe` |
| 9 | **FD2（第一步）**：测高收敛下沉为 `lib/lineageLayout` | FD2 | `6ff94fc` |
| 10 | **FD2（第二步）**：详情列抽成 `LineageNodeDetailPanel` + 9 条测试 | FD2 | `6d1c63b` |
| 11 | **FD3（第一步）**：`useDataQualityPermissions` 权限矩阵 | FD3 | `8caa2d2` |
| 12 | **FD3（第二步）**：`utils/dataQualityExecutions` 纯派生 + 9 条测试 | FD3 | `46ee4e7` |
| 13 | **FD3（第三步）**：`useTargetWorkspaceDialogs` + 14 条测试 | FD3 | `45a9741` |
| 14 | **FD3（第四步）**：`useTargetScheduleEditor` + 7 条测试 | FD3 | `21c3b40` |
| 15 | ExplainSQLPage 页面测试（补齐轮询/流式遗留） | ① | `e0bbba8` |
| 16 | **FD3（第五步）**：Suggestions 行读数进 utils + 权限 composable | FD3 | `e475c03` |
| 17 | **FD3（第六步）**：Extractions target row 组件 + 3 条测试 | FD3 | `0886bbc` |

## 实施说明

### 1. 轮询取消 + 流式生命周期

- `usePagedFetch.reset/refresh` 接受 `{ signal }`：外部 signal（轮询轮次）被中止时转发到本次请求的 controller；被中止请求的答案不再落盘；signal 已中止时不发请求。`getQualityExecution` 增加 `options.signal`。
- 5 个轮询任务全部把轮次 signal 传进实际读（`refreshExecutions({signal})`、`load(false, signal)`、`getQualityExecution(name,{signal})`、`refreshPage({signal})`、建议作业两个 list + get），关闭页面/抽屉会真正断掉在途 RPC。
- 新增 `composables/useStreaming.ts`：reset/帧处理/代际/abort/scope 清理集中在一处，`QualityExplainDrawer` 与 `ExplainSQLPage` 都改用它。此前 ExplainSQLPage 的代际逻辑没有任何测试；现在该逻辑由 7 条 composable 测试覆盖（替换流丢帧、cancel、错误与 abort 区分、scope 释放、参数透传、done 语义），抽屉原有三条行为测试保留。
- 遗留已补齐：`ExplainSQLPage` 现有 3 条页面级测试（流式渲染、卸载中止、错误映射、按钮禁用）。

### 2. markdown 按需加载

- 新增 `components/common/AsyncMarkdown.ts`：`defineAsyncComponent` + 动态 `import("markstream-vue")`，CSS 随该异步 chunk 加载；`main.ts` 不再静态引入 markstream 样式。
- 守卫测试 `AsyncMarkdown.test.ts`：导入 loader 不评估渲染器、全仓无其它静态 import、入口不引用其样式。
- 构建验证：入口 `index-*.js` 不含 markstream/exports chunk 引用；渲染器经 `exports-*.js`（679 kB）随解释面板按需加载，Monaco 仍是独立 3.3 MB 异步 chunk。

### 3. utils 分层

- `environmentName`/`environmentId` 下沉到 `utils/environment.ts`；`api/environment.ts` 反向 import 并 re-export，现有调用点不变。
- 新增 `src/test/layering.test.ts`：按 `frontend/AGENTS.md` 的依赖方向检查 utils/lib/composables/api 四层不反向 import（`lib/notify` 的 `vue-sonner` 为已知例外，测试注释说明）。

### 4. 可访问性

- `applyLocale()` 同步 `document.documentElement.lang`；新增 `initLocale()` 在 bootstrap 时应用存储语言（此前首屏没有写 `lang`）。store 测试断言切换与启动两条路径。
- 移动抽屉：新增 `composables/useDrawerFocus.ts`（Escape 关闭、Tab 循环、焦点被移出时拉回、关闭时归还给打开前的元素），接在 `AppSidebar` 上。安装的 `@vueuse/core@15` 没有 focus-trap 原语，因此手写且可单测（5 条边界测试）。

### 5. FD15

- `lib/brand.ts` 的 `BRAND_NAME`：AppHeader/AppSidebar/AuthLayout（评审漏数了 AuthLayout，实测 3 处）统一使用；守卫测试保证没有第二处硬编码。
- `OPENLINEAGE_META_TYPE = 100` 换成 `MetaType.OPENLINEAGE`。
- `utils/apiTimeout.ts` + `api/client.ts`：默认 60s RPC 上限，用 `AbortSignal.any([caller, AbortSignal.timeout])` 组合；`ExplainSQLService`、`LLMService`、`*Sync*`、`*Run*`、`ExplainQualityExecution` 不设客户端上限。超时路径由纯函数测试覆盖（100%）。
- 有意保留：`LineageGraphPage` 三处 `extractErrorMessage` 在 `console.error` 诊断里（要原文本，不走用户文案）；`SuggestionWorkbench` 的 `rawError` 是截断提示旁边展示的服务器原文，`syncDraftErrorHintKey` 也要用它分类。两者都不是用户可见错误文案的唯一来源（同一处已有 `formatError`）。

## 验证

- 完整门禁：`biome:ci`、`lint:ci`、`i18n`、`type-check`、`test:ci`、`build` 全绿。
- `test:ci`：**143 个测试文件 / 1,407 条用例**通过；per-file 覆盖率阈值通过（新增 `useStreaming` 7 条、`useDrawerFocus` 8 条、`apiTimeout`/`brand` 守卫、`AsyncMarkdown` 守卫、`usePagedFetch` 12 条）。
- `usePagedFetch` 新增外部 signal 用例（已中止不发请求、转发中止、答案不落盘）；`QualityCheckDrawer`/`SuggestionJobsPanel` 的调用断言跟上新参数。

## FD7：RuleConfigForm 按规则族拆分

- 父组件 `RuleConfigForm.vue` 从 **1205 行降到 261 行**：只保留共享的 fail/warn 阈值块、`oneOfHints`、字段 id/必填/错误的薄封装，以及一个按 `ruleType` 分发的组件 switch。
- 新增 `components/data-quality/rule-configs/` 14 个规则族组件：`RowCountConfig`（含 ROW_COUNT_CHANGE）、`NullPercentConfig`、`UniquenessConfig`（UNIQUE/UNIQUE_COMBINATION）、`AcceptedValuesConfig`、`RangeConfig`、`ScalarConfig`（REGEX/STRING_LENGTH/REFERENTIAL_INTEGRITY）、`CrossColumnConfig`、`FreshnessConfig`、`CustomSQLConfig`、`SchemaDriftConfig`、`GroupCheckConfig`、`AggregateConfig`、`CrossTableConfig`、`CelConfig`，外加共享的 `ruleConfigField.ts`（字段 id/必填/错误/CSV 列转换）。
- schema-drift 的期望列状态机从组件内 `WeakMap` 抽成 `composables/useSchemaDriftColumns.ts`：只有"本组件自动填充"的值会在改名时刷新/清除，读者输入或切换的值永不覆盖；新增 11 条测试覆盖自动填充、清除、按行隔离、全部填充等语义（此前零测试）。
- 新增规则类型现在只需加一个规则族组件 + 一行 switch；`RuleConfigForm.test.ts` 27 条与下游 219 条相关测试全部保持通过。

## FD2：god view 拆分（进行中）

- 第一步已完成：`settleNodeHeights` 的四轮收敛（含测高未就绪时保留旧布局、pass 预算、全部节点测高）与 `heightFor` 回退下沉到 `lib/lineageLayout.ts`，6 条纯函数测试覆盖（100%），页面只保留 DOM 测量与 `stack()` 回调。
- 第二步已完成（`6d1c63b`）：详情列抽成 `components/lineage/LineageNodeDetailPanel.vue`（402 行，含外部对象的 namespace/datasetType、字段 trail、来源徽标、相关 runs、空态），页面从 2016 行降到 1807 行；父页面只传 7 个派生值 + 7 个动作事件。新增 **9 条组件测试**（空态、身份/计数、外部对象、trail、runs、动作 emit、禁用态、无 trail）。
- 仍需完成：`useLineageGraphData`（nodeDataMap/展开状态/单节点与度数取数）与 `useLineageSelection`（选中/字段揭示/跳转动作）两个 composable；页面目标降到约 300 行。

## FD3：god view 下沉（进行中）

- **权限矩阵**（`8caa2d2`）：`composables/useDataQualityPermissions.ts` 统一 14 个 DQ 权限名；`DataQualityTargetPage`（8 个 `canX`）与 `QualityCheckDrawer`（7 个）改为一次解构，两处重复的权限字符串消失；4 条测试覆盖授予/拒绝/权限集变化/未加载 profile。
- **执行页纯派生**（`46ee4e7`）：`utils/dataQualityExecutions.ts` 承接 target 行读数（GUID/名称/路径、最新批次 state/outcome、verdict 计数、failed/unfinished、时间与耗时、状态提示），页面里约 120 行私有函数删除；`utils/dataQualityExecutions.test.ts` 9 条测试覆盖边界（无 target、无 batch、无 counts、未完成、运行中、无起止时间），覆盖率 100% 行。
- **抽屉状态机**（`45a9741`）：`composables/useTargetWorkspaceDialogs.ts` 承接 inline composer（新建/编辑/从 AI 草案打开）、check 侧栏、以及 `?check=`/`?add=1`/`?edit=1` 三条深链的解析与 URL 清理；TargetPage **1502 → 1355 行**，页面只剩提交/关闭时的业务处理。新增 **14 条测试**：三种打开方式、scope/列种子、侧栏开关与 query 清理、深链（有效/过期/与 add 冲突/edit 无对应规则）、`add=1` 无权限提示、无 query 时不写 URL。
- **排期编辑器**（`21c3b40`）：`composables/useTargetScheduleEditor.ts` 承接打开时从 Duration 回填、客户端校验、保存并回写 summary；TargetPage **1355 → 1308 行**。7 条测试覆盖整点回填、空排期默认、非法/合法校验、非法不保存、成功保存与回写、失败保留对话框、开关绑定；覆盖率 100%。
- **ExplainSQLPage 页面测试**（`e0bbba8`）：补齐上一轮遗留的页面级测试——切到自定义 SQL、输入、点击 Explain，断言 `explainSQL` 收到 `{ signal }`、流式内容渲染、卸载中止在途流、失败显示映射文案而非原始错误、无 SQL 时按钮禁用。3 条测试。
- **Suggestions 行读数与权限**（`e475c03`）：`suggestionRowText`/`suggestionLabelTypeKey`/`suggestionRuleDraftTypeKey`/`suggestionSeverity`/`suggestionConfidence*`/`suggestionGroupMetaType`/`selectableSuggestions`/`groupSelectionState` 收进 `utils/dataQualitySuggestion.ts`（9 个纯函数），页面删除约 60 行；三个权限改走 `useDataQualityPermissions`。新增 9 条测试，util 覆盖率 98.2% 行 / 90.6% 分支。
- **Executions 行组件**（`0886bbc`）：`components/data-quality/TargetRunRow.vue`（262 行）承接 target 行的全部展示（名称/类型/时间/状态/计数/失败数/展开按钮）与展开后的 `TargetScanHistory`；页面 **1493 → 1279 行**，只保留行点击/展开动作。新增 3 条组件测试（行读数、toggle emit、展开时渲染历史并转发 run 展开事件），Executions 页面 15 条测试保持通过。
- 剩余：Executions 平铺执行表的行组件可继续拆；以及 FD2 的 `useLineageGraphData`/`useLineageSelection`。

### 排序调整

FD2 的两个 composable 与画布/路由/快照状态互相引用（`expandedDirections`、`updateGraphState`、`snapshot`、`route` 交叉），直接抽取需要把这些依赖全部注入，风险高于收益，且该页没有页面测试兜底。因此先做 FD3 中可测、边界清晰的下沉（权限矩阵、纯派生函数、抽屉状态机），FD2 的数据层留到之后单独处理，并在记录中保持可见。

## 尚未开始

- **FD2 后续**：`LineageGraphPage` 的 `useLineageGraphData`/`useLineageSelection` 两个 composable（页面目标约 300 行；依赖画布/路由/快照，需先补页面级测试）。
- **FD3**：三个 DQ god view（Executions/Target/Suggestions）的抽屉、URL、权限、轮询下沉。
- **FD12**：随上述下沉补页面/纯函数测试。
