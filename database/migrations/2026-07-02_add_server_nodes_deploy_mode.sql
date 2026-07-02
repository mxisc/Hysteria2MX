SET @has_deploy_mode := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'server_nodes'
      AND COLUMN_NAME = 'deploy_mode'
);

SET @sql := IF(
    @has_deploy_mode = 0,
    'ALTER TABLE server_nodes ADD COLUMN deploy_mode ENUM(''ssh'', ''local'') NOT NULL DEFAULT ''ssh'' AFTER current_node',
    'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
