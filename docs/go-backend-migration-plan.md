# ChatAPI 后端迁移到 Go 工程规划

本文档用于规划 `refactor/migrate-to-go` 分支的后端迁移。目标不是简单把 Flask 代码翻译成 Go，而是把后端重构成可工程化协作、可运营部署、可开源发布、可长期维护的 Go 服务，同时保持现有前端和 OpenAI/Anthropic 兼容接口尽量无感迁移。

## 1. 迁移目标

### 1.0 当前落地状态

截至当前重构分支的第一阶段实现，仓库已允许直接删除旧 Python `backend/` 内部实现，并开始在 `backend/` 目录内以 Go 结构替换：

- `backend/` 作为 Go 后端工程根目录，内部采用 `cmd/`、`internal/`、`web/` 结构。
- 旧 Flask `backend/` 实现可以直接移除，不再做双后端长期并存。
- 第一批实现优先目标不是一次性补齐所有业务，而是先建立可长期演进的 Go 基础骨架：
  - 统一配置加载和命令入口；
  - SQLite 启动与 bootstrap migration；
  - `serve` / `lab` 模式分离；
  - 健康检查、基础鉴权占位、Lab 访问控制、前端静态托管。

当前已额外落地的最小业务闭环：

- `/v1/responses` 请求可创建真实 pending turn，而不是仅返回静态占位。
- 请求会写入 SQLite `conversations` / `messages` 表，并携带前端当前需要的 `request_debug`、`realtime_status` 元数据。
- Web 控制台可通过 `/api/conversations/{id}/messages` 读取消息。
- 工作台核心交互 `/api/chat/output/delta`、`/api/chat/output/complete`、`/api/conversations/{id}/abort` 已有最小实现。
- 已新增更适合后续前端重构的 path-based 控制接口：`/api/conversations/{id}/respond`、`/api/conversations/{id}/stream/delta`、`/api/conversations/{id}/stream/complete`；旧 `/api/chat/output/*` 先作为兼容别名保留。
- `/api/chat/output/complete` 可结束等待中的 pending turn，并把完成结果回传给原始 `/v1/responses` 请求。
- `/api/ws` 已切到真实 WebSocket 广播骨架，可发送 snapshot / conversation_upsert / connection_count 事件。
- 已新增 Go `httptest` 集成测试，覆盖 `responses`、`chat/completions`、`messages` 三套协议的 pending/complete/abort 基础链路。
- `backend/internal/protocol` 已开始承接三套协议的请求提取与完成响应构造，当前已收敛出显式 `Protocol`、`TurnRequest`、`TurnResult`、`ConversationMeta` 和 `PendingStreamEvent`，handler 不再自己分支 Anthropic 流式特殊逻辑；当前 `TurnRequest` 已开始补齐多模态输入部件提取、`tool_choice` 和 `response_format` 解析，完成响应和 Responses/Anthropic SSE 终态事件也已补上统一 `usage` 外壳。这批结构现已继续下推到 store/repository：pending turn 创建时会把 `input_parts`、`tool_choice`、`response_format` 一并写入 `request_debug`，SQLite/PostgreSQL 的 request reader 也会按结构化字段回读。剩余缺口主要在更完整的 reasoning/tool choice 执行语义、chat completion 流式 usage 兼容细节，以及后续让自动化规则/上游辅助直接复用这套结构而不是回退到原始 JSON。
- 当前最小实现已覆盖 `assistant_message`、`thinking`、`tool_call`、`tool_result` 四种人工完成模式，并用集成测试守护消息持久化顺序与三套协议返回外壳。PostgreSQL 路径下也已开始覆盖 `thinking`、`tool_call`、`tool_result` 这些完成模式，确认完成响应和消息 metadata 在两种数据库后端下保持一致。
- 协议层已把非流式和流式都作为一等能力：`stream=false` 或缺省 `stream` 会等待人工/自动完成后返回一次性 JSON，`stream=true` 走真实 SSE 链路；当前已覆盖 OpenAI Responses、Chat Completions、Anthropic Messages 三套协议的非流闭环和最小流式集成测试，并补上了 `tool_call` 的基础流式返回外壳。PostgreSQL 路径下也已补上三套协议的最小非流闭环、基础 SSE 闭环，以及部分完成模式分支测试，确保 pending turn、realtime 和协议 encoder 在两种数据库后端下行为一致。
- pending turn 已补上最小状态机约束：`delta` 会把会话推进到 `streaming`，终态后的 `delta` / `complete` / `abort` 会返回 `409`，并用集成测试守护这些行为。
- `backend/internal/service` 已开始收敛统一的 `TurnControlCommand`，把 WebUI 手工回复、后续应用 API 和自动化规则共享的 turn control 输入模型从 handler 中抽离出来。
- 已补上 `request_id -> conversation_id -> TurnControlCommand` 的最小解析链路，并先用于 `lab` 路由；后续应用 API 的 `/api/app/requests/{request_id}/*` 将直接复用这层能力。
- 已补上统一的 request 读取视图（列表 + 详情）并先用于 `GET /lab/requests`、`GET /lab/requests/{request_id}`；后续 `/api/app/requests` 应直接复用这套 request reader，而不是再单独拼查询结构。
- 已新增 `user_app_api_keys` 的最小存储、哈希校验和应用 API 鉴权中间件；当前已打通 `GET /api/app/me`、`GET /api/app/requests`、`GET /api/app/requests/{request_id}`、`POST /api/app/requests/{request_id}/delta|complete|abort` 的最小链路，并对 scope、`allowed_request_actions`、owner 隔离做了集成测试。
- 已补上应用 API key 的最小管理与审计基础：`GET/POST/DELETE /api/user/app-api-keys` 已可在 Lab actor 和正式 session actor 下工作，未登录访问会明确返回 `401 session required`；`app_api_key_audit_logs` 已开始记录 `/api/app/*` 请求结果，`last_used_at` 也已做最小节流更新。
- 应用 API Key 创建当前已开始做服务端白名单校验：scope 必须来自后端支持集合，且至少选择一个；`resource_limits` 只接受已知 key、已知类型和值域，未知 key/类型会返回 `400`；同时还会校验 resource limit 与 scopes 的语义匹配，例如 `allowed_request_actions` 必须伴随 `requests:respond`，`allowed_request_ids` 必须伴随 `requests:read|requests:respond`，`max_model_keys` 必须伴随 `model_keys:write`，避免生成永远不会生效或语义自相矛盾的 key 配置。
- 当前旧前端 `ApiKeyManagementPanel` 仍请求 `/api/user/api-keys*` 并假设服务端可再次返回完整明文 key；这与 Go 重构版“应用 API Key 只存 hash、明文只在创建时返回一次”的安全边界冲突，因此不应继续为旧接口补明文兼容，后续前端应改接 `/api/user/app-api-keys` 并调整为“一次性展示 + 前缀/元数据列表”模型。
- 应用 API Key 配置契约当前还已开始显式暴露：`GET /api/user/app-api-keys/schema` 会返回后端支持的 scopes、resource limit、依赖 scopes 和枚举值，避免前端或外部管理工具继续把这些规则硬编码在本地。
- 应用 API 当前已覆盖 `requests:read` / `requests:respond` / `conversations:read` 的最小链路：`/api/app/requests*`、`/api/app/conversations`、`/api/app/conversations/{conversation_id}/messages` 均已打通，并对 scope 与 owner 隔离做了集成测试。
- 已新增虚拟模型 API Key 的最小存储、可解密密文保存、管理接口和模型兼容入口鉴权：`GET/POST/DELETE /api/user/model-api-keys` 已可在 Lab actor 和正式 session actor 下工作，未登录访问会明确返回 `401 session required`；`/v1/responses`、`/v1/chat/completions`、`/messages` 等入口在生产模式要求 `Authorization: Bearer sk-...`，Lab 模式仍允许免 key，但如果请求携带有效 `sk-...` 会按该 key 所属用户写入 `owner_id`。
- 用户侧虚拟模型配置已补上最小接口：`GET/POST/DELETE /api/config/models` 当前按 actor 读写 `user_configs.virtual_models`，用于保存当前用户自己的虚拟模型列表；`GET /models` / `GET /v1/models` 也已开始优先返回当前 actor 的已启用虚拟模型，而不是硬编码单个 `chatapi-lab`。若当前用户没有配置模型，则回退到默认 `chatapi-lab`。
- 为兼容当前前端设置面板，`/api/config/models` 当前同时返回 `models: string[]` 和 `items`；前端仍按简单字符串列表工作，后续前端改造时再逐步切到更完整的模型对象结构。
- 为兼容当前前端自动规则面板，`GET/POST /api/config/automation-rules` 已补上用户侧兼容接口，直接按当前 actor 读写自己的自动规则列表，响应格式对齐前端使用中的 `{ ok, rules }`；底层复用与应用 API 同一套 `automation_rules` 存储与校验逻辑，后续前端切到应用 API 时不需要再迁移数据结构。
- 自动化规则 schema metadata 已开始对外暴露：`GET /api/config/automation-rules/schema` 和 `GET /api/app/automation-rules/schema` 当前返回 action 类型、legacy matcher 支持的 `field` / `match_type`，以及 typed condition 列表，供前端和外部程序做前置校验；规则读写接口本身的 JSON 形状本轮保持不变。
- 应用 API 当前已开始覆盖 `model_keys:read` / `model_keys:write` / `model_keys:delete`：`/api/app/model-keys` 可按 scope 和 `allowed_virtual_models` / `allowed_model_key_ids` 管理当前用户自己的虚拟模型 API Key。
- 应用 API 创建虚拟模型 API Key 时已开始支持 `max_model_keys`：按当前用户未撤销虚拟模型 key 数量限制创建，达到上限返回 `403`。
- 应用 API 当前已开始覆盖 `automation:read` / `automation:write`：`GET/PUT /api/app/automation-rules` 可读写当前用户自己的自动化规则，并支持 `allowed_automation_rule_ids` 限制外部程序只能管理指定规则。
- 应用 API 当前已开始覆盖 `statistics:read`：`GET /api/app/statistics/summary` 返回当前用户自己的请求态势摘要，包括总请求数、waiting/streaming/closed/aborted 计数、自动化规则命中数、按模型和状态聚合、最老 pending 等待秒数。
- 应用 API 当前已开始覆盖 `max_requests_per_minute` 资源限制：在单进程内按应用 API Key 做 1 分钟窗口限流，超限返回 `429` 并写入 `app_api_key_audit_logs`，管理员审计聚合视图中显示为 `app_api.request` failure / `rate_limited`。
- 应用 API 当前已开始覆盖 `allowed_source_ips` 资源限制：支持精确 IP 和 CIDR，默认按直连 `RemoteAddr` 判断；配置 `CHATAPI_TRUSTED_PROXIES` 后，仅当直连来源命中可信代理时才读取 `X-Forwarded-For` / `X-Real-IP`；拒绝时返回 `403` 并记录 `source_ip_forbidden`。
- 应用 API Key 创建已开始支持 `expires_at`：用户创建应用 API Key 时可传 RFC3339 过期时间，必须晚于当前时间；过期 key 鉴权返回 `401`。
- 管理员运行时监控已落地最小接口：`GET /api/admin/runtime/summary`、`GET /api/admin/runtime/automation`、`GET /api/admin/runtime/memory`、`GET /api/admin/runtime/system`、`GET /api/admin/runtime/connections`、`GET /api/admin/runtime/queue`、`GET/PUT /api/admin/runtime/settings`、`POST /api/admin/runtime/gc`，仅允许 admin session actor 访问，应用 API Key 和虚拟模型 API Key 不能访问；当前返回 Go runtime、内存、GC、Linux 系统内存/load/disk/RSS/FD、自动化规则命中/失败摘要，以及运行期 `no_rules` / `no_match` 跳过计数、`skip_by_reason` 聚合、`skip_by_rule` 聚合和最近未命中样本、pending turn、realtime subscriber 和数据库观测信息。SQLite 部署会返回主库/WAL 文件大小；PostgreSQL 部署会返回连接池 `max/total/acquired/idle/constructing` 连接数以及 empty/canceled acquire 计数，并支持进程内调整 Go GC 百分比和内存限制。
- Realtime 事件广播已补上最小背压策略：每个订阅者使用固定队列，队列满时记录 recoverable drop，连续满队列会断开慢订阅者并累计 `slow_disconnects`；`/api/admin/runtime/queue` 和 `/metrics` 都会暴露相关计数。
- Realtime 连接限额已补上最小统一池：`CHATAPI_REALTIME_MAX_CONNECTIONS`、`CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER`、`CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER` 控制全局、单用户和浏览器控制台预留名额；当前 `/api/ws` 作为 `webui` 连接接入，service 层已提供 `api` / `sse` lease，后续 API/SSE 长连接必须复用同一套限额，避免同用户 API/SSE 连接占满后 WebUI 进不来。管理员连接监控和 `/metrics` 已输出分类连接数与拒绝计数。
- 管理员存储监控已落地最小接口：`GET /api/admin/storage/summary`、`GET /api/admin/storage/users`、`POST /api/admin/storage/cleanup`、`POST /api/admin/storage/vacuum`，返回数据库观测、uploads 目录大小、按 owner 估算的 conversations/messages 文本与 metadata 占用，以及清理候选预览；当前 cleanup 要求显式传 `dry_run`，`dry_run:true` 只预览，`dry_run:false` 会按同一候选算法删除已关闭/已终止的 conversations 并级联删除 messages，同时跳过 `waiting` / `streaming` 活跃请求并写审计日志；清理会识别候选会话正文和 metadata JSON 中的 `/api/uploads/imgs/{filename}` 引用，删除不再被保留会话引用的本地上传图片文件与 `uploaded_images` 元数据，并返回候选/已删除图片数量和字节数；如果文件系统删除失败，会写入 `storage_file_deletion_failures` 恢复队列并继续清理其他对象。`summary.database` 已与 runtime summary 统一：SQLite 返回主库/WAL 文件大小，PostgreSQL 返回连接池 `max/total/acquired/idle/constructing` 和 empty/canceled acquire 计数。SQLite vacuum 需要显式 `dry_run:false` 才会执行 WAL checkpoint 和 `VACUUM`；PostgreSQL 部署下 `dry_run:true` 仍可用于查看数据库观测，但 `dry_run:false` 会明确返回不支持，而不是伪装成服务端内部错误。每日存储维护 worker 已可通过 `CHATAPI_STORAGE_CLEANUP_ENABLED=1` 启用，按本地时区 `CHATAPI_STORAGE_CLEANUP_TIME=HH:MM` 执行旧会话清理、孤儿图片清理、上传删除失败重试；只有 SQLite 部署才会额外执行 WAL checkpoint 和可选 SQLite VACUUM，避免 PostgreSQL 部署每日产生无意义告警。
- 管理员存储监控已开始把 `uploaded_images` 元数据纳入用户维度估算，`/api/admin/storage/users` 返回每个 owner 的 `image_count`、`image_bytes`、默认配额、单用户 override、最终 `storage_quota_bytes` 和 `storage_over_quota`，summary 的 `estimated_bytes` 也会包含已落库图片字节数。
- 管理员请求态势已落地最小接口：`GET /api/admin/requests/overview`，返回全局请求总数、waiting/streaming/closed/aborted 计数、自动化规则命中数，以及按 owner、model、status 聚合。
- pending turn 过期清理已落地最小版本：新增 `CHATAPI_PENDING_TURN_TTL`，默认 `0` 表示关闭；启用后后台 worker 会定期把超过 TTL 的 `waiting` / `streaming` 会话标记为 `expired`，并让仍在等待的兼容接口请求收到 `request_timeout` 错误响应。
- 通用审计日志已开始落地：SQLite bootstrap 会创建 `audit_logs`，当前已记录图片上传成功/失败、用户创建/删除应用 API Key、用户创建/删除虚拟模型 API Key、自动化规则自动完成命中、管理员手动 GC、管理员运行时设置修改、管理员存储 cleanup dry-run 预览和实际执行；`GET /api/admin/audit/logs` 可查询通用审计日志，并支持 `include_app_api=1` 把应用 API 请求细表按统一审计形态聚合到返回列表。
- 配置诊断命令已落地最小版本：`chatapi doctor [serve|lab]` 复用 `.env` 加载和 config 解析，输出 JSON 诊断报告，并覆盖生产 master key、session secret、默认管理员密码、SQLite serve 降级、Lab 暴露、OIDC 私密 RP 必填项、OIDC redirect 和 scope、realtime 连接预留配置等风险。
- 数据库版本诊断已落地最小版本：SQLite 和 PostgreSQL bootstrap 都会维护 `db_meta` 和 `schema_migrations`；当前 `chatapi db check` 已能通过统一 store 打开两种数据库，输出 schema version、dirty 状态、创建来源、最近迁移时间、已应用迁移列表，以及 SQLite 主库/WAL/SHM 文件存在性和字节数。
- 基础运维命令已落地最小版本：`chatapi version` 输出 JSON 版本信息；`chatapi config print --redact [serve|lab]` 输出最终配置的脱敏 JSON，master key、session secret、管理员密码、Lab token/password、OIDC client secret 和非 SQLite DSN 不会明文输出；`chatapi migrate up|status|down --force` 已开始同时支持 SQLite 和 PostgreSQL 的当前 schema 状态查询与本地重置，`chatapi migrate-db sqlite-to-postgres --sqlite <path> --postgres <dsn>` 已开始支持 SQLite 到 PostgreSQL 的最小全量搬迁。当前 SQLite 和 PostgreSQL 都已开始进入注册式增量 migration：SQLite 的首个 `0002_sqlite_app_api_indexes` 已补上应用 API Key 相关索引，PostgreSQL 的首个 `0002_postgresql_request_indexes` 已补上 `messages.response_id` 和 `request_debug.request_id` 读取相关索引。`chatapi oidc test` 可拉取 OIDC discovery document 并校验 issuer 与核心 endpoint。
- SMTP-only 邮件基础能力已落地最小版本：配置项只保留 `CHATAPI_SMTP_*`，`chatapi smtp test --dry-run` 可离线检查 SMTP 配置，`chatapi smtp test --connect-only` 可执行 SMTP 连接/TLS/Auth 握手但不发信，`chatapi smtp test --to user@example.com` 才会真实发送测试邮件；配置输出和诊断不会打印 SMTP password。
- 健康检查已补齐部署探针分层：`GET /api/health` 保持轻量 DB ping，`GET /api/ready` 检查数据库和 migration 状态；当数据库不可用或 `migration_dirty=true` 时 ready 返回 `503`。
- `/metrics` 已落地最小 Prometheus 文本端点，默认关闭；仅当 `CHATAPI_METRICS_ENABLED=1` 时注册，当前输出 HTTP 请求数/状态码/耗时、Go runtime、自动化命中/失败计数、运行期 `no_rules` / `no_match` 跳过计数、按 reason 聚合的 `chatapi_automation_rule_skips_total{reason=...}`、pending turn、realtime 队列，以及 SQLite 文件大小或 PostgreSQL 连接池指标等基础指标。
- SQLite bootstrap schema 已补齐用户体系基础表：`users`、`user_identities`、`user_configs`、`config`，并已补上 `users` / `user_identities` / `config` / `user_configs` 的 SQLite 仓储基础方法和 repository 测试。当前业务仍使用 Lab actor 和 `.env` admin session；这些表和仓储先作为后续 OIDC、本地用户、管理员用户管理、用户配置和系统配置的稳定落点。
- PostgreSQL repository 已补到第三批业务表：新增 `internal/repository/postgresql`，使用 `github.com/jackc/pgx/v5/pgxpool`，并为 `users`、`user_identities`、`config`、`user_configs`、`user_app_api_keys`、`app_api_key_audit_logs`、`user_api_keys`、`audit_logs`、`automation_rules`、`uploaded_images`、`storage_user_quotas`、`storage_file_deletion_failures`、`conversations`、`messages` 以及 pending turn 状态机相关方法提供真实实现和可选 contract tests；测试通过 `CHATAPI_PG_TEST_DSN` 启用，未配置时跳过。当前 `chatapi serve`、`/api/ready`、`chatapi db check`、`chatapi migrate up|status|down --force` 已开始通过统一 runtime store 接入 PostgreSQL；PostgreSQL 侧也已开始支持 bootstrap 后继续应用注册式增量 migration，首个 `0002_postgresql_request_indexes` 会补上请求读取相关索引。SQLite 和 PostgreSQL 的 migration 定义现已开始按数据库拆到包内 `sql/*.up.sql` 文件，并通过共享的 `internal/repository/migrationplan` 统一做文件名解析、顺序校验和 SQL 装载，避免两套后端继续手写重复的 Go 内联 SQL 切片。`chatapi migrate-db sqlite-to-postgres` 已落地首版：当前会先确认目标 PostgreSQL 业务表为空，再把 SQLite 中的 users、OIDC identities、system/user configs、虚拟模型 API Key、应用 API Key、应用 API Key 审计日志、通用审计日志、自动化规则、上传图片元数据、存储配额、上传删除失败队列、conversations 和 messages 一次性迁入 PostgreSQL。HTTP 集成测试已补到 `/metrics`、`/api/admin/runtime/*`、`/api/admin/storage/*` 的最小 PostgreSQL 路径，以及 `responses` / `chat/completions` / `messages` 三套协议在 PostgreSQL 下的非流闭环和基础 SSE 闭环。剩余缺口主要在更完整的 PostgreSQL 集成测试矩阵，以及后续把迁移工具从“全量快照装载”演进为更适合超大库的分批流式搬迁。
- 密码哈希和本地 users 表登录基础已落地：新增 `internal/platform/password`，新密码使用 Argon2id PHC 风格格式，旧 `salt$sha256(salt+password)` 可验证并返回 `NeedsUpgrade`。serve 模式 `POST /api/auth/login` 会优先按 username/email 查询 `users` 表，验证本地密码，成功后建立同一类 session；旧 hash 登录成功后会自动升级为 Argon2id 并更新 `last_login_at`。`.env` 的 `CHATAPI_ADMIN_PASSWORD` 仍保留为 `admin` 用户恢复入口。
- 管理员用户管理已落地最小接口：`GET /api/admin/users` 列出本地用户，`POST /api/admin/users` 创建本地用户并写入 Argon2id 密码 hash，`PUT /api/admin/users/{user_id}/password` 重置密码，`DELETE /api/admin/users/{user_id}` 当前实现为停用用户而非物理删除，保留历史请求、上传、API key 等 owner 归属；这些接口仅允许 admin session 访问，并写入 `admin.user` 审计事件。
- 管理员用户历史最小接口已补上：`GET /api/admin/users/{user_id}/history?limit=30` 当前按会话 `metadata.owner_id` 聚合该用户最近消息，返回 `recent_messages` 供前端详情弹窗直接展示；首版先在 service 层复用 `ListConversations + ListMessages` 做排序截断，后续如果单用户消息量过大，再下推为 repository 级定制查询。
- 用户配置已落地最小接口：`GET /api/user/config` 返回当前 actor 的 `user_configs` 列表和聚合 config map，`POST /api/user/config` 按 key upsert JSON object 配置并写入 `user.config` 审计事件；Lab actor 和 serve session actor 都复用同一套 owner 隔离。
- 系统配置已落地最小管理接口：`GET /api/admin/config` 返回 `config` 表列表和聚合 config map，`POST /api/admin/config` 按 key upsert JSON object 配置并写入 `admin.config` 审计事件；当前只负责持久化和管理，不会自动覆盖 `.env` 派生的运行时配置，后续各服务再逐步读取对应 key。
- 面向当前前端系统设置面板的最小接口也已补上：`GET/POST /api/config/system` 当前要求 admin actor，读写 `config.system_settings` 单条 JSON，并把前端已使用的字段展平成固定响应结构；`realtime_*`、图片大小上限和 SMTP provider 选项会优先回显当前运行时配置，其他字段先作为可持久化设置保留，后续再逐步接入真实运行时行为。
- 管理员 SMTP 测试邮件接口已补上：`POST /api/admin/send-test-email` 当前直接读取 `.env` 派生的 SMTP 运行时配置发送测试邮件，不读取数据库里的 provider 凭据；配置缺失或 SMTP 禁用时返回 `400`，真实发送失败返回 `502`，并记录 `admin.email / send_test_email` 审计事件。这样前端系统设置页可以验证“当前进程实际生效的 SMTP 配置”，而不是仅验证持久化设置值。
- 用户侧改密最小接口已补上：`POST /api/user/password` 当前按 actor 更新本地 `users.password_hash`，用于前端设置页的“重置密码”表单；这条链路只负责已登录用户的本地密码更新，不包含邮箱找回、验证码或 TOTP 二次确认，后续正式账号恢复流程再单独补齐。
- Upload/Image Store 已落地最小兼容接口：`POST /api/uploads/imgs` 使用服务端生成文件名、内容嗅探和大小限制写入 `data/uploads/imgs`，并写入 `uploaded_images` 元数据表记录 owner、原始文件名、MIME、字节数和访问 URL；`GET /api/uploads/imgs/{filename}` 使用严格文件名白名单和根目录校验读取图片；`GET /api/uploads/imgs/usage` 返回文件数与字节数；`CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES` 可先按 owner 已上传图片字节数阻断新图片上传；管理员可通过 `PUT/DELETE /api/admin/storage/users/{owner_id}/quota` 设置或恢复单用户配额覆盖；`GET /api/admin/storage/orphans` 可 dry-run 预览无元数据的孤儿图片，`POST /api/admin/storage/orphans/cleanup` 可在显式 `dry_run:false` 后删除这些孤儿文件并写审计日志。
- 本地 session 已落地最小版本：serve 模式下 `POST /api/auth/login` 优先验证 `users` 表本地账号，失败后允许 `admin` 使用 `.env` 的 `CHATAPI_ADMIN_PASSWORD` 作为恢复入口，成功后用独立 `CHATAPI_SESSION_SECRET` 写入 HMAC 签名 HttpOnly cookie；`GET /api/auth/session` 可读取当前 actor，`POST /api/auth/logout` 会清除 cookie；管理员接口已可通过 session actor 访问，应用 API Key 和虚拟模型 API Key 仍不能访问管理员后台。session 认证的非 GET `/api/*` 请求已执行 Origin/Referer 同源校验，Lab actor 和 API Key 请求不走 CSRF。`chatapi setup` 已生成 `CHATAPI_SESSION_SECRET`；如果老部署未配置，serve 启动会生成随机 session secret 并持久化到 `config` 表的 `security.session_secret`，Lab 使用进程内不安全默认值且不持久化。本地管理员登录已补上最小进程内失败限流，连续失败后返回 `429` 并写审计事件；后续多实例部署应迁移到 Redis 或数据库限流器。
- 注册与邮箱找回密码最小闭环已补上：`GET /api/auth/register/config` / `POST /api/auth/register/send-code` / `POST /api/auth/register` 和 `GET /api/auth/password/config` / `POST /api/auth/password/send-code` / `POST /api/auth/password/reset` 已可工作。当前系统会从 `config.system_settings` 读取 `external_registration_enabled`、`email_verification_enabled`、注册邮箱域名限制等配置；密码找回是否可用取决于当前进程 SMTP 配置是否有效。邮箱验证码当前通过新增 `auth_verification_codes` 表持久化，按 `email + purpose` upsert，验证码本体不落库，只保存基于 master key 的 hash；注册成功后创建本地 `users` 记录，找回密码成功后更新 `users.password_hash`。当前已先补上单邮箱/单用途 60 秒发送限流和 5 次错误尝试上限，超过后必须重新发码；GeeTest 验证和后台清理任务仍待继续补齐。
- TOTP 双因素认证已补上最小闭环：`GET /api/auth/totp/setup` 生成新的 TOTP secret、otpauth URI 和二维码 PNG base64；`POST /api/auth/totp/confirm` 校验验证码后启用；`POST /api/auth/totp/reset` 删除当前用户的 TOTP 配置。启用后，本地账号和 `.env` 恢复管理员登录都会在密码正确后要求额外提交 `totp` 字段；缺失或错误时返回 `401` 和 `totp_required=true`，前端可直接切出验证码输入框。TOTP secret 当前使用服务端 master key 加密后保存在 `user_configs.security.totp`，而通用 `/api/user/config` 会过滤 `security.*` 保留键，避免敏感配置回显到普通设置接口。
- OIDC RP 登录已落地最小闭环：新增 `GET /api/auth/oidc/config`、`GET /api/auth/oidc/login`、`GET /api/auth/oidc/callback`，使用 `golang.org/x/oauth2` 和 `github.com/coreos/go-oidc/v3/oidc` 走 Authorization Code Flow + PKCE，校验 state、nonce、ID Token issuer/audience/expiry/signature；callback 会在有 access token 时拉取 UserInfo 补充缺失 claims，并要求 UserInfo `sub` 与 ID Token `sub` 一致；登录成功后创建同一类 ChatAPI session。当前按 `CHATAPI_OIDC_ALLOWED_DOMAINS` / `CHATAPI_OIDC_ALLOWED_EMAILS` 做 allowlist，allowlist 命中和自动关联已有本地邮箱都要求 `email_verified=true`；`CHATAPI_OIDC_AUTO_CREATE_USER=1` 时可自动创建本地用户并 upsert `user_identities`，命中 `CHATAPI_OIDC_ADMIN_EMAILS` 且 `email_verified=true` 时同步为 admin；管理员邮箱列表变化后，下次 OIDC 登录会降级非本地显式管理员。用户侧 OIDC 身份查看/解绑已落地：`GET /api/user/identities` 列出当前用户身份，`DELETE /api/user/identities/{identity_id}` 只能解绑当前用户自己的身份，并阻止无本地密码用户删除最后一个登录方式。当前 `httptest` 已补上 OIDC callback 成功链路和 UserInfo `sub` 不一致拒绝分支，覆盖本地测试 provider discovery、token exchange、JWKS 验签、UserInfo merge、管理员邮箱映射和 session 建立；后续仍需补绑定发起流程和更细审计 metadata。
- `owner_id` 的来源已不再直接硬编码在业务层；当前通过统一的 `RequestActor` 上下文注入 Lab actor、app api principal 和 virtual model key principal，后续接 session、OIDC 用户时只需要继续往同一个 actor 上下文注入即可。
- `/api/user/*` 已开始收敛到统一的“交互式用户 actor”边界：当前只允许 `lab`、本地 session、OIDC session 访问；应用 API Key 与虚拟模型 API Key 即使携带同一 `user_id`，也不能直接调用用户设置、用户密钥、用户身份等交互式路由，避免程序化凭据越权复用 WebUI 用户接口。

第一阶段完成后，再按模块补齐认证、会话、pending turn、协议兼容、自动化规则、管理后台和 PostgreSQL 仓储。

### 1.1 必须保持的产品能力

- 保持现有 Web 控制台可用：登录、注册、TOTP、会话列表、消息查看、人工输出、自动化规则、系统设置、用户设置、API Key 管理、统计页、图片上传。
- 工作台 Tool Call 标签页支持“请求大模型”辅助按钮，可调用用户预配置的上游模型生成说明文字并填写 Tool Call 表单，但不自动发送。
- 用户除虚拟模型 API Key 外，还可以创建应用 API Key，用于外部自动化读写自己的自动化规则、查看收到的请求、提交回复、终止请求和管理自己的虚拟模型 API Key。
- 支持可选 OAuth 2.0 / OpenID Connect 登录接入，ChatAPI 作为私密 Relying Party，由 `.env` 配置客户端密钥。
- 保持兼容协议入口：
  - `GET /models`
  - `GET /v1/models`
  - `POST /responses`
  - `POST /v1/responses`
  - `POST /chat/completions`
  - `POST /v1/chat/completions`
  - `POST /messages`
  - `POST /v1/messages`
- 保持人工接管的核心语义：客户端请求进入等待态，Web 控制台可以追加 delta、完成输出、终止等待，会话状态实时同步。
- 保持 SQLite 单文件体验用于 Lab 模式、本地开发、测试和轻量单机部署；生产部署推荐 PostgreSQL。
- 保持一键本地调试：单二进制 + SQLite + 静态前端目录即可运行；正式部署提供 PostgreSQL 配置和迁移路径。

### 1.2 工程化目标

- 后端从脚本式 Flask 应用迁移为分层 Go 服务，明确 handler、service、repository、protocol、infra 边界。
- 使用显式 schema migration 管理数据库结构，禁止在运行时代码里散落 `CREATE TABLE IF NOT EXISTS` 作为长期演进方案。
- 提供稳定的配置加载、日志、健康检查、指标、测试、CI、发布产物和 Docker 镜像。
- 将协议兼容能力沉淀为独立包，避免 OpenAI Responses、Chat Completions、Anthropic Messages 的转换逻辑散落在 handler 中。
- 将安全能力作为默认工程约束：密码哈希升级、会话安全、CSRF、CORS、SSRF 防护、上传限制、限流、审计日志。

### 1.3 非目标

- 首个 Go 版本不重写前端。
- 首个 Go 版本不要求 Lab 模式使用 PostgreSQL。
- 首个 Go 版本不追求多实例完全无状态横向扩容；但正式部署路径应优先面向 PostgreSQL，并为后续 Redis 扩展留出接口。
- 首个 Go 版本不改变公开 API 的响应字段命名和错误结构，除非是修复明确的安全问题。

## 2. 核心概念

### 2.1 虚拟模型

“虚拟模型”指 ChatAPI 对外模拟出来的模型服务。外部 Agent、OpenAI SDK、Anthropic SDK 调用 ChatAPI 的 `/v1/responses`、`/v1/chat/completions`、`/messages` 时，以为自己在调用一个模型；实际上请求会进入 ChatAPI 的 pending turn，由真人、自动化规则或调试工具完成回复。

虚拟模型相关概念：

- 虚拟模型接口：`/v1/responses`、`/v1/chat/completions`、`/messages` 等兼容入口。
- 虚拟模型 API Key：ChatAPI 发放的 `sk-...`，存于 `user_api_keys`，用于认证外部 Agent 对虚拟模型接口的调用；生产模式使用可解密密文存储，便于用户回看和复制，Lab 模式可使用临时 key 或免 key。
- 虚拟模型名称：用户在 ChatAPI 中配置并暴露给外部客户端的模型名，例如自定义的 `model` 列表。

虚拟模型不代表 ChatAPI 背后一定有真实 LLM。它的核心是“人类或自动化系统作为模型响应者”。

### 2.2 上游模型

“上游模型”指用户自己配置的真实 LLM 服务，例如 OpenAI-compatible 服务、Anthropic-compatible 服务或其他兼容实现。ChatAPI 可以在工作台中调用上游模型辅助用户，但上游模型不是 ChatAPI 对外暴露的虚拟模型。

上游模型相关概念：

- 上游模型接口：用户配置的 `base_url` 和协议类型，可能是 Responses、Chat Completions 或 Anthropic Messages。
- 上游模型 API Key：用户配置给真实 LLM 服务的 key，默认只保存在浏览器本地，用于“请求大模型”辅助功能，不上传到 ChatAPI 服务端。
- 上游模型名称：真实 LLM 服务侧的 `model` 参数。

上游模型只能辅助填写工作台表单，例如 Tool Call 草稿。它不能自动替用户提交回复，也不属于应用 API Key 的 `model_keys:*` 管理范围。

### 2.3 Key 类型边界

- 虚拟模型 API Key：`sk-...`，让外部 Agent 调用 ChatAPI 的虚拟模型接口。
- 应用 API Key：`ak-...`，让外部自动化程序调用 `/api/app/*` 管理自己的 ChatAPI 工作台资源。
- 上游模型 API Key：真实 LLM 服务的 key，默认只用于浏览器端直连上游模型。

这三类 key 必须在存储、权限、路由和 UI 文案上分开，避免用户误把上游模型 key 当成 ChatAPI key，或误把应用 key 当成虚拟模型 key。

推荐 WebUI 分成三个独立配置区：

- 虚拟模型 API Key：管理 ChatAPI 发放的 `sk-...`，用于外部 Agent 调用虚拟模型接口。
- 应用 API Key：管理 ChatAPI 发放的 `ak-...`，用于外部自动化调用 `/api/app/*`。
- 上游模型：管理浏览器本地保存的真实 LLM `base_url`、协议、模型名和 key，用于 Tool Call 辅助。

### 2.4 Lab 模式

“Lab 模式”是 ChatAPI 的本地实验室/调试模式，面向开发 Agent、调试 SDK 请求、演示协议兼容和手动构造流式响应。

Lab 模式特征：

- 通过显式命令启动，例如 `chatapi lab`。
- 默认使用 SQLite，不要求 PostgreSQL。
- 默认跳过登录、注册、OIDC、TOTP、API Key 管理、管理员后台等完整生产流程。
- 启动后自动打开浏览器，直接进入请求工作台。
- 浏览器打开后即可查看收到的虚拟模型请求体、手动输出 delta、complete、abort，并复制 curl。
- 只注册 Lab 路由和虚拟模型兼容入口，不注册生产管理接口。

Lab 模式不是生产免鉴权开关，不能通过 `.env` 或 `chatapi serve` 开启。

## 3. 当前后端边界

现有后端是 Flask 应用，主要模块如下：

- 应用入口：`backend/app.py`、`backend/main.py`
- 配置：`backend/core/config.py`
- 鉴权：`backend/core/auth.py`
- 数据仓库：
  - `backend/repositories/conversations.py`
  - `backend/repositories/users.py`
  - `backend/repositories/system_config.py`
- 业务服务：
  - pending turn：等待人工回复的请求生命周期
  - response stream：SSE 流式输出
  - realtime broker：WebSocket 同步会话和连接状态
  - automation rules：自动回复规则
  - image assets：图片上传、归属、清理
  - email：SMTP
  - csrf、rate limit、ntfy、url safety
- 路由：
  - `/api/auth/*`
  - `/api/admin/*`
  - `/api/user/*`
  - `/api/conversations/*`
  - `/api/chat/output/*`
  - `/api/config/*`
  - `/api/uploads/*`
  - `/api/statistics/*`
  - OpenAI/Anthropic 兼容接口

Go 迁移必须先复刻这些边界，再逐步提高内部质量。

## 4. 目标架构

### 3.1 推荐分层

```text
backend/
  cmd/chatapi/
    main.go

  internal/app/
    app.go               # 组装配置、日志、数据库、路由、中间件、后台任务

  internal/config/
    config.go            # env/.env 加载、默认值、校验、敏感配置脱敏

  internal/http/
    router.go            # 路由注册
    middleware/          # auth、csrf、cors、request id、recover、logging、rate limit
    handlers/            # 只处理 HTTP 输入输出，不放复杂业务

  internal/domain/
    auth/
    conversation/
    turn/
    realtime/
    automation/
    config/
    upload/
    notification/
    statistics/

  internal/repository/
    sqlite/
    postgresql/
    migrations/

  internal/protocol/
    openairesponses/
    chatcompletions/
    anthropicmessages/
    sse/

  internal/platform/
    email/
    ntfy/
    storage/
    clock/
    idgen/
    password/
    session/

  internal/observability/
    log.go
    metrics.go
    health.go

  web/
    dist/                # 可选，发布包内嵌或外置
```

### 3.2 依赖方向

- `handlers` 依赖 `domain service`，不直接拼 SQL。
- `domain service` 依赖接口，不依赖具体数据库实现。
- `repository/sqlite` 和 `repository/postgresql` 分别实现 repository 接口。
- `protocol` 包负责请求规范化和响应构建，业务服务只处理统一后的 turn model。
- `platform` 放外部系统适配，例如邮件、ntfy、文件存储、密码哈希、session codec。
- `config` 和 `observability` 可被应用层和基础设施层依赖，避免反向依赖。

### 3.3 核心运行模型

Go 服务启动后应组装以下组件：

1. 加载配置和 `.env`。
2. 初始化结构化日志和 request id。
3. 按配置打开数据库连接池；SQLite 设置 WAL/foreign keys/busy timeout，PostgreSQL 设置连接池和 statement timeout。
4. 执行 schema migration。
5. 初始化管理员用户和 session secret。
6. 初始化 repository。
7. 初始化 pending turn manager、realtime hub、rate limiter、image store、automation engine。
8. 注册 HTTP 路由和中间件。
9. 启动后台清理任务：过期 pending turn、孤儿图片、可选会话统计预聚合。
10. 监听 HTTP/HTTPS，支持优雅关闭。

## 5. 技术选型

### 4.1 Go 版本

- 推荐 Go `1.23+`。
- 原因：保持较新的标准库能力和工具链体验，同时对自托管用户足够友好。
- 发布时提供 Linux amd64/arm64、Darwin amd64/arm64、Windows amd64 产物。

### 4.2 HTTP 框架

推荐：标准库 `net/http` + `chi`。

- 依赖：`github.com/go-chi/chi/v5`
- 原因：
  - API 小，长期维护稳定。
  - 中间件组合清晰。
  - 与标准库、SSE、WebSocket、静态文件服务集成成本低。
  - 对开源项目来说比重型框架更容易维护。

不推荐首选 Gin/Fiber：

- Gin 可用但 handler/context 风格更框架化，长期迁移到标准库接口成本更高。
- Fiber 基于 fasthttp，与标准库生态不完全一致，SSE/WebSocket/中间件适配要更谨慎。

### 4.3 数据库和迁移

数据库需要同时支持 SQLite 和 PostgreSQL，但定位不同：

- SQLite：默认用于 `chatapi lab` / Lab 模式、本地开发、测试、轻量单机部署和旧 Flask 数据迁移入口。
- PostgreSQL：推荐用于正式部署、长期运行、多用户、较高并发和需要更稳定运维能力的场景。

推荐依赖：

- SQLite 驱动：`modernc.org/sqlite`
- SQLite 备选：`github.com/mattn/go-sqlite3`
- PostgreSQL 驱动/连接池：`github.com/jackc/pgx/v5`
- migration：`github.com/golang-migrate/migrate/v4`

SQLite 选择 `modernc.org/sqlite` 的理由：

- 纯 Go，交叉编译和开源用户本地构建更省事。
- 适合 Lab 模式单二进制体验。

PostgreSQL 选择 `pgx` 的理由：

- Go 生态事实标准之一，连接池、context、类型支持成熟。
- 更适合正式部署的并发写入、连接治理、备份恢复和外部运维。

配置建议：

- `CHATAPI_DB_DRIVER=sqlite|postgres`
- `CHATAPI_DB_PATH=./data/chatapi.sqlite3`
- `CHATAPI_DATABASE_URL=postgres://user:pass@host:5432/chatapi?sslmode=require`
- `CHATAPI_DB_MAX_OPEN_CONNS`
- `CHATAPI_DB_MAX_IDLE_CONNS`
- `CHATAPI_DB_CONN_MAX_LIFETIME_SECONDS`
- `CHATAPI_DB_STATEMENT_TIMEOUT_SECONDS`

默认行为：

- `chatapi lab` 默认 `CHATAPI_DB_DRIVER=sqlite`，使用临时或指定 `--data-dir` 下的 SQLite 文件。
- `chatapi serve` 如果未配置 `CHATAPI_DATABASE_URL`，可以回退到 SQLite 以保留轻量部署能力，但文档应明确生产推荐 PostgreSQL。
- Docker Compose 正式示例应默认包含 PostgreSQL；本地 lab compose 可以单独提供 SQLite 示例。
- `serve` 使用 SQLite 时应在启动日志和管理员后台显示降级提示；当用户数、并发连接数、pending turn 数、WAL 大小或数据库文件大小超过阈值时，提示迁移到 PostgreSQL。

需要验证的风险：

- 在高并发写入、WAL、busy timeout、长连接 SSE 场景下做压力测试。
- 如果遇到兼容或性能问题，再切换到 `mattn/go-sqlite3` 并在发布文档里说明 CGO 要求。
- PostgreSQL 和 SQLite 的 SQL 差异必须通过 repository 测试矩阵覆盖，不能让业务层依赖某个数据库的私有行为。

SQLite 连接建议：

- `SetMaxOpenConns(1)` 或小连接数起步，避免 SQLite 写锁争用。
- 启动时执行：
  - `PRAGMA journal_mode=WAL`
  - `PRAGMA foreign_keys=ON`
  - `PRAGMA busy_timeout=30000`
  - `PRAGMA synchronous=NORMAL`

PostgreSQL 连接建议：

- 设置合理的 `MaxConns`、`MinConns`、连接生命周期和 idle timeout。
- 使用事务隔离级别 `READ COMMITTED` 起步，必要时对关键状态转换使用行锁或乐观锁。
- pending turn 的状态变更必须短事务完成，长连接 SSE/WebSocket 不能持有数据库事务。
- 生产部署必须提供备份、恢复和 migration 操作说明。

### 4.4 配置

推荐：

- `github.com/joho/godotenv` 读取 `.env`
- 自写 typed config struct + env parser，不引入复杂配置框架

必须保持兼容的配置项：

- `CHATAPI_ENV_FILE`
- `CHATAPI_ADMIN_USERNAME`
- `CHATAPI_ADMIN_PASSWORD`
- `CHATAPI_SESSION_SECRET`
- `CHATAPI_DB_DRIVER`
- `CHATAPI_DATA_DIR`
- `CHATAPI_DB_PATH`
- `CHATAPI_DATABASE_URL`
- `CHATAPI_DB_MAX_OPEN_CONNS`
- `CHATAPI_DB_MAX_IDLE_CONNS`
- `CHATAPI_DB_CONN_MAX_LIFETIME_SECONDS`
- `CHATAPI_DB_STATEMENT_TIMEOUT_SECONDS`
- `CHATAPI_HOST`
- `CHATAPI_PORT`
- `CHATAPI_CORS_ORIGINS`
- `CHATAPI_WEB_DIST_DIR`
- `CHATAPI_TLS_CERT_FILE`
- `CHATAPI_TLS_KEY_FILE`
- `CHATAPI_OIDC_ENABLED`
- `CHATAPI_OIDC_PROVIDER_NAME`
- `CHATAPI_OIDC_ISSUER_URL`
- `CHATAPI_OIDC_CLIENT_ID`
- `CHATAPI_OIDC_CLIENT_SECRET`
- `CHATAPI_OIDC_REDIRECT_URL`
- `CHATAPI_OIDC_SCOPES`
- `CHATAPI_OIDC_ALLOWED_DOMAINS`
- `CHATAPI_OIDC_ALLOWED_EMAILS`
- `CHATAPI_OIDC_ADMIN_EMAILS`
- `CHATAPI_OIDC_AUTO_CREATE_USER`
- SMTP、Geetest 相关配置

### 4.5 日志和观测

推荐：

- 日志：标准库 `log/slog`
- request id：自定义中间件或 `github.com/go-chi/chi/v5/middleware`
- 指标：`github.com/prometheus/client_golang`

日志等级：

- `trace`：极细粒度调试信息，例如协议 adapter 中间结构、状态机尝试转换；默认关闭，只在本地排查时开启。
- `debug`：开发调试信息，例如配置加载结果、路由注册、上游辅助解析失败详情；生产默认关闭。
- `info`：正常运营事件，例如服务启动、migration 完成、用户登录成功、请求完成摘要。
- `warn`：可恢复异常或风险，例如 SQLite 生产降级提示、OIDC allowlist 拒绝、慢客户端断开、存储配额接近上限。
- `error`：请求失败、数据库错误、邮件发送失败、上游代理失败、状态机异常拒绝等需要排查的问题。
- `fatal`：服务无法继续启动或运行，例如 migration dirty、数据库不可用、必要密钥缺失；记录后进程退出。
- `audit`：安全和运营审计事件，可以作为独立日志 channel 或结构化字段 `event_type=audit` 输出；不受普通 debug/info 过滤影响。

配置建议：

- `CHATAPI_LOG_LEVEL=info|debug|trace|warn|error`
- `CHATAPI_LOG_FORMAT=json|text`
- `CHATAPI_AUDIT_LOG_ENABLED=1`
- `CHATAPI_AUDIT_LOG_PATH=./data/audit.log`

日志约束：

- 生产默认 `info`，Lab 模式默认 `debug`，测试可用 `trace`。
- 所有日志必须带 `request_id`；用户相关日志带 `user_id`、`key_prefix`、`route`、`status`，避免记录完整 key、cookie、Authorization、上游 API Key、密码、OIDC code/token。
- 请求体默认不进日志；需要调试协议时只在 Lab 模式或显式 `trace` 下记录脱敏后的片段和大小。
- fatal/error 日志应给出可操作原因，例如缺失哪个 env、哪个 migration dirty、哪个目录无权限。

必须提供：

- `GET /api/health`：轻量健康检查，返回 `{ "ok": true, "title": "..." }`
- `GET /api/ready`：可选，检查数据库可用性和 migration 状态
- `GET /metrics`：默认关闭或仅管理员/内网开启，避免公开泄露运行信息

当前 Go 重构分支已落地：

- `GET /api/health`：执行轻量数据库 ping，返回 `ok`、运行模式和数据库 driver。
- `GET /api/ready`：执行数据库 ping 并读取 migration 状态，返回 `database` 与 `migration` 分项；数据库不可用、migration 状态不可读或 dirty 时返回 `503`。
- `GET /metrics`：当前已实现基础 Prometheus 文本输出，默认关闭；通过 `CHATAPI_METRICS_ENABLED=1` 开启。已覆盖 HTTP 请求数、状态码、耗时 sum/count、Go runtime、pending turn、realtime 和 SQLite 文件大小。后续正式部署文档应要求只暴露给内网 Prometheus 或反向代理鉴权后的管理员。

关键指标：

- HTTP 请求数、延迟、状态码
- 当前 pending turn 数
- 当前 WebSocket/SSE 连接数
- 当前浏览器控制台 WebSocket 连接数、API/SSE 连接数、被拒绝连接数
- 数据库查询错误数
- 自动化规则命中数
- 上传图片容量
- 邮件发送成功/失败数
- 进程内存、Go heap、goroutine 数、GC 次数和停顿时间
- 数据库大小、SQLite WAL 或 PostgreSQL 连接池状态、uploads 大小、每用户估算存储占用

### 4.6 Session、Cookie、CSRF

推荐：

- Session cookie：`github.com/gorilla/sessions`
- Cookie 存储或 SQLite session 存储二选一，首版推荐签名 cookie + 服务端 session secret。

约束：

- Cookie `HttpOnly=true`
- `SameSite=Lax`
- 生产 HTTPS 时 `Secure=true`
- 所有 session 认证的非 GET `/api/*` 请求执行 Origin/Referer 校验，保持现有 CSRF 语义。
- API Key 请求不依赖 Cookie，不走 CSRF 校验。
- 当前 Go 重构分支已先实现 session actor 的 Origin/Referer 同源校验：`Origin` 优先，缺失时回退 `Referer`；允许与当前请求 origin 或 `CHATAPI_BASE_URL` origin 相同；跨站或缺失来源的 session mutation 返回 `403 csrf origin check failed`。

### 4.7 OAuth 2.0 / OIDC 登录

Go 重构版支持可选 OAuth 2.0 / OpenID Connect 登录，定位是 ChatAPI 作为私密 RP 接入外部 IdP，而不是在 ChatAPI 内实现 OAuth/OIDC Provider。

推荐依赖：

- OAuth 2.0：`golang.org/x/oauth2`
- OIDC：`github.com/coreos/go-oidc/v3/oidc`

私密 RP 约束：

- `client_secret` 只允许从 `.env` / 进程环境变量读取，不写入数据库，不通过管理接口返回，不暴露给前端。
- 默认使用 Authorization Code Flow。
- 如 IdP 支持，优先启用 PKCE；即使是 confidential client，也可以保留 PKCE 作为额外保护。
- 必须校验 `state`，OIDC 场景必须校验 `nonce`。
- 必须校验 ID Token 的 issuer、audience、expiry、signature。
- callback 只接受服务端配置的 redirect URL，禁止从请求参数动态指定回跳地址。
- 生产环境必须使用 HTTPS redirect URL，本地开发允许 `http://localhost`。

建议路由：

- `GET /api/auth/oidc/config`：返回是否启用、登录按钮文案、provider 名称，不返回 secret。
- `GET /api/auth/oidc/login`：创建 state/nonce/PKCE，写入短期临时 cookie 或服务端临时存储，重定向到 IdP。
- `GET /api/auth/oidc/callback`：校验 state/nonce/code，换 token，校验 ID Token，绑定或创建本地用户，建立 ChatAPI session。
- `GET /api/user/identities`：列出当前用户已绑定的外部身份。
- `DELETE /api/user/identities/{identity_id}`：解除当前用户的外部身份绑定，不删除本地用户；如果这是无本地密码账号的最后一个登录方式，必须拒绝。

用户绑定策略：

- 新增 `user_identities` 表保存外部身份绑定：
  - `id`
  - `user_id`
  - `provider`
  - `issuer`
  - `subject`
  - `email`
  - `email_verified`
  - `display_name`
  - `created_at`
  - `updated_at`
- 唯一约束：`(issuer, subject)`。
- 如果已有 identity，直接登录对应用户。
- 如果没有 identity：
  - `CHATAPI_OIDC_AUTO_CREATE_USER=1` 时，按 allowlist 校验后创建本地用户并绑定。
  - `CHATAPI_OIDC_AUTO_CREATE_USER=0` 时，只允许绑定到已登录本地账号，或由管理员预创建并绑定。
- 不建议仅凭 email 自动登录已有账号；如果要支持，必须同时要求 `email_verified=true` 且 email 命中 allowlist，并记录审计日志。

访问控制：

- `CHATAPI_OIDC_ALLOWED_DOMAINS` 限制邮箱域名，逗号分隔。
- `CHATAPI_OIDC_ALLOWED_EMAILS` 限制具体邮箱，逗号分隔。
- 两者都为空时，仅表示不做邮箱 allowlist；是否允许开放注册仍由 `CHATAPI_OIDC_AUTO_CREATE_USER` 和系统注册策略决定。
- `CHATAPI_OIDC_ADMIN_EMAILS` 限制可通过 OIDC 自动成为管理员的邮箱，逗号分隔。

管理员识别：

- OIDC 登录成功后，如果 ID Token 或 UserInfo 中的 email 命中 `CHATAPI_OIDC_ADMIN_EMAILS`，且 `email_verified=true`，则该本地用户 role 应设置为 `admin`。
- 如果 email 未命中 `CHATAPI_OIDC_ADMIN_EMAILS`，OIDC 自动创建的新用户默认 role 为 `user`。
- 如果用户曾因 OIDC 管理员邮箱登录被提升为 admin，但后来邮箱不再在 `CHATAPI_OIDC_ADMIN_EMAILS` 中，下一次 OIDC 登录时应降级为 `user`，除非该用户也被本地后台显式标记为管理员。
- 本地 `.env` 管理员账号仍保留，作为 OIDC 配置错误或 IdP 故障时的恢复入口。
- 不允许仅凭未验证邮箱授予管理员权限；如果 IdP 不提供 `email_verified`，必须当作未验证处理，不能自动授予 admin。
- 管理员 role 变更必须记录审计日志，包含 issuer、subject、email、变更前后 role 和触发原因。

与现有登录体系的关系：

- 本地用户名密码登录继续保留，作为自托管和 IdP 故障时的回退路径。
- TOTP 只作用于本地密码登录；OIDC 登录的 MFA 由外部 IdP 负责。
- OIDC 登录成功后仍建立 ChatAPI 自己的 session cookie，后续权限判断复用现有 session/auth 中间件。
- API Key 仍由 ChatAPI 本地生成和管理，不直接接受外部 IdP access token 调用 `/v1/*`。

配置示例：

```env
CHATAPI_OIDC_ENABLED=0
CHATAPI_OIDC_PROVIDER_NAME=Example SSO
CHATAPI_OIDC_ISSUER_URL=https://idp.example.com
CHATAPI_OIDC_CLIENT_ID=chatapi
CHATAPI_OIDC_CLIENT_SECRET=change-this-secret
CHATAPI_OIDC_REDIRECT_URL=https://chat.example.com/api/auth/oidc/callback
CHATAPI_OIDC_SCOPES=openid,email,profile
CHATAPI_OIDC_ALLOWED_DOMAINS=example.com
CHATAPI_OIDC_ALLOWED_EMAILS=
CHATAPI_OIDC_ADMIN_EMAILS=admin@example.com,owner@example.com
CHATAPI_OIDC_AUTO_CREATE_USER=0
```

### 4.8 WebSocket 和 SSE

推荐：

- WebSocket：`nhooyr.io/websocket`
- SSE：基于 `net/http` 原生 `http.Flusher`

原因：

- `nhooyr.io/websocket` 支持 context，和 Go 服务优雅关闭配合更好。
- SSE 不需要额外依赖，手写 encoder 更容易保证 OpenAI/Anthropic 兼容格式。

### 4.9 密码、TOTP、验证码

现状密码是 `salt$sha256(salt+password)`，Go 版应兼容验证，但新密码写入应升级为 Argon2id。

推荐：

- Argon2id：`golang.org/x/crypto/argon2`
- TOTP：`github.com/pquerna/otp`

当前 Go 重构分支已新增 `internal/platform/password`：

- `Hash(plain)` 生成 Argon2id PHC 风格字符串。
- `Verify(plain, encoded)` 支持 Argon2id 和旧 `salt$sha256(salt+plain)`，并在旧格式验证成功时返回 `NeedsUpgrade=true`。
- 该包不记录明文密码，不暴露 salt 或 hash 以外的运行时状态。当前本地用户登录 service 已在旧 hash 登录成功后调用 `Hash` 生成新 hash，并通过 users repository 更新 `password_hash` 和 `last_login_at`。

迁移策略：

- 登录时识别旧 hash 格式，验证成功后自动重写为 Argon2id。
- 虚拟模型 API Key 从 Go 重构版开始使用可解密密文存储；应用 API Key 只存 hash；上游模型 API Key 默认只存浏览器本地，不进入服务端数据库。
- 需要可解密存储的服务端密钥统一使用 `CHATAPI_MASTER_KEY` 派生加密密钥；Lab 模式可以使用本地调试默认 master key，生产部署必须提示管理员显式配置和备份。

### 4.10 邮件、通知和外部 API

Go 重构版只保留 SMTP 邮件发送能力，不再迁移其他第三方邮件服务商适配。这样可以减少外部 SDK、配置项、安全审计面和长期维护负担。

邮件模块仍建议保留统一抽象，方便内部调用注册验证码、密码重置、测试邮件等场景：

```go
type Sender interface {
    Send(ctx context.Context, message Message) error
}
```

实现：

- SMTP：标准库 `net/smtp` 或维护中的第三方 SMTP 包
- ntfy：标准库 HTTP client

SMTP 发送必须支持：

- host、port、username、password、from、use_tls/starttls 等基础配置。
- 连接超时和发送超时。
- 明确的错误日志，但不打印密码、验证码和完整收件人隐私信息。
- 测试邮件接口。

当前 Go 重构分支已先落地 SMTP-only 的基础实现：

- 配置项为 `CHATAPI_SMTP_ENABLED`、`CHATAPI_SMTP_HOST`、`CHATAPI_SMTP_PORT`、`CHATAPI_SMTP_USERNAME`、`CHATAPI_SMTP_PASSWORD`、`CHATAPI_SMTP_FROM`、`CHATAPI_SMTP_SECURITY`、`CHATAPI_SMTP_TIMEOUT`。
- `CHATAPI_SMTP_SECURITY` 支持 `none`、`starttls`、`tls`；生产建议使用 `starttls` 或 `tls`。
- `chatapi smtp test --dry-run` 只做本地配置校验，输出 JSON 报告，不连接 SMTP 服务器。
- `chatapi smtp test --connect-only` 会连接 SMTP 服务器，完成 TLS/STARTTLS 和认证握手后退出，不发送 `MAIL FROM` / `RCPT TO` / `DATA`。
- `chatapi smtp test --to user@example.com [--subject ...]` 会发送一封测试邮件；不传 `--to` 时自动降级为 dry-run，避免误发。
- `chatapi config print --redact` 会脱敏 SMTP password；`doctor` 只输出配置风险，不打印密钥。

ntfy 外部请求必须统一使用带超时的 `http.Client`，并复用现有私网 URL 策略，防 SSRF。

### 4.11 OpenAPI 和 SDK 兼容测试

推荐生成并维护：

- `docs/api/openapi.yaml`
- `docs/api/compatibility.md`

兼容测试必须覆盖：

- OpenAI Python SDK 对 `/v1/responses`
- OpenAI Python SDK 对 `/v1/chat/completions`
- Anthropic Python SDK 对 `/messages`
- 非流式等待和流式等待
- tool call、thinking、image input、manual delta、abort

## 6. 数据模型和迁移策略

### 5.1 核心表

必须兼容读取的现有表：

- `users`
- `user_api_keys`
- `user_configs`
- `uploaded_images`
- `config`
- `conversations`
- `messages`

Go 重构版新增表：

- `user_identities`
- `user_app_api_keys`
- `app_api_key_audit_logs`
- `auth_verification_codes`

现有结构中 JSON 主要存在：

- `conversations.metadata`
- `messages.metadata`
- `user_configs.value`
- `config.value`

首版不强制拆分这些 JSON 字段，避免破坏兼容性。

`users`、`user_configs`、`config` 和 `user_identities` 已在 SQLite bootstrap schema 中创建。当前 Go 重构分支已先补齐 `users` / `user_identities` 的 SQLite repository 基础能力，包括创建/更新/查询用户、按 username/email 查询用户、列出用户、按 OIDC provider + subject upsert / 查询外部身份和列出用户身份；同时补齐 `config` / `user_configs` 的 JSON 值 CRUD 仓储，用于后续系统设置、用户设置和运行时配置持久化。本地用户名密码登录已开始使用 `users` 表，仍保留 `.env` 管理员恢复入口和 Lab actor；OIDC 登录成功后也会写入同一套 `users` / `user_identities`。

`user_identities` 是 Go 重构版为 OIDC 登录新增的表。首个 migration 应在不影响旧库登录的前提下创建该表；旧用户默认没有外部身份绑定，仍可使用本地密码登录。

`user_app_api_keys` 是 Go 重构版为外部自动化新增的应用 API Key 表。它与现有 `user_api_keys` 分离：`user_api_keys` 只用于 OpenAI/Anthropic 模型兼容接口，`user_app_api_keys` 用于操作 ChatAPI 自己的工作台资源。

### 5.2 数据库版本和 migration 表

Go 重构版需要新增数据库元信息表和迁移历史表，用于处理自动升级、跨多个版本逐步升级、升级中断恢复和诊断。

推荐新增键值表：

```sql
CREATE TABLE IF NOT EXISTS db_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
```

建议写入的 key：

- `schema_version`：当前数据库 schema 版本，例如 `7`。
- `app_version`：最后一次成功运行该数据库的 ChatAPI 版本，例如 `1.2.0`。
- `migration_dirty`：上次迁移是否中断，`0` 或 `1`。
- `migration_lock`：迁移锁标记，防止多进程同时升级。
- `created_by`：首次创建数据库的实现，例如 `python-flask` 或 `go`.
- `last_migrated_at`：最后一次迁移完成时间。

同时保留 migration 历史表：

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL,
  checksum TEXT NOT NULL DEFAULT '',
  dirty BOOLEAN NOT NULL DEFAULT 0
);
```

如果使用 `golang-migrate`，可以沿用其默认表结构，但仍建议保留 `db_meta` 作为 ChatAPI 自己的版本和诊断键值表。

当前 Go 重构分支的 SQLite bootstrap 已先落地最小可诊断版本：

- `db_meta` 包含 `schema_version`、`migration_dirty`、`migration_lock`、`created_by`、`last_migrated_at`，并为每个键维护 `updated_at`。
- `schema_migrations` 包含 `version`、`name`、`applied_at`、`checksum`、`dirty`。
- 当前版本号使用可读字符串 `0001_bootstrap`、`0002_xxx` 这种单调递增文本版本，同时保持 `db_meta.schema_version` 可诊断。当前 Go 重构分支已先落地 SQLite 侧的注册式 migration 序列，并用 `0002_sqlite_app_api_indexes` 验证从 bootstrap 到增量升级的流程。
- bootstrap 会向前兼容旧的薄 `db_meta` / `schema_migrations` 表，自动补齐缺失列。

启动升级流程：

1. 打开目标数据库后先创建 `db_meta` 和 migration 表。
2. 读取 `db_meta.schema_version`；如果不存在，则根据现有表结构识别旧 Flask 数据库版本并写入基线版本。
3. 如果 `migration_dirty=1`，服务拒绝启动，并提示用户先备份后运行 `chatapi migrate repair` 或按文档恢复。
4. 获取迁移锁，防止多个进程同时升级同一个数据库。
5. 按版本顺序逐个执行 migration，不允许跳跃执行。
6. 每个 migration 成功后更新 `schema_migrations` 和 `db_meta.schema_version`。
7. 全部成功后写入 `app_version`、`last_migrated_at`，清除 dirty 和 lock。
8. 任意一步失败时设置 dirty，保留错误日志，不继续启动业务服务。

### 5.3 迁移原则

- 第一个 Go migration 必须能识别并接管现有 Flask 创建的表。
- 所有 migration 必须按 `N_name.up.sql` 形式单调递增编号，禁止修改已经发布版本的 migration 文件。
- 支持从旧版本逐步升级到最新版本，例如 `1 -> 2 -> 3 -> 4`，不能只写“当前版本直接建表”的逻辑。
- 不删除老字段。
- 新增字段必须有默认值。
- 涉及安全升级的迁移采用惰性升级，例如登录时升级密码 hash。
- 每个 migration 必须有单元测试，使用临时 SQLite 文件从旧 schema 初始化后执行升级。
- 自动升级前应检测数据库文件是否可写，并在生产文档中要求升级前备份。
- `chatapi serve` 默认可自动执行向前兼容 migration；破坏性或高风险 migration 必须要求用户显式运行 `chatapi migrate up`。
- 不支持自动降级；回滚依赖备份恢复。`down` migration 仅用于开发和测试环境。

### 5.4 Repository 实现策略

必须从一开始提供两套 repository 实现：

- `internal/repository/sqlite`
- `internal/repository/postgresql`

要求：

- repository 接口不暴露 SQLite 或 PostgreSQL 专属类型。
- SQL 查询分别集中在各自 repository 包内，业务层不拼 SQL。
- 业务层不依赖 SQLite JSON 函数、PostgreSQL JSONB 函数、RETURNING 等数据库专属能力；如果确实需要，封装在 repository 内并提供双实现。
- migration 文件可以按数据库拆分目录，例如 `migrations/sqlite` 和 `migrations/postgresql`。当前重构分支已先在各自 repository 包内使用 `sql/*.up.sql` 文件并通过 `embed` 装载；后续如需独立发布 migration 工具，再评估是否抽到统一顶层目录。
- 单元测试必须对 SQLite 和 PostgreSQL repository 跑同一套 contract tests。
- CI 至少跑 SQLite；PostgreSQL 使用 service container 跑集成测试。为了避免多个 package/子测试共享同一个库时互相 `Reset/Bootstrap` 造成死锁或 catalog 冲突，PostgreSQL 测试应默认按“每个测试独立 schema/DSN”隔离，而不是整套测试共用一个 schema。

当前 Go 重构分支已新增 `internal/repository/storetest` 作为 repository contract test 的复用入口；SQLite repository 已开始复用这套 contract 覆盖 users、user_identities、config、user_configs、应用 API Key、应用 API Key 审计日志、虚拟模型 API Key、通用审计日志、自动化规则、上传图片、用户存储配额、上传删除失败队列，以及 conversations/messages/requests/pending turn 最小状态机。PostgreSQL repository 已挂载同一套 contract tests，使用 `CHATAPI_PG_TEST_DSN` 可选启用；当前已覆盖 users、user_identities、config、user_configs、user_app_api_keys、app_api_key_audit_logs、user_api_keys、audit_logs、automation_rules、uploaded_images、storage_user_quotas、storage_file_deletion_failures、conversations、messages 和 pending turn 生命周期。为保证真实 PG 容器上 `go test ./...` 可并发稳定运行，测试基建已开始通过 `internal/testutil/pgtest` 为每个测试生成独立 schema 和派生 DSN，避免 `Reset/Bootstrap` 互相冲突。HTTP 层也已补上 PostgreSQL 下 `/metrics`、管理员 runtime summary/connections/queue、管理员 storage summary/users/vacuum，以及 OpenAI Responses / Chat Completions / Anthropic Messages 三套协议的最小非流闭环与基础 SSE 集成覆盖；后续继续补 PostgreSQL 专属 migration、运行时切换和更完整的集成测试。

SQLite 到 PostgreSQL 迁移：

- 老用户可以先用 SQLite 启动 Go 版完成旧 Flask 数据接管。
- 当前已提供 `chatapi migrate-db sqlite-to-postgres --sqlite ./data/chatapi.sqlite3 --postgres $CHATAPI_DATABASE_URL`。
- 当前首版会复制 users、OIDC identities、system/user configs、conversations、messages、uploads 元数据、虚拟模型 API keys、应用 API keys、应用 API key 审计日志、通用审计日志、自动化规则、存储配额和删除失败队列。
- 当前命令会要求目标 PostgreSQL 业务表为空，避免做隐式 merge；如果目标已有业务数据，命令直接失败。
- 当前实现采用“先读 SQLite 全量快照，再在 PostgreSQL 单事务导入”的策略，优点是首版简单稳定，缺点是大库迁移时内存占用会更高；后续再演进为分批流式搬迁。
- 文件 uploads 仍在文件系统，数据库只迁移元数据；部署文档需要说明 uploads 目录同步。

## 7. 关键业务模块设计

### 6.1 Turn Manager

Turn Manager 是 Go 迁移成败的核心。

职责：

- 创建 pending turn。
- 按 owner/conversation/request id 查询等待状态。
- 限制每用户 pending 数量和等待时长。
- 处理人工 delta、complete、abort。
- 支持自动化规则直接完成。
- 负责等待态和会话 metadata 的一致性。
- 对 SSE/WebSocket 发布状态变化。

建议把所有人工/自动化回复入口统一收束到一个共享命令模型，例如 `TurnControlCommand`：

```go
type TurnControlCommand struct {
    Kind                TurnControlKind
    ConversationID      string
    ResponseID          string
    OutputText          string
    Mode                string
    ToolName            string
    ToolCallID          string
    ToolOutput          string
    ReasoningStreamMode string
    AbortReason         string
}
```

WebUI、应用 API、自动化规则都只负责把各自请求翻译成这一个命令对象，再交给 Turn Manager / service 执行，避免三套入口各自拼字段、各自做状态判断。

与之配套，建议把“当前请求归属于哪个用户/身份”的问题也统一收束到一个共享 request actor 上下文，例如：

```go
type RequestActor struct {
    UserID   string
    Username string
    Role     string
    Source   string
}
```

Lab 模式、session 用户、应用 API Key、后续虚拟模型 API Key 中间件都只负责把 actor 注入到 context；Turn Manager / service 从 context 读取 owner，而不是在业务逻辑里硬编码 `lab-user` 或散落各自的用户解析逻辑。

推荐模型：

```go
type PendingTurn struct {
    RequestID      string
    ConversationID string
    OwnerID        string
    Protocol       Protocol
    Model          string
    CreatedAt      time.Time
    ExpiresAt      time.Time
    Done           chan struct{}
    Result         *TurnResult
    Abort          *TurnAbort
}
```

注意事项：

- 所有 map 访问必须有锁或使用单 goroutine actor 模型。
- `Done` 只能关闭一次。
- SSE 客户端断开时不能误删已经被 Web 控制台接管且仍有效的 pending turn，除非当前协议语义明确要求丢弃。
- abort 和 complete 并发时必须确定优先级，建议先到者胜，后到者返回 409。
- pending turn 状态机必须集中在 Turn Manager，禁止 handler、应用 API、自动化规则各自修改状态。
- 推荐状态：`pending`、`streaming`、`completed`、`aborted`、`expired`。
- 状态转换必须具备原子语义；数据库落库时使用版本号/CAS 或行锁，避免 WebUI、应用 API、自动化规则同时 complete。
- delta 可以多次追加，但只有 `pending` 或 `streaming` 可追加；`completed`、`aborted`、`expired` 后任何输出操作返回 409。
- complete、abort、expire 都是终态转换，必须只成功一次。
- WebSocket/SSE 长连接不能持有数据库事务；状态快照、最终消息、审计记录使用短事务完成，流式 delta 通过内存 broker 发送，必要时完成时批量落库。

### 6.2 Protocol Adapter

统一内部请求模型：

```go
type TurnRequest struct {
    Protocol       Protocol
    Model          string
    Stream         bool
    InputMessages  []Message
    Tools          []Tool
    ToolChoice     any
    Raw            json.RawMessage
}
```

每个协议包负责：

- 解析请求
- 校验必要字段
- 转成内部消息模型
- 根据 TurnResult 构建最终响应
- 根据 TurnResult 构建 SSE chunk
- 构建错误响应

非流和流式协议要求：

- `stream=false` 或缺省 `stream`：模型兼容入口应创建 pending turn，等待 WebUI / 应用 API / 自动化规则完成后，一次性返回目标协议的最终 JSON。当前前端没有流式输出功能时，可以先只使用非流式手工回复路径。
- `stream=true`：模型兼容入口应返回目标协议要求的 SSE event/chunk，delta 只通过内存 realtime hub 广播，最终完成时再用短事务落库。
- 非流和流式必须共享同一个内部 `TurnRequest` / `TurnResult` 状态机，不能维护两套转换逻辑。
- 当前 Go 重构分支允许后端接口和旧 WebUI 临时不完全兼容；新的 path-based manual output API 和返回格式应以本文档为准，后续前端按新契约改造。

协议包不能访问数据库。

可抽取通用包：

ChatAPI 的 OpenAI Responses、Chat Completions、Anthropic Messages 三套协议归一化逻辑应尽量设计成可独立复用的 Go 包，而不是只服务当前 handler。建议后续拆成 `chatapi-protocol` 或类似模块。

包边界建议：

- 输入：原始 HTTP 请求 body、headers、query、目标协议类型。
- 输出：统一的 `TurnRequest` / `ModelRequest` 结构。
- 支持把统一结果重新编码为 OpenAI Responses、Chat Completions、Anthropic Messages 的非流式响应和 SSE event。
- 支持 tools、tool choice、tool call、multimodal content、reasoning、response_format、usage、error 格式的双向映射。
- 不依赖数据库、Web 框架、ChatAPI 用户体系、Turn Manager。
- 只依赖标准库和少量 JSON/schema 工具，方便其他项目引用。
- 当前 Go 重构分支已先把协议层错误输出收回 `internal/protocol`：模型兼容入口在创建 pending turn 之前会先做最小请求校验（例如 `stream` 必须为布尔值、请求里必须有至少一个用户输入 part），并按协议返回错误 envelope，而不是直接吐通用 `http.Error`。Responses / Chat Completions 当前统一返回 OpenAI 风格 `{error:{message,type,code,param}}`，Anthropic Messages 返回 `{type:"error", error:{type,message}}`。人工 abort 终态也会按原请求协议返回同一套错误外形。
- 当前协议层还已开始补齐 `tools` / `tool_choice` / `response_format` 的最小契约校验：当 `tool_choice.type=function` 时，必须给出 `tool_choice.function.name`；如果本次请求同时声明了 tools 列表，该名字还必须出现在声明的 tools 里。这样既能约束真实 tool schema 场景，也不会打断当前把 `tool_choice` 当作结构化元数据使用、但暂时不附带完整 tools 列表的请求。`response_format.type=json_schema` 时，必须给出 `json_schema.name` 和对象形态的 `json_schema.schema`。这样后续无论是 ChatAPI handler、KirariNetwork 上游网关还是浏览器端辅助，只要复用这套 protocol 包，就能得到一致的 400 错误语义，而不是各自手写校验。
- 协议编码侧当前也已开始从“直接拼 map”收口到可复用 encoder：Responses/Chat Completions/Anthropic 的 tool call、tool result、usage、tool-use block 已抽到共享 helper，`BuildResponseForMeta(meta, result)` 可在不依赖 `store.Conversation` 的情况下直接输出最终响应 JSON，便于后续把协议包拆出去给 KirariNetwork 或独立代理复用。
- 协议测试侧当前也已开始补 fixture/golden 风格覆盖：同一个 `TurnResult` 会分别走 Responses / Chat Completions / Anthropic Messages 的非流和流式编码断言，尤其固定 assistant message、tool call、tool result、usage 等关键字段，降低后续继续补 reasoning、多模态或独立拆包时的回归风险。
- 输入归一化侧当前已继续补齐两类常见多模态变体：OpenAI Responses 风格的 `input` 现在不仅支持 message 数组，也支持直接给 `input_text` / `input_image` content part 数组；Anthropic `image` block 的 `source.media_type` / `source.data|url` 也已通过协议测试固化。这样后续浏览器端辅助或外部 SDK 直接复用 protocol 包时，不需要额外再套一层“伪 message” 包装。
- 工具结果输入侧当前也已开始归一化：OpenAI Responses `function_call_output`、Chat Completions `role=tool` 消息，以及 Anthropic `tool_result` block 都会统一映射成 `InputPart{Type:"tool_result", Text:"..."}`，并参与 `UserContent` 拼接。这样自动化规则、后续上游辅助和调试界面读取请求上下文时，可以直接看到工具返回文本，而不需要分别理解三套协议的工具结果外形。
- 指令角色侧当前也已开始显式保留：`TurnRequest` 已新增 `SystemContent` / `DeveloperContent`，会从 Chat Completions / Responses 的 `role=system|developer` 消息，以及 Anthropic 的顶层 `system` 字段里提取文本。这样后续如果要做更完整的上游代理、调试上下文展示或策略校验，不需要再回退到原始 body 里自己翻 role。
- assistant 上下文侧当前也已开始显式保留：`TurnRequest.AssistantContent` 会提取三套协议里历史 assistant 文本消息内容。这样后续如果要在浏览器端做“请求大模型”辅助、展示更完整的最近上下文，或在后端做更细的规则匹配，不需要只盯着最后一条 user/tool 输入。
- 这些解析出来的上下文字段当前已继续下推到 pending turn `request_debug` 持久化：SQLite / PostgreSQL 在创建请求时都会把 `system_text`、`developer_text`、`assistant_text` 写进 message metadata，并在 `GetRequest/ListRequests` 读取路径回填到统一 `store.Request`。这样协议层新增信息不会只停留在解析阶段，后续管理端、Lab 模式和应用 API 读取请求详情时都能复用。
- Lab 请求详情和应用 API 请求详情当前已能直接读到这些上下文字段；对应集成测试已开始覆盖 `GET /lab/requests/{requestID}` 和 `GET /api/app/requests/{requestID}` 的 `system_text` / `developer_text` / `assistant_text` 返回，确保 SQLite / PostgreSQL 两条后端路径下都不会在读取阶段丢字段。
- 在此基础上，Lab 和应用 API 的 request detail 当前还已新增轻量 `parsed` 视图：把 `request_format`、model、system/developer/assistant/user 文本、input parts、tool choice、tool schemas、response format 以及 `request_body_keys` 归到一处，避免调试界面必须同时拼接平铺字段和原始 `request_body` 才能看清解析结果。
- `GET /lab/requests` 和 `GET /api/app/requests` 当前还会并行返回 `parsed_items` 轻量摘要数组，用于列表页直接展示解析后的请求关键信息，而不必把 detail 级别的完整 `input_parts` / `tool_schemas` 重复塞回列表。摘要当前包含 `request_id`、协议格式、model、system/developer/assistant/user 文本、`input_part_types`、tool choice、response format 和 `request_body_keys`。

这个包可以同时被 ChatAPI 和 KirariNetwork 使用：ChatAPI 用它接收外部 Agent 请求并归一化为 pending turn，KirariNetwork 可用它把不同上游模型协议归一化为统一模型网关响应。

### 6.3 Realtime Hub

职责：

- 管理 WebSocket 订阅。
- 管理 SSE/WebSocket 连接计数和限制。
- 推送 conversation upsert/delete、snapshot、connection counts。

Go 版应使用 owner 分片 map：

```go
type Hub struct {
    mu sync.RWMutex
    subscribers map[string]map[string]*Subscriber
    connections map[string][]Connection
}
```

要求：

- 每个 subscriber 有有限队列，满队列时按策略断开或丢弃旧事件。
- 事件广播必须有背压策略：每个连接设置 send queue 上限，慢客户端超过阈值后断开或只丢弃可恢复的 snapshot/update 事件。当前 Go 重构分支已按订阅者固定队列实现 recoverable drop 计数和慢订阅者断开计数。
- 事件需要区分关键事件和可恢复事件；complete、abort、权限变更等关键事件不能静默丢弃，conversation list refresh、connection count 等可由重连后 snapshot 修复。
- 浏览器重连后必须能拉取 owner 级 snapshot，避免中途丢事件导致 UI 永久不一致。
- 全局连接数和单用户连接数与系统配置保持一致，但必须区分浏览器控制台连接和 API/SSE 连接。当前 Go 重构分支已实现统一连接 lease 和分类计数，`webui` / `api` / `sse` 会进入同一个限额池。
- 单用户连接限制必须为浏览器控制台预留连接名额，避免 API/SSE 连接占满后用户无法打开 WebUI。当前实现通过 `CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER` 在同用户非 WebUI 连接申请时保留名额，WebUI 连接可以使用该预留位。
- 推荐做法是按连接 kind 分池计数：`webui_ws`、`api_sse`、`app_api`、`lab`，分别配置上限，并提供 `webui_reserved_connections_per_user`。
- 如果采用统一连接池，也必须在驱逐策略中优先驱逐同用户最旧的 API/SSE 连接，不能驱逐最后一个浏览器控制台连接。
- WebSocket 写操作单 goroutine 化，避免并发写 panic。

### 6.4 Automation Rules

首版可以先等价迁移现有规则引擎，不扩展 DSL。

工程要求：

- 规则解析和规则执行拆开。
- 规则执行必须可测试，输入为 TurnRequest 和会话上下文，输出为 TurnResult 或 nil。
- 增加规则执行超时和最大输出长度限制。
- 记录规则命中日志和指标。

当前 Go 重构分支已先落地最小执行内核：

- `AutomationRuleService` 不再只负责 CRUD，已开始提供基于结构化 `TurnRequest` 的规则匹配入口。
- 当前已支持最小条件子集：`conditions.contains` / `conditions.excludes` 配合 `action.type=output_text` 的自动完成动作。
- 规则匹配会复用协议层已经结构化的 `UserContent` / `InputParts`，而不是重新从原始 JSON body 做临时字符串解析。
- `contains/excludes` 当前同时支持两种 matcher 形态：
  - legacy matcher：`{ "field": "...", "match_type": "substring|exact", "pattern": "..." }`
  - typed condition block：`{ "type": "...", ... }`
- legacy matcher 当前支持显式 `field` 选择，已落地字段包括：默认 `text`、`user_content`、`input_part.text`、`input_part.type`、`input_part.media_type`、`input_part.url`、`tool_choice.type`、`tool_choice.name`、`response_format.type`、`response_format.name`、`model`、`protocol`。未传 `field` 时保持兼容，默认仍匹配文本内容。
- typed condition block 当前已落地：`text_contains`、`text_is`、`user_content_contains`、`user_content_is`、`model_is`、`protocol_is`、`tool_choice_is`、`response_format_is`、`input_part_type_is`、`input_media_type_contains`、`input_media_type_is`、`input_url_contains`。这样后续扩展条件能力时，可以新增显式类型，而不是继续堆散乱的 `field + pattern` 组合。
- 自动化规则当前已开始复用 richer request context：legacy `field` 已补上 `system_content`、`developer_content`、`assistant_content`、`tool_result`；typed condition 也已补上 `system_content_contains|is`、`developer_content_contains|is`、`assistant_content_contains|is`、`tool_result_contains|is`。这样规则不仅能看最后一条 user 文本，也能利用历史 assistant 回复、显式 system/developer 指令和最近工具返回文本做匹配。
- 后端当前已新增规则结构化 parser/validator：`ReplaceRules` 不再直接接受任意裸 `map[string]any` 入库，而是先解析成内部规则对象、校验 action 和 matcher、再输出规范化 payload。这样 WebUI、应用 API、未来导入导出以及规则执行共享同一套校验逻辑；对外 JSON 形状暂时保持不变，前端本轮不需要同步改协议。
- 规则命中后，`ChatAPIService` 会在 pending turn 落库并进入 realtime 广播后，复用同一套 `CompleteConversation` 状态机直接自动完成请求；因此非流请求会直接返回自动结果，流式请求会在 SSE 起始事件后收到同样的完成事件，而不会走另一条旁路逻辑。
- 当前自动执行是 best-effort：规则读取或匹配异常不会阻断主请求链路，只会退化为普通 pending turn。
- 当前输出会受服务内最大长度限制截断；自动化规则自动完成命中已写入 `audit_logs`，并汇总到 `/api/app/statistics/summary`、`/api/admin/requests/overview`、`/api/admin/runtime/summary`、`/api/admin/runtime/automation` 和 `/metrics`。当前还会把 `ListAutomationRules` 失败、自动完成失败写入 `audit_logs` 的 `outcome=failure`，并把运行期 `no_rules` / `no_match` 跳过计数、规则级 `skip_by_reason` 聚合、按规则分组的 `skip_by_rule` 统计以及最近未命中样本暴露到管理员运行时接口；`/metrics` 继续只暴露低基数的 reason 维度，避免把 rule id 带进指标标签。更细的规则执行超时和逐条规则级跳过审计仍待继续补齐。

### 6.5 Upload/Image Store

要求：

- 文件名必须由服务端生成或严格白名单校验。
- MIME 类型用内容嗅探，不只信任请求头。
- 路径必须经过 `filepath.Clean` 后验证仍在 uploads 根目录内。
- 执行单文件大小、单请求大小、用户总量限制。
- 保持 `/api/uploads/imgs/<filename>` 兼容。

当前 Go 重构分支已先落地最小兼容面：

- `POST /api/uploads/imgs`：接受 multipart 文件字段 `file` / `image` / `upload`，使用 `http.DetectContentType` 嗅探 PNG/JPEG/GIF/WebP，服务端生成 UUID 文件名，写入 `data/uploads/imgs`；默认 `CHATAPI_UPLOAD_MAX_BYTES=10485760`。
- `uploaded_images`：记录上传文件的 `owner_id`、服务端文件名、原始文件名、MIME、字节数、访问 URL 和创建时间。Lab 模式当前归属到 `lab-user`；生产模式后续接入 session/OIDC 后从统一 `RequestActor` 注入真实用户。
- `GET /api/uploads/imgs/{filename}`：只接受单段文件名，拒绝空文件名、路径分隔符和 `..`，并在服务端解析后验证仍位于 `data/uploads/imgs` 根目录。
- `GET /api/uploads/imgs/usage`：统计 uploads/imgs 目录的文件数和总字节数，目录不存在时返回 0。
- 上传成功/失败会写入通用 `audit_logs`；管理员可通过 `GET /api/admin/storage/orphans` 预览无元数据的孤儿图片，并通过 `POST /api/admin/storage/orphans/cleanup` 显式执行删除。用户配额已支持 `PUT/DELETE /api/admin/storage/users/{owner_id}/quota` 做单用户覆盖或恢复默认值。

### 6.6 资源治理与运维监控

管理员后台应提供资源监控和资源治理能力，方便自托管用户长期运行 ChatAPI。

管理员监控面板：

- 系统资源：CPU 使用率、系统内存、可用内存、磁盘空间、负载。
- ChatAPI 进程资源：RSS、虚拟内存、Go heap、goroutine 数、线程数、文件描述符数。
- Go runtime：GC 次数、最近 GC 时间、GC pause、heap in-use、heap idle、next GC 目标。
- 数据资源：SQLite 主库大小、WAL 大小、uploads 目录大小、每用户估算存储占用。
- 连接资源：WebUI WebSocket 连接数、API/SSE 连接数、应用 API 请求速率、被限流/拒绝连接数。
- 业务资源：pending turn 数、等待超时数、自动化规则命中数、手动回复数。
- 请求态势：当前 pending 请求数、最老 pending 等待时长、每用户 pending 数、每虚拟模型 pending 数、平均人工回复耗时、自动化规则命中率、超时率、应用 API 回复平均耗时。
- 队列态势：Realtime subscriber 队列长度、慢客户端断开数、丢弃的可恢复事件数、WebUI 重连次数。

建议接口：

- `GET /api/admin/runtime/summary`
- `GET /api/admin/runtime/automation`
- `GET /api/admin/runtime/memory`
- `GET /api/admin/runtime/system`
- `GET /api/admin/runtime/connections`
- `GET /api/admin/runtime/queue`
- `GET /api/admin/runtime/settings`
- `PUT /api/admin/runtime/settings`
- `GET /api/admin/requests/overview`
- `GET /api/admin/storage/summary`
- `GET /api/admin/storage/users`
- `GET /api/admin/storage/orphans`
- `POST /api/admin/storage/orphans/cleanup`
- `POST /api/admin/storage/cleanup`
- `POST /api/admin/runtime/gc`

当前 Go 重构分支已先落地运行时监控的服务内指标和 Linux 系统级探针：

- `GET /api/admin/runtime/summary`：返回 Go runtime 基本信息、系统资源快照、Go 内存快照、自动化规则命中/失败摘要，以及运行期 `no_rules` / `no_match` 跳过计数、`skip_by_reason` 聚合、`skip_by_rule` 聚合、最近未命中样本、pending turn 统计、realtime subscriber 队列统计、SQLite 主库/WAL 文件大小。
- `GET /api/admin/runtime/automation`：返回与 summary 中同源的自动化诊断视图，首版用于独立读取 `hits`、`failures`、`no_rules`、`no_match`、`skip_by_reason`、`skip_by_rule` 和 `recent_skips`，避免前端为查看自动化状态必须拉取整份 runtime summary。当前已支持查询参数 `limit`、`reason`、`rule_id` 过滤最近未命中样本。
- `GET /api/admin/runtime/memory`：返回当前 Go `runtime.MemStats` 的核心字段，包括 heap、sys、next GC、GC 次数和 pause 累计。
- `GET /api/admin/runtime/system`：返回主机名、CPU 数、load average、系统总内存/可用内存、ChatAPI 进程 RSS、打开 FD 数，以及 `data_dir` 所在文件系统总容量/可用容量。当前实现使用 Linux `/proc` 和 `statfs`，非 Linux 部署后续按目标平台补齐。
- `GET /api/admin/runtime/connections`：返回当前 realtime subscriber 数、WebUI subscriber 数、API/SSE lease 数、总连接数和被限额拒绝的连接数。当前 `/api/ws` 已按 `webui` 计数，后续 API/SSE 长连接接入时必须使用 realtime hub 的 `Acquire` / `Release`。
- `GET /api/admin/runtime/queue`：返回 realtime subscriber 当前队列长度、总容量、recoverable/critical drop 和慢客户端断开计数字段；当前慢客户端策略为连续多次队列满后关闭订阅者。
- `GET /api/admin/runtime/settings`：返回当前 ChatAPI 记录的运行时治理参数，包括 `gogc` 和 `memory_limit_bytes`。`0` 表示该项未由 ChatAPI 显式管理。
- `PUT /api/admin/runtime/settings`：接受可选 `gogc`、`memory_limit_bytes` 非负值并立即作用于当前 Go 进程；成功修改会写入 `audit_logs`。当前持久化仍通过 `CHATAPI_RUNTIME_GOGC` 和 `CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES` 环境变量完成，管理接口修改重启后不会保留。通过接口写回 `0` 会恢复 Go 默认 GC 百分比和近似无限内存限制。
- `POST /api/admin/runtime/gc`：触发一次 `runtime.GC()` 和 `debug.FreeOSMemory()`，返回 GC 后内存快照。
- `/metrics` 已输出系统内存、进程 RSS/FD、data dir 磁盘容量和 load average 等 gauge；当前自动化观测还会输出 `chatapi_automation_hits_total`、`chatapi_automation_failures_total`、`chatapi_automation_no_rules_total`、`chatapi_automation_no_match_total` 以及带 `reason` label 的 `chatapi_automation_rule_skips_total`。真正的 CPU usage 百分比需要跨采样窗口计算，后续可在 runtime monitor 中维护滑动窗口。
- `GET /api/admin/storage/summary`：返回数据库观测、uploads 目录大小、估算用户数、估算总字节数、会话数和消息数。当前 `summary.database` 结构与 `GET /api/admin/runtime/summary` 复用同一套 `DatabaseInfo`：SQLite 返回主库/WAL 路径和字节数，PostgreSQL 返回连接池统计。
- `GET /api/admin/storage/users`：返回每个 owner 的估算字节数、会话数、消息数、图片数、图片字节数、默认配额、单用户 override 配额、最终生效配额和是否超过配额。当前估算范围包含 conversation/message 文本、metadata JSON，以及已写入 `uploaded_images` 的图片字节数；孤儿文件仍只体现在 uploads 目录总量中。
- `PUT /api/admin/storage/users/{owner_id}/quota`：设置单用户存储配额覆盖，body 为 `{"quota_bytes": 104857600}`；`quota_bytes=0` 表示该用户不限制。
- `DELETE /api/admin/storage/users/{owner_id}/quota`：删除单用户覆盖，恢复使用全局默认配额。
- `GET /api/admin/storage/orphans`：扫描 `data/uploads/imgs` 下没有 `uploaded_images` 元数据的单层文件，返回 dry-run 预览、文件数、字节数和文件列表。
- `POST /api/admin/storage/orphans/cleanup`：要求显式传入 `{"dry_run": false}`，重新扫描当前 orphan 列表后只删除 uploads/imgs 根目录下的单层孤儿文件，返回候选文件数、候选字节数、实际删除文件数和实际删除字节数，并写入 `audit_logs`。
- `GET /api/admin/requests/overview`：返回所有用户请求的总数、pending/streaming/closed/aborted 计数、自动化规则命中数、按状态/模型/owner 聚合和最老 pending 等待秒数；当前不返回平均人工回复耗时、自动化命中率和超时率，因为这些需要额外事件计量。
- `POST /api/admin/storage/cleanup`：必须显式传 `dry_run`；请求参数为 `owner_id`、`keep_recent_conversations`、`keep_recent_days`，返回候选会话数、候选消息数、估算可回收字节数和按 owner 聚合的计划。`dry_run:false` 会删除候选 conversations，并通过数据库外键级联删除 messages；候选算法会跳过 `waiting` / `streaming` 活跃请求，避免清理正在等待人工/自动化回复的 turn。
- `POST /api/admin/storage/vacuum`：必须显式传 `dry_run`；`dry_run:true` 返回当前数据库观测快照，便于管理员先确认数据库类型和体量；`dry_run:false` 仅在 SQLite 下执行 WAL checkpoint 和 SQLite `VACUUM`，返回执行前后数据库信息并写入审计日志。PostgreSQL 下该接口会返回明确的 `400` 不支持错误，避免误判为服务端故障。
- 当前会话/消息清理执行已支持删除候选会话独占引用的 `/api/uploads/imgs/{filename}` 本地上传图片文件和 `uploaded_images` 元数据；如果同一图片仍被保留会话引用则不会删除。文件删除失败会进入 `storage_file_deletion_failures` 队列，后续每日存储维护会重试。每日存储维护已能按用户有效配额识别超额用户，并复用同一候选算法清理旧会话；后续可再补上传后即时触发和更细的保留策略 UI。

GC 设置：

- 支持在系统设置中配置 Go `GOGC` 百分比，例如 `50`、`100`、`200`。
- 支持配置内存软上限，对应 Go `GOMEMLIMIT`，例如 `512MiB`、`1GiB`。
- 支持管理员手动触发一次 GC，仅用于诊断或低峰期回收。
- 配置变更必须审计，运行时变更应调用 `debug.SetGCPercent` 和 `debug.SetMemoryLimit`，并在重启后从配置恢复。
- 不建议暴露“每隔 N 秒强制 GC”作为默认策略；如果必须提供，应标记为诊断选项，因为过于频繁的强制 GC 会降低吞吐和增加延迟。

用户存储占用估算：

- 每用户估算占用应至少包含：
  - conversations/messages 文本和 metadata 的估算字节数。
  - uploaded_images 文件大小。
  - 该用户相关 pending/request 历史记录。
- SQLite 行级占用无法精确拆分时，可以使用字段长度估算；uploads 使用真实文件大小。
- 管理员后台应展示每用户估算占用、会话数、消息数、图片数、最近活跃时间。

用户存储配额：

- 支持全局默认用户存储上限。当前 Go 重构分支已先通过 `CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES` 实现默认用户图片上传配额，值为 `0` 表示不限制；超过后 `POST /api/uploads/imgs` 返回 `507` 并写入失败审计。
- 支持单用户覆盖上限。当前 Go 重构分支已通过 `storage_user_quotas` 和管理员 `PUT/DELETE /api/admin/storage/users/{owner_id}/quota` 落地；上传校验会优先使用 override，否则回退全局默认值。
- 支持超额策略：
  - `block_new_uploads`：阻止新图片上传。当前 Go 重构分支已实现。
  - `auto_prune_old_conversations`：自动清理旧会话。当前 Go 重构分支已在每日存储维护中实现，按用户有效配额找出超额用户，并复用 `keep_recent_conversations` / `keep_recent_days` 保留策略删除旧会话。
  - `block_new_conversations`：阻止新会话。当前尚未实现，需谨慎评估是否会破坏调试流程。
- 自动清理必须优先删除最旧、最久未活跃的会话，并同步清理孤儿图片。当前 Go 重构分支按每用户最近活跃时间排序并保留最近 N 个或最近 N 天，孤儿图片清理由每日维护同批执行。
- 每次自动清理都要写审计日志，包括用户、释放空间、删除会话数、删除图片数和触发原因。当前每日存储维护已记录 `scheduled_cleanup` 审计 metadata，后续可进一步拆出 per-user quota prune 审计事件。

定时清理和空间回收：

- 支持配置每日定时清理时间，例如 `03:00`。
- 定时任务应执行：
  - 过期 pending turn 清理。当前 Go 重构分支已先提供 `CHATAPI_PENDING_TURN_TTL`，按固定 ticker 清理超时的 `waiting` / `streaming` turn；后续再接管理员可视化配置。
  - 孤儿图片清理和上传删除失败重试。当前 Go 重构分支的每日存储维护 worker 已执行。
  - 超配额用户旧会话清理。当前 Go 重构分支已支持每日维护时按用户有效配额识别超额用户，并按配置的保留策略清理旧会话。
  - SQLite WAL checkpoint。
  - 可选 `VACUUM` 或 `VACUUM INTO`，仅在管理员显式开启时执行。当前 Go 重构分支支持 `CHATAPI_STORAGE_VACUUM_ENABLED=1` 后在每日存储维护中执行 SQLite `VACUUM`。
- `VACUUM` 可能长时间锁库，默认不应每天自动执行；推荐先做 WAL checkpoint 和普通 prune，必要时由管理员手动执行空间回收。

建议系统配置 key：

- `value.runtime_gogc`
- `value.runtime_memory_limit_bytes`
- `value.storage_default_quota_bytes`
- `value.storage_cleanup_enabled`
- `value.storage_cleanup_time`
- `value.storage_cleanup_keep_recent_conversations`
- `value.storage_cleanup_keep_recent_days`
- `value.storage_vacuum_enabled`
- `value.pending_turn_ttl`
- `value.realtime_webui_reserved_connections_per_user`
- `value.realtime_max_webui_connections_per_user`
- `value.realtime_max_api_connections_per_user`

当前 Go 重构分支已先提供环境变量 `CHATAPI_RUNTIME_GOGC`、`CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES`、`CHATAPI_PENDING_TURN_TTL`、`CHATAPI_STORAGE_CLEANUP_ENABLED`、`CHATAPI_STORAGE_CLEANUP_TIME`、`CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_CONVERSATIONS`、`CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_DAYS` 和 `CHATAPI_STORAGE_VACUUM_ENABLED`。默认不启用自动存储清理和自动 VACUUM；后续接入系统配置表后，再把管理员后台修改持久化到 `value.runtime_*`、`value.pending_turn_ttl` 和 `value.storage_*`。

### 6.7 上游模型辅助

工作台 Tool Call 标签页上方新增“请求大模型”按钮，用于在人工回复时调用用户自己的上游模型，帮助理解当前上下文并生成 Tool Call 表单草稿。该功能只辅助填写，不自动提交给等待中的客户端。

首版默认采用浏览器端直连上游模型：用户的上游模型 API Key 只保存在浏览器本地，不上传到 ChatAPI 服务端。这样可以减少服务端密钥托管和 SSRF 风险，也方便用户直接连接 Ollama、LM Studio、本地 OpenAI-compatible server 等本地模型服务。

上游模型辅助的浏览器本地配置项：

- `upstream_assistant.enabled`
- `upstream_assistant.protocol`：`responses`、`chat_completions`、`anthropic_messages`
- `upstream_assistant.base_url`
- `upstream_assistant.api_key`
- `upstream_assistant.model`
- `upstream_assistant.extra_headers`：可选，JSON object
- `upstream_assistant.timeout_seconds`
- `upstream_assistant.max_input_messages`

存储要求：

- 默认配置属于浏览器本地配置，建议存放在 IndexedDB 或 localStorage；ChatAPI 服务端不保存用户私有上游 key。
- 浏览器端可以保存 `base_url`、`protocol`、`model`、`extra_headers` 和 `api_key`，但 UI 必须明确提示这些凭据保存在当前浏览器环境。
- 服务端 `GET/POST /api/user/config` 只用于普通用户偏好设置，不应保存 `upstream_assistant.api_key` 这类上游模型私密凭据；如果前端复用该接口保存上游助手非敏感偏好，必须过滤掉 API key。
- 如果未来增加服务端上游代理，必须默认关闭，由管理员显式开启，并使用服务端密钥加密存储上游 key。
- 前端导出配置时默认不导出完整 key，除非用户显式选择包含敏感字段。

协议支持：

- OpenAI Responses：调用上游 `/v1/responses` 或用户配置的兼容路径。
- OpenAI Chat Completions：调用上游 `/v1/chat/completions`。
- Anthropic Messages：调用上游 `/messages`。
- 三套协议都要通过统一前端 adapter 输出同一个内部结果模型；服务端 protocol adapter 可以复用 schema 和测试 fixture，但首版不承担上游请求代理。

交互流程：

1. 用户在浏览器本地设置中配置上游 `base_url`、`api_key`、`model`、协议类型。
2. 工作台等待请求进入后，用户打开 Tool Call 标签页。
3. 用户点击“请求大模型”。
4. 前端从当前 pending turn 读取上下文、可用 tools schema、当前草稿和本地上游配置，构造上游请求。
5. 上游模型必须先返回一段说明性文字，解释它准备调用哪个工具、为什么需要这些参数、有哪些不确定性。
6. 前端解析模型输出中的结构化 Tool Call 草稿。
7. 前端把说明性文字展示在按钮下方或表单上方，并把 Tool Call 表单字段填好。
8. 表单保持未提交状态，必须由用户检查后手动点击发送/完成。

推荐上游输出格式：

```json
{
  "explanation": "说明为什么建议这样填写。",
  "tool_call": {
    "name": "tool_name",
    "arguments": {
      "key": "value"
    }
  },
  "confidence": "medium",
  "warnings": []
}
```

实现要求：

- 前端提示词必须要求模型输出“说明文字 + 结构化 JSON”，并对 JSON 做严格校验。
- 如果模型输出无法解析，只展示说明/原文，不填写表单。
- 如果工具名称不在当前请求 tools schema 内，不能填写表单。
- 如果参数字段不符合 schema，前端应标记校验错误，不能自动修正后发送。
- 支持用户取消正在进行的上游请求。
- 上游请求默认不进入 ChatAPI 的公开 `/v1/*` pending turn 流程，避免递归调用自身。
- 如果 base URL 指向当前 ChatAPI 实例，应提示递归风险并拒绝或要求显式确认。
- 浏览器直连会受 CORS 限制；文档应说明如果云厂商不允许浏览器跨域请求，用户需要使用允许 CORS 的兼容网关、本地代理或未来可选的服务端上游代理。

建议路由：

- 首版默认不需要服务端配置路由和 assist 路由，上游配置由浏览器本地保存。
- 可选提供 `GET /api/workspace/tool-call/assist-context`，只返回当前用户可见的上下文、tools schema 和草稿，不接收上游 key，不请求上游模型。
- 如果未来启用服务端代理，再增加 `POST /api/workspace/tool-call/assist`，并要求管理员显式开启。

安全边界：

- 浏览器端直连时，ChatAPI 服务端不接触上游 API Key，也不承担 SSRF 风险。
- 浏览器端仍需设置超时、响应体大小上限和取消能力，避免页面被异常上游响应拖死。
- 不把上游 API Key 写入服务端日志、错误响应、审计详情或 WebSocket 状态。
- 浏览器本地保存 key 的安全边界必须在 UI 中说明：XSS、浏览器插件、共享电脑和被注入的前端资源仍可能读取本地凭据。
- 该功能不会替用户发出 Tool Call，只能填草稿。
- 服务端上游代理如果未来实现，必须使用 URL safety：默认拒绝 localhost、loopback、link-local、private IP、metadata 地址；DNS 解析后检查 IP；redirect 后重新校验；默认要求管理员配置 allowlist。Lab 模式可以通过显式参数放宽，以支持本地模型。

### 6.8 KirariNetwork OIDC 上游模型

ChatAPI 可以把 KirariNetwork 作为特殊的 OIDC delegated LLM upstream：用户通过 KirariNetwork OIDC 授权 ChatAPI，ChatAPI 后端使用该用户的 delegated access token 请求 KirariNetwork 的模型网关。

定位：

- KirariNetwork 是 OIDC Provider + LLM Gateway。
- ChatAPI 是 KirariNetwork 的 confidential RP。
- 模型调用使用用户授权后的 access token，不使用 ChatAPI 全局 API key。
- 权限、模型白名单、价格、配额和请求日志仍由 KirariNetwork 按 OIDC client policy 和用户 group 管理。

ChatAPI 配置建议：

```env
CHATAPI_KIRARI_ENABLED=0
CHATAPI_KIRARI_ISSUER_URL=https://kirari.example.com
CHATAPI_KIRARI_CLIENT_ID=chatapi
CHATAPI_KIRARI_CLIENT_SECRET=change-this-secret
CHATAPI_KIRARI_REDIRECT_URL=https://chat.example.com/api/integrations/kirari/callback
CHATAPI_KIRARI_SCOPES=openid,profile,email,offline_access,llm:read,llm:stream
CHATAPI_KIRARI_ALLOWED_ISSUERS=https://kirari.example.com
```

ChatAPI 用户级数据：

- `kirari_subject`
- `issuer_url`
- `access_token_ciphertext`
- `refresh_token_ciphertext`
- `expires_at`
- `granted_scopes`
- `model_meta_cache_json`
- `model_meta_cache_expires_at`

所有 token 必须用服务端 master key 加密存储；日志、审计和前端响应只展示 issuer、subject、scope、过期时间和模型摘要，不展示 token。

交互流程：

1. 管理员在 KirariNetwork 创建 `is_public=false` 的 OIDC client，启用 `llm:read`、`llm:stream`，配置 LLM policy 和 allowed models。
2. ChatAPI `.env` 配置 KirariNetwork issuer、client id、client secret 和 redirect URL。
3. 用户在 ChatAPI 用户设置中点击“连接 KirariNetwork”。
4. ChatAPI 发起 authorization code + state + nonce；可以同时使用 PKCE，即使 confidential client 不强制也可保留。
5. KirariNetwork 用户登录并同意授权。
6. ChatAPI 后端用 client secret 换取 access token、refresh token 和 ID token。
7. ChatAPI 校验 issuer、audience、nonce、expiry、签名和 email/email_verified 策略，建立或绑定当前用户的 Kirari identity。
8. Tool Call “请求大模型”或其他辅助功能选择 Kirari 上游模型时，ChatAPI 后端刷新 token 并请求 KirariNetwork LLM Gateway。

模型能力发现：

- 首选调用 KirariNetwork 的 `/api/llm/meta`，读取当前 delegated token 可用模型、模型标签、价格、额度和可用性。
- 如果 KirariNetwork 后续在 discovery document 中暴露 `llm_meta_endpoint`、`llm_chat_completions_endpoint`、`llm_supported_scopes`，ChatAPI 可自动发现。
- 首版可以显式配置 meta endpoint 和 chat completions endpoint，避免要求 KirariNetwork 立即修改。

请求路径：

- `GET {issuer}/api/llm/meta`
- `POST {issuer}/api/llm/chat/completions`
- `Authorization: Bearer <user_access_token>`

ChatAPI 内部建议把 Kirari 上游抽象为 `UpstreamProvider`：

```go
type UpstreamProvider interface {
    ListModels(ctx context.Context, userID string) ([]UpstreamModel, error)
    ChatCompletions(ctx context.Context, userID string, req ChatCompletionRequest) (*ChatCompletionResult, error)
}
```

安全边界：

- Kirari issuer 必须来自管理员 allowlist 或 `.env`，不能由普通用户任意输入。
- ChatAPI 后端请求 KirariNetwork 时仍复用 URL safety，避免配置错误指向内网或 metadata 地址。
- access token 过期时自动使用 refresh token 续期；refresh 失败则要求用户重新连接。
- 用户断开 KirariNetwork 连接时删除本地加密 token，并尽量调用 KirariNetwork revoke/logout 能力。
- Kirari 模型请求应记录审计日志：user id、issuer、kirari subject、model、耗时、状态码、错误类别，不记录 prompt 全文和 token。

### 6.9 可复用 Kirari OIDC LLM Client 包

可以把 ChatAPI 对接 KirariNetwork 模型能力抽成独立 Go 包，供其他类似项目复用。建议定位为 `kirari-llm-client` 或更通用的 `oidc-llm-client`。

包能力建议：

- OIDC discovery、authorization URL 构造、state/nonce/PKCE 生成和校验。
- authorization code token exchange，支持 confidential client secret。
- access token 自动刷新和 token storage 接口。
- ID token 校验和 UserInfo 拉取。
- Kirari `/api/llm/meta` 客户端：读取模型价格、模型标签、可用性、额度和限制。
- Kirari `/api/llm/chat/completions` 客户端：支持非流式和流式。
- 可选支持 OpenAI-compatible `/v1/models`、`/v1/chat/completions` 适配，方便未来 KirariNetwork 增加标准路由。

包边界建议：

- 包内不直接读写数据库，只定义 `TokenStore` 接口，由 ChatAPI 或其他项目实现加密存储。
- 包内不依赖具体 Web 框架，只提供 `http.Handler` 辅助或纯函数，Gin/chi/echo 项目都能接。
- 包内不处理 ChatAPI 用户创建、管理员识别、session cookie，只返回 OIDC identity 和 token set。
- 包内默认不记录 prompt；调用方传入 logger 时也必须脱敏。

这个包可以先在 ChatAPI 仓库内部 `internal/platform/kirari` 落地，接口稳定后再抽到独立仓库。

### 6.10 Admin 和用户配置

后台配置应继续落在 `config` 表，用户配置落在 `user_configs` 表。

当前 Go 重构分支已提供最小表驱动接口：

- `GET /api/admin/config` / `POST /api/admin/config`：仅 admin session 可访问，直接读写 `config` 表中的 JSON object 配置。首版只做持久化，不自动覆盖 env 派生运行时配置。
- `GET /api/user/config` / `POST /api/user/config`：按当前 actor 的 `user_id` 读写 `user_configs`，用于普通用户偏好。不得保存上游模型 API Key 等浏览器本地私密凭据。

Go 版需要增加配置 schema：

- key
- 类型
- 默认值
- 是否公开给前端
- 是否只有管理员可写
- 校验函数

避免 handler 里散落字符串 key 和类型转换。

### 6.11 应用 API Key 与自动化控制 API

Go 重构版需要把 API Key 分成三类概念，避免混淆：

- 虚拟模型 API Key：现有 `user_api_keys`，用于 `/v1/responses`、`/v1/chat/completions`、`/messages` 等模型兼容入口，让外部 Agent 把 ChatAPI 当成一个“虚拟模型”调用。
- 应用 API Key：新增 `user_app_api_keys`，用于外部自动化控制当前用户自己的 ChatAPI 工作台资源。
- 上游模型 API Key：用于 Tool Call 辅助里的真实上游模型调用，默认保存在浏览器本地，不属于这里的 `model_keys:*` 管理范围。

应用 API Key 的目标场景：

- 外部联网自动化程序读取自己收到的 pending 请求。
- 外部程序查看请求详情、headers、body、messages、tools。
- 外部程序读写自己的自动化规则。
- 外部程序创建、列出、删除自己的虚拟模型 API Key，用于自动化测试时动态发放模型兼容接口凭证。
- 外部程序提交人工回复、追加流式 delta、完成响应、终止请求。
- 外部程序拉取最近会话和消息，用于真实联网调试闭环。

应用 API Key 不允许：

- 登录 Web 控制台。
- 调用管理员接口。
- 管理其他用户。
- 读取或修改系统配置。
- 创建、查看、删除其他应用 API Key。
- 读取用户密码、OIDC 绑定、SMTP、上游模型 API Key 等敏感配置。

建议数据模型：

```text
user_app_api_keys
  id
  user_id
  name
  key_hash
  key_prefix
  scopes_json
  resource_limits_json
  ip_allowlist_json
  expires_at
  last_used_at
  created_at
  revoked_at
```

应用 API Key 必须只存 hash，不存明文。创建时只展示一次原始 key，建议前缀使用 `ak-`，避免和虚拟模型 API Key 的 `sk-` 混淆。

建议 scopes：

- `requests:read`：查看当前用户收到的 pending 请求和历史请求摘要。
- `requests:respond`：追加 delta、complete、abort。
- `conversations:read`：读取自己的会话和消息。
- `automation:read`：读取自己的自动化规则。
- `automation:write`：创建、更新、删除自己的自动化规则。
- `model_keys:read`：列出自己的虚拟模型 API Key 元信息。
- `model_keys:write`：创建自己的虚拟模型 API Key，key 以可解密密文保存，可按用户权限回看和复制。
- `model_keys:delete`：删除自己的虚拟模型 API Key。
- `statistics:read`：读取自己的统计信息。

建议 resource limits：

- `allowed_model_key_ids`：只允许管理指定虚拟模型 API Key；为空表示按 scope 允许当前用户全部虚拟模型 key。
- `allowed_request_ids`：只允许读取和响应指定 request id。
- `allowed_conversation_ids`：只允许读取指定会话及其消息。
- `allowed_virtual_models`：只允许处理指定虚拟模型名称收到的请求。
- `allowed_automation_rule_ids`：只允许读写指定自动化规则。
- `allowed_request_actions`：细分 `delta`、`complete`、`abort`，避免只需要流式输出的程序获得终止能力。
- `max_requests_per_minute`：覆盖默认限流。
- `max_model_keys`：限制该应用 API Key 创建虚拟模型 API Key 时，当前用户最多可持有的未撤销虚拟模型 key 数量。
- `allowed_source_ips`：可选 IP allowlist，适合部署在固定出口的自动化程序。当前 Go 重构分支已支持精确 IP 和 CIDR；默认按直连 `RemoteAddr` 判断，如果配置 `CHATAPI_TRUSTED_PROXIES`，且直连来源命中可信代理列表，应用 API 鉴权会读取 `X-Forwarded-For` / `X-Real-IP` 作为真实客户端 IP。不要把不受控网络段加入可信代理。

建议应用 API 路由统一放在 `/api/app/*`，只接受应用 API Key，不接受浏览器 session：

- `GET /api/app/me`
- `GET /api/app/requests`
- `GET /api/app/requests/{request_id}`
- `POST /api/app/requests/{request_id}/delta`
- `POST /api/app/requests/{request_id}/complete`
- `POST /api/app/requests/{request_id}/abort`
- `GET /api/app/conversations`
- `GET /api/app/conversations/{conversation_id}/messages`
- `GET /api/app/automation-rules`
- `PUT /api/app/automation-rules`
- `GET /api/app/model-keys`
- `POST /api/app/model-keys`
- `DELETE /api/app/model-keys/{key_id}`
- `GET /api/app/statistics/summary`

用户管理自己的应用 API Key 的 session 路由：

- `GET /api/user/app-api-keys`
- `GET /api/user/app-api-keys/schema`
- `POST /api/user/app-api-keys`：当前支持 `name`、`scopes`、`resource_limits` 和可选 RFC3339 `expires_at`。
- `DELETE /api/user/app-api-keys/{key_id}`

用户管理自己的虚拟模型 API Key 的 session 路由：

- `GET /api/user/model-api-keys`
- `POST /api/user/model-api-keys`
- `DELETE /api/user/model-api-keys/{key_id}`

认证方式：

- `Authorization: Bearer ak-...`
- 可选兼容 `X-ChatAPI-App-Key: ak-...`

实现要求：

- 应用 API Key 解析独立于虚拟模型 API Key 解析，避免把 app key 当成虚拟模型 key 调用 `/v1/*`。
- 每个应用 API 请求都必须绑定 owner_id，只能访问 key 所属用户的数据。
- 每个接口都要检查 scope，不能只检查 key 有效。
- scope 通过后仍要检查 resource limits，不能只靠 owner_id 粗粒度授权。
- 创建应用 API Key 时也要校验 scope 和 resource limits 本身，不能把未知 key、错误类型或无意义组合原样存库，再把歧义留给运行时。
- resource limit 还要校验与 scopes 的对应关系，不能允许“只给 statistics:read 却挂 allowed_request_ids”这类表面合法、实际无效的配置混入库中。
- 最好提供可机器读取的 schema 接口，把 scopes、resource_limits、依赖 scopes 和枚举值都暴露出来，避免 WebUI/CLI 再重复维护一份权限矩阵。
- 支持过期时间和撤销。当前 Go 重构分支的用户侧应用 API Key 创建接口已支持 `expires_at`，过期时间必须晚于当前时间。
- 记录 `last_used_at`，但写入频率要节流，避免高频请求导致 SQLite 写放大。
- 对 `requests:respond` 加限流，避免外部自动化错误循环刷屏。
- 对 `model_keys:write` 加数量限制和频率限制。当前 Go 重构分支先支持 `max_model_keys`，按当前用户未撤销虚拟模型 key 数量限制创建；后续再接系统/用户默认上限。

当前 Go 重构分支已落地的最小接口形状：

- `GET /api/app/automation-rules`：需要 `automation:read`，返回当前用户自己的规则数组；如果配置了 `allowed_automation_rule_ids`，只返回允许的规则。
- `GET /api/app/automation-rules/schema`：需要 `automation:read`，返回当前后端支持的自动化规则 schema metadata，供外部程序做表单/配置校验。
- `PUT /api/app/automation-rules`：需要 `automation:write`，请求体为 `{ "rules": [...] }`。未配置 `allowed_automation_rule_ids` 时替换当前用户全部规则；配置后只允许替换指定规则，提交未授权规则 id 返回 `403`。
- `GET /api/app/requests*`：当前已开始同时检查 `allowed_request_ids`、`allowed_conversation_ids` 和 `allowed_virtual_models`。列表接口会直接过滤未授权请求；详情和响应接口命中未授权对象时返回 `403`。
- `GET /api/app/conversations*`：当前已开始同时检查 `allowed_conversation_ids` 和 `allowed_virtual_models`。列表接口会直接过滤未授权会话；消息读取命中未授权对象时返回 `403`。
- `max_requests_per_minute`：当前已按应用 API Key 在单 Go 进程内实现 1 分钟窗口限流；值为空或 `0` 表示不限流。超限请求返回 `429 app api key rate limited`，并记录 `error_code=rate_limited`。后续多实例部署需要迁移到 Redis 或数据库限流器。
- `allowed_source_ips`：当前已在应用 API 鉴权后、scope 校验前执行。未配置时不限制；配置后请求来源 IP 不匹配则返回 `403 app api key source ip forbidden`，并记录 `error_code=source_ip_forbidden`。
- `GET /api/app/model-keys`：需要 `model_keys:read`，返回当前用户自己的虚拟模型 API Key；如果配置了 `allowed_model_key_ids`，只返回允许的 key。
- `POST /api/app/model-keys`：需要 `model_keys:write`，请求体包含 `name`、`model`；如果配置了 `allowed_virtual_models`，只能为允许的虚拟模型创建 key；如果配置了 `max_model_keys`，当前用户未撤销虚拟模型 key 数量达到上限时返回 `403`。
- `DELETE /api/app/model-keys/{key_id}`：需要 `model_keys:delete`，只能删除当前用户自己的 key；如果配置了 `allowed_model_key_ids`，只能删除允许的 key。
- `GET /api/app/statistics/summary`：需要 `statistics:read`，当前返回请求态势摘要和自动化规则命中数；首版不返回 token、价格、平均耗时等需要额外计量的数据，避免给外部自动化暴露误导性指标。
- 虚拟模型 key 使用 `sk-` 前缀和可解密密文保存；应用 API Key 使用 `ak-` 前缀和 hash 保存。两套鉴权中间件完全分离，`ak-` 不能访问模型兼容入口，`sk-` 不能访问 `/api/app/*`。
- 自动化规则当前以 `automation_rules.rule_json` 保存完整规则 JSON，同时单独保存 `user_id`、`id`、`enabled`、`created_at`、`updated_at`；当前最小自动执行内核、WebUI 配置接口和应用 API 已开始复用同一张表与 `AutomationRuleService`，避免规则格式分叉。
- 当前最小自动执行内核已开始支持 `conditions.contains`、`conditions.excludes`、`match_type=substring|exact` 和 `action.type=output_text`，命中后会直接复用统一 turn complete 路径结束 pending turn，而不是额外实现一套旁路写消息逻辑。
- 所有应用 API 响应都使用稳定 JSON，方便脚本调用。
- `complete` 和 `abort` 与 Web 控制台操作共享同一个 Turn Manager 状态机，避免双写和竞态。

安全边界：

- 应用 API Key 默认不开启通配 scope；创建时用户必须选择权限。
- 应用 API Key 默认不配置通配 resource；UI 应鼓励用户绑定具体虚拟模型、自动化规则或动作集合。
- 高风险 scope 如 `automation:write`、`requests:respond`、`model_keys:write`、`model_keys:delete` 应在 UI 中突出提示。
- 应用 API Key 明文只展示一次。
- 审计日志记录 key id、key prefix、scope、owner、路径、状态码、错误类别，不记录请求体里的敏感内容。
- 如果用户禁用或删除应用 API Key，后续请求必须立即失效。

当前重构分支的最小落地策略：

- 先完成 `user_app_api_keys` 存储、`ak-` 前缀密钥哈希校验、scope 校验和 `allowed_request_actions` 校验。
- 先打通 `/api/app/requests*` 这一组最关键的自动化调试接口。
- 先在当前单用户 Lab 语境下通过 `owner_id` 做 owner 隔离验证，后续接入正式 session / 用户体系后沿用相同 owner 约束。
- 会话侧的应用 API Key 管理接口（`/api/user/app-api-keys`）已可复用统一 session actor；后续仍需继续补审计查询页、频率限制和 resource limits 细项 UI。

## 8. API 兼容计划

### 7.1 路由兼容清单

Go 版首个可替换版本必须覆盖：

- `GET /api/health`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/session`
- `GET /api/auth/oidc/config`
- `GET /api/auth/oidc/login`
- `GET /api/auth/oidc/callback`
- `POST /api/auth/oidc/unlink`
- `GET /api/auth/register/config`
- `POST /api/auth/register/send-code`
- `POST /api/auth/register`
- `GET /api/auth/password/config`
- `POST /api/auth/password/send-code`
- `POST /api/auth/password/reset`
- `GET /api/auth/totp/setup`
- `POST /api/auth/totp/confirm`
- `POST /api/auth/totp/reset`
- `GET /api/user/config`
- `POST /api/user/config`
- `POST /api/user/password`
- `GET /api/user/app-api-keys`
- `POST /api/user/app-api-keys`
- `DELETE /api/user/app-api-keys/{key_id}`
- `GET /api/user/upstream-assistant/config`
- `POST /api/user/upstream-assistant/config`
- `DELETE /api/user/upstream-assistant/config`
- `POST /api/workspace/tool-call/assist`
- `GET /api/app/me`
- `GET /api/app/requests`
- `GET /api/app/requests/{request_id}`
- `POST /api/app/requests/{request_id}/delta`
- `POST /api/app/requests/{request_id}/complete`
- `POST /api/app/requests/{request_id}/abort`
- `GET /api/app/conversations`
- `GET /api/app/conversations/{conversation_id}/messages`
- `GET /api/app/automation-rules`
- `PUT /api/app/automation-rules`
- `GET /api/app/model-keys`
- `POST /api/app/model-keys`
- `DELETE /api/app/model-keys/{key_id}`
- `GET /api/app/statistics/summary`
- `GET /api/user/model-api-keys`
- `POST /api/user/model-api-keys`
- `DELETE /api/user/model-api-keys/{key_id}`
- `GET /api/admin/users`
- `GET /api/admin/users/{user_id}/history`
- `POST /api/admin/users`
- `DELETE /api/admin/users/{user_id}`
- `PUT /api/admin/users/{user_id}/password`
- `POST /api/admin/send-test-email`
- `GET /api/admin/runtime/summary`
- `GET /api/admin/runtime/memory`
- `GET /api/admin/runtime/connections`

说明：

- 旧 `/api/user/api-keys*` 路由不再作为 Go 版目标兼容面。核心原因不是路径命名，而是旧前端要求服务端可重复展示完整明文 key，这与应用 API Key 必须 hash 存储、只在创建时返回一次的长期安全模型直接冲突。
- WebUI 后续应统一改接 `/api/user/app-api-keys*`，列表只展示名称、前缀、scope、资源限制、创建/使用时间；原“生成 key”按钮可以保留，但应作为创建表单的本地辅助，而不是独立服务端接口。
- `GET /api/admin/runtime/queue`
- `GET /api/admin/runtime/settings`
- `PUT /api/admin/runtime/settings`
- `POST /api/admin/runtime/gc`
- `GET /api/admin/storage/summary`
- `GET /api/admin/storage/users`
- `GET /api/admin/storage/orphans`
- `POST /api/admin/storage/orphans/cleanup`
- `POST /api/admin/storage/cleanup`
- `GET /api/conversations`
- `POST /api/conversations`
- `GET /api/conversations/{conversation_id}`
- `GET /api/conversations/{conversation_id}/messages`
- `DELETE /api/conversations/{conversation_id}`
- `POST /api/conversations/prune`
- `POST /api/conversations/{conversation_id}/abort`
- `POST /api/conversations/{conversation_id}/rename`
- `POST /api/conversations/{conversation_id}/respond`
- `POST /api/conversations/{conversation_id}/stream/delta`
- `POST /api/conversations/{conversation_id}/stream/complete`
- `POST /api/chat/output/complete`
- `POST /api/chat/output/delta`
- 后续前端改造建议优先改接 path-based 接口，而不是继续围绕 `/api/chat/output/*` 做扩展：
  - 非流式人工回复：`POST /api/conversations/{conversation_id}/respond`
  - 流式人工回复：`POST /api/conversations/{conversation_id}/stream/delta` + `POST /api/conversations/{conversation_id}/stream/complete`
  - 终止请求：`POST /api/conversations/{conversation_id}/abort`
- 这样可以把“非流式一次完成”和“流式逐段输出”拆成两条明确语义路径，避免后续前端把所有手工回复都硬塞进流式草稿模型。
- `GET /api/config/models`
- `POST /api/config/models`
- `DELETE /api/config/models/{model_id}`
- `GET /api/config/stream-heartbeat`
- `POST /api/config/stream-heartbeat`
- `GET /api/config/automation-rules`
- `POST /api/config/automation-rules`
- `GET /api/config/system`
- `POST /api/config/system`
- `GET /api/config/app-info`
- `GET /api/uploads/imgs/{filename}`
- `GET /api/uploads/imgs/usage`
- `GET /api/statistics/summary`
- `GET /models`
- `GET /v1/models`
- `POST /responses`
- `POST /v1/responses`
- `POST /chat/completions`
- `POST /v1/chat/completions`
- `POST /messages`
- `POST /v1/messages`

WebSocket 路由需要以当前前端 `resolveWebSocketUrl('/api/ws')` 为准，Go 版保持 `/api/ws`。

Lab 模式额外路由只在 `chatapi lab` 中注册，不能出现在生产 `serve` 模式：

- `GET /lab`
- `GET /lab/events`
- `GET /lab/requests`
- `GET /lab/requests/{request_id}`
- `POST /lab/requests/{request_id}/delta`
- `POST /lab/requests/{request_id}/complete`
- `POST /lab/requests/{request_id}/abort`

### 7.2 兼容验收方法

每个路由应有三类测试：

- handler 单元测试：状态码、响应字段、权限校验。
- service 单元测试：业务规则、边界条件、并发。
- compatibility 测试：用现有前端和 `tests/` 下 SDK 模拟脚本跑通。

关键兼容测试：

- 老 SQLite 数据库启动 Go 服务后可登录。
- 旧用户密码登录成功后可升级 hash。
- 当前 Go 重构分支已先落地本地 users 表登录、签名 cookie session 和 OIDC Authorization Code 最小闭环；OIDC 成功后会创建或绑定本地用户，并建立同一类 ChatAPI session。
- OIDC 登录邮箱命中 `CHATAPI_OIDC_ADMIN_EMAILS` 且 `email_verified=true` 时可获得 admin role；未验证邮箱不能获得 admin。
- OIDC 关闭时相关入口不泄露 provider 配置和 client secret。
- 旧 API Key 可继续访问 `/v1/*`。
- 应用 API Key 可按 scope 读写自己的自动化规则、查看请求、提交回复、管理自己的虚拟模型 API Key；虚拟模型 API Key 和应用 API Key 不能互相替代。
- 管理员后台可查看运行时资源、连接数和用户存储占用，可设置配额并执行/定时执行清理。
- 浏览器控制台 WebSocket 连接有预留名额，不能被同用户 API/SSE 连接完全挤出。
- Web 控制台创建会话，OpenAI SDK 发送请求，人工输出后 SDK 收到正确响应。
- Lab 模式跳过登录后，浏览器可直接看到 SDK 请求体，并可手动流式输出。
- Tool Call 标签页点击“请求大模型”后，可通过三套上游协议生成说明文字和 Tool Call 表单草稿，且不会自动发送。
- 流式请求中途 abort，SDK 收到符合当前错误格式的事件。
- 自动化规则命中时，不需要人工输出即可返回。

## 9. 安全设计

### 8.1 默认安全策略

- 默认管理员密码仍兼容 `.env`，但启动时如果是 `change-me` 必须打印高危告警。
- Session secret 与 master key 分离。`CHATAPI_SESSION_SECRET` 显式配置优先；如果未配置，serve 启动会自动生成并持久化到 `config` 表 key `security.session_secret`。Lab 模式使用进程内不安全默认值，不写入数据库。
- 登录失败应有限流，避免暴力破解。当前 Go 重构分支已先对本地用户名密码登录做进程内 username + 直连 IP 失败限流，连续 5 次失败后锁定 1 分钟，成功登录会重置失败计数；后续多实例部署需要接入共享限流器。
- OIDC callback 必须校验 state、nonce、issuer、audience、expiry 和签名。
- OIDC client secret 只允许来自环境变量，不能写入数据库、日志、前端响应或系统配置接口。
- OIDC 管理员识别必须同时满足邮箱命中 `CHATAPI_OIDC_ADMIN_EMAILS` 和 `email_verified=true`。
- 虚拟模型 API Key 使用服务端可解密密文存储，便于调试场景回看和复制；密文加密依赖 master key，生产部署必须备份 master key。
- 应用 API Key 只存 hash，必须按 scope 和 resource limits 授权，不能访问管理员接口、系统配置、其他用户数据；只有带 `model_keys:*` scope 且通过 resource limits 时才能管理自己的虚拟模型 API Key。
- 上游模型 API Key 默认只保存在浏览器本地，不写入服务端数据库；如未来启用服务端上游代理，必须使用加密存储和脱敏展示。
- API Key 管理接口只能通过 session 访问。当前 Go 重构分支已先支持 Lab actor 和本地管理员 session actor；serve 模式 session mutation 已加 Origin/Referer CSRF 校验。
- 管理员接口必须 session 登录且 role 为 admin。当前 Go 重构分支已支持 serve 模式 `admin` 本地 session，且继续拒绝 `ak-` / `sk-` bearer key。
- 所有上传、ntfy URL、邮件目标都做输入校验。
- 管理员资源监控接口必须只允许 admin session 访问，不能通过应用 API Key 访问。
- 手动 GC、存储清理、VACUUM 等操作必须审计，并提供防重复执行保护。
- 错误响应不泄露堆栈和 SQL。
- Lab 模式虽然会跳过登录/鉴权，但必须由显式命令行参数启动，并在日志和页面上标记为非生产模式；生产 `serve` 模式不能读取该开关绕过鉴权。

### 8.2 SSRF 防护

保留 ntfy 私网 URL 策略：

- `disabled`
- `admin`
- `all`

实现时统一通过 URL safety 包判断：

- 拒绝 localhost、loopback、link-local、private IP、metadata 地址。
- 域名解析后也要检查 IP。
- HTTP client 禁止自动跟随到私网地址，或在 redirect hook 中重新校验。
- 禁止访问 cloud metadata 常见地址和域名，例如 `169.254.169.254`、`metadata.google.internal` 等。
- 连接前和 redirect 后都必须校验最终 IP；DNS rebinding 风险较高的场景应缓存一次解析结果并固定连接目标。
- 服务端上游模型代理如果未来实现，默认必须启用同一套 URL safety，并优先使用管理员 allowlist；只有 Lab 模式显式放宽时才允许 localhost/内网地址。

### 8.3 审计日志

建议记录以下事件：

- 登录成功/失败
- OIDC 登录成功/失败、身份绑定/解绑、allowlist 拒绝、管理员 role 自动同步
- 虚拟模型 API Key 创建/删除
- 应用 API Key 创建/删除/撤销、scope/resource 拒绝、自动化接口调用、虚拟模型 API Key 管理
- 应用 API Key 触发的 delta、complete、abort、自动化规则变更、虚拟模型 API Key 创建/删除
- 自动化规则自动完成命中，包括命中规则 id、会话 id、协议类型和模型名
- 上游模型辅助调用成功/失败、浏览器本地配置创建/删除、服务端代理 URL safety 拒绝
- 管理员创建/删除用户、重置密码
- 系统配置变更
- 资源治理配置变更、手动 GC、存储清理、自动清理结果
- TOTP 开启/重置
- 上传失败和超限
- ntfy/email 发送失败

当前 Go 重构分支已开始落地通用 `audit_logs` 表和 `AuditService`：

- 已记录图片上传成功/失败，包括 actor、来源、文件元数据、结果和错误类别。
- 已记录管理员手动 GC，包括 GC 后内存摘要。
- 已记录管理员存储 cleanup dry-run 预览和实际执行，包括保留策略、候选会话数、候选消息数、估算可回收字节数，以及执行时的实际删除会话/消息数；已记录管理员设置/删除单用户存储配额覆盖和手动 SQLite vacuum。
- 已记录用户普通配置更新，审计 metadata 只记录更新的配置 key，不记录完整配置值。
- 已记录管理员系统配置更新，审计 metadata 只记录更新的配置 key，不记录完整配置值。
- 已记录自动化规则自动完成命中，当前事件类型为 `automation.rule`，`action=auto_complete`，`outcome=success`，metadata 会附带 `conversation_id`、`request_format`、`model`。
- 已记录自动化规则读取或自动完成失败，当前同样使用 `event_type=automation.rule`、`action=auto_complete`、`outcome=failure`，metadata 会补充 `reason=list_rules_failed|complete_pending_failed` 和错误摘要。
- `GET /api/admin/audit/logs` 已提供通用审计日志查询，支持 `limit`、`event_type`、`actor_user_id` 基础过滤，仅允许 admin session actor 访问；默认查询范围是 `audit_logs`，传 `include_app_api=1` 时会把 `app_api_key_audit_logs` 映射为 `event_type=app_api.request` 的统一审计条目并合并返回。
- 审计 metadata 会过滤包含 password、secret、token、authorization、key 的字段，避免误写敏感值。
- 应用 API Key 请求当前仍写入 `app_api_key_audit_logs`，用于保留 key id、scope 拒绝和状态码等细节；管理员审计查询可通过 `include_app_api=1` 聚合查看这些请求，后续再扩展更细的分页游标和 source 过滤。

仍待补齐的审计事件包括 OIDC 角色变更/绑定发起细节、ntfy/email 发送失败、上游模型辅助调用，以及自动化规则逐条规则级跳过等更细事件；OIDC 登录成功/失败、用户 OIDC 身份解绑、本地 session 登录/登出、管理员运行时/存储/用户/系统配置操作、用户配置、上传、API Key 管理，以及自动化规则自动完成命中/失败已开始写入 `audit_logs`。

审计日志应独立于普通运行日志等级：即使 `CHATAPI_LOG_LEVEL=warn`，关键安全事件仍应写入 audit channel。审计日志只记录必要元数据和结果，不记录完整请求体、密钥、密码、OIDC token 或上游模型输出全文。

## 10. 可运营设计

### 9.1 部署形态

必须支持：

- 本地二进制运行
- Docker Compose
- systemd
- Nginx 反代前端和 API
- Go 后端直接托管 `frontend/dist`
- Lab 模式默认 SQLite
- 正式部署推荐 PostgreSQL

推荐目录：

```text
/opt/chatapi/
  chatapi
  .env
  data/
    uploads/imgs/
  web/
    dist/
```

SQLite 本地/lab 模式可额外使用：

```text
/opt/chatapi/data/chatapi.sqlite3
```

### 9.2 命令行

推荐支持：

```bash
chatapi serve
chatapi lab --host 127.0.0.1 --port 5000 --open-browser
chatapi lab --host 0.0.0.0 --port 5000 --allow-remote-lab --open-browser
chatapi lab --host 0.0.0.0 --port 5000 --allow-remote-lab --password "dev-password"
chatapi migrate up
chatapi migrate status
chatapi migrate down
chatapi migrate-db sqlite-to-postgres --sqlite ./data/chatapi.sqlite3 --postgres "$CHATAPI_DATABASE_URL"
chatapi admin reset-password --username admin
chatapi setup
chatapi doctor
chatapi config print --redact
chatapi db check
chatapi oidc test
chatapi smtp test
chatapi version
```

首版至少应包含 `serve`、`lab`、`doctor`、`db check`、`config print --redact` 和 `version`；migration 能力必须可由启动流程和独立 CLI 调用。`debug` 可以作为 `lab` 的兼容别名，但文档和 UI 文案统一使用 Lab 模式。当前 Go 重构分支已先落地 `serve`、`lab`、`doctor`、`db check`、`config print --redact`、`oidc test`、`smtp test`、`setup`、`version`，以及 SQLite/PostgreSQL 的 `migrate up|status|down --force` 和 `migrate-db sqlite-to-postgres` 命令；SQLite 和 PostgreSQL 都已开始支持 bootstrap 后继续应用注册式增量 migration，后续再支持按版本回滚。

首次启动向导：

- 当数据库为空、没有管理员账号、没有 session secret 或缺少必要 master key 时，`serve` 应进入明确的 bootstrap 状态，而不是静默使用危险默认值。
- 支持 `chatapi setup` 或首次访问 `/setup` 完成初始化；仅在无管理员账号时可用。当前 Go 重构分支已先落地 CLI 版 `chatapi setup`：默认只输出 `.env` 初始化模板并生成 `CHATAPI_MASTER_KEY`、`CHATAPI_SESSION_SECRET`、`CHATAPI_ADMIN_PASSWORD`，`--write-env` 写入 `backend/.env`，已有文件需要 `--force` 才覆盖；Web `/setup` 仍待后续用户体系一起实现。
- setup 完成后立即关闭 bootstrap 能力，并写入审计日志。
- 如果检测到 `CHATAPI_ADMIN_PASSWORD=change-me`、未配置生产 master key、OIDC callback URL 与外部访问 URL 不一致，应在 setup 和 `doctor` 中给出明确错误或高危警告。

配置诊断命令：

- `chatapi doctor`：检查配置、数据库、migration dirty 状态、静态前端目录、uploads 权限、session/master key、SQLite 降级阈值和端口监听建议。
- 当前 Go 重构分支的 `chatapi doctor [serve|lab]` 输出 JSON 报告，包含 `ok`、`summary` 和 `items`；已覆盖配置校验错误、serve 模式 master key、默认管理员密码、SQLite serve 降级提示、Lab 远程暴露风险、前端 dist 目录、CORS wildcard、OIDC 私密 RP 必填项、OIDC HTTPS redirect、`openid` scope、realtime 连接预留配置和日志等级。存在 `error` 级诊断时命令以非零状态退出；`warn` 只提示不阻止。
- 当前 `doctor` 尚不连接数据库，不检查 migration dirty、schema version、SQLite WAL、PostgreSQL 连接池和 uploads 归属；数据库状态由 `chatapi db check` 负责。
- `chatapi config print --redact`：打印最终配置，敏感字段脱敏。当前 Go 重构分支已支持可选模式参数 `serve|lab`，默认按 `serve` 解析；敏感字段仅显示 `<redacted>` 或空字符串。
- `chatapi db check`：检查数据库连接、schema version、migration 历史、SQLite WAL 状态或 PostgreSQL 连接池配置。
- 当前 Go 重构分支的 `chatapi db check` 已支持 SQLite 和 PostgreSQL，输出 JSON，包含 `schema_version`、`migration_dirty`、`migration_lock`、`created_by`、`last_migrated_at`、`applied` migration 列表；SQLite 额外返回 `sqlite.database`、`sqlite.wal`、`sqlite.shm` 的 path / exists / bytes。命令会按 driver 走统一 runtime store 打开并在需要时执行幂等 bootstrap；如果 dirty 为 true、数据库不可打开或 PostgreSQL DSN 缺失，命令以非零状态退出。
- `chatapi migrate up`：当前对 SQLite 和 PostgreSQL 执行幂等 schema 初始化并输出 JSON status；两种数据库在 bootstrap 后都会继续按顺序执行已注册的增量 migration。
- `chatapi migrate status`：当前读取 SQLite 或 PostgreSQL 的 migration status，不执行 bootstrap；如果 schema 尚未初始化或 dirty 为 true，以非零状态退出。
- `chatapi migrate down --force`：当前 SQLite 和 PostgreSQL 版本都会删除 bootstrap 管理的所有表，属于测试/本地重置用危险操作；不带 `--force` 会拒绝执行。后续引入正式 migration 文件后再支持按版本回滚。
- `chatapi oidc test`：当前已拉取 discovery document，校验 issuer、authorization/token/JWKS endpoint、client id、client secret 和 redirect URL 配置；输出 JSON，只返回 client secret 是否配置，不打印 secret。
- `chatapi smtp test`：当前支持 `--dry-run` 配置校验、`--connect-only` 连接/TLS/Auth 握手诊断和 `--to` 测试邮件发送，输出 TLS/auth 诊断。

### 9.3 Lab 模式

Go 重构版应提供一个类似 Jupyter Notebook 的 Lab 模式，用于开发 Agent、调试 SDK 请求、手工构造流式输出。该模式不是生产部署模式，不使用完整控制台功能。

启动方式：

```bash
chatapi lab
chatapi lab --host 0.0.0.0 --port 5000
chatapi lab --host 0.0.0.0 --port 5000 --allow-remote-lab --password "dev-password"
chatapi lab --port 0
chatapi lab --no-open-browser
```

默认行为：

- 默认监听 `127.0.0.1`，默认端口使用正式服务默认端口或 `5000`；`--port 0` 表示由系统分配空闲端口。
- 默认启动时生成一次性 Lab 访问 token，自动打开默认浏览器，打开地址为带 token 的 Lab 工作台首页。
- 如果用户传入 `--password <password>`，则使用固定 Lab 口令保护页面，不再生成 URL token；浏览器打开后进入轻量口令页或通过一次本地 session cookie 进入工作台。
- 启动日志必须打印监听地址、浏览器地址、OpenAI/Anthropic 兼容 base URL。
- 默认使用 SQLite 和临时数据目录；允许通过 `--data-dir` 指定持久化目录。
- 不要求登录，不要求 API Key，不启用注册、用户管理、系统设置、OIDC、TOTP、SMTP 等完整后台能力。

安全边界：

- 该模式必须通过独立的 `chatapi lab` 命令启动，不能通过 `.env` 或生产 `serve` 命令开启。
- 页面顶部和日志必须提示“Lab 模式，已跳过生产鉴权”。
- 如果 host 不是 `127.0.0.1` 或 `localhost`，必须要求显式参数，例如 `--allow-remote-lab`；启动日志必须打印更强的风险提示。
- Lab 访问 token 和 `--password` 都不是生产账号体系，但可以避免误访问；远程 Lab 下必须启用 token 或 `--password`，不能提供完全裸露模式。
- `--password` 不写入数据库，不复用生产用户密码，不进入 `.env` 长期配置；实现时只在进程内保存 hash，日志不得打印明文。
- 不读取生产 session secret，不创建管理员账号，不修改生产用户表，除非用户显式指定已有 `--data-dir`。
- 不暴露管理员接口、用户管理接口、系统配置写接口和 API Key 管理接口。

Lab 模式保留的核心能力：

- 接收并展示请求体：
  - `POST /v1/responses`
  - `POST /v1/chat/completions`
  - `POST /messages`
- 自动创建一个默认调试会话，浏览器打开后直接进入对话/请求工作台。
- 展示请求 headers、query、body、解析后的协议类型、model、messages、tools、stream 参数。
- 支持手动输入输出内容。
- 支持手动流式输出：逐段追加 delta、发送 tool call、发送 thinking、完成响应、终止响应。
- 支持一键复制当前请求对应的 `curl`，包括 URL、headers、body。
- 支持复制 SDK base URL 和示例代码片段。
- 支持清空当前调试会话和导出请求/响应 JSON。
- 支持 WebSocket 或页面内事件同步，使浏览器能看到请求进入等待态。

Lab 模式建议路由：

- `GET /lab`：Lab 工作台首页。
- `GET /lab/events` 或 `/api/ws`：Lab 事件同步。
- `GET /lab/requests`：请求列表。
- `GET /lab/requests/{request_id}`：请求详情。
- `POST /lab/requests/{request_id}/delta`：追加手动输出。
- `POST /lab/requests/{request_id}/complete`：完成响应。
- `POST /lab/requests/{request_id}/abort`：终止响应。
- `POST /lab/requests/{request_id}/copy-curl`：生成 curl 文本，也可以由前端本地生成。

实现建议：

- Lab 模式复用 protocol adapter、pending turn manager、SSE encoder 和 manual output controller，不复制另一套协议实现。
- `lab` 的 `{request_id}` 控制链路应与后续应用 API 共享同一个 request resolver：先按 `request_id` 找到 pending turn / conversation，再统一翻译成 `TurnControlCommand` 执行。
- 使用独立的 `LabAuth` 或路由分组绕过鉴权，避免在正式 auth 中间件里加入大量例外。
- 前端可以复用现有对话组件，但应构建一个更轻量的 debug shell，只保留请求查看、手工输出、curl 复制等控件。
- 自动打开浏览器可使用小型跨平台库，或按 OS 调用 `xdg-open`、`open`、`rundll32`；失败时只打印 URL。
- Lab 模式也要跑协议兼容测试，确保它和正式服务返回格式一致。

### 9.4 配置示例

发布包必须包含：

- `.env.example`
- `deploy/docker-compose.yml`
- `deploy/systemd/chatapi.service`
- `deploy/nginx/chatapi.conf`

### 9.5 备份和恢复

文档必须提供：

- 停服备份 SQLite 和 uploads 目录。
- 在线备份使用 SQLite backup API 或 `VACUUM INTO`。
- 恢复时版本不能低于备份时的 schema version。
- 升级前自动提示备份。
- `chatapi doctor` 应展示 `db_meta.schema_version`、`app_version`、dirty 状态、最近迁移时间。
- 如果数据库处于 dirty 状态，服务应拒绝启动，避免在半升级状态继续写入数据。

## 11. 开源发布设计

### 10.1 仓库结构

建议迁移完成后的顶层结构：

```text
backend-go/             # 迁移阶段可先用此目录，稳定后再决定是否替换 backend/
frontend/
docs/
deploy/
scripts/
tests/
README.md
EN-README.md
LICENSE
```

迁移阶段不要直接删除 Python 后端。建议：

1. 新增 `backend-go/`。
2. Go 后端达到兼容验收后，在 README 中切换推荐部署方式。
3. 保留 Python 后端一个过渡版本。
4. 下一个大版本再考虑移除或归档 Python 后端。

### 10.2 版本策略

推荐语义化版本：

- `v0.x`：迁移开发期，API 仍可能调整。
- `v1.0.0`：Go 后端成为默认后端，公开 API 兼容承诺生效。
- patch：bugfix 和安全修复。
- minor：向后兼容功能。
- major：破坏性变更。

### 10.3 Release 产物

GitHub Release 应包含：

- `chatapi-linux-amd64.tar.gz`
- `chatapi-linux-arm64.tar.gz`
- `chatapi-darwin-amd64.tar.gz`
- `chatapi-darwin-arm64.tar.gz`
- `chatapi-windows-amd64.zip`
- `checksums.txt`
- Docker image：`ghcr.io/<owner>/chatapi:<version>`

每个 tar 包包含：

- `chatapi` 二进制
- `web/dist` 或说明如何下载前端构建产物
- `.env.example`
- `LICENSE`
- 简短部署说明

### 10.4 CI/CD

GitHub Actions 必须包含：

- Go lint：`go vet`、`staticcheck`
- Go test：单元测试、race test、coverage
- 前端 lint/build
- 兼容测试：启动 Go 服务后跑 `tests/` SDK 脚本
- Docker build
- Release 构建和 checksum

建议新增 Makefile：

```makefile
make dev
make test
make test-race
make lint
make build
make docker-build
make release-snapshot
```

## 12. 测试策略

### 11.1 单元测试

重点覆盖：

- protocol adapter
- pending turn 并发状态机
- automation rules
- auth/session/csrf/OIDC
- Kirari OIDC token exchange、refresh、ID token 校验、token store 接口
- repository migration
- repository contract tests for SQLite and PostgreSQL。当前已新增 `internal/repository/storetest`，SQLite/PostgreSQL 已覆盖 users、user_identities、config、user_configs、应用 API Key、应用 API Key 审计日志、虚拟模型 API Key、通用审计日志、自动化规则、uploads、storage quotas、deletion failure queue，以及 conversations/messages/requests/pending turn 的最小状态转换；后续继续补更高并发和完整协议层集成用例。
- URL safety
- upload path safety
- config validation
- upstream assistant browser-side adapter、structured output schema validation、CORS/error handling
- Kirari LLM client meta parsing、chat completions request、stream handling、token refresh failure
- app api key hash storage、scope/resource enforcement、owner isolation
- storage quota estimation、cleanup policy、realtime connection reservation

### 11.2 集成测试

使用临时 SQLite 文件和 PostgreSQL service container 启动完整服务，覆盖：

- 用户登录和 session
- OIDC login/callback、用户绑定、allowlist 和管理员邮箱同步
- KirariNetwork OIDC 连接、token 加密存储、refresh token 续期、断开连接
- Lab 模式请求捕获、curl 生成、手动流式输出、abort
- upstream assistant 浏览器本地配置、调用上游模型、说明文字展示、Tool Call 表单草稿填充
- KirariNetwork `/api/llm/meta` 模型能力读取和缓存、`/api/llm/chat/completions` 非流式/流式调用
- API Key 访问
- 应用 API Key 读写自动化规则、读取请求、delta/complete/abort 回复、创建/删除虚拟模型 API Key、scope/resource 拒绝
- 会话 CRUD
- 人工输出完整链路
- SSE 流式链路
- WebSocket 实时同步
- WebUI WebSocket 预留连接不被同用户 API/SSE 连接挤出
- 图片上传和读取
- 用户存储配额、自动清理旧会话、孤儿图片清理

### 11.3 兼容测试

保留现有 `tests/` Python SDK 测试，并新增 Go 服务启动脚本。

建议测试矩阵：

- `stream=false` / `stream=true`
- OpenAI Responses / Chat Completions / Anthropic Messages
- 文本 / tool call / thinking / image
- 人工输出 / 自动化规则输出 / abort / timeout
- 上游模型辅助：Responses / Chat Completions / Anthropic Messages 三套协议
- KirariNetwork delegated LLM upstream：meta、chat completions、stream、refresh token 过期重连

### 11.4 并发和稳定性测试

至少覆盖：

- 同一用户 100 个并发 pending turn。
- 多用户 WebSocket 连接和断线重连。
- 同用户 API/SSE 连接打满时，浏览器控制台仍能建立至少一个 WebSocket。
- SSE 客户端中途断开。
- SQLite 写入压力。
- PostgreSQL 连接池、事务、并发状态转换压力。
- 自动化规则高频命中。
- 大量会话和图片下的存储占用估算与清理任务耗时。

使用：

- Go `-race`
- `go test -run TestX -count=100`
- `vegeta` 或 `wrk` 做 HTTP 压测

## 13. 分阶段路线图

### Phase 0：基线冻结和契约记录

输出：

- 当前 API 路由清单。
- 当前 SQLite schema 快照。
- 当前响应样例。
- 当前兼容测试脚本可稳定运行。

验收：

- Python 后端现有测试通过。
- 至少保存一份旧数据库 fixture。

### Phase 1：Go 项目骨架

工作：

- 新增 `backend-go/`。
- 初始化 Go module、配置加载、日志、路由、健康检查。
- 初始化 `serve` / `lab` / `version` 命令行结构。
- 加入 Makefile、CI 初版、Dockerfile。

验收：

- `go test ./...` 通过。
- `chatapi serve` 可启动 `GET /api/health`。
- `chatapi lab --port 0 --no-open-browser` 可启动并打印 Lab 工作台 URL。

### Phase 2：Repository 和 migration

工作：

- 实现 migration。
- 实现 `db_meta` 版本键值表、migration 历史表、dirty 状态和迁移锁。
- 实现 SQLite 和 PostgreSQL 两套 users、config、conversations、messages repository。
- 能读取旧数据库。
- 提供 SQLite 到 PostgreSQL 迁移工具。

验收：

- 旧 fixture 自动迁移成功。
- 跨多个版本 fixture 可按顺序逐步升级到最新 schema。
- migration 失败会写入 dirty 状态并阻止服务启动。
- repository contract tests 在 SQLite 和 PostgreSQL 下都覆盖 CRUD、权限隔离和并发状态转换。
- `chatapi migrate-db sqlite-to-postgres` 可迁移旧 SQLite fixture 到 PostgreSQL。

### Phase 3：认证和控制台 API

工作：

- 登录、登出、session、注册、密码重置、TOTP、OIDC RP 登录。当前已先落地本地 users 表登录、`.env` admin 恢复登录、登出、session、注册、邮箱验证码发送、密码重置、TOTP 最小闭环、OIDC RP 最小登录闭环和用户侧 OIDC 身份查看/解绑；GeeTest 校验、验证码后台清理任务和 OIDC 绑定发起流程仍待补齐。
- 用户配置、系统配置、虚拟模型 API Key、应用 API Key、上游模型辅助的浏览器本地配置、KirariNetwork 连接、管理员用户管理。当前用户配置已先支持 `GET/POST /api/user/config` 按当前 actor 读写普通 JSON object 偏好；系统配置已先支持 `GET/POST /api/admin/config` 做表驱动持久化；管理员用户管理已先支持列表、创建、重置密码和停用用户；用户详情历史、物理删除策略和 OIDC 身份管理后台仍待补齐。
- 当前前端设置页仍有几处必须跟随 Go 契约一起调整：`/api/user/api-keys*` 需要切到 `/api/user/app-api-keys*` 并改成一次性展示明文 key；`/api/user/password`、`/api/admin/send-test-email` 仍待后端补齐或在前端切到新的配置/诊断流程。
- CSRF、CORS、Cookie 策略。当前已先落地 session mutation Origin/Referer 校验和 HttpOnly SameSite cookie。

验收：

- 前端登录和设置页可用。
- 旧密码登录成功并升级 hash。
- OIDC 可选开启，私密 RP 配置只来自 `.env`，callback 后可建立本地 session，并按 `CHATAPI_OIDC_ADMIN_EMAILS` 同步管理员 role。
- KirariNetwork 连接可完成 authorization code 授权，token 加密入库，可读取 `/api/llm/meta` 并缓存模型价格、可用性和额度。

### Phase 4：会话、上传、统计、实时同步

工作：

- 会话 CRUD。
- 图片上传和读取。
- WebSocket hub。
- 统计接口。

验收：

- 前端工作台可正常展示、重命名、删除、同步会话。
- 多窗口实时同步可用。

### Phase 5：协议兼容和 pending turn

工作：

- OpenAI Responses adapter。
- Chat Completions adapter。
- Anthropic Messages adapter。
- pending turn manager。
- SSE 流式响应。
- 人工 delta/complete/abort。
- Lab 模式复用协议 adapter 和 pending turn manager，支持查看请求体、手动流式输出和复制 curl。
- 工作台 Tool Call 标签页“请求大模型”辅助能力，浏览器端直连支持三套上游协议，生成说明文字并填充表单草稿。
- 工作台 Tool Call 标签页可选择 KirariNetwork delegated LLM upstream，由 ChatAPI 后端使用用户 access token 请求 KirariNetwork 模型。
- 应用 API Key 可读取 pending 请求并执行 delta/complete/abort，复用同一个 Turn Manager 状态机。

验收：

- 现有 `tests/` SDK 脚本通过。
- 前端人工接管完整链路通过。
- Go `httptest` 集成测试至少覆盖三套协议的非流式闭环，并覆盖 Responses / Chat Completions / Anthropic Messages 的 `stream=true` 基础 SSE 行为，以及 `tool_call` 的基础流式返回。
- Go `httptest` 集成测试应覆盖 `waiting -> streaming -> closed/aborted` 的最小状态流转，以及终态后的 `delta` / `complete` / `abort` 返回 `409`。
- Go `httptest` 集成测试应覆盖虚拟模型 API Key 的创建、可解密回看、撤销后拒绝访问、模型兼容入口 owner 归属，以及应用 API Key 通过 `model_keys:*` scope 和 resource limits 管理虚拟模型 API Key。
- Go `httptest` 集成测试应覆盖应用 API Key 通过 `automation:read/write` 读写自动化规则，并覆盖缺少 scope 与 `allowed_automation_rule_ids` 拒绝。
- Go `httptest` 集成测试应覆盖应用 API Key 通过 `statistics:read` 读取自己的请求统计摘要，验证 owner 隔离和缺少 scope 拒绝。
- Go `httptest` 集成测试应覆盖管理员运行时监控接口、connections/queue 接口、GC 触发接口、serve 模式无 admin session 拒绝，以及 `ak-` / `sk-` key 不能访问管理员接口。
- Go `httptest` 集成测试应覆盖管理员存储监控 summary/users 接口，验证会话/消息估算结果和 API Key 不能访问管理员存储接口。
- Go `httptest` 集成测试应覆盖管理员请求态势 overview 接口，验证全局状态/model/owner 聚合和 API Key 不能访问管理员请求接口。
- Lab 模式下 OpenAI/Anthropic SDK 请求可进入等待态，浏览器完成输出后 SDK 收到兼容响应。
- Tool Call 辅助不会自动发送，只能由用户审核后手动提交。
- KirariNetwork token 过期时可自动 refresh；refresh 失败时提示用户重新连接，不泄露 token。
- 应用 API Key 不能访问 `/v1/*` 模型接口，虚拟模型 API Key 不能访问 `/api/app/*` 控制接口。
- abort、timeout、client disconnect 行为符合现有语义。

### Phase 6：自动化规则、通知、运营能力

工作：

- 自动化规则引擎。
- 应用 API Key 读写自己的自动化规则。
- ntfy。
- SMTP 邮件模块。
- 管理员运行时监控、内存/GC 监控、连接监控。
- 用户存储占用估算、存储配额、定时清理和空间回收。
- Realtime 连接限额修正，为浏览器控制台预留 WebSocket 名额。当前已完成 service 层统一 lease、WebUI 预留规则和分类监控；后续 API/SSE 长连接入口接入时复用该能力。
- metrics、doctor、systemd、compose、nginx 文档。

验收：

- 自动化规则配置和执行可用。
- 外部自动化可用应用 API Key 管理规则并完成真实联网调试闭环。
- 管理员后台可查看 ChatAPI 进程内存、Go heap、连接数、每用户存储占用。
- 用户超过存储配额后可按策略自动清理旧会话并释放孤儿图片。
- 同用户 API/SSE 连接占满时，浏览器工作台仍可连接并管理请求。
- SMTP 邮件测试、注册验证、密码重置可用。
- Docker Compose 一键启动。

### Phase 7：灰度和默认切换

工作：

- README 默认后端切换到 Go。
- Python 后端标记 legacy。
- 发布 release candidate。
- 收集兼容问题。

验收：

- 新安装默认使用 Go 后端。
- 老数据可平滑升级。
- 发布说明写清回滚方式。

## 14. 风险和应对

### 13.1 协议细节不兼容

风险：OpenAI/Anthropic SDK 对流式事件、字段顺序、错误结构敏感。

应对：

- 保留响应 golden file。
- 使用真实 SDK 做兼容测试。
- protocol adapter 独立测试，不把响应构造散落在业务层。

### 13.2 Pending turn 并发竞态

风险：complete、abort、client disconnect、timeout 同时发生导致重复写消息或请求卡死。

应对：

- 状态机集中在 Turn Manager。
- 所有状态转换返回明确错误。
- race test 和高重复次数测试。

### 13.3 SQLite 锁争用

风险：SQLite 在 SSE/WebSocket 长连接、频繁写入、多用户正式部署下容易遇到锁等待和维护困难。

应对：

- SQLite 定位为 Lab 模式、本地开发、测试和轻量单机。
- 正式部署推荐 PostgreSQL。
- 长连接不持有事务。
- 写事务短小。
- busy timeout。
- 单独压测。
- 从首版开始提供 `repository/sqlite` 和 `repository/postgresql` 两套实现，并用 contract tests 保持行为一致。

### 13.4 老数据迁移失败

风险：用户的 SQLite 文件存在历史版本差异。

应对：

- migration 前检查表和字段。
- 提供 `chatapi doctor`。
- 升级前文档强调备份。
- 提供回滚说明。

### 13.5 安全兼容和安全升级冲突

风险：兼容旧 API Key 明文存储与长期安全目标冲突。

应对：

- 首版兼容读取。
- 新 key 可逐步改 hash 存储。
- 后续版本提供迁移窗口和轮换提示。

## 15. 近期落地建议

建议按以下顺序开工：

1. 在 `backend-go/` 初始化 Go module、`cmd/chatapi`、config、logger、health route。
2. 写出 SQLite schema fixture、PostgreSQL migration 和 repository contract tests。
3. 先实现 auth/session/users/config，这能最快让前端登录。
4. 实现 conversations/messages 和 WebSocket snapshot，让前端工作台能加载。
5. 最后集中攻克 pending turn、SSE 和三套协议 adapter。

迁移过程中必须始终保持 Python 后端可运行，直到 Go 后端通过完整兼容验收。这样可以让每个阶段都有可对照的行为基线，也能给开源用户保留回滚路径。
