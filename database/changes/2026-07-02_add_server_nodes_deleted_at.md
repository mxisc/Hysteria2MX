# 增加节点软删除时间

## 目的

支持节点离线或关机后从面板移除，并在节点重新上线时让 Agent 识别已删除状态并执行自卸载。

## 范围

- 表：`server_nodes`
- 新增列：`deleted_at DATETIME NULL`
- 新增索引：`idx_server_nodes_deleted_at (deleted_at)`

## 执行顺序

1. 执行 `database/migrations/2026-07-02_add_server_nodes_deleted_at.sql`
2. 部署新版面板与 Agent
3. 验证节点列表、订阅、流量统计不返回 `deleted_at IS NOT NULL` 的节点

## 回滚

如需回滚，可在确认没有软删除节点需要自卸载后执行：

```sql
ALTER TABLE server_nodes DROP INDEX idx_server_nodes_deleted_at;
ALTER TABLE server_nodes DROP COLUMN deleted_at;
```

## 验证

- 删除离线节点后，`server_nodes.deleted_at` 应写入时间
- 对应节点用户副本应从 `hysteria_users` 移除
- 旧 Agent 再次访问面板时应收到已删除状态并停止本地服务
- 面板返回已删除状态后应立即轮换该 Agent 的 `shared_secret`，后续旧凭据只能得到未授权响应

## 风险

软删除节点记录会暂时保留 Agent 鉴权记录，用于首次回连时识别并下发自卸载状态；返回自卸载状态后会废弃旧凭据。业务查询必须过滤 `deleted_at IS NULL`，避免订阅或统计继续暴露已删除节点。
