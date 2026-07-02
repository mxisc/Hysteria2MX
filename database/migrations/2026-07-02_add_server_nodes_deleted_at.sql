SET @has_deleted_at := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'server_nodes'
      AND COLUMN_NAME = 'deleted_at'
);

SET @sql := IF(
    @has_deleted_at = 0,
    'ALTER TABLE server_nodes ADD COLUMN deleted_at DATETIME NULL AFTER agent_service_name',
    'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @has_deleted_at_index := (
    SELECT COUNT(*)
    FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'server_nodes'
      AND INDEX_NAME = 'idx_server_nodes_deleted_at'
);

SET @sql := IF(
    @has_deleted_at_index = 0,
    'ALTER TABLE server_nodes ADD INDEX idx_server_nodes_deleted_at (deleted_at)',
    'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
