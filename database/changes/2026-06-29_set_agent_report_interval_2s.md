# 变更说明：将 Agent 默认心跳间隔调整为 2 秒

## 目的

用户管理实时速率改为 2 秒刷新后，让 Agent 默认每 2 秒上报一次缓存流量快照，减少页面切换后的等待时间。

## 范围

- 影响表：`server_nodes`、`node_agents`
- 影响列：
  - `server_nodes.agent_report_interval_seconds`
  - `node_agents.report_interval_seconds`

## 执行顺序

1. 修改两列默认值为 `2`
2. 将仍为旧默认值 `5` 的已有记录更新为 `2`
3. 保留手动配置为其他间隔的记录

## 回滚计划

如需回滚：

```sql
ALTER TABLE server_nodes MODIFY COLUMN agent_report_interval_seconds INT NOT NULL DEFAULT 5;
UPDATE server_nodes SET agent_report_interval_seconds = 5 WHERE agent_report_interval_seconds = 2;
ALTER TABLE node_agents MODIFY COLUMN report_interval_seconds INT NOT NULL DEFAULT 5;
UPDATE node_agents SET report_interval_seconds = 5 WHERE report_interval_seconds = 2;
```

## 验证方法

- 确认新建节点默认 `agent_report_interval_seconds = 2`
- 确认已有默认配置节点已更新为 `2`
- 升级或重新下发 Agent 配置后，确认 Agent 心跳约 2 秒一次

## 风险

节点数量很大且每节点用户很多时，2 秒心跳会增加面板写入频率和心跳 JSON 大小。必要时可在节点配置中手动调高单节点上报间隔。
