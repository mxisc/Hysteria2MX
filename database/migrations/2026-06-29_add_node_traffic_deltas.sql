-- ============================================================
-- 数据库迁移脚本: 为节点流量历史增加增量字段
-- 创建日期: 2026-06-29
-- 说明: 持久化每次同步的新增流量，供近 30 天节点流量统计使用，
--       避免节点服务重启后累计计数归零导致历史统计被清空。
-- ============================================================

SET @delta_rx_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'node_traffic_history'
      AND COLUMN_NAME = 'delta_rx'
);
SET @add_delta_rx_sql := IF(
    @delta_rx_exists = 0,
    'ALTER TABLE node_traffic_history ADD COLUMN delta_rx BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT ''本次同步新增下行流量(字节)'' AFTER total_tx',
    'SELECT 1'
);
PREPARE add_delta_rx_stmt FROM @add_delta_rx_sql;
EXECUTE add_delta_rx_stmt;
DEALLOCATE PREPARE add_delta_rx_stmt;

SET @delta_tx_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'node_traffic_history'
      AND COLUMN_NAME = 'delta_tx'
);
SET @add_delta_tx_sql := IF(
    @delta_tx_exists = 0,
    'ALTER TABLE node_traffic_history ADD COLUMN delta_tx BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT ''本次同步新增上行流量(字节)'' AFTER delta_rx',
    'SELECT 1'
);
PREPARE add_delta_tx_stmt FROM @add_delta_tx_sql;
EXECUTE add_delta_tx_stmt;
DEALLOCATE PREPARE add_delta_tx_stmt;
