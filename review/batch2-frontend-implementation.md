# 架构评审第二批（前端横向抽象）落地记录

> 基线：`8acdbfd`（main，2026-10-31，工作区干净）· 分支：stack `arch-review/batch2-*`（5 层、10 个提交 + 本记录）· 每项独立提交。完成后由 4 名独立 subagent 分三层 + 跨层各评审一遍，发现的 P1/P2 已修正（见"评审后修正"）。

## 结论

第二批"前端横向抽象"6 项（FD4/5/6/8/9/10）全部交付：**FD8/FD9/FD10 全量完成；FD4/FD5/FD6 的原语完成、按约定范围落地**（9 处 debounce、8 处一次性定时器、5 处轮询、4 个过滤器页 + Executions 混合页、2 个 `useAsync` 落地页）。净改动 54 个文件、+2,340/−1,520 行（含本记录）；新增共享层全部进入 per-file 覆盖率门禁并有测试，数据质量四页删掉 507 行重复逻辑。

| # | 事项 | 条目 | 状态 | 提交 |
| --- | --- | --- | --- | --- |
| 1 | `Permission` 类型 + 调用点扫描测试 + OAuth scope 单一来源 | FD8 | 完成 | `c8c9421` |
| 2 | i18n 动态 key 豁免单一来源（脚本 → ESLint） | FD9 | 完成 | `954c771` |
| 3 | 流式 explain 加 `AbortSignal` + 代际 | FD10 | 完成 | `de25a2b` |
| 4 | `useAsync` / `usePolling` 原语 | FD4/5（原语） | 完成 | `6170216` |
| 5 | debounce/一次性定时器 + 轮询迁移 | FD4/5（迁移） | 完成 | `4703294` |
| 6 | `utils/queryFilters` + `useUrlFilters` | FD6（原语） | 完成 | `7bffed1` |
| 7 | 5 个数据质量页收敛到 `useUrlFilters` | FD6（迁移） | 完成 | `dc89611` |
| 8 | 本记录 | — | 完成 | `73a78a3` |
| 9 | 评审修正：定时器作用域（P1/P2） | FD4 后续 | 完成 | `1910b82` |
| 10 | 评审修正：composable 加固（P2/P3） | FD4/5/6 后续 | 完成 | `c755e11` |

## PR 栈（自底向上）

| 层 | 分支 | 内容 | 提交 |
| --- | --- | --- | --- |
| 1 | `arch-review/batch2-frontend-guards` | FD8 权限类型/扫描/scope、FD9 i18n 单一来源、FD10 流式取消 | `c8c9421`…`de25a2b` |
| 2 | `arch-review/batch2-async-timers` | `useAsync`/`usePolling` 原语、定时器与轮询迁移、2 个 `useAsync` 落地 | `6170216`、`4703294` |
| 3 | `arch-review/batch2-url-filters` | `utils/queryFilters` + `useUrlFilters`、5 个数据质量页迁移 | `7bffed1`、`dc89611` |
| 4 | `arch-review/batch2-tracking` | 本记录 | `73a78a3` |
| 5 | `arch-review/batch2-frontend-review-fixes` | 评审后的 P1/P2/P3 修正 | `1910b82`、`c755e11` |

## 评审后修正

4 名独立 reviewer 分别评审三层改动 + 跨层验证（其中一人独立重跑了全部门禁），结论是"局部需改、架构可发"。已修正：

| 级别 | 发现 | 修正 |
| --- | --- | --- |
| P1 | `useTimeoutFn` 在浏览器默认 `immediate: true`，注册成功后的"切回登录"定时器在**每次页面加载 1.5s 后**触发，会清掉 `?expired=1` 的会话过期提示、把刚点注册的用户弹回登录表单 | 新增 `composables/useTimers.ts#useOneShot`（显式 `{ immediate: false }` + scope 清理），8 处站点全部改用它 |
| P2 | `useDebounceFn` 没有 scope 清理，卸载后待触发的 debounce 仍会发请求；在 Targets 页可在 300ms 内把 `q` 写进下一个页面的 URL | 新增 `useTimers.ts#useDebounced`（`onScopeDispose(cancel)`），9 处站点全部改用；新增作用域释放测试 |
| P2 | `useUrlFilters` 的自写标记在"写入被外部导航抢先"后不会被清掉，之后一次真正的外部导航可能被当成自身写入而跳过 adopt，URL 与页面过滤条件静默分叉 | watcher 在任何非自身写入的 query 上先清标记再 adopt；补"抢先写入后再导航到该 query 仍会 adopt"回归测试 |
| P3 | `useAsync` 在 scope 释放后，忽略 abort 的请求仍可能写入 `data/error` 或触发 `onError` toast | dispose 时递增 ticket，晚到结果不再写；补"晚到结果被丢弃"测试 |
| P3 | `usePolling` 的 `timeoutMs` 只在排下一轮时检查，隐藏标签页会拖过 deadline | deadline 改由独立定时器执行；补"隐藏时 deadline 仍生效"测试 |
| P3 | `SuggestionJobsPanel` 轮询与用户手动刷新相撞会多触发一次立即重载 | 轮询轮次不再设 `reloadRequested`（`loadJobs(true)`） |
| P3 | Executions 轮询在手动刷新已清空 in-progress 后仍会发一轮请求 | 轮次内先判 `hasInProgress()` |
| P3 | `IssueDetail` 在 issue→issue 路由复用组件时显示上一个 issue | `watch(issueName, load)`（`useAsync` 已使该改动安全） |
| nit | ESLint 的 `src` 是 cwd 相对路径；权限扫描不覆盖 `anyPermission: [...]` 数组；Targets 仍手写 `=== "1"` | 改用配置文件的绝对 URL；扫描补数组；改用 `readFlag` |

评审确认但**有意保留**的取舍（已写入代码注释/本节）：

- FD9 的 ESLint `no-unused-keys` 现在完全派生自脚本的正则（2,629 个 locale 键全部进入 ignores）。这是"单一来源"的代价：动态前缀族里**新增**的死键不会被两道门禁发现（脚本此前也忽略动态前缀），i18n 脚本成为 unused 键的唯一独立检查。
- `usePolling` 提供了每轮的 `AbortSignal`，但只有部分任务会消费它（列表页的 `usePagedFetch.refresh` 与 `getQualityExecution` 尚未接 signal），因此"关闭即取消在途请求"只完成一半；接口注释已如实描述。
- `isAborted` 会把服务端返回的 `Code.Canceled` 一并当作"调用方取消"丢弃；与 `usePagedFetch` 的既有约定一致。
- `DataQualityTargetsPage` 刷新失败时保留旧行（旧实现清空），页面错误文案仍会显示，属于有意的行为变化。
- `DataQualityExecutionDetailPage` 未迁 `useAsync`：它的轮询是"静默失败、不覆盖页面错误"，迁移会改变用户可见行为，留给 FD3。
- 非规范 URL（`?status=01`、`?q=`）现在会触发一次额外的 adopt/请求；旧实现按解析后的过滤器比较会保持安静，无状态错误。
- `QualityCheckDrawer` 的 N+1 轮询优化、服务端"取消生成释放 LLM 配额"的协议语义不在本批。

## 实施说明与偏差

### FD8：权限标识从字符串到可执行约束

- `PERMISSION_GROUPS` 改为 `as const satisfies readonly PermissionGroup[]`，从目录派生 `type Permission`；`ALL_PERMISSIONS` 为 `readonly Permission[]`。
- `authStore.hasPermission`、`RouteMeta.permission`（新增 `vue-router` 模块声明）、侧栏 `MenuItem.permission/anyPermission`、设置导航条目全部改为 `Permission`，拼错即 `vue-tsc` 失败。
- `permissions.test.ts` 新增扫描测试：源码中所有 `hasPermission("…")`、`permission: "…"` 与 `anyPermission: [...]` 字面量必须属于目录。实测现有 52 个唯一字面量（88 处调用）全部在 89 项目录内，因此**没有批量替换调用点**。
- OAuth scope `metaxisdata.mcp.read` 收进 `lib/oauth.ts` 的 `MCP_READ_SCOPE`，同意页改用它；`oauth.test.ts` 增加读 `backend/api/oauth/resource.go` 的 `MCPReadScope` 常量比对测试。

### FD9：i18n 豁免单一来源

- 单一来源为 `scripts/check-vue-i18n.mjs`：新增导出 `eslintUnusedKeyIgnores(localeFile, sourceDir)`，返回"脚本能追踪到的键 + `DYNAMIC_PREFIXES` 命中键"的并集。
- 返回并集而不是只展开前缀，是因为该 ESLint 规则**不能**跟随 `handleError/formatError/showSuccess` 的键，而脚本可以；只展开前缀会误报约 250 个键。并集后 ESLint 的 unused 集合是脚本的子集（见上文取舍）。
- `eslint.config.mjs` 从 650 行降到 102 行，删除 507 条手写 `ignores`；配置加载时多约 4s（完整 lint:ci 26s → 30s），`check-vue-i18n.test.mjs` 覆盖展开逻辑。

### FD10：流式 explain 可取消、有代际

- `explainSQL(input, { signal })` 与 `explainQualityExecution({ …, signal })` 透传 Connect `CallOptions`。
- `QualityExplainDrawer`：generation + `AbortController` + `onScopeDispose`；切换执行/关闭抽屉都会 abort 旧流，只有当前代际能写 state。新增"切换执行不混流""关闭即断流""abort 后仍到达的帧被丢弃"三条测试。
- `ExplainSQLPage`：同样的 generation + abort，卸载断流；`catch` 从 `String(e)` 改为 `formatError(e)`（该页仍无测试文件，属既有缺口）。
- `utils/error.ts` 新增 `isAborted`，`usePagedFetch` 改为复用，删掉私有 `isSuperseded`。

### FD4：`useAsync` + 定时器收敛

- 新增 `composables/useAsync.ts`：ticket + AbortSignal + `onScopeDispose`，返回 `{ data, loading, error, refresh, cancel }`。
- 9 处真实 debounce 与 8 处一次性定时器改为 `useTimers` 的两个包装（`useDebounced`/`useOneShot`），删除各自的 timer 变量、`clearTimeout` 与 `onBeforeUnmount`。
- `useAsync` 落地 2 个单资源页作为种子：`DataQualityIssueDetailPage`（含 issue→issue 路由 watch）与 `DataQualityTargetsPage`（目录单次聚合读）。29 处手写 `isLoading` 的其余部分随 FD3 拆分迁移。

### FD5：`usePolling` + 轮询收敛

- 新增 `composables/usePolling.ts`：`{ intervalMs, shouldContinue, pauseWhenHidden = true, timeoutMs?, continueOnError?, onError? }` → `{ start, stop, active, until }`。递归 `setTimeout`（上一轮结束才排下一轮，不重叠）、隐藏标签页暂停/可见恢复、独立 deadline 定时器、`stop()` abort 且解析 `until()`。
- 迁移：Executions 列表（5s，轮次内判 in-progress）、ExecutionDetail（3s）、SuggestionJobsPanel（2.5s，保留失败重读与 reload 语义）、SuggestionJobHistoryDrawer（2.5s）、QualityCheckDrawer `waitForRun`（2s + 60s deadline，`continueOnError` + `until()`；同时修掉旧实现"关闭抽屉时 wait 永不 resolve"的挂起）。
- `SuggestionJobTargetDebugDialog` 的重试定时器不是轮询，改为 `useOneShot`。

### FD6：URL↔过滤器收敛

- `utils/queryFilters.ts`：`queryString`、`readFlag`、`enumFilterValue`、`enumFromFilter`、`sameQuery`，纯函数、100% 覆盖。
- `composables/useUrlFilters.ts`：`{ keys, parse, serialize }` → `{ read, isCurrent, write, watchExternal }`。`write` 只增删本页 keys、保留外键、等值不写；`watchExternal` 只在外部变化时 adopt，自身 `replace` 通过记录比对跳过，无回环。
- 数据质量 scope 的解析/写回从 Checks/Suggestions 两页抽到 `utils/dataQualityTarget.ts`（`scopeFromQuery`、`writeScopeToQuery`）。
- 迁移：Checks、Issues、Suggestions、Targets 四页删除各自四件套并统一走 composable（`dc89611` 四页净删除 507 行）。
- **Executions 是混合形态**：它的 URL 不是过滤器集合，而是视图状态（`view/rule/target/run` + 局部 patch）。只把 URL 操作收敛到 `useUrlFilters` 并复用纯函数，`flatView`、跨页链接 chip、枚举 Select 的既有行为不变。

## 验证

- 前端完整门禁（栈顶）：`biome:ci`、`lint:ci`、`i18n`、`type-check`、`test:ci`（per-file 95/85）、`build` 全绿。
- `test:ci`：**137 个测试文件 / 1,374 条用例**通过（基线 `8acdbfd` 为 132 文件 / 1,320 条），覆盖率 98.6% stmts / 92.8% branch / 100% funcs / 98.6% lines，per-file 阈值通过；新增 `useTimers` 100% 覆盖，`usePolling` 97.1% 行 / 93.9% 分支。
- `type-check` 约 25s；`lint:ci` 约 30s（配置加载扫描源码多约 4s）；`build` 约 32s（vite 4s），Monaco 仍为独立异步 chunk（3.3 MB，`lazy-editor` 测试通过）。
- 迁移涉及页面/组件的定向测试全部通过：5 个数据质量页、QualityCheckDrawer/SuggestionJobsPanel/SuggestionJobHistoryDrawer/SuggestionJobTargetDebugDialog/TargetPicker/QualityExplainDrawer、ExplainSQL/Login/ManualSQL/MetadataBrowser/LineageGraph/AuditLogs/OpenLineage 设置与 run 详情等。
- 4 名 reviewer 的独立验证：其中一人独立重跑六道门禁（含基线 132/1,320 对照），另一人用 `git show` 逐页对照迁移前实现；评审期间一次 `loginRedirect.test.ts` 超时被确认为基线已有的测试时序抖动（单独运行 2.1s 通过），与本批无关。

## 本批未做（仍留给后续批次）

- FD2/FD3 god view 拆分（LineageGraphPage、三个 DQ 页、MetadataBrowserPage 的页面私有逻辑）——第三批。
- FD7 `RuleConfigForm` 按规则族拆分；FD11 markdown 懒加载；FD12 视图层测试补齐；FD13 `utils → api` 反向依赖；FD14 `<html lang>`/焦点陷阱；FD15 品牌名常量/请求超时。
- 29 处手写 `isLoading` 未逐一迁移（已迁 2 处，其余随 FD3 下沉）；`QualityCheckDrawer` 的 N+1 轮询优化；服务端"取消生成释放 LLM 配额"的协议语义。
