# ChatAPI Go 后端代码结构整理计划

本文档基于当前 `refactor/migrate-to-go` 分支的实际代码做结构级检查，目标不是讨论代码风格，而是整理哪些实现会继续推高复杂度，以及后续如何做职责拆分、语义统一和可维护性收口。

## 1. 先说结论

当前后端不是“乱到不可维护”，但已经出现几个会持续放大的结构问题：

- 装配层过重，`router.go` 同时承担依赖组装、模块接线和路由注册。
- `ChatAPIService` 已经成为业务中心点，继续吸收准入、状态机、副作用和查询职责。
- `store.Store` 是单一巨型接口，导致 service 层几乎默认依赖全仓储能力。
- 多个大模块内部已经开始混合“查询聚合 + 业务决策 + 文件系统副作用 + 审计/通知”。
- handler 层存在较多重复的 actor 提取、鉴权前置、JSON decode、错误映射。
- `service` 包同时放了领域逻辑、运行时模块、契约 schema builder，语义边界偏宽。
- 代码中 `map[string]any` 和 `[]any` 使用频率较高，很多契约没有真正类型化。

正面的部分也很明确：

- 协议适配已经有独立 `protocol` 包，不再完全散在 handler 中。
- SQLite / PostgreSQL 已经共享统一 `store` 契约和 contract tests。
- `workspace tool call assist`、`kirari integration`、`realtime` 这些较新的模块，抽象方向整体比老核心模块更清楚。

## 2. 结构诊断

### 2.1 装配层过重

[backend/internal/http/router.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/router.go:21) 当前同时做了三件事：

- 依赖初始化
- service 之间的运行时接线
- 全量路由注册

这里的问题不是文件长，而是“模块所有权”变得模糊。例如：

- `chatService.SetNtfyNotificationService(...)` 这种后置 setter 注入把依赖关系藏到了装配细节里 [router.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/router.go:47)
- `ToolCallAssistService` 在 router 里临时把 `WorkspaceToolCallService` 和 `KirariIntegrationService` 拼起来 [router.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/router.go:72)
- runtime/storage/admin/auth/app api 几套模块都集中在一个构造函数里

继续沿这个方向写，新增功能基本都会堆回这个文件。

### 2.2 `ChatAPIService` 职责过多

[backend/internal/service/chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:20) 当前同时承担：

- 模型兼容入口的 pending turn 创建
- 会话配额准入
- 用户级消息限流
- 自动化规则自动完成
- pending turn 生命周期变更
- realtime 广播
- ntfy 通知
- 请求/会话/消息查询
- 统计摘要

典型信号：

- service 内部直接 `NewAutomationRuleService` [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:42)
- service 内部临时 `NewUserConfigService`、`NewStorageMonitorService`、`NewAuditService` [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:150) [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:178) [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:212)
- 通知能力通过 setter 额外补线 [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:55)
- 查询类方法和状态机命令类方法混在同一个 service 上 [statistics.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/statistics.go:39)

这会让测试越来越依赖“大而全”的 service 实例，而不是稳定的小接口。

### 2.3 `store.Store` 是单一巨型接口

[backend/internal/store/store.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/store/store.go:487) 当前把：

- 用户/身份
- app api key / model api key
- system config / user config
- 审计日志
- 自动化规则
- 上传图片 / 存储配额 / 删除失败队列
- 会话 / 消息 / 请求 / pending turn
- migration / vacuum / checkpoint

全部揉进了一个接口。

这会带来三个后果：

- service 层天然容易越界，因为任何 service 都“理论上能做所有事”
- mock / fake / contract 设计会越来越重
- repository 代码组织虽然按文件拆了，但抽象层仍然是单体仓储

### 2.4 存储治理模块过大，并且存在明显 N+1 / 全量扫描路径

[backend/internal/service/storage_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/storage_monitor.go:20) 已经变成一个复合型大模块，里面同时包含：

- 存储概览
- 用户占用估算
- quota 管理
- 清理候选规划
- 清理执行
- orphan 图片扫描
- 删除失败重试
- SQLite checkpoint/vacuum

并且有明显的扩展风险：

- `Users()` 先列全量 conversations，再对每个会话 `ListMessages` [storage_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/storage_monitor.go:194)
- `cleanupPlan()` 对每个候选会话再次查 messages [storage_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/storage_monitor.go:544)
- `referencedImagesOutsideCandidates()` 再次全量扫 conversations + messages [storage_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/storage_monitor.go:687)

这在小库上可接受，但如果继续叠加管理需求，会让这个模块同时成为结构问题和性能问题。

### 2.5 handler 层重复逻辑较多，部分 handler 也在膨胀

`AuthHandler`、`AppAPIHandler`、`WorkspaceToolCallHandler` 都开始出现重复模式：

- 从 context 取 actor / principal
- 做一次“是否已认证 / 是否交互式 actor”判断
- decode JSON body
- 调 service
- 针对一组固定错误做 HTTP 映射

参考：

- [backend/internal/http/handlers/auth.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/handlers/auth.go:28)
- [backend/internal/http/handlers/app_api.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/handlers/app_api.go:16)
- [backend/internal/http/handlers/workspace_tool_call.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/handlers/workspace_tool_call.go:16)

`AppAPIHandler` 当前还同时负责：

- schema
- request list/detail/replay
- conversation/message 查询
- automation rule 读写
- model key 管理
- statistics

这已经是一个“小型子系统 handler”。

### 2.6 `service` 包语义过宽

当前 `service` 包里不仅有业务逻辑，还有大量机器可读 schema builder：

- `BuildAuthSchema`
- `BuildAdminRuntimeSchema`
- `BuildAppAPIKeySchema`
- `BuildWorkspaceToolCallContextSchema`
- 以及几十个 `*Schema` 结构

参考搜索结果可见 schema builder 分布非常广：[backend/internal/service](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service)

这会导致：

- service 包越来越像“所有上层逻辑的杂物间”
- schema 变更和业务逻辑变更耦在一起
- handler 很难依赖更窄的模块

### 2.7 上下文 actor 语义已经形成隐式依赖

[backend/internal/service/request_actor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/request_actor.go:8) 定义的 `RequestActor` 是正确方向，但当前很多 service 直接从 `context` 取 owner：

- `ChatAPIService` create / automation / notify [chatapi.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/chatapi.go:83)
- `UploadService` [uploads.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/uploads.go:130)

问题不在“有 actor 上下文”，而在于业务方法把它当成默认输入。这样 service 会被 HTTP 调用约定强绑定，不利于后续 worker / CLI / tests / job 复用。

### 2.8 查询接口不少还是“全量扫 + 内存过滤”

这类实现目前常见于功能快速落地阶段，但需要明确是过渡态：

- `WorkspaceToolCallService.resolveRequest()` 在缺 `request_id` 时先 `ListRequests()` 再按 `conversation_id` 过滤 [workspace_tool_call.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/workspace_tool_call.go:69)
- `StatisticsSummaryForOwner()` / `RequestsOverview()` 通过全量 request 列表做聚合 [statistics.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/statistics.go:39)
- `AdminUserHistoryService` 也有类似 service 层拼查询的倾向

这会让 service 同时承担“读模型聚合器”和“业务逻辑”，而且后面数据库大了以后需要重写路径。

### 2.9 类型边界还不够硬，`map[string]any` 仍然偏多

当前 `map[string]any` 在三个层面都很多：

- 协议适配层 [backend/internal/protocol/request.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/protocol/request.go:17)
- 配置 / 规则 /资源限制
- handler 请求体与响应拼装

其中有些位置是合理的，比如协议兼容入口和 schema metadata；但也有不少业务语义已经稳定，仍然停留在动态 map：

- app api resource limits
- tool call assist 结构化输出
- system/user config known fields
- automation rule payload

长期看，内部结构应该逐步类型化，动态 map 只留在边界层和真正自由形态的扩展位。

## 3. 目标结构

目标不是把后端拆成很多空壳，而是把“变化频率不同的东西”分开。

建议收敛成下面几层：

### 3.1 组合层

新增明确的应用装配层，例如：

- `internal/app/runtime`
- `internal/app/httpserver`
- `internal/app/modules`

职责：

- 构造共享基础设施
- 按模块组装 service
- 注册路由

要求：

- `router.go` 只保留路由挂载，不再手工 new 所有 service
- 禁止运行时 `SetXxxService(...)` 这类后置注入成为常态

### 3.2 领域服务层

按业务域拆 service，而不是按“谁先写”堆文件。

建议的核心分组：

- `turn`：pending turn 生命周期、协议入口编排、人工输出、自动化完成
- `auth`：local / session / oidc / totp / registration / password reset
- `apikey`：app api key / virtual model key
- `workspace`：tool call context / assist / upstream draft parsing
- `upstream`：delegated provider registry、provider error model、stream adapter
- `storageops`：quota、cleanup planning、cleanup execution、orphan scan
- `adminops`：runtime、requests overview、audit read model、user admin flows

### 3.3 仓储接口层

不要立刻推翻 SQLite / PostgreSQL 实现，但要逐步停止让所有 service 直接依赖总接口。

方向：

- `store.Store` 继续作为底层全实现适配层存在
- 在 service 层定义更窄的依赖接口，例如：
  - `TurnRepository`
  - `UserConfigRepository`
  - `AuditRepository`
  - `StorageRepository`
  - `RequestReadRepository`

这样可以先不大动 repository 实现，但把 service 的依赖面收窄。

### 3.4 读模型与命令模型分离

当前不少 service 既负责状态修改，又负责汇总查询。

建议区分：

- `Command`：创建 pending、delta、complete、abort、cleanup execute
- `Query`：requests overview、statistics summary、tool call context、storage usage

这不一定要上 CQRS，但至少在包和类型上把“修改系统状态”和“读取聚合视图”拆开。

## 4. 需要统一的语义

### 4.1 actor 语义

统一几类调用者：

- `InteractiveUserActor`
- `AppAPIPrincipal`
- `VirtualModelPrincipal`
- `AdminSessionActor`
- `LabActor`

要求：

- handler / middleware 负责把外部凭据解析成 actor/principal
- service 方法优先显式接收 `actor` 或 `ownerID`，不要默认从 `context` 里偷取业务参数
- `context` 只保留 trace / cancel / deadline / request-scoped metadata

### 4.2 “虚拟模型 / 上游模型 / delegated upstream” 三类概念

当前文档概念已经明确，但代码边界还没完全对应：

- 虚拟模型：ChatAPI 对外暴露的模型兼容入口
- 上游模型：用户自己配置、通常浏览器端直连的真实模型
- delegated upstream：由 ChatAPI 后端代表用户调用的上游，如 Kirari

建议在代码包级别也对应：

- `virtualmodel`
- `upstreambrowser` 或仅保留前端侧概念
- `delegatedupstream`

避免 `ToolCallAssistService`、`KirariIntegrationService`、`upstream_provider.go` 之间继续出现职责交错。

### 4.3 turn 控制语义

当前已经有 `TurnControlCommand`，这是对的，但还不够统一。

建议明确：

- `CreatePendingTurn`
- `AppendDraftDelta`
- `FinalizeTurn`
- `AbortTurn`
- `ExpireTurn`
- `AutoCompleteTurn`

并把不同入口都路由到同一模块，而不是继续由 `ChatAPIService` 做分派中心。

### 4.4 错误语义

当前不同 handler 对同一类错误的 HTTP 映射有重复实现。

建议统一三层错误：

- 领域错误：`ErrForbidden`、`ErrTurnConflict`、`ErrRateLimited`
- 外部依赖错误：`ProviderError`、`SMTPError`、`OIDCError`
- 传输映射：HTTP / SSE / OpenAI / Anthropic 错误体

这样错误定义在领域层，映射在 transport adapter 层，不要在每个 handler 手写一遍。

### 4.5 schema builder 归位

机器可读 schema 很有价值，但不应继续散落在 `service` 包的业务实现旁边。

建议逐步迁到：

- `internal/schema/httpapi`
- 或 `internal/contract`

原则：

- 业务 service 不负责描述自己的 HTTP 表单结构
- handler 从 contract/schema 模块取 schema

## 5. 分阶段整理计划

### Phase A：先收装配层和命名边界

目标：

- 把 `router.go` 降成路由挂载入口
- 建立模块装配层
- 明确包级术语

工作：

- 新增 `internal/app/modules` 或同类目录
- 把 auth/chat/workspace/admin/storage/upstream 的构造逻辑移出 `router.go`
- 取消 `SetNtfyNotificationService(...)` 这类后置注入，改为构造注入
- 在代码注释和文档中统一 `virtual model` / `upstream model` / `delegated upstream`

完成标志：

- [backend/internal/http/router.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/http/router.go:21) 明显缩短
- router 不再直接 `new` 大量 service

### Phase B：拆 `ChatAPIService`

目标：

- 让 turn 生命周期回到单一核心模块
- 把查询和副作用拆出去

建议拆分：

- `TurnGateway`：协议入口编排
- `TurnManager`：pending turn 状态机
- `TurnAdmissionService`：会话配额 / 用户级消息限流
- `TurnNotifier`：ntfy / 未来其他通知
- `TurnQueryService`：requests / conversations / statistics
- `AutomationExecutor`：自动化规则匹配和自动完成

优先顺序：

1. 先拆查询类方法
2. 再拆准入和通知
3. 最后把自动化完成和状态机收口

完成标志：

- `ChatAPIService` 不再既负责查询又负责状态修改又负责副作用
- `turn_control.go` 不再直接绑定到一个巨型 service

### Phase C：收窄仓储依赖

目标：

- service 只依赖自己需要的 repository 能力

工作：

- 在 service 层先定义窄接口
- 让现有 `sqlite.Store` / `postgresql.Store` 继续实现这些窄接口
- 新增面向聚合查询的 repository 方法，替代 service 层全量扫描

优先补的方法：

- `GetRequestByConversationID`
- `ListRequestsByOwner`
- `CountRequestsByOwnerAndStatus`
- `SummarizeRequests`
- `EstimateStorageByOwner`
- `ListConversationImageRefs`

完成标志：

- `WorkspaceToolCallService.resolveRequest()` 不再 `ListRequests()` 后过滤
- `StatisticsSummaryForOwner()` / `RequestsOverview()` 不再依赖全量列表
- `StorageMonitorService` 的全量扫描和 N+1 明显下降

### Phase D：拆存储治理模块

目标：

- 把 [storage_monitor.go](/home/zyf/Code/Projects/chatapi-go-refactor/backend/internal/service/storage_monitor.go:20) 从“超级 service”拆成多个稳定组件

建议拆分：

- `StorageUsageService`
- `StorageCleanupPlanner`
- `StorageCleanupExecutor`
- `StorageOrphanScanner`
- `StorageDeletionRetryService`
- `DatabaseMaintenanceService`

要求：

- 规划逻辑和执行逻辑分离
- 文件系统副作用和数据库副作用分离
- dry-run 结果对象保持统一

完成标志：

- 不再由一个千行 service 负责所有存储治理动作

### Phase E：规范 handler 层

目标：

- 降低 handler 重复代码
- 让 handler 只做 transport adapter

工作：

- 抽统一的 actor/principal 获取 helper
- 抽统一 body decode helper
- 抽统一 error -> HTTP 映射 helper
- 对 `AuthHandler`、`AppAPIHandler`、`WorkspaceToolCallHandler` 分子模块

建议：

- `AppAPIHandler` 按 `requests` / `conversations` / `automation_rules` / `model_keys` / `statistics` 分文件
- `AuthHandler` 按 `session` / `local_login` / `registration` / `password_reset` / `oidc` / `totp` 分文件

完成标志：

- handler 文件长度显著下降
- 错误映射不再散落复制

### Phase F：把 schema / contract 从业务 service 中迁出

目标：

- 收紧 `service` 包语义

工作：

- 新建 `internal/contract` 或 `internal/schema/httpapi`
- 将 `Build*Schema()` 迁移过去
- 业务 service 保留真正业务输入/输出类型

完成标志：

- `service` 包主要承载业务逻辑，不再混杂大量 API 描述结构

### Phase G：逐步类型化内部契约

目标：

- 内部逻辑尽量使用 typed DTO
- `map[string]any` 留在边界层

优先类型化对象：

- app api resource limits
- tool call assist 结构化输出
- user/system known config fields
- automation rule action / condition 内部执行模型

保留动态结构的区域：

- 协议入口原始请求 body
- schema metadata
- 允许用户自由扩展的配置 payload

完成标志：

- service 之间传递的 `map[string]any` 明显减少
- 结构化输出和资源限制不再主要靠运行时反射判断

## 6. 执行原则

- 不做“一次性大重写”，按模块收口。
- 每一步先保持现有行为，再逐步收边界。
- 优先拆依赖关系和输入输出模型，再考虑目录移动。
- repository 先收窄接口，再补新查询，不急着重写 SQLite / PostgreSQL 实现。
- 对外 API 契约可以暂时不变，但内部 command/query/contract 必须逐步稳定下来。

## 7. 建议的最近执行顺序

如果按投入产出比排序，建议先做这四步：

1. 收 `router.go` 装配层。
2. 把 `ChatAPIService` 查询职责拆出来。
3. 给 request/statistics/storage 补专用 repository 查询，替代 service 层全量扫描。
4. 拆 `storage_monitor.go`。

这四步做完，后面再继续做 Tool Call assist、delegated upstream、管理后台和自动化规则增强，结构成本会低很多。
