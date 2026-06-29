# 变更说明：为 `node_traffic_history` 增加节点流量增量字段

## 目的

- 将节点近 30 天流量统计改为基于每次同步的增量持久化计算。
- 避免节点服务重启后累计计数归零，导致已落库的 30 天节点流量被清空。

## 范围

- 影响表：`node_traffic_history`
- 新增字段：
  - `delta_rx BIGINT UNSIGNED NOT NULL DEFAULT 0`
  - `delta_tx BIGINT UNSIGNED NOT NULL DEFAULT 0`
- 影响代码：
  - 节点流量快照写入逻辑
  - 流量统计页的节点近 30 天汇总查询

## 执行顺序

1. 先执行 `database/migrations/2026-06-29_add_node_traffic_deltas.sql`
2. 再部署包含 Go 后端与前端改动的新版本
3. 后端后续每次同步节点流量时，会把本次新增流量写入 `delta_rx` / `delta_tx`

## 可重复执行性

- 迁移脚本通过 `information_schema.COLUMNS` 检查列是否已存在
- 仅在列不存在时执行 `ALTER TABLE`
- 同一脚本重复执行不会重复加列，也不会破坏已有数据

## 回滚方案

1. 回滚应用版本到不依赖 `delta_rx` / `delta_tx` 的旧版本
2. 如需回滚结构，可在确认不再使用新统计逻辑后执行：
   - `ALTER TABLE node_traffic_history DROP COLUMN delta_tx;`
   - `ALTER TABLE node_traffic_history DROP COLUMN delta_rx;`

## 验证方式

- 执行迁移后确认 `node_traffic_history` 存在 `delta_rx`、`delta_tx`
- 连续同步两次同一节点流量，确认第二次写入的 `delta_rx`、`delta_tx` 为本次新增值
- 重启节点服务后再次同步，确认节点近 30 天流量不会因累计计数归零而把已落库历史抹掉

## 风险

- 本次迁移只为后续同步写入增量值，历史旧快照不会自动精确回填增量
- 因此迁移前已经落库的旧节点历史，在新逻辑上线后的首个统计窗口内可能不完整
- 若节点在两次同步之间发生重启，重启前但尚未同步的那段流量仍无法回补，这是采样式统计的天然边界
