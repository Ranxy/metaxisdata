# P1-4（目标级 cron 调度、检查频率档与维护窗口）— 对抗性 review

Reviewer：一次独立对抗性 review，在全新上下文中只读执行，对象为分支 `feat/dq-schedule-tiers`
（base `3419066` / main，feature commits `5a76af5`、`a915414`、`600a30c`、`16a8267`）。

**快照与并发说明**：review 开始时 head 为 `16a8267`，`.agents/docs/data-quality.md` 与
`.agents/docs/notifications.md` 尚未提交（按要求 review 磁盘内容）。review 期间另一位作者先把文档提交为
`1d6fe34`（仅文档），随后又提交了 `18b46be`（把频率档改成按目标 slot 的 gate，并新增迁移
`0.1/0039##quality_execution_scheduled_at.sql`）与 `6f136e9`（文档同步）；review 结束前工作区又出现
新的未提交改动（`backend/common/schedule/{schedule,schedule_test}.go`、`backend/store/quality_{target,target_schedule}.go` 等），
从内容看是在补 DST 去重/IANA 时区收紧/cron 长度上限/缺 schedule 行容忍（分别对应 B2/N3/N4/S1）。这些都
**晚于本 review 的固定快照**，不属于本 review 的对象；本文所有结论固定到 **`1d6fe34`**。为排除并发影响，最后阶段在 `/tmp/dq-review`
（`git archive 1d6fe34` 的干净副本）上重跑了 gofmt/build/unit/lint/迁移集成/DQ 集成/新场景，并在此副本上
做 buf 生成物核对；前端在 1d6fe34 与 16a8267 完全一致的工作区中运行。本 review 未修改任何代码、文档或
生成物，未提交；唯一写入是本文件。

完整读了 `git diff 3419066..1d6fe34`（65 files / +4130 / −1208），包括后端 schedule 叶子包、store 查询与
迁移 0038、API/proto（v1 与 store 两份）、runner 推进、报告/CSV、前端 composable/组件/文案/glossary、
以及 `backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/` 的 buf 产物；读了
`AGENTS.md`、`backend/AGENTS.md`、`proto/AGENTS.md`、`frontend/AGENTS.md`、`backend/migrator/AGENTS.md`、
[`.agents/docs/data-quality.md`](../docs/data-quality.md) 的 D-89/D-90、§3.15、I-54/I-55、§4.6、§6、§7
#35–#38、[`docs/data-quality.md`](../../docs/data-quality.md)、
[`notifications.md`](../docs/notifications.md) 的 O-15/O-16/O-17、以及
[`data-quality-competitive-gap-analysis.md`](data-quality-competitive-gap-analysis.md) 的 P1-4 行。

**修复与复验（Lead 补记）**：`1d6fe34` 之后的提交 `18b46be`（B1：`quality_execution.scheduled_at` + 迁移 `0039`，gate 改为 slot 与完整 tier period 比较）、`6f136e9`（文档同步）、`7be0aed`（B2/S1/N1–N6 修复、回归测试、Makefile/README）已处理本 review 的全部发现；随后一位**独立的 post-fix verifier**（全新上下文、只读）在 `7be0aed` 上逐条复验并重跑了全部门禁，确认全部关闭，另报两个低severity残留——2 小时 DST fold 的重复墙钟与升级前 NULL `scheduled_at` 行的说明缺失——分别由 `f9f3fc5`（去重跳过整段 fold）与 `d7a9205`（文档补一句）处理。最终 HEAD 为 `d7a9205`。

## 实际验证过的内容

| 命令 / probe | 结果 |
| --- | --- |
| `gofmt -l backend/`（干净副本） | 空（exit 0） |
| `go build ./...`（干净副本） | exit 0 |
| `go test -count=1 ./...`（干净副本） | **全部通过**（exit 0；含 `store` / `api/v1` / `runner/qualityrunner` / `common/schedule`） |
| `golangci-lint run --allow-parallel-runners` | **exit 1**：显示 3 条 `revive/multiline-if-init`（默认 `max-same-issues: 3` 截断）；`--max-same-issues=0 --max-issues-per-linter=0` 实测 **176 条**，全部同一条规则。`git blame 1d6fe34` 显示其中 **4 条由本 feature 新增**（`backend/store/quality_target_schedule.go:182/198/342/620`，5a76af5），其余 172 条在 base 3419066 及更早已存在（`backend/migrator/migrator.go`、`backend/store/*.go`、`backend/server/init.go`、`backend/api/**` 等） |
| `golangci-lint run --allow-parallel-runners --build-tags=integration ./backend/test/integration/...` | **exit 1**：3 条 `multiline-if-init`（`backend/test/integration/env/service_env.go:1076`、`env/testenv.go:149`、`runner/schemasync_lineage_postgres_service_test.go:94`）。这三个文件都不在 diff 中，`main` 上同样存在 → **pre-existing** |
| `make test-integration-data-quality`（干净副本，Docker 真 PostgreSQL + MySQL） | **PASS**；`TestDataQualityJourneyMySQLRealServerIntegration` 13.7 s，总 34.3 s。注意该 make 目标只跑 journey（见 N5） |
| `go test -tags=integration -run '^TestDataQualityScheduleTiersRealServerIntegration$' ./backend/test/integration/runner`（干净副本） | **PASS**（47.4 s）。新场景覆盖 cron 往返、档位 head-only、强制两轮 gate、CSV/报告 |
| `go test -count=1 -tags=integration -v ./backend/migrator/`（干净副本） | **PASS**（32.5 s）：`TestMigrateSchemaFreshInstall` / `Upgrade` / `LegacyAdoption` / **`LATESTMatchesTheIncrementChain`** / `SerializesConcurrentReplicas` → 0038 与 `LATEST.sql` 一致 |
| `buf format -d proto`；`buf lint proto` | 均 exit 0，无 diff |
| `/tmp` 干净副本 `cd proto && buf generate` 后与提交产物逐字节 diff | `backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/` **全部无差异**（无生成物漂移、无手改） |
| `pnpm --dir frontend biome:check` / `lint` / `i18n` / `type-check` / `test run` | 全绿（exit 0）：biome 550 files；i18n 118 terms / 97 rules；vitest **171 files / 1812 tests** |
| **真实推进语义** probe（/tmp `postgres:16-alpine` + LATEST.sql schema，直接调用 `store.FindDueQualityTargets` / `CountPendingQualityTargetRules`；interval 目标按 runner 的 `next = 完成时刻 + interval` 推进，cron 目标按 `spec.Next(now)` 推进，run 在槽位后 2 s 开始） | **发现 B1**：`cron */15 + HOURLY` = 1 h 0 m（正确）；**`15m interval + HOURLY` = 45 m 4 s**；**`35m interval + HOURLY` = 35 m 4 s（每轮，档位失效）**；**`24h interval + WEEKLY` = 144 h（6 天）**；**`15m interval + DAILY` = 23 h 45 m 4 s**；`1h interval + HOURLY`（等节奏）= 每轮（正确） |
| 固定网格 probe（直接给 `next_run_at` = 槽位，12 个场景：follow/hourly/daily/weekly 在 15m、1h、24h、35m、90m、7d 与 cron 目标上） | 全部符合文档（0/4/8/12、0/7/14、每轮、no-op、pending 每轮归零、无规则目标可 settle）。**注意**：该 probe 固定了槽位，掩盖了 B1；它只证明 SQL 的 ceil 边界数学在固定网格下成立 |
| store 不变量 probe（ensure/新检查加入下一槽/update 从 now 重排/自定义节奏保留/暂停后无 next/重新启用按存储节奏/切回 interval 清 cron/坏 cron 行回退/归档后无 next/手动执行不触发 gate） | 10 项全部 OK |
| cron 语义 probe（`30 1 * * *` / `0 1` / `15 1` / `0 2` 在 America/New_York 跨秋季跳变；`30 2` / `0 2` / `0 3` 跨春季跳变；`0 0 1 * 1` 的 dom/dow OR；严格 after；`0 0 29 2 *` 从 2096 起） | **发现 B2**：`30 1 * * *` 于 2025-11-02 触发 **05:30Z 与 06:30Z 两次**（本地都是 01:30，EDT/EST 各一次）；春季跳变 02:30 正确跳到 3/10；dom/dow OR、严格 after 正确；`0 0 29 2 *` 从 2096-03-01 起 `Next` 返回 0 → 走 interval 回退（robfig 五年 yearLimit 边界，仅记录） |
| 缺 schedule 行的 `ListQualityTargets` probe（规则存在、schedule 行不存在） | **发现 S1**：`targets=0 err=failed to scan a quality target: sql: Scan error on column index 14, name "schedule_cron": converting NULL to string is unsupported` |
| 时区 probe（`schedule.Parse(0, "0 2 * * *", z, "")`） | **发现 N3**：`Local`、`EST`、`MST` 被接受；`Not/AZone` 被拒 |
| `go mod tidy -diff` | **发现 N1**：非空，`github.com/robfig/cron/v3` 应进直接依赖块、当前在 indirect 块 |

## 发现与关闭

全部发现均针对 `1d6fe34`；下表「状态」列已按最终 HEAD（`7be0aed`）的修复与复验更新，原始证据仍指向 review 快照。

| # | 严重度 | 发现（证据） | 建议修复 | 状态 |
| --- | --- | --- | --- | --- |
| B1 | **blocking** | **interval 目标的频率档少挡一轮，实际跑得比档位更密；默认（interval）形态下核心承诺不成立。** 真实推进语义下：15 分钟目标 + HOURLY 每 **45 分钟**、35 分钟目标 + HOURLY **每 35 分钟（=每轮，档位完全失效）**、每日目标 + WEEKLY 每 **6 天**、15 分钟目标 + DAILY 每 **≈23 小时 45 分**（probe 的跳过周期按 due 推进；真实 tick 为 10 s，95 个跳过周期最多再加 ~16 min，仍可能在同一自然日跑两次）；cron 目标因槽位固定在墙钟上，才是 1 h / 7 天。根因：`advanceSettledTarget` 用 `now + interval`（`now` 是执行完成时刻，见 `backend/runner/qualityrunner/schedule.go:304-310`、`:376-380`）推进，interval 槽位随 run 延迟漂移；`qualityFrequencyGateSQL` 用 `gate.started_at > s.next_run_at - (k−1)×cycle`（`backend/store/quality_frequency.go:60-75`），漂移后第 k−1 轮的边界恰好等于上一次 `started_at`，严格 `>` 放行。文档与实现承诺的「15 分钟目标 HOURLY 每 4 轮」「35 分钟目标 HOURLY 每 2 轮（70 分钟）」「每日 WEEKLY 每 7 轮」「at most about once an hour/a day/a week」因此不成立（`.agents/docs/data-quality.md:126` D-90、`:760` §7 #37、`docs/data-quality.md:100-120`）。测试为什么没抓到：`TestQualityFrequencyGate` 只钉 SQL 文本；集成场景用的是 cron 目标（固定槽）且只验证 k=96 的 DAILY 跨两轮，掩盖了 off-by-one。 | 最小修复：把 gate 的比较改为 `gate.started_at >= s.next_run_at - window`（漂移后第 k−1 轮边界与上次 `started_at` 相等时即挡住）。更稳的修法：按 run 的 **due slot**（而不是 `started_at`）与**完整 tier period** 比较（`scheduled_at` 方案；这样 interval/cron 两种槽位语义都成立，且能同时消掉对齐漂移）。补一个「interval 目标 + 真实完成时刻推进」的节奏测试（例如 15m/HOURLY 断言相邻两次 run 的开始间隔 ≥ 1 h）。 | **已修复**：`18b46be` 把 gate 改为按 run 的 due slot（`quality_execution.scheduled_at`，迁移 `0039`）与**完整 tier period** 比较，不再有 k−1 轮量化；`7be0aed` 用真实推进语义补 `TestDataQualityIntervalCadenceSlotsRealServerIntegration`（见 post-fix 段） |
| B2 | **blocking** | **DST 秋季重复的小时触发两次，与多处文档和测试意图相反。** probe：`30 1 * * *` 在 America/New_York 的 2025-11-02 依次产生 `2025-11-02T05:30:00Z`（01:30 EDT）与 `2025-11-02T06:30:00Z`（01:30 EST）两次；`0 1 * * *` 同理，`*/15 1 * * *` 会在两小时内跑 8 次。`Spec.Next` 直接委托 `robfig/cron/v3`，没有去重（`backend/common/schedule/schedule.go:133-147`）。文档多处承诺「秋季重复时刻只触发一次」：`.agents/docs/data-quality.md:125`（D-89）、`:684`（§6 失败模式）、`:760`（§7 #35）、用户文档 `docs/data-quality.md:280` 与 Troubleshooting `:791`。`TestNextCronAcrossDST`（`backend/common/schedule/schedule_test.go:130-136`）用的是 `0 2 * * *`，02:00 **不在**重复的 01:00–01:59 内，因此断言 pass 但检测不到重复。附带影响：在该跳变日保存 cron 时，`NominalInterval` 可能取到重复对之间的 1 小时并落库为 `schedule_interval_seconds`，影响回退 interval、gate 量化和 `row_count_change` 窗口。 | 二选一：(a) 代码去重——在 `Spec.Next`（或 `nextTargetRun`）对 DST 重复窗口去重（同一本地墙钟的日期+时分只触发一次），并补 `30 1 * * *` 与 `*/15 1 * * *` 两个用例；(b) 若接受两次，则把上述文档与测试改为「重复小时会触发两次」，并在 §7 #35 写明后果（当天的档位节奏也按两次计）。 | **已修复（采用 a）**：`7be0aed` 在 `Spec.Next`/`NominalInterval`/保存期校验共用 `nextOccurrence`，按 30m/1h/90m/2h 偏移识别重复墙钟并跳过第二发；`TestNextCronFallBackFiresOnce`、`TestNextCronRepeatedHourFiresOncePerWallClock` 钉住 |
| S1 | should-fix | **`ListQualityTargets` 在「规则存在、target schedule 行不存在」时整页失败（NULL→string 回归）。** probe 输出见上。代码把 `schedule_cron`/`schedule_timezone` 扫进 `string`（`backend/store/quality_target.go:312-313`），SQL 是 `LEFT JOIN quality_target_schedule`（`:462`、`:468`），缺行时两列为 NULL；旧代码用 `sql.NullInt32` 容忍缺行。代码本身承认这种行可能存在：`backend/store/quality_target_schedule.go:309`（"missing schedule row is not an error here"）、`backend/api/v1/data_quality_target.go:348`（"A target whose rules predate the target-level schedule has no row until it is edited"）；迁移 0030 也没有回填旧规则。一个手工/历史遗留行会让目标目录、报告装配、元数据信任信号等所有走 `ListQualityTargets` 的读路径返回 Internal。 | 扫描 `sql.NullString`（invalid → ""），或在 SQL 里 `COALESCE(sch.schedule_cron, '')`/`COALESCE(sch.schedule_timezone, '')`；补一条「规则有、schedule 行缺」的 store 测试。 | **已修复**：`7be0aed` 改为 `sql.NullString`；`TestDataQualityTargetWithoutScheduleRowRealServerIntegration` 钉住 |
| N1 | nit | `go mod tidy` 未跑干净：`github.com/robfig/cron/v3` 被 `backend/common/schedule` 直接 import，却仍在 `go.mod` 的 indirect 块（`go mod tidy -diff` 会把挪到直接依赖块）。违反 `backend/AGENTS.md` 的 Tidy 步骤；当前 CI 没有 tidy 检查，所以不会红。 | `go mod tidy` 并提交 `go.mod`/`go.sum`。 | **已修复**（`7be0aed`，`go mod tidy` 后无 diff） |
| N2 | nit | 注释与实现不一致：`backend/component/qualityreport/report.go:232` 写「ScheduleInterval is the target's fixed interval; it is zero for a cron target」，但 `backend/api/v1/data_quality_report.go:271` 对 cron 目标也无条件写入名义间隔；`backend/api/v1/data_quality_service.go:765` 仍写「schedules the target one interval out」，而 cron 目标排的是下一次发生。 | 更新这两处注释（cron 目标也会带名义 interval；cron 目标按下一次发生重排）。 | **已修复**（`7be0aed`） |
| N3 | nit | **时区校验比文档宽。** `schedule.Parse` 用 `time.LoadLocation`，因此 `Local`、`EST`、`MST` 等也被接受（probe 输出）；文档（`docs/data-quality.md` 与 D-89）承诺 "IANA time zone"。其中 `Local` 会让存储的调度依赖部署机时区，跨部署/迁移时语义漂移；`EST`/`MST` 是固定偏移名，虽稳定但不是 IANA 名。 | 除 `UTC` 外要求 `Area/Location` 形式（含 `/`），或明确列出接受集合并同步文档。 | **已修复**：`ValidateTimeZoneName` 只接受 `UTC` 或含 `/` 的 IANA 名，`dataquality.ValidateTimeZone`（检查时区）共用同一规则（`7be0aed`） |
| N4 | nit | **cron 输入没有长度上限。** `Parse` 只检查字段数（`strings.Fields(...) != 5`），没有长度约束；超长字段的解析错误会被 `fmt.Errorf("cron %q is not a valid expression: %v", ...)` 与 `connectErrorInvalid("%v", err)` 原样回显，最大可放大约一个请求体（gRPC/Connect 默认消息上限内）。 | 加一个字节数上限（如 256 B）并在错误里不回显原文；或至少不回显超长字段。 | **已修复**：>256 字节直接拒绝且不回显原文（`7be0aed`，`TestCronLengthIsBounded`） |
| N5 | nit | **DQ 专用集成门禁不包含本特性的新场景。** `make test-integration-data-quality` 只 `-run '^TestDataQualityJourney…$'`（`Makefile:97-98`）；新场景 `TestDataQualityScheduleTiersRealServerIntegration` 不含 "MySQL" 也不匹配该正则。CI 的 `make test-integration`（`.github/workflows/mysql-integration.yml:76`）会跑全包，所以 CI 覆盖到了，但按文档只跑 DQ 门禁的开发者不会跑到它。 | 把新场景加入该 make 目标（或改名并入 journey 的 regex），并在 `backend/test/integration/README.md` 说明。 | **已修复**：`test-integration-data-quality` 现覆盖 journey + tiers + interval-cadence + missing-schedule-row 四个场景，README 同步（`7be0aed`） |
| N6 | nit | **lint 门禁的默认 `max-same-issues=3` 掩盖了新引入的告警。** 放开限制后有 176 条 `multiline-if-init`，其中本 diff 新增 4 条（`backend/store/quality_target_schedule.go:182/198/342/620`，blame 5a76af5）；默认命令只显示别处的 3 条旧告警，看不出新增。仓库同规则已有 172 条，属既有实践，不阻断。 | 与仓库现状保持一致即可；若要求新代码零新增告警，把这 4 处多行 `if` 初始化改成先赋值再判断。 | **已修复**：本 diff 新增的 4 处 multiline-if-init 改为先赋值再判断；其余同规则告警均为 base 既有（`7be0aed`） |

## post-fix 验证

复验对象为最终 HEAD（`7be0aed`，含 `18b46be`、`6f136e9` 与 `7be0aed` 三组修复）。B1/B2/S1 的回归场景与全部门禁在 Docker 真 PostgreSQL + MySQL 上重跑；Lead 另在最终 HEAD 上重跑了全量门禁（下表）。

| 检查 | 结果 |
| --- | --- |
| **B1 真实推进节奏**（`TestDataQualityIntervalCadenceSlotsRealServerIntegration`，PASS 0.96s；模拟 runner：slot = `next_run_at`，`started_at` = slot+2s，advance = 完成时刻 + interval） | 15m interval + HOURLY：默认档每轮跑，HOURLY 只在第 1/5/9 轮（第 2/3/4 轮被 2s 漂移挡住），相邻 HOURLY slot 间隔 ≥ 1h；35m interval + HOURLY 在第 1/3/5/7 轮（每 2 轮，≥70m）；6h + DAILY 的 8 次间隔均 ≥ 24h、+ WEEKLY 的 2 次间隔 7d56s；每轮 `CountPendingQualityTargetRules == 0`（被跳过的检查不挂住周期） |
| **B1 cron 目标不变**（`TestDataQualityScheduleTiersRealServerIntegration`，PASS 19.7s） | 第一轮强制周期跑默认 + DAILY 档，第二轮只跑默认档，weekly 档仍 1 条执行，目标 settle 到未来 |
| **B2 DST 去重**（`TestNextCronFallBackFiresOnce` / `TestNextCronRepeatedHourFiresOncePerWallClock` / `TestNextCronAcrossDST`） | `30 1 * * *` 在 America/New_York 2025-11-02 只在 05:30Z 触发一次，下一次跳到 11-03；`*/15 1 * * *` 在重复小时每面墙钟只触发一次；春季跳变日 02:xx 仍跳过；`NominalInterval` 仍是约一天 |
| **S1 缺 schedule 行**（`TestDataQualityTargetWithoutScheduleRowRealServerIntegration`，PASS 0.54s） | 删掉 schedule 行后 `ListQualityTargets` 仍列出目标（schedule 为空、无错误），`GetQualityTarget` 正常 |
| **N1–N6** | `go mod tidy -diff` 空；两处注释与实现一致；`Local/EST/MST` 被拒、`UTC`/`Area/Location` 接受；>256B cron 拒绝且不回显；DQ 门禁含四个场景且 README 说明；本 diff 无新增 lint 告警 |
| **全量门禁（`7be0aed`，独立 verifier 复跑）** | `gofmt -l backend/` 空；`go build ./...` exit 0；`go test -count=1 ./...` 全绿；`go test -tags=integration ./backend/migrator/...` PASS（31.2s，`TestMigrateSchemaLATESTMatchesTheIncrementChain` 7.15s，schema 0.1.39）；`make test-integration-data-quality` PASS（四个场景，57.7s）；`buf format -d proto`/`buf lint proto` exit 0 且 `/tmp` 副本 `buf generate` 三处产物逐字节一致；前端 biome（550 files）/lint/i18n（118 terms、97 rules）/type-check/test run（171 files / 1812 tests）全绿 |
| **verifier 复验** | 独立 verifier 逐条核对 B1/B2/S1/N1–N6，全部确认关闭（证据见上表各行）；另报两个低 severity 残留并已关闭：(a) 2 小时 fold（Antarctica/Troll）在 4 次跳过后仍会漏一次重复墙钟 → `f9f3fc5` 改为逐个跳过并以 128 为界（覆盖 2h fold × 1m 粒度），新增 Troll 回归用例（同一墙钟不双发、fold 后从 03:00 恢复）；(b) 升级前 `scheduled_at` 为 NULL 的旧执行不参与 gate（升级后首个周期可能多跑一次、自愈）→ `d7a9205` 在 §4.6 写明。`f9f3fc5`/`d7a9205` 之后重跑 `go test ./backend/common/schedule/ ./backend/store/ ./backend/runner/qualityrunner/` 与 DQ 集成场景（PASS） |

## 残留与已知边界

- **B1/B2/S1 已按最终 HEAD 复验**：`18b46be` 的 slot 方案由真实推进语义的回归场景与独立 verifier 共同确认，不再有 k−1 轮量化；DST 去重现在覆盖 30m/1h/2h 及历史 90m fold（`f9f3fc5` 的 Troll 用例保证 2 小时 fold 不双发且不跳过 fold 后的首个合法发生）。slot 比较是绝对时刻：DST 短日只会让某个档位多等一个 slot（不系统漂移、不双跑）；改期不重置档位（沿用上一次 run 的 slot，属刻意的 per-check period 语义）；升级前 `scheduled_at` 为 NULL 的旧执行不阻挡（一次性、良性的档位重置，已写入 §4.6）。B1 的「blocking/未修复」只对 `1d6fe34` 成立。
- **B1 的节奏结论**（针对 `1d6fe34` 的 k−1 轮实现）来自 store 层调用 + 精确模拟 runner 的推进语义（interval: 完成时刻 + interval；cron: `Spec.Next(now)`）；修复后的 slot 版本已由 post-fix 的真实推进回归场景取代，不再依赖该模拟。
- `make test-integration`（全包 + `-race`）与前端 `audit:ui` 未在本次运行；前端 DST/时区展示没有浏览器级 e2e。
- 极端边界仅记录未上报：`0 0 29 2 *` 在 2096 年之后会因 robfig 的五年 `yearLimit` 被判 never fires（今天不会触发；届时的存储行会走 interval 回退）。
- `frequency_tier` 不进 revision/契约 fingerprint 已核对（`qualityRuleSemanticChanged` 不看它，`qualityContractFingerprint` 只哈希 settings+checks），与 D-90/§4.6 一致；维护窗口「检查照跑、站内信照写、仅出站投递推迟」与 notifications.md O-15/O-16 及 dispatcher 代码一致。
