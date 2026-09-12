# 05 · 组件层：component/llm、dbfactory、state

**范围**：`backend/component/llm/`（`agent.go`、`client.go`、`fetcher.go`、`registry.go`、`tools.go`、`message.go`、`event.go`、`debug.go`）、`backend/component/dbfactory/dbfactory.go`、`backend/component/state/state.go`、`backend/config/profile.go`。

**结论**：LLM agent 循环有两个会相互放大的高危缺陷（吞掉 body 读取错误 + 1MiB 静默截断；channel 发送不感知 ctx 导致 goroutine 永久泄漏），并且"流式"实际是整包缓冲。`dbfactory`/`state` 很小，但 `dbfactory` 是 SSRF 与 nil deref 的入口。

**阶段 0 更新**：C-H4 ◐、M8 ◐——两者的"任意已认证用户可利用"入口已由管理员权限注解关闭，但组件内部的 nil 判空、URL 校验、响应大小限制**均未修**；C-H1/C-H2/C-H3 未处理。

**阶段 2 更新**：C-H1 ✅（所有发送 ctx-aware + handler 取消子 context）、C-H2 ✅（真流式 + 错误/截断传播 + 不缓存残缺结果）、M1 ✅（空回答与 MaxTurns 耗尽均为错误）、M2 ✅（畸形 chunk/未知 finish_reason 报错，tool-call index 不再丢）、M3 ✅（真流式 + 超时改造 + 共享 http.Client）；C-H3（XOR 混淆）、M4-M8、M10 仍未处理（阶段 3）。

---

## 高（High）

### C-H1. Agent 循环在消费者退出/客户端断开时泄漏 goroutine
> **✅ 已修复（阶段 2）** · `8acbfe6`：新增 `sendEvent`/`sendRaw`，所有发送都是 `select { case ch <- evt: case <-ctx.Done(): return }`；`ExplainSQL` handler 用 `context.WithCancel(ctx)` 派生子 context 并在返回时 `cancel()`，因此即使消费者只是停止 range（连接未被框架取消），生产者也会退出。

- **位置**：`backend/component/llm/agent.go:20,42-47,49,113`、`backend/api/v1/explain_sql_service.go:142,184`
- **证据**：channel 容量 32；唯一的 ctx 检查是 L42-47 的非阻塞 `select`；所有 `ch <- ...` 都没有 `ctx.Done()` 分支。消费者在 `AgentEventError` 或 `stream.Send` 失败时提前返回。
- **影响**：消费者停止 range 后，生产者填满 32 槽即永久阻塞；每次中断的 ExplainSQL 流泄漏 1 个 goroutine + 累积的 `messages`（系统提示 + 最多 6 轮工具结果）。反复中断是任意已认证用户可用的廉价内存/goroutine DoS。
- **修复**：所有发送走 `select { case ch <- evt: case <-ctx.Done(): return }` 的辅助函数。

### C-H2. 吞掉 body 读取错误 + 1MiB 静默截断，并当作成功写入缓存
> **✅ 已修复（阶段 2）** · `8acbfe6`：改为 `bufio.Scanner` 逐行读 SSE，读取错误、畸形 chunk、`finish_reason=length`、未知 finish_reason、超过 32MiB 上限、无 `finish_reason` 且无 `[DONE]` 的 EOF、空回答全部返回错误；`ExplainSQL` 在 `AgentEventError` 时提前返回，因此这些结果既不返回也不写缓存。

- **位置**：`backend/component/llm/agent.go:193,205,272`
- **证据**：`bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))`；错误被丢弃；`parseStream` 未见 `finish_reason` 时仍发 `Done`。
- **影响**：TCP reset、5 分钟超时或 ctx 取消时，`bodyBytes` 是部分 SSE，但 `StatusCode` 仍 200 → `streamOneTurn` 返回"成功"的 assistant 消息；`ExplainSQL` 解析该部分文本并 `UpsertExplainSQLCache` 持久化（`explain_sql_service.go:212`），污染按 SQL/meta hash 索引的缓存。超过 1MiB 的响应静默截断，丢失尾部 tool-call 参数。
- **修复**：检查 `ReadAll` 错误并作为 `rawStreamChunk{Error:...}` 返回；改用 `bufio.Scanner`/`json.Decoder` 直接读 `resp.Body`；把截断视为错误；出错/截断时禁止写缓存。（另：`data: [DONE]` 视为正常结束，以兼容不设 `finish_reason` 的 provider；`llmDebugBodyLimit` 只限制调试日志副本，不影响解析。）

### C-H3. 凭据"加密"用的是同库存储的密钥，且无完整性校验
- **位置**：`backend/common/utils.go:65-84`、`backend/store/setting.go:155-168`、`backend/server/init.go:29-38`
- **证据**：重复密钥 XOR + base64；seed 来自同一 PG 的 `setting.AUTH_SECRET`。
- **影响**：(a) 任何 DB 读取/备份即可还原所有实例密码、SSH 私钥、SSL key、LLM API key；(b) 轮换/丢失 `AUTH_SECRET` 会静默解出乱码，无报错；(c) 相同明文产生相同密文（等值泄漏）；(d) `seed == ""` 且输入非空时整数除零 panic。
- **修复**：AES-256-GCM（或 `secretbox`），密钥来自环境/KMS，随机 nonce + 版本前缀，认证失败 fail-closed；`seed == ""` 时返回错误。
- **详见**：`07-common-utils.md`。

### C-H4. `dbfactory` 连接任意用户主机，且 instance 为 nil 时 panic
> **◐ 部分修复（阶段 0）** · `ec49607`：触发该路径的 `validate_only`（`CreateInstance`/`AddDataSource`/`UpdateDataSource` 等）已要求 workspaceAdmin，任意已认证用户不再能驱动内网探测。**内网 allowlist/deny 经产品决策主动放弃**（自托管场景必须允许连接内网数据库）。**剩余**：`instance` 为 nil 的 panic 与 `db.Open` 错误未包装仍未修。

- **位置**：`backend/component/dbfactory/dbfactory.go:29-31,40-58`、`backend/utils/utils.go:11`
- **证据**：`utils.DataSourceFromInstanceWithType(instance, ...)` 解引用 `instance.Metadata` 无判空；`db.Open(...)` 无 host/port 校验。
- **影响**：配合 `api/v1/instance_service.go:286-303` 的 `validate_only` 路径，任意已认证用户可让服务端对任意内网地址做 TCP 连接 + 协议探测（SSRF 端口扫描，错误信息可区分服务）；`db.Open` 错误未包装，调用方无法分类。
- **修复**：判空返回 `common.Errorf(common.Invalid, ...)`；增加可选出网 allowlist / 阻断 metadata IP；用 `common.Wrapf(err, common.DBConnectionFailure, ...)` 包装。

---

## 中（Medium）

- **M1. 达到最大轮次与"成功"无法区分**：`agent.go:41-113`，`MaxTurns` 从未被设置（恒为 6，`explain_sql_service.go:128-134`）；第 6 轮的工具结果追加后从未发给模型，最终文本可能是空，但同样发 `AgentEventAgentEnd{Done:true}`，且 `evt.Done` 从未被读取（`explain_sql_service.go:186-187`）；被截断的答案仍会入缓存。空内容响应（如代理返回 HTML）也会被当成有效解释缓存。 —— **✅ 已修复（阶段 2，`8acbfe6`）**：循环耗尽 MaxTurns 且仍有 tool call 时改发 `AgentEventError`（"reached the maximum of N turns"），空回答同样报错，因此都不会被当作成功或写入缓存。`MaxTurns` 仍未由调用方显式设置（默认 6）。
- **M2. `parseStream` 静默丢弃畸形 chunk、tool-call index 间隙与非 `data: ` 行**：`agent.go:230,251,264,277`。`buildAccumulatedToolCalls` 按连续 `0..len-1` 索引 map，provider 从 index 1 开始或留空即丢 tool call；`finish_reason: "length"` 被当正常完成。 —— **✅ 已修复（阶段 2，`8acbfe6`）**：畸形 `data:` 行报错，`finish_reason` 只接受 `stop`/`tool_calls`（`length` 与未知值报错），tool call 按 index 排序收集（不再丢非连续索引），非 `data: ` 行仍跳过（兼容 SSE 注释/心跳）。
- **M3. 全量缓冲使流式名存实亡，且 5 分钟超时是总生成上限**：`agent.go:193`、`client.go:6`。`AgentEventContent` 在整轮结束后一次性产出，TTFT = 总生成时间；超过 5 分钟中途失败并按 H2 被当作成功。每次请求新建 `*http.Client`（连接仍复用 `http.DefaultTransport`）。 —— **✅ 已修复（阶段 2，`8acbfe6`）**：body 边到边解析，"流式"名副其实；去掉 5 分钟总超时，改为 30s 响应头超时 + 60s 空闲读超时（`idleTimeoutReader` 在无数据时取消请求，长回答不会被切断）；`llmHTTPClient` 改为包级共享（连接池化）。
- **M4. 有 tool call 时 assistant 文本被丢弃**：`message.go:25-29`，`ConvertToLlm` 在 `len(ToolCalls)>0` 时不带 `Content`，下一轮丢失模型的推理/前言；`default:` 空分支静默丢弃未知 role。
- **M5. `NewDBDebugLogger` 无界 fire-and-forget goroutine + 脱离请求的 context**：`debug.go:14-18`，每次 LLM 调用一个 goroutine、`context.Background()`、错误丢弃（`_ = err`）、无并发上限、无保留策略。
- **M6. CEL 条件 fail-open**：`common/cel.go:257-299`，`if !celtypes.IsBool(out) { return true, nil }`；env 声明了 `resource.database` 等属性但 `EvalBindingCondition` 只绑定 `request.time`。任何引用 `resource.*` 的 binding 求值为 residual 即返回 true → 本应限定单库的角色被全局授予。当前 binding 构造不带 condition（`store/policy.go:68`），属潜伏。
- **M7. 每次 binding 求值都新建 CEL 环境**：`common/cel.go:262`，`cel.NewEnv` 在 `validateIAMBinding`（`utils/member.go:17-24`）中每个 binding 调用一次，而 `GetUserFormattedRolesMap` 每请求遍历所有 binding。
- **M8. fetcher SSRF + 无界响应读取**：`fetcher.go:26,49`，`url := baseURL + "/v1/models"` 无 scheme/host 校验，`json.NewDecoder(resp.Body).Decode` 无 `io.LimitReader`；配合 `llm_service.go:94-97` 任意 base_url → 带存储密钥的 SSRF 与内存放大（详见 `04` B-H5）。（**◐ 阶段 0**：profile 写操作与 `FetchLLMModels` 已限 workspaceAdmin，任意已认证用户的利用路径被切断；`base_url` 校验与 `LimitReader` 仍未加。）
- **M9. `BuildContextFromMetadata` 位置化配对并行切片**：`tools.go:36-49`，调用方 `explain_sql_service.go:341-361` 只在查找成功时 append，之后 `guids[:len(metas)]` 配对错位，导致 DBName/SchemaName 归属错误并进入 LLM 提示。
- **M10. 每请求查询全部 LLM profile 且无缓存**：`registry.go:34-35` + `explain_sql_service.go:84`，每次 ExplainSQL 一次 DB 往返 + 解密全部 profile 的 key；`configs[0]` 是隐式的"最近更新"。

---

## 低（Low）

- `registry.go:55` `APIKey: p.Metadata.ApiKeyEncrypted` 实际是解密后的明文（字段名误导，容易被打日志）；`BaseURL` 为空时生成相对路径 `/v1/chat/completions`，报出令人困惑的 `unsupported protocol scheme`。
- `state.go:49-52` `resourceLimiter.Decrement` 可能把计数减成负数（不配对调用时）。
- `state.go:17` `TokenExpireCache` 容量 128（见 `02` H1）。
- ~~`config/profile.go` 的 `Profile.Secret` 从未被赋值（`getBaseProfile` 不含它），因此 `store.GetSecret` 总是回退到 DB 设置——这既是 C1（JWT 密钥）问题的另一半，也说明"从 profile 注入密钥"的设计从未接通。~~ —— **阶段 0 已接线（`adfec91`）**：`getBaseProfile` 现在用 `os.Getenv("JWT_SECRET")` 赋值；注意 JWT 密钥与 `store.GetSecret()`（字段混淆）已分离，不要再把 `JWT_SECRET` 写进 `store.Secret`，否则会破坏既有数据的去混淆。

---

## 死代码与遗留债务

- `AgentConfig.Hooks`/`AgentHooks.BeforeToolCall`/`AfterToolCall`（`event.go:44-52,74-76`）从未被设置，`agent.go:71-95` 两个 hook 分支是死代码；`AgentEvent.Done` 从未被读取；`AgentConfig.MaxTurns` 从未设置；`AgentEventTurnEnd` 发出但无消费者。
- `metric` 包与 `plugin/metric` 的 Reporter/Collector 无任何实现（见 `07`）。
- `config.Profile.LastActiveTS` 只写不读。
- `component/state` 的 `TokenExpireCache`/`resourceLimiter` 语义见 `02`。
