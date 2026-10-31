# 架构评审第二批（前端横向抽象）落地记录

> 基线：`8acdbfd`（main，2026-10-31，工作区干净）· 分支：stack `arch-review/batch2-frontend-*`（4 层、8 个提交）· 每项独立提交。

## 结论

第二批"前端横向抽象"6 项（FD4/5/6/8/9/10）全部完成，无暂缓项。净改动 52 个文件、+2,041/−1,517 行（含本记录）；新增的共享层（`useAsync`/`usePolling`/`useUrlFilters`/`queryFilters`）全部进入 per-file 覆盖率门禁并有测试，视图层删掉约 900 行重复逻辑。

| # | 事项 | 条目 | 状态 | 提交 |
| --- | --- | --- | --- | --- |
| 1 | `Permission` 类型 + 调用点扫描测试 + OAuth scope 单一来源 | FD8 | 完成 | `c8c9421` |
| 2 | i18n 动态 key 豁免单一来源（脚本 → ESLint） | FD9 | 完成 | `954c771` |
| 3 | 流式 explain 加 `AbortSignal` + 代际 | FD10 | 完成 | `de25a2b` |
| 4 | `useAsync` / `usePolling` 原语 | FD4/5（原语） | 完成 | `6170216` |
| 5 | 15 处 debounce/一次性定时器 + 5 处轮询迁移 | FD4/5（迁移） | 完成 | `4703294` |
| 6 | `utils/queryFilters` + `useUrlFilters` | FD6（原语） | 完成 | `7bffed1` |
| 7 | 5 个数据质量页收敛到 `useUrlFilters` | FD6（迁移） | 完成 | `dc89611` |
| 8 | 本记录 | — | 完成 | 本提交 |

## PR 栈（自底向上）

| 层 | 分支 | 内容 | 提交 |
| --- | --- | --- | --- |
| 1 | `arch-review/batch2-frontend-guards` | FD8 权限类型/扫描/scope、FD9 i18n 单一来源、FD10 流式取消 | `c8c9421`…`de25a2b` |
| 2 | `arch-review/batch2-async-timers` | `useAsync`/`usePolling` 原语、15 处定时器与 5 处轮询迁移、2 个 `useAsync` 落地 | `6170216`、`4703294` |
| 3 | `arch-review/batch2-url-filters` | `utils/queryFilters` + `useUrlFilters`、5 个数据质量页迁移 | `7bffed1`、`dc89611` |
| 4 | `arch-review/batch2-tracking` | 本记录 | 本提交 |

## 实施说明与偏差

### FD8：权限标识从字符串到可执行约束

- `PERMISSION_GROUPS` 改为 `as const satisfies readonly PermissionGroup[]`，从目录派生 `type Permission`；`ALL_PERMISSIONS` 为 `readonly Permission[]`。
- `authStore.hasPermission`、`RouteMeta.permission`（新增 `vue-router` 模块声明）、侧栏 `MenuItem.permission/anyPermission`、设置导航条目全部改为 `Permission`，拼错即 `vue-tsc` 失败。
- `permissions.test.ts` 新增扫描测试：源码中所有 `hasPermission("…")` 与 `permission: "…"` 字面量必须属于目录。实测现有 52 个唯一字面量全部在 90 项目录内，因此**没有批量替换 83 处调用点**（类型 + 扫描已覆盖）。
- OAuth scope `metaxisdata.mcp.read` 收进 `lib/oauth.ts` 的 `MCP_READ_SCOPE`，同意页改用它；`oauth.test.ts` 增加读 `backend/api/oauth/resource.go` 的 `MCPReadScope` 常量比对测试。

### FD9：i18n 豁免单一来源

- 单一来源为 `scripts/check-vue-i18n.mjs`：新增导出 `eslintUnusedKeyIgnores(localeFile, sourceDir)`，返回"脚本能追踪到的键 + `DYNAMIC_PREFIXES` 命中键"的并集。
- 返回并集而不是只展开前缀，是因为该 ESLint 规则**不能**跟随 `handleError/formatError/showSuccess` 的键，而脚本可以；只展开前缀会误报 250 个键（实测）。并集让两条检查看到同一个"未使用"集合。
- `eslint.config.mjs` 从 650 行降到 102 行，删除 507 条手写 `ignores`；`check-vue-i18n.test.mjs` 覆盖展开逻辑。

### FD10：流式 explain 可取消、有代际

- `explainSQL(input, { signal })` 与 `explainQualityExecution({ …, signal })` 透传 Connect `CallOptions`。
- `QualityExplainDrawer`：generation + `AbortController` + `onScopeDispose`；切换执行/关闭抽屉都会 abort 旧流，只有当前代际能写 state。
- `ExplainSQLPage`：同样的 generation + abort，卸载断流；`catch` 从 `String(e)` 改为 `formatError(e)`。
- `utils/error.ts` 新增 `isAborted`（`Code.Canceled` 或 DOM abort），`usePagedFetch` 改为复用，删掉私有 `isSuperseded`。
- 新增测试：抽屉"切换执行不混流""关闭即断流"；`isAborted` 三态。

### FD4：`useAsync` + 定时器收敛

- 新增 `composables/useAsync.ts`：ticket + AbortSignal + `onScopeDispose`，返回 `{ data, loading, error, refresh, cancel }`；配 7 条单测（含乱序响应、abort 错误丢弃、scope 释放）。
- 8 处真实 debounce（ConnectedAppsSection、TargetPicker、MetadataBrowserPage、ManualSQL 筛选与 ID 校验、AuditLogs、DQExecutions、DQTargets、ExplainSQL 搜索）改为 `useDebounceFn`，删除各自的 timer 变量、`clearTimeout` 与 `onBeforeUnmount`。
- 6 处一次性定时器（复制反馈 ×3、注册成功跳转、版本 diff 复制、血缘 fitView ×2、调试重试）改为 `useTimeoutFn`，随 scope 自动清理。
- `useAsync` 落地 2 个单资源页作为种子：`DataQualityIssueDetailPage`、`DataQualityTargetsPage`（含目录单次聚合读）。原计划第三个 `ExecutionDetail` **未迁移**：它的轮询是"静默失败、不覆盖页面错误"，而 `useAsync` 会记录轮询错误，迁移会改变用户可见行为；该页只做了轮询收敛，留在后续 FD3 拆分时处理。

### FD5：`usePolling` + 6 处轮询收敛

- 新增 `composables/usePolling.ts`：`{ intervalMs, shouldContinue, pauseWhenHidden = true, timeoutMs?, continueOnError?, onError? }` → `{ start, stop, active, until }`。递归 `setTimeout`（上一轮结束才排下一轮，不会重叠）、隐藏标签页暂停/可见恢复、`stop()` abort 在途请求、`until()` 供"等到静止"场景。
- 迁移：Executions 列表（5s）、ExecutionDetail（3s）、SuggestionJobsPanel（2.5s，保留其失败重读与 reload 语义）、SuggestionJobHistoryDrawer（2.5s，仅在打开且仍有活跃作业时）、QualityCheckDrawer `waitForRun`（2s + 60s deadline，`continueOnError` + `until()`）。
- `SuggestionJobTargetDebugDialog` 的重试定时器不是轮询，改为 `useTimeoutFn`。
- 未做 `QualityCheckDrawer` 的 N+1 优化（改为一次 `listQualityExecutions`），因为它改变服务端查询语义，按评审建议单列。

### FD6：URL↔过滤器收敛

- `utils/queryFilters.ts`：`queryString`、`readFlag`、`enumFilterValue`、`enumFromFilter`、`sameQuery`，纯函数、100% 覆盖。
- `composables/useUrlFilters.ts`：`{ keys, parse, serialize }` → `{ read, isCurrent, write, watchExternal }`。`write` 只增删本页 keys、保留外键、等值不写；`watchExternal` 只在外部变化时 adopt，自身 `replace` 通过记录查询比对跳过，无回环。
- 数据质量 scope 的解析/写回从 Checks/Suggestions 两页抽到 `utils/dataQualityTarget.ts`（`scopeFromQuery`、`writeScopeToQuery`）。
- 迁移：Checks、Issues、Suggestions、Targets 四页删除各自四件套（`queryString`/`enumFilter`/`filtersFromQuery`/`queryFromFilters`/`sameFilters`/`sameQuery`/`writeFiltersToUrl`/route watcher），约 900 行重复逻辑消失。
- **Executions 是混合形态**：它的 URL 不是过滤器集合，而是视图状态（`view/rule/target/run` + 局部 patch）。按计划只把 URL 操作收敛到 `useUrlFilters`（`parse`/`serialize`/`write`）并复用纯函数，`flatView`、跨页链接 chip、枚举 Select 的既有行为不变。

## 验证

- 前端完整门禁（栈顶）：`biome:ci`、`lint:ci`、`i18n`、`type-check:diagnostics`、`test:ci`（per-file 95/85）、`build` 全绿。
- `test:ci`：**136 个测试文件 / 1,365 条用例**通过（基线 128 文件 / 1,320 条），共享层 per-file 阈值通过。
- 迁移涉及页面/组件的定向测试全部通过：5 个数据质量页、QualityCheckDrawer/SuggestionJobsPanel/SuggestionJobHistoryDrawer/SuggestionJobTargetDebugDialog/TargetPicker/QualityExplainDrawer、ExplainSQL/Login/ManualSQL/MetadataBrowser/LineageGraph/AuditLogs/OpenLineage 设置与 run 详情等。
- `pnpm --dir frontend build` 成功；Monaco 仍为异步 chunk（3.3 MB 独立 chunk，未进入静态依赖）。
- 说明：`biome:ci` 在分层过程中因格式化中断过一次，`biome check --write .` 后重跑通过；最终提交前所有门禁均已重跑。

## 本批未做（仍留给后续批次）

- FD2/FD3 god view 拆分（LineageGraphPage、三个 DQ 页、MetadataBrowserPage 的页面私有逻辑）——第三批。
- FD7 `RuleConfigForm` 按规则族拆分；FD11 markdown 懒加载；FD12 视图层测试补齐；FD13 `utils → api` 反向依赖；FD14 `<html lang>`/焦点陷阱；FD15 品牌名常量/请求超时。
- 29 处手写 `isLoading` 未逐一迁移：本批交付原语并在 2 个单资源页落地，其余随 FD3 的 composable 下沉顺带迁移。
- `QualityCheckDrawer` 的 N+1 轮询优化、服务端"取消生成释放 LLM 配额"的协议语义。
