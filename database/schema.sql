CREATE TABLE IF NOT EXISTS admin_users (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    display_name VARCHAR(128) NOT NULL,
    role ENUM('super_admin', 'operator', 'auditor', 'viewer') NOT NULL DEFAULT 'super_admin',
    status ENUM('active', 'disabled') NOT NULL DEFAULT 'active',
    last_login_at DATETIME NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS server_nodes (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    current_node TINYINT(1) NOT NULL DEFAULT 0,
    name VARCHAR(128) NOT NULL DEFAULT 'default-node',
    host VARCHAR(255) NOT NULL,
    ssh_port INT NOT NULL DEFAULT 22,
    ssh_username VARCHAR(128) NOT NULL,
    ssh_auth_type ENUM('password', 'key') NOT NULL DEFAULT 'password',
    ssh_password TEXT NULL,
    ssh_private_key_path TEXT NULL,
    sudo_password TEXT NULL,
    install_script VARCHAR(255) NOT NULL DEFAULT 'bash <(curl -fsSL https://get.hy2.sh/)',
    service_name VARCHAR(128) NOT NULL DEFAULT 'hysteria-server',
    config_path VARCHAR(255) NOT NULL DEFAULT '/etc/hysteria/config.yaml',
    listen_port INT NOT NULL DEFAULT 443,
    traffic_stats_listen VARCHAR(64) NOT NULL DEFAULT '127.0.0.1:9999',
    traffic_stats_secret VARCHAR(255) NOT NULL DEFAULT '',
    tls_mode ENUM('acme', 'self_signed') NOT NULL DEFAULT 'acme',
    tls_cert_path VARCHAR(255) NOT NULL DEFAULT '/etc/hysteria/server.crt',
    tls_key_path VARCHAR(255) NOT NULL DEFAULT '/etc/hysteria/server.key',
    domain VARCHAR(255) NOT NULL,
    acme_email VARCHAR(255) NOT NULL,
    obfs_password VARCHAR(255) NOT NULL,
    masquerade_url VARCHAR(255) NOT NULL DEFAULT 'https://www.cloudflare.com',
    bandwidth_up_mbps INT NOT NULL DEFAULT 200,
    bandwidth_down_mbps INT NOT NULL DEFAULT 200,
    manage_mode ENUM('agent', 'ssh') NOT NULL DEFAULT 'agent',
    agent_enabled TINYINT(1) NOT NULL DEFAULT 1,
    agent_report_interval_seconds INT NOT NULL DEFAULT 2,
    agent_task_poll_interval_seconds INT NOT NULL DEFAULT 1,
    agent_install_path VARCHAR(255) NOT NULL DEFAULT '/usr/local/bin/mxinhy-agent',
    agent_config_path VARCHAR(255) NOT NULL DEFAULT '/etc/mxinhy-agent.json',
    agent_service_name VARCHAR(128) NOT NULL DEFAULT 'mxinhy-agent',
    deleted_at DATETIME NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_server_nodes_deleted_at (deleted_at)
);

CREATE TABLE IF NOT EXISTS hysteria_users (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    public_id VARCHAR(64) NOT NULL,
    node_id BIGINT UNSIGNED NOT NULL,
    username VARCHAR(64) NOT NULL,
    auth_password VARCHAR(255) NOT NULL,
    status ENUM('active', 'suspended') NOT NULL DEFAULT 'active',
    quota_gb BIGINT UNSIGNED NOT NULL DEFAULT 0,
    used_gb BIGINT UNSIGNED NOT NULL DEFAULT 0,
    used_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
    speed_limit_mbps INT NOT NULL DEFAULT 0,
    expires_at DATETIME NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_hysteria_users_public_id (public_id),
    UNIQUE KEY uniq_node_user (node_id, username),
    CONSTRAINT fk_hysteria_users_node FOREIGN KEY (node_id) REFERENCES server_nodes(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    admin_id BIGINT UNSIGNED NULL,
    action VARCHAR(128) NOT NULL,
    target_type VARCHAR(64) NOT NULL,
    target_id VARCHAR(64) NULL,
    ip_address VARCHAR(64) NULL,
    details_json JSON NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_audit_logs_created_at (created_at),
    INDEX idx_audit_logs_action (action),
    CONSTRAINT fk_audit_logs_admin FOREIGN KEY (admin_id) REFERENCES admin_users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS system_settings (
    setting_key VARCHAR(128) PRIMARY KEY,
    setting_value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    filename VARCHAR(255) NOT NULL UNIQUE,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS node_traffic_history (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    node_id BIGINT UNSIGNED NOT NULL,
    total_rx BIGINT UNSIGNED NOT NULL DEFAULT 0,
    total_tx BIGINT UNSIGNED NOT NULL DEFAULT 0,
    delta_rx BIGINT UNSIGNED NOT NULL DEFAULT 0,
    delta_tx BIGINT UNSIGNED NOT NULL DEFAULT 0,
    user_count INT UNSIGNED NOT NULL DEFAULT 0,
    online_count INT UNSIGNED NOT NULL DEFAULT 0,
    recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_node_recorded (node_id, recorded_at),
    CONSTRAINT fk_node_traffic_history_node FOREIGN KEY (node_id) REFERENCES server_nodes(id) ON DELETE CASCADE
);

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
);

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
);
