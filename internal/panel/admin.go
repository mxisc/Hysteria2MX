package panel

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type adminService struct {
	db *sql.DB
}

func newAdminService(db *sql.DB) *adminService {
	return &adminService{db: db}
}

func (s *adminService) listPage(ctx context.Context, page int, pageSize int) (map[string]any, error) {
	page, pageSize, offset := resolvePagination(page, pageSize)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, username, display_name, role, status, last_login_at, created_at, updated_at FROM admin_users ORDER BY id ASC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id          int64
			username    string
			displayName string
			role        string
			status      string
			lastLoginAt sql.NullTime
			createdAt   sql.NullTime
			updatedAt   sql.NullTime
		)
		if err := rows.Scan(&id, &username, &displayName, &role, &status, &lastLoginAt, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"id":            id,
			"username":      username,
			"display_name":  displayName,
			"role":          normalizeRole(role),
			"status":        normalizeAdminStatus(status),
			"last_login_at": nullTimeValue(lastLoginAt),
			"created_at":    nullTimeValue(createdAt),
			"updated_at":    nullTimeValue(updatedAt),
		})
	}

	return map[string]any{
		"items": items,
		"pagination": map[string]any{
			"page":       page,
			"pageSize":   pageSize,
			"total":      total,
			"totalPages": totalPages(total, pageSize),
		},
	}, nil
}

func (s *adminService) create(ctx context.Context, payload map[string]any) (map[string]any, error) {
	username := toTrimmedString(payload["username"])
	displayName := toTrimmedString(payload["display_name"])
	password := toTrimmedString(payload["password"])
	role := normalizeRole(toTrimmedString(payload["role"]))
	status := normalizeAdminStatus(toTrimmedString(payload["status"]))

	if username == "" || displayName == "" || password == "" {
		return nil, errors.New("用户名、显示名称和密码不能为空")
	}
	if err := validateAdminUsername(username); err != nil {
		return nil, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	result, err := s.db.ExecContext(ctx, `INSERT INTO admin_users (username, password_hash, display_name, role, status) VALUES (?, ?, ?, ?, ?)`,
		username, passwordHash, displayName, role, status)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.find(ctx, id)
}

func (s *adminService) update(ctx context.Context, id int64, payload map[string]any, actorID int64) (map[string]any, error) {
	current, err := s.findWithPassword(ctx, id)
	if err != nil {
		return nil, err
	}

	username := defaultString(toTrimmedString(payload["username"]), current["username"].(string))
	displayName := defaultString(toTrimmedString(payload["display_name"]), current["display_name"].(string))
	role := normalizeRole(defaultString(toTrimmedString(payload["role"]), current["role"].(string)))
	status := normalizeAdminStatus(defaultString(toTrimmedString(payload["status"]), current["status"].(string)))
	passwordHash := current["password_hash"].(string)

	if newPassword := toTrimmedString(payload["password"]); newPassword != "" {
		passwordHash, err = hashPassword(newPassword)
		if err != nil {
			return nil, err
		}
	}

	if err := validateAdminUsername(username); err != nil {
		return nil, err
	}
	if err := s.assertSuperAdminSafety(ctx, id, role, status, actorID, false, current); err != nil {
		return nil, err
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE admin_users SET username=?, display_name=?, role=?, status=?, password_hash=? WHERE id=?`,
		username, displayName, role, status, passwordHash, id); err != nil {
		return nil, err
	}

	return s.find(ctx, id)
}

func (s *adminService) delete(ctx context.Context, id int64, actorID int64) error {
	if id == actorID {
		return errors.New("不能删除当前登录账号")
	}

	current, err := s.findWithPassword(ctx, id)
	if err != nil {
		return err
	}
	if err := s.assertSuperAdminSafety(ctx, id, "viewer", "disabled", actorID, true, current); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM admin_users WHERE id = ?`, id)
	return err
}

func (s *adminService) find(ctx context.Context, id int64) (map[string]any, error) {
	item, err := s.findWithPassword(ctx, id)
	if err != nil {
		return nil, err
	}
	delete(item, "password_hash")
	return item, nil
}

func (s *adminService) findWithPassword(ctx context.Context, id int64) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, display_name, role, status, last_login_at, created_at, updated_at FROM admin_users WHERE id = ? LIMIT 1`, id)
	var (
		adminID      int64
		username     string
		passwordHash string
		displayName  string
		role         string
		status       string
		lastLoginAt  sql.NullTime
		createdAt    sql.NullTime
		updatedAt    sql.NullTime
	)
	if err := row.Scan(&adminID, &username, &passwordHash, &displayName, &role, &status, &lastLoginAt, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("管理员不存在")
		}
		return nil, err
	}

	return map[string]any{
		"id":            adminID,
		"username":      username,
		"password_hash": passwordHash,
		"display_name":  displayName,
		"role":          normalizeRole(role),
		"status":        normalizeAdminStatus(status),
		"last_login_at": nullTimeValue(lastLoginAt),
		"created_at":    nullTimeValue(createdAt),
		"updated_at":    nullTimeValue(updatedAt),
	}, nil
}

func (s *adminService) assertSuperAdminSafety(ctx context.Context, targetID int64, nextRole string, nextStatus string, actorID int64, isDelete bool, current map[string]any) error {
	currentRole := current["role"].(string)
	currentStatus := current["status"].(string)
	if currentRole != "super_admin" || currentStatus != "active" {
		return nil
	}
	if !isDelete && nextRole == "super_admin" && nextStatus == "active" {
		return nil
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users WHERE role='super_admin' AND status='active' AND id <> ?`, targetID).Scan(&count); err != nil {
		return err
	}
	if count <= 0 {
		return errors.New("系统至少需要保留一个启用中的超级管理员")
	}
	if targetID == actorID {
		return errors.New("不能降低或停用当前超级管理员自身权限")
	}
	return nil
}

func resolvePagination(page int, pageSize int) (int, int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, (page - 1) * pageSize
}

func totalPages(total int, pageSize int) int {
	if total <= 0 {
		return 1
	}
	return (total + pageSize - 1) / pageSize
}

func nullTimeValue(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.Format("2006-01-02 15:04:05")
}

func toTrimmedString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}
