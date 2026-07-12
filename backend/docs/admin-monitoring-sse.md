# 管理员实时监控 SSE

## 定位

管理员设置采用普通 HTTP GET/PATCH，不提供实时同步。只有连接状态和运行指标属于持续变化的监控数据，
通过独立的单向 SSE 流发送。该流只在管理员打开用户管理界面时建立，离开界面后立即关闭。

## 边界

- `service/chat/workspace`
  - 拥有当前进程实例的工作台连接注册表。
  - 暴露只读 presence 快照和连接数变化订阅。
  - 不知道管理员 UI、SSE 或系统指标。
- `service/admincontrol/monitoring`
  - 聚合 presence 和进程运行指标。
  - 生成稳定的管理员监控事件。
  - 不查询或修改管理员设置。
- `http/handler`
  - 校验管理员身份由现有 middleware 完成。
  - 将 monitoring 事件编码为 `text/event-stream`。
- 前端用户管理
  - 用户列表通过 `GET /api/admin/users?page=1&page_size=10` 做服务端分页。
  - SSE 只增量更新连接数字段和顶部运行指标，不重传用户列表。

## 接口与事件

`GET /api/admin/monitor/stream?user_ids=user_1,user_2`

每个 SSE 连接最多订阅当前页的 100 个用户 ID。翻页时前端先关闭旧连接，分页请求成功后再使用新页 ID
建立 SSE。订阅只存在于当前进程内存中，不按管理员账号持久化。

- `monitor.snapshot`
  - 建立连接后发送一次。
  - 包含总连接数、各用户连接数和当前运行指标。
- `user.connection.updated`
  - 某用户连接建立或断开时立即发送。
  - 包含 `user_id`、`connection_count`、`total_connections`。
  - 携带单调 `sequence`；并发连接变化即使发布乱序，客户端也会拒绝旧序号。
- `system.metrics.updated`
  - 每两秒采样一次。
  - 包含 uptime、goroutine、CPU 核数和 Go 进程内存指标。
  - 同时携带当前在线用户连接数快照，用于修正极端背压下被丢弃的增量事件。

当前数据全部属于单进程实例，不冒充跨实例全局统计。若未来需要部署级统计，应在 monitoring 下接入共享
presence/metrics backend，而不是改变这些事件的实例语义。

`total_connections` 始终表示当前进程实例的全部 workspace WebSocket 连接，不受 `user_ids` 过滤；
只有 `user_connections` 和 `user.connection.updated` 按当前页过滤。

presence 增量是低延迟提示，允许在订阅者背压时丢弃；每两秒发送的完整连接数快照才是权威状态。因此
监控界面的最坏收敛窗口约为两秒，不要求可靠事件总线语义。
