# 环境管理（Environment）— Reference

> Status：**implemented**（`51f6d5a`）。维护参考，对应 `backend/api/v1/environment_service.go`、`backend/store/environment.go`、`backend/store/environment_mutation.go`。
> Related：`docs/security-posture.md`（工作区级授权）、`.agents/docs/iam-and-permissions.md`（权限目录）。

## 这是什么

“环境”不是数据库枚举，而是 `setting` 表里 `SettingName.ENVIRONMENT` 一行的 JSONB（`store.EnvironmentSetting`，字段 `id/title/tags/color`），首次启动由 `backend/server/init.go:126` 播种 `test`/`prod`。`EnvironmentService`（List/Create/Update/Delete）把它当资源（`environments/{id}`）暴露；实例表单用 `EnvironmentSelect` 选择而不是输入自由文本；Settings → Environments 页负责改名、改色、删除。**没有数据库迁移**，存储形状本来就存在。

## 决策

| 决策 | 理由 |
| --- | --- |
| 写权限用 `metaxisdata.settings.update` | 环境属于工作区设置，不是一个独立的资源族。 |
| 读路径新增 `metaxisdata.environments.list`，授予 member 基线 | ACL 守卫拒绝无注解的读，且没有现成权限同时覆盖实例表单、数据库页与设置页。 |
| id 由标题派生且创建后不可变 | 实例存的是裸 id（`instance.environment`）；id 一旦可改，既有引用就会悬空，所以只有标题可改。 |
| 创建时只填展示名，颜色由服务端给 | 用户从不输入 id；颜色从固定调色板派生。 |
| `tags` 保留但不出面 | 字段在 API 上往返（`update_mask` 支持 `tags`），本轮没有编辑界面。 |
| 仍被存活实例引用时禁止删除 | 否则留下无法解析的悬空 id。 |
| 实例上的环境仍然必填 | `environmentRequired` 校验保留，只换输入控件。 |

## API 契约

| RPC | HTTP | 权限 | 审计 | 说明 |
| --- | --- | --- | --- | --- |
| `ListEnvironments` | `GET /v1/environments` | `metaxisdata.environments.list` | 否 | 内存分页（`parseLimitAndOffset` + `paginateInMemory`），默认 50、上限 1000；填充 `instance_count`。 |
| `CreateEnvironment` | `POST /v1/environments` | `metaxisdata.settings.update` | 是 | 只接收 `title` 与可选的 `color`；`name` 由服务端派生。标题重复 → `CodeAlreadyExists`。 |
| `UpdateEnvironment` | `PATCH /v1/environments/{environment.name}` | `metaxisdata.settings.update` | 是 | `update_mask` 只接受 `title`/`color`/`tags`，其它路径 → `CodeInvalidArgument`；`name`（id）不可改。 |
| `DeleteEnvironment` | `DELETE /v1/{name=environments/*}` | `metaxisdata.settings.update` | 是 | 存活实例数 > 0 → `CodeFailedPrecondition`，消息里带计数。 |

字段形状（`proto/v1/v1/environment_service.proto`）：`name`（OUTPUT_ONLY，`environments/{id}`）、`title`（REQUIRED）、`color`（OPTIONAL，调色板键）、`tags`（OPTIONAL，保留）、`instance_count`（OUTPUT_ONLY，仅 List 填充）。

id 派生（`backend/store/environment_mutation.go:120`）：标题转小写，`[a-z0-9]` 之外的连续字符压成一个 `-`，去掉首尾 `-`；结果为空（例如全中文标题）时回退 `env`，再依次 `env-1`、`env-2` … 直到不冲突——确定性，因此可单测。标题重复判定**大小写不敏感**。颜色留空时按 id 做 FNV 哈希落到调色板（`slate`/`blue`/`green`/`amber`/`orange`/`red`/`violet`/`pink`），所以同 id 删除后重建颜色不变；显式传入非调色板键 → `CodeInvalidArgument`。

## 存储机制

- 环境仍住在 `setting.value` 的 JSONB 里，不建独立表。
- 三个写操作都走 `mutateEnvironments`（`backend/store/environment.go:123`）：`BEGIN` → `SELECT … FOR UPDATE` 锁住 `ENVIRONMENT` 行 → 纯函数改内存 → `protojson.Marshal` → `INSERT … ON CONFLICT DO UPDATE` → `COMMIT` → 刷新 `settingCache`。
- `db.environment` 目前没有任何写入方：读路径用 `COALESCE(db.environment, instance.environment)`（`backend/store/database.go:300`），所以数据库继承实例的环境；`UpdateDatabaseMessage.EnvironmentID`（`backend/store/database.go:39`）是为覆盖预留的，尚无调用者。

## 不变量

| 规则 | 破坏后果 |
| --- | --- |
| 环境 id 创建后不可变 | 已配置该环境的实例与库指向一个不再存在的值。 |
| 写路径必须持有 `ENVIRONMENT` 行的 `FOR UPDATE` 锁 | 两个并发写各自覆写整个 JSONB blob，先写的那次新增静默丢失。 |
| 调色板键必须由服务端校验，任意字符串不能进 `color` | 前端按 key 映射颜色类名，未知键会渲染成没有样式的徽标。 |
| 删除前先 `CountInstancesByEnvironment`，且只数 `deleted = false` | 留下悬空引用；已删除的实例不该继续占住环境。这道检查在 service 层（`backend/api/v1/environment_service.go:131`），store 的 `DeleteEnvironment` 不设防，新调用方必须自己重复它。 |
| 实例创建/更新时的环境存在性校验保留 | 绕过选择器的 API 客户端能写入不存在的环境。 |
| `instance_count` 只由 `ListEnvironments` 填充 | 别的 RPC 返回它，会让客户端以为自己拿到了统计。 |

## 失败场景

| 场景 | 行为 |
| --- | --- |
| 标题重复 | `CodeAlreadyExists`（`common.Conflict` 经 `backend/api/v1/error_interceptor.go:67` 映射）。 |
| 标题为空或全空白 | `CodeInvalidArgument`。 |
| 颜色不在调色板 | `CodeInvalidArgument`。 |
| `update_mask` 缺失或含未知路径 | `CodeInvalidArgument`。 |
| 删除仍被引用的环境 | `CodeFailedPrecondition`，消息含实例数。 |
| 调用方无 `settings.update` | 选择器仍可选中已有环境，但创建项被禁用并提示联系管理员；设置页只读。 |
| 值指向一个已不存在的环境 id | `titleOf` 回退显示裸 id，值不会被静默清空。 |

## 未做项

- 在 UI 里编辑 `tags`（字段能往返，没有界面；`backend/store/environment_mutation.go:68` 已支持该 patch）。
- 把环境从 settings blob 迁到独立表——将来若需要独立权限或排序再说，现在不需要。
- 新增 `metaxisdata.environments.create` 权限；若要让非管理员自助建环境，需要补目录项并放宽 member 基线。
- 按环境做策略（例如只有特定角色能指派 `prod`）。

## 代码与测试位置

- 服务层：`backend/api/v1/environment_service.go`；注册 `backend/server/grpc_routes.go:107`（构造）、`:178`（handler）、`:262`（REST gateway）；播种 `backend/server/init.go:126`。
- 存储层：`backend/store/environment.go`、`backend/store/environment_mutation.go`（纯函数，不碰数据库）。
- 前端：`frontend/src/api/environment.ts`、`frontend/src/store/modules/environment.ts`、`frontend/src/components/environment/EnvironmentSelect.vue`、`frontend/src/pages/settings/EnvironmentSettingsPage.vue`、颜色映射 `frontend/src/utils/environment.ts`、路由 `frontend/src/router/index.ts:67`（`permission: metaxisdata.settings.get`）。
- 测试：`backend/store/environment_mutation_test.go`、`backend/api/v1/environment_service_test.go`、`frontend/src/store/modules/environment.test.ts`、`frontend/src/utils/environment.test.ts`；端到端 `backend/test/integration/runner/environment_service_test.go`。
- 门禁：`go test ./backend/...`；`make test-integration` 跑端到端用例；`frontend/` 下按序 `pnpm biome:check`、`pnpm lint`、`pnpm i18n`（改过 locale 再加 `pnpm i18n:sort`）、`pnpm type-check`、`pnpm test run`。
