# 管理员系统设置实现

## 实现状态

本文档描述的管理员设置控制面已经实现。当前权威接口为：

- `GET /api/admin/settings/catalog`
- `GET /api/admin/settings/overview`
- `GET/PATCH /api/admin/settings/{auth|access|chat|media|automation|realtime}`
- `GET /api/admin/settings/runtime`

旧的 `/api/admin/auth/settings`、`/api/admin/access/settings` 和前端引用但后端不存在的
`/api/config/system` 均不再作为入口，避免形成第二套配置 authority。

设置更新刻意采用 last-write-wins：PATCH 只要求至少一个字段，repository 无条件写入最后提交的值。
本项目定位为单管理员使用的协议调试工具，不处理多个管理员并发编辑，因此不实现 revision、CAS、409
冲突恢复、设置轮询或设置更新 WS。该取舍是产品语义，不是待补功能；后续审查不应以缺少乐观锁或设置
实时同步作为问题。管理员进入领域页面时 GET，点击保存时 PATCH，保存响应就是当前页面的新基线。

用户列表与运行指标的实时性不属于设置同步。管理员用户列表使用服务端分页，并为当前页用户单独建立
`/api/admin/monitor/stream?user_ids=...` SSE；该边界见 `admin-monitoring-sse.md`。

配置核心明确区分两种表示：repository 中的 persisted document 保留数据库原始字段，领域读取时再
投影为叠加 environment precedence 的 effective snapshot。PATCH 只在 persisted document 上合并
本次已知字段，因此不会清除被环境变量暂时覆盖的数据库值，也不会在滚动升级期间删除当前实例尚不认识
的字段。descriptor 负责严格检查 JSON 基本类型、整数性、范围和枚举，领域 validator 继续负责跨字段
业务约束。

运行时接入情况：

- `auth`：注册、密码登录、邮箱验证、密码找回和 GeeTest 策略即时生效。
- `access`：匿名、用户、session、app key、model key 限流即时生效。
- `chat`：pending TTL 由后台循环读取当前快照并执行过期处理。
- `media`：请求图片预处理和输出图片上传/读取共享当前媒体策略。
- `automation`：全局执行开关、步骤上限和循环间隔上限即时生效；运行任务在等待期间和每个步骤边界统一重查当前策略，仅取消已不再满足策略的执行，各实例语义一致。
- `realtime`：新的 WebSocket 握手按当前进程实例总连接上限和单用户单实例连接上限准入；这些计数不冒充跨实例全局配额。
- `runtime`：数据库、监听地址、目录、凭据配置状态等仅脱敏只读，保持环境变量 authority。

前端使用独立 `/admin/settings` 工作区。L1 常用项直接展示，L2 策略分区展示，L3 高级项默认折叠，
L4 启动项只在运行环境页展示。旧 `SystemSettingsPanel` 及其专用类型和状态 hook 已删除。
保存期间配置控件会禁用，存在草稿时也不能手动重新加载，避免普通 HTTP 响应覆盖用户输入。配置存储刷新
失败时，运行时继续使用 last-known-good 快照；管理员页面明确显示 stale 状态并暂停保存。

`admincontrol/settings` 只依赖各领域暴露的 `SettingsDomain` 契约，不依赖具体 `settingscore.Service`。
配置表仍由 `repository/config` 提供共享 last-write-wins 持久化原语，但 storage key、字段、校验、有效快照和更新
副作用均由领域 service 私有拥有；auth/access 原有的无条件写入口已经删除。

## 设计依据

需要重新设计。现在配置来源已经分散到环境变量、认证设置、访问控制、媒体策略、实时通信、自动化等多个位置，继续恢复旧版 SystemSettingsPanel 会形成一个很长的表单，而且会把“启动
  参数”和“可热更新业务策略”混在一起。

  建议按作用域、重要程度、更新方式三个维度重新建模，而不是只按配置名称分组。

  配置分级

   级别         定位                            UI 位置                     示例
  ━━━━━━━━━━━  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  ━━━━━━━━━━━━━━━━━━━━━━━━━━  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   L1 常用      管理员经常调整、影响用户行为    管理设置一级导航            注册开关、登录方式、全局并发、图片上传开关
  ───────────  ──────────────────────────────  ──────────────────────────  ──────────────────────────────────────────────────
   L2 策略      偶尔调整、有明确领域归属        对应页面的“更多设置”        pending 限制、请求超时、媒体大小、自动化默认间隔
  ───────────  ──────────────────────────────  ──────────────────────────  ──────────────────────────────────────────────────
   L3 高级      容易误配、偏运维                独立“高级”页面，默认折叠    WS 心跳、GC 周期、AVIF 参数、协议限制
  ───────────  ──────────────────────────────  ──────────────────────────  ──────────────────────────────────────────────────
   L4 启动项    不应在线修改或包含敏感信息      “运行环境”只读页面          数据库、监听地址、存储目录、密钥、超级管理员种子
  ───────────  ──────────────────────────────  ──────────────────────────  ──────────────────────────────────────────────────
   独立资源     不适合表达成 key/value          独立管理页面                OAuth Provider、邮件服务、模型、管理员、操作任务

  L4 不应复制进数据库。环境变量仍然是权威来源，前端只显示“已配置/未配置”“来源”“修改后需重启”，不能返回密钥明文。

  建议的一级页面

  管理设置不再放进当前普通设置弹窗。管理员入口进入独立工作区，例如 /admin/settings，左侧使用紧凑导航：

  1. 概览
      - 注册是否开放
      - 当前认证方式
      - 请求并发和 pending 使用情况
      - 媒体存储状态
      - 需要重启的配置提示
      - 配置校验警告

  2. 访问与认证
      - 注册、密码登录、会话策略
      - 全局并发、单用户并发、pending 上限
      - 常用配置直接展示
      - CSRF、Cookie、会话时长等放入“高级策略”

  3. 聊天与协议
      - 请求超时、默认流式策略
      - 协议输入限制
      - conversation/pending 生命周期策略
      - 特殊协议兼容选项放二级展开

  4. 媒体
      - 图片处理总开关
      - 文件大小、尺寸和数量
      - SVG 拒绝策略
      - AVIF 质量、存储路径和孤儿清理放入高级区域

  5. 自动化
      - 只放全局默认值和全局安全上限
      - 具体自动化规则仍在自动化管理页面，不塞进系统配置
 6. 通知与集成
      - 邮件、ntfy 等独立 provider
      - 凭据只支持覆盖，不回显
      - 连接测试作为明确命令，不属于普通保存操作

  7. 实时通信
      - 默认只显示连接状态和在线连接数
      - WS 心跳、重连窗口、事件保留等全部放高级设置

  8. 运行环境
      - 环境变量和程序启动配置只读
      - 标记配置来源：environment、database、default
      - 标记是否需要重启

  后端边界

  不要重新实现一个巨型 SystemConfig map[string]any。建议增加统一的配置控制面，但保持各领域拥有自己的类型和校验：

  internal/service/admincontrol/settings
      catalog.go       配置分组、级别、来源、可编辑性
      query.go         聚合管理员配置视图
      update.go        分发领域更新
      validation.go    跨配置校验
      redaction.go     敏感字段脱敏

  internal/service/auth/authn/settings
  internal/service/access/settings
  internal/service/chat/settings
  internal/service/chat/preprocess/settings
  internal/service/chat/workspace/settings
  internal/service/automation/settings
  internal/ops/observability/settings

  admincontrol/settings 只负责编排，不直接读写 SQL，也不自己拥有全部配置。认证配置仍由 auth 负责，媒体配置仍由 preprocess/media 负责，自动化默认策略仍由 automation 负责。

  持久化层建议按领域保存：

  internal/repository/config
      auth.go
      access.go
      chat.go
      media.go
      automation.go
      realtime.go

  底层可以继续共用一张配置表，但 repository 对 service 暴露的是类型化接口，不暴露任意字符串 key。

   配置模型

  每个配置字段至少需要这些元数据：

  type SettingDescriptor struct {
      Key             string
      Group           string
      Level           SettingLevel
      Source          SettingSource
      Editable        bool
      Sensitive       bool
      RestartRequired bool
  }

  实际值和描述信息分开：

  type SettingValue struct {
      Key       string
      Value     any
      Source    SettingSource
      UpdatedAt time.Time
  }

  本项目不在设置值上携带 revision。多个管理员同时提交时，最后完成的数据库写入生效。

  接口设计

  不建议让前端对整个系统配置做一次全量覆盖。按领域读取和 patch：

  GET   /api/admin/settings/catalog
  GET   /api/admin/settings/overview

  GET   /api/admin/settings/auth
  PATCH /api/admin/settings/auth

  GET   /api/admin/settings/access
  PATCH /api/admin/settings/access

  GET   /api/admin/settings/chat
  PATCH /api/admin/settings/chat

  GET   /api/admin/settings/media
  PATCH /api/admin/settings/media

  GET   /api/admin/settings/automation
  PATCH /api/admin/settings/automation

  GET   /api/admin/settings/realtime
  PATCH /api/admin/settings/realtime

  GET   /api/admin/settings/runtime

  已有 /api/admin/auth/settings 和 /api/admin/access/settings 可以迁移到统一命名。项目允许破坏性升级的话，直接替换旧接口，避免长期保留两套 authority。

  catalog 用于搜索、分级、权限、来源和重启提示，但不要把整个前端做成完全由 schema 动态生成的表单。常用页面应使用明确组件，只有 L3 高级配置可以大量复用 schema-driven 控件。

  更新语义

  一次 PATCH 应遵循：

  1. HTTP middleware 完成管理员身份校验，handler 只解析请求。
  2. admincontrol/settings 分发到领域配置服务并完成校验。
  3. repository 按 last-write-wins 持久化，领域服务替换本进程有效快照。

  需要重启的配置不能伪装成热更新。响应中明确返回：

  {
    "applied": ["media.reject_svg"],
    "restart_required": ["realtime.listen_address"]
  }

  前端交互

  前端建议新增：

  frontend/src/features/admin-settings/
      api/
      model/
      pages/
      sections/
      components/

  页面设计原则：

  - 一级导航只展示 6 到 8 个领域，不展示几十个配置键。
  - 每页顶部只放最常用的 3 到 6 项。
  - L2 放在带标题的次级分区。
  - L3 使用“高级设置”折叠区域，并支持配置搜索。
  - 每个领域独立保存，不提供一个覆盖全系统的“保存全部”。
  - 有改动时显示底部固定操作栏：撤销、保存。
  - 保存前只对危险变更显示确认对话框。
  - 数值配置使用带单位输入，例如 30 秒、20 MiB，不能让用户猜单位。
  - 敏感配置显示“已配置”，修改时输入新值；空值不能自动解释成删除。
  - 环境变量覆盖数据库值时，控件只读并显示“由环境变量管理”。
  - 设置页面不建立 WS 或轮询；重新进入页面时读取最新值。

实施顺序

  1. 盘点所有现有环境变量、数据库配置键及运行时消费者，建立配置清单，明确 owner、级别、默认值和热更新能力。
  2. 实现类型化领域 settings service，先覆盖现有 auth/access，消除裸 key 读取。
  3. 新增 admincontrol/settings 聚合查询、脱敏和审计。
  4. 接入 media、chat、automation、realtime 的运行时配置快照。
  5. 注册新的 /api/admin/settings/* API，删除不存在但旧前端仍引用的 /api/config/system。
  6. 实现独立管理员设置页面，先完成概览、认证、访问、媒体四页。
  7. 接入高级配置和运行环境只读页。
  8. 删除旧 SystemSettingsPanel 以及旧配置类型，避免形成第二套配置模型。

  验收时需要重点覆盖：非管理员拒绝、环境变量覆盖、敏感值不回显、last-write-wins、跨字段校验、
  数据库失败不更新内存、热更新即时生效，以及需重启配置不误报生效。
