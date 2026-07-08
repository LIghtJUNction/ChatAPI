# Turn Lifecycle Mutation Design

## 目的

这轮重构解决三类问题：

1. `turn.Service` 同时承担 turn 控制编排、timeline event 组装、event 反查补发，职责混杂。
2. repository 的 `AbortPendingInput` / `DisconnectPendingInput` 通过可选 `Event` 字段被具体用例拉形。
3. request identity 在 `PendingTurn`、conversation metadata、message metadata 之间分散推导，没有稳定权威来源。

这份文档记录当前收口后的语义，作为后续扩展 `expire / retry / tool handoff / system notice` 的基线。

## 新的核心概念

### 1. TurnIdentity

位置：

- `internal/repository/common/chat.go`

定义：

- `OwnerID`
- `RequestID`

语义：

- 描述“一次 turn lifecycle mutation 归属于谁、对应哪次请求”。
- 不再让 `turn.Service` 在多个 helper 里分别猜 owner/request。

权威来源：

- 在线热路径：`PendingTurn`
- 冷路径/恢复路径：repository `GetLatestRequestForConversation(...)`

### 2. PendingTurnMutationResult

位置：

- `internal/repository/common/chat.go`

定义：

- `Conversation`
- `Message`
- `Event`

语义：

- 表示一次 pending turn 状态迁移的完整结果。
- repository 不再只返回“状态更新后的 conversation”，而是返回这次 mutation 产生的领域事实。

目前用于：

- `AbortPendingTurnWithEvent`
- `DisconnectPendingTurnWithEvent`

后续可扩展到：

- `ExpirePendingTurnWithEvent`
- `RetryPendingTurnWithEvent`
- `ToolHandoffWithEvent`

### 3. PendingTurnLifecycleMutationInput

位置：

- `internal/repository/common/inputs.go`

定义：

- `ConversationID`
- `Reason`
- `Identity`
- `EventID`
- `EventType`
- `EventLevel`
- `EventTitle`
- `EventDetail`
- `EventMetadata`
- `EventCreatedAt`

语义：

- 明确表达“这是一个 lifecycle mutation，并且它会产生一个 event artifact”。
- 不再用 `AbortPendingInput{Event:*AppendConversationEventInput}` 这种拼接式接口。

设计取舍：

- 仍然保留事件字段，是因为 abort/disconnect 这类 mutation 本身就是“状态迁移 + 生命周期事件”一体的领域动作。
- 但事件字段现在属于明确的 lifecycle mutation 输入，而不是把通用 `AppendConversationEventInput` 直接塞到原接口里。

## Repository 边界

### 旧接口问题

旧接口：

- `AbortPendingTurn(ctx, AbortPendingInput) (Conversation, Message, error)`
- `DisconnectPendingTurn(ctx, DisconnectPendingInput) (Conversation, Message, error)`

问题：

1. 返回值不包含刚插入的 event。
2. service 需要重新 `ListConversationEvents`，再取最后一条猜测刚插入的 event。
3. transaction 内的 mutation artifact 没有被建模，service 被迫知道持久化细节。

### 新接口

新增：

- `AbortPendingTurnWithEvent(ctx, PendingTurnLifecycleMutationInput) (PendingTurnMutationResult, error)`
- `DisconnectPendingTurnWithEvent(ctx, PendingTurnLifecycleMutationInput) (PendingTurnMutationResult, error)`
- `GetLatestRequestForConversation(ctx, conversationID) (Request, error)`

保留：

- `AbortPendingTurn`
- `DisconnectPendingTurn`

说明：

- 旧接口现在只是薄转发，便于平滑迁移现有调用点。
- 新逻辑全部应优先走 `WithEvent` 版本。
- 后续可以在其他调用点迁移完成后删掉旧接口。

### Repository 现在承担什么

repository 负责：

1. 在单个事务里更新 conversation 状态
2. 在同一个事务里插入 conversation event
3. 返回刚刚持久化的 event artifact

repository 不负责：

1. realtime publish
2. pending registry 的内存状态流转
3. HTTP / WS 响应语义

## turn.Service 边界

### 重构前

`turn.Service` 自己负责：

1. 组装 event input
2. 调 store 做状态更新
3. 再查一遍 event 列表
4. 取最后一条 event
5. publish realtime timeline

这会导致 service 同时像：

- turn orchestrator
- timeline event assembler
- mutation artifact recovery layer

### 重构后

`turn.Service` 现在负责：

1. 从 pending / repo 解析 `TurnIdentity`
2. 调用 lifecycle mutation repository
3. 根据返回的 `PendingTurnMutationResult`：
   - publish conversation summary
   - publish timeline event
   - 更新 pending registry

`turn.Service` 不再负责：

1. `ListConversationEvents` 再反查最新 event
2. 从 message metadata 逆序扫描 request id
3. 在多个 helper 里分别猜 owner/request

## request identity 语义

### 在线热路径

来源：

- `PendingTurn`

原因：

- 对于尚未结束的请求，这是当前最权威的运行态 source。
- 可以直接得到 `OwnerID + RequestID`。

### 冷路径

来源：

- repository `GetLatestRequestForConversation(...)`

原因：

- 服务重启后 in-memory pending registry 消失，只能从持久化视图恢复。
- 权威恢复入口应由 repository 暴露，而不是让 service 直接扫 message metadata。

### 不再推荐的来源

不再把这些作为 service 内部直接推导的主来源：

- `Conversation.Metadata`
- `Message.Metadata.request_debug`

它们仍然可能是底层存储载体的一部分，但不再应该由 `turn.Service` 直接扫描和拼装语义。

## 当前保留的兼容部分

这轮没有继续处理的点：

1. `AbortPendingTurn` / `DisconnectPendingTurn` 旧接口仍保留
2. logging factory 里 HTTP structured log 和 console summary 仍未拆开

原因：

- 第一项是为了让迁移成本受控，先把新路径立起来。
- 第二项属于 observability 分层问题，不影响 turn/timeline 正确性。

## 后续建议

### 1. 删除旧 lifecycle 接口

当所有调用点迁移到：

- `AbortPendingTurnWithEvent`
- `DisconnectPendingTurnWithEvent`

后，可以删掉：

- `AbortPendingTurn`
- `DisconnectPendingTurn`
- 旧的 `AbortPendingInput`
- 旧的 `DisconnectPendingInput`

### 2. 把更多 lifecycle 动作统一进 mutation 模型

建议后续新增时直接沿用：

- `PendingTurnLifecycleMutationInput`
- `PendingTurnMutationResult`

适合纳入的动作：

- expire
- retry
- tool handoff
- system notice

### 3. timeline 事件进一步前移成统一领域事件

目前 repo 已经能返回 mutation 产生的 `ConversationEvent`。

后续如果 timeline 成为更核心的统一读模型，可以继续把：

- 消息
- 系统事件
- pending state transition

进一步收敛成同一类 domain mutation/result，再由 workspace/ws 层消费。

## 结论

这轮之后，turn lifecycle 的边界变成：

- repository：事务内生成 mutation artifact
- turn service：消费 mutation artifact，编排 pending/realtime
- request identity：热路径走 pending，冷路径走 repo

这样后续再加 lifecycle 类型时，不需要再重复：

- 在 service 里拼 event
- 在 service 里重查 event
- 在 service 里自己猜 request identity

这就是这轮重构真正固定下来的语义。
