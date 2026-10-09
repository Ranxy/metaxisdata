# 后端仍未开放的工程债 — Reference

> Status: **活跃清单**（核对基线 `89a51cc`，2026-10-08）。来自早期 backend-review 的逐条复核；已修复与已过时的条目不再保留，历史见 git。
> 编号沿用旧 backend-review 的 `A/D/E`（与 `.agents/docs/security-open-items.md` 的 `H/M/L` 编号不同源，勿混用）。
> 有意接受的设计决策见 [docs/security-posture.md](../../docs/security-posture.md)；开放的安全类条目见 `.agents/docs/security-open-items.md`（不要重复）。

## 仍开放

| # | 位置 | 问题 | 影响 | 建议 |
| --- | --- | --- | --- | --- |
| D3 | `backend/store/setting.go:101`（`GetSetting`）、`:130`、`backend/store/idp.go:37`、`backend/store/instance.go:75`、`backend/store/manual_sql.go:288`、`backend/store/meta_resource.go:136/157/192/222/238/279/374`、`backend/store/principal.go:163`、`backend/store/database.go:69/97` | 单语句读仍包 `BeginTx(ReadOnly)` + `Commit`，全 store 共 14 处；`GetSetting` 跑单条 `listSettingImpl`，`GetManualSQL`（`manual_sql.go:272` → `ListManualSQL:286`）还要在同一个事务里带出 tags/attributes（`:676`、`:707`） | 每次读多两个往返与一个事务；纯读路径没有一致性收益 | 单语句直接在连接池上跑；只有多语句不变量才开事务 |
| D4 | 反写调用方 `find`：`backend/store/instance.go:55`（`find.ShowDeleted = true`）、`backend/store/policy.go:316`（`find.ShowAll = true`）、`backend/store/meta_resource.go:381`（`find.LimitPreObjectType`）。nil `find` 无守卫：`backend/store/column_lineage.go:254`、`openlineage_run.go:444`、`openlineage_task.go:432`、`external_dataset.go:110`、`namespace_mapping.go:75`、`llm.go:185` | getter 直接改调用方传进来的 `find`（今天调用方恰好都新建结构，属潜伏）；6 个 `List*` 不判 nil，只有 `manual_sql.go:565`、`notification.go:101` 容忍 nil | 复用同一 `find` 的调用方语义被静默改变；传 nil 直接 panic | getter 复制到局部或改显式选项；6 个 list 统一 nil 语义（拒绝或空过滤器） |
| D6 | `backend/store/manual_sql.go:24`（字段）、`:511`、`:557`、`:645`（读时由 name 回填）；`backend/store/openlineage_api_key.go:82-96` 与 `backend/migrator/migration/LATEST.sql` 的 `openlineage_api_key` 建表 | ① `ManualSQLMessage.ManualSQLID` 没有对应列（`manual_sql` 表只有 `name`），读路径用 `name` 推导；② OL API key 只有 `revoked_at`，没有 `expires_at`，也不在 store 消息里 | 幻影字段让「资源名」有两个真相来源；key 只能手动吊销 | ① 删除 `ManualSQLID` 或补列；② 加过期列（需 schema 增量 + `LATEST.sql` 同步） |
| D10 | store 版：`backend/store/manual_sql.go:402`（`buildDeleteColumnLineageByGUIDStatement`）+ `backend/store/meta_resource.go:85`（子树条件，连后代一起删）；runner 版：`backend/runner/schemasync/syncer.go:1076-1089`（`meta_guid`/`source`/`target` 精确匹配的 3 谓词） | 血缘删除有两套 SQL，各自演化 | 转义 / 子树语义漂移：删不干净或删过头 | 合并为一个带 source/target 方向参数的 helper |
| D20 | `backend/store/instance.go:199`（调用）、`:276-277`（`hasHostPortFilter`） | 实例列表是否 `CROSS JOIN` 数据源，靠对生成 SQL 的子串匹配（`strings.Contains(where, "ds ->> 'host'")`） | join 与 `backend/api/v1/filter.go` 的列文本强耦合，改列名即静默丢 join（值本身已参数化，用户不可控） | 让 filter 翻译器返回结构化标记（如 `NeedsDataSourcesJoin`） |
| C8 | `backend/api/auth/header.go:91-96`；常量 `backend/api/auth/auth.go:36` | `GetTokenDuration(_ context.Context, _ *store.Store)` 忽略两个参数，恒返回 7 天常量，6 个调用点（`backend/api/auth/header.go:51` 等）白传 ctx/store；`token_duration` 设置已删除（`proto/store/store/setting.proto:79` 已 `reserved`），注释仍写 "maybe we can add a setting in the future" | 死参数 + 误导性未来注释 | 删参改常量，或真正接回一个可配置项 |
| 文档残留 | `backend/AGENTS.md:22` | 目录表仍列 `backend/plugin/metric/` 「Metric collection and reporting」，该包已随指标栈删除（见 D8），仓库里不存在 | 新 agent 会去找不存在的包 | 删掉该行（AGENTS.md 重写会顺带消失） |

## 有意保留（勿再开启）

| 条目 | 今天的理由 | 记录处 |
| --- | --- | --- |
| **A4** 连接测试把驱动错误原样回传 | 旧 review 判为信息泄漏，今天**有意反转**：`dataSourceConnectionError`（`backend/api/v1/instance_service.go:664-670`）特意保留驱动原因，注释（`:654-663`）说明测试连接需要实例/数据源写权限、host/port/凭据本就是调用方给的，收敛成 `invalid datasource <type>` 会让该操作无从报告 | 代码注释即理由（未进 `docs/security-posture.md`） |
| **D7** store 层不校验数据源 | 只有 `CreateInstance` 调 `validateDataSources`（`backend/store/instance.go:98`，定义 `:291`），`UpdateInstance` 刻意不调；校验留在 API 层 `checkInstanceDataSources`（`backend/api/v1/instance_service.go:149`，定义 `:200`）。理由：`SyncInstance` 用库里既有 metadata 调 `store.UpdateInstance`，历史 0/2 个 ADMIN 行若在 store 层被拒会让同步永久失败 | 未进 `docs/security-posture.md`（原理由随旧 review 删除，保留在 git 历史） |
| **D8** 同步失败只有 `slog.Warn`，无计数器/告警 | `backend/metric/`、`backend/plugin/metric/` 均已删除（已核实不存在）；恢复计数需先决定是否重建遥测设施 | 未进 `docs/security-posture.md`；残留引用见「仍开放 · 文档残留」 |
| **D9** runner 间隔/上限是编译期常量 | `backend/runner/schemasync/syncer.go:30-40`（15m/10s/15m/100/3）、`backend/runner/lineageanalyzer/analyzer.go:25-30`（1h/10s/10）；无运维覆盖，想要可配置需先做产品决策 | 未进 `docs/security-posture.md`（已接受设计） |
| **F1–F7 六项已接受决策** | 工作区级单租户授权（:5）、gRPC 反射匿名（:12）、凭证加密「同库数据密钥」的取舍（:7）、破坏性 schema 同步仅日志（:13）、`audit_log`/`meta_registry_resource_history` 永久保留（:16）、反向代理头契约（:17） | `docs/security-posture.md` 对应行 |

## 流程事实

| 项 | 事实（基线 `89a51cc`） |
| --- | --- |
| CI 实跑 | 已在 GitHub 实跑：`.github/workflows/ci.yml` 共 78 次 run，最近的 main push（`89a51cc`）与同分支 PR run 均为 `success`。旧 E7「从未实跑」作废 |
| 覆盖率门禁 | 前端已有：`frontend/vitest.config.mts:50-58` 对 `src/utils`、`src/lib`、`src/composables` 设 per-file 阈值，CI `frontend` job 跑 `test:ci` 强制（`.github/workflows/ci.yml:144-145`）。Go 侧只有 `go test -race -count=1 -cover ./...`（`:55`），无阈值 |
| 0 个 `_test.go` 的包（E12） | 11 个可补测的：`backend/bin/server`（main）、`backend/bin/server/cmd`、`backend/common/log`、`backend/common/stacktrace`、`backend/component/dbfactory`、`backend/config`、`backend/plugin/db/mssql`、`backend/plugin/idp`、`backend/plugin/schema/mssql`、`backend/runner/maintenance`、`backend/utils`；另有 buf 生成的 `backend/common/permission/gen`（不必补测） |
| 文件级缺口（E13） | `backend/api/v1/group_service.go`、`role_service.go` 无测试文件；store 只剩 `column_lineage.go`/`openlineage_run.go`/`namespace_mapping.go`/`external_dataset.go`/`llm.go` 无直接测试（10 个 store 测试集中在 database/instance/manual_sql/meta_resource/encryption/audit_log/env 等）；`backend/runner/schemasync` 已有 `syncer_test.go`/`operation_test.go`/`object_definition_test.go`，主循环缺口已收窄 |
| 测试基建遗留（E3 余半） | 启动 seed 仍建 `it_app`（`backend/test/integration/env/testenv.go:96`、`:125`），reset 仍清 `it_drop_me` 的连接与库（`:111`、`:152`、`:156`），但仓库已无用例创建它（只在 `backend/test/integration/README.md:54` 被提到）；`it_app` 仍被 `backend/test/integration/runner/instance_data_source_service_test.go:69` 当作 data source 的 database 值，删启动库前要先改该用例 |
| 风格遗留（E9 余半） | `backend/migrator/migrator_test.go` 有 **16** 处 `t.Fatalf`（行 33、38、41、58、63、83、86、104、112、124、134、141、147、164、175、190）未迁 testify；旧 review 写的 15 处已不准 |

> 旧 A3（审计脱敏按字段名黑名单 + 子串，`backend/component/audit/audit.go:224`、`:242`；实测漏 `ssl_ca`，`ssl_key`/`ssl_cert`/`ssh_private_key` 已覆盖，见 `backend/api/v1/audit_test.go:62-64`）已由 `.agents/docs/security-open-items.md` 的 **L5** 承接（含「长期改 proto sensitive 注解 + 值模式扫描」的建议），本表不重复。

## 已作废的旧结论（勿再引用）

| 旧结论（出处） | 今天的事实 |
| --- | --- |
| 凭证仍是「同库 `AUTH_SECRET` 种子 XOR」，只补威胁模型文档（A1/F3/C12、`07 U-H2`、`04 M18`、`05 C-H3`、`backend-review/README` 阶段 5 的回滚） | `v1:` 前缀 AES-256-GCM + 12B nonce + 16B tag（`backend/common/crypto/crypto.go:29`、`:113-118`）；数据密钥由 `backend/store/encryption.go:85` 解析，可用 `METAXISDATA_ENCRYPTION_KEY` 包裹，取舍见 `docs/security-posture.md:7` |
| 令牌吊销仍是进程内 LRU，跨副本失效（A2） | 已落库 `revoked_token`（`backend/store/revoked_token.go` + `backend/migrator/migration/0.1/0013##revoked_token.sql`）；进程内只剩 ≤30s 决策缓存，见 `docs/security-posture.md:19` |
| CI 从未在 GitHub 上实跑（E7） | 78 次 run，最近 `89a51cc` 与同分支 PR 均 `success` |
| 前端无覆盖率门禁（E6 前半） | `frontend/vitest.config.mts:50-58` per-file 阈值 + CI `test:ci` 强制 |
| 模块报告里的 `✅/◐/⏳` 标记与 backend-review 的 open 清单 | 标记两个方向都严重过期（旧 `13-remaining-work.md:3-5` 自述，该文件已随整组删除）；本文件是唯一收敛结果，历史见 git |

## 复核方式

逐条 `grep`/`read` 当前代码、`proto/` 与 `LATEST.sql` 即可复核；「0 测试的包」用「列出全部含 `.go` 的目录，再看有没有 `*_test.go`」重新统计。修好一条就从本表删除：安全类写进 `docs/security-open-items.md`，设计类写进 `docs/security-posture.md`，并把本文件顶部的基线 sha 与日期更新为当次 HEAD。
