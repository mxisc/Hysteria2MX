SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = 'hysteria_users'
      AND column_name = 'used_bytes'
);

SET @add_column_sql := IF(
    @column_exists = 0,
    'ALTER TABLE hysteria_users ADD COLUMN used_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER used_gb',
    'SELECT 1'
);
PREPARE add_column_stmt FROM @add_column_sql;
EXECUTE add_column_stmt;
DEALLOCATE PREPARE add_column_stmt;

UPDATE hysteria_users
SET used_bytes = used_gb * 1024 * 1024 * 1024
WHERE used_bytes = 0
  AND used_gb > 0;
