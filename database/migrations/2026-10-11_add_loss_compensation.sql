SET @has_column := (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='server_nodes' AND COLUMN_NAME='disable_loss_compensation');
SET @sql := IF(@has_column=0,'ALTER TABLE server_nodes ADD COLUMN disable_loss_compensation TINYINT(1) NOT NULL DEFAULT 0 AFTER obfs_enabled','SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
