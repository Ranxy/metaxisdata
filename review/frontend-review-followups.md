# 前端评审剩余项落地记录（第一批：S 项）

> 基线：`da15e45`（第二批评审修正栈顶）· 分支：`arch-review/frontend-followups` · 每项独立提交。后续 FD7 / FD2 / FD3 继续叠在同一分支上。

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

## 实施说明

### 1. 轮询取消 + 流式生命周期

- `usePagedFetch.reset/refresh` 接受 `{ signal }`：外部 signal（轮询轮次）被中止时转发到本次请求的 controller；被中止请求的答案不再落盘；signal 已中止时不发请求。`getQualityExecution` 增加 `options.signal`。
- 5 个轮询任务全部把轮次 signal 传进实际读（`refreshExecutions({signal})`、`load(false, signal)`、`getQualityExecution(name,{signal})`、`refreshPage({signal})`、建议作业两个 list + get），关闭页面/抽屉会真正断掉在途 RPC。
- 新增 `composables/useStreaming.ts`：reset/帧处理/代际/abort/scope 清理集中在一处，`QualityExplainDrawer` 与 `ExplainSQLPage` 都改用它。此前 ExplainSQLPage 的代际逻辑没有任何测试；现在该逻辑由 7 条 composable 测试覆盖（替换流丢帧、cancel、错误与 abort 区分、scope 释放、参数透传、done 语义），抽屉原有三条行为测试保留。
- 遗留：`ExplainSQLPage` 仍无页面级测试；代际逻辑已由 `useStreaming` 覆盖，页面只剩装配。

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

## 尚未开始

- **FD7**：`RuleConfigForm` 14 个规则族拆分 + `useSchemaDriftColumns`。
- **FD2**：`LineageGraphPage` 详情面板 / `lib/lineageLayout` / 两个 composable 拆分。
- **FD3**：三个 DQ god view（Executions/Target/Suggestions）的抽屉、URL、权限、轮询下沉。
- **FD12**：随上述下沉补页面/纯函数测试。
