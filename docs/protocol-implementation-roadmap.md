# 协议完整实现路线

本文描述 `chatapi-go-refactor` 后续把 Responses / Chat Completions / Anthropic Messages 三套协议实现完整的落地路线。

目标不是把三套协议分别堆三遍，而是先抽出统一语义，再由协议专属 encoder 输出各自格式。这样相同语义只实现一次，不同协议只负责表达差异。

相关背景文档：

- [protocol-compatibility-gap-analysis.md](/home/zyf/Code/Projects/chatapi-go-refactor/docs/protocol-compatibility-gap-analysis.md)
- [protocol-stream-event-reference.md](/home/zyf/Code/Projects/chatapi-go-refactor/docs/protocol-stream-event-reference.md)

## 1. 总体原则

### 1.1 统一语义优先

这些能力应作为统一语义实现一次：

- 文本输出
- 思考 / reasoning 输出
- tool call
- tool result
- error / failed / abort
- stop sequence
- max output token 限制
- structured output 配置
- request options 保留和展示

然后分别映射到：

- Responses event tree
- Chat Completions chunk
- Anthropic Messages event

### 1.2 协议差异保留在边缘

这些内容不要强行归一化成完全等价：

- Responses `previous_response_id`、`reasoning`、`text`、`include`、`truncation`
- Chat Completions `n`、`prediction`、`modalities`、`audio`、`reasoning_effort`
- Anthropic `thinking`、`mcp_servers`、`context_management`、`top_k`

它们应进入统一 options 壳，但保留原始字段来源和 provider-specific 标签。

### 1.3 handler / workspace 不理解协议细节

最终链路应该是：

```text
HTTP / WS handler
  -> ingress / workspace
  -> chat/control
  -> chat/turn
  -> chat/protocolruntime
  -> protocol encoder
  -> pending stream / events bus
  -> streaming SSE / workspace WS
```

`handler` 只适配 HTTP/WS。

`workspace` 只处理工作台实时控制语义。

`turn` 只处理 pending、draft、complete、abort 状态机。

`protocolruntime` 把统一输出动作翻译成协议事件。

`protocol` 只放纯协议模型、解析、编码。

## 2. 目标模块结构

### 2.1 现有模块继续保留

- `backend/internal/protocol`
  - 纯协议层。
  - 不依赖 service。
  - 不读写 pending、conversation、workspace。

- `backend/internal/service/chat/control`
  - HTTP 和 WS 的统一控制入口。

- `backend/internal/service/chat/turn`
  - pending turn 生命周期。

- `backend/internal/service/chat/streaming`
  - SSE 写出。

- `backend/internal/service/chat/workspace`
  - 工作台 WS snapshot / command / realtime event。

### 2.2 新增模块

新增：

```text
backend/internal/service/chat/protocolruntime
backend/internal/protocol/debugview
```

`protocolruntime` 职责：

- 接收统一的 turn output action。
- 根据 conversation/request protocol 选择 encoder。
- 返回协议 stream events。
- 管理单个 pending turn 的协议运行时状态，例如 Anthropic 当前 content block 是否已打开。

`debugview` 职责：

- 从 `TurnRequest` 投影出 UI/控制台专用字段 chip。
- 标记字段来源协议、类别、支持级别。
- 让前端展示协议特有字段时不直接解析 raw request body。

建议类型：

```go
type ActionKind string

const (
    ActionDelta    ActionKind = "delta"
    ActionComplete ActionKind = "complete"
    ActionAbort    ActionKind = "abort"
)

type Action struct {
    Kind                ActionKind
    DeltaText           string
    OutputText          string
    Mode                string
    ToolName            string
    ToolCallID          string
    ToolOutput          string
    ReasoningStreamMode string
    ErrorBody           map[string]any
}

type Result struct {
    StreamEvents []protocol.StreamEvent
}
```

## 3. 分阶段路线

## 阶段 1：扩统一请求模型

### 要加什么

在 `backend/internal/protocol` 新增：

- `options.go`
  - `TurnOptions`
  - `ProviderExtras`
  - 支持级别标记类型

修改：

- `request.go`
  - `TurnRequest` 增加 `Options TurnOptions`
  - `TurnRequest` 增加 `RawBody map[string]any`

- `request_responses.go`
  - 解析 `instructions`
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

- `request_openai.go`
  - 解析 `max_tokens`
  - `max_completion_tokens`
  - `temperature`
  - `top_p`
  - `stop`
  - `n`
  - `presence_penalty`
  - `frequency_penalty`
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

- `request_anthropic.go`
  - 解析 `max_tokens`
  - `temperature`
  - `top_p`
  - `top_k`
  - `stop_sequences`
  - `metadata`
  - `thinking`
  - `service_tier`
  - `mcp_servers`
  - `context_management`

- `request_build.go`
  - 能把已归一化字段按原协议 rebuild 回请求体。
  - Responses structured output 使用 `text.format` 语义，不再只使用 Chat Completions 的 `response_format`。

### 要改哪些系统

- `service/chat/turn/submitter.go`
  - request debug metadata 写入 `options` 摘要和 `raw_body`。

- `frontend/src/components/ChatMessageList.tsx`
  - request debug 中展示 options。

- `frontend/src/lib/chat-format.tsx`
  - tool schema 继续保持从 debug 读取，但兼容 `parameters` / `input_schema` / `function.parameters`。

### 验收测试

后端：

- `go test ./internal/protocol -count=1`
- 新增测试：
  - Responses options parse/rebuild。
  - Chat Completions options parse/rebuild。
  - Anthropic options parse/rebuild。
  - unknown provider-specific 字段不会丢失。
  - `n > 1`、audio-only 这类暂不支持语义能被显式标记。

前端：

- `npm --prefix frontend run build`
- 构造带 `input_schema` 的 tool schema，前端能显示字段。

## 阶段 1.5：协议 debug view / UI capability 投影

### 要加什么

新增：

```text
backend/internal/protocol/debugview
```

核心类型：

- `OptionChip`
  - `key`
  - `label`
  - `value`
  - `protocol`
  - `category`
  - `support_level`
  - `detail`

- `Projection`
  - `option_chips`

职责：

- 从统一的 `TurnRequest.Options` 生成 UI 可直接展示的 chip。
- 把 Responses / Chat Completions / Anthropic 的特有字段显式标成 provider-specific。
- 把暂不执行的字段标成 `stored_only` 或 `partially_applied`。
- 把已知不支持的字段标成 `unsupported`，例如 `n > 1`。

### 要改哪些系统

- `service/chat/turn/submitter.go`
  - 创建 pending request 时调用 `debugview.ProjectRequest`。
  - 把 `option_chips` 写入 request debug metadata。

- `repository/sqlite` 和 `repository/postgresql`
  - `request_debug` 增加 `option_chips`。

- `frontend/src/types/chat.ts`
  - 增加 `OptionChip` 类型。

- `frontend/src/components/ChatMessageList.tsx`
  - 用户消息请求详情里展示 option chips。
  - 原始 `request_options/raw_request_body` 仍放在折叠 debug 信息中。

### 验收测试

- `go test ./internal/protocol/debugview -count=1`
- `npm --prefix frontend run build`
- 构造带 Anthropic `thinking/mcp_servers`、Responses `reasoning/text`、Chat `n>1` 的请求：
  - 前端能看到 provider-specific chip。
  - `n>1` 能显示 unsupported chip。
  - service / turn / workspace 不需要自己解析协议特有字段。

## 阶段 2：引入 `chat/protocolruntime`

### 要加什么

新增目录：

```text
backend/internal/service/chat/protocolruntime
```

文件建议：

- `runtime.go`
  - `Runtime`
  - `Action`
  - `Result`

- `state.go`
  - 每个 pending turn 的协议状态：
    - Responses output index / content index
    - Anthropic content block index / block open 状态
    - Chat Completions choice index

- `responses.go`
  - Responses action 到 event tree。

- `chat.go`
  - Chat Completions action 到 chunk。

- `anthropic.go`
  - Anthropic action 到 event。

- `error.go`
  - 三套协议 error / abort / failed 输出。

### 要改哪些系统

- `service/chat/turn/pending_types.go`
  - `PendingTurn` 增加 runtime state。
  - `PendingEvent` 增加 `StreamEvents []protocol.StreamEvent`。

- `service/chat/turn/service.go`
  - `UpdateDraft` 和 complete 不再直接拼 `DeltaText` 让 streaming 判断协议。
  - 改成调用 `protocolruntime.Apply(action)`。

- `service/chat/streaming/service.go`
  - 移除 `anthropicBlockStarted`。
  - 只写 `PendingEvent.StreamEvents`。

### 验收测试

后端：

- `go test ./internal/service/chat/protocolruntime ./internal/service/chat/streaming ./internal/service/chat/turn -count=1`

新增测试：

- Responses 文本 delta：
  - `response.created`
  - `response.in_progress`
  - `response.output_item.added`
  - `response.content_part.added`
  - `response.output_text.delta`
  - `response.output_text.done`
  - `response.content_part.done`
  - `response.output_item.done`
  - `response.completed`

- Chat Completions 文本 delta：
  - 首块 role
  - 多块 content delta
  - 尾块 finish_reason
  - `[DONE]`

- Anthropic 文本 delta：
  - `message_start`
  - `content_block_start`
  - `content_block_delta`
  - `content_block_stop`
  - `message_delta`
  - `message_stop`

## 阶段 3：补 tool call / tool result 完整流

### 要加什么

在 `protocolruntime` 实现统一 tool action：

- `ModeToolCall`
- `ModeToolResult`

Responses 输出：

- `response.output_item.added`，`item.type=function_call`
- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`
- `response.output_item.done`
- complete 时 `response.completed.output` 包含 `function_call`

Chat Completions 输出：

- `choices[].delta.tool_calls[].id`
- `choices[].delta.tool_calls[].type=function`
- `choices[].delta.tool_calls[].function.name`
- `choices[].delta.tool_calls[].function.arguments`
- finish reason `tool_calls`

Anthropic 输出：

- `content_block_start`，`content_block.type=tool_use`
- `content_block_delta`，`delta.type=input_json_delta`
- `content_block_stop`
- `message_delta.stop_reason=tool_use`
- `message_stop`

### 要改哪些系统

- `frontend/src/hooks/useChatWorkspace.ts`
  - tool call complete 后不阻塞下一次输出。
  - command ack / error 明确处理。

- `frontend/src/hooks/chatWorkspace/buildToolCallPayload.ts`
  - object / array / enum / required 字段校验更明确。

- `frontend/src/components/ChatPane.tsx`
  - tool schema 为空时说明来自“请求未声明 schema”，不是“当前 tool 无参数”。

### 验收测试

后端：

- Responses tool call SSE sequence 测试。
- Chat Completions tool call chunk 测试。
- Anthropic tool use stream 测试。
- tool result 输入 parse/rebuild 测试：
  - Responses `function_call_output`
  - Chat `role=tool`
  - Anthropic `tool_result`

前端：

- `npm --prefix frontend run build`
- 用含 `input_schema` 的 tool schema 请求：
  - 前端显示参数字段。
  - 必填字段为空不能发送。
  - 发送后能继续输出或结束。

## 阶段 4：补 thinking / reasoning 完整流

### 要加什么

统一语义：

- `ModeThinking`
- `ReasoningStreamMode`
  - `summary`
  - `reasoning_text`

Responses 输出：

- summary 模式：
  - `response.output_item.added`，`item.type=reasoning`
  - `response.reasoning_summary_part.added`
  - `response.reasoning_summary_text.delta`
  - `response.reasoning_summary_text.done`
  - `response.reasoning_summary_part.done`
  - `response.output_item.done`

- reasoning text 模式：
  - 如果 SDK/上游使用官方事件名，按文档统一到支持集合。
  - 当前代码里的非标准 `response.reasoning_text.delta` 需要校正或明确标记为兼容扩展。

Chat Completions 输出：

- 若只是展示思考内容，不能伪装成 OpenAI 官方 chunk 字段。
- 推荐作为普通 assistant content 中的 `<think>...</think>` 兼容输出，或者作为 ChatAPI 扩展 metadata 只进入 workspace timeline。

Anthropic 输出：

- `content_block_start`，`content_block.type=thinking`
- `content_block_delta`，`delta.type=thinking_delta`
- `content_block_stop`

### 要改哪些系统

- `frontend/src/types/chat.ts`
  - `ReasoningStreamMode` 修正 `summery` 拼写为 `summary`，保留旧值兼容迁移。

- `frontend/src/hooks/useChatWorkspace.ts`
  - thinking 输出时按协议选择模式。

- `frontend/src/lib/chat-format.tsx`
  - Chat Completions `<think>` 展示继续保留。
  - Responses / Anthropic 的 thinking timeline 由后端显式 metadata 驱动，不再靠纯文本猜。

### 验收测试

后端：

- Responses summary reasoning event sequence。
- Responses reasoning text event sequence。
- Anthropic thinking_delta sequence。
- Chat Completions thinking fallback 不破坏 chunk 协议。

前端：

- thinking chip / card 能显示。
- Responses summary 和 reasoning text 模式能切换。
- “添加思考内容”不再输出成普通 answer。

## 阶段 5：补 error / incomplete / cancelled / abort 生命周期

### 要加什么

统一 error action：

- `ActionError`
- error kind：
  - user abort
  - request disconnected
  - validation failed
  - internal failed
  - incomplete / max token

Responses 输出：

- `response.failed`
- `response.incomplete`
- `response.cancelled`

Chat Completions 输出：

- OpenAI-style error body。
- 流式场景写 error event 后 `[DONE]`。

Anthropic 输出：

- `event: error`
- body 符合 Anthropic error shape。

### 要改哪些系统

- `protocol/errors.go`
  - 增加更细的 protocol error kind。

- `service/chat/turn/service.go`
  - abort / disconnect / expire 都通过 protocolruntime 生成协议输出。

- `service/chat/workspace`
  - timeline system event 继续作为工作台事件，不和协议 error 混在一起。

### 验收测试

后端：

- abort 后 SSE 客户端收到协议正确 error / done。
- disconnect 后数据库 pending 状态立刻更新。
- workspace 收到 timeline system event。
- protocol error body snapshot 测试。

前端：

- request disconnected 显示为 timeline system chip。
- error complete 后按钮状态恢复，可继续下一次输出。

## 阶段 6：实现 stop / max output / store 的真实行为

### 要加什么

统一行为：

- stop sequence 截断。
- max output chars/tokens 限制。
- store=false 的本地策略。

建议新增：

```text
backend/internal/service/chat/outputpolicy
```

职责：

- 对人工 delta / complete 文本应用 stop sequence。
- 对 max output 做本地限制。
- 返回 applied chips / policy result。

不建议放到 `protocolruntime`，因为这是行为策略，不是协议编码。

### 要改哪些系统

- `service/chat/control`
  - command 进入 turn 前应用 output policy，或由 turn 调用 policy。

- `service/chat/turn`
  - policy result 写入 message metadata / request debug。

- `frontend`
  - 显示 `stop hit`、`max_out enforced` 这类 applied chip。

### 验收测试

后端：

- stop 命中后 delta 被截断。
- stop 未命中正常输出。
- max output 超限返回 incomplete 或本地 policy error。
- `store=false` 的行为被显式测试；如果暂不支持，必须返回或展示 partially applied。

## 阶段 7：前端协议调试面收口

### 要加什么

前端新增或整理：

```text
frontend/src/lib/protocol-debug.ts
frontend/src/components/ProtocolDebugPanel.tsx
```

职责：

- 展示 request options。
- 展示 raw request。
- 展示 normalized request。
- 展示 supported / applied / stored-only / unsupported 标签。
- 展示最近生成的协议事件序列。

### 要改哪些系统

- `ChatMessageList.tsx`
  - 请求 debug 展示委托给 `ProtocolDebugPanel`。

- `ChatPane.tsx`
  - composer 输出模式和协议能力绑定。

- `useChatWorkspace.ts`
  - workspace command error / ack 闭环。

### 验收测试

前端：

- `npm --prefix frontend run build`
- 手工或组件测试覆盖：
  - Responses request options 展示。
  - Chat Completions tool schema 表单。
  - Anthropic thinking 配置展示。
  - unsupported 字段展示。

## 阶段 8：协议探针和回归测试常态化

### 要加什么

整理现有探针：

```text
tests/protocol_stream_probe.py
tests/responses_sse_probe.py
tests/protocol_fixtures/
```

新增 golden fixtures：

- `responses_text_stream.json`
- `responses_tool_call_stream.json`
- `responses_reasoning_stream.json`
- `chat_text_stream.json`
- `chat_tool_call_stream.json`
- `anthropic_text_stream.json`
- `anthropic_tool_use_stream.json`
- `anthropic_thinking_stream.json`

### 要改哪些系统

- CI 或本地脚本：
  - Go unit tests 负责内部 encoder snapshot。
  - Python probe 负责真实 HTTP SSE 行为。

### 验收测试

- `go test ./internal/protocol ./internal/service/chat/... -count=1`
- `npm --prefix frontend run build`
- `uv run tests/protocol_stream_probe.py --base-url http://localhost:PORT`

## 4. 推荐实际执行顺序

第一轮：

1. 扩 `TurnRequest.Options` 和 `RawBody`。
2. 补 request parse/rebuild 测试。
3. 前端 tool schema 兼容 `input_schema`。

第二轮：

1. 新增 `protocol/debugview`。
2. request debug 写入 `option_chips`。
3. 前端请求详情展示 option chips。

第三轮：

1. 新增 `chat/protocolruntime`。
2. 把 `streaming` 改成只写 `StreamEvents`。
3. 补三协议文本流完整生命周期测试。

第四轮：

1. 补 tool call / tool result 完整流。
2. 修前端 tool form 和 command 状态。
3. 验收当前用户反馈的 tool schema 和“不能继续输出/结束输出”问题。

第五轮：

1. 补 thinking / reasoning。
2. 修 `summery` 拼写兼容。
3. 前端 thinking 显示从文本猜测逐步切到 timeline metadata。

第六轮：

1. 补 error / abort / incomplete 生命周期。
2. 统一 request disconnected 的协议输出和 workspace system event。

第七轮：

1. 实现 output policy。
2. stop / max output / store 行为落地。

第八轮：

1. 前端协议调试面板。
2. golden fixtures 和 probe 常态化。

## 5. 不建议的做法

不建议：

- 在 `streaming.Service` 里继续维护协议状态。
- 在 `turn.Service` 里直接拼 Responses / Anthropic 事件。
- 为 Responses / Chat / Anthropic 各建一个业务 service。
- 让前端通过猜 `request_debug` 或 raw metadata 决定协议语义。
- 为每个新 event 直接横向修改 handler、turn、streaming、workspace、frontend。

这些做法短期能补功能，但会让协议调试工具长期维持多套 authority。

## 6. 完成标准

协议完整实现至少应满足：

- 三套协议都能接收常见请求字段，并保留 raw/options。
- 三套协议的文本流生命周期完整。
- 三套协议的 tool call / tool result 可用。
- Responses / Anthropic 的 reasoning/thinking 有协议正确输出。
- abort / error / disconnect 有协议正确输出，同时 workspace 有 timeline system event。
- 前端能展示 tool schema、request options、unsupported/applied 状态。
- 后端 unit test 覆盖 encoder 和 runtime。
- HTTP SSE probe 能跑通真实端到端流。
