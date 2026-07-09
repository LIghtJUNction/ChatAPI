# 协议流式事件参考与实测记录

本文整理：

- OpenAI Responses 事件全集的本地 SDK 参考
- 当前上游端点对 `responses` / `chat.completions` / `messages` 三套协议的实测结果
- 对 `chatapi-go-refactor` 后续兼容实现的优先级建议

相关探针与结果文件：

- 探针脚本：[responses_sse_probe.py](/home/zyf/Code/Projects/chatapi-go-refactor/tests/responses_sse_probe.py)
- 三协议探针：[protocol_stream_probe.py](/home/zyf/Code/Projects/chatapi-go-refactor/tests/protocol_stream_probe.py)
- 三协议深度探针：[protocol_deep_probe.py](/home/zyf/Code/Projects/chatapi-go-refactor/tests/protocol_deep_probe.py)
- 最近一次三协议结果：[.protocol-stream-probe-results.json](/home/zyf/Code/Projects/chatapi-go-refactor/tests/.protocol-stream-probe-results.json)

## 0. 2026-07-09 深度探针结论

本轮使用 `backend/docs/1.txt` 中的真实端点，通过本地反代同时记录上游原始 SSE 和官方 Python SDK 解析结果。

本地日志目录：

- `tests/.sse-probe-logs/deep_responses_forced_tool.*`
- `tests/.sse-probe-logs/deep_responses_reasoning.*`
- `tests/.sse-probe-logs/deep_chat_forced_tool.*`
- `tests/.sse-probe-logs/deep_messages_forced_tool.*`
- `tests/.sse-probe-logs/deep_messages_thinking.*`

这些日志是本地探针输出，不应提交到版本库。

### Abort / disconnect 策略

当前后端按以下规则实现：

- Responses：请求失败、abort、disconnect 统一输出 `response.failed`。
- Chat Completions：没有稳定官方 abort SSE 事件；abort/disconnect 时直接结束连接，不自造错误 chunk。
- Anthropic Messages：只使用官方 `event: error`，payload 保持 `{"type":"error","error":{...}}` 形状。

`../new-api` 没有提供“客户端 abort 的官方生成格式”。可参考的部分是它消费 `response.failed`、以及 Claude stream 的标准 block 状态机；不能把它当作 abort 输出格式的权威来源。

### 本轮已实测到的关键事件

Responses forced tool：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added`，`item.type=function_call`
4. 多次 `response.function_call_arguments.delta`
5. `response.function_call_arguments.done`
6. `response.output_item.done`，`item.type=function_call`
7. `response.completed`

Responses reasoning：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added`，`item.type=reasoning`
4. `response.output_item.done`，`item.type=reasoning`
5. message item 的 text part 生命周期
6. `response.completed`

该端点本轮没有暴露 `response.reasoning_summary_text.delta` 或 `response.reasoning_text.delta`。所以后端可以支持这些事件形状，但不能假设所有 reasoning 请求都会有 delta。

Chat Completions forced tool：

1. 首块 `choices[].delta.role=assistant`
2. 多块 `choices[].delta.tool_calls[]`
3. 尾块 `finish_reason=tool_calls`
4. `[DONE]`

Anthropic Messages forced tool：

1. `message_start`
2. `ping`
3. `content_block_start`，`content_block.type=tool_use`
4. 多次 `content_block_delta`，`delta.type=input_json_delta`
5. `content_block_stop`
6. `message_delta`，`stop_reason=tool_use`
7. `message_stop`

Anthropic Messages thinking：

1. `message_start`
2. `ping`
3. `content_block_start`，`content_block.type=thinking`
4. 多次 `content_block_delta`，`delta.type=thinking_delta`
5. `content_block_delta`，`delta.type=signature_delta`
6. `content_block_stop`
7. `content_block_start`，`content_block.type=text`
8. 多次 `content_block_delta`，`delta.type=text_delta`
9. `content_block_stop`
10. `message_delta`
11. `message_stop`

本地 ChatAPI runtime 不生成这组官方 thinking 事件。`signature_delta` 是上游生成的签名，Anthropic SDK 的最终 `thinking` block 也要求 `signature` 字段；因此本地人工完成时不能安全伪造 `thinking_delta/signature_delta`。如果未来做上游 Anthropic 透传，应原样转发这组事件。

## 1. Responses 除了现在常见这些，还有哪些事件

根据本地 `openai-go v1.12.0` 的事件常量定义：

- 参考文件：[constants.go](/home/zyf/go/pkg/mod/github.com/openai/openai-go@v1.12.0/shared/constant/constants.go:109)
- 以及 Responses stream union：[response.go](/home/zyf/go/pkg/mod/github.com/openai/openai-go@v1.12.0/responses/response.go:10007)

当前 SDK 中可见的 Responses 相关事件族包括：

基础生命周期：

- `response.queued`
- `response.created`
- `response.in_progress`
- `response.completed`
- `response.failed`
- `response.incomplete`
- `response.cancelled`

文本输出：

- `response.output_item.added`
- `response.output_item.done`
- `response.content_part.added`
- `response.content_part.done`
- `response.output_text.delta`
- `response.output_text.done`
- `response.output_text.annotation.added`

拒绝输出：

- `response.refusal.delta`
- `response.refusal.done`

reasoning 相关：

- `response.reasoning_summary_part.added`
- `response.reasoning_summary_part.done`
- `response.reasoning_summary_text.delta`
- `response.reasoning_summary_text.done`
- `response.reasoning_summary.delta`
- `response.reasoning_summary.done`

tool / function call：

- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`

audio：

- `response.audio.delta`
- `response.audio.done`
- `response.audio.transcript.delta`
- `response.audio.transcript.done`

web search：

- `response.web_search_call.in_progress`
- `response.web_search_call.searching`
- `response.web_search_call.completed`

file search：

- `response.file_search_call.in_progress`
- `response.file_search_call.searching`
- `response.file_search_call.completed`

code interpreter：

- `response.code_interpreter_call.in_progress`
- `response.code_interpreter_call.interpreting`
- `response.code_interpreter_call.completed`
- `response.code_interpreter_call_code.delta`
- `response.code_interpreter_call_code.done`

image generation：

- `response.image_generation_call.in_progress`
- `response.image_generation_call.generating`
- `response.image_generation_call.partial_image`
- `response.image_generation_call.completed`

MCP：

- `response.mcp_call_arguments.delta`
- `response.mcp_call_arguments.done`
- `response.mcp_call.in_progress`
- `response.mcp_call.completed`
- `response.mcp_call.failed`
- `response.mcp_list_tools.in_progress`
- `response.mcp_list_tools.completed`
- `response.mcp_list_tools.failed`

结论：

- 你现在文档里列的 `created / in_progress / output_item / content_part / output_text / completed / failed` 只是核心子集
- 真要做“协议调试工具级完整实现”，至少要承认 Responses 事件面比目前实现宽很多
- 但第一阶段不需要一次性把 `audio / image_generation / mcp / code_interpreter / web_search / file_search` 全补上
- 当前产品判断是这些内置工具事件不属于本地人工调试主链路；没有对应执行系统时不应生成伪事件，只保留/展示请求字段

## 2. Responses 实测

使用端点：

- `https://api.hanhegufei.online/v1/responses`
- 通过官方 `openai` Python SDK 调用

实测到的基础文本事件序列：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added`
4. `response.content_part.added`
5. 多次 `response.output_text.delta`
6. `response.output_text.done`
7. `response.content_part.done`
8. `response.output_item.done`
9. `response.completed`

这是标准的 item/part 生命周期，不是简化流。

tool 场景额外实测到：

- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`

reasoning 场景样本里出现了：

- `response.output_item.added` `item.type=reasoning`
- `response.output_item.done` `item.type=reasoning`

但当前样本里没有实测到：

- `response.reasoning_summary_text.delta`
- `response.reasoning_summary_text.done`
- `response.refusal.*`
- `response.incomplete`

这不代表不存在，只代表这个上游在当前样本下没有走到。

## 3. Chat Completions 实测

使用端点：

- `https://api.hanhegufei.online/v1/chat/completions`
- 通过官方 `openai` Python SDK 调用

基础文本结果摘要：

- 文本通过 `choices[].delta.content` 连续返回
- 第一块包含 `delta.role = assistant`
- 最后一块给出 `finish_reason`

2026-07-09 forced tool 样本额外确认：

- 工具调用通过 `choices[].delta.tool_calls[]` 分块返回
- arguments 是字符串分片，需要按 `tool_calls[].index` / `id` 聚合
- 尾块 `finish_reason = tool_calls`
- 标准结束仍是 `[DONE]`

从这次样本看，Chat Completions 是典型 OpenAI chunk 风格：

1. 首块 role
2. 多块 content delta
3. 尾块 finish_reason

对 `chatapi-go-refactor` 的启示：

- Chat Completions 不需要模拟 Responses 那种 item/part 事件树
- 但如果内部统一状态机更细，可以在出口再降级编码为 chunk

## 4. Anthropic Messages 实测

使用端点：

- `https://api.hanhegufei.online/v1/messages`
- 通过官方 `anthropic` Python SDK 调用

实测事件类型：

- `message_start`
- `content_block_start`
- 多次 `content_block_delta`
- `content_block_stop`
- `message_delta`
- `message_stop`

这是标准 Anthropic Messages 流式骨架。

基础文本样本中：

- `content_block_delta.delta.type = text_delta`
- 末尾 `message_delta` 负责携带 stop reason / usage

2026-07-09 forced tool / thinking 样本额外确认：

- tool use block 使用 `content_block_start.content_block.type=tool_use`
- 工具参数通过 `content_block_delta.delta.type=input_json_delta` 分片返回
- thinking block 使用 `content_block_start.content_block.type=thinking`
- thinking 内容通过 `content_block_delta.delta.type=thinking_delta` 分片返回
- thinking block 结束前可能有 `signature_delta`
- thinking block 结束后再开启 text block，index 会递增

## 5. 另一条 Responses 端点实测

单独还测了：

- `https://gaccode.com/codex/responses`

基础文本流也返回了标准 Responses 核心序列：

1. `response.created`
2. `response.in_progress`
3. `response.output_item.added`
4. `response.content_part.added`
5. `response.output_text.delta`
6. `response.output_text.done`
7. `response.content_part.done`
8. `response.output_item.done`
9. `response.completed`

这说明：

- 至少有不止一个上游实现采用了完整 item/part 生命周期
- 所以后续 Go 重构版不应该继续停留在“最小文本 delta 模拟”

## 6. 参数回显与事件完整性是两回事

这次实测反复说明了一件事：

- 某个端点的 SSE 事件可以很标准
- 但它对请求参数的实现仍然可能不完整

例如在 Responses 样本里：

- `store=false` 常能回显
- `parallel_tool_calls=false` 常能回显
- `reasoning.effort`、`text.verbosity` 有时能回显

但下面这些经常没有按请求值回显：

- `temperature`
- `top_p`
- `max_output_tokens`
- `user`
- `metadata`

所以对协议调试工具来说，要分开看两件事：

1. 流式事件形状是否标准
2. 请求字段语义是否真的被实现

## 7. 对 `chatapi-go-refactor` 的优先级建议

### 第一优先级：补齐 Responses 核心生命周期

至少补齐：

- `response.in_progress`
- 普通文本的 `response.output_item.added`
- 普通文本的 `response.content_part.added`
- `response.output_text.done`
- `response.content_part.done`
- `response.output_item.done`

这些已经被多个上游样本证明是“常见现实行为”，不是纸面事件。

### 第二优先级：补齐 function call 流

至少补：

- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`

因为这在真实上游里确实出现。

### 第三优先级：reasoning item 语义

当前重构版已经有一部分 reasoning 事件，但建议进一步统一：

- `reasoning` output item 的 added/done
- `reasoning summary` 事件族
- 与 `reasoning_text` 的模式切换

### 第四优先级：承认更大的事件面

文档、前端、后端都应先承认这些事件族是 Responses 的一部分：

- `refusal.*`
- `incomplete`
- `queued`
- `audio.*`
- `web_search_call.*`
- `file_search_call.*`
- `code_interpreter_*`
- `image_generation_call.*`
- `mcp_*`

即使当前不实现，也应该在支持矩阵里标 `unsupported`，而不是假装不存在。

## 8. 建议的文档结论

如果后续要对外说“Responses 兼容”，更准确的说法应该分层：

- `基础文本流兼容`
- `tool call 流兼容`
- `reasoning 流部分兼容`
- `advanced tool/audio/mcp/image/code-interpreter events 未实现`

这样比单纯说“支持 Responses”更诚实，也更方便前端调试面板做状态标签。
