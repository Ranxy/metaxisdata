# 前端评审剩余项落地记录

> 基线：`da15e45`（第二批评审修正栈顶）· 分支：`arch-review/frontend-followups`（在当前 worktree 分支栈上叠加）· 每项独立提交、逐层跑前端门禁。未 push、未创建 PR。

## 结论

评审报告 [03-frontend](.agents/worktrees/review-architecture-2026-10/review/03-frontend.md) 的剩余项已**按本次范围**落地：目标 ①–⑧（⑦含 FD2/FD3 两行）全部有提交与测试，**81 个文件、+7,3xx/−2,882 行**（最终数字以下方"验证"为准）。可执行验证：完整套件从 `da15e45` 的 **137 文件 / 1,374 用例**增长到 **155 文件 / 1,525 用例（+151）**，`biome:ci`、`lint:ci`、`i18n`、`type-check`、`test:ci`（per-file 95/85）、`build` 全绿。

保留项：FD2 的 `LineageGraphPage` 仍有 1,492 行（评审的 god view 目标是"显著缩小 + 可测下沉"，未到其 ~300 行理想值；展开走查/fitView/快照仍在页面）；FD12 的页面级测试缺口只从 26/47 降到 25/47（本次只补了 ExplainSQLPage，LineageGraphPage 仍无页面测试）。

## 目标核对

| 项 | 内容 | 证据（提交 / 产物 / 测试） | 状态 |
| --- | --- | --- | --- |
| ① | 轮询 AbortSignal 链路 + ExplainSQLPage 测试 | `875cd55`（`usePagedFetch` 外部 signal、5 个轮询任务贯穿、`useStreaming` 生命周期）；`e0bbba8`（页面级 3 条测试：流式渲染、卸载中止、错误映射、按钮禁用） | 完成 |
| ② | FD11 markdown 懒加载 | `132aacf`：`AsyncMarkdown` + 守卫测试；构建验证入口不再静态引用渲染器（679 kB 异步 chunk），CSS 随 chunk | 完成 |
| ③ | FD13 utils→api 反向依赖 | `68b1979`：环境名助手下沉 utils、api 反向 re-export；新增 `src/test/layering.test.ts` 四层依赖守卫 | 完成 |
| ④ | FD14 a11y | `9224508`：`applyLocale`/`initLocale` 同步 `<html lang>`；移动抽屉 ESC/Tab 循环/焦点归还（`useDrawerFocus` + 8 条测试）；`01f6887` 补边界分支 | 完成 |
| ⑤ | FD15 洁净度/超时 | `6bc8d29`：`BRAND_NAME` 单一常量（含评审漏掉的 AuthLayout）、`MetaType.OPENLINEAGE` 替换魔数、`apiTimeout` 默认 60s + 长任务豁免（100% 覆盖） | 完成 |
| ⑥ | FD7 RuleConfigForm 按规则族拆分 | `ef870fe`：父组件 1205 → 261 行、14 个规则族组件 + `useSchemaDriftColumns`（11 条测试）；下游 27+219 条测试保持通过 | 完成 |
| ⑦ | FD2 god view 拆分 | `6ff94fc`（测高收敛 → `lib/lineageLayout`，7 条测试）、`6d1c63b`（详情列组件，9 条）、`cf04e7a`（`useLineageGraphData`，13 条）、`edec083`（`useLineageSelection`，8 条）；LineageGraphPage **2026 → 1492 行** | 完成 |
| ⑦ | FD3 god view 拆分 | `8caa2d2`（权限矩阵）、`46ee4e7`（执行页纯派生 + 9 条测试）、`45a9741`（Target 抽屉状态机 + 14 条）、`21c3b40`（排期编辑器 + 7 条）、`e475c03`（Suggestions 行读数 + 9 条）、`0886bbc`/`30a8b2a`（Executions 两套行组件 + 8 条）；三个页面 1611/1518/1420 → **1030/1301/1346 行** | 完成 |
| ⑧ | FD12 随下沉补测试 | 上述每项均带测试；本分支新增 +151 条（含组件与纯函数），共享层 per-file 门禁全部通过 | 完成 |

## 第二轮评审与修正

6 名独立 reviewer（按层 + 跨层）对本分支做了只读评审，跨层 reviewer 独立重跑了六道门禁。结论：无 P0，重构方向与测试总体可信；发现 **3 个 P1、若干 P2/P3**，已全部修正（见 `46b92cb`、`20e5cbf`）：

| 级别 | 发现 | 修正 |
| --- | --- | --- |
| P1 | 超时被 `AbortSignal.timeout` 变成 `Canceled`，而 `isAborted` 会把所有 `Canceled` 当作"调用方取消"丢弃 → 超时页面静默空白/错误 toast 说"请求已取消" | `apiTimeout` 改为手写 deadline controller，超时由 `client.ts` 抛 `ConnectError(DeadlineExceeded)`；不再使用 `AbortSignal.any/timeout`（兼容目标浏览器），timer 在请求结束时清理 |
| P1 | `SubscribeNotifications` 不在豁免表里，通知推送流每 60s 被切断重建 | 豁免改为方法族 `^(Batch)?(Sync|Run|Suggest|Profile|Preview|Subscribe|Explain)...`，补测试 |
| P1 | 豁免漏掉 LLM 支撑的 `Suggest*`、`Profile*`、`BatchSyncInstances` | 同上；测试覆盖每个族 |
| P2 | Suggestions 组元数据类型 fallback 从 `MetaType.TABLE` 变成 `0`（UNSPECIFIED），无 target 的组显示 "Other" 而非 "Tables" | 恢复 `?? MetaType.TABLE`，测试断言 TABLE |
| P2 | 权限矩阵测试一次授予全部 14 项，无法发现两项接反 | 改为逐项表驱动：只授予一个标识符，断言仅对应属性为 true |
| P2 | 路由切换时 `initialLoad` 撞上 poll 的 abort，`loading` 布尔守卫导致新 load 直接 return → 空白页且无错误 | 改为 ticket（`loadTicket`）语义，最新请求才写状态；删除布尔守卫 |
| P2/P3 | 抽屉 Tab 陷阱会抢走抽屉内 popover 的焦点、Escape 会被菜单与抽屉重复处理；抽屉对 AT 不是 modal | Tab 时跳过 portalled 菜单层；Escape 尊重 `defaultPrevented`；aside 补 `role/aria-modal/aria-label` 与 `id`，opener 补 `aria-expanded/controls` |
| P3 | `AsyncMarkdown` 无 loading/error 故事（懒加载 chunk 失败会永久空白） | `defineAsyncComponent` 增加 `delay/timeout/loadingComponent/errorComponent` |
| P3 | brand/layering 守卫偏弱（未覆盖未加引号文本/index.html；相对与副作用导入漏检、层级规则不全） | brand 匹配任意出现并校验 `index.html` title；layering 解析相对导入、检查副作用/再导出、补 components/store 规则 |
| P3 | `OPENLINEAGE_META_TYPE` 还有一处、`NodeLineageData` 重复定义、面板 prop 契约漂移、`parseExpandDepth` 注释错误、TDZ 顺序隐患、重复 import、解释页 reset 在 await 之后 | 逐项修正 |
| P3 | 测试弱点：外部数据集插入分支未覆盖、度数断言锁定重复 id、layout 用例未真正测"缺席节点"、抽屉 Escape 未测 `defaultPrevented`、行断言过于宽松、排期并发保存未测 | 逐一补强；度数读取改为去重 |
| P3 | `test:ci` 在并发跑门禁时偶发 `loginRedirect` 超时（5s） | 该用例超时放宽到 10s，门禁串行执行 |

第二轮跨层 reviewer 同时纠正了本记录的基线数字（此前误用评审报告快照）：实现前 `da15e45` 为 **137 文件 / 1,374 用例**，页面基线为 **2026/1611/1420**（LineageGraph/Executions/Suggestions，Target 1518），`lineageLayout` 为 **7** 条测试。

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
有意保留：`extractErrorMessage` 在 `console.error` 诊断里的用法（`LineageGraphPage` 一处、`useLineageGraphData` 两处）；`SuggestionWorkbench.rawError` 是截断提示旁展示的服务器原文（分类也要用），两者都不是用户可见错误文案的唯一来源。

### ⑥ FD7
父组件只保留共享阈值块 + 字段 id/必填/错误薄封装 + ruleType 分发；14 个规则族组件各自拥有模板与少量脚本；schema-drift 期望列状态机抽成 composable 并首次获得测试。

### ⑦ FD2 / FD3
- FD2：页面 2026 → 1492 行；详情列、测高收敛、数据层、选中层四个下沉物均有测试。
- FD3：Target/Executions/Suggestions 三页分别 1518/1611/1420 → 1301/1030/1346 行；权限矩阵、执行页读数、抽屉状态机、排期编辑器、Suggestions 行读数、Executions 两套行组件均已抽离。
- 仍留在页面内的：LineageGraph 的展开走查/fitView/快照重置与模板装配；Target/Suggestions 的行级组件可继续拆。这些是后续可选项，不属于本次目标。

## 验证（最终）

- `biome:ci`：521 文件通过；`lint:ci`：0 警告；`i18n`：key/占位符/词汇表全通过。
- `type-check`：`vue-tsc --build --force` 通过。
- `test:ci`：**155 个测试文件 / 1,525 条用例**通过；共享层 per-file 95/85 通过（新增模块 `useStreaming`/`useTimers`/`useUrlFilters`/`useSchemaDriftColumns`/`useTargetWorkspaceDialogs`/`useTargetScheduleEditor`/`useDataQualityPermissions`/`useLineageGraphData`/`useLineageSelection`/`useDrawerFocus`/`apiTimeout` 等均达标或 100%）。
- `build`：生产构建成功；入口无 markstream 静态引用，Monaco 仍为独立异步 chunk。
- 与基线 `da15e45` 对比：85 文件、+7,625/−2,908 行；提交数 35（22 实现/测试 + 13 记录）。
