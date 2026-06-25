SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = 'hysteria_users'
      AND column_name = 'public_id'
);

SET @add_column_sql := IF(
    @column_exists = 0,
    'ALTER TABLE hysteria_users ADD COLUMN public_id VARCHAR(64) NULL AFTER id',
    'SELECT 1'
);
PREPARE add_column_stmt FROM @add_column_sql;
EXECUTE add_column_stmt;
DEALLOCATE PREPARE add_column_stmt;

UPDATE hysteria_users
SET public_id = CONCAT('usr_', SUBSTRING(SHA2(CONCAT(id, ':', username, ':', UUID()), 256), 1, 20))
WHERE public_id IS NULL OR public_id = '';

ALTER TABLE hysteria_users MODIFY public_id VARCHAR(64) NOT NULL;

SET @index_exists := (
    SELECT COUNT(*)
    FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = 'hysteria_users'
      AND index_name = 'uniq_hysteria_users_public_id'
);

SET @add_index_sql := IF(
    @index_exists = 0,
    'ALTER TABLE hysteria_users ADD UNIQUE KEY uniq_hysteria_users_public_id (public_id)',
    'SELECT 1'
);
PREPARE add_index_stmt FROM @add_index_sql;
EXECUTE add_index_stmt;
DEALLOCATE PREPARE add_index_stmt;
