# P1-3（检查的执行语义：时间变量 / 采样 / 超时 / 检查时区）— 对抗性 review

Reviewer：一次独立对抗性 review 在全新上下文中、只读执行，对象为分支 `feat/dq-execution-semantics`
（base `b6ad40c`，review 时的 feature head `26e0e6c`，`.agents/worktrees/feat-dq-execution-semantics`）。
完整读了 `git diff b6ad40c..HEAD` 的 66 个文件（`backend/generated-go/`、`frontend/src/types/proto-es/`、
`proto/gen/grpc-doc/` 是 buf 产物，另在 `/tmp` 的副本里跑 `buf generate` 做幂等性核对），并读了
[`.agents/docs/data-quality.md`](../docs/data-quality.md) 的 D-85…D-88 / §3.14 / I-51…I-53 / §6 / §7、
[`docs/data-quality.md`](../../docs/data-quality.md)、
[`data-quality-competitive-gap-analysis.md`](data-quality-competitive-gap-analysis.md) 的 P1-3 行，以及
`frontend/AGENTS.md`、各层 `AGENTS.md` 与 i18n glossary。

四项已定范围不重新讨论：① 时间变量是封闭白名单（`${now}/${today}/${yesterday}` + 单个 `±Nm/h/d`），只允许出现在
filter 值与 custom SQL；② 分区窗口由 filter + 变量承担，不做 `${partition}`；③ 采样对用户是一个抽象意图
（不采样 / 按比例 / 按行数），机制只在方言层，不可表达时保存期拒绝而不是静默降级；④ 单检查 `timeout_seconds`
1–300（0=60s），批次 15m 截止不变；另加工作区级检查时区（默认 UTC）。代码或文档与这些决定矛盾时按缺陷上报。

## 实际验证过的内容

| 命令 / probe | 结果 |
| --- | --- |
| `gofmt -l backend/`；`golangci-lint run …`（changed packages，并与 `main` 干净 checkout 对比） | 干净；仅 `backend/api/v1/auth_service*.go` 两条 `multiline-if-init`，`main` 上同样存在 |
| `go test ./backend/component/dataquality/ ./backend/api/v1/ ./backend/runner/qualityrunner/ ./backend/store/` | 通过 |
| `make test-integration-data-quality`（真 PostgreSQL + MySQL） | **PASS**；旅程 33.7 s，改写后的 unique 查询在 MySQL 上实跑 |
| `go test -tags=integration -run '^TestDataQualityExecutionSemanticsMySQLRealServerIntegration$'` | **PASS**；时间变量预览/存储/执行、采样 + filter 同跑、按引擎/类型拒绝、超时往返 |
| `buf format -d` / `buf lint` / `/tmp` 副本 `buf generate` + diff | 干净 / 干净 / 生成物逐字节一致 |
| 前端 `biome check`、`lint`、`i18n`、`type-check`、`test run` | 全绿；170 files / 1790 tests，glossary 116 terms / 97 rules |
| 真实引擎 SQL probe（PG 16 / MySQL 8.3 / MSSQL 2019 / Doris 4.1.4 / StarRocks 4.1） | 采样 + filter 的 positional 与 numbered 绑定、`LIMIT`/`TOP` 有界派生关系、percent 采样（PG/MSSQL，含小数比例）、`LIMIT 0`/`TOP 0` 探测包装、单遍重复查询（MySQL `ONLY_FULL_GROUP_BY`、Doris、StarRocks）全部可执行；NULL 键语义与旧公式一致（PG 100/1/103、MSSQL 99/1/102、MySQL filtered 0/0/3） |
| Doris `TABLESAMPLE` 在 tablet 统计缺失/到位两种状态、SQL cache 开关 | 复现了 blocking 缺陷（见下表） |
| 未运行 | 前端 `audit:ui`（需要带 `--remote-debugging-port` 的浏览器）；PG/MSSQL 的 percent 采样走 runner/API 的端到端（集成 harness 的 DQ 实例是 MySQL，只做了 SQL 层 probe）；StarRocks 的真实 DQ 执行 |

Review 结束时工作区 `git status --porcelain` 为空，未改任何文件、未提交。

## 发现与关闭

全部发现已在 `7a93269`（后端）、`7444ff1`（前端）、`91306d4`（集成测试）、`26e0e6c`（文档）关闭；
post-fix 验证见下一节。

| # | 严重度 | 发现（证据） | 修复 | 状态 |
| --- | --- | --- | --- | --- |
| B1 | **blocking** | Doris 的 `TABLESAMPLE` 按 tablet 采样：tablet 行数统计缺失时（刚导入/新分区）**读全表却仍记 `sampled=true`**，其语句缓存还能把结果固化在字节稳定的规则 SQL 上。本地 Doris 4.1.4 复现：新导入的 8 tablet / 8000 行表，`TABLESAMPLE (10 PERCENT)` 与 `(50 PERCENT)` 都返回 8000；统计到位后恢复为 1/8。违反 I-51 | 从 `mysqlDialect.SupportsTableSample`/`SampledRelation`、前端 `PERCENT_SAMPLING_ENGINES`、D-87/§7 #31/用户文档中移除 Doris；按比例只保留 PG/MSSQL | 已修复 |
| S1 | should-fix | 文档四处承诺 `timeout_seconds` 越界 clamp，但 runner 先跑 `ValidateConfig`，越界值直接 `ERROR(UNSUPPORTED)`，`CheckTimeout` 的 clamp 不可达 | 严格范围移到保存/预览路径（`ValidateRule`）；`ValidateConfig` 不再拦该字段，存量越界行由 `CheckTimeout` clamp；文档同步 | 已修复 |
| S2 | should-fix | percent 边界四层不一致：后端 (0,100)、proto 注释 1–99、前端 1–99、D-85 (0,100)。API 写入 0.5% 的规则在编辑器里无法再保存 | 后端改为 1–99；D-85/§4.2/§7 同步 | 已修复 |
| S3 | should-fix | custom SQL 的 token 写在注释或 PG `$$…$$` 里能通过保存期解析，执行时渲染成占位符却多带一个参数 → 运行期错误，与 D-86 “保存期拒绝”的承诺不符 | custom SQL 改为渲染**服务端生成的 ISO 字面量**（不引参数），保存期与执行期同一渲染；注释/字符串里惰性、不影响语句 | 已修复 |
| S4 | should-fix | 用户自己写的 `$1`/`@p1` 会与变量生成的占位符编号冲突（PG/MSSQL 可能静默错绑） | 同上：不再生成占位符，用户占位符原样保留 | 已修复 |
| S5 | should-fix | 单遍重复检测新增的 `keyed` 别名可能与目标真实列同名（`all_columns` 下使外层引用歧义） | 别名改为 `__dq_keyed`，并避开目标列集（冲突时 `__dq_keyed_2`…） | 已修复 |
| S6 | should-fix（取舍） | 实例引擎尚未加载时编辑器乐观提供“按比例”，MySQL 目标上保存会被服务端拒绝 | 保留（避免实例缓存失败时卡死已存的 percent 规则），在 §7 #31 与用户文档写明服务端兜底 | 已记录 |
| S7 | should-fix | `schema_drift` 没有目标查询，却显示“查询超时”控件（暗示一个不存在的预算） | 前端对元数据检查隐藏该字段；两者都不适用时整组隐藏 | 已修复 |
| S8 | nit | `execution.go` 注释仍写“今天只有 CEL 规则可被采样” | 注释更新为采样由 config 决定 | 已修复 |
| S9 | should-fix（文档） | `docs/data-quality.md` 声称干跑成功会展示语句与参数，实际 UI 丢弃了预览响应（只在失败详情里显示） | 文档改为“被拒绝时错误里会给语句与绑定值；成功不展示；变量在执行时才解析” | 已修复 |
| N1 | nit | `${today-1d}` 用固定 24h 减法而 `${yesterday}` 用日历减一天，DST 日会不一致；超长数字偏移可能在判界前回绕 | 日偏移改用 `AddDate`，`h`/`m` 用时长；偏移先按天等价判界再缩放 | 已修复 |
| N2 | nit | 切换检查类型会清空刚设置的超时/采样（它们是类型无关的） | 切换时保留执行设置 | 已修复 |
| N3 | nit（自查补充） | 采样时 `sample_rows = total_rows`，但 `group_check` 的总数是分组数，不是行数 | `group_check` 不写 `sample_rows`；D-87/I-51 注明 | 已修复 |
| N4 | nit | §6 把“存储行再被拒”写成只发生在 `Compile`，实际第一道是 `ValidateConfig` | 文档措辞修正 | 已修复 |
| N5 | nit | `update_mask=data_quality` 整体替换消息，老客户端会把检查时区清回 UTC | 记录为 §7 #34 的已知行为（默认值本就是 UTC） | 已记录 |

## post-fix 验证

| 命令 | 结果 |
| --- | --- |
| `go test ./backend/component/dataquality/ ./backend/api/v1/ ./backend/runner/qualityrunner/ ./backend/store/` | 通过（含新增的 DST/溢出、contract filter 变量、用户 `$1`、注释/`$$`、别名避让、`group_check` sample_rows、超时 clamp 用例） |
| `gofmt -l backend/`、`go build -o /tmp/… ./backend/bin/server/main.go`、`golangci-lint`（changed packages） | 干净 / 通过 / 仅剩 `main` 上已有的两条告警 |
| `buf format --exit-code` / `buf lint` / `buf generate` + `git status --porcelain` | 干净 / 干净 / 无生成物漂移 |
| `go test -tags=integration -run '^(TestDataQualityExecutionSemantics…|TestDataQualityJourney…)$'` | **PASS**（37.4 s）。新场景现在覆盖：采样 + filter 同时生效的真实执行；custom SQL 的预览与执行语句都含渲染后的日期、不含 token；单遍重复检测的 NULL 键语义（`failed_rows=0/total=4`；`count_nulls` 时 `failed_rows=1`） |
| 前端 `biome check` / `lint` / `i18n` / `type-check` / `test run` | 全绿；170 files / 1790 tests |

## 残留与已知边界

- PG/MSSQL 的 percent 采样没有 CI 端到端（集成 harness 的 DQ 实例是 MySQL），只做到真实引擎 SQL probe；Doris 已不再适用。
- 没有“查询真的超时”的用例，也没有失败样本查询共享超时预算的用例；批次 15m × 单检查超时的交互是代码阅读结论。
- 前端在实例引擎未知时乐观提供“按比例”（服务端兜底拒绝），见 §7 #31。
- `LoadLocation` 每次执行调用一次（可缓存，未做）；干跑失败时错误详情会显示真实 SQL（含机制词），这是诊断需要，未在配置界面暴露。
- 边界本身写在 [`.agents/docs/data-quality.md`](../docs/data-quality.md) §7 #2、#31–#34 与 [`docs/data-quality.md`](../../docs/data-quality.md) 的 Troubleshooting。
