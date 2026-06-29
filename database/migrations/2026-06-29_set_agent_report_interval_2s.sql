-- ============================================================
-- 数据库迁移脚本: 将 Agent 默认心跳间隔调整为 2 秒
-- 创建日期: 2026-06-29
-- 说明: 用户管理实时速率改为 2 秒刷新后，Agent 心跳默认也调整为 2 秒
-- ============================================================

ALTER TABLE server_nodes
    MODIFY COLUMN agent_report_interval_seconds INT NOT NULL DEFAULT 2;

UPDATE server_nodes
SET agent_report_interval_seconds = 2
WHERE agent_report_interval_seconds = 5;

ALTER TABLE node_agents
    MODIFY COLUMN report_interval_seconds INT NOT NULL DEFAULT 2;

UPDATE node_agents
SET report_interval_seconds = 2
WHERE report_interval_seconds = 5;
