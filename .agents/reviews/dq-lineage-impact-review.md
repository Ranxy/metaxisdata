# P1-7（血缘影响面、根因候选与消费覆盖率）— 对抗性 review

Reviewer：一次独立对抗性 review，在全新上下文中只读执行。对象为分支 `feat/dq-lineage-impact`
（base `main@5357c2a`，HEAD `ec69c21`；feature commits `bb93e3d` 后端/proto、`bb36ead` 前端、
`ec69c21` 文档）。

**快照与工作树**：review 开始时 `git rev-parse HEAD` = `ec69c21`，`git status --short` 为空（干净）；
全部验证与 probe 结束后再次检查仍为空，期间无并发提交，因此所有结论固定到 **`ec69c21`**。
本 review 未修改任何产品代码、测试、文档或生成物，未提交；唯一写入是本文件（写入后
`git status --short` 只有这一条未跟踪文件）。`build/metaxisdata` 是命令要求的构建产物，已被
`.gitignore` 忽略（`git check-ignore` 命中）。

**读了什么**：`git diff 5357c2a..HEAD`（56 文件、+8313/−371）全文与每个新文件全文，含
`backend/store/{lineage_impact,quality_impact}.go`、`backend/store/meta_schema_fingerprint.go` 的新查询、
`backend/api/v1/data_quality_impact.go`、`backend/api/v1/data_quality_report.go`、`backend/component/
qualityreport/{report,labels}.go` 与 `report.html.tmpl`、`backend/mcp/{tool,server}.go` 与三份测试、
proto 新 RPC/消息、集成场景、前端 `utils/dataQualityImpact.ts`、三个新组件、三个接线页面、locale/
glossary/i18n 脚本与 `proto/gen` 产物。为判定正确性另读了未改动的依赖实现：
`backend/store/{column_lineage,quality_target,quality_execution,quality_overview,meta_resource,
meta_resource_history,meta_schema_fingerprint}.go`、`backend/component/dataquality/target.go`、
`backend/api/v1/{data_quality_convert,data_quality_target,lineage_service_graph}.go`、
`backend/runner/qualityrunner/{admission,schedule}.go`、`backend/common/guid.go`、
`backend/api/v1/{audit,acl_interceptor}.go`，以及 `backend/mcp/{ref,guide}.go`、`backend/mcp/guide.md`、
`docs/{data-quality,mcp}.md`、`.agents/docs/{data-quality,mcp-server,i18n-glossary}.md`、
`AGENTS.md`、`backend/AGENTS.md`、`backend/mcp/AGENTS.md`、`proto/AGENTS.md`、`frontend/AGENTS.md`、
`.agents/docs/frontend-ui.md`。probe 在 `/tmp/dqprobe`（`git archive HEAD` 的干净副本 + 临时 `_test.go`）
上跑，未触碰工作区。

**范围核对**：diff 与 P1-7 的声明一致：两个只读 RPC + 报告章节 + 两个 MCP 工具 + 三处前端；
**没有**迁移、新表、新权限、新审计注解、路由改动（`frontend/src/router/` 无 diff、`audit-ui-layout.mjs`
无 diff）；生成物由 `buf generate` 重放验证无漂移；除此之外只有文档与 gap-analysis 记录。

## 实际验证过的内容

| 命令 / probe | 结果 |
| --- | --- |
| `git log --oneline main..HEAD`；`git status --short` | 3 commits（`bb93e3d`/`bb36ead`/`ec69c21`）；工作区干净（开始与结束都是） |
| `gofmt -l backend/` | 空（0 行，exit 0） |
| `golangci-lint run --allow-parallel-runners` | **exit 1**：默认 `max-same-issues: 3` 只显示 3 条 `revive/multiline-if-init`（`backend/api/v1/auth_service.go:638`、`backend/api/v1/auth_service_refresh.go:43`、`backend/migrator/migrator.go:228`），三个文件都不在 diff 中 → pre-existing。注意：本环境默认输出与任务预期的 `backend/plugin/db/{mysql,mssql}/sync.go` 不同（那是文件顺序/截断口径差异，不是分支问题；全量里确实同时有这两处） |
| `golangci-lint run --allow-parallel-runners --max-same-issues=0 --max-issues-per-linter=0` | **175 条，全部同一条 `revive/multiline-if-init`**（58 个文件，golangci-lint v2.14.0）；与 `git diff --name-only 5357c2a..HEAD` 求交集只有 **`backend/store/quality_impact.go`**（`:80`、`:189` 两条，文件为新增）→ **本 diff 新引入 2 条**（N1），其余 173 条在 diff 外 → pre-existing |
| `go test -count=1 ./backend/...` | **全部通过**（exit 0；61 个包 `ok`，无 `FAIL`） |
| `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go` | **exit 0**，`build/metaxisdata` 54,804,743 B |
| `go test -count=1 -tags=integration -run '^TestDataQualityImpactRealServerIntegration$' ./backend/test/integration/runner/` | **PASS**（`ok ... 20.988s`；真实 Docker PostgreSQL/MySQL，非 skip）：插边 → 覆盖率先有 gap、建规则后 gap 减少且 `monitored+gap=consumed` → 上游 FAIL 成为下游目标 `-1` 距离候选（带执行资源名/窗口/无结构变更）→ 报告章节出现 |
| `cd proto && buf format -w .` | exit 0；`git status --short` 无输出 |
| `cd proto && buf lint .` | exit 0 |
| `cd proto && buf generate`；再 `git status --short` | exit 0；**无任何变更**（`backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/` 与提交逐字节一致） |
| `pnpm --dir frontend biome:check` | exit 0（558 files，No fixes applied） |
| `pnpm --dir frontend lint` | exit 0 |
| `pnpm --dir frontend i18n` | exit 0（`i18n glossary: 121 terms and 97 rules match glossary.json.`） |
| `pnpm --dir frontend type-check` | exit 0 |
| `pnpm --dir frontend test run` | **175 files / 1877 tests 全过**（exit 0） |
| **walk 截断 probe**（`/tmp/dqprobe`，`git archive HEAD` + 临时测试；SQL 语义 fetcher：DISTINCT + `ORDER BY guid` + `LIMIT budget+1`） | **发现 S1**：菱形图 `a→{b1,b2}`、`b1→{b2,c}`、`b2→{c}`，`maxNodes=4`、`depth=3` 时 `walkLineage` 返回 `nodes=[a,b1,b2] depth=1 truncated=false err=nil`；`c` 已被层探测返回却被静默丢弃。换成真实预算（100/50）只是同一机制换到更大层 |
| **报告 never-run probe**（同一副本，`qualityReportImpactOf` + `qualityreport.Render`） | **发现 S2**：summary 为 `enabled_rule_count=1, worst_status=UNSPECIFIED` 的资产映射出 `WorstStatusKey="UNSPECIFIED"`，报告渲染 `<span class="badge status-unspecified">UNSPECIFIED</span>`；同一响应在 SPA 上是 “Never run” |
| **报告 upstream 截断 probe** | **发现 S3**：`upstream_truncated=true` 的响应经 `qualityReportImpactOf`→`Render` 后，文档中没有任何 “cut short/truncat/omitted” 提示；`qualityreport.Impact` 根本没有该字段 |

## 结论摘要

- **Blocking：0**（明确：没有 blocking）。
- **Should-fix：3（S1–S3）**：一处 walk 截断漏报会静默给出“完整”的错误答案；一处报告把“从未运行”渲染成裸枚举 `UNSPECIFIED` 且与 SPA 不一致；一处报告吞掉上游 walk 截断标记。
- **Nit：6（N1–N6）**：新文件 2 条 lint、MCP page_size 契约、两处文档过时数字、MCP/文档把“概览覆盖率”错记成 `get_quality_signal`、报告计数文案单复数、问题页窗口文案。
- 其余声明（只读/不审计/无迁移/四类对象/两层分母/结构变更三条件/精确选择/HTML 转义/2 MiB 裁剪/工具守卫/前端权限与竞态）逐条核对与文档一致，见“刻意决策与已核实一致”。

## 发现

| # | 严重度 | 一句话 |
| --- | --- | --- |
| S1 | should-fix | `walkLineage` 在“截断前缀全是已访问节点”时返回 `truncated=false`，静默丢弃层探测已返回的新节点 |
| S2 | should-fix | 报告把“已监控但从未运行”的下游资产渲染成裸枚举 `UNSPECIFIED`，与 SPA 的 “Never run” 不一致 |
| S3 | should-fix | 报告丢弃 `upstream_truncated`，上游根因被 walk 截断时仍以完整列表呈现 |
| N1 | nit | 新文件 `backend/store/quality_impact.go` 引入 2 条 `multiline-if-init`，被默认 `max-same-issues=3` 掩盖 |
| N2 | nit | MCP 消费覆盖率工具的 `page_size` schema 宣称 default 25/max 200，RPC 实际是 default 20/max 100 |
| N3 | nit | `.agents/docs/mcp-server.md:26` 仍写 “这 15 个只读 handler”，`.agents/docs/data-quality.md:769` 仍写 “工具数 15”（现为 17） |
| N4 | nit | MCP 工具描述与 `docs/mcp.md` 把“概览的对象覆盖率”错记成 `get_quality_signal` 的覆盖率（后者是单对象的列覆盖率） |
| N5 | nit | 报告计数文案无单数形式：会输出 “1 more candidates are not shown.” / “1 walked objects …” |
| N6 | nit | 问题详情页复用影响卡，窗口标题仍写 “in the {hours} hours before this run”，但锚点是 `first_seen_time`，没有 run |

### S1 — walk 截断漏报：`truncated=false` 但新节点被丢弃

**证据**：`backend/api/v1/data_quality_impact.go:203-224`。层探测返回 `LIMIT budget+1` 行（`remaining+1`），
代码先 `peers = peers[:remaining]` 再对 `visited` 去重；若被截断后留下的前缀**全是已访问节点**，
`next` 为空，循环在 `:217` `break`，`truncated` 标志在 `:222` 之前就被跳过，最终 `:226` 返回
`false`。此时被丢弃的探测行（或更后面的行）可能是真正的新下游对象。

probe（干净副本，SQL 语义 fetcher）的最小复现：

```
图：a→{b1,b2}, b1→{b2,c}, b2→{c}    （无自环、无环）
walkLineage(ctx,"a",depth=3,maxNodes=4,step=+1,fetch)
=> nodes=[{a 0} {b1 1} {b2 1}] depth=1 truncated=false err=<nil>
```

预算过程：level1 用掉 2 个新节点（`remaining=2`，不截断）；level2 时 `remaining=1`，层探测按
`ORDER BY guid` 返回 `{b2(已访问), c(新)}`，截到 `[b2]` 后无新节点 → 直接 `break`，`c` 消失且不报截断。
把预算换成真实的 100/50 只是把“已收集 99/49 个节点后的某层”换成同一条件；菱形/汇合血缘
（多个 view 共享上游）就会产生已访问节点排在前面。

**为什么重要**：`downstream_truncated` / `upstream_truncated` 是这个只读投影唯一的完整性信号，
报告、SPA、MCP 都靠它们；`data_quality_impact.go:178-181` 的注释明确承诺“预算/截止时间截断要报成
truncation，而不是静默缩短列表”，`proto/v1/v1/data_quality_service.proto:3042-3049` 与 D-92/§3.16
也这样写。现在一个被截断的 walk 可以声称自己完整，且丢掉的恰好是“受影响资产/根因候选”。

**修复（最小）**：不要先按 `remaining` 切再对 `visited` 去重——遍历 `peers`（最多 `remaining+1` 行）
把未访问节点加到预算为止；只要 `len(peers) > remaining`（或预算被填满）就置 `truncated`，并把
`truncated` 的判断放到 `len(next)==0` 的 `break` 之前。补一个“层前缀全是已访问节点”的回归用例
（现有 `TestWalkLineageReportsTruncation` 只覆盖了前缀全是新节点的情况：
`backend/api/v1/data_quality_impact_test.go:68-78`）。

### S2 — 报告把“从未运行”渲染成 `UNSPECIFIED`（与 SPA 不一致）

**证据**：从未运行过的目标在目录聚合里没有 `results` 行 → `COALESCE(r.worst_rank, 0)` =
`QualityTargetStatusFromRank(0)` = `UNSPECIFIED`（`backend/store/quality_target.go:329`、`:162-178`、
`:426-461`）。报告映射原样转成渲染键：`entry.WorstStatusKey = qualityReportEnumKey(summary.GetWorstStatus())`
（`backend/api/v1/data_quality_report.go:477`），而 `qualityReportEnumKey` 会剥掉 `QUALITY_STATUS_`
前缀得到非空字符串 `"UNSPECIFIED"`（`:429-437`）。模板侧“从未运行”的分支比较的是空串
（`backend/component/qualityreport/report.go:975 case asset.WorstStatusKey == ""`），因此永远不可达，
走默认分支 `statusLabel(...)` → `humanizeEnum("UNSPECIFIED")`（`report.go:979-982`）。

probe 渲染出的行：

```html
<td>inst/shop/public/orders</td><td>Table</td><td class="num">1</td>
<td><span class="badge status-unspecified">UNSPECIFIED</span></td>
<td class="num">0</td><td>Never run</td>
```

同一份数据在 SPA 是正确的：`frontend/src/components/data-quality/QualityImpactCard.vue:335-338`
把 `UNSPECIFIED` 映射成 `dataQuality.targets.neverRun`（“Never run”）。

**为什么重要**：报告是“可转发给别人的文件”，`UNSPECIFIED` 是内部枚举泄漏而不是人能读的状态；
这也违反 D-92/§3.16 明确写下的“未监控/全暂停/未运行三态与面板一致”，以及 `report.go:975` 的
注释“never run is the honest state”。现有测试没有覆盖这个状态：`report_test.go` 的
`TestRenderImpactSection` 只有 FAIL/全暂停/未监控（`backend/component/qualityreport/report_test.go:485-544`），
`TestQualityReportImpactOf` 的 summary 只带 FAIL（`backend/api/v1/data_quality_report_test.go:407-489`），
尽管 §3.16 的测试清单声称报告测试覆盖了“未运行”。

**修复**：在 `qualityReportImpactOf` 里把 `UNSPECIFIED`（以及 summary 缺失时）留成空
`WorstStatusKey`，让渲染层走 “Never run”；或给报告模型加显式的 never-run 态。补一条
`summary{enabledRuleCount>0, worstStatus: UNSPECIFIED}` 的报告/映射用例。

### S3 — 报告丢弃 `upstream_truncated`

**证据**：`qualityReportImpactOf` 只搬了 `DownstreamTruncated`、`DownstreamOmitted`、`RootCausesOmitted`
（`backend/api/v1/data_quality_report.go:462-469`）；`qualityreport.Impact` 结构里根本没有 upstream
截断字段（`backend/component/qualityreport/report.go:238-256`），模板也没有对应提示
（`report.html.tmpl` 只有 `Impact.CausesNote`）。probe：给 `GetQualityTargetImpactResponse` 设
`UpstreamTruncated: true` 后渲染，文档中没有任何 cut short/truncated/omitted 提示。

**为什么重要**：上游走查上限 50 节点、两层（`backend/api/v1/data_quality_impact.go:34-35`、
`:124-127`），一个根目标有 50+ 直接上游（宽 join/union 的视图很常见）就会 `upstream_truncated=true`；
此时报告仍可能输出 “Possible upstream causes” 或 “No upstream check failed or changed structure in
the window.”，让读者以为根因列表是完整的。而同一个响应在 SPA 上会显示
`dataQuality.impact.footnoteUpstreamTruncated`（`frontend/src/utils/dataQualityImpact.ts:224,241-245`），
API/proto 也专门给了这个字段（`proto/v1/v1/data_quality_service.proto:3049`）——报告是唯一吞掉它的
消费者，属于实现与“报告章节说明截断/省略”承诺的不一致（报告对下游截断是写了提示的：
`report.go` 的 `ImpactTruncated`）。

**修复**：给 `qualityreport.Impact` 加 `UpstreamTruncated bool` + 一条 en/zh label，在
`qualityReportImpactOf` 映射，`newImpactView` 追加到 `CausesNote`，并补一个
`TestRenderImpactSection` 的截断用例。

### N1 — 新文件引入 2 条 lint 告警，且被默认门禁掩盖

`backend/store/quality_impact.go:80`、`:189` 的 `rows.Scan(...)` 跨行 `if-init` 触发
`revive/multiline-if-init`（文件是本 diff 新增）。默认 `golangci-lint run` 因 `max-same-issues: 3`
只显示别处 3 条旧告警，看不出来；`--max-same-issues=0` 全量 175 条中和 diff 求交只有这个文件的
这 2 条（见验证表）。仓库同规则已有 173 条，属既有实践，不阻断；但本分支自己上一轮 P1-4 review
的结论是“新增 0 条”，建议把这两处改成先赋值再判断（`scanErr := rows.Scan(...); if scanErr != nil`），
与仓库现状保持一致。

### N2 — MCP 消费覆盖率工具的 `page_size` 契约与 RPC 不一致

`get_quality_consumption_coverage` 用通用 `pageSizeProperty()`（`backend/mcp/tool.go:210`），它来自
`defaultPageSize=25` / `maxPageSize=200`（`tool.go:23-27`、`:2106-2107`），于是工具说明告诉模型
“default 25, max 200”；但该 RPC 的 proto 写明 “0-100. Zero means 20; a larger value is coerced to 100”
（`proto/v1/v1/data_quality_service.proto:3074-3076`），服务端 `qualityConsumptionGapLimit`
也把 >100 收成 100（`backend/api/v1/data_quality_impact.go:501-510`），`.agents/docs/mcp-server.md`
的工具表也写的是 `page_size (20/100)`。模型要 200 只拿到 100，且会按 200 判断“列表是否完整”。
建议给该工具用专门的描述（default 20 / max 100），或按文档口径统一上限。

### N3 — 维护文档里的工具数仍是 15

本分支把 MCP 工具面更新到 17（`.agents/docs/mcp-server.md:9`、`:89`、`wantTools`、
`backend/test/integration/runner/mcp_service_test.go:199` 都已改），但同一文档 `:26` 仍写
“这 15 个只读 handler”，`.agents/docs/data-quality.md:769` 的 P1-8 行仍写 “工具数 15”。
建议顺带改成 17（或改成不写死的表述）。

### N4 — MCP/用户文档把“概览的对象覆盖率”错记成 `get_quality_signal` 的覆盖率

`backend/mcp/tool.go:206` 的消费覆盖率工具描述写 “This is not the coverage get_quality_signal
reports: there every monitorable object counts…”，`docs/mcp.md:129` 同样写
“Its denominator is not `get_quality_signal`'s coverage — there every monitorable object counts…”。
但 `get_quality_signal` 返回的是**单对象**的列覆盖率（`qualitySignalResult.CoveredColumnCount/
TotalColumnCount`，`backend/mcp/tool.go:666-687`），它没有 workspace 的对象覆盖率；“每个可监控对象
都进分母”的那张覆盖率是 `GetQualityOverview`（`proto/v1/v1/data_quality_service.proto:2682-2691`），
而它没有 MCP 工具。这正是本特性最容易混的两个分母，把对照对象写错会引导模型拿列覆盖率和
消费覆盖率互相比。建议改成“the overview's coverage（本 MCP 面不提供）”或只写分母口径。

### N5 — 报告计数文案没有单数形式

`backend/component/qualityreport/labels.go:282-284` 的三条英文模板是固定复数：
`"%d more assets are not shown."`、`"%d more candidates are not shown."`、
`"%d walked objects are not listed…"`，count=1 时输出 “1 more candidates are not shown.” /
“1 walked objects …”。测试甚至把这个错误串钉住了（`report_test.go:544` 断言
`"1 more candidates are not shown."`）。建议改成 “1 more candidate / N more candidates” 两套，
或改成不带复数的 “N additional … omitted”。

### N6 — 问题详情页的窗口文案说 “before this run”

问题详情把影响卡锚定在 `issue.firstSeenTime`（`frontend/src/pages/data-quality/DataQualityIssueDetailPage.vue:185-189`），
但卡片的窗口标题来自 `dataQuality.impact.window` = “in the {hours} hours before this run”
（`frontend/src/locales/en-US.json:809`、`zh-CN.json:809`）。问题页没有“本次运行”这个对象，
文案应改成与锚点一致的中性表述（如 “before the issue was first seen”），或让卡片按页面传
锚点名词。

## 刻意决策与已核实一致（下一位读者不必重新纠结）

以下都按“记录在案的决策不是 bug”处理，并核对了实现没有与决策矛盾：

- **只读、无表/迁移/权限/审计**：两个 RPC 只有 `google.api.http` + `permission =
  "metaxisdata.dataQuality.rules.list"`，无 `audit=true`；handler 与 store 只查不写
  （`data_quality_impact.go:63-100`、`store/quality_impact.go`、`store/lineage_impact.go`）；
  `auth.go` 的 `Audit` context 由注解驱动，未注解即不审计。
- **四类对象 + omitted 口径**：`qualityLineageAssetType` 只报 TABLE/EXTERNAL_TABLE/VIEW/
  MATERIALIZED_VIEW（`data_quality_impact.go:253-270`），其余被走到的对象计入
  `omitted_downstream_count`；同 GUID 多类型按该顺序取一（`:261-269`），`ListMetaRegistryResourceDigest`
  的批量 GUID 过滤确实只查给定 GUID（`store/meta_resource.go:330-336`）。
- **“空 = 已记录血缘里没有”的措辞**：proto 注释、报告 `ImpactDescription`、SPA 三处文案、
  MCP 工具描述、`docs/data-quality.md` 全部明说“不是完整消费方列表”，没有发现把空列表说成
  “没有消费方”的文案。
- **根因是候选 + 窗口**：`[as_of-window, as_of]`，`as_of` 客户端给、未来按 now
  （`data_quality_impact.go:465-475`），`window_hours` 0→24/>168 收为 168、`depth` 0→2/>3 收为 3
  （`:477-499`），与 proto/文档一致（负值也按 0 处理，属同一 coercion 口径）。
- **结构变更三条件**：`meta_registry_schema_fingerprint.go:224-242` 要求指纹 `updated_at` ∈ 窗口、
  当前 open history 行 `valid_from` ∈ 窗口、且存在 `valid_from < from` 的更早版本；我追了
  `upsertMetaRegistryHistory`（metadata hash 相同不新开版本，`meta_resource_history.go:14-30`）、
  `BatchDeleteMetaRegistryAt`（关闭 open 行并删指纹，recreate 会留下旧版本行 → 会被报）、
  `buildUpsertMetaRegistrySchemaFingerprintQuery`（hash guard 只在形状变时推进 `updated_at`）：
  新建对象（无更早版本）与升级回填（当前版本早于窗口）都被排除，删除后同名重建会被报，
  与 D-92/§7 #40 一致。
- **失败证据**：`state=COMPLETED AND status=FAIL`（int16 绑定）、`(target_guid,target_type::INT)
  IN (SELECT * FROM unnest($1::text[],$2::INT[]))` 成对匹配、`started_at` 闭区间、
  `MAX(started_at)` 是最新、`(ARRAY_AGG(id ORDER BY started_at DESC,id DESC))[1:$5]` 在 SQL 里截断
  （`quality_execution.started_at` NOT NULL，DESC 无 NULL 排序问题）、无证据的目标从 map 缺席而不是
  0（`store/quality_impact.go:22-102`），集成场景实际跑过这些 SQL。
- **精确选择**：资产监控状态走一次 `ListQualityTargets(TargetKeys)`（`data_quality_impact.go:294-311`），
  `TargetKeys` 谓词被下推进 `live/targets/latest/issues/drafts` 各自的扫描（`store/quality_target.go:379-405`），
  没有分页、没有逐资产 N+1；summary 缺失=未监控、`enabled_rule_count=0`=全部暂停，
  与 proto 注释一致。
- **路径构造**：`qualityTargetPathFromGUID` 与 `dataquality.ResolveTarget` 完全同规则（3 段无 schema、
  4 段有 schema、最后一段是 object，`data_quality_impact.go:516-530` vs `component/dataquality/target.go:67-77`），
  GUID 转义由 `common.SplitMetaGUID` 处理。
- **消费覆盖率**：分母 = 有出边且自环排除、存活实例、四类对象；分子 = ENABLED 且非删除检查
  （`store/quality_impact.go:104-163`）；`COUNT(*) OVER ()` 与 `COUNT(*) FILTER (WHERE NOT monitored)
  OVER ()` 在 LIMIT 前算总数，`ORDER BY monitored ASC, readers DESC, guid ASC` 保证限额优先花在 gap 上，
  因此 gap 少于限额时 `monitored+gap=consumed` 仍成立（集成断言过）；确实是一条按 `source_guid`
  分组的 SQL，不是多次扫描。概览覆盖率与本指标两个分母的差异在 proto/文档/UI 都有说明。
- **报告**：锚点 `failedExecutions[0].StartedAt` 确为窗口内最新 FAIL（`data_quality_report.go:91-100` 取 `failedExecutions[0]`；`ListQualityExecutions` 默认
  `ORDER BY started_at DESC, id DESC`，`store/quality_execution.go:181-186`）；无失败时不列根因并
  渲染“no failing check anchors an upstream window”；渲染上限 10 资产/5 候选并写未显示数量；
  `html/template` 自动转义（`report.go:7`，全包无 `template.HTML` 拼接路径/证据）；章节无外部资源，
  自包含；受上限约束的 15 行影响面不会让 2 MiB 裁剪循环无法收敛（`Render` 的预算里没有影响面，
  但行数与路径长度由目标对象名决定，正常 schema 名远小于余量）。
- **MCP**：两个工具的 `RPC`/`Permission` 与 proto 注解逐条相等、都在只读权限白名单、都走 `dispatch`、
  都复用 RPC handler；`wantTools` 17 条、`tool_schema_test` 参数结构对账、集成工具数 17 均已同步；
  `resolveObject` 只做查表/搜索、不拼 GUID；`depth/window_hours` 的 0 原样透传到 RPC 后按默认处理；
  不可挂检查的对象在 Run 内先拒绝（`tool.go:1514-1520`）。
- **前端**：影响卡 `v-if="canView && hasTarget"` 且在 `load()` 里再次检查，没有 `rules.list` 时不发请求
  不渲染；执行详情只在 finished FAIL/WARN 显示（`isExecutionInProgress` 排除 PENDING/RUNNING，
  ERROR/ABANDONED 的 outcome 不是 FAIL/WARN），锚点=执行开始时间；问题页锚点=`firstSeenTime`；
  卡片/对话框都有 generation + AbortController 防旧答案写回；概览消费覆盖卡在 `PageState` 之外、
  自己的请求/错误，并随刷新按钮 `refreshKey` 重读；i18n 键在两种 locale 都存在（门禁通过）、
  动态前缀（distance/window/footnote）在 `check-vue-i18n.mjs:112-117` 有注释说明；`frontend/src/router/`
  与布局审计脚本无 diff，没有改动注册路由。
- **proto/API 卫生**：自定义 GET 读的路由与相邻既有 `qualityTargets:get`/`qualityOverview` 风格一致；
  字段注释与行为一致（除 S2/S3 的实现缺陷外）；生成物 `buf generate` 重放零 diff（说明不是手改）；
  本特性没有让任何 proto 字段变成死字段（`depth_reached` 虽未被 SPA/报告使用，但 MCP 原样返回）。

## 残留边界

- `pnpm --dir frontend audit:ui` 未运行（环境无 live app/CDP 浏览器），与作者记录一致；布局审计的
  注册路由未被本分支改动。
- 未跑 `make test-integration` 全量/`-race`；按任务只跑了指定的影响面集成场景（PASS）与
  `go test ./backend/...`（全部单测）。
- 本 review 的 probe 在 `/tmp/dqprobe` 的 `git archive HEAD` 副本上执行，不在仓库里留下任何文件；
  工作区在 review 结束时仍只有本 review 文件是未跟踪的。

## Post-fix 复验（Lead，2026-11-02）

Review 定稿于 `ec69c21`。以下修复提交为 **`157b4eb`**（`fix(data-quality): close the P1-7 review findings`），
本文件随后单独提交；产品代码/测试/文档的修复范围与下面逐条对应，review 本身未改动。

| # | 状态 | 修复 | 复验 |
| --- | --- | --- | --- |
| S1 | **fixed** | `walkLineage` 把截断判定放到去重之前：层探测超过剩余预算即置 `truncated`；遍历 peers 时预算填满也置 `truncated`，`len(next)==0` 时先返回截断再 break。补 `TestWalkLineageReportsTruncationBehindAVisitedPrefix`（review 的无环菱形图，断言 `c` 被收入且 `truncated=true`）。 | `go test ./backend/api/v1/ -run TestWalkLineage` 全过；原 `TestWalkLineage*` 用例不变仍过 |
| S2 | **fixed** | `qualityReportImpactOf` 只在 `worst_status != UNSPECIFIED` 时写 `WorstStatusKey`，于是渲染层走 “Never run”；`TestQualityReportImpactOf` 增加 never-run 资产断言（空 key + Monitored），`TestRenderImpactSection` 断言报告出现 “Never run/未运行” 且不出现 `UNSPECIFIED`。 | 上述两测试 + 全量 `go test ./backend/...` 过 |
| S3 | **fixed** | `qualityreport.Impact` 增加 `UpstreamTruncated`、`labels` 增加 en/zh 文案、`newImpactView` 追加到上游脚注、`qualityReportImpactOf` 映射；响应设置 `upstream_truncated=true` 的渲染用例断言中英提示都出现。 | 同上 |
| N1 | **fixed** | `backend/store/quality_impact.go` 两处 `rows.Scan` 改为先赋 `scanErr` 再判断。`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0` 对 diff 路径输出为空（全量仍是 173 条既有 `revive/multiline-if-init`，均在 diff 外）。 | lint 全量 + 路径过滤 |
| N2 | **fixed** | `get_quality_consumption_coverage` 用自己的 `page_size` schema（1–100、0=20、>100 收为 100），并把原值直接交给 RPC（不再先按通用 25/200 规范化）。 | `go test ./backend/mcp/` 过；工具 schema 对账测试不变 |
| N3 | **fixed** | `.agents/docs/mcp-server.md` 的 “15 个只读 handler” 与 `.agents/docs/data-quality.md` 的 “工具数 15” 均改为 17。 | 文档 grep |
| N4 | **fixed** | 消费覆盖率工具的 Description 与 `docs/mcp.md` 改成“不是概览的对象覆盖率（本只读面不暴露该 summary），也不是 `get_quality_signal` 的单对象列覆盖率”。 | 文档/mcp 工具描述 grep |
| N5 | **fixed** | 报告三条计数文案改为不含单复数歧义的措辞（`Failed checks in the window: %d · failed runs: %d`、`More assets not shown: %d.`、`More candidates not shown: %d.`、`Objects walked through but not listed…: %d.`），中文同步；渲染测试断言更新。 | `report_test.go` 过 |
| N6 | **fixed** | `QualityImpactCard` 新增 `anchor: "run" \| "issue"`；`impactWindowText`/`impactWindowFallbackText` 按锚点选 `dataQuality.impact.window` / `windowIssue`；问题详情页传 `anchor="issue"`，执行详情页保持 run。新增 util 与卡片测试（issue 文案出现、run 文案不出现）。 | 前端 `test run`：1880 用例过 |

修复后的全量门禁（均在 `157b4eb` 工作树上执行）：

| 命令 | 结果 |
| --- | --- |
| `gofmt -l backend/` | 空 |
| `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go` | exit 0 |
| `go test -count=1 ./backend/...` | 全过 |
| `golangci-lint run --allow-parallel-runners --max-same-issues=0 --max-issues-per-linter=0`；按 `git diff --name-only` 过滤 | 173 条既有 `multiline-if-init`，**diff 文件 0 条** |
| `go test -count=1 -tags=integration -run '^TestDataQualityImpactRealServerIntegration$' ./backend/test/integration/runner/` | PASS（22.151s，真实 Docker） |
| `cd proto && buf format -w . && buf lint . && buf generate`，再查生成物 status | 零 diff（生成物与提交一致） |
| `pnpm --dir frontend biome:check` / `lint` / `i18n` / `type-check` | 全部 exit 0 |
| `pnpm --dir frontend test run` | 175 files / **1880 tests** 全过（修复新增 3 个用例） |

残余：`pnpm --dir frontend audit:ui` 仍无法在本环境执行（无 live app + CDP），布局审计注册路由未改动；
全量 `make test-integration` 未跑（按任务只跑影响面场景）。两条均在 review 的“残留边界”内，不构成新风险。
