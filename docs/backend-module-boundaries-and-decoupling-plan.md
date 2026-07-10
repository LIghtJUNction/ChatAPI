# ChatAPI Go 后端模块边界与解耦规划

本文档关注四个问题：

1. 这个系统天然可以拆成哪些相对独立的模块。
2. 当前代码里有哪些不必要的耦合。
3. 这些模块应该在哪一层、通过什么机制通信。
4. 哪些部分不应该继续携带 ChatAPI 私有业务语义，而应封装成相对通用的模块。

这份文档不讨论 UI，也不讨论“以后也许可以怎么做”，而是基于当前代码现状给出可执行的模块边界方案。

自动化模块已经按录制式工作流重新落地；本文涉及旧 automation executor 的文件清单和规则形状仅作历史参考，当前边界以 [recorded-automation-design.md](./recorded-automation-design.md) 为准。

## 1. 系统的相对独立模块

按当前业务和代码分布，这个后端天然可以拆成下面几组模块。

### 1.1 Runtime / Bootstrap 模块

职责：

- 启动命令
- 配置加载与校验
- 打开数据库
- 创建 HTTP Server
- 启动后台 worker
- 打开浏览器、打印 Lab URL、doctor / migrate / setup / smtp / oidc CLI

现有代码主要在：

- [backend/internal/app/app.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/app/app.go:46)
- [backend/internal/config](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/config/config.go)

这是一层典型的应用运行时模块，本身不该承载业务决策。

### 1.2 Identity / Auth 模块

职责：

- session cookie
- 本地用户名密码登录
- 注册 / 邮箱验证码 / 找回密码
- TOTP
- OIDC RP 登录与绑定
- actor 建立

现有代码主要在：

- [backend/internal/http/handlers/auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/handlers/auth.go:28)
- [backend/internal/service/local_auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/local_auth.go)
- [backend/internal/service/oidc_auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/oidc_auth.go:34)
- [backend/internal/service/totp.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/totp.go)
- [backend/internal/service/session.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/session.go)

这是一个非常独立的领域，不应该和 turn 生命周期、tool call、存储清理这些模块互相知道太多实现细节。

### 1.3 Turn Runtime / ChatAPI 核心模块

职责：

- 接收 `/v1/responses` / `/v1/chat/completions` / `/messages`
- 把协议请求归一化成内部 turn request
- 创建 pending turn
- 等待人工/自动化完成
- delta / complete / abort / timeout
- 维护实时状态和终态

现有代码主要在：

- [backend/internal/protocol](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/protocol/request.go)
- [backend/internal/service/chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:20)
- [backend/internal/service/pending.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/pending.go)
- [backend/internal/service/turn_control.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/turn_control.go:10)

这是系统最核心的私有业务模块。

### 1.4 Request / Conversation Read Model 模块

职责：

- 请求列表 / 详情
- 会话列表 / 消息查看
- replay curl
- 用户统计摘要
- 管理员请求态势
- Tool Call assist 上下文读取

现有代码分散在：

- [backend/internal/service/statistics.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/statistics.go:12)
- [backend/internal/service/workspace_tool_call.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/workspace_tool_call.go:13)
- [backend/internal/http/handlers/request_view.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/handlers/request_view.go)
- [backend/internal/http/handlers/request_debug.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/handlers/request_debug.go)

这一层本质上是“查询视图模块”，不应该和 turn 状态修改强耦合。

### 1.5 Automation 模块

职责：

- 自动化规则 schema / payload 解析
- 规则匹配
- 决策：命中 / 跳过 / 无规则 / 无匹配
- 触发自动完成

现有代码主要在：

- [backend/internal/service/automation_rules.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/automation_rules.go:13)
- [backend/internal/service/automation_engine.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/automation_engine.go)
- [backend/internal/service/automation_observer.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/automation_observer.go)

自动化是围绕 turn runtime 的扩展能力，不应该反过来成为核心 turn service 的内嵌细节。

### 1.6 Realtime / Event Delivery 模块

职责：

- WebSocket 订阅
- SSE / API / WebUI 连接配额
- 事件广播
- 队列背压与慢订阅者处理

现有代码主要在：

- [backend/internal/service/realtime.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/realtime.go:16)

这是典型的运行时基础模块，应该服务 turn runtime 和 admin runtime，但不应该知道业务规则细节。

### 1.7 Tool Call Workspace / Assist 模块

职责：

- 读取当前请求 / 会话上下文
- 生成 Tool Call assist 输入
- 调用 delegated upstream 或解析浏览器直连返回
- 校验结构化输出并生成草稿

现有代码主要在：

- [backend/internal/service/workspace_tool_call.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/workspace_tool_call.go:21)
- [backend/internal/service/workspace_tool_call_assist.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/workspace_tool_call_assist.go:46)

这是一个工作台产品模块，不应和 ChatAPI 核心 turn 状态机深度耦合。

### 1.8 Upstream / Delegated Provider 模块

职责：

- provider descriptor
- provider error model
- provider registry
- 上游请求 / stream adapter
- token refresh / provider-specific delegated auth

现有代码主要在：

- [backend/internal/service/upstream_provider.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/upstream_provider.go:11)
- [backend/internal/service/kirari_integration.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/kirari_integration.go:29)
- [backend/internal/platform/kirari/client.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/kirari/client.go:33)

这里其实已经具备演化成独立模块的基础。

### 1.9 API Key / Principal 模块

职责：

- 应用 API Key 创建 / 鉴权 / 限流 / scope/resource limit
- 虚拟模型 API Key 创建 / 鉴权
- principal 解析

现有代码主要在：

- [backend/internal/service/app_api_keys.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/app_api_keys.go:19)
- [backend/internal/service/model_api_keys.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/model_api_keys.go:16)
- [backend/internal/http/middleware/app_api_auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/middleware/app_api_auth.go:16)
- [backend/internal/http/middleware/model_api_auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/middleware/model_api_auth.go)

这个模块本身可以很独立，但现在还混着不少 HTTP 和业务语义。

### 1.10 StorageOps / Uploads 模块

职责：

- 图片上传
- 用户存储估算
- quota
- cleanup planning / execute
- orphan 扫描
- vacuum / checkpoint

现有代码主要在：

- [backend/internal/service/uploads.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/uploads.go)
- [backend/internal/service/storage_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/storage_monitor.go:20)

这里其实已经不只是“monitor”，而是一整个存储运维子系统。

### 1.11 Admin Operations / Audit 模块

职责：

- runtime summary / queue / memory / connections
- admin requests overview
- admin users / deletion / ownership
- admin audit query

现有代码主要在：

- [backend/internal/service/runtime_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/runtime_monitor.go)
- [backend/internal/service/admin_*](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/admin_users.go)
- [backend/internal/service/audit.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/audit.go)

这是一个典型的运营平面，不应让核心业务模块为了 admin 页面而暴露太多内部实现。

### 1.12 Persistence / Repository 模块

职责：

- SQLite / PostgreSQL 实现
- migration
- sqlite-to-postgres 数据搬迁

现有代码主要在：

- [backend/internal/store/store.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/store/store.go:487)
- [backend/internal/repository/sqlite/store.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/repository/sqlite/store.go:23)
- [backend/internal/repository/postgresql/store.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/repository/postgresql/store.go:20)

这是底层实现模块，不应直接承载上层业务含义。

## 2. 当前不必要的耦合

### 2.1 Turn Runtime 与副作用模块耦合过深

`ChatAPIService` 当前直接知道：

- 自动化规则服务
- 审计
- 用户配置
- 存储配额服务
- ntfy 通知
- realtime

参考：

- [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:42)
- [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:150)
- [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:178)
- [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:212)

这里不必要的部分在于：

- turn runtime 不应该自己 new 其他 service
- turn runtime 不应该同时负责“状态机 + 准入策略 + 查询 + 通知”
- 自动化完成和 ntfy 通知都应该通过更清楚的扩展点挂接

### 2.2 Query 模块和 Command 模块混在一起

现在请求/会话查询、统计聚合和 pending turn 状态修改都挂在 `ChatAPIService` 上。

这会导致：

- handler 只能依赖一个越来越大的 service
- 任何读模型扩展都会继续加重 turn 核心模块

### 2.3 Tool Call Workspace 对 Request Read Model 的耦合方式不合适

[workspace_tool_call.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/workspace_tool_call.go:69) 在缺少 `request_id` 时，会 `ListRequests()` 后内存过滤 `conversation_id`。

这里的问题不是性能本身，而是 workspace 模块没有通过一个明确的 request query 接口拿上下文，而是直接操作底层全量读取。

### 2.4 API Key 模块里混了策略、schema、HTTP 使用模式和运行时限流

`AppAPIKeyService` 当前同时承担：

- key 生成与鉴权
- resource limit 规范化
- source IP 判断
- 进程内限流
- schema 生成

参考：

- [app_api_keys.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/app_api_keys.go:31)

这里不必要的耦合在于：

- principal/authentication 和 rate limiting 是两类能力
- schema/contract 不应该和认证核心服务混放
- source IP 判断更偏 middleware / transport policy

### 2.5 虚拟模型 API Key 和 App API Key 的抽象没有共享

`ModelAPIKeyService` 和 `AppAPIKeyService` 里有明显相似的事情：

- key prefix
- last_used_at 节流更新
- principal 解析
- revoke/list/create 生命周期

参考：

- [model_api_keys.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/model_api_keys.go:24)
- [app_api_keys.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/app_api_keys.go:31)

这两个模块不该完全合并，但应该共享更底层的 key material / principal authentication 机制。

### 2.6 Auth 模块和 OIDC / delegated upstream 概念层级还不够清晰

当前有两套和 OIDC 相关的东西：

- ChatAPI 自己作为 RP 的登录 OIDC
- Kirari delegated upstream 的 OIDC 授权

对应代码：

- [oidc_auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/oidc_auth.go:34)
- [platform/kirari/client.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/kirari/client.go:44)
- [kirari_integration.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/kirari_integration.go:29)

现在虽然代码已经分开，但概念边界还没有在模块层明确：

- 一个是“登录身份”
- 一个是“用户授权 ChatAPI 代表自己访问某个 provider”

后面如果继续接更多 delegated provider，这个边界必须更硬。

### 2.7 Admin Runtime / StorageOps / Realtime 之间耦合是“直接读内部结构”

`RuntimeMonitorService`、`StorageMonitorService`、`RealtimeHub` 之间很多统计关系现在更像“谁方便谁来读”，而不是通过稳定 read model/observer 暴露。

这在功能快速增长期没问题，但继续扩展会导致 admin 模块反向绑定大量内部数据结构。

### 2.8 Repository 总接口让所有模块默认可越权

[store.Store](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/store/store.go:487) 当前把几乎所有数据能力放在一个接口里。

因此任何 service 都“可以”：

- 顺手读用户配置
- 顺手查请求
- 顺手改系统配置
- 顺手写审计

这就是很多服务越界的根本原因之一。

## 3. 推荐的模块联系机制

不是所有模块都要通过事件总线联系。这个系统更适合分三类联系机制。

### 3.1 同步命令调用：用于强一致核心链路

适用模块：

- protocol adapter -> turn manager
- WebUI/App API/Lab -> turn control
- auth handler -> auth application service
- app api auth middleware -> key authentication service

机制：

- 显式接口
- typed command / typed result
- 不从 context 偷业务参数

例如：

- `CreatePendingTurnCommand`
- `FinalizeTurnCommand`
- `AuthenticateAppAPIKeyCommand`

这一层适合放在 application service 层。

### 3.2 同步查询调用：用于读模型与上下文读取

适用模块：

- tool call workspace -> request/conversation query
- admin requests page -> request overview query
- admin storage page -> storage usage query
- app api -> request detail / conversation detail / statistics

机制：

- 单独的 query service / read repository
- 返回只读 DTO，不返回底层 store 原始结构

不要让 workspace、admin、app api 自己去拼底层 `List*` + 过滤。

### 3.3 领域事件 / 运行时事件：用于副作用和观测

适用模块：

- turn created
- turn delta appended
- turn completed
- turn aborted
- automation matched / skipped / failed
- notification requested
- audit append

推荐机制：

- 先做进程内 typed event bus，不需要一上来上 MQ
- 事件对象只表达事实，不携带 transport 细节
- 副作用订阅者独立消费

例如：

- `TurnWaitingCreated`
- `TurnCompleted`
- `TurnAborted`
- `AutomationDecisionRecorded`

用这类事件挂接：

- ntfy 通知
- 审计
- runtime counters
- 未来的 metrics 细分统计

这样 turn manager 只发布事件，不直接知道每个副作用服务。

## 4. 各模块建议在哪一层联系

### 4.1 HTTP / Middleware 层

只做：

- 外部协议解析
- principal / actor 建立
- 传输错误映射
- schema 输出

不做：

- 复杂业务决策
- 多模块编排
- 查询拼装

### 4.2 Application 层

这里应该是模块之间最主要的联系层。

建议存在以下 application service：

- `AuthApplication`
- `TurnApplication`
- `TurnQueryApplication`
- `WorkspaceApplication`
- `AutomationApplication`
- `AdminRuntimeApplication`
- `StorageOpsApplication`
- `APIKeyApplication`

职责：

- 接收 typed command/query
- 调用一个或多个领域服务 / repository
- 发布领域事件

### 4.3 Domain 层

这里保留真正的业务逻辑：

- turn state rules
- automation matching
- auth role sync
- api key scope/resource rules
- quota policy

不应该夹杂：

- HTTP status code
- SSE 事件写法
- schema JSON 形状

### 4.4 Infrastructure 层

包括：

- repository/sqlite
- repository/postgresql
- platform/kirari
- platform/email
- platform/ntfy
- platform/secretbox
- platform/apikey

职责：

- 对外部系统和底层能力做实现适配

不应承载：

- ChatAPI 的具体 turn / actor / workspace 语义

## 5. 适合抽成相对通用模块的部分

下面这些不应该继续带着 ChatAPI 的私有业务语义。

### 5.1 协议归一化模块

当前 [backend/internal/protocol](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/protocol/request.go) 已经很接近通用模块。

它做的是：

- OpenAI Responses / Chat Completions / Anthropic Messages 的请求提取
- 完成响应构造
- SSE 事件构造
- tool schema 归一化

这部分可以进一步抽成通用包，前提是：

- 去掉对 `store.Conversation` 的直接依赖
- 用更中性的 `ConversationMeta` / `TurnRequest` / `TurnResult`
- 把 ChatAPI 私有的 pending/status 语义留在外层

适合抽出的包方向：

- `llmprotocol`
- `llmcompat`
- `llmturn`

### 5.2 Delegated upstream provider 抽象

当前 [upstream_provider.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/upstream_provider.go:11) 和 `platform/kirari/client.go` 已经有雏形。

适合通用化的部分：

- provider descriptor
- provider error model
- OIDC-delegated provider token handling interface
- chat completions raw / non-stream adapter
- 统一 stream 读取器

不适合抽出去的部分：

- Tool Call assist 的 prompt/schema
- ChatAPI 的 workspace/request context 拼装

### 5.3 API Key material 与密钥存储基础能力

适合通用化的部分：

- key prefix
- hash/verify
- encrypt/decrypt
- “创建时返回明文，存储时 hash 或密文”的基础模式

现有代码：

- [platform/apikey/apikey.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/apikey/apikey.go:1)
- [platform/secretbox/secretbox.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/secretbox/secretbox.go:1)

可以进一步提炼成更明确的 key material 工具层。

### 5.4 OIDC client / delegated authorization client

[platform/kirari/client.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/kirari/client.go:44) 里有一部分其实并不特定于 Kirari：

- discovery
- authorization URL + PKCE
- code exchange
- ID token / userinfo 校验
- refresh token

如果后续真的要复用到其他项目，建议拆成两层：

- 通用 `oidcdelegated` client
- Kirari-specific endpoint/meta/chat completions adapter

不要把 ChatAPI 的用户配置存储和业务状态塞进通用 client。

### 5.5 Ntfy / URL Safety / Browser Open / SMTP

这些已经比较像基础设施模块了：

- [platform/ntfy](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/ntfy/ntfy.go)
- [platform/urlsafety](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/urlsafety/ntfy.go)
- [platform/browser](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/browser/browser.go)
- [platform/email](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/platform/email/smtp.go)

这里要继续保持“通用基础设施”定位，不要往里写 ChatAPI 的 actor、conversation、audit 逻辑。

## 6. 不适合抽成通用模块的部分

下面这些应该明确保留为 ChatAPI 私有业务模块。

### 6.1 Pending turn 状态机

这是产品本体，不通用。

包括：

- waiting / streaming / closed / aborted
- WebUI / App API / Lab / automation 共用的 turn 控制语义
- 人工接管和超时

### 6.2 Tool Call 工作台上下文拼装

虽然 assist provider 可以通用，但“把当前请求、最近消息、draft、normalized tool schema 拼成工作台上下文”是 ChatAPI 的产品逻辑，不是通用 SDK。

### 6.3 应用 API 的 scope/resource 语义

虽然“scope + resource limit”模式是通用的，但这些具体 scope：

- `requests:read`
- `requests:respond`
- `automation:write`
- `model_keys:delete`

都是 ChatAPI 私有资源模型。

### 6.4 管理后台运行时和存储运维平面

这里的统计口径、清理语义、WebUI 预留连接规则，都是 ChatAPI 运行方式决定的，不适合作为通用模块抽出。

## 7. 建议的目标模块关系

建议关系如下：

- HTTP handlers
  - 只依赖 application interfaces
- Middleware
  - 只负责建立 actor/principal
- Application services
  - 编排 domain services、repositories、event bus
- Domain modules
  - 负责规则和状态机
- Read model / query modules
  - 负责列表、详情、概览、统计、workspace context
- Infrastructure adapters
  - 实现 repository、provider client、notification client、crypto、email、browser

模块之间建议这样联系：

- `TurnApplication` -> `TurnRepository` + `TurnAdmissionPolicy` + `EventBus`
- `AutomationApplication` 订阅 `TurnWaitingCreated` 或由 `TurnApplication` 显式调用，再发布 `AutomationDecisionRecorded`
- `NotificationApplication` 订阅 `TurnWaitingCreated` / `TurnCompleted` / `TurnAborted`
- `RuntimeMonitor` 订阅领域事件更新计数，而不是直接知道太多 turn 内部流程
- `WorkspaceApplication` 只依赖 `RequestQueryRepository`、`ConversationQueryRepository`、`AssistProviderRegistry`

## 8. 最值得先做的解耦步骤

如果按优先级排，建议先做下面几步：

1. 定义 application 层接口，把 handler 从具体巨型 service 上摘下来。
2. 把 request/conversation/statistics/tool-call-context 这批查询职责从 `ChatAPIService` 拆出。
3. 给 turn runtime 加进程内 typed event bus，把 ntfy / 审计 / 监控从核心 service 里往外拿。
4. 给 API key 模块拆出独立的 principal authentication / rate limiting / contract schema 三层。
5. 给 delegated upstream 模块建立独立 package，避免继续寄生在 workspace tool call service 上。
6. 在 service 层引入窄 repository interface，停止所有模块直接依赖总 `store.Store`。

## 9. 一句话总结

这个系统可以拆成三块主轴：

- `核心产品轴`：turn runtime、automation、workspace
- `身份与凭据轴`：auth、api key、principal
- `运营与基础设施轴`：storageops、realtime、admin runtime、repository、platform

当前最大的问题不是模块数量不够，而是这些主轴之间还在通过“大 service + 大 store + 直接调用副作用”的方式连接。后续应该把联系收敛到：

- application command/query
- 进程内 typed domain events
- 窄 repository interfaces

这样既不会把系统过度架构化，也能把现在已经出现的耦合点压住。
