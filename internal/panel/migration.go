package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type migrationService struct {
	rootDir string
}

func newMigrationService(cfg Config) *migrationService {
	return &migrationService{rootDir: projectRootFromConfig(cfg)}
}

func (s *migrationService) ensureUpToDate(db *sql.DB) error {
	if s == nil || db == nil {
		return errors.New("数据库不可用")
	}
	if err := s.ensureMigrationTable(db); err != nil {
		return err
	}
	files, err := s.listMigrationFiles()
	if err != nil {
		return err
	}
	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		applied, err := s.hasMigration(db, fileName)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		sqlContent, err := os.ReadFile(filePath)
		if err != nil || strings.TrimSpace(string(sqlContent)) == "" {
			return fmt.Errorf("迁移文件 %s 为空或无法读取", fileName)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("自动升级数据库结构失败，请检查迁移文件 %s", fileName)
		}
		if _, err := tx.Exec(string(sqlContent)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("自动升级数据库结构失败，请检查迁移文件 %s", fileName)
		}
		if err := s.recordMigration(tx, fileName); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("自动升级数据库结构失败，请检查迁移文件 %s", fileName)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("自动升级数据库结构失败，请检查迁移文件 %s", fileName)
		}
	}
	return nil
}

func (s *migrationService) markAllAsApplied(db *sql.DB) error {
	if s == nil || db == nil {
		return errors.New("数据库不可用")
	}
	if err := s.ensureMigrationTable(db); err != nil {
		return err
	}
	files, err := s.listMigrationFiles()
	if err != nil {
		return err
	}
	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		applied, err := s.hasMigration(db, fileName)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (filename) VALUES (?) ON DUPLICATE KEY UPDATE filename = VALUES(filename)`, fileName); err != nil {
			return err
		}
	}
	return nil
}

func (s *migrationService) ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
		filename VARCHAR(255) NOT NULL UNIQUE,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	return err
}

func (s *migrationService) listMigrationFiles() ([]string, error) {
	pattern := filepath.Join(s.rootDir, "database", "migrations", "*.sql")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func (s *migrationService) hasMigration(db *sql.DB, fileName string) (bool, error) {
	var exists int
	if err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE filename = ? LIMIT 1`, fileName).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *migrationService) recordMigration(tx *sql.Tx, fileName string) error {
	_, err := tx.Exec(`INSERT INTO schema_migrations (filename) VALUES (?) ON DUPLICATE KEY UPDATE filename = VALUES(filename)`, fileName)
	return err
}
