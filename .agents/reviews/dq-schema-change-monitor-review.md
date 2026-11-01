# P1-2（全引擎自动 schema 变更监控）— 对抗性 review

Reviewer：一次独立对抗性 review 在全新上下文中、只读执行，对象为分支 `feat-dq-schema-change-monitor`
（feature head `f5db15d`，base `7049c0d`，`.agents/worktrees/feat-dq-schema-change-monitor`）。完整读了
`git diff 7049c0d..HEAD`（56 个文件；其中 `backend/generated-go/`、`frontend/src/types/proto-es/`、
`proto/gen/` 为 buf 产物，只做幂等性核对），并读了
[`data-quality-competitive-gap-analysis.md`](data-quality-competitive-gap-analysis.md) 的 P1-2 行与 §6.2、
新增的 [`.agents/docs/schema-fingerprint.md`](../docs/schema-fingerprint.md)，以及
[`data-quality.md`](../docs/data-quality.md)、[`schemasync.md`](../docs/schemasync.md)、
[`object-ddl-storage.md`](../docs/object-ddl-storage.md)、[`notifications.md`](../docs/notifications.md)、
[`data-quality-target-centric.md`](../docs/data-quality-target-centric.md)、`docs/data-quality.md`、
`docs/notifications.md`、i18n glossary 与各层 `AGENTS.md`。

四项已定范围不重新讨论：① 结构变更通知按 owner 分发（无 owner → admins，未监控目标不发）；② 新规则统一用
引擎无关指纹、旧 DDL hash 快照走兼容读；③ 目标级"确认 Schema 变更"动作；④ 指纹 = 有序列（名/类型/可空/顺序）
+ 主键 + 视图/物化视图 definition，排除注释/默认值/字符集/非主键索引/统计。代码或文档与这些决定矛盾时按缺陷上报。

## 实际验证过的内容

| 命令 | 结果 |
| --- | --- |
| `git diff 7049c0d..HEAD` 全量阅读；生成物抽查 | 完成 |
| `gofmt -l backend/` | 干净 |
| `go build ./...`、`go test ./backend/... -count=1` | 通过 |
| `make test-integration-data-quality` | **PASS**（旅程 13.2 s，迁移到 `0.1.37`）：MySQL 上 `target_schema_changed` 为真、收到一条 `TARGET_SCHEMA_CHANGED`、确认后清信号、再次确认返回 0 |
| `make test-integration-smoke` | **PASS**（runner 102.4 s，migrator 23.6 s，含 `LATEST.sql` 与增量链一致性） |
| `golangci-lint run … --max-same-issues 0`（与 base `7049c0d` 干净 checkout 对比） | 两侧均 173 条既有 `revive multiline-if-init`；归一化后**完全相同**，本分支未新增 |
| `buf format -d` / `buf lint` / `buf generate` + `git status` | 干净 / 干净 / 幂等 |
| 前端 `biome check`、`lint:ci`、`i18n`（112 terms / 95 rules）、`type-check`、`test run` | 全绿；169 files / 1760 tests |
| 外部 probe（`go test -overlay`，测试写在 `/tmp`） | ① 重复 `(GUID, object_type)` 快照 → 同一 key 产生 **2 条写集**；② 外发渲染新 kind → 标题 `Data quality: inst/db/orders`、正文为空、链接指到 `/data-quality/executions`；③ 旧二进制读新 JSONB：`DiscardUnknown` 把未知枚举名降为 `UNSPECIFIED`（安全） |
| 未运行 | 单独的 `make test-integration-mysql`（smoke 已覆盖同一包，含 DQ 旅程）；StarRocks/Doris 容器（本机无镜像配置）；目录 SQL 谓词的真实 PostgreSQL 专项 probe（由检查 + Go 真值表覆盖，post-fix 由旅程补上） |

Review 结束时工作区 `git status --porcelain` 为空，未改任何文件、未提交。

## 发现与关闭

全部发现已在 `bfbc175` 关闭；post-fix 验证见下一节。

| # | 严重度 | 发现（证据） | 修复 | 状态 |
| --- | --- | --- | --- | --- |
| 1 | major | **StarRocks/Doris 上指纹看不到可空性与主键**：驱动解析了 `IS_NULLABLE` 却只用于默认值推导，`ColumnMetadata.Nullable` 从未赋值（`backend/plugin/db/starrocks/sync.go`）；`TableMetadata.Indexes` 恒空（同文件 `// TODO: add index information`）。因此这两个引擎上的指纹只有 name/type/order，而 scope ④ 与文档承诺"列可空/主键"全引擎生效；对这两引擎的新规则是相对旧 DDL hash 的回归（`ALTER … NULL/NOT NULL` 不再点亮、不发通知） | 把列映射抽成可测的 `newColumnMetadata` 并真正写入 `Nullable`（可空性在 catalog 已有，只是没落库）；主键信息该引擎 catalog 确实不提供，不臆造：按引擎边界写进文档并登记为开放工程债 `backend-open-items.md`「StarRocks/Doris 无索引元数据」，旅程/单测覆盖可空性映射 | 已修 `bfbc175` |
| 2 | major | **快照里重复的 `(GUID, object_type)` 会让整个库同步失败**：`prepareSchemaFingerprints` 直接用 `bmc.guidList`，未去重（`backend/runner/schemasync/schema_fingerprint.go`）；指纹 upsert 与 metadata upsert 一样是 `INSERT … ON CONFLICT DO UPDATE`，重复源行会报 "cannot affect row a second time"。而 `batchMetaCreate.diff` 明确按 first-wins 去重并说明过这个约束 | `collectSchemaFingerprints` 按 first-wins 去重（与 `batchMetaCreate.diff` 同规则），并在形状检查前标记 key，保证后一条重复不会描述一条与已存 metadata 不一致的形状；单测断言重复快照只产生 1 条写集且取第一条的指纹 | 已修 `bfbc175` |
| 3 | major | **新事件经外发渠道投递时正文为空、链接错误**：`enqueueOutbound` 会投递该事件，但 `outbound/render.go` 的 `renderDataQuality` 没有 `TARGET_SCHEMA_CHANGED` 分支，落到默认标题 `Data quality: <target>`、正文空；`link()` 也落到 `/data-quality/executions`。用户收到的邮件/IM 无法判断发生了什么 | 增加中英分支（标题 + 目标 + "复核并确认"说明），并让 `link()` 按 `detail.target_guid` 生成目标工作台链接（与前端一致的 `;`→路径分段、空段用 `~` 编码）；新增渲染与链接单测 | 已修 `bfbc175` |
| 4 | minor | **`SendToWorkspaceAdmins` 先解析管理员再 claim 去重键**：被抑制的重复也会付 IAM policy 读 + 组成员展开，且与 `claim` 注释声明的抑制目的矛盾 | 两个发送入口都先 claim 再解析受众：`SendToWorkspaceAdmins` claim → 解析 admins → `deliverToRecipients`（不碰 gate）失败时 release；`SendToRecipients` 同样 claim → `deliverToRecipients`；新增测试断言两次相同发送只读一次 IAM policy | 已修 `bfbc175` |
| 5 | minor | **多处文档/注释仍把信号描述成 schema/meta hash，并有过时断言**：`LATEST.sql` 的 `target_meta_hash` 注释、`store/quality_target.go` 的 `SchemaChanged` 注释、`.agents/docs/data-quality.md` §3.1/§3.10/§6、`object-ddl-storage.md` 说 DDL hash "纯展示"（实际兼容分支会读）、`schema-fingerprint.md` 把"检查 CSV 导出"列为消费者（实际是目标导出的 `schema_changed` 列）、说"只有编辑规则会补 NULL 快照"（确认操作也会补）、把"分区/属性变化不再点亮"写成无条件（对 legacy 快照不成立）；`docs/data-quality.md` 说"保存任意编辑都会重建该检查基线"（实际只有定义变更会） | 逐条改成结构指纹口径、补上 legacy/确认/NULL 快照的边界、修正消费者列表与编辑口径；operator 文档改成"只有改定义（类型/目标/filter/阈值/参数）才重建该检查，改标题/严重级别/owner 不重建" | 已修 `bfbc175` |
| 6 | nit | **术语没有统一**：新文案用 "structure/结构"（通知、按钮），既有徽标用 "Target schema changed/目标 Schema 已变更"；glossary 未登记新术语 | 用户可见文案统一为既有徽标的词（en "Target schema changed" / "Acknowledge schema change"，zh "目标 Schema 已变更" / "确认 Schema 变更"）；不新增 glossary 行——这里没有新专业术语，`schema`（zh 保留 `Schema`）已覆盖，`fingerprint` 是内部概念、不进 UI；维护文档里的"结构指纹"是内部机制名 | 已修 `bfbc175` |

## Post-fix 验证（实现方在 `bfbc175` 上重跑）

- **#1：** `backend/plugin/db/starrocks/sync_test.go` 新增 `TestNewColumnMetadataRecordsNullability`（YES/NO/未定义三态）；`Nullable` 现在进入存储元数据。主键缺口不是靠文档掩盖：`schema-fingerprint.md` 新增「引擎元数据边界」，`backend-open-items.md` 记录债务与补法；旅程仍覆盖 MySQL 的完整列+主键口径。
- **#2：** `TestCollectSchemaFingerprintsDeduplicatesCollidingResources` 断言重复快照 1 条写、取第一条指纹；`go test ./backend/runner/schemasync/` 通过。
- **#3：** `outbound/render_test.go` 新增 `TestRenderDataQualityTargetSchemaChanged`（en/zh 标题、正文、`/data-quality/targets/inst-1/app/~/orders` 链接）与 `TestQualityTargetLink`（含空段与空 GUID）。
- **#4：** `service_test.go` 新增 `TestSendToWorkspaceAdminsClaimsBeforeResolvingTheAudience`：两次相同发送只解析一次 IAM policy、只写一条站内信。
- **#5：** 按发现逐条改文并 grep 复核：`target_meta_hash`/`schema_changed` 的说明只剩"结构指纹"或 DDL 兼容分支语境；消费者列表与三个导出 RPC 一致；operator 文档的编辑口径与 `DefinitionChanged`（dimension/rule_type/target/config）一致。
- **#6：** `pnpm i18n` 通过（112 terms / 95 rules），两种语言同词、无 placeholder 漂移；前端 `notificationText`/目标页测试跟随文案键更新。
- **额外补强：** 旅程新增"目录 SQL 谓词"断言——DDL 变更后 `ListQualityTargets` 的 `schema_changed` 与 `GetQualityRule` 的 `target_schema_changed` 一致为真，确认后一致为假，把"两个谓词必须同改"的不变量钉在真实 PostgreSQL+MySQL 上。
- **门禁（post-fix 全量重跑）：**`gofmt -l` 干净；`go build ./...`；`go test ./backend/... -count=1` 通过；`golangci-lint` 相对 base 零新增（两侧 173 条既有 `multiline-if-init`）；`buf format/lint/generate` 干净且幂等；前端 `biome check`（无改动）、`lint:ci`、`i18n`、`type-check`、`test run` 全绿（169 files / 1760 tests）；`make test-integration-data-quality` 通过（33.3 s）；`make test-integration-smoke` 通过（runner 102.1 s，migrator 22.5 s）；`make test-integration`（`-race`，含全部 runner 场景与迁移链）通过（runner 100.9 s，migrator 23.1 s）。

## 复核确认正确的部分

- **指纹口径与稳定性**：canonical 编码复用 `meta_hash.go`，不走 protojson；列顺序由驱动的 `ORDINAL_POSITION`/`attnum`/`column_id` 排序保证，主键表达式由 `array_agg(... ORDER BY k)` 保序，因此同一结构不会跨同步抖动；注释/默认值/字符集/非主键索引/统计不参与，同一结构在两引擎上得到同一指纹。
- **事务与生命周期**：指纹行只在 metadata 行存在时存在，顺序 metadata → DDL → 指纹，同一事务；删除只经 `BatchDeleteMetaRegistryAt` 级联；回填/新建/删除/回滚都不产生通知。
- **legacy 兼容与两个谓词**：Go `qualitySchemaChangedSince` 与 SQL `qualityTargetSchemaChangedSQL` 真值表逐项一致（snapshot/fingerprint/legacy 空值与相等组合），兼容分支只放宽"未变"，不放宽"已变"；升级前 StarRocks/Doris 规则不会一次性误报。
- **通知语义**：只在"旧行存在且值不同"的迁移上触发；收件人 = 目标非归档检查 owner 去重、无 owner → admins、无检查 → 不发；1h per-target 去重；多收件人一次 claim、每人一行、外发一条（audience）；失败只记日志。
- **确认语义**：只写 `target_meta_hash`/`updated_at`，不写 revision、不 bump revision、不动 `updated_by`；幂等（第二次返回 0）；无指纹 `FailedPrecondition`；权限复用 `rules.update`、写审计；前端就地清目标徽标与逐检查徽标。
- **兼容性**：store/v1 新枚举值 5 数值兼容；旧二进制 `DiscardUnknown` 读新 JSONB 安全（probe）；CSV/报告/MCP 等既有消费者自动获得新口径；`buf generate` 生成物随提交且幂等。

## 残余风险（已记录，不阻塞）

- **确认的 TOCTOU**：读当前指纹与更新规则不在一个事务里；两步之间若 sync 提交了更新的指纹，规则会被基线到旧形状，界面报成功而目录仍亮徽标，需要再点一次确认。
- **主键口径的粒度**：只编码主键 key 表达式，不编码 prefix length/descending/opclass/生成列表达式；主键前缀长度变化这类"确实是不同主键"的场景会碰撞（scope ④ 的刻意最小化）。
- **StarRocks/Doris 无索引元数据**：新规则看不到 primary-key 变化（见 `backend-open-items.md`）；`schema_drift` 也不比主键。
- **确认会连带补齐 NULL 快照**：升级间隙内新建、快照为 NULL 的规则，其徽标不会自己出现，但对该目标的确认会一并补上；这条已写进文档。
- **1h 去重窗口**：确认后同一小时内的第二次真实结构变化会被抑制成一条（与 issue 通知一致）。
- **无 FK**：指纹表没有指向 metadata 行的外键，"严格子集"是事务级不变量而非 schema 约束；并发删除实例/库时理论上可留孤儿行（DDL 表同样）。
- **成本**：每次 sync 对全库对象做一次 hash 读 + 全量指纹计算（含视图定义）；与已有 digest 读同量级、随对象数增长，未做 DDL 那样的轮换/周期全量拆分（记录在 `schemasync.md` 历史债务）。
- **legacy StarRocks 规则的属性敏感度**：升级前保存的规则仍按 DDL hash 比较，分区/属性变化仍会点亮，直到该规则被编辑或目标被确认；之后换成指纹口径。
- **深链编码**：`qualityTargetHref` 复用 `guidToRouteParams`，仍继承"字面量为 `~` 的分段会被解码为空段"的既有歧义。
- **归档规则**：直接 `GetQualityRule` 归档规则仍会计算 `target_schema_changed`，而目录与确认排除归档规则，因此该徽标无法在目标级清除（不影响执行）。
- **未覆盖**：单目标 HTML 报告与周报 digest 不展示结构变更信号（未承诺）；没有逐列 diff 与指纹历史；结构变更通知不检查规则级静音（结构变更不是 issue，按决定不套用 issue 静音）。
- **通知文案的"下次执行前"**：目标只剩暂停检查时没有"下次执行"，文案略超前（不影响可操作性）。

## 结论

`fix-then-ship`，post-fix 可发。三个 major 都在 `bfbc175` 关闭：StarRocks/Doris 的 nullability 真正落库（主键缺口按引擎边界文档化 + 开放债）、重复快照不再会拖垮整库同步、外发渠道能读懂并正确深链结构变更事件；两个 minor（去重 claim 顺序、过时文档/注释）与一个术语 nit 同样关闭。指纹写路径的事务/生命周期、legacy 兼容的两处谓词、通知与确认语义都符合四项范围决定，全部门禁与真实 PostgreSQL+MySQL 集成（含 `-race` 全量与 DQ 旅程）通过。
