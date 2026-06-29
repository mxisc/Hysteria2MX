-- ============================================================
-- 数据库迁移脚本: Agent 运维架构
-- 创建日期: 2026-06-05
-- 说明: 为节点增加 Agent 管理字段，并新增 node_agents / agent_tasks 表
-- 特性: 通过 information_schema + 动态 SQL 保持重复执行安全
-- ============================================================

SET @schema_name := DATABASE();

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'manage_mode'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN manage_mode ENUM(''agent'', ''ssh'') NOT NULL DEFAULT ''agent'' AFTER bandwidth_down_mbps'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'agent_enabled'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN agent_enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER manage_mode'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'agent_report_interval_seconds'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN agent_report_interval_seconds INT NOT NULL DEFAULT 2 AFTER agent_enabled'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'agent_task_poll_interval_seconds'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN agent_task_poll_interval_seconds INT NOT NULL DEFAULT 1 AFTER agent_report_interval_seconds'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'agent_install_path'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN agent_install_path VARCHAR(255) NOT NULL DEFAULT ''/usr/local/bin/mxinhy-agent'' AFTER agent_task_poll_interval_seconds'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'agent_config_path'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN agent_config_path VARCHAR(255) NOT NULL DEFAULT ''/etc/mxinhy-agent.json'' AFTER agent_install_path'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
    EXISTS (
        SELECT 1
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = @schema_name
          AND TABLE_NAME = 'server_nodes'
          AND COLUMN_NAME = 'agent_service_name'
    ),
    'SELECT 1',
    'ALTER TABLE server_nodes ADD COLUMN agent_service_name VARCHAR(128) NOT NULL DEFAULT ''mxinhy-agent'' AFTER agent_config_path'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS node_agents (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    node_id BIGINT UNSIGNED NOT NULL,
    agent_id VARCHAR(64) NOT NULL UNIQUE,
    shared_secret TEXT NOT NULL,
    status ENUM('pending', 'online', 'offline', 'error') NOT NULL DEFAULT 'pending',
    version VARCHAR(32) NOT NULL DEFAULT '',
    capabilities_json JSON NULL,
    report_interval_seconds INT NOT NULL DEFAULT 2,
    task_poll_interval_seconds INT NOT NULL DEFAULT 1,
    installed_at DATETIME NULL,
    last_seen_at DATETIME NULL,
    last_ip VARCHAR(64) NULL,
    last_error TEXT NULL,
    last_service_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    last_service_message TEXT NULL,
    last_total_rx BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_total_tx BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_user_count INT UNSIGNED NOT NULL DEFAULT 0,
    last_online_count INT UNSIGNED NOT NULL DEFAULT 0,
    last_payload_json JSON NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_node_agents_node (node_id),
    INDEX idx_node_agents_status (status, last_seen_at),
    CONSTRAINT fk_node_agents_node FOREIGN KEY (node_id) REFERENCES server_nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_tasks (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    node_id BIGINT UNSIGNED NOT NULL,
    agent_id VARCHAR(64) NOT NULL,
    opcode INT NOT NULL,
    payload_json JSON NULL,
    status ENUM('pending', 'running', 'done', 'error') NOT NULL DEFAULT 'pending',
    created_by BIGINT UNSIGNED NULL,
    exit_code INT NULL,
    output_text LONGTEXT NULL,
    result_json JSON NULL,
    error_message TEXT NULL,
    expires_at DATETIME NULL,
    started_at DATETIME NULL,
    finished_at DATETIME NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_agent_tasks_dispatch (node_id, agent_id, status, id),
    INDEX idx_agent_tasks_created (created_at),
    INDEX idx_agent_tasks_expires (expires_at),
    CONSTRAINT fk_agent_tasks_node FOREIGN KEY (node_id) REFERENCES server_nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
