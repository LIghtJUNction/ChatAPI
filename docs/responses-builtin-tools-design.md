# Responses Built-in Tools Design

本文描述 ChatAPI 如何在工作台中人工模拟 OpenAI Responses API 的 built-in tools，并把结果按官方 Responses stream/output item 格式返回给发起请求的客户端。

目标工具：

- `web_search`
- `image_generation`
- `code_interpreter`

非目标：

- 不在 ChatAPI 后端真实联网搜索、真实生图或真实执行代码。
- 不把 built-in tools 混入普通 function tool call / tool result 闭环。
- 不为 Chat Completions 或 Anthropic Messages 发明同名事件。

## 1. 背景与语义

Responses API 的 built-in tools 是服务端内置工具。它们和普通 function tools 的语义不同：

- 普通 function tool：模型要求调用方执行工具，客户端后续提交 `function_call_output`。
- built-in tool：模型服务端自己执行工具，并在同一条 response stream 中输出进度和最终 output item。

ChatAPI 是人工 pending turn 系统，因此这里的“服务端执行”由工作台用户通过表单模拟。对外客户端仍看到标准 Responses built-in tool 事件。

## 2. 前端设计

### 2.1 显示条件

在当前 composer 的“工具调用”tab 后面新增一个隐藏 tab：

- label: `内置工具`
- value: `builtin_tool`

只在以下条件满足时显示：

- 当前 conversation/request 的 `request_format` 是 `responses`
- 原始请求 `tools[]` 中包含至少一个 supported built-in tool：
  - `web_search`
  - `image_generation`
  - `code_interpreter`

`url_context` 不显示，因为它不是 OpenAI Responses built-in tool，RikkaHub 在 Responses API 中也将它标记为 unsupported。

### 2.2 表单结构

tab 内第一项是工具类型下拉菜单。下拉选项只包含本次请求启用过的 built-in tools。

```ts
type BuiltinToolKind = 'web_search' | 'image_generation' | 'code_interpreter'
```

#### Web Search

字段：

- `query`: string，必填
- `phase`: enum，默认 `completed`
  - `searching`
  - `completed`

首版可以只暴露“一次性完成”，内部同时发 `in_progress -> searching -> completed`。

#### Image Generation

字段：

- `image`: 文件上传或图片 URL/base64 输入，必填
- `status`: 默认 `completed`
- `partial_image`: 后续扩展，首版不需要

提交前端时可以传本地文件或已上传媒体 ID。后端最终需要拿到可放入 Responses `result` 的 base64 图片。为了兼容 RikkaHub，最关键的是在 `response.output_item.done.item.result` 中返回 base64。

#### Code Interpreter

字段：

- `code`: string，必填
- `logs`: string，可选
- `image_url`: string，可选
- `status`: 默认 `completed`

首版不真实执行代码，只模拟 code interpreter output item 和相关 progress event。

### 2.3 UI 呈现

工作台 timeline 中应把 built-in tool 输出显示成专门的 assistant/tool progress item，而不是普通 function tool call 表单结果。

建议卡片：

- `web_search`: `Searched: {query}`
- `image_generation`: 显示缩略图和 `Image generated`
- `code_interpreter`: 显示代码块、logs 和可选图片

## 3. 后端命令模型

新增 workspace/control 命令，不复用普通 `tool_call`。

```go
type BuiltinToolCommand struct {
    ConversationID string
    RequestID      string
    ToolKind       string
    WebSearch      *WebSearchPayload
    Image          *ImageGenerationPayload
    Code           *CodeInterpreterPayload
}

type WebSearchPayload struct {
    Query string
    Phase string
}

type ImageGenerationPayload struct {
    ImageBase64 string
    MediaType   string
    Status      string
}

type CodeInterpreterPayload struct {
    Code     string
    Logs     string
    ImageURL string
    Status   string
}
```

接口进入点建议放在现有 turn control application service，而不是 handler 或 workspace hub：

```text
HTTP/WS workspace command
  -> service/chat/control
  -> service/chat/turn
  -> service/chat/protocolruntime
  -> service/chat/streaming
```

## 3.1 Existing Chain Impact

这部分不能“完全复用”普通 function tool call 链路，但也不需要重做一条独立的大系统。正确做法是复用现有 turn/control/streaming/timeline 管道，在“动作类型”和“协议编码”处增加一个窄分支。

### 可以复用的链路

以下部分应该继续复用：

- HTTP / WS workspace command transport
  - 仍然由工作台发 command。
  - 仍然走统一 actor/session 鉴权。
  - 仍然走现有 command ack/error 机制。

- `service/chat/control`
  - 继续作为 workspace 控制命令入口。
  - 负责校验 actor 是否能操作该 conversation/request。
  - 负责把前端 payload 变成 turn action。

- `service/chat/turn`
  - 继续拥有 pending turn 状态迁移。
  - 继续负责把 action 写入 stream/runtime。
  - 继续负责完成后持久化 message/timeline。

- `service/chat/protocolruntime`
  - 继续作为“统一动作 -> 协议 SSE event”的唯一编码点。
  - 新增 `ActionBuiltinTool`，但只在 Responses 协议下产生事件。

- `service/chat/streaming`
  - 不需要知道 web/image/code 的业务语义。
  - 只继续把 runtime 产出的 `protocol.StreamEvent` 写给等待中的 HTTP SSE。

- `service/chat/events` + `workspace`
  - 继续通过 timeline append / conversation upsert 广播前端。
  - 不为 built-in tool 新开 websocket transport。

### 必须特殊处理的地方

以下地方需要显式分叉，不能强行复用普通 tool call：

#### 1. Request capability projection

现有 normalized tool schemas 主要面向 function tools：

```text
tools[].type=function
functions[]
Anthropic input_schema tools
```

Responses built-in tools 不是 schema-driven function tool。它们没有用户可填写的 JSON Schema，也没有 `function.name/parameters`。

因此需要在 request projection 中额外产生：

```json
{
  "builtin_tools": [
    { "kind": "web_search" },
    { "kind": "image_generation" },
    { "kind": "code_interpreter" }
  ]
}
```

这会破坏“所有工具都能归一成 normalized tool schema”的假设，但这个破坏是正确的：built-in tool 和 function tool 本来就不是同一个概念。

#### 2. Frontend composer mode

不能把它塞进现有 `tool_call` tab。

普通 `tool_call` tab 的语义是：

- 选择 function schema
- 填 arguments
- 对外输出 `function_call`
- 调用方后续提交 `function_call_output`

Built-in tool tab 的语义是：

- 选择本次请求启用的 built-in tool
- 人工填写服务端工具执行结果/进度
- 对外输出 Responses built-in progress/output item
- 调用方不会再提交 tool result

因此前端需要新增 composer mode：

```ts
'builtin_tool'
```

它可以复用表单样式和校验 UI，但不能复用 tool call payload builder。

#### 3. Turn action type

不能用 `Mode=tool_call` 承载 built-in tools。

`tool_call` 当前代表 function call。强行复用会让后端无法区分：

- 要不要关闭/等待 tool result
- 要不要输出 `function_call_arguments.delta/done`
- 要不要生成 function call message metadata

应新增：

```go
ActionBuiltinTool
```

这样 runtime 和 turn service 可以保持清晰分派。

#### 4. Protocol runtime output

Built-in tools 只属于 Responses。

因此 `protocolruntime` 需要特殊规则：

- Responses: 输出官方 built-in tool stream events。
- Chat Completions: unsupported / no-op，不能发非官方 chunk。
- Anthropic Messages: unsupported / no-op，不能发非官方 content block。

这会破坏“每个 action 都能映射到三套协议”的通用性，但这是协议事实，不是实现缺陷。

#### 5. Timeline item projection

现有 timeline 里有 message/event 拼装，普通 tool call 可能展示为工具调用卡片。

Built-in tool 应新增独立 timeline kind：

```ts
kind: 'builtin_tool'
```

不能伪装成普通 assistant text，也不能伪装成 function tool call。否则 UI 和后续状态恢复会把“客户端需要执行工具”和“服务端已执行内置工具”混在一起。

### 不应该特殊处理的地方

以下地方不应该为 built-in tools 加例外：

- Router
  - 不加新 HTTP endpoint。
  - 继续走现有 workspace command / control endpoint。

- Streaming writer
  - 不识别 built-in tool。
  - 只写 runtime event。

- Workspace hub
  - 不识别 built-in tool 协议细节。
  - 只广播 timeline projection。

- Repository 表结构
  - 首版不需要新增专门表。
  - built-in tool timeline/message metadata 可先进入现有 message/event metadata。

- Media preprocess
  - image generation 的输出图片不是用户输入 preprocess。
  - 如果前端上传一张“生成结果图”，可以复用已有 media/localstore，但语义应标为 generated/output media，而不是 input image。

### 通用性边界

这次新增会引入一个新的顶层概念：

```text
function tool != built-in tool
```

这是必要的通用性破坏。它避免了更坏的混合：

- 把 built-in tool 当 function schema。
- 把 server-side tool progress 当 client-side tool call。
- 把 image/code/search output 塞进 assistant text。

新增概念后，系统反而更稳定：

- function tool 链路继续只处理 function call / tool result。
- built-in tool 链路只处理 Responses 内置工具 output item。
- timeline 可以统一展示两者，但不混淆其领域语义。

## 3.2 Minimal Implementation Surface

最小实现只需要改这些位置：

- `backend/internal/protocol`
  - 解析 Responses `tools[]` 中的 built-in tool。
  - 在 debug/projection 中暴露 `builtin_tools`。

- `backend/internal/service/chat/control`
  - 新增 built-in tool command 校验和分派。

- `backend/internal/service/chat/turn`
  - 接收 built-in tool action。
  - 写 stream event。
  - 写 timeline metadata。

- `backend/internal/service/chat/protocolruntime`
  - 新增 `ActionBuiltinTool`。
  - 实现 Responses web/image/code 事件序列。

- `backend/internal/service/chat/timeline` 或 workspace projection
  - 把 built-in tool metadata 投影成 timeline item。

- `frontend/src/hooks/useChatWorkspace.ts`
  - 根据 request projection 判断是否显示 tab。
  - 提交 `builtin_tool` command。

- `frontend/src/components/ChatPane.tsx`
  - 新增隐藏 tab 和三种表单。

- `frontend/src/lib/visibleMessages.ts` / timeline renderer
  - 展示 built-in tool card。

不需要改：

- protocol router。
- auth middleware。
- SSE writer。
- 普通 function tool schema parser。
- 普通 tool result 提交流程。

## 3.3 Risk Points

- Web search query 对外客户端不一定可见。
  - SDK stream event 没有 query 字段。
  - query 必须至少进入 ChatAPI timeline。
  - 是否放入 output item extra metadata 需要真实客户端验证，默认不要污染官方形状。

- Image generation base64 可能很大。
  - 对外 Responses `result` 是 base64。
  - 内部存储应尽量使用 media asset/path，只有写 SSE 时再转成 base64。
  - 不要把 base64 长期存入 message metadata。

- Code interpreter image output URL。
  - SDK output image 是 URL，不是 base64。
  - 如果由本地媒体产生，需要有可访问 `/api/media/assets/{fileID}` URL 或签名 URL。

- Output index 管理。
  - built-in tool output item 和 assistant message/function call/reasoning 共用 Responses output array。
  - `protocolruntime` 必须统一管理 `responsesOutputIndex`，不能每个 action 自己从 0 开始。

## 4. Runtime Action

在 `service/chat/protocolruntime` 中新增 action kind：

```go
const ActionBuiltinTool ActionKind = "builtin_tool"

type BuiltinToolAction struct {
    ToolKind string
    ItemID   string

    Query string

    ImageBase64 string
    MediaType   string

    Code     string
    Logs     string
    ImageURL string
}
```

只有 `ProtocolResponses` 支持该 action。其他协议返回空事件或显式 unsupported，由 control 层决定错误语义。

## 5. Responses Event Mapping

所有 built-in tool 都应该作为 Responses output item 输出。这样比只发 progress event 更兼容客户端，也更符合 SDK union。

### 5.1 Web Search

SDK stream events：

- `response.web_search_call.in_progress`
- `response.web_search_call.searching`
- `response.web_search_call.completed`

事件字段：

- `type`
- `item_id`
- `output_index`
- `sequence_number`

建议输出序列：

```text
response.output_item.added
response.web_search_call.in_progress
response.web_search_call.searching
response.web_search_call.completed
response.output_item.done
```

注意：openai-go 的 web search progress event 本身不携带 query 字段。为了让工作台显示 query，query 应存入 ChatAPI timeline metadata。对外客户端能否显示 query 取决于其是否读取 output item 额外字段；不能依赖非官方字段。

### 5.2 Image Generation

SDK stream events：

- `response.image_generation_call.in_progress`
- `response.image_generation_call.generating`
- `response.image_generation_call.partial_image`
- `response.image_generation_call.completed`

output item：

```json
{
  "id": "ig_...",
  "type": "image_generation_call",
  "status": "completed",
  "result": "<base64>"
}
```

首版建议输出序列：

```text
response.output_item.added item.status=in_progress
response.image_generation_call.in_progress
response.image_generation_call.generating
response.image_generation_call.completed
response.output_item.done item.status=completed item.result=<base64>
```

`partial_image` 后续再加。RikkaHub 当前关键消费点是 `response.output_item.done.item.result`。

### 5.3 Code Interpreter

请求 tool 结构：

```json
{
  "type": "code_interpreter",
  "container": {
    "type": "auto",
    "file_ids": []
  }
}
```

SDK stream events：

- `response.code_interpreter_call.in_progress`
- `response.code_interpreter_call_code.delta`
- `response.code_interpreter_call_code.done`
- `response.code_interpreter_call.interpreting`
- `response.code_interpreter_call.completed`

output item：

```json
{
  "id": "ci_...",
  "type": "code_interpreter_call",
  "code": "print('hello')",
  "container_id": "container_...",
  "outputs": [
    { "type": "logs", "logs": "hello\n" },
    { "type": "image", "url": "https://..." }
  ],
  "status": "completed"
}
```

首版建议输出序列：

```text
response.output_item.added item.status=in_progress
response.code_interpreter_call.in_progress
response.code_interpreter_call_code.delta
response.code_interpreter_call_code.done
response.code_interpreter_call.interpreting
response.code_interpreter_call.completed
response.output_item.done item.status=completed
```

## 6. Timeline And WS Projection

新增 timeline item kind：

```ts
type TimelineBuiltinToolItem = {
  kind: 'builtin_tool'
  tool_kind: 'web_search' | 'image_generation' | 'code_interpreter'
  status: 'searching' | 'generating' | 'interpreting' | 'completed' | 'failed'
  title: string
  query?: string
  image_url?: string
  code?: string
  logs?: string
}
```

timeline 是工作台视图，不等于外部协议事件。它可以携带 query、logs、缩略图等 UI 友好字段。

WS 增量仍走现有 timeline append / conversation upsert 语义，不需要为 built-in tool 单独开 transport。

## 7. Request Capability Projection

后端需要在 request debug/projection 中暴露当前请求支持的 built-in tools，避免前端自己解析 raw body。

建议在 debug view 增加：

```json
{
  "builtin_tools": [
    { "kind": "web_search", "label": "搜索" },
    { "kind": "image_generation", "label": "图片生成" },
    { "kind": "code_interpreter", "label": "代码执行" }
  ]
}
```

来源：

- Responses `tools[]`
- 只识别 SDK 标准 built-in tool 类型
- function tools 仍走已有 normalized tool schema

## 8. Validation

control 层校验：

- request 必须是 Responses 协议
- selected tool must exist in this request's `builtin_tools`
- `web_search.query` 非空
- `image_generation` 必须有图片内容
- `code_interpreter.code` 非空

错误应返回工作台 command error，不应向外部客户端生成伪事件。

## 9. Tests

后端单测：

- `protocolruntime`:
  - web search event order
  - image generation event order and final `item.result`
  - code interpreter event order and final outputs
  - non-Responses protocol rejects or emits no built-in events

- `protocol/debugview`:
  - Responses request with built-in tools projects `builtin_tools`
  - function tools and built-in tools can coexist

- `turn/control`:
  - disabled built-in tool cannot be submitted
  - enabled built-in tool writes stream events and timeline item

前端测试：

- request without built-in tools hides tab
- request with `web_search` shows tab and only search option
- request with `image_generation` shows image form
- request with `code_interpreter` shows code/log form

集成测试：

- RikkaHub-compatible image generation:
  - request includes `tools:[{type:"image_generation"}]`
  - submit image from workspace
  - SSE contains `response.output_item.done.item.type=image_generation_call`
  - `item.result` is non-empty base64

- Web search:
  - request includes `tools:[{type:"web_search"}]`
  - submit query from workspace
  - SSE contains `response.web_search_call.searching` and `completed`
  - workspace timeline shows query

- Code interpreter:
  - request includes `tools:[{type:"code_interpreter",container:{type:"auto"}}]`
  - submit code/logs
  - SSE contains code delta/done and final output item

## 10. Open Questions

- Whether to include non-standard `query` in `web_search_call` output item metadata. Default should be no for protocol output; keep query in ChatAPI timeline metadata.
- Whether `image_generation_call.partial_image` should be emitted when the user uploads a preview before final submit. Defer until a concrete client requires it.
- Whether code interpreter image outputs should accept local media IDs and expose them as signed URLs.首版只接受 URL string.
