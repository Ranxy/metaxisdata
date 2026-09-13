# 14 · 阶段 8 待确认决策清单

本文件把 [`13-remaining-work.md`](13-remaining-work.md) 第六节的 10 个待确认议题展开成可逐条拍板的决策卡。每条包含：**已核对的现状事实**、
**选项与代价**、**推荐**、**影响到的清单条目**。决策结果请回填到本文件末尾的「决策记录」表，之后阶段 8 的批次即可照此执行。

> 核对基线 `HEAD = ce3c645`；行号为核对时快照。标注「推荐」的是基于当前代码与既有阶段决策给出的建议，不是已定结论。

---

## F1 · 部署边界：单租户工作区，还是需要 per-resource（实例/数据库）授权？

**现状事实**（已验证）
- IAM 只有 WORKSPACE 策略：`proto/v1/v1/iam_service.proto:30-33` 的 `IamPolicy` 只有 `repeated Binding bindings`，无 resource/scope 字段；服务只有
  `GetWorkspaceIamPolicy`/`SetWorkspaceIamPolicy`。
- `instance` 表没有 owner/created_by 维度（`proto/store/store/instance.proto`、`backend/store/instance.go` 中 grep 无命中）。
- `memberBaselinePermissions`（`backend/store/predefined_roles.go:32-52`）**有意**把实例/数据库/血缘/OpenLineage/ExplainSQL 的读权限给
  所有已认证用户，注释写着 "discovery/read baseline"。
- 写路径已全部要求 `workspaceAdmin`；读路径已全部带 `permission` 注解（阶段 4 收口）。

**选项**
- **A. 保持单工作区（推荐）**：把"所有已认证成员可读全部实例/数据库/血缘"写成明确的产品契约（`AGENTS.md` + 部署文档），
  `13` 的 `A-C1`/`B-C2` 从"缺陷"降级为"已接受设计"。
- **B. 引入 per-instance ownership 与 scoped permission**：需要给实例/数据源加 owner 列（schema 增量）、扩展 IAM 策略形状与解析器、
  在 store 查询加范围谓词、前端加授权管理，属子系统级改造。

**推荐**：A。阶段 6 已按单租户处理，且自托管产品的核心用法就是"一个团队看全部元数据"；若将来要做多租户再按 B 立专项。

**影响条目**：F1、`04 A-C1`、`04 B-C2`、`02 阶段4-未做`、`03 A-C1`。

---

## F2 · gRPC 反射：保持匿名，还是接入认证/鉴权？

**现状事实**（已验证）
- `backend/server/grpc_routes.go:149-153` 用 `grpcreflect.NewHandlerV1/V1Alpha(reflector)` 注册，**没有传 `handlerOpts`**；而所有业务 handler
  都传了 `handlerOpts`（含 debug/audit/ACL 拦截器，`grpc_routes.go:92-103`）。因此反射请求不经过任何拦截器。
- `backend/api/auth/config.go:10-16` 的 `IsAuthenticationAllowed` 里有 `/grpc.reflection` 前缀豁免，因上述原因**不可达**。
- 报告中"前缀与 v1.3.0 实际路径不匹配"的指控经核对**是错的**（`grpcreflect v1.3.0` 的 v1/v1alpha 路径确实以 `/grpc.reflection` 开头）。

**选项**
- **A. 保持匿名 + 删除死分支并把策略写进文档（推荐）**：反射只暴露已注册 service 的方法/消息定义（自托管下等同公开 API 文档），
  改动最小；删掉不可达的豁免分支，避免下一个人误以为它生效。
- **B. 接入拦截器并限权**：给反射 handler 传 `handlerOpts`，为反射方法补白名单/权限语义，并加装配级测试。需要决定"谁能看 schema 定义"，
  且反射不是普通 Connect 业务方法，ACL 注解覆盖需要额外处理。

**推荐**：A。除非有合规要求要隐藏 API 表面，否则公开 schema 定义不是新暴露面。

**影响条目**：F2、`01 H3`（`13` 的 A7）、E8 的装配测试。

---

## F3 · 凭证加密：恢复 AEAD + 库外密钥，还是接受"同库 XOR"？

**现状事实**（已验证）
- `backend/common/utils.go:66-98`：`Obfuscate/Unobfuscate` = `base64(XOR(src, seed))`，无 nonce、无 MAC、密文确定性；空 seed 已拒绝。
- seed 是 `store.GetSecret` 读的 `AUTH_SECRET`（`backend/store/setting.go:161-175`），由 `backend/server/init.go:22-38` 在**同一个 PostgreSQL** 里
  首次启动生成，并且**同时是 JWT 签名密钥**（`backend/server/server.go:147-158`）。
- `store/llm.go:41-64`、`store/instance.go:322-375` 用它对实例密码/SSH/SSL/LLM key 做混淆；字段名分别是 `api_key_encrypted`、
  `obfuscated_*`。
- 阶段 3 曾实现 AES-256-GCM + `METADATA_SECRET_KEY`（`a1faf65`），阶段 5 **有意整体回滚**为 XOR（`7870016`）。

**选项**
- **A. 保持 XOR，明确威胁模型（推荐）**：零迁移；在 `AGENTS.md`/部署文档写明"有 DB 读权限或备份即可解密全部凭证，这是自托管单库部署下的
  已知取舍"。`13` 的 `A1`/`C12` 转为"已接受风险"。
- **B. 恢复 AEAD + 库外密钥**：需要运维提供 env/KMS 密钥来源、版本前缀、既有密文的双读/迁移路径；密钥丢失/轮换会造成不可逆的凭证失效
  （阶段 3 的破坏性影响正是阶段 5 回滚的原因）。
- **C. 可选库外密钥**：配置了就 AEAD、没配就 XOR。灵活但把"两套语义"长期留在代码里，且阶段 5 已否决过类似的优先级方案。

**推荐**：A（近期）+ 把 B 记成"有 KMS/secret 注入时的专项"。若坚持 B/C，需同时设计回滚与密钥丢失预案。

**影响条目**：F3、`13 A1`、`13 C12`、`05 C-H3`、`07 U-H2`；`08` 的字段命名。

---

## F4 · 破坏性 schema 同步：保持仅日志，还是加硬拦截？

**现状事实**（已验证）
- `backend/runner/schemasync/syncer.go:654-656`：`existMap` 里每个剩余对象直接进 `deletes`，没有任何数量/比例校验。
- 唯一保护是 `logSchemaSyncDeletion`（`:775-793`，条数/类型分布/前 20 GUID）与实例级软删的 Warn（`:347-362`）。
- 注释本身写明：MySQL 的 `information_schema` 只列连接用户可见的对象，且**不报错**——权限收窄就会得到残缺快照。
- 阶段 1 的产品决策是"只记日志、不拦截、不设阈值"。

**选项**
- **A. 保持仅日志**：零行为变化，但一次权限事故仍会清空注册表与 `column_lineage`。
- **B. 空快照拒绝 + 可选收缩阈值（推荐）**：新快照为空时拒绝删除（并记 Error）；收缩比例阈值做成配置项、默认关闭（或给保守默认）。
  这能挡住最常见、最危险的"快照变空"，又不影响正常的批量删除。
- **C. 显式破坏性确认开关**：同步到"会删除 N 条"时要求调用方带 `destructive=true`（或实例级 full-sync 确认），最安全但改变运维流程。

**推荐**：B。空快照几乎一定是异常而非意图，拒绝它的代价极低；阈值可配置以免误伤。

**影响条目**：F4、`06 R-H3`（`13` 的 B1）；与「破坏性删除留痕」文档同步更新。

---

## F5 · 视图的列：生成 COLUMN 元资源，还是只由 `viewMetadata.columns` 提供？

**现状事实**（已验证）
- `backend/runner/schemasync/syncer.go:729-736` 的 `getChildMetadataResources` 只对 `MetaType_TABLE` 生成 COLUMN 资源；但
  `backend/store/meta_resource.go:583-584` 的 `getNextLevelObjectType` 对 VIEW/MV/EXTERNAL_TABLE 也宣称下一层是 COLUMN。
- 前端元数据浏览器用 `listMetadata({parentGuid})`（sublevel，`MetadataBrowserPage.vue:897`）列子节点；视图详情组件则直接读
  `viewMetadata.columns`（`ViewMetadataDetail.vue:79-80,176-177`）。因此**浏览器里视图节点的列页签为空**，详情页却有列。

**选项**
- **A. 给视图/MV/外部表生成 COLUMN 元资源（推荐）**：与 `getNextLevelObjectType` 的声明一致，浏览器与详情都能列列；代价是 registry 行数增加
  （视图列会同时存在于 ViewMetadata 与新的 COLUMN 行）。
- **B. 收掉 `getNextLevelObjectType` 对这三类的 COLUMN 声明**：代码最简，浏览器不再假装有下一层，列只在详情页可见。
- **C. 前端在视图节点改走详情数据**：不改后端，但绕开通用的 sublevel 树接口，前端要特判。

**推荐**：A。既然 `getNextLevelObjectType` 已经这样声明，补齐实现比收窄能力更一致；若确认"视图列只在详情页看"是产品意图，则选 B。

**影响条目**：F5、`06 Low-view-columns`/`S-Q3`（`13` 的 B13）、`09` 的列相关 UI。

---

## F6 · `audit_log` 与 `meta_registry_resource_history` 要不要保留期？

**现状事实**（已验证）
- `backend/runner/maintenance/maintenance.go:44-89` 只清理过期 ExplainSQL 缓存、7 天前的 `llm_debug_log`、以及按设置的 OpenLineage run。
- 全仓库没有 `DELETE FROM audit_log` 或 `DELETE FROM meta_registry_resource_history`；两者只增不减。

**选项**
- **A. 明确永久保留**：审计合规友好，零风险；历史表随元数据变更持续增长（自托管小规模通常可接受）。
- **B. 加可配置保留期，默认不清理（推荐）**：照 `openlineage_retention_days` 的模式（默认 0 = 永久），需要时由管理员开启；
  兼顾默认安全与运维可控。
- **C. 只对历史表加保留/归档，审计永久**：折中，但需要额外决定历史保留窗口。

**推荐**：B。默认行为与 A 相同，但给运维留了出口；审计保留期需管理员显式开启，避免误删证据。

**影响条目**：F6、`03 TBC-3`；`13` 的 F6。

---

## F7 · 前置反向代理：靠文档化契约，还是改代码收紧信任判定？

**现状事实**（已验证）
- CSRF（`backend/server/csrf.go:59-79`）信任 `Sec-Fetch-Site` 的 `same-origin/none/same-site`；当 **Origin 与 Referer 都缺失**时也判为 trusted。
- 审计客户端 IP（`backend/api/v1/audit.go:344-351`）只信 `--trusted-proxies` 名单内对端发来的 `X-Forwarded-For`。
- 是否有代理、代理是否规范化/剥离这些头，是部署事实，代码内无法判定。

**选项**
- **A. 文档化部署契约（推荐）**：要求代理规范化/剥离 `Origin`/`Sec-Fetch-Site`，并把代理地址加入 `--trusted-proxies`；保留现状判定。
- **B. 代码收紧**：双头缺失改为 untrusted；对 `same-site` 也要求 Origin 匹配。更安全，但可能破坏老浏览器或非浏览器 cookie 客户端
  （API 客户端用 Bearer，不受影响）。

**推荐**：A + 可选 B 作为后续加固。当前没有已被证明的绕过，先固定部署契约成本最低；若部署环境不可控，再上 B。

**影响条目**：F7、`02 待确认2`；`01` 的 CORS/CSRF 文档。

---

## F8 · 域白名单（`domains`/`enforce_identity_domain`）：暴露，还是删除？

**现状事实**（已验证）
- `proto/store/store/setting.proto:34,37` 有 `repeated string domains = 9` 与 `bool enforce_identity_domain = 10`；
  `backend/api/v1/user_service.go:586-615` 的 `validateEmailWithDomains` 在读它，并 gating 注册/更新/登录。
- **没有任何写入方**：`UpdateWorkspaceProfileSetting` 只接受 `external_url`/`disallow_signup`/`disallow_password_signin`/
  `openlineage_retention_days` 四个 mask 路径（`setting_service.go:54-69`）；v1 设置消息不暴露这两个字段；`init.go` 只写空消息。
- 也就是说这是一条"生效但永远为空 ⇒ 等于关闭"的安全控制。

**选项**
- **A. 删除两个 store 字段 + `validateEmailWithDomains` 的域分支**：清理不可达表面（与阶段 3 reserved 未实现 setting 的做法一致）。
- **B. 通过设置 API + 前端暴露（推荐）**：恢复这条控制（限制注册/登录邮箱域），对默认 `disallow_signup=false` 的自托管部署有实际价值；
  代价是 store 字段保留 + v1 加字段/mask + `/settings/general` 加表单项。

**推荐**：B（若认可"允许注册但限制公司域"这一用法）；否则 A。两者都优于现状的"存在但不可配置"。

**影响条目**：F8、`08 setting-domain-allowlist-unreachable`（`13` 的 C11）。

---

## F9 · ExplainSQL 的 `provider_name` 要不要给前端入口？

**现状事实**（已验证）
- 后端已按 `req.Msg.ProviderName` 解析启用的 profile 并把它并入缓存 key（`backend/api/v1/explain_sql_service.go:49-66,81`）。
- 前端封装 `providerName` 并总是序列化（`frontend/src/api/explain.ts:9,18`），但**没有任何 `.vue` 传它**（`grep providerName` 只命中生成类型
  与 `api/explain.ts`），所以服务端恒回退 `configs[0]`。

**选项**
- **A. 前端加 provider/模型选择器（推荐）**：后端已就绪且缓存 key 已按 provider/model 隔离；需要拉取启用的 profile 列表、加选择器与 i18n。
- **B. 移除/`reserved` 该字段**：简化契约；但会丢掉"多 provider 下让用户选模型"的能力。

**推荐**：A。功能已完成到 API 层，只差一个 UI 入口；若短期不做 UI，则应把字段 `reserved` 而不是长期悬空。

**影响条目**：F9、`04 B-Q5`。

---

## F10 · OpenLineage `eventTime` 无时区偏移：宽容解析，还是显式拒绝？

**现状事实**（已验证）
- `backend/api/v1/openlineage_handler.go:253-258` 只用 `time.Parse(time.RFC3339Nano)`；无偏移即解析失败 → 记 Warn、`EventTime = nil`。
- `ListOpenLineageRun` 按 `event_time DESC NULLS LAST, id DESC` 排序（`store/openlineage_run.go:391`），保留清理也**豁免** NULL 行。
  即"无偏移的时间戳"会静默失去时间序位置并逃过保留期。

**选项**
- **A. 按 UTC 兜底解析（推荐）**：对无偏移值尝试追加 `Z` 后再解析；兼容不规范生产者，行为可预期。
- **B. 显式拒绝**：返回 400 并在响应里说明 `eventTime` 必须带时区偏移（OpenLineage 规范要求 RFC3339 带偏移）。
- **C. 保持现状并文档化**：不推荐——静默 NULL 会污染排序与保留策略。

**推荐**：A。兼容优先；若确认所有生产者的实现都规范，B 也是合理选择（更严格）。

**影响条目**：F10、`04 B-Q6`；`03` 的保留清理语义。

---

## 决策记录

| 编号 | 决策 | 日期/人 | 备注 |
| --- | --- | --- | --- |
| F1 | 保持单租户工作区授权 | 本轮会话 / 用户 | `A-C1`、`B-C2`、`阶段4-未做`、`03 A-C1` 转为"已接受设计"，只在文档写明读基线 |
| F2 | 保持反射匿名 + 删除不可达豁免分支 | 本轮会话 / 用户 | 实现动作＝删 `auth/config.go` 的 `/grpc.reflection` 死分支 + 文档写明"公开 schema 定义" |
| F3 | 保持 `AUTH_SECRET` 种子 XOR + 记录威胁模型 | 本轮会话 / 用户 | `13 A1`/`C12` 转为"已接受风险"，不做 AEAD/库外密钥；文档写明"有 DB 读权限即可解密凭证" |
| F4 | **保持仅日志（未采纳推荐的空快照拒绝）** | 本轮会话 / 用户 | `13 B1` 转为"已接受设计"；保留 `logSchemaSyncDeletion` 与软删 Warn，不加阈值/确认开关 |
| F5 | **收掉 COLUMN 声明（未采纳推荐的生成元资源）** | 本轮会话 / 用户 | `13 B13` 实现动作＝从 `getNextLevelObjectType` 去掉 VIEW/MV/EXTERNAL_TABLE 的 COLUMN，前端不再显示列子层级，列只在详情页 |
| F6 | **明确永久保留（未采纳推荐的可配置保留期）** | 本轮会话 / 用户 | `audit_log` 与 `meta_registry_resource_history` 永久保留，写进文档；`13 F6` 关闭 |
| F7 | 文档化反向代理部署契约 | 本轮会话 / 用户 | 保留现状 CSRF/XFF 判定；部署文档要求代理规范化/剥离头并把代理加入 `--trusted-proxies` |
| F8 | 暴露域白名单 | 本轮会话 / 用户 | `13 C11` 实现动作＝v1 设置字段 + `UpdateWorkspaceProfileSetting` mask 路径 + `/settings/general` 表单项 |
| F9 | **加前端选择器 + settings 增加管理员配置的「允许的 provider 列表」**（自定义） | 本轮会话 / 用户 | 见下方补充；这是一个新增的管理面设置，不只是 UI |
| F10 | 按 UTC 兜底解析无时区 `eventTime` | 本轮会话 / 用户 | `13 B16`（新增）实现动作＝缺失偏移时追加 `Z` 再解析 |

**F9 补充（自定义决策的展开）**：ExplainSQL 前端要加 provider/模型选择器；同时新增一个**工作区级"允许的 provider 列表"设置**，由管理员在
Settings 中配置，ExplainSQL 的选择器只列出该列表内且已启用的 profile。也就是说：

1. `WorkspaceProfileSetting`（或等价 setting）新增"allowed LLM provider profiles"列表；`/settings/general`（或 LLM 设置页）提供多选 UI；
2. `ExplainSQLService` 在解析 `provider_name` 时校验其属于允许列表（不在列表内 → `InvalidArgument`，并确保缓存 key 仍按 provider/model 隔离）；
3. 前端只从"允许列表 ∩ 已启用"里渲染选择器，默认仍回退到第一个允许项；
4. 需补单测（列表为空时语义、请求非允许 provider 被拒）与前端 Vitest。

这个决策同时让阶段 8 多出一个小功能项（见 `13` 的 `C14`），并需要一次 proto/store 设置字段改动（走 `LATEST.sql` + 增量 + `buf generate`）。

---

## 决策后的批次映射（实际结果）

| 原批次 | 决策后的实际内容 |
| --- | --- |
| 批 0 · 决策 | **已关闭**（F1–F10 全部拍板，见上表） |
| 批 1 · 契约与文档一致性 | **✅ 已完成**：`C1`、`C4`、`C5`、`C6`、`C7`、`C9`、`C10`、`C13`、`D14`、`D15`、`D16`、`D21`、`B14`，以及 F2 的死分支删除与 F1/F3 的威胁模型文档（`04b9adc` `21a95a9` `d9b0f16` `c1b6c21` `507e06d` `4f78442`；逐项对照见 `13` 第八节「批 1 实施记录」） |
| 批 2 · 正确性 | **✅ 已完成**：`B2`–`B12` 与 `B16`（`39b2ce5` `ce5a4ff` `7ecca87` `e7e64f1` `1c8509b` `29a143a` `2d8e2c2` `e9ce8a5` `2c26648`；两处偏差——B11 保持同步执行、B12 未强制目录等于基线行——见 `13` 第八节「批 2 实施记录」） |
| 批 3 · 安全收尾 | **✅ 已完成**：`A4`、`A5`、`A6`、`A8`、`A9`（`ac616d7` `cdf5ec9` `2968b88` `dd9c32d`；`A6` 有一处偏差——未收紧 scheme/私网，改为"改 `base_url` 必须同请求给 `api_key`"，见 `13` 第八节「批 3 实施记录」） |
| 批 4 · 性能与整洁 | 不变；`C12` 随 F3 关闭（保留命名，最多改注释） |
| 批 5 · 测试与 CI | 不变 |
| 批 6 · 依赖决策的收尾 | 实现：`B13`（收 COLUMN 声明，F5）、`C11`（暴露域白名单，F8）、`C14`（provider 选择器 + 允许列表，F9）；文档：F4/F6/F7 的取舍写进 `AGENTS.md`/部署文档；`F6` 关闭为永久保留 |
