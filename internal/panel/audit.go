package panel

import (
	"context"
	"database/sql"
	"encoding/json"
)

type auditService struct {
	db *sql.DB
}

func newAuditService(db *sql.DB) *auditService {
	return &auditService{db: db}
}

func (s *auditService) log(ctx context.Context, adminID int64, action string, targetType string, targetID string, details map[string]any, ip string) {
	if s == nil || s.db == nil {
		return
	}
	var detailsJSON any
	if len(details) > 0 {
		if data, err := json.Marshal(details); err == nil {
			detailsJSON = string(data)
		}
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (admin_id, action, target_type, target_id, ip_address, details_json) VALUES (?, ?, ?, ?, ?, ?)`,
		nullIfZero(adminID), action, targetType, nullIfEmpty(targetID), nullIfEmpty(ip), detailsJSON,
	)
}
