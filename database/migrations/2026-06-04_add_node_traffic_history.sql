-- ============================================================
-- 数据库迁移脚本: 添加节点流量历史表
-- 创建日期: 2026-06-04
-- 说明: 用于记录各节点的流量历史数据，支持总览页面的流量趋势图功能
-- ============================================================

-- 检查表是否已存在，避免重复创建
CREATE TABLE IF NOT EXISTS node_traffic_history (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    node_id BIGINT UNSIGNED NOT NULL,
    total_rx BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '总下行流量(字节)',
    total_tx BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '总上行流量(字节)',
    user_count INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '用户数量',
    online_count INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '在线客户端数量',
    recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '记录时间',
    INDEX idx_node_recorded (node_id, recorded_at),
    CONSTRAINT fk_node_traffic_history_node FOREIGN KEY (node_id) REFERENCES server_nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
