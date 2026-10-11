package panel

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Convert the earlier operator override once, then let the node editor own it.
func migrateLegacyObfsConfig(db *sql.DB, cfg *Config) error {
	if strings.TrimSpace(cfg.ObfsDisabledNodeIDs) == "" {
		return nil
	}
	for _, value := range strings.Split(cfg.ObfsDisabledNodeIDs, ",") {
		id, e := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if e != nil || id < 1 {
			return fmt.Errorf("旧混淆配置无效，请检查节点编号")
		}
		if _, e = db.Exec(`UPDATE server_nodes SET obfs_enabled=0 WHERE id=?`, id); e != nil {
			return fmt.Errorf("节点混淆设置迁移失败")
		}
	}
	values, e := loadEnvFile(cfg.WritablePath())
	if e != nil {
		return fmt.Errorf("旧混淆配置迁移失败")
	}
	delete(values, "HYSTERIA_OBFS_DISABLED_NODES")
	if e = SaveConfigFile(cfg.WritablePath(), values); e != nil {
		return fmt.Errorf("旧混淆配置迁移失败")
	}
	cfg.ObfsDisabledNodeIDs = ""
	return nil
}
