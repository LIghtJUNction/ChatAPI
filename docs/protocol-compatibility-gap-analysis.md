# 协议兼容缺口与归一化建议

本文面向 `chatapi-go-refactor` 当前版本，整理三套外部协议在“协议调试工具”场景下的兼容缺口、字段语义、归一化边界，以及建议的请求处理与 SSE 生命周期策略。

本文基于当前实现观察，重点参考：

- `backend/internal/protocol/request.go`
- `backend/internal/protocol/request_build.go`
- `backend/internal/protocol/response.go`
- `backend/internal/protocol/stream.go`
- `backend/internal/protocol/protocol_test.go`

另参考了 `~/Code/Projects/new-api`，但它更适合作为“兼容消费方”参考，不适合作为完整 Responses SSE 生命周期的权威依据。它能识别和转译部分 Responses 事件，但没有证明自己完整生成或完整覆盖官方事件族。

## 1. 当前重构版现状

当前 `chatapi-go-refactor` 已具备：

- 三套协议基础入站解析：Responses / Chat Completions / Anthropic Messages
- 基础结构化抽象：`TurnRequest`、`TurnResult`、`ConversationMeta`
- 基础非流式输出
- 基础 SSE 输出
- Responses 的最小 reasoning 事件支持
- tool schema / tool choice / response_format 的最小归一化

当前明显缺口：

- 请求顶层参数透传和语义保留不足
- 不同协议专有字段尚未做“保留语义”建模
- Responses SSE 生命周期仍是“最小可用集”，不是“完整协议实现”
- Chat Completions / Anthropic 的流式细粒度事件仍偏简化
- 前端缺少“字段支持级别”展示模型

## 1.1 阶段 1.5 / 2 后的现状

当前已经补齐：

- `TurnRequest.Options` / `RawBody`，请求字段可以保留到 debug 视图。
- `debugview.ProjectRequest`，前端可以展示协议专有字段 chip。
- `protocolruntime.Runtime`，pending turn 拥有协议 runtime，streaming 层只负责写出 runtime 产生的事件。
- Responses abort/disconnect 输出 `response.failed`。
- Chat Completions abort/disconnect 直接断开连接，不生成非官方错误 chunk。
- Anthropic Messages abort/disconnect 输出官方 `event:error` 形状。
- 工具 schema 兼容 `parameters`、`function.parameters`、`input_schema`。

仍然没有完整实现：

- Responses function call 的完整 item 生命周期和 `function_call_arguments.delta/done` 分片状态机。
- Chat Completions tool call 的多 chunk 聚合/分片状态机。
- Anthropic Messages 多 content block 基础状态机已经实现：`tool_use` 和 text block index 递增由 runtime 管理；本地 runtime 不生成官方 `thinking` block，因为 Anthropic `thinking` 需要上游签名。
- Responses reasoning delta 不能假设一定存在；真实端点可能只返回 reasoning item added/done。
- Responses `refusal`、`incomplete`、annotations 等事件族仍未实现；audio、web search、file search、code interpreter、image generation、MCP 等内置工具事件有 SDK-visible 形状，但当前没有本地执行系统，按 out-of-scope 处理。

因此，后端现在是“基础协议 + 字段保留 + 最小 runtime”，还不是完整协议模拟器。下一阶段应该把 runtime 从简单 builder 升级成按协议维护 block/item/chunk 状态的 state machine。

## 2. 先定一个原则

对于协议调试工具，目标不应该只是“能跑通 SDK”，而应该拆成三层能力：

1. **语法兼容**
   能接收字段，不因为未知字段直接丢语义。

2. **语义保留**
   即使后端当前不执行该语义，也要把字段保存在统一结构和 debug/raw payload 中，前端能看到。

3. **行为兼容**
   对真正影响生成、流式输出、工具调用、推理、截断、存储的字段，后端要有实际行为。

建议把每个字段明确分成四类：

- `A` 完全归一化并驱动行为
- `B` 归一化但当前只保留/展示
- `C` 协议专有，仅原样透传并打标签
- `D` 当前拒绝支持，显式返回 unsupported

## 3. 建议扩展统一请求模型

当前 `TurnRequest` 字段太薄，建议至少补一层“协议参数壳”：

```go
type TurnRequest struct {
    Protocol         Protocol
    ConversationID   string
    Model            string
    Stream           bool
    SystemContent    string
    DeveloperContent string
    AssistantContent string
    UserContent      string
    InputParts       []InputPart
    ToolSchemas      []ToolSchema
    ToolChoice       ToolChoice
    ResponseFormat   ResponseFormat

    Options TurnOptions
    RawBody  map[string]any
}

type TurnOptions struct {
    Instructions        string
    PreviousResponseID  string
    Store               *bool
    Metadata            map[string]any
    Include             []string
    MaxOutputTokens     *int
    MaxTokens           *int
    MaxCompletionTokens *int
    Temperature         *float64
    TopP                *float64
    TopK                *int
    Stop                []string
    N                   *int
    PresencePenalty     *float64
    FrequencyPenalty    *float64
    Seed                *int64
    User                string
    StreamOptions       map[string]any
    ParallelToolCalls   *bool
    Reasoning           map[string]any
    ReasoningEffort     string
    Thinking            map[string]any
    ServiceTier         string
    Text                map[string]any
    Truncation          string
    Modalities          []string
    Audio               map[string]any
    Prediction          map[string]any
    MCPServers          []map[string]any
    ContextManagement   map[string]any

    ProviderExtras map[string]any
}
```

原则：

- 统一字段进 `TurnOptions`
- 无法归一化的字段进 `ProviderExtras`
- 原始请求保留 `RawBody`
- 前端调试面板同时展示：
  - 统一字段视图
  - 原始字段视图
  - 支持级别标签

## 4. 各协议字段归类建议

### 4.1 OpenAI Responses

缺失字段：

- `instructions`
- `previous_response_id`
- `store`
- `metadata`
- `include`
- `max_output_tokens`
- `parallel_tool_calls`
- `reasoning`
- `service_tier`
- `stream_options`
- `temperature`
- `top_p`
- `text`
- `truncation`
- `user`

建议如下。

#### 可归一化且应有真实行为

- `instructions`
  - 本质是额外指令层。
  - 建议归一化到 `TurnOptions.Instructions`。
  - 行为上不要偷偷并入 `system` 文本；应单独保留。
  - 前端显示标签：`normalized + behavior-pending` 或 `normalized + applied`。

- `previous_response_id`
  - Responses 特有强语义字段。
  - 如果 ChatAPI 不做官方 response store，则无法真正按 OpenAI 语义回溯上下文。
  - 建议归一化到 `PreviousResponseID`，并作为“会话串联提示”保留。
  - 如果本系统已有 `conversation_id`，不要假装两者等价。
  - 建议行为：
    - 有映射关系时：用于查找上一条 turn/request
    - 无映射关系时：仅保留和展示，标记 `semantic-not-fully-applied`

- `store`
  - 控制服务端是否存储 response。
  - 对调试工具是重要行为字段。
  - 建议归一化到 `Store *bool`。
  - 行为：
    - `true/false` 应真实影响是否创建可回查 response 记录
    - 若系统架构暂时无法关闭存储，至少显式标注“已接收但未严格执行”

- `max_output_tokens`
  - 强行为字段。
  - 归一化到 `MaxOutputTokens`
  - 行为：
    - 限制人工补全上限
    - 限制自动化规则输出上限
    - 限制未来上游辅助模型输出上限

- `temperature`
- `top_p`
  - 对“真人补全”本身无意义，但对未来“自动补全/上游模型辅助”有意义。
  - 建议统一保留，当前不影响人工模式。
  - 标签建议：`normalized, no-local-effect`

- `truncation`
  - Responses 特有，涉及上下文截断策略。
  - 若当前系统没有自动上下文裁剪器，则不能伪实现。
  - 建议保留并在未来接入 context builder。
  - 标签：`provider-specific, behavior-pending`

#### 可归一化但先以保留/展示为主

- `metadata`
  - 强烈建议完整保留。
  - 可用于调试标签、审计、工作台过滤、自动化规则。
  - 建议行为：
    - 落库存 request metadata
    - 前端展示为 KV

- `include`
  - 控制响应额外展开字段，偏输出定制。
  - 当前阶段建议保留，不必先做严格行为。
  - 若后端未来有多种 usage/details/reasoning 级别输出，可据此裁剪响应体。

- `parallel_tool_calls`
  - 能跨协议归一化。
  - 建议统一保留。
  - 当前人工调试工具若一次只允许单 tool call 完成，则应明确标记“接受但不执行并发语义”。

- `reasoning`
  - 这是 Responses 明显专有的大对象。
  - 建议保留原始对象，不要过早扁平化。
  - 可与当前已有 `ReasoningStreamMode` 并存。
  - 前端需要专门标签：`responses-only`

- `service_tier`
  - 偏路由/计费/优先级语义。
  - 对 ChatAPI 很有价值。
  - 建议保留并透传到调度、日志、审计。

- `stream_options`
  - 统一保留为对象。
  - 后续可用于 `include_usage`、heartbeat、chunking 风格等。

- `text`
  - Responses 新文本输出配置对象，强专有。
  - 先保留原始对象。
  - 若以后要支持 `text.format` / structured text 配置，再细化。

- `user`
  - 可跨协议归一化为“最终用户标识”。
  - 对审计、风控、统计有意义。
  - 应真实保留。

### 4.2 OpenAI Chat Completions

缺失字段：

- `max_tokens`
- `max_completion_tokens`
- `temperature`
- `top_p`
- `stop`
- `n`
- `penalty`
- `seed`
- `user`
- `stream_options`
- `parallel_tool_calls`
- `reasoning_effort`
- `modalities`
- `audio`
- `prediction`
- `metadata`
- `service_tier`

建议如下。

#### 可归一化且应有真实行为

- `max_tokens`
- `max_completion_tokens`
  - 两者语义接近但不完全同名。
  - 建议统一规则：
    - `max_completion_tokens` 优先
    - 否则回退 `max_tokens`
  - 前端应显示源字段来源。

- `stop`
  - 强行为字段。
  - 对人工模式也可执行。
  - 建议行为：
    - 流式 delta 时检测 stop sequence
    - 命中后停止继续输出
    - 最终 finish_reason 设为 `stop`

- `n`
  - Chat Completions 特有多候选语义。
  - 当前系统基本不支持。
  - 不建议假装归一化到单轮单结果。
  - 建议：
    - `n=1` 正常
    - `n>1` 标记 `unsupported-semantic`
    - 可先返回 400，或接受但明确声明只取第一条
  - 对协议调试工具，更推荐显式不支持并提示。

- `modalities`
- `audio`
  - 若以后要调试音频输出，这是强行为字段。
  - 当前若未实现音频响应，不应只静默透传。
  - 建议：
    - 接收并保留
    - 若请求要求音频模态，则返回 `unsupported`

#### 可归一化但先保留

- `temperature`
- `top_p`
- `seed`
- `user`
- `stream_options`
- `parallel_tool_calls`
- `metadata`
- `service_tier`
  - 都建议保留进 `TurnOptions`
  - 当前人工模式可无本地行为，但要在前端显示“已接收”

- `reasoning_effort`
  - OpenAI 新系模型语义，和 Responses `reasoning`、Anthropic `thinking` 不等价。
  - 可以统一归入“推理强度”大类，但不要丢原字段名。
  - 建议：
    - 统一字段：`ReasoningEffort`
    - 同时保留 provider raw

- `prediction`
  - 比较专有，偏 speculative/prefill 语义。
  - 先保留，不建议本地伪实现。

#### 专有且不宜强行归一化

- `presence_penalty` / `frequency_penalty`
  - 可以归一化，但对人工模式无意义。
  - 如果文中说 `penalty`，建议拆开处理，不要做成模糊单字段。

### 4.3 Anthropic Messages

缺失字段：

- `max_tokens`
- `temperature`
- `top_p`
- `top_k`
- `stop_sequences`
- `metadata`
- `thinking`
- `service_tier`
- `mcp_servers`
- `context_management`

建议如下。

#### 可归一化且应有真实行为

- `max_tokens`
  - 统一归入 completion/output token 限制。

- `stop_sequences`
  - 统一映射到 `Stop []string`
  - 行为与 Chat Completions `stop` 相同

- `thinking`
  - Anthropic 强专有，但不能忽略。
  - 它不是简单等价于 Responses `reasoning`。
  - 建议统一策略：
    - 建统一概念 `ReasoningConfig`
    - 同时保留 `ThinkingRaw` 原对象
  - 当前若系统只支持“人工公开思考内容”，则只能部分映射：
    - `thinking.enabled/adaptive` 这类控制可保留
    - `budget_tokens` 可映射到推理预算
    - `display` 语义要原样保留

#### 可归一化但先保留

- `temperature`
- `top_p`
- `top_k`
- `metadata`
- `service_tier`
  - 都建议保留

- `mcp_servers`
  - Anthropic 很专有，而且含执行环境语义。
  - 不建议塞进通用 tools。
  - 建议放 `ProviderExtras` 或 `TurnOptions.MCPServers`
  - 前端显示标签：`anthropic-only`
  - 当前若不支持 MCP server 真接入，应明确展示“配置已接收，未执行”

- `context_management`
  - Anthropic 专有上下文控制对象。
  - 当前没有完整 context 管理器时，只能保留。
  - 不要映射成简单布尔。

## 5. 哪些字段是某一套 API 特有，不能强归一化

下面这些字段建议保留“统一入口 + 原样对象”，但不要伪装成完全等价：

- Responses
  - `previous_response_id`
  - `reasoning`
  - `text`
  - `truncation`
  - `include`

- Chat Completions
  - `n`
  - `prediction`
  - `modalities`
  - `audio`
  - `reasoning_effort`

- Anthropic
  - `thinking`
  - `mcp_servers`
  - `context_management`
  - `top_k`

处理建议：

- 后端请求模型保留这些字段
- 前端显式展示协议专有标签
- 未实现真实行为时不要静默吞掉
- request debug / audit 中保留原始 JSON

## 6. 前端应该怎么显示

不建议只是“把未知字段原样 JSON 透传给前端”。

建议前端每个字段显示三类信息：

- 字段名
- 支持级别标签
- 实际行为说明

标签建议：

- `Normalized`
- `Applied`
- `Stored Only`
- `Provider Specific`
- `Unsupported`
- `Partially Applied`

示例：

- `max_output_tokens`
  - `Normalized`
  - `Applied`

- `previous_response_id`
  - `Provider Specific`
  - `Partially Applied`

- `thinking`
  - `Provider Specific`
  - `Stored Only`

- `n`
  - `Unsupported`（当 `n > 1`）

### 6.1 适合做小 Chip 可视化的字段

对于协议调试工具，很多字段即使当前不驱动本地生成行为，仍然非常适合在消息卡片、请求卡片或 timeline 角落显示小 `chip`，这样用户能快速确认“我确实传到了后端”。

建议把这些 `chip` 分成两组：

- `Request Chips`
  - 表示用户请求里带了什么参数
- `Applied Chips`
  - 表示后端真的按这个参数执行了什么行为

建议优先展示这些字段。

#### 很适合显示为 Request Chip 的字段

- `temperature`
- `top_p`
- `top_k`
- `seed`
- `service_tier`
- `reasoning_effort`
- `parallel_tool_calls`
- `store`
- `truncation`
- `max_output_tokens`
- `max_tokens`
- `max_completion_tokens`
- `modalities`
- `audio`
- `user`

这类字段的共同点是：

- 数值或枚举短小
- 用户很关心自己有没有传对
- 即使当前后端没完全执行，也值得让用户肉眼看到

建议展示样式示例：

- `temp=0.7`
- `top_p=0.95`
- `top_k=40`
- `seed=1234`
- `tier=flex`
- `reasoning=high`
- `parallel_tools=true`
- `store=false`
- `truncate=auto`
- `max_out=512`
- `modalities=text,audio`

#### 很适合显示为 Provider-Specific Chip 的字段

- Responses
  - `previous_response_id`
  - `include`
  - `text`
  - `reasoning`

- Chat Completions
  - `prediction`
  - `n`

- Anthropic
  - `thinking`
  - `mcp_servers`
  - `context_management`

建议这些字段不要直接展开整段 JSON，而是先用摘要 chip：

- `prev_resp`
- `include=2`
- `text_cfg`
- `reasoning_cfg`
- `prediction`
- `n=3`
- `thinking`
- `mcp=2`
- `context_mgmt`

用户点击后再展开原始对象。

#### 更适合显示为 Applied Chip 的字段

这些字段只有在后端真的执行了语义时，才建议打 `Applied` 样式：

- `stop`
- `stop_sequences`
- `max_output_tokens`
- `max_tokens`
- `max_completion_tokens`
- `store`
- `parallel_tool_calls`
- `response_format`
- `modalities`
- `audio`

例如：

- `stop hit`
- `max_out enforced`
- `store off`
- `json_schema applied`
- `audio unsupported`

### 6.2 不建议直接做成 Chip、而更适合折叠展示的字段

这些字段通常是对象或数组，信息量太大，不适合直接塞在角落里：

- `metadata`
- `stream_options`
- `reasoning`
- `thinking`
- `text`
- `audio`
- `prediction`
- `context_management`
- `mcp_servers`

建议：

- 先显示一个摘要 chip
- 再在详情面板里展开完整 JSON

示例：

- `metadata(4)`
- `stream_opts`
- `thinking`
- `audio_cfg`
- `mcp=2`

### 6.3 前端最值得补的几类可视化

除了字段 chip，本项目尤其适合再补三类状态标记：

- `Accepted`
  - 后端已成功解析该字段

- `Applied`
  - 后端已真实按该字段执行

- `Ignored` / `Unsupported`
  - 后端收到但未执行，或当前不支持

这样用户看到 `temp=0.7` 时，不会误以为系统真的按采样执行了，只是知道：

- 字段传对了
- 后端接住了
- 但当前也许只是 `Stored Only`

## 7. 哪些字段必须驱动真实行为

优先级最高的一批：

- `stream`
- `max_output_tokens` / `max_tokens` / `max_completion_tokens`
- `stop` / `stop_sequences`
- `tool_choice`
- `parallel_tool_calls` 至少要影响 UI/校验
- `store`
- `response_format`
- `modalities` / `audio` 如果请求显式要求音频

第二优先级：

- `metadata`
- `user`
- `service_tier`
- `reasoning` / `thinking` / `reasoning_effort`

第三优先级：

- `include`
- `stream_options`
- `text`
- `prediction`
- `context_management`
- `mcp_servers`

## 8. Responses SSE 生命周期建议

你列出的旧 Python 版生命周期更接近“完整事件模型”，重构版应该朝它靠拢。

建议以 Responses 为最高保真协议，至少实现以下状态机。

### 8.1 建议支持的核心事件

基础生命周期：

- `response.created`
- `response.in_progress`
- `response.completed`
- `response.failed`

普通文本输出：

- `response.output_item.added`
- `response.content_part.added`
- `response.output_text.delta`
- `response.output_text.done`
- `response.content_part.done`
- `response.output_item.done`

reasoning 输出：

- `response.reasoning_summary_part.added`
- `response.reasoning_summary_text.delta`
- `response.reasoning_summary_text.done`
- `response.reasoning_summary_part.done`

或：

- `response.reasoning_text.delta`
- `response.reasoning_text.done`

tool call 输出：

- `response.output_item.added`
- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`
- `response.output_item.done`

### 8.2 为什么不该只发 `response.created -> output_text.delta -> response.completed`

因为这只够“看起来像流式文本”，不够“协议调试”：

- SDK 或代理可能依赖 item/part 粒度
- reasoning / tool call 无法准确复现
- 前端无法精确回放完整事件树
- 很难验证不同 provider 的事件映射是否正确

### 8.3 推荐的最小完整文本流

对于普通 assistant 文本，推荐至少改成：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added`
4. `response.content_part.added`
5. 多次 `response.output_text.delta`
6. `response.output_text.done`
7. `response.content_part.done`
8. `response.output_item.done`
9. `response.completed`

### 8.4 推荐的最小完整 tool call 流

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added` `item.type=function_call`
4. 多次 `response.function_call_arguments.delta`
5. `response.function_call_arguments.done`
6. `response.output_item.done`
7. `response.completed`

### 8.5 推荐的最小完整 reasoning 流

如果是 summary 模式：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added` `item.type=reasoning`
4. `response.reasoning_summary_part.added`
5. 多次 `response.reasoning_summary_text.delta`
6. `response.reasoning_summary_text.done`
7. `response.reasoning_summary_part.done`
8. `response.output_item.done`
9. `response.completed`

如果是 reasoning text 模式：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added`
4. `response.content_part.added`
5. 多次 `response.reasoning_text.delta`
6. `response.reasoning_text.done`
7. `response.content_part.done`
8. `response.output_item.done`
9. `response.completed`

## 9. 当前重构版 SSE 与建议差距

当前 `backend/internal/protocol/stream.go` 已有：

- `response.created`
- `response.output_text.delta`
- `response.completed`
- `response.failed`
- 一部分 reasoning added/delta/done
- `response.output_item.added`
- `response.content_part.added`
- `response.content_part.done`
- `response.output_item.done`

仍缺或不完整：

- `response.in_progress`
- 普通文本流的 `response.output_text.done`
- tool call 流的细粒度 `response.function_call_arguments.delta/done`
- 更稳定的 item/part index 语义
- 非 reasoning 文本输出也应走 item/part 生命周期

建议顺序：

1. 先补 `response.in_progress`
2. 再补普通文本的 item/part 生命周期
3. 再补 tool call arguments delta/done
4. 最后统一整理 completed payload 与 usage/details

## 10. `new-api` 能不能作为参考

结论：**可以参考，但不能当规范依据。**

适合参考的部分：

- 它如何消费 `response.output_text.delta`
- 它如何把 Responses 事件转译成 Chat Completions chunk
- 它如何识别 `response.output_item.added/done`
- 它如何处理 `response.reasoning_summary_text.delta`

不适合直接相信的部分：

- 它没有证明自己完整实现了 Responses 全生命周期
- 一些事件是注释掉或忽略的
- 它的目标更像网关转译，不是协议调试器的高保真事件模拟器

所以：

- `new-api` 适合做“消费兼容性回归样本”
- 不适合做“你应该发哪些事件”的最终清单

## 11. 建议的实现策略

### 第一阶段：先把字段接住

目标：

- 三套协议所有常见顶层字段都能解析
- 未支持语义也不丢
- 落入 `TurnOptions + ProviderExtras + RawBody`

### 第二阶段：把关键行为做实

先做：

- token 上限
- stop sequences
- store
- parallel tool calls 的能力声明
- audio/modalities 的显式 unsupported

### 第三阶段：补全 Responses 生命周期

目标：

- 文本
- reasoning
- tool call
- failed

都能走完整事件树。

### 第四阶段：前端展示支持级别

这样调试工具才真的能回答：

- “这个字段有没有接收到？”
- “这个字段有没有真实生效？”
- “这个字段是不是这套协议专有？”
- “当前不支持是后端没做，还是该协议本身不通用？”

## 12. 最终建议

如果目标是“协议完整实现”，最重要的不是一次性把所有字段都做成有行为，而是：

- **所有字段先可见、可保留、可审计**
- **关键控制字段必须真实生效**
- **协议专有字段不要被错误归一化**
- **Responses SSE 必须补成完整生命周期**

一句话总结：

`chatapi-go-refactor` 下一步最应该做的不是单纯继续堆字段，而是先建立“字段支持级别模型 + 完整 Responses 事件状态机”，这样三套协议后续再扩都不会乱。
