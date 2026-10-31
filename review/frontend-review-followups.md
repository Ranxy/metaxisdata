# 前端评审剩余项落地记录

> 基线：`da15e45`（第二批评审修正栈顶）· 分支：`arch-review/frontend-followups`（在当前 worktree 分支栈上叠加）· 每项独立提交、逐层跑前端门禁。未 push、未创建 PR。

## 结论

评审报告 [03-frontend](.agents/worktrees/review-architecture-2026-10/review/03-frontend.md) 的剩余项已全部落地：**20 个实现/文档提交、81 个文件、+7,277/−2,882 行**。目标中的 8 项全部完成，并且每一项都有可执行验证：新增/强化 **约 110 条测试**，完整套件从 149 文件 / 1,461 用例增长到 **155 文件 / 1,513 用例**，`biome:ci`、`lint:ci`、`i18n`、`type-check`、`test:ci`（per-file 95/85）、`build` 全绿。

## 目标核对

| 项 | 内容 | 证据（提交 / 产物 / 测试） | 状态 |
| --- | --- | --- | --- |
| ① | 轮询 AbortSignal 链路 + ExplainSQLPage 测试 | `875cd55`（`usePagedFetch` 外部 signal、5 个轮询任务贯穿、`useStreaming` 生命周期）；`e0bbba8`（页面级 3 条测试：流式渲染、卸载中止、错误映射、按钮禁用） | 完成 |
| ② | FD11 markdown 懒加载 | `132aacf`：`AsyncMarkdown` + 守卫测试；构建验证入口不再静态引用渲染器（679 kB 异步 chunk），CSS 随 chunk | 完成 |
| ③ | FD13 utils→api 反向依赖 | `68b1979`：环境名助手下沉 utils、api 反向 re-export；新增 `src/test/layering.test.ts` 四层依赖守卫 | 完成 |
| ④ | FD14 a11y | `9224508`：`applyLocale`/`initLocale` 同步 `<html lang>`；移动抽屉 ESC/Tab 循环/焦点归还（`useDrawerFocus` + 8 条测试）；`01f6887` 补边界分支 | 完成 |
| ⑤ | FD15 洁净度/超时 | `6bc8d29`：`BRAND_NAME` 单一常量（含评审漏掉的 AuthLayout）、`MetaType.OPENLINEAGE` 替换魔数、`apiTimeout` 默认 60s + 长任务豁免（100% 覆盖） | 完成 |
| ⑥ | FD7 RuleConfigForm 按规则族拆分 | `ef870fe`：父组件 1205 → 261 行、14 个规则族组件 + `useSchemaDriftColumns`（11 条测试）；下游 27+219 条测试保持通过 | 完成 |
| ⑦ | FD2 god view 拆分 | `6ff94fc`（测高收敛 → `lib/lineageLayout`，6 条测试）、`6d1c63b`（详情列组件，9 条）、`cf04e7a`（`useLineageGraphData`，13 条）、`edec083`（`useLineageSelection`，8 条）；LineageGraphPage **2021 → 1492 行** | 完成 |
| ⑦ | FD3 god view 拆分 | `8caa2d2`（权限矩阵）、`46ee4e7`（执行页纯派生 + 9 条测试）、`45a9741`（Target 抽屉状态机 + 14 条）、`21c3b40`（排期编辑器 + 7 条）、`e475c03`（Suggestions 行读数 + 9 条）、`0886bbc`/`30a8b2a`（Executions 两套行组件 + 8 条）；三个页面 1635/1518/1506 → **1030/1301/1346 行** | 完成 |
| ⑧ | FD12 随下沉补测试 | 上述每项均带测试；本分支新增/强化约 110 条（含组件与纯函数），共享层 per-file 门禁全部通过 | 完成 |

## 提交索引

| # | 事项 | 对应条目 | 提交 |
| --- | --- | --- | --- |
| 1 | 轮询轮次可取消在途读；流式生命周期抽成 `useStreaming` | FD4/FD5/FD10 续 | `875cd55` |
| 2 | markdown 渲染器改为按需加载 | FD11 | `132aacf` |
| 3 | utils 不再反向依赖 api + 分层守卫测试 | FD13 | `68b1979` |
| 4 | `<html lang>` 跟随语言；移动抽屉 ESC/焦点陷阱/焦点归还 | FD14 | `9224508` |
| 5 | 品牌名单一常量、`OPENLINEAGE` 用枚举、RPC 默认超时 | FD15 | `6bc8d29` |
| 6 | 轮询读新增 signal 参数后的测试适配 | — | `d7bf597` |
| 7 | 抽屉焦点边界分支测试 | — | `01f6887` |
| 8 | FD7：RuleConfigForm 按规则族拆分 + `useSchemaDriftColumns` | FD7 | `ef870fe` |
| 9 | FD2（一）：测高收敛下沉为 `lib/lineageLayout` | FD2 | `6ff94fc` |
| 10 | FD2（二）：详情列抽成 `LineageNodeDetailPanel` + 9 条测试 | FD2 | `6d1c63b` |
| 11 | FD3（一）：`useDataQualityPermissions` 权限矩阵 | FD3 | `8caa2d2` |
| 12 | FD3（二）：`utils/dataQualityExecutions` 纯派生 + 9 条测试 | FD3 | `46ee4e7` |
| 13 | FD3（三）：`useTargetWorkspaceDialogs` + 14 条测试 | FD3 | `45a9741` |
| 14 | FD3（四）：`useTargetScheduleEditor` + 7 条测试 | FD3 | `21c3b40` |
| 15 | ExplainSQLPage 页面测试 | ① | `e0bbba8` |
| 16 | FD3（五）：Suggestions 行读数进 utils + 权限 composable | FD3 | `e475c03` |
| 17 | FD3（六）：Executions target row 组件 + 3 条测试 | FD3 | `0886bbc` |
| 18 | FD3（七）：Executions flat row 组件 + 5 条测试 | FD3 | `30a8b2a` |
| 19 | FD2（三）：`useLineageGraphData` 数据层 + 13 条测试 | FD2 | `cf04e7a` |
| 20 | FD2（四）：`useLineageSelection` 选中层 + 8 条测试 | FD2 | `edec083` |

（另有 8 个 docs 提交更新本记录；分支栈顶部为 `arch-review/frontend-followups`。）

## 实施要点与偏差

### ① 轮询取消 / 流式生命周期
- `usePagedFetch.reset/refresh` 接受 `{ signal }` 并转发中止；已中止的轮次不发请求，被中止请求的答案不落盘。`getQualityExecution` 增加 `signal`。
- 5 个轮询任务把轮次 signal 贯穿到实际读：Executions 列表、执行详情、建议作业面板/历史抽屉、`QualityCheckDrawer.waitForRun`。
- 新增 `useStreaming`（reset/帧/代际/abort/scope 释放），Drawer 与 ExplainSQLPage 共用；页面不再各自维护 generation+controller。

### ② FD11
`AsyncMarkdown` 动态加载 `markstream-vue` 与 CSS，`main.ts` 不再全局引入样式；守卫测试钉住"唯一静态入口 + 入口不引用样式"。

### ③ FD13
`environmentName/Id` 下沉 `utils/environment.ts`，`api/environment.ts` 反向 re-export；新增四层依赖方向守卫（utils/lib/composables/api）。

### ④ FD14
`applyLocale` 同步 `<html lang>`，bootstrap 调 `initLocale`；安装的 `@vueuse/core@15` 无 focus-trap 原语，故手写 `useDrawerFocus`（Escape、Tab 循环、焦点拉回/归还）并测试。

### ⑤ FD15
`lib/brand.ts` 单一品牌名（评审漏数的 AuthLayout 一并收编）；`OPENLINEAGE_META_TYPE=100` 换 `MetaType.OPENLINEAGE`；`utils/apiTimeout` 默认 60s 并发起方 signal 组合（`AbortSignal.any`），LLM/Explain/Sync/Run 豁免。
有意保留：`LineageGraphPage` 三处 `extractErrorMessage` 在 `console.error` 诊断里；`SuggestionWorkbench.rawError` 是截断提示旁展示的服务器原文（分类也要用），两者都不是用户可见错误文案的唯一来源。

### ⑥ FD7
父组件只保留共享阈值块 + 字段 id/必填/错误薄封装 + ruleType 分发；14 个规则族组件各自拥有模板与少量脚本；schema-drift 期望列状态机抽成 composable 并首次获得测试。

### ⑦ FD2 / FD3
- FD2：页面 2021 → 1492 行；详情列、测高收敛、数据层、选中层四个下沉物均有测试。
- FD3：Target/Executions/Suggestions 三页分别 1518/1635/1506 → 1301/1030/1346 行；权限矩阵、执行页读数、抽屉状态机、排期编辑器、Suggestions 行读数、Executions 两套行组件均已抽离。
- 仍留在页面内的：LineageGraph 的展开走查/fitView/快照重置与模板装配；Target/Suggestions 的行级组件可继续拆。这些是后续可选项，不属于本次目标。

## 验证（最终）

- `biome:ci`：521 文件通过；`lint:ci`：0 警告；`i18n`：key/占位符/词汇表全通过。
- `type-check`：`vue-tsc --build --force` 通过。
- `test:ci`：**155 个测试文件 / 1,513 条用例**通过；共享层 per-file 95/85 通过（新增模块 `useStreaming`/`useTimers`/`useUrlFilters`/`useSchemaDriftColumns`/`useTargetWorkspaceDialogs`/`useTargetScheduleEditor`/`useDataQualityPermissions`/`useLineageGraphData`/`useLineageSelection`/`useDrawerFocus`/`apiTimeout` 等均达标或 100%）。
- `build`：生产构建成功；入口无 markstream 静态引用，Monaco 仍为独立异步 chunk。
- 与基线 `da15e45` 对比：81 文件、+7,277/−2,882。
