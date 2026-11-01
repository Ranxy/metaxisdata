# Schema Fingerprint（目标结构指纹）— Reference

> Status：**implemented**（P1-2）。范围：`backend/store/{schema_fingerprint,meta_schema_fingerprint,quality_schema_change}.go`、`backend/runner/schemasync/schema_fingerprint.go`、`data_quality_*` handlers。
> Related：[schemasync.md](schemasync.md)（写入事务与删除不变量）、[object-ddl-storage.md](object-ddl-storage.md)（DDL 存储，与本表职责不同）、[data-quality.md](data-quality.md)（`target_schema_changed`、结构变更通知与确认操作）、[notifications.md](notifications.md)（消息类型与去重）。

## 它是什么

每个表、外部表、视图、物化视图一份**与引擎无关的结构指纹**：有序列（列名、类型、可空、顺序）+ 主键表达式；视图与物化视图另含它们的 `definition`。指纹在 schema sync 时由**已同步的元数据**推导，因此 MySQL / MariaDB / TiDB / OceanBase / PostgreSQL / SQL Server 都有，而不只是抓得到 DDL 的 StarRocks/Doris。

它服务的判断只有一句：**"检查保存时依赖的结构，现在还是不是那个结构"**。消费方：

- 目标目录与工作台徽标（`QualityTargetSummary.schema_changed` / `QualityRule.target_schema_changed`）；
- 目标目录的 CSV 导出（`schema_changed` 列）与 MCP `get_quality_signal`（同一字段，自动生效）；
- 结构变更通知（`TARGET_SCHEMA_CHANGED`）；
- `AcknowledgeQualityTargetSchema`，即"确认 Schema 变更"后重建基线。

`schema_drift` 规则与它**不是一回事**：drift 规则把"保存时写进 config 的列快照"和当前列比对，是用户逐条声明的检查；指纹是自动维护的信号。drift 规则不会被确认操作改写。

## 指纹口径

纯函数：`store.CalcSchemaFingerprint(*storepb.StoredMetadata) ([]byte, bool)`（`backend/store/schema_fingerprint.go`）。它把元数据裁剪成只含下列字段的 message，再用**同一套 canonical 编码**（`meta_hash.go` 的 `encodeCanonicalMessage`，不走 protojson）编码后取 SHA-256；domain 常量 `metaxisdata.schema.fingerprint.v1\x00` 是口径版本。

| 参与指纹 | 不参与指纹 |
| --- | --- |
| 列名、类型、可空性、列的先后顺序（repeated 字段顺序） | 注释、默认值、字符集、排序规则、`position` 之外的展示属性 |
| 主键（`IndexMetadata.primary`）的 key 表达式及顺序 | 非主键索引、唯一性、索引名/类型/可见性/注释 |
| 视图、物化视图的 `definition`（视图的 SQL 正文就是它的结构） | 视图依赖列、触发器、规则、`skip_dump` |
| — | 表统计（`row_count`/`data_size`/`index_size`/`data_free`）与对象名（GUID 已是身份） |

`ok=false` 只对没有形状的对象类型（COLUMN/SCHEMA/DATABASE/SEQUENCE/FUNCTION/PROCEDURE/UNSPECIFIED）返回；这些对象不落行、不参与比较。

两条必须知道的后果：

1. **口径版本变更 = 全量重写**。改这个函数（增加字段、覆盖新类型）必须同时 bump domain 常量，并在同一变更里说明：所有已存指纹都会变化一次，所有已保存检查会在下一次 sync 后显示"结构已变更"，需要用户确认。
2. **StarRocks/Doris 的语义收窄**（P1-2 的刻意取舍）：这些引擎上的**指纹快照**只看列/主键/视图定义，因此只改分区或 table properties 不再点亮徽标；**升级前保存**的 StarRocks/Doris 规则快照仍是 DDL hash，legacy 分支会继续按 DDL 比较，直到该规则被编辑或目标被确认（此后换成指纹口径）。DDL 仍照常抓取并展示（[object-ddl-storage.md](object-ddl-storage.md)）；需要"属性也敏感"的场景用 `schema_drift` 或 DDL 浏览。另见下文的引擎元数据边界。

## 存储

表 `meta_registry_schema_fingerprint`（迁移 `0037##meta_registry_schema_fingerprint.sql`，镜像在 `LATEST.sql`）：

| 列 | 作用 |
| --- | --- |
| `guid`, `object_type` | `PRIMARY KEY (guid, object_type)`，与 `meta_registry_resource` 同 key；无 FK。 |
| `fingerprint` | `CalcSchemaFingerprint` 的 32 字节结果。 |
| `updated_at` | 行时间戳；upsert 的 `WHERE fingerprint IS DISTINCT FROM` 守卫保证形状未变时不重写、不产生死元组。 |

为什么独立表：DDL 表只在抓得到 DDL 的引擎上有行且是 **display-only**，`meta_registry_resource` 是每次元数据列表都读的热表。指纹行是派生数据，读路径按主键批量取 32 字节。

## 写入（schema sync）

`SyncDatabaseSchema`（`backend/runner/schemasync/database_sync.go`）里：

1. 事务前：`prepareSchemaFingerprints(ctx, store, bmc.guidList)` 从**本次快照**计算四类对象的指纹（纯计算），并用一次 `ListMetaRegistrySchemaFingerprints` 读已存值做 Go 侧 diff。只有"值变化"和"首次写入"进入写集；写集与 `changed` 分开——`changed` 只含**之前有行且值不同**的对象。
2. 事务内：顺序是 metadata 行 → DDL 行 → 指纹行。指纹行严格是 metadata 行的子集。
3. 删除：`BatchDeleteMetaRegistryAt` 是删当前 metadata 行的唯一入口，它在同一事务里级联删 DDL 行与指纹行，因此"表被同步删除"不会留下孤儿指纹。
4. 提交后：`reportSchemaFingerprintChanges` 才发结构变更通知（回滚的发不出去）。

读成本：每个 database 每次 sync 多一次按主键的 hash 读（行数与已有 digest 读同量级），写只发生在变化或首次回填。回填不产生通知。

## 读取（data-quality）

| 位置 | 逻辑 |
| --- | --- |
| 规则保存/编辑 | `DataQualityService.targetSchemaHash` 读 `ListMetaRegistrySchemaFingerprints`，把当前指纹写进 `quality_rule.target_meta_hash`（也是 revision 快照的一部分）。目标还没指纹（升级后该库尚未同步）时写 NULL：不判定变化，而不是假装变了。 |
| 目标目录聚合 SQL | `store.qualityTargetSchemaChangedSQL()`：`l.target_meta_hash <> f.fingerprint`，且若 `meta_registry_resource_schema.schema_hash` 存在还要求与其也不同。 |
| 规则/工作台读 | `DataQualityService.qualitySchemaChangedSince(snapshot, signal)`：与上面同义的 Go 谓词。 |
| 读取信号 | `ListMetaRegistrySchemaSignals(keys)`：一条 LEFT JOIN 查询同时返回指纹与 legacy DDL hash，请求侧驱动 join，缺行可区分。 |

**legacy 兼容分支**：升级前保存的 StarRocks/Doris 规则，其快照是 DDL hash；迁移里无法把它换算成指纹。因此"快照 == 当前 DDL hash"也算未变，避免升级后所有存量规则一次性显示变了。该分支只放宽"未变"，不放宽"已变"；两个谓词（SQL 与 Go）是同一个判断，必须一起改。

## 结构变更通知

- 新增 DQ 通知 kind `DATA_QUALITY_NOTIFICATION_KIND_TARGET_SCHEMA_CHANGED = 5`（store 与 v1 枚举值兼容，`buf generate` 产物随提交）。
- 触发条件只有一个：本次 sync 中**该对象旧指纹存在且新值不同**。新建对象、首次回填、被删除对象、没有非归档检查的对象都不发。
- 收件人：该目标非归档（未归档、未删除）检查的 owner 去重；有检查但全部无 owner → 工作区管理员；没有检查 → 不发（见 `store.ListQualitySchemaChangeTargets`）。
- 去重：`notification.DataQualityDedupeKey(kind, targetGUID, now)`，窗口 1h，进程内 gate + 数据库 `(recipient_id, dedupe_key)` 唯一索引双重抑制；同一事件对多个 owner 只 claim 一次（`notification.Service.SendToRecipients`），外发渠道按 audience 只投一条。
- 通知 detail 只有 target（`target_guid`、`target_path`）与 kind，没有 rule/execution：一次结构变更属于整个目标。前端文案 `notifications.dataQualityTargetSchemaChangedTitle/Message`，链接到目标工作台。
- 投递是 best effort：通知写失败只记日志，不影响 sync 结果。

## 确认 Schema 变更（`AcknowledgeQualityTargetSchema`）

- RPC：`POST /v1/{parent=workspaces/*}/qualityTargets:acknowledgeSchemaChange`；请求 `parent` + `target_guid` + 可选 `meta_type`（目标有规则时服务端自行推导）；响应 `baselined_rule_count`。
- 语义：把该目标**非归档、未删除**的每条检查的 `target_meta_hash` 更新为当前指纹（`store.BaselineQualityRulesToSchemaHash`），清掉 `schema_changed`。只写 `target_meta_hash` 与 `updated_at`，**不写 `quality_rule_revision`、不 bump revision**：revision 记录的是检查定义，re-baseline 改变的是"这条定义是对着哪个形状复核过的"，不是定义本身。这是"revision 不可变"的**显式例外**，见 [data-quality.md](data-quality.md) 的不变量。
- 幂等：第二次调用返回 0。目标没有指纹（已从 registry 消失或该库尚未同步）→ `FailedPrecondition`；权限 `dataQuality.rules.update`；写审计；`updated_by` 不动（"谁确认的"由审计账本记录，不冒充规则编辑人）。
- 前端入口：目标工作台页头"目标 Schema 已变更"徽标旁的"确认 Schema 变更"按钮（有 `rules.update` 时显示），成功后就地清徽标并提示条数。

## 引擎元数据边界

- **StarRocks/Doris 没有索引/主键元数据**：该驱动的 `information_schema` 不提供索引信息（`backend/plugin/db/starrocks/sync.go` 的 `// TODO: add index information`），所以这两个引擎上的指纹是「列名/类型/可空/顺序」而不是完整的主键口径。主键变化在这两个引擎上由 `schema_drift` 规则（列快照比对）或升级前的 DDL-hash 规则（DDL 里包含 PRIMARY KEY）覆盖；新规则的主键变化不会点亮徽标。记录在 [backend-open-items.md](backend-open-items.md)。
- 其他引擎（MySQL/MariaDB/TiDB/OceanBase 走 MySQL 驱动、PostgreSQL、SQL Server）的元数据带 `Nullable` 与 `IndexMetadata.primary`，指纹是完整的列 + 主键口径。StarRocks/Doris 的 `Nullable` 在本功能里一并接通（驱动此前只把它用于默认值推导）。
- 视图/物化视图的 `definition` 在所有引擎上都有；列顺序由同步查询的 `ORDINAL_POSITION`/`attnum`/`column_id` 排序保证，因此指纹不会因遍历顺序抖动。

## 不变量

1. 指纹行只在 `meta_registry_resource` 行存在时存在，且与其创建/更新/删除在**同一事务**；删除只经 `BatchDeleteMetaRegistryAt`。
2. 指纹是派生只读数据：不写 history、不改 `meta_hash`、不触发血缘/校验、不影响同步的收敛判定。
3. 目录 SQL 谓词与 API Go 谓词是同一个判断；legacy DDL hash 兼容分支只能在两个位置一起改。
4. 通知只在提交后、只在"旧值→新值"的迁移上发；回滚、回填、删除不发。
5. `target_meta_hash` 有两个写者：规则保存（随 revision 一起写）与确认操作（不写 revision）。除此之外没有第三个。

## 失败模式

| 场景 | 行为 |
| --- | --- |
| 读已存指纹失败 | 该 database 的 sync 失败并重试（与读 digest 同级）；不会写一半 |
| 对象的类型声明有形状但元数据没有 | 跳过该对象并 `Warn`，不失败整库同步 |
| 升级后该库还没同步过 | 没有指纹行：`schema_changed` 不判定（不是 false 到 true 的误报），确认操作返回 `FailedPrecondition`；一次同步后目标有指纹。在这个间隙里**新建**的规则快照为 NULL，目标有指纹后不会自动补：编辑一次该规则（改定义）或对目标做一次确认（确认会连带补齐该目标全部非归档规则）才重写快照 |
| 升级前保存的 StarRocks/Doris 规则 | 快照是 DDL hash，legacy 分支使其保持"未变"；规则一旦被编辑就换成指纹 |
| 结构改了又改回（两次 sync 之间） | 指纹相同，没有事件；信号表达的是"当前形状与保存时是否一致"，不是变更日志 |
| 规则归档后结构变化 | 归档检查不参与通知与确认（与目录聚合一致） |
| 通知写失败 | 只记日志；下次真正的结构变化仍会通知（去重按事件窗口） |
| 单副本 / 同步间隔 | 发现延迟 ≤ 该数据库的 `sync_interval`（默认 15 分钟）；离线改动在恢复后的第一次同步被合并成一个事件 |

## Where things live

- 纯函数与口径：`backend/store/schema_fingerprint.go`；测试 `backend/store/schema_fingerprint_test.go`（稳定性、逐字段灵敏度、排除项、视图定义、无形状类型）。
- 表与查询：迁移 `backend/migrator/migration/0.1/0037##meta_registry_schema_fingerprint.sql`、`LATEST.sql`；`backend/store/meta_schema_fingerprint.go`；查询形状守卫 `backend/store/meta_schema_fingerprint_test.go`。
- 写入与通知：`backend/runner/schemasync/schema_fingerprint.go`（`prepareSchemaFingerprints`/`diffSchemaFingerprints`/`reportSchemaFingerprintChanges`）；调用点 `backend/runner/schemasync/database_sync.go`；测试 `backend/runner/schemasync/schema_fingerprint_test.go`。
- 读取与确认：`backend/api/v1/data_quality_service.go`（`targetSchemaHash`/`targetSchemaChanges`/`qualitySchemaChangedSince`）、`backend/api/v1/data_quality_target.go`（`AcknowledgeQualityTargetSchema`）、`backend/store/quality_target.go`（`qualityTargetSchemaChangedSQL`）、`backend/store/quality_schema_change.go`；测试 `backend/api/v1/data_quality_schema_change_test.go`、`backend/store/quality_schema_change_test.go`。
- 通知：`backend/component/notification/{service,data_quality}.go`（`SendToRecipients`、dedupe kind）；前端 `frontend/src/lib/notificationText.ts`、`frontend/src/pages/data-quality/DataQualityTargetPage.vue`。
- 集成：`backend/test/integration/runner/data_quality_journey_service_test.go` 的 `assertJourneySchemaDriftAfterDDLChange`（MySQL 改表 → 布尔为真 → 通知一次 → 确认 → 清信号 → 再次确认返回 0）。
- 门禁：`gofmt`、`golangci-lint run --allow-parallel-runners`、`go test ./backend/...`、`make test-integration-smoke`/`make test-integration-mysql`；前端 `pnpm biome:check lint i18n type-check test`；`buf format/lint/generate`。

## 开放边界（刻意）

- 只有布尔信号，没有"哪些列变了"的 diff，也没有指纹历史；通知里不带变更明细。
- 不覆盖函数/序列/过程；`schema_drift` 的 config 快照不被自动推进（否则会掩盖漂移）。
- 结构变更不阻塞执行、不自动暂停检查、不改问题台账——确认是人的动作。
- 集成覆盖走 MySQL journey；StarRocks/Doris 的 DDL 抓取不参与指纹，PG/SQL Server 由同一段元数据代码覆盖但未单独加 journey。
