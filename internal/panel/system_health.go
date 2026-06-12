package panel

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var requiredHealthTables = []string{"admin_users", "server_nodes", "hysteria_users", "audit_logs"}

type systemHealthService struct {
	db      *sql.DB
	rootDir string
	cfg     *Config
}

func newSystemHealthService(db *sql.DB, cfg *Config) *systemHealthService {
	rootDir := "."
	if cfg != nil {
		rootDir = projectRootFromConfig(*cfg)
	}
	return &systemHealthService{db: db, cfg: cfg, rootDir: rootDir}
}

func (s *systemHealthService) check(ctx context.Context) map[string]any {
	issues := make([]map[string]any, 0)

	if s.db == nil || s.cfg == nil || strings.TrimSpace(s.cfg.DBName) == "" {
		issues = append(issues, map[string]any{
			"level":   "error",
			"title":   "数据库检查失败",
			"message": "无法完成系统依赖检查，请检查数据库配置和服务状态。",
		})
	} else {
		for _, table := range requiredHealthTables {
			if !s.tableExists(ctx, s.cfg.DBName, table) {
				issues = append(issues, map[string]any{
					"level":   "error",
					"title":   "依赖数据不完整",
					"message": "系统依赖的数据表不完整，请检查数据库初始化状态。",
				})
				break
			}
		}
	}

	for _, path := range s.writableDirectories() {
		if err := os.MkdirAll(path, 0o775); err != nil {
			issues = append(issues, map[string]any{
				"level":   "error",
				"title":   "运行目录不可用",
				"message": "系统运行目录创建失败，请检查运行环境写入权限。",
			})
			continue
		}
		if !isWritableDirectory(path) {
			issues = append(issues, map[string]any{
				"level":   "error",
				"title":   "运行目录不可写",
				"message": "系统运行目录不可写，请检查部署目录权限。",
			})
		}
	}

	return map[string]any{
		"ok":         len(issues) == 0,
		"checked_at": time.Now().Format("2006-01-02 15:04:05"),
		"issues":     issues,
	}
}

func (s *systemHealthService) tableExists(ctx context.Context, databaseName string, table string) bool {
	if s.db == nil {
		return false
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`, databaseName, table).Scan(&count)
	return err == nil && count > 0
}

func (s *systemHealthService) writableDirectories() []string {
	return []string{
		filepath.Join(s.rootDir, "storage"),
		filepath.Join(s.rootDir, "storage", "jobs"),
		filepath.Join(s.rootDir, "storage", "locks"),
		filepath.Join(s.rootDir, "storage", "ssh-keys"),
	}
}

func isWritableDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}

	testFile := filepath.Join(path, ".write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(testFile)
	return true
}
