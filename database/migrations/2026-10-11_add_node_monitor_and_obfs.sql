SET @has_obfs_enabled := (SELECT COUNT(*) FROM information_schema.COLUMNS
 WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='server_nodes' AND COLUMN_NAME='obfs_enabled');
SET @sql := IF(@has_obfs_enabled=0,
 'ALTER TABLE server_nodes ADD COLUMN obfs_enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER obfs_password','SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS node_system_metrics (
 id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
 node_id BIGINT UNSIGNED NOT NULL,
 sampled_at_ms BIGINT NOT NULL,
 cpu_percent DOUBLE NULL,
 memory_percent DOUBLE NOT NULL,
 swap_percent DOUBLE NOT NULL,
 disk_percent DOUBLE NOT NULL,
 load1 DOUBLE NOT NULL,
 rx_rate DOUBLE NULL,
 tx_rate DOUBLE NULL,
 payload_json JSON NOT NULL,
 UNIQUE KEY uniq_node_sample (node_id,sampled_at_ms),
 INDEX idx_monitor_retention (sampled_at_ms),
 CONSTRAINT fk_node_system_metrics_node FOREIGN KEY (node_id) REFERENCES server_nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
