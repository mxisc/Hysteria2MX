package panel

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type hysteriaService struct {
	db      *sql.DB
	cfg     *Config
	sshKeys *sshKeyUploadService
	remote  *remoteExecutor
	agents  *agentService
}

type nodeRecord struct {
	ID                           int64
	CurrentNode                  int
	DeployMode                   string
	Name                         string
	Host                         string
	SSHPort                      int
	SSHUsername                  string
	SSHAuthType                  string
	SSHPassword                  string
	SSHPrivateKeyPath            string
	SudoPassword                 string
	InstallScript                string
	ServiceName                  string
	ConfigPath                   string
	ListenPort                   int
	TrafficStatsListen           string
	TrafficStatsSecret           string
	TLSMode                      string
	TLSCertPath                  string
	TLSKeyPath                   string
	Domain                       string
	ACMEEmail                    string
	ObfsPassword                 string
	MasqueradeURL                string
	BandwidthUpMbps              int
	BandwidthDownMbps            int
	ManageMode                   string
	AgentEnabled                 bool
	AgentReportIntervalSeconds   int
	AgentTaskPollIntervalSeconds int
	AgentInstallPath             string
	AgentConfigPath              string
	AgentServiceName             string
	DeletedAt                    sql.NullTime
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

type userRecord struct {
	ID             int64
	PublicID       string
	NodeID         int64
	Username       string
	AuthPassword   string
	Status         string
	QuotaGB        int64
	UsedGB         int64
	UsedBytes      int64
	SpeedLimitMbps int
	ExpiresAt      sql.NullTime
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type subscriptionFormat string

const (
	subscriptionFormatURI    subscriptionFormat = "uri"
	subscriptionFormatClash  subscriptionFormat = "clash"
	trafficSummaryWindowDays                    = 30
)

func newHysteriaService(db *sql.DB, cfg *Config, sshKeys *sshKeyUploadService, remote *remoteExecutor, agents *agentService) *hysteriaService {
	return &hysteriaService{db: db, cfg: cfg, sshKeys: sshKeys, remote: remote, agents: agents}
}

func (s *hysteriaService) panelStateForUser(ctx context.Context, user *authUser) (map[string]any, error) {
	node, err := s.getStoredNode(ctx, 0)
	if err != nil {
		return nil, err
	}

	service := map[string]any{
		"command":  "",
		"output":   "未配置节点",
		"exitCode": 1,
	}
	userMetrics := map[string]any{
		"userCount":       0,
		"activeUserCount": 0,
		"quotaTotalGb":    0,
		"quotaUsedGb":     0,
	}

	if node != nil {
		service = s.cachedServiceStatus(ctx, *node)
		userMetrics, err = s.getUserMetricsForNode(ctx, node.ID)
		if err != nil {
			return nil, err
		}
	}

	nodeCount := 0
	if s.db != nil {
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM server_nodes WHERE deleted_at IS NULL`).Scan(&nodeCount)
	}

	var presentedNode any
	if node != nil {
		presentedNode, err = s.presentNode(ctx, *node, s.canViewSensitiveFields(user))
		if err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"node":    presentedNode,
		"service": service,
		"metrics": map[string]any{
			"nodeCount":       nodeCount,
			"userCount":       userMetrics["userCount"],
			"activeUserCount": userMetrics["activeUserCount"],
			"quotaTotalGb":    userMetrics["quotaTotalGb"],
			"quotaUsedGb":     userMetrics["quotaUsedGb"],
		},
	}, nil
}

func (s *hysteriaService) listNodesPageForUser(ctx context.Context, user *authUser, page int, pageSize int) (map[string]any, error) {
	page, pageSize, offset := resolvePagination(page, pageSize)
	total := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM server_nodes WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, current_node, deploy_mode, name, host, ssh_port, ssh_username, ssh_auth_type, ssh_password, ssh_private_key_path,
		       sudo_password, install_script, service_name, config_path, listen_port, traffic_stats_listen, traffic_stats_secret,
		       tls_mode, tls_cert_path, tls_key_path, domain, acme_email, obfs_password, masquerade_url, bandwidth_up_mbps,
		       bandwidth_down_mbps, manage_mode, agent_enabled, agent_report_interval_seconds, agent_task_poll_interval_seconds,
		       agent_install_path, agent_config_path, agent_service_name, deleted_at, created_at, updated_at
		FROM server_nodes
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC
		LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]any, 0)
	for rows.Next() {
		record, err := scanNodeRecord(rows)
		if err != nil {
			return nil, err
		}
		item, err := s.presentNode(ctx, record, s.canViewSensitiveFields(user))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return paginatedResult(items, page, pageSize, total), nil
}

func (s *hysteriaService) getNodeForUser(ctx context.Context, user *authUser, id int64) (map[string]any, error) {
	node, err := s.getStoredNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("节点不存在")
	}
	if node.DeletedAt.Valid {
		return nil, errors.New("节点不存在")
	}
	return s.presentNode(ctx, *node, s.canViewSensitiveFields(user))
}

func (s *hysteriaService) saveNode(ctx context.Context, payload map[string]any, id int64) (map[string]any, error) {
	var current *nodeRecord
	var err error
	if id > 0 {
		current, err = s.getStoredNode(ctx, id)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, errors.New("节点不存在")
		}
		if current.DeletedAt.Valid {
			return nil, errors.New("节点不存在")
		}
	}

	data, err := s.normalizeNodePayload(payload, current)
	if err != nil {
		return nil, err
	}
	if err := s.assertNodeIdentityAvailable(ctx, data, id); err != nil {
		return nil, err
	}

	encrypted, err := s.encryptNodeSecrets(data)
	if err != nil {
		return nil, err
	}

	if current != nil {
		if err := s.cleanupReplacedManagedNodeKey(current.SSHPrivateKeyPath, data["ssh_private_key_path"].(string), data["ssh_auth_type"].(string)); err != nil {
			return nil, err
		}
		_, err = s.db.ExecContext(ctx, `
			UPDATE server_nodes SET
				deploy_mode=?, name=?, host=?, ssh_port=?, ssh_username=?, ssh_auth_type=?, ssh_password=?, ssh_private_key_path=?, sudo_password=?,
				install_script=?, service_name=?, config_path=?, listen_port=?, traffic_stats_listen=?, traffic_stats_secret=?, tls_mode=?,
				tls_cert_path=?, tls_key_path=?, domain=?, acme_email=?, obfs_password=?, masquerade_url=?, bandwidth_up_mbps=?,
				bandwidth_down_mbps=?, manage_mode=?, agent_enabled=?, agent_report_interval_seconds=?, agent_task_poll_interval_seconds=?,
				agent_install_path=?, agent_config_path=?, agent_service_name=?
			WHERE id=?`,
			encrypted["deploy_mode"], encrypted["name"], encrypted["host"], encrypted["ssh_port"], encrypted["ssh_username"], encrypted["ssh_auth_type"],
			encrypted["ssh_password"], encrypted["ssh_private_key_path"], encrypted["sudo_password"], encrypted["install_script"],
			encrypted["service_name"], encrypted["config_path"], encrypted["listen_port"], encrypted["traffic_stats_listen"],
			encrypted["traffic_stats_secret"], encrypted["tls_mode"], encrypted["tls_cert_path"], encrypted["tls_key_path"],
			encrypted["domain"], encrypted["acme_email"], encrypted["obfs_password"], encrypted["masquerade_url"],
			encrypted["bandwidth_up_mbps"], encrypted["bandwidth_down_mbps"], encrypted["manage_mode"], encrypted["agent_enabled"],
			encrypted["agent_report_interval_seconds"], encrypted["agent_task_poll_interval_seconds"], encrypted["agent_install_path"],
			encrypted["agent_config_path"], encrypted["agent_service_name"], current.ID)
		if err != nil {
			return nil, normalizeDBError(err, "节点保存失败")
		}
		return s.getNodeForUser(ctx, nil, current.ID)
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO server_nodes (
			deploy_mode, name, host, ssh_port, ssh_username, ssh_auth_type, ssh_password, ssh_private_key_path, sudo_password,
			install_script, service_name, config_path, listen_port, traffic_stats_listen, traffic_stats_secret, tls_mode,
			tls_cert_path, tls_key_path, domain, acme_email, obfs_password, masquerade_url, bandwidth_up_mbps, bandwidth_down_mbps,
			manage_mode, agent_enabled, agent_report_interval_seconds, agent_task_poll_interval_seconds, agent_install_path,
			agent_config_path, agent_service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		encrypted["deploy_mode"], encrypted["name"], encrypted["host"], encrypted["ssh_port"], encrypted["ssh_username"], encrypted["ssh_auth_type"],
		encrypted["ssh_password"], encrypted["ssh_private_key_path"], encrypted["sudo_password"], encrypted["install_script"], encrypted["service_name"],
		encrypted["config_path"], encrypted["listen_port"], encrypted["traffic_stats_listen"], encrypted["traffic_stats_secret"], encrypted["tls_mode"],
		encrypted["tls_cert_path"], encrypted["tls_key_path"], encrypted["domain"], encrypted["acme_email"], encrypted["obfs_password"],
		encrypted["masquerade_url"], encrypted["bandwidth_up_mbps"], encrypted["bandwidth_down_mbps"], encrypted["manage_mode"],
		encrypted["agent_enabled"], encrypted["agent_report_interval_seconds"], encrypted["agent_task_poll_interval_seconds"],
		encrypted["agent_install_path"], encrypted["agent_config_path"], encrypted["agent_service_name"])
	if err != nil {
		return nil, normalizeDBError(err, "节点创建失败")
	}

	newID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.getNodeForUser(ctx, nil, newID)
}

func (s *hysteriaService) deleteNode(ctx context.Context, id int64) error {
	node, err := s.getStoredNode(ctx, id)
	if err != nil {
		return err
	}
	if node == nil {
		return errors.New("节点不存在")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM hysteria_users WHERE node_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE server_nodes SET deleted_at = COALESCE(deleted_at, NOW()) WHERE id = ?`, id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if s.sshKeys != nil {
		s.sshKeys.deleteManagedKey(node.SSHPrivateKeyPath, "")
	}

	return nil
}

func (s *hysteriaService) listUsersPageForUser(ctx context.Context, user *authUser, page int, pageSize int, keyword string) (map[string]any, error) {
	page, pageSize, offset := resolvePagination(page, pageSize)
	keyword = strings.TrimSpace(keyword)
	where := `WHERE 1=1`
	args := []any{}
	if keyword != "" {
		where += ` AND username LIKE ?`
		args = append(args, "%"+keyword+"%")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM hysteria_users u
		INNER JOIN server_nodes n ON n.id = u.node_id AND n.deleted_at IS NULL `+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, pageSize, offset)
	rows, err := s.db.QueryContext(ctx, `
                SELECT u.id, u.public_id, u.node_id, u.username, u.auth_password, u.status, u.quota_gb, u.used_gb, u.used_bytes, u.speed_limit_mbps, u.expires_at, u.created_at, u.updated_at
		FROM hysteria_users u
		INNER JOIN server_nodes n ON n.id = u.node_id AND n.deleted_at IS NULL
		`+where+` ORDER BY u.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]any, 0)
	for rows.Next() {
		record, err := scanUserRecord(rows)
		if err != nil {
			return nil, err
		}
		item, err := s.presentUser(record, s.canViewSensitiveFields(user))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return paginatedResult(items, page, pageSize, total), nil
}

func (s *hysteriaService) listLogicalUsersPageForUser(ctx context.Context, user *authUser, page int, pageSize int, keyword string, filter string) (map[string]any, error) {
	page, pageSize, offset := resolvePagination(page, pageSize)
	keyword = strings.TrimSpace(keyword)
	filter = strings.TrimSpace(filter)

	where := `WHERE n.deleted_at IS NULL`
	args := []any{}
	if keyword != "" {
		where += ` AND u.username LIKE ?`
		args = append(args, "%"+keyword+"%")
	}

	abnormalSQL := `(u.status <> 'active' OR u.expires_at < NOW() OR (u.quota_gb > 0 AND u.used_bytes >= u.quota_gb * 1024 * 1024 * 1024))`
	having := ``
	switch filter {
	case "abnormal":
		having = ` HAVING SUM(CASE WHEN ` + abnormalSQL + ` THEN 1 ELSE 0 END) > 0`
	case "single":
		having = ` HAVING COUNT(*) = 1`
	case "multi":
		having = ` HAVING COUNT(*) > 1`
	}

	groupedSQL := `
                SELECT u.username,
                       COUNT(*) AS node_count,
                       MAX(u.quota_gb) AS quota_gb,
                       SUM(u.used_bytes) AS used_bytes,
                       SUM(CASE WHEN ` + abnormalSQL + ` THEN 1 ELSE 0 END) AS abnormal_count,
                       MAX(u.updated_at) AS last_updated_at
                FROM hysteria_users u
                INNER JOIN server_nodes n ON n.id = u.node_id
                ` + where + `
                GROUP BY u.username` + having

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+groupedSQL+`) grouped_users`, args...).Scan(&total); err != nil {
		return nil, err
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, pageSize, offset)
	rows, err := s.db.QueryContext(ctx, groupedSQL+` ORDER BY last_updated_at DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type groupedUserRow struct {
		Username      string
		NodeCount     int
		QuotaGB       int64
		UsedBytes     int64
		AbnormalCount int
	}

	groups := make([]groupedUserRow, 0)
	usernames := make([]string, 0)
	for rows.Next() {
		var item groupedUserRow
		var lastUpdatedAt time.Time
		if err := rows.Scan(&item.Username, &item.NodeCount, &item.QuotaGB, &item.UsedBytes, &item.AbnormalCount, &lastUpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, item)
		usernames = append(usernames, item.Username)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return paginatedResult([]any{}, page, pageSize, total), nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(usernames)), ",")
	detailArgs := make([]any, 0, len(usernames))
	for _, username := range usernames {
		detailArgs = append(detailArgs, username)
	}
	detailRows, err := s.db.QueryContext(ctx, `
                SELECT u.id, u.public_id, u.node_id, u.username, u.auth_password, u.status, u.quota_gb, u.used_gb, u.used_bytes, u.speed_limit_mbps, u.expires_at, u.created_at, u.updated_at,
                       COALESCE(n.name, '') AS node_name
                FROM hysteria_users u
                INNER JOIN server_nodes n ON n.id = u.node_id AND n.deleted_at IS NULL
                WHERE u.username IN (`+placeholders+`)
                ORDER BY u.username ASC, n.name ASC, u.id DESC`, detailArgs...)
	if err != nil {
		return nil, err
	}
	defer detailRows.Close()

	detailsByUsername := map[string][]any{}
	nodesByUsername := map[string][]any{}
	for detailRows.Next() {
		var record userRecord
		var nodeName string
		if err := detailRows.Scan(
			&record.ID,
			&record.PublicID,
			&record.NodeID,
			&record.Username,
			&record.AuthPassword,
			&record.Status,
			&record.QuotaGB,
			&record.UsedGB,
			&record.UsedBytes,
			&record.SpeedLimitMbps,
			&record.ExpiresAt,
			&record.CreatedAt,
			&record.UpdatedAt,
			&nodeName,
		); err != nil {
			return nil, err
		}
		detail, err := s.presentUser(record, s.canViewSensitiveFields(user))
		if err != nil {
			return nil, err
		}
		detail["node_name"] = nodeName
		detailsByUsername[record.Username] = append(detailsByUsername[record.Username], detail)
		nodesByUsername[record.Username] = append(nodesByUsername[record.Username], map[string]any{
			"id":   record.NodeID,
			"name": nodeName,
		})
	}
	if err := detailRows.Err(); err != nil {
		return nil, err
	}

	items := make([]any, 0, len(groups))
	for _, group := range groups {
		status := "normal"
		if group.AbnormalCount > 0 {
			status = "partial_abnormal"
		}
		items = append(items, map[string]any{
			"username":       group.Username,
			"status":         status,
			"node_count":     group.NodeCount,
			"quota_gb":       group.QuotaGB,
			"used_gb":        roundBytesToGigabytes(group.UsedBytes, 0),
			"abnormal_count": group.AbnormalCount,
			"nodes":          nodesByUsername[group.Username],
			"details":        detailsByUsername[group.Username],
		})
	}

	return paginatedResult(items, page, pageSize, total), nil
}

func (s *hysteriaService) createUser(ctx context.Context, payload map[string]any) (map[string]any, error) {
	data, err := s.normalizeUserPayload(payload, nil)
	if err != nil {
		return nil, err
	}
	nodes, err := s.listStoredNodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, errors.New("请先创建节点")
	}
	for _, node := range nodes {
		if err := s.assertUniqueUserCredential(ctx, node.ID, data["auth_password"].(string), 0); err != nil {
			return nil, err
		}
		if existing, err := s.findStoredUserByNodeAndUsername(ctx, node.ID, toString(data["username"])); err != nil {
			return nil, err
		} else if existing != nil {
			return nil, errors.New("该用户名已存在，请勿重复创建")
		}
	}

	encryptedPassword, err := EncryptValue(data["auth_password"].(string), *s.cfg)
	if err != nil {
		return nil, errors.New("用户认证密码加密失败")
	}

	var createdID int64
	for index, node := range nodes {
		result, err := s.db.ExecContext(ctx, `
                INSERT INTO hysteria_users (public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.generateUserPublicID(ctx), node.ID, data["username"], encryptedPassword, data["status"], data["quota_gb"], data["used_gb"], bytesFromGigabytesFloat(float64Value(data["used_gb"])), data["speed_limit_mbps"], data["expires_at"])
		if err != nil {
			return nil, normalizeDBError(err, "用户创建失败")
		}
		if index == 0 {
			createdID, err = result.LastInsertId()
			if err != nil {
				return nil, err
			}
		}
		s.queueLocalAuthRefresh(ctx, node, nil)
	}
	return s.getUser(ctx, createdID)
}

func (s *hysteriaService) updateUser(ctx context.Context, id int64, payload map[string]any) (map[string]any, error) {
	current, err := s.getStoredUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, errors.New("用户不存在")
	}

	currentPlain, err := s.decryptUser(*current)
	if err != nil {
		return nil, err
	}

	data, err := s.normalizeUserPayload(payload, &currentPlain)
	if err != nil {
		return nil, err
	}
	relatedUsers, err := s.listStoredUsersByUsername(ctx, currentPlain.Username)
	if err != nil {
		return nil, err
	}
	if len(relatedUsers) == 0 {
		return nil, errors.New("用户不存在")
	}
	for _, item := range relatedUsers {
		if err := s.assertUniqueUserCredential(ctx, item.NodeID, data["auth_password"].(string), item.ID); err != nil {
			return nil, err
		}
	}

	encryptedPassword, err := EncryptValue(data["auth_password"].(string), *s.cfg)
	if err != nil {
		return nil, errors.New("用户认证密码加密失败")
	}

	nextUsername := toString(data["username"])
	nextStatus := normalizeUserStatus(toString(data["status"]))
	nextQuota := int64Value(data["quota_gb"])
	nextUsedBytes := bytesFromGigabytesFloat(float64Value(data["used_gb"]))
	nextExpires, _ := data["expires_at"].(sql.NullTime)
	for _, item := range relatedUsers {
		_, err = s.db.ExecContext(ctx, `
                UPDATE hysteria_users SET username=?, auth_password=?, status=?, quota_gb=?, used_gb=?, used_bytes=?, speed_limit_mbps=?, expires_at=? WHERE id=?`,
			data["username"], encryptedPassword, data["status"], data["quota_gb"], data["used_gb"], nextUsedBytes, data["speed_limit_mbps"], data["expires_at"], item.ID)
		if err != nil {
			return nil, normalizeDBError(err, "用户更新失败")
		}

		if node, nodeErr := s.getStoredNode(ctx, item.NodeID); nodeErr == nil && node != nil {
			kickClients := []string{}
			if item.Username != nextUsername {
				kickClients = append(kickClients, item.Username, nextUsername)
			}
			if item.AuthPassword != toString(data["auth_password"]) {
				kickClients = append(kickClients, item.Username)
			}
			if nextStatus != "active" || (nextExpires.Valid && !nextExpires.Time.After(time.Now())) || (nextQuota > 0 && nextUsedBytes >= gigabytesToBytes(nextQuota)) {
				kickClients = append(kickClients, item.Username, nextUsername)
			}
			s.queueLocalAuthRefresh(ctx, *node, uniqueTrimmedStrings(kickClients))
		}
	}
	return s.getUser(ctx, id)
}

func (s *hysteriaService) deleteUser(ctx context.Context, id int64) error {
	current, err := s.getStoredUser(ctx, id)
	if err != nil || current == nil {
		return err
	}
	user, err := s.decryptUser(*current)
	if err != nil {
		return err
	}
	relatedUsers, err := s.listStoredUsersByUsername(ctx, user.Username)
	if err != nil {
		return err
	}
	for _, item := range relatedUsers {
		if _, err = s.db.ExecContext(ctx, `DELETE FROM hysteria_users WHERE id = ?`, item.ID); err != nil {
			return err
		}
		if node, nodeErr := s.getStoredNode(ctx, item.NodeID); nodeErr == nil && node != nil {
			s.queueLocalAuthRefresh(ctx, *node, []string{user.Username})
		}
	}
	return err
}

func (s *hysteriaService) getUser(ctx context.Context, id int64) (map[string]any, error) {
	user, err := s.getStoredUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}
	return s.presentUser(*user, true)
}

func (s *hysteriaService) getUserSubscriptionInfo(ctx context.Context, id int64, apiBaseURL string) (map[string]any, error) {
	user, err := s.getStoredUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	decrypted, err := s.decryptUser(*user)
	if err != nil {
		return nil, err
	}
	if err := s.ensureSubscriptionUsersOnAvailableNodes(ctx, decrypted); err != nil {
		return nil, err
	}
	entries, err := s.resolveSubscriptionEntries(ctx, decrypted.Username)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("当前用户没有可订阅的节点")
	}

	nodes := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		nodes = append(nodes, map[string]any{
			"id":   entry.Node.ID,
			"name": entry.Node.Name,
		})
	}

	return map[string]any{
		"url":        s.buildUserSubscriptionURL(decrypted, apiBaseURL),
		"username":   decrypted.Username,
		"node_count": len(entries),
		"nodes":      nodes,
	}, nil
}

func (s *hysteriaService) renderUserSubscription(ctx context.Context, publicID string, token string, format subscriptionFormat) (string, error) {
	user, err := s.getStoredUserByPublicID(ctx, publicID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", errors.New("订阅链接无效或已失效")
	}

	decrypted, err := s.decryptUser(*user)
	if err != nil {
		return "", err
	}
	if s.buildUserSubscriptionToken(decrypted) != strings.TrimSpace(token) {
		return "", errors.New("订阅链接无效或已失效")
	}

	if err := s.ensureSubscriptionUsersOnAvailableNodes(ctx, decrypted); err != nil {
		return "", err
	}
	entries, err := s.resolveSubscriptionEntries(ctx, decrypted.Username)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", errors.New("当前订阅暂无可用节点")
	}

	if format == subscriptionFormatClash {
		return renderClashSubscriptionEntries(entries), nil
	}
	return renderURISubscriptionEntries(entries), nil
}

func renderURISubscriptionEntries(entries []subscriptionEntry) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, buildConnectionURIForNodeUser(entry.Node, entry.User))
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderClashSubscriptionEntries(entries []subscriptionEntry) string {
	proxyNames := make([]string, 0, len(entries))
	var builder strings.Builder
	builder.WriteString("proxies:\n")
	for _, entry := range entries {
		name := entry.User.Username + "@" + entry.Node.Name
		proxyNames = append(proxyNames, name)
		builder.WriteString("  - name: " + yamlQuote(name) + "\n")
		builder.WriteString("    type: hysteria2\n")
		builder.WriteString("    server: " + yamlQuote(subscriptionServerHost(entry.Node)) + "\n")
		builder.WriteString(fmt.Sprintf("    port: %d\n", entry.Node.ListenPort))
		builder.WriteString("    password: " + yamlQuote(entry.User.AuthPassword) + "\n")
		if normalizeTLSMode(entry.Node.TLSMode) == "self_signed" {
			builder.WriteString("    skip-cert-verify: true\n")
		}
		if strings.TrimSpace(entry.Node.Domain) != "" {
			builder.WriteString("    sni: " + yamlQuote(strings.TrimSpace(entry.Node.Domain)) + "\n")
		}
		if strings.TrimSpace(entry.Node.ObfsPassword) != "" {
			builder.WriteString("    obfs: salamander\n")
			builder.WriteString("    obfs-password: " + yamlQuote(entry.Node.ObfsPassword) + "\n")
		}
		upMbps, downMbps := subscriptionBandwidth(entry.Node, entry.User)
		if upMbps > 0 {
			builder.WriteString(fmt.Sprintf("    up: %s\n", yamlQuote(fmt.Sprintf("%d Mbps", upMbps))))
		}
		if downMbps > 0 {
			builder.WriteString(fmt.Sprintf("    down: %s\n", yamlQuote(fmt.Sprintf("%d Mbps", downMbps))))
		}
	}

	builder.WriteString("proxy-groups:\n")
	builder.WriteString("  - name: " + yamlQuote("PROXY") + "\n")
	builder.WriteString("    type: select\n")
	builder.WriteString("    proxies:\n")
	for _, name := range proxyNames {
		builder.WriteString("      - " + yamlQuote(name) + "\n")
	}
	builder.WriteString("rules:\n")
	builder.WriteString("  - MATCH,PROXY\n")
	return builder.String()
}

func (s *hysteriaService) onlineClients(ctx context.Context, nodeID int64, page int, pageSize int) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("请先保存节点配置")
	}

	if s.shouldUseAgent(*node) {
		result, err := s.runAgentTask(ctx, *node, opcodeFetchOnline, nil, 20)
		if err != nil {
			return nil, err
		}
		itemsRaw, _ := mapValue(result["payload"])["items"].([]any)
		items := make([]map[string]any, 0, len(itemsRaw))
		for _, item := range itemsRaw {
			if row, ok := item.(map[string]any); ok {
				items = append(items, row)
			}
		}
		return s.sliceListResponse(items, page, pageSize), nil
	}

	result, err := s.callTrafficStatsAPI(ctx, *node, "/online", "GET", nil)
	if err != nil {
		return nil, err
	}
	payload := mapValue(result["payload"])
	items := make([]map[string]any, 0, len(payload))
	for id, connections := range payload {
		items = append(items, map[string]any{
			"id":          id,
			"connections": intValue(connections),
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		if intValue(items[i]["connections"]) != intValue(items[j]["connections"]) {
			return intValue(items[i]["connections"]) > intValue(items[j]["connections"])
		}
		return toString(items[i]["id"]) < toString(items[j]["id"])
	})
	return s.sliceListResponse(items, page, pageSize), nil
}

func (s *hysteriaService) dumpStreams(ctx context.Context, nodeID int64, page int, pageSize int) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("请先保存节点配置")
	}

	if s.shouldUseAgent(*node) {
		result, err := s.runAgentTask(ctx, *node, opcodeFetchStreams, nil, 20)
		if err != nil {
			return nil, err
		}
		itemsRaw, _ := mapValue(result["payload"])["items"].([]any)
		items := make([]map[string]any, 0, len(itemsRaw))
		for _, item := range itemsRaw {
			if row, ok := item.(map[string]any); ok {
				items = append(items, row)
			}
		}
		return s.sliceListResponse(items, page, pageSize), nil
	}

	result, err := s.callTrafficStatsAPI(ctx, *node, "/dump/streams", "GET", nil)
	if err != nil {
		return nil, err
	}
	streamsRaw, _ := mapValue(result["payload"])["streams"].([]any)
	items := make([]map[string]any, 0, len(streamsRaw))
	for _, item := range streamsRaw {
		if row, ok := item.(map[string]any); ok {
			items = append(items, row)
		}
	}
	return s.sliceListResponse(items, page, pageSize), nil
}

func (s *hysteriaService) getRealtimeTrafficStats(ctx context.Context, nodeID int64, page int, pageSize int, usernames []string) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("请先保存节点配置")
	}

	if s.shouldUseAgent(*node) {
		items, err := s.getCachedAgentUserTrafficStats(ctx, node.ID, usernames)
		if err != nil {
			return nil, err
		}
		response := s.sliceListResponse(items, page, pageSize)
		response["exitCode"] = 0
		response["output"] = ""
		return response, nil
	}

	result, err := s.callTrafficStatsAPI(ctx, *node, "/traffic", "GET", nil)
	if err != nil {
		return nil, err
	}
	items, err := s.buildRealtimeUserTrafficStats(ctx, node.ID, mapValue(result["payload"]), usernames)
	if err != nil {
		return nil, err
	}
	response := s.sliceListResponse(items, page, pageSize)
	response["exitCode"] = 0
	response["output"] = ""
	return response, nil
}

func (s *hysteriaService) getCachedAgentUserTrafficStats(ctx context.Context, nodeID int64, usernames []string) ([]map[string]any, error) {
	if s.agents == nil {
		return []map[string]any{}, nil
	}
	agent, err := s.agents.getAgentRecordByNodeID(ctx, nodeID)
	if err != nil || agent == nil {
		return []map[string]any{}, err
	}
	payload := mapValue(agent["last_payload_json"])
	return s.buildRealtimeUserTrafficStats(ctx, nodeID, mapValue(payload["user_traffic"]), usernames)
}

func (s *hysteriaService) buildRealtimeUserTrafficStats(ctx context.Context, nodeID int64, payload map[string]any, usernames []string) ([]map[string]any, error) {
	items := buildUserTrafficStatsItems(nodeID, payload, usernames)
	users, err := s.listUsers(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	usersByUsername := make(map[string]userRecord, len(users))
	for _, user := range users {
		usersByUsername[user.Username] = user
	}
	for _, item := range items {
		user, ok := usersByUsername[toString(item["username"])]
		if !ok {
			continue
		}
		liveUsedBytes := liveUsedBytesForUser(user, item)
		item["live_used_bytes"] = liveUsedBytes
		item["live_used_gb"] = roundBytesToGigabytes(liveUsedBytes, user.UsedGB)
	}
	return items, nil
}

func buildUserTrafficStatsItems(nodeID int64, payload map[string]any, usernames []string) []map[string]any {
	allowed := make(map[string]bool)
	for _, item := range usernames {
		item = strings.TrimSpace(item)
		if item != "" {
			allowed[item] = true
		}
	}
	items := make([]map[string]any, 0, len(payload))
	for username, raw := range payload {
		if len(allowed) > 0 && !allowed[username] {
			continue
		}
		stats, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rx := int64Value(stats["rx"])
		tx := int64Value(stats["tx"])
		items = append(items, map[string]any{
			"node_id":     nodeID,
			"username":    username,
			"rx":          rx,
			"tx":          tx,
			"rx_human":    formatBytes(rx),
			"tx_human":    formatBytes(tx),
			"total_human": formatBytes(rx + tx),
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		left := int64Value(items[i]["rx"]) + int64Value(items[i]["tx"])
		right := int64Value(items[j]["rx"]) + int64Value(items[j]["tx"])
		if left != right {
			return left > right
		}
		return toString(items[i]["username"]) < toString(items[j]["username"])
	})
	return items
}

func (s *hysteriaService) getTrafficOverview(ctx context.Context, hours int, page int, pageSize int) (map[string]any, error) {
	if hours < 1 {
		hours = 1
	}
	if hours > 168 {
		hours = 168
	}
	page, pageSize, offset := resolvePagination(page, pageSize)

	seriesRows, err := s.db.QueryContext(ctx, `
		SELECT recorded_at, SUM(total_rx) AS total_rx, SUM(total_tx) AS total_tx
		FROM node_traffic_history
		WHERE recorded_at >= DATE_SUB(NOW(), INTERVAL ? HOUR)
		GROUP BY recorded_at
		ORDER BY recorded_at ASC`, hours)
	if err != nil {
		return nil, err
	}
	defer seriesRows.Close()

	series := make([]any, 0)
	for seriesRows.Next() {
		var recordedAt time.Time
		var totalRX int64
		var totalTX int64
		if err := seriesRows.Scan(&recordedAt, &totalRX, &totalTX); err != nil {
			return nil, err
		}
		series = append(series, map[string]any{
			"recorded_at": formatTime(recordedAt),
			"total_rx":    totalRX,
			"total_tx":    totalTX,
		})
	}

	total := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM server_nodes WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			n.id, n.name, n.host,
			COALESCE((
				SELECT SUM(h.delta_rx)
				FROM node_traffic_history h
				WHERE h.node_id = n.id
				  AND h.recorded_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
			), 0) AS total_rx,
			COALESCE((
				SELECT SUM(h.delta_tx)
				FROM node_traffic_history h
				WHERE h.node_id = n.id
				  AND h.recorded_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
			), 0) AS total_tx,
			latest.user_count, latest.online_count, latest.recorded_at
		FROM server_nodes n
		LEFT JOIN (
			SELECT h.node_id, h.user_count, h.online_count, h.recorded_at
			FROM node_traffic_history h
			INNER JOIN (
				SELECT node_id, MAX(recorded_at) AS max_recorded_at
				FROM node_traffic_history
				GROUP BY node_id
			) grouped ON grouped.node_id = h.node_id AND grouped.max_recorded_at = h.recorded_at
		) latest ON latest.node_id = n.id
		WHERE n.deleted_at IS NULL
		ORDER BY n.updated_at DESC, n.id DESC
		LIMIT ? OFFSET ?`,
		trafficSummaryWindowDays,
		trafficSummaryWindowDays,
		pageSize,
		offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]any, 0)
	for rows.Next() {
		var (
			id          int64
			name        string
			host        string
			totalRX     sql.NullInt64
			totalTX     sql.NullInt64
			userCount   sql.NullInt64
			onlineCount sql.NullInt64
			recordedAt  sql.NullTime
		)
		if err := rows.Scan(&id, &name, &host, &totalRX, &totalTX, &userCount, &onlineCount, &recordedAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"id":          id,
			"name":        name,
			"host":        host,
			"onlineCount": nullInt64(onlineCount),
			"userCount":   nullInt64(userCount),
			"totalRx":     nullInt64(totalRX),
			"totalTx":     nullInt64(totalTX),
			"recordedAt":  nullTimeValue(recordedAt),
		})
	}

	result := paginatedResult(items, page, pageSize, total)
	result["series"] = series
	return result, nil
}

func (s *hysteriaService) cachedServiceStatus(ctx context.Context, node nodeRecord) map[string]any {
	if node.ManageMode != "agent" || !node.AgentEnabled {
		return map[string]any{
			"command":  "status",
			"output":   "SSH 模式状态检查尚未迁移到 Go 面板",
			"exitCode": 1,
		}
	}

	agent, err := s.getAgentRecordByNodeID(ctx, node.ID)
	if err != nil || agent == nil {
		return map[string]any{
			"command":  "agent:status",
			"output":   "节点尚未部署 Agent，请先执行安装流程",
			"exitCode": 1,
		}
	}

	agentStatus := toString(agent["status"])
	serviceStatus := toString(agent["last_service_status"])
	message := strings.TrimSpace(toString(agent["last_service_message"]))
	if message == "" {
		if agentStatus == agentStatusOnline {
			message = "Agent 已在线，但尚未回传详细状态"
		} else {
			message = "Agent 当前离线或未完成首次心跳"
		}
	} else if agentStatus != agentStatusOnline {
		message = "Agent 当前离线或未完成首次心跳"
	}

	exitCode := 1
	if agentStatus == agentStatusOnline && serviceStatus == "running" {
		exitCode = 0
	}

	return map[string]any{
		"command":  "agent:status",
		"output":   message,
		"exitCode": exitCode,
	}
}

func (s *hysteriaService) getAgentRecordByNodeID(ctx context.Context, nodeID int64) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, status, version, report_interval_seconds, task_poll_interval_seconds, installed_at, last_seen_at,
		       last_ip, last_error, last_service_status, last_service_message, last_total_rx, last_total_tx, last_user_count,
		       last_online_count
		FROM node_agents WHERE node_id = ? LIMIT 1`, nodeID)

	var (
		agentID                 string
		status                  string
		version                 string
		reportIntervalSeconds   int
		taskPollIntervalSeconds int
		installedAt             sql.NullTime
		lastSeenAt              sql.NullTime
		lastIP                  sql.NullString
		lastError               sql.NullString
		lastServiceStatus       sql.NullString
		lastServiceMessage      sql.NullString
		lastTotalRX             int64
		lastTotalTX             int64
		lastUserCount           int64
		lastOnlineCount         int64
	)
	if err := row.Scan(&agentID, &status, &version, &reportIntervalSeconds, &taskPollIntervalSeconds, &installedAt, &lastSeenAt, &lastIP, &lastError, &lastServiceStatus, &lastServiceMessage, &lastTotalRX, &lastTotalTX, &lastUserCount, &lastOnlineCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return map[string]any{
		"agent_id":                   agentID,
		"status":                     status,
		"version":                    version,
		"report_interval_seconds":    reportIntervalSeconds,
		"task_poll_interval_seconds": taskPollIntervalSeconds,
		"installed_at":               nullTimeValue(installedAt),
		"last_seen_at":               nullTimeValue(lastSeenAt),
		"last_ip":                    nullStringValue(lastIP),
		"last_error":                 nullStringValue(lastError),
		"last_service_status":        nullStringValue(lastServiceStatus),
		"last_service_message":       nullStringValue(lastServiceMessage),
		"last_total_rx":              lastTotalRX,
		"last_total_tx":              lastTotalTX,
		"last_user_count":            lastUserCount,
		"last_online_count":          lastOnlineCount,
	}, nil
}

func (s *hysteriaService) getUserMetricsForNode(ctx context.Context, nodeID int64) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
                       COALESCE(SUM(quota_gb), 0),
                       COALESCE(SUM(used_bytes), 0)
		FROM hysteria_users WHERE node_id = ?`, nodeID)
	var userCount int
	var activeUserCount int
	var quotaTotalGB int64
	var quotaUsedBytes int64
	if err := row.Scan(&userCount, &activeUserCount, &quotaTotalGB, &quotaUsedBytes); err != nil {
		return nil, err
	}

	return map[string]any{
		"userCount":       userCount,
		"activeUserCount": activeUserCount,
		"quotaTotalGb":    quotaTotalGB,
		"quotaUsedGb":     roundBytesToGigabytes(quotaUsedBytes, 0),
	}, nil
}

func (s *hysteriaService) getStoredNode(ctx context.Context, id int64) (*nodeRecord, error) {
	query := `
		SELECT id, current_node, deploy_mode, name, host, ssh_port, ssh_username, ssh_auth_type, ssh_password, ssh_private_key_path,
		       sudo_password, install_script, service_name, config_path, listen_port, traffic_stats_listen, traffic_stats_secret,
		       tls_mode, tls_cert_path, tls_key_path, domain, acme_email, obfs_password, masquerade_url, bandwidth_up_mbps,
		       bandwidth_down_mbps, manage_mode, agent_enabled, agent_report_interval_seconds, agent_task_poll_interval_seconds,
		       agent_install_path, agent_config_path, agent_service_name, deleted_at, created_at, updated_at
		FROM server_nodes`
	args := []any{}
	if id > 0 {
		query += ` WHERE id = ? LIMIT 1`
		args = append(args, id)
	} else {
		query += ` WHERE deleted_at IS NULL ORDER BY updated_at DESC, id DESC LIMIT 1`
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	record, err := scanNodeRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := s.decryptStoredNodeSecrets(&record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *hysteriaService) listStoredNodes(ctx context.Context) ([]nodeRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, current_node, deploy_mode, name, host, ssh_port, ssh_username, ssh_auth_type, ssh_password, ssh_private_key_path,
		       sudo_password, install_script, service_name, config_path, listen_port, traffic_stats_listen, traffic_stats_secret,
		       tls_mode, tls_cert_path, tls_key_path, domain, acme_email, obfs_password, masquerade_url, bandwidth_up_mbps,
		       bandwidth_down_mbps, manage_mode, agent_enabled, agent_report_interval_seconds, agent_task_poll_interval_seconds,
	       agent_install_path, agent_config_path, agent_service_name, deleted_at, created_at, updated_at
		FROM server_nodes
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]nodeRecord, 0)
	for rows.Next() {
		record, err := scanNodeRecord(rows)
		if err != nil {
			return nil, err
		}
		if err := s.decryptStoredNodeSecrets(&record); err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *hysteriaService) getNodeOrCurrent(ctx context.Context, nodeID int64) (*nodeRecord, error) {
	node, err := s.getStoredNode(ctx, nodeID)
	if err != nil || node == nil {
		return node, err
	}
	if node.DeletedAt.Valid {
		return nil, nil
	}
	return node, nil
}

func (s *hysteriaService) decryptStoredNodeSecrets(record *nodeRecord) error {
	if record == nil || s == nil || s.cfg == nil {
		return nil
	}
	var err error
	if record.SSHPassword, err = DecryptValue(record.SSHPassword, *s.cfg); err != nil {
		return err
	}
	if record.SudoPassword, err = DecryptValue(record.SudoPassword, *s.cfg); err != nil {
		return err
	}
	if record.ObfsPassword, err = DecryptValue(record.ObfsPassword, *s.cfg); err != nil {
		return err
	}
	if record.TrafficStatsSecret, err = DecryptValue(record.TrafficStatsSecret, *s.cfg); err != nil {
		return err
	}
	return nil
}

func (s *hysteriaService) getStoredUser(ctx context.Context, id int64) (*userRecord, error) {
	row := s.db.QueryRowContext(ctx, `
                SELECT id, public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at, created_at, updated_at
		FROM hysteria_users WHERE id = ? LIMIT 1`, id)
	record, err := scanUserRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *hysteriaService) getStoredUserByPublicID(ctx context.Context, publicID string) (*userRecord, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
                SELECT id, public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at, created_at, updated_at
		FROM hysteria_users WHERE public_id = ? LIMIT 1`, publicID)
	record, err := scanUserRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *hysteriaService) presentNode(ctx context.Context, record nodeRecord, canViewSensitive bool) (map[string]any, error) {
	item, err := s.decryptNode(record)
	if err != nil {
		return nil, err
	}

	keyMetadata := map[string]any{
		"uploaded":         false,
		"private_key_name": nil,
		"public_key_name":  nil,
	}
	if s.sshKeys != nil {
		keyMetadata = s.sshKeys.describe(item["ssh_private_key_path"].(string))
	}

	item["ssh_password"] = ""
	item["ssh_private_key_path"] = ""
	item["ssh_private_key_uploaded"] = keyMetadata["uploaded"]
	item["ssh_private_key_name"] = keyMetadata["private_key_name"]
	item["ssh_public_key_name"] = keyMetadata["public_key_name"]
	item["sudo_password"] = ""
	item["traffic_stats_secret"] = ""
	if !canViewSensitive {
		item["obfs_password"] = ""
	}

	agent, err := s.getAgentRecordByNodeID(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if s.agents != nil {
		item["agent"] = s.agents.presentAgent(agent)
	} else {
		item["agent"] = agent
	}
	return item, nil
}

func (s *hysteriaService) presentUser(record userRecord, canViewSensitive bool) (map[string]any, error) {
	item, err := s.decryptUser(record)
	if err != nil {
		return nil, err
	}
	if !canViewSensitive {
		item.AuthPassword = ""
	}
	return map[string]any{
		"id":               item.ID,
		"public_id":        item.PublicID,
		"node_id":          item.NodeID,
		"username":         item.Username,
		"auth_password":    item.AuthPassword,
		"status":           item.Status,
		"quota_gb":         item.QuotaGB,
		"used_gb":          roundBytesToGigabytes(item.UsedBytes, item.UsedGB),
		"speed_limit_mbps": item.SpeedLimitMbps,
		"expires_at":       nullTimeValue(item.ExpiresAt),
		"created_at":       formatTime(item.CreatedAt),
		"updated_at":       formatTime(item.UpdatedAt),
	}, nil
}

func (s *hysteriaService) decryptNode(record nodeRecord) (map[string]any, error) {
	sshPassword, err := DecryptValue(record.SSHPassword, *s.cfg)
	if err != nil {
		return nil, err
	}
	sudoPassword, err := DecryptValue(record.SudoPassword, *s.cfg)
	if err != nil {
		return nil, err
	}
	obfsPassword, err := DecryptValue(record.ObfsPassword, *s.cfg)
	if err != nil {
		return nil, err
	}
	trafficSecret, err := DecryptValue(record.TrafficStatsSecret, *s.cfg)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id":                               record.ID,
		"current_node":                     0,
		"deploy_mode":                      normalizeDeployMode(record.DeployMode),
		"name":                             record.Name,
		"host":                             record.Host,
		"ssh_port":                         record.SSHPort,
		"ssh_username":                     record.SSHUsername,
		"ssh_auth_type":                    normalizeSSHAuthType(record.SSHAuthType),
		"ssh_password":                     sshPassword,
		"ssh_private_key_path":             record.SSHPrivateKeyPath,
		"sudo_password":                    sudoPassword,
		"install_script":                   defaultString(record.InstallScript, "bash <(curl -fsSL https://get.hy2.sh/)"),
		"service_name":                     defaultString(record.ServiceName, "hysteria-server"),
		"config_path":                      defaultString(record.ConfigPath, "/etc/hysteria/config.yaml"),
		"listen_port":                      record.ListenPort,
		"traffic_stats_listen":             defaultString(record.TrafficStatsListen, "127.0.0.1:9999"),
		"traffic_stats_secret":             trafficSecret,
		"tls_mode":                         normalizeTLSMode(record.TLSMode),
		"tls_cert_path":                    defaultString(record.TLSCertPath, "/etc/hysteria/server.crt"),
		"tls_key_path":                     defaultString(record.TLSKeyPath, "/etc/hysteria/server.key"),
		"domain":                           record.Domain,
		"acme_email":                       record.ACMEEmail,
		"obfs_password":                    obfsPassword,
		"masquerade_url":                   defaultString(record.MasqueradeURL, "https://www.cloudflare.com"),
		"bandwidth_up_mbps":                record.BandwidthUpMbps,
		"bandwidth_down_mbps":              record.BandwidthDownMbps,
		"manage_mode":                      normalizeManageMode(record.ManageMode),
		"agent_enabled":                    record.AgentEnabled,
		"agent_report_interval_seconds":    maxInt(record.AgentReportIntervalSeconds, 1),
		"agent_task_poll_interval_seconds": maxInt(record.AgentTaskPollIntervalSeconds, 1),
		"agent_install_path":               defaultString(record.AgentInstallPath, "/usr/local/bin/mxinhy-agent"),
		"agent_config_path":                defaultString(record.AgentConfigPath, "/etc/mxinhy-agent.json"),
		"agent_service_name":               defaultString(record.AgentServiceName, "mxinhy-agent"),
		"created_at":                       formatTime(record.CreatedAt),
		"updated_at":                       formatTime(record.UpdatedAt),
	}, nil
}

func (s *hysteriaService) decryptUser(record userRecord) (userRecord, error) {
	authPassword, err := DecryptValue(record.AuthPassword, *s.cfg)
	if err != nil {
		return userRecord{}, err
	}
	record.AuthPassword = authPassword
	record.Status = normalizeUserStatus(record.Status)
	return record, nil
}

func (s *hysteriaService) encryptNodeSecrets(data map[string]any) (map[string]any, error) {
	result := cloneMap(data)
	for _, field := range []string{"ssh_password", "sudo_password", "obfs_password", "traffic_stats_secret"} {
		encrypted, err := EncryptValue(toString(data[field]), *s.cfg)
		if err != nil {
			return nil, err
		}
		result[field] = encrypted
	}
	return result, nil
}

func (s *hysteriaService) normalizeNodePayload(payload map[string]any, current *nodeRecord) (map[string]any, error) {
	currentMap := map[string]any{}
	if current != nil {
		var err error
		currentMap, err = s.decryptNode(*current)
		if err != nil {
			return nil, err
		}
	}

	data := map[string]any{
		"name":                             defaultString(toTrimmedString(payload["name"]), defaultString(toString(currentMap["name"]), "default-node")),
		"deploy_mode":                      normalizeDeployMode(defaultString(toTrimmedString(payload["deploy_mode"]), defaultString(toString(currentMap["deploy_mode"]), "ssh"))),
		"host":                             toTrimmedString(payload["host"]),
		"ssh_port":                         boundedInt(payload["ssh_port"], 1, 65535, maxInt(intValue(currentMap["ssh_port"]), 22)),
		"ssh_username":                     defaultString(toTrimmedString(payload["ssh_username"]), defaultString(toString(currentMap["ssh_username"]), "root")),
		"ssh_auth_type":                    normalizeSSHAuthType(defaultString(toTrimmedString(payload["ssh_auth_type"]), defaultString(toString(currentMap["ssh_auth_type"]), "password"))),
		"ssh_password":                     toString(payload["ssh_password"]),
		"ssh_private_key_path":             toTrimmedString(payload["ssh_private_key_path"]),
		"sudo_password":                    toString(payload["sudo_password"]),
		"install_script":                   defaultString(toTrimmedString(payload["install_script"]), "bash <(curl -fsSL https://get.hy2.sh/)"),
		"service_name":                     defaultString(toTrimmedString(payload["service_name"]), "hysteria-server"),
		"config_path":                      defaultString(toTrimmedString(payload["config_path"]), "/etc/hysteria/config.yaml"),
		"listen_port":                      boundedInt(payload["listen_port"], 1, 65535, maxInt(intValue(currentMap["listen_port"]), 443)),
		"traffic_stats_listen":             defaultString(toTrimmedString(payload["traffic_stats_listen"]), "127.0.0.1:9999"),
		"traffic_stats_secret":             toTrimmedString(payload["traffic_stats_secret"]),
		"tls_mode":                         normalizeTLSMode(defaultString(toTrimmedString(payload["tls_mode"]), defaultString(toString(currentMap["tls_mode"]), "acme"))),
		"tls_cert_path":                    defaultString(toTrimmedString(payload["tls_cert_path"]), "/etc/hysteria/server.crt"),
		"tls_key_path":                     defaultString(toTrimmedString(payload["tls_key_path"]), "/etc/hysteria/server.key"),
		"domain":                           toTrimmedString(payload["domain"]),
		"acme_email":                       toTrimmedString(payload["acme_email"]),
		"obfs_password":                    toTrimmedString(payload["obfs_password"]),
		"masquerade_url":                   defaultString(toTrimmedString(payload["masquerade_url"]), "https://www.bing.com"),
		"bandwidth_up_mbps":                boundedInt(payload["bandwidth_up_mbps"], 0, 100000, maxInt(intValue(currentMap["bandwidth_up_mbps"]), 200)),
		"bandwidth_down_mbps":              boundedInt(payload["bandwidth_down_mbps"], 0, 100000, maxInt(intValue(currentMap["bandwidth_down_mbps"]), 200)),
		"manage_mode":                      normalizeManageMode(defaultString(toTrimmedString(payload["manage_mode"]), defaultString(toString(currentMap["manage_mode"]), "agent"))),
		"agent_enabled":                    toBool(payload["agent_enabled"], boolValue(currentMap["agent_enabled"], true)),
		"agent_report_interval_seconds":    boundedInt(payload["agent_report_interval_seconds"], 1, 86400, maxInt(intValue(currentMap["agent_report_interval_seconds"]), 2)),
		"agent_task_poll_interval_seconds": boundedInt(payload["agent_task_poll_interval_seconds"], 1, 86400, maxInt(intValue(currentMap["agent_task_poll_interval_seconds"]), 1)),
		"agent_install_path":               defaultString(toTrimmedString(payload["agent_install_path"]), defaultString(toString(currentMap["agent_install_path"]), "/usr/local/bin/mxinhy-agent")),
		"agent_config_path":                defaultString(toTrimmedString(payload["agent_config_path"]), defaultString(toString(currentMap["agent_config_path"]), "/etc/mxinhy-agent.json")),
		"agent_service_name":               defaultString(toTrimmedString(payload["agent_service_name"]), defaultString(toString(currentMap["agent_service_name"]), "mxinhy-agent")),
	}

	if current != nil {
		if data["ssh_password"] == "" {
			data["ssh_password"] = toString(currentMap["ssh_password"])
		}
		if data["sudo_password"] == "" {
			data["sudo_password"] = toString(currentMap["sudo_password"])
		}
		if data["traffic_stats_secret"] == "" {
			data["traffic_stats_secret"] = toString(currentMap["traffic_stats_secret"])
		}
	}

	if data["deploy_mode"] == "local" {
		data["host"] = defaultString(toTrimmedString(payload["host"]), "127.0.0.1")
		data["ssh_port"] = 22
		data["ssh_username"] = defaultString(toTrimmedString(payload["ssh_username"]), "root")
		data["ssh_auth_type"] = "password"
		data["ssh_password"] = ""
		data["ssh_private_key_path"] = ""
	} else if data["ssh_auth_type"] == "key" {
		if token := toTrimmedString(payload["ssh_private_key_token"]); token != "" {
			if s.sshKeys == nil {
				return nil, errors.New("SSH 密钥服务不可用")
			}
			path, err := s.sshKeys.commit(token, toString(currentMap["ssh_private_key_path"]))
			if err != nil {
				return nil, err
			}
			data["ssh_private_key_path"] = path
		} else if current != nil && toString(currentMap["ssh_private_key_path"]) != "" {
			data["ssh_private_key_path"] = toString(currentMap["ssh_private_key_path"])
		} else if toTrimmedString(payload["ssh_private_key_path"]) != "" {
			return nil, errors.New("请通过上传方式提供 SSH 私钥和公钥文件")
		} else {
			return nil, errors.New("请选择 SSH 私钥和公钥文件后再保存节点")
		}
		data["ssh_password"] = ""
	} else {
		data["ssh_private_key_path"] = ""
	}

	for _, required := range []string{"host", "ssh_username", "obfs_password"} {
		if strings.TrimSpace(toString(data[required])) == "" {
			return nil, errors.New(required + " 不能为空")
		}
	}
	for _, required := range []string{"agent_install_path", "agent_config_path", "agent_service_name"} {
		if strings.TrimSpace(toString(data[required])) == "" {
			return nil, errors.New(required + " 不能为空")
		}
	}
	if data["tls_mode"] == "acme" {
		for _, required := range []string{"domain", "acme_email"} {
			if strings.TrimSpace(toString(data[required])) == "" {
				return nil, errors.New(required + " 不能为空")
			}
		}
	} else {
		for _, required := range []string{"tls_cert_path", "tls_key_path"} {
			if strings.TrimSpace(toString(data[required])) == "" {
				return nil, errors.New(required + " 不能为空")
			}
		}
	}
	return data, nil
}

func (s *hysteriaService) normalizeUserPayload(payload map[string]any, current *userRecord) (map[string]any, error) {
	currentValue := userRecord{}
	if current != nil {
		currentValue = *current
	}

	username := defaultString(toTrimmedString(payload["username"]), currentValue.Username)
	authPassword := strings.TrimSpace(toString(payload["auth_password"]))
	if authPassword == "" {
		authPassword = currentValue.AuthPassword
	}
	if username == "" || authPassword == "" {
		return nil, errors.New("用户名和认证密码不能为空")
	}

	status := normalizeUserStatus(defaultString(toTrimmedString(payload["status"]), currentValue.Status))
	quotaGB := int64(boundedInt64(payload["quota_gb"], 0, 1<<31-1, currentValue.QuotaGB))
	usedGB := boundedFloat64(payload["used_gb"], 0, 1<<31-1, roundBytesToGigabytes(currentValue.UsedBytes, currentValue.UsedGB))
	speedLimit := boundedInt(payload["speed_limit_mbps"], 0, 100000, currentValue.SpeedLimitMbps)
	nodeID := int64(boundedInt64(payload["node_id"], 1, 1<<31-1, currentValue.NodeID))
	if nodeID <= 0 {
		return nil, errors.New("请选择节点")
	}
	expiresAt, err := normalizeNullableDateTime(payload["expires_at"], currentValue.ExpiresAt)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"node_id":          nodeID,
		"username":         username,
		"auth_password":    authPassword,
		"status":           status,
		"quota_gb":         quotaGB,
		"used_gb":          usedGB,
		"speed_limit_mbps": speedLimit,
		"expires_at":       expiresAt,
	}, nil
}

func (s *hysteriaService) assertUniqueUserCredential(ctx context.Context, nodeID int64, authPassword string, ignoreUserID int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, auth_password FROM hysteria_users WHERE node_id = ?`, nodeID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var encrypted string
		if err := rows.Scan(&id, &encrypted); err != nil {
			return err
		}
		if ignoreUserID > 0 && id == ignoreUserID {
			continue
		}
		plain, err := DecryptValue(encrypted, *s.cfg)
		if err != nil {
			return err
		}
		if plain == authPassword {
			return errors.New("该节点已存在相同认证密码，请更换")
		}
	}
	return nil
}

func (s *hysteriaService) assertNodeIdentityAvailable(ctx context.Context, data map[string]any, ignoreNodeID int64) error {
	query := `SELECT id FROM server_nodes WHERE deleted_at IS NULL AND deploy_mode = ? AND host = ? AND ssh_port = ? AND ssh_username = ? AND listen_port = ?`
	args := []any{data["deploy_mode"], data["host"], data["ssh_port"], data["ssh_username"], data["listen_port"]}
	if ignoreNodeID > 0 {
		query += ` AND id <> ?`
		args = append(args, ignoreNodeID)
	}
	query += ` LIMIT 1`

	var existingID int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&existingID)
	if err == nil {
		return errors.New("已存在相同节点，请勿重复提交")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (s *hysteriaService) cleanupReplacedManagedNodeKey(currentPath string, updatedPath string, authType string) error {
	if s.sshKeys == nil || strings.TrimSpace(currentPath) == "" {
		return nil
	}
	if authType != "key" {
		s.sshKeys.deleteManagedKey(currentPath, "")
		return nil
	}
	if updatedPath != "" && updatedPath != currentPath {
		s.sshKeys.deleteManagedKey(currentPath, updatedPath)
	}
	return nil
}

type subscriptionEntry struct {
	Node nodeRecord
	User userRecord
}

func (s *hysteriaService) ensureSubscriptionUsersOnAvailableNodes(ctx context.Context, template userRecord) error {
	if !isUserSubscribable(template) {
		return nil
	}

	nodes, err := s.listStoredNodes(ctx)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if !s.isNodeAvailableForSubscription(ctx, node) {
			continue
		}
		existing, err := s.findStoredUserByNodeAndUsername(ctx, node.ID, template.Username)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}

		encryptedPassword, err := EncryptValue(template.AuthPassword, *s.cfg)
		if err != nil {
			return errors.New("用户认证密码加密失败")
		}
		if _, err := s.db.ExecContext(ctx, `
                        INSERT INTO hysteria_users (public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at)
                        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.generateUserPublicID(ctx), node.ID, template.Username, encryptedPassword, template.Status, template.QuotaGB, template.UsedGB, template.UsedBytes, template.SpeedLimitMbps, template.ExpiresAt); err != nil {
			return normalizeDBError(err, "订阅用户同步失败")
		}
		s.queueLocalAuthRefresh(ctx, node, nil)
	}

	return nil
}

func (s *hysteriaService) resolveSubscriptionEntries(ctx context.Context, username string) ([]subscriptionEntry, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
                SELECT id, public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at, created_at, updated_at
		FROM hysteria_users WHERE username = ? ORDER BY id DESC`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]subscriptionEntry, 0)
	for rows.Next() {
		user, err := scanUserRecord(rows)
		if err != nil {
			return nil, err
		}
		decrypted, err := s.decryptUser(user)
		if err != nil {
			return nil, err
		}
		if !isUserSubscribable(decrypted) {
			continue
		}

		node, err := s.getStoredNode(ctx, decrypted.NodeID)
		if err != nil || node == nil {
			continue
		}
		if !s.isNodeAvailableForSubscription(ctx, *node) {
			continue
		}
		entries = append(entries, subscriptionEntry{Node: *node, User: decrypted})
	}

	sort.Slice(entries, func(i int, j int) bool {
		if entries[i].Node.CurrentNode != entries[j].Node.CurrentNode {
			return entries[i].Node.CurrentNode > entries[j].Node.CurrentNode
		}
		return entries[i].Node.Name < entries[j].Node.Name
	})
	return entries, nil
}

func (s *hysteriaService) buildUserSubscriptionURL(user userRecord, apiBaseURL string) string {
	baseURL := normalizePublicSiteBaseURL(apiBaseURL)
	if baseURL == "" && s.cfg != nil {
		baseURL = normalizePublicSiteBaseURL(s.cfg.PublicAPIBaseURL)
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1"
	}
	return strings.TrimRight(baseURL, "/") + fmt.Sprintf("/subscription/%s?token=%s", url.PathEscape(user.PublicID), url.QueryEscape(s.buildUserSubscriptionToken(user)))
}

func (s *hysteriaService) buildUserSubscriptionToken(user userRecord) string {
	secret := ""
	if s.cfg != nil {
		secret = s.cfg.EncryptionKey
	}
	return signCookiePayload([]byte(fmt.Sprintf("subscription|%s|%s|%s|%s", user.PublicID, user.Username, user.AuthPassword, formatTime(user.UpdatedAt))), secret)
}

func (s *hysteriaService) generateUserPublicID(ctx context.Context) string {
	for attempt := 0; attempt < 8; attempt++ {
		hexValue, err := randomHex(10)
		if err != nil {
			continue
		}
		value := "usr_" + hexValue
		var exists int
		err = s.db.QueryRowContext(ctx, `SELECT 1 FROM hysteria_users WHERE public_id = ? LIMIT 1`, value).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return value
		}
	}
	return "usr_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (s *hysteriaService) canViewSensitiveFields(user *authUser) bool {
	return user == nil || userCan(user, "node.manage") || userCan(user, "user.manage") || userCan(user, "admin.manage")
}

func (s *hysteriaService) shouldUseAgent(node nodeRecord) bool {
	return normalizeManageMode(node.ManageMode) == "agent" && node.AgentEnabled
}

func (s *hysteriaService) usesLocalNodeAuth(node nodeRecord) bool {
	return s.shouldUseAgent(node)
}

func (s *hysteriaService) buildLocalAgentAuthURL() string {
	return "http://127.0.0.1:18081/auth"
}

func (s *hysteriaService) isNodeAvailableForSubscription(ctx context.Context, node nodeRecord) bool {
	if !s.usesLocalNodeAuth(node) {
		return true
	}
	if s.agents == nil {
		return false
	}
	agent, err := s.agents.getAgentRecordByNodeID(ctx, node.ID)
	if err != nil || agent == nil {
		return false
	}
	return s.agents.isRecordOnline(agent)
}

func (s *hysteriaService) queueLocalAuthRefresh(ctx context.Context, node nodeRecord, kickClients []string) {
	if !s.usesLocalNodeAuth(node) || s.agents == nil {
		return
	}
	payload, err := s.buildLocalAuthTaskPayload(ctx, node)
	if err == nil {
		_, _ = s.agents.enqueueTask(ctx, node.ID, opcodeSyncLocalAuth, payload, 0, 300)
	}
	kickClients = uniqueTrimmedStrings(kickClients)
	if len(kickClients) == 0 {
		return
	}
	_, _ = s.agents.enqueueTask(ctx, node.ID, opcodeKickClients, map[string]any{"clients": kickClients}, 0, 180)
}

func (s *hysteriaService) enforceRealtimeTrafficQuota(ctx context.Context, nodeID int64, payload map[string]any) error {
	if s == nil || s.db == nil || len(payload) == 0 {
		return nil
	}
	node, err := s.getStoredNode(ctx, nodeID)
	if err != nil || node == nil {
		return err
	}
	suspendedUsers := make([]string, 0)
	for username, raw := range payload {
		stats, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		user, err := s.findStoredUserByNodeAndUsername(ctx, nodeID, username)
		if err != nil || user == nil {
			continue
		}
		if normalizeUserStatus(user.Status) != "active" || user.QuotaGB <= 0 {
			continue
		}
		if liveUsedBytesForUser(*user, stats) < gigabytesToBytes(user.QuotaGB) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE hysteria_users SET status = 'suspended' WHERE id = ?`, user.ID); err == nil {
			suspendedUsers = append(suspendedUsers, username)
		}
	}
	suspendedUsers = uniqueTrimmedStrings(suspendedUsers)
	if len(suspendedUsers) > 0 {
		s.queueLocalAuthRefresh(ctx, *node, suspendedUsers)
	}
	return nil
}

func (s *hysteriaService) listUsers(ctx context.Context, nodeID int64) ([]userRecord, error) {
	node, err := s.getStoredNode(ctx, nodeID)
	if err != nil || node == nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
                SELECT id, public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at, created_at, updated_at
		FROM hysteria_users WHERE node_id = ? ORDER BY id DESC`, node.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userRecord, 0)
	for rows.Next() {
		record, err := scanUserRecord(rows)
		if err != nil {
			return nil, err
		}
		record, err = s.decryptUser(record)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, nil
}

func (s *hysteriaService) listStoredUsersByUsername(ctx context.Context, username string) ([]userRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT id, public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at, created_at, updated_at
                FROM hysteria_users WHERE username = ? ORDER BY id DESC`, strings.TrimSpace(username))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userRecord, 0)
	for rows.Next() {
		record, err := scanUserRecord(rows)
		if err != nil {
			return nil, err
		}
		record, err = s.decryptUser(record)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *hysteriaService) findStoredUserByNodeAndUsername(ctx context.Context, nodeID int64, username string) (*userRecord, error) {
	row := s.db.QueryRowContext(ctx, `
                SELECT id, public_id, node_id, username, auth_password, status, quota_gb, used_gb, used_bytes, speed_limit_mbps, expires_at, created_at, updated_at
		FROM hysteria_users WHERE node_id = ? AND username = ? LIMIT 1`, nodeID, username)
	record, err := scanUserRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *hysteriaService) buildAuthBackendURL(node nodeRecord, apiBaseURL string) string {
	if s.usesLocalNodeAuth(node) {
		return s.buildLocalAgentAuthURL()
	}
	baseURL := normalizePublicAPIBaseURL(apiBaseURL)
	if baseURL == "" && s.cfg != nil {
		baseURL = normalizePublicAPIBaseURL(s.cfg.PublicAPIBaseURL)
	}
	if baseURL == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + fmt.Sprintf("/hysteria/auth/%d?token=%s", node.ID, url.QueryEscape(s.buildNodeAuthToken(node)))
}

func (s *hysteriaService) validateNodeAuthToken(ctx context.Context, nodeID int64, token string) bool {
	node, err := s.getStoredNode(ctx, nodeID)
	if err != nil || node == nil {
		return false
	}
	return s.buildNodeAuthToken(*node) == strings.TrimSpace(token)
}

func (s *hysteriaService) buildNodeAuthToken(node nodeRecord) string {
	secret := ""
	if s.cfg != nil {
		secret = s.cfg.EncryptionKey
	}
	return signCookiePayload([]byte(fmt.Sprintf("node-auth|%d|%s", node.ID, node.ObfsPassword)), secret)
}

func (s *hysteriaService) authorizeClient(ctx context.Context, nodeID int64, credential string) (string, error) {
	users, err := s.listUsers(ctx, nodeID)
	if err != nil {
		return "", err
	}
	for _, user := range users {
		if user.AuthPassword != credential {
			continue
		}
		if normalizeUserStatus(user.Status) != "active" {
			return "", errors.New("账号已停用")
		}
		if user.ExpiresAt.Valid && !user.ExpiresAt.Time.After(time.Now()) {
			return "", errors.New("账号已过期")
		}
		if user.QuotaGB > 0 && user.UsedBytes >= gigabytesToBytes(user.QuotaGB) {
			return "", errors.New("流量已用尽")
		}
		return user.Username, nil
	}
	return "", errors.New("认证失败")
}

func (s *hysteriaService) buildLocalAuthTaskPayload(ctx context.Context, node nodeRecord) (map[string]any, error) {
	users, err := s.listUsers(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(users))
	for _, user := range users {
		expiresAt := ""
		if user.ExpiresAt.Valid {
			expiresAt = formatTime(user.ExpiresAt.Time)
		}
		items = append(items, map[string]any{
			"username":     user.Username,
			"credential":   user.AuthPassword,
			"status":       normalizeUserStatus(user.Status),
			"quota_gb":     user.QuotaGB,
			"used_gb":      roundBytesToGigabytes(user.UsedBytes, user.UsedGB),
			"speed_limit":  user.SpeedLimitMbps,
			"expires_at":   expiresAt,
			"subscribable": isUserSubscribable(user),
			"updated_at":   formatTime(user.UpdatedAt),
		})
	}
	return map[string]any{"users": items}, nil
}

func (s *hysteriaService) install(ctx context.Context, apiBaseURL string, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存 SSH 和 Hysteria2 配置")
	}
	preflightOutput := ""
	dependencyResult, err := s.ensureNodeInstallDependencies(*node)
	if err != nil {
		return nil, err
	}
	if dependencyResult != nil {
		if intValue(dependencyResult["exitCode"]) != 0 {
			dependencyResult["output"] = s.humanizeNodeInstallOutput(toString(dependencyResult["output"]))
			return dependencyResult, nil
		}
		preflightOutput = strings.TrimSpace(toString(dependencyResult["output"]))
	}
	installResult, err := s.remote.run(*node, node.InstallScript, "")
	if err != nil {
		return nil, err
	}
	if intValue(installResult["exitCode"]) != 0 {
		installResult["output"] = s.humanizeNodeInstallOutput(joinInstallOutputs(preflightOutput, toString(installResult["output"])))
		return installResult, nil
	}
	tlsResult, err := s.prepareTLSAssets(ctx, *node)
	if err != nil {
		return nil, err
	}
	if tlsResult != nil && intValue(tlsResult["exitCode"]) != 0 {
		tlsResult["output"] = s.humanizeNodeInstallOutput(joinInstallOutputs(preflightOutput, toString(tlsResult["output"])))
		return tlsResult, nil
	}
	deployResult, err := s.deployConfig(ctx, apiBaseURL, node.ID)
	if err != nil {
		return nil, err
	}
	if intValue(deployResult["exitCode"]) != 0 {
		deployResult["output"] = s.humanizeNodeInstallOutput(joinInstallOutputs(preflightOutput, toString(deployResult["output"])))
		return deployResult, nil
	}
	restartResult, err := s.runSSHServiceAction(*node, "restart")
	if err != nil {
		return nil, err
	}
	if intValue(restartResult["exitCode"]) != 0 {
		restartResult["output"] = s.humanizeNodeInstallOutput(joinInstallOutputs(preflightOutput, toString(restartResult["output"])))
		return restartResult, nil
	}
	if s.shouldUseAgent(*node) {
		agentResult, err := s.deployAgentBootstrap(ctx, *node, apiBaseURL)
		if err != nil {
			return nil, err
		}
		if intValue(agentResult["exitCode"]) != 0 {
			agentResult["output"] = s.humanizeNodeInstallOutput(joinInstallOutputs(preflightOutput, toString(agentResult["output"])))
			return agentResult, nil
		}
		return map[string]any{
			"command":  "ssh/bootstrap-agent",
			"output":   joinInstallOutputs(preflightOutput, toString(restartResult["output"]), toString(agentResult["output"])),
			"exitCode": 0,
		}, nil
	}
	restartResult["output"] = joinInstallOutputs(preflightOutput, toString(restartResult["output"]))
	return restartResult, nil
}

func (s *hysteriaService) ensureNodeInstallDependencies(node nodeRecord) (map[string]any, error) {
	packages := s.requiredNodeInstallPackages(node)
	if len(packages) == 0 {
		return nil, nil
	}
	command := s.buildNodeDependencyInstallCommand(node, packages)
	return s.remote.run(node, command, "ssh/ensure-node-deps")
}

func (s *hysteriaService) requiredNodeInstallPackages(node nodeRecord) []string {
	packages := []string{"curl"}
	if normalizeTLSMode(node.TLSMode) == "self_signed" {
		packages = append(packages, "openssl")
	}
	unique := make(map[string]struct{}, len(packages))
	result := make([]string, 0, len(packages))
	for _, item := range packages {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, exists := unique[name]; exists {
			continue
		}
		unique[name] = struct{}{}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (s *hysteriaService) buildNodeDependencyInstallCommand(node nodeRecord, packages []string) string {
	missingChecks := make([]string, 0, len(packages))
	for _, pkg := range packages {
		missingChecks = append(missingChecks, fmt.Sprintf(`if ! command -v %s >/dev/null 2>&1; then missing_packages="$missing_packages %s"; fi`, pkg, pkg))
	}
	packageList := strings.Join(packages, " ")
	installScript := strings.Join([]string{
		"set -e",
		`missing_packages=""`,
		strings.Join(missingChecks, "\n"),
		`missing_packages="$(printf '%s\n' "$missing_packages" | xargs)"`,
		`if [ -z "$missing_packages" ]; then exit 0; fi`,
		`if command -v apt-get >/dev/null 2>&1; then`,
		`  apt-get update >/dev/null && DEBIAN_FRONTEND=noninteractive apt-get install -y $missing_packages >/dev/null`,
		`elif command -v dnf >/dev/null 2>&1; then`,
		`  dnf install -y $missing_packages >/dev/null`,
		`elif command -v yum >/dev/null 2>&1; then`,
		`  yum install -y $missing_packages >/dev/null`,
		`elif command -v apk >/dev/null 2>&1; then`,
		`  apk add --no-cache $missing_packages >/dev/null`,
		`elif command -v zypper >/dev/null 2>&1; then`,
		`  zypper --non-interactive install $missing_packages >/dev/null`,
		`elif command -v pacman >/dev/null 2>&1; then`,
		`  pacman -Sy --noconfirm $missing_packages >/dev/null`,
		`else`,
		`  echo "该节点系统暂不支持自动安装依赖，请先手动安装以下依赖后再重试: ` + packageList + `" >&2`,
		`  exit 1`,
		`fi`,
		`echo "已自动安装节点依赖: $missing_packages"`,
	}, "\n")
	return buildPrivilegedRemoteCommand(node, installScript, "当前 SSH 账号缺少可用的提权能力，无法自动安装节点依赖。请改用 root 账号登录，或为当前账号配置 sudo 权限/填写 sudo 密码后重试")
}

func buildPrivilegedRemoteCommand(node nodeRecord, script string, privilegeHint string) string {
	trimmedScript := strings.TrimSpace(script)
	if trimmedScript == "" {
		return ""
	}
	hint := defaultString(strings.TrimSpace(privilegeHint), "当前 SSH 账号缺少可用的提权能力，无法执行所需操作")
	if strings.TrimSpace(node.SudoPassword) != "" {
		return fmt.Sprintf(
			`if [ "$(id -u)" -eq 0 ]; then bash -lc %s; elif command -v sudo >/dev/null 2>&1; then printf '%%s\n' %s | sudo -S -p '' bash -lc %s || { echo %s >&2; exit 1; }; else echo %s >&2; exit 1; fi`,
			shellQuote(trimmedScript),
			shellQuote(node.SudoPassword),
			shellQuote(trimmedScript),
			shellQuote(hint),
			shellQuote(hint),
		)
	}
	return fmt.Sprintf(
		`if [ "$(id -u)" -eq 0 ]; then bash -lc %s; elif command -v sudo >/dev/null 2>&1; then sudo -n bash -lc %s || { echo %s >&2; exit 1; }; else echo %s >&2; exit 1; fi`,
		shellQuote(trimmedScript),
		shellQuote(trimmedScript),
		shellQuote(hint),
		shellQuote(hint),
	)
}

func joinInstallOutputs(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		filtered = append(filtered, text)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func (s *hysteriaService) uninstall(ctx context.Context, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存节点配置")
	}
	if s.agents != nil {
		s.agents.markAgentOfflineByNodeID(ctx, node.ID, "节点执行了卸载流程")
	}
	return s.remote.run(*node, s.buildUninstallCommand(*node), "")
}

func (s *hysteriaService) deployConfig(ctx context.Context, apiBaseURL string, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存节点配置")
	}
	if s.shouldUseAgent(*node) && s.agents != nil {
		if agent, err := s.agents.getAgentRecordByNodeID(ctx, node.ID); err == nil && agent != nil {
			payload, err := s.buildConfigTaskPayload(ctx, *node, apiBaseURL)
			if err != nil {
				return nil, err
			}
			return s.runAgentTask(ctx, *node, opcodeWriteConfig, payload, 30)
		}
	}
	return s.deployConfigOverSSH(ctx, *node, apiBaseURL)
}

func (s *hysteriaService) serviceAction(ctx context.Context, action string, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存节点配置")
	}
	switch action {
	case "start", "stop", "restart", "status", "upgrade-agent":
	default:
		return nil, errors.New("不支持的服务操作")
	}
	if action == "upgrade-agent" {
		apiBaseURL := ""
		if s.cfg != nil {
			apiBaseURL = s.cfg.PublicAPIBaseURL
		}
		return s.upgradeAgent(ctx, *node, apiBaseURL)
	}
	if s.shouldUseAgent(*node) {
		if action == "status" {
			return s.buildAgentStatusResult(ctx, *node), nil
		}
		opcode := map[string]int{"restart": opcodeRestartHY2, "stop": opcodeStopHY2, "start": opcodeStartHY2}[action]
		return s.runAgentTask(ctx, *node, opcode, nil, 20)
	}
	return s.runSSHServiceAction(*node, action)
}

func (s *hysteriaService) upgradeAgent(ctx context.Context, node nodeRecord, apiBaseURL string) (map[string]any, error) {
	if !s.shouldUseAgent(node) {
		return nil, errors.New("该节点未启用 Agent 管理")
	}
	result, err := s.deployAgentBootstrap(ctx, node, apiBaseURL)
	if err != nil {
		return nil, err
	}
	if intValue(result["exitCode"]) != 0 {
		result["output"] = s.humanizeNodeInstallOutput(toString(result["output"]))
	}
	return result, nil
}

func (s *hysteriaService) logs(ctx context.Context, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存节点配置")
	}
	if s.shouldUseAgent(*node) {
		result, err := s.runAgentTask(ctx, *node, opcodeFetchLogs, map[string]any{"lines": 200}, 20)
		if err != nil {
			return nil, err
		}
		payload := mapValue(result["payload"])
		return map[string]any{
			"command":  "agent:logs",
			"output":   strings.TrimSpace(defaultString(toString(payload["logs"]), toString(result["output"]))),
			"exitCode": intValue(result["exitCode"]),
		}, nil
	}
	return s.remote.run(*node, `tail -n 50 /var/log/mxinhy-agent/hysteria.log 2>/dev/null || echo "节点日志文件尚未生成，请先通过 Agent 重新下发配置或重启服务以启用文件日志"`, "")
}

func (s *hysteriaService) syncTrafficUsage(ctx context.Context, clear bool, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存节点配置")
	}
	var result map[string]any
	if s.shouldUseAgent(*node) {
		result, err = s.runAgentTask(ctx, *node, opcodeSyncTrafficClear, map[string]any{"clear": clear}, 20)
		if err != nil {
			return nil, err
		}
		result, err = s.applyTrafficUsagePayload(ctx, *node, mapValue(mapValue(result["payload"])["traffic"]), result)
		if err != nil {
			return nil, err
		}
	} else {
		result, err = s.syncTrafficUsageForNode(ctx, *node, clear)
		if err != nil {
			return nil, err
		}
	}
	_ = s.recordTrafficSnapshot(ctx, node.ID)
	return result, nil
}

func (s *hysteriaService) recordTrafficSnapshot(ctx context.Context, nodeID int64) error {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return err
	}
	if s.shouldUseAgent(*node) && s.agents != nil {
		agent, agentErr := s.agents.getAgentRecordByNodeID(ctx, node.ID)
		if agentErr == nil && agent != nil {
			_ = s.insertTrafficSnapshot(
				ctx,
				node.ID,
				int64Value(agent["last_total_rx"]),
				int64Value(agent["last_total_tx"]),
				intValue(agent["last_user_count"]),
				intValue(agent["last_online_count"]),
			)
		}
		return nil
	}
	trafficResult, err := s.callTrafficStatsAPI(ctx, *node, "/traffic", "GET", nil)
	if err != nil {
		return nil
	}
	onlineResult, err := s.callTrafficStatsAPI(ctx, *node, "/online", "GET", nil)
	if err != nil {
		return nil
	}
	totalRx := int64(0)
	totalTx := int64(0)
	userCount := 0
	for _, raw := range mapValue(trafficResult["payload"]) {
		stats, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		totalRx += int64Value(stats["rx"])
		totalTx += int64Value(stats["tx"])
		userCount++
	}
	onlineCount := len(mapValue(onlineResult["payload"]))
	_ = s.insertTrafficSnapshot(ctx, node.ID, totalRx, totalTx, userCount, onlineCount)
	return nil
}

func (s *hysteriaService) insertTrafficSnapshot(ctx context.Context, nodeID int64, totalRx int64, totalTx int64, userCount int, onlineCount int) error {
	if s == nil || s.db == nil {
		return nil
	}

	previousRx, previousTx, hasPrevious, err := s.getLatestTrafficSnapshotTotals(ctx, nodeID)
	if err != nil {
		return err
	}

	deltaRx := computeTrafficDelta(totalRx, previousRx, hasPrevious)
	deltaTx := computeTrafficDelta(totalTx, previousTx, hasPrevious)

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO node_traffic_history (node_id, total_rx, total_tx, delta_rx, delta_tx, user_count, online_count) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nodeID,
		totalRx,
		totalTx,
		deltaRx,
		deltaTx,
		userCount,
		onlineCount,
	)
	return err
}

func (s *hysteriaService) getLatestTrafficSnapshotTotals(ctx context.Context, nodeID int64) (int64, int64, bool, error) {
	if s == nil || s.db == nil {
		return 0, 0, false, nil
	}

	var totalRx int64
	var totalTx int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT total_rx, total_tx
		FROM node_traffic_history
		WHERE node_id = ?
		ORDER BY recorded_at DESC, id DESC
		LIMIT 1`,
		nodeID,
	).Scan(&totalRx, &totalTx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	return totalRx, totalTx, true, nil
}

func (s *hysteriaService) runSSHServiceAction(node nodeRecord, action string) (map[string]any, error) {
	command := fmt.Sprintf("systemctl %s %s", action, shellQuote(node.ServiceName))
	if action == "status" {
		command = fmt.Sprintf("systemctl status %s --no-pager", shellQuote(node.ServiceName))
	}
	return s.remote.run(node, command, "")
}

func (s *hysteriaService) buildAgentStatusResult(ctx context.Context, node nodeRecord) map[string]any {
	if s.agents == nil {
		return map[string]any{"command": "agent:status", "output": "节点尚未部署 Agent，请先执行安装流程", "exitCode": 1}
	}
	record, err := s.agents.getAgentRecordByNodeID(ctx, node.ID)
	if err != nil || record == nil {
		return map[string]any{"command": "agent:status", "output": "节点尚未部署 Agent，请先执行安装流程", "exitCode": 1}
	}
	agent := s.agents.presentAgent(record)
	agentStatus := toString(agent["status"])
	serviceStatus := toString(agent["last_service_status"])
	message := toString(agent["last_service_message"])
	if strings.TrimSpace(message) == "" {
		if agentStatus == agentStatusOnline {
			message = "Agent 已在线，但尚未回传详细状态"
		} else {
			message = "Agent 当前离线或未完成首次心跳"
		}
	} else if agentStatus != agentStatusOnline {
		message = "Agent 当前离线或未完成首次心跳"
	}
	exitCode := 1
	if agentStatus == agentStatusOnline && serviceStatus == "running" {
		exitCode = 0
	}
	return map[string]any{"command": "agent:status", "output": message, "exitCode": exitCode}
}

func (s *hysteriaService) runAgentTask(ctx context.Context, node nodeRecord, opcode int, payload map[string]any, timeoutSeconds int) (map[string]any, error) {
	if s.agents == nil {
		return nil, errors.New("节点 Agent 尚未部署，请先执行安装流程")
	}
	task, err := s.agents.enqueueTask(ctx, node.ID, opcode, payload, 0, 120)
	if err != nil {
		return nil, errors.New("节点 Agent 尚未部署，请先执行安装流程")
	}
	done, err := s.agents.waitForTask(ctx, int64Value(task["id"]), timeoutSeconds)
	if err != nil {
		return nil, err
	}
	return s.agents.buildTaskResult(done, "agent:"+opcodeLabel(opcode)), nil
}

func (s *hysteriaService) buildConfigTaskPayload(ctx context.Context, node nodeRecord, apiBaseURL string) (map[string]any, error) {
	users, err := s.listUsers(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	authURL := s.buildAuthBackendURL(node, apiBaseURL)
	payload := map[string]any{
		"config_base64": base64.StdEncoding.EncodeToString([]byte(buildHysteriaConfig(node, users, authURL))),
	}
	if s.usesLocalNodeAuth(node) {
		localAuthPayload, err := s.buildLocalAuthTaskPayload(ctx, node)
		if err != nil {
			return nil, err
		}
		payload["local_auth"] = localAuthPayload
	}
	return payload, nil
}

func (s *hysteriaService) queueConfigDeploy(ctx context.Context, apiBaseURL string, nodeID int64) (map[string]any, error) {
	node, err := s.getNodeOrCurrent(ctx, nodeID)
	if err != nil || node == nil {
		return nil, errors.New("请先保存节点配置")
	}
	if s.shouldUseAgent(*node) && s.agents != nil {
		payload, err := s.buildConfigTaskPayload(ctx, *node, apiBaseURL)
		if err != nil {
			return nil, err
		}
		task, err := s.agents.enqueueTask(ctx, node.ID, opcodeWriteConfig, payload, 0, 300)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"command":  "agent:write-config",
			"output":   "配置已加入异步同步队列，Agent 将在下一次轮询时自动应用",
			"exitCode": 0,
			"payload": map[string]any{
				"task_id":    task["id"],
				"taskStatus": task["status"],
			},
		}, nil
	}
	return s.deployConfig(ctx, apiBaseURL, nodeID)
}

func (s *hysteriaService) deployConfigOverSSH(ctx context.Context, node nodeRecord, apiBaseURL string) (map[string]any, error) {
	payload, err := s.buildConfigTaskPayload(ctx, node, apiBaseURL)
	if err != nil {
		return nil, err
	}
	command := fmt.Sprintf("mkdir -p $(dirname %s) && echo %s | base64 -d | tee %s >/dev/null",
		shellQuote(node.ConfigPath), shellQuote(toString(payload["config_base64"])), shellQuote(node.ConfigPath))
	return s.remote.run(node, command, "")
}

func (s *hysteriaService) prepareTLSAssets(ctx context.Context, node nodeRecord) (map[string]any, error) {
	_ = ctx
	if normalizeTLSMode(node.TLSMode) != "self_signed" {
		return nil, nil
	}
	subjectName := strings.TrimSpace(node.Domain)
	if subjectName == "" {
		subjectName = strings.TrimSpace(node.Host)
	}
	san := "DNS:" + subjectName
	if net.ParseIP(subjectName) != nil {
		san = "IP:" + subjectName
	}
	command := fmt.Sprintf(
		"mkdir -p $(dirname %s) && if [ ! -s %s ] || [ ! -s %s ]; then openssl req -x509 -nodes -newkey rsa:2048 -keyout %s -out %s -days 3650 -subj %s -addext %s; fi && chown hysteria:hysteria %s %s 2>/dev/null || true && chmod 600 %s 2>/dev/null || true && chmod 644 %s 2>/dev/null || true",
		shellQuote(node.TLSCertPath), shellQuote(node.TLSCertPath), shellQuote(node.TLSKeyPath), shellQuote(node.TLSKeyPath), shellQuote(node.TLSCertPath),
		shellQuote("/CN="+subjectName), shellQuote("subjectAltName="+san), shellQuote(node.TLSCertPath), shellQuote(node.TLSKeyPath), shellQuote(node.TLSKeyPath), shellQuote(node.TLSCertPath),
	)
	return s.remote.run(node, command, "")
}

func (s *hysteriaService) deployAgentBootstrap(ctx context.Context, node nodeRecord, apiBaseURL string) (map[string]any, error) {
	if s.agents == nil {
		return nil, errors.New("节点安装失败：请先为面板配置可被节点访问的公网地址，然后再重试安装")
	}
	resolved := normalizePublicAPIBaseURL(apiBaseURL)
	if resolved == "" && s.cfg != nil {
		resolved = normalizePublicAPIBaseURL(s.cfg.PublicAPIBaseURL)
	}
	if resolved == "" {
		return nil, errors.New("节点安装失败：请先在系统设置中配置面板公网地址（public_api_base_url），并确认节点可以访问该地址后再重试")
	}
	bootstrap, err := s.agents.buildBootstrapConfig(ctx, node, resolved)
	if err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(map[string]any{
		"node_id":                    node.ID,
		"agent_id":                   bootstrap["agent_id"],
		"agent_secret":               bootstrap["agent_secret"],
		"panel_api_base_url":         bootstrap["panel_api_base_url"],
		"service_name":               bootstrap["service_name"],
		"config_path":                bootstrap["config_path"],
		"traffic_stats_listen":       bootstrap["traffic_stats_listen"],
		"traffic_stats_secret":       bootstrap["traffic_stats_secret"],
		"local_auth_listen":          bootstrap["local_auth_listen"],
		"local_auth_state_path":      bootstrap["local_auth_state_path"],
		"report_interval_seconds":    bootstrap["report_interval_seconds"],
		"task_poll_interval_seconds": bootstrap["task_poll_interval_seconds"],
		"agent_service_name":         bootstrap["agent_service_name"],
	})
	if err != nil {
		return nil, errors.New("Agent 配置生成失败")
	}
	serviceUnit, err := s.renderAgentServiceUnit(toString(bootstrap["agent_install_path"]), toString(bootstrap["agent_config_path"]))
	if err != nil {
		return nil, err
	}
	archResult, err := s.remote.run(node, "uname -m", "ssh/detect-agent-arch")
	if err != nil {
		return nil, err
	}
	if intValue(archResult["exitCode"]) != 0 {
		return archResult, nil
	}
	target := agentBinaryTargetForArch(toString(archResult["output"]))
	if target == "" {
		return map[string]any{"command": "ssh/bootstrap-agent", "output": "当前服务器架构不受支持，Agent 仅支持 x86_64/amd64 和 aarch64/arm64", "exitCode": 1}, nil
	}
	downloadURL := s.buildAgentBinaryDownloadURL(resolved, target, toString(bootstrap["agent_id"]), toString(bootstrap["agent_secret"]))
	downloadTempPath := toString(bootstrap["agent_install_path"]) + ".download.tmp"
	serviceUnitPath := "/etc/systemd/system/" + toString(bootstrap["agent_service_name"]) + ".service"
	command := strings.Join([]string{
		"set -e",
		"mkdir -p " + shellQuote(filepath.Dir(toString(bootstrap["agent_install_path"]))),
		"rm -f " + shellQuote(downloadTempPath),
		"if command -v curl >/dev/null 2>&1; then curl -fsSL " + shellQuote(downloadURL) + " -o " + shellQuote(downloadTempPath) + "; elif command -v wget >/dev/null 2>&1; then wget -qO " + shellQuote(downloadTempPath) + " " + shellQuote(downloadURL) + "; else echo \"需要在节点服务器安装 curl 或 wget 后再部署 Agent\" >&2; exit 1; fi",
		"mv " + shellQuote(downloadTempPath) + " " + shellQuote(toString(bootstrap["agent_install_path"])),
		"chmod 755 " + shellQuote(toString(bootstrap["agent_install_path"])),
		"mkdir -p " + shellQuote(filepath.Dir(toString(bootstrap["agent_config_path"]))),
		"echo " + shellQuote(base64.StdEncoding.EncodeToString(configJSON)) + " | base64 -d > " + shellQuote(toString(bootstrap["agent_config_path"])),
		"chmod 600 " + shellQuote(toString(bootstrap["agent_config_path"])),
		"echo " + shellQuote(base64.StdEncoding.EncodeToString([]byte(serviceUnit))) + " | base64 -d > " + shellQuote(serviceUnitPath),
		"chmod 644 " + shellQuote(serviceUnitPath),
		"systemctl daemon-reload",
		"systemctl enable " + shellQuote(toString(bootstrap["agent_service_name"])) + " >/dev/null 2>&1 || true",
		"systemctl restart " + shellQuote(toString(bootstrap["agent_service_name"])),
		"echo Agent bootstrap completed",
	}, " && ")
	return s.remote.run(node, command, "ssh/bootstrap-agent")
}

func (s *hysteriaService) humanizeNodeInstallOutput(output string) string {
	message := strings.TrimSpace(output)
	if message == "" {
		return "节点安装失败，请检查节点网络、SSH 凭据和面板部署配置后重试"
	}

	lower := strings.ToLower(message)
	switch {
	case strings.Contains(message, "需要在节点服务器安装 curl 或 wget 后再部署 Agent"):
		return "节点安装失败：节点服务器缺少 curl 或 wget，无法下载 Agent 安装包，请先安装其中一个工具后重试"
	case strings.Contains(lower, "failed to connect to 127.0.0.1 port 80"),
		strings.Contains(lower, "failed to connect to localhost port 80"),
		strings.Contains(lower, "http://127.0.0.1"),
		strings.Contains(message, "Agent 部署需要可访问的面板 API 地址"):
		return "节点安装失败：Agent 无法访问面板接口。请先在系统设置中配置可被节点访问的面板公网地址（public_api_base_url），并确认节点可以访问该地址后再重试"
	case strings.Contains(lower, "unit mxinhy-agent.service not found"),
		strings.Contains(lower, "failed to restart mxinhy-agent.service"):
		return "节点安装失败：Agent 服务尚未创建成功。请检查 Agent 安装包下载是否成功，以及 systemd 是否可正常加载 `mxinhy-agent.service` 后再重试"
	default:
		return message
	}
}

func (s *hysteriaService) renderAgentServiceUnit(agentInstallPath string, agentConfigPath string) (string, error) {
	templatePath := filepath.Join(projectRootFromConfig(*s.cfg), "deploy", "systemd", "mxinhy-agent.service")
	template, err := os.ReadFile(templatePath)
	if err != nil || strings.TrimSpace(string(template)) == "" {
		return "", errors.New("Agent systemd 模板不存在")
	}
	unit := strings.ReplaceAll(string(template), "{{AGENT_INSTALL_PATH}}", agentInstallPath)
	unit = strings.ReplaceAll(unit, "{{AGENT_CONFIG_PATH}}", agentConfigPath)
	return unit, nil
}

func agentBinaryTargetForArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "amd64":
		return "linux-amd64"
	case "aarch64", "arm64":
		return "linux-arm64"
	default:
		return ""
	}
}

func (s *hysteriaService) buildAgentBinaryDownloadURL(panelAPIBaseURL string, target string, agentID string, secret string) string {
	expiresAt := time.Now().Unix() + 900
	signature := s.agents.buildDownloadSignature(target, agentID, expiresAt, secret)
	return strings.TrimRight(panelAPIBaseURL, "/") + "/agent/download/" + url.QueryEscape(target) + "?agent_id=" + url.QueryEscape(agentID) + "&expires=" + strconv.FormatInt(expiresAt, 10) + "&signature=" + url.QueryEscape(signature)
}

func (s *hysteriaService) buildAgentUpgradeInstruction(record map[string]any, payload map[string]any) map[string]any {
	if s == nil || s.cfg == nil || s.agents == nil || record == nil {
		return nil
	}
	currentVersion := strings.TrimSpace(toString(payload["version"]))
	if currentVersion == "" || currentVersion == currentAgentVersion {
		return nil
	}
	target := strings.TrimSpace(toString(payload["target"]))
	if target != "linux-amd64" && target != "linux-arm64" {
		return nil
	}
	apiBaseURL := normalizePublicAPIBaseURL(s.cfg.PublicAPIBaseURL)
	if apiBaseURL == "" {
		return nil
	}
	downloadURL := s.buildAgentBinaryDownloadURL(apiBaseURL, target, toString(record["agent_id"]), toString(record["shared_secret"]))
	return map[string]any{
		"version":      currentAgentVersion,
		"download_url": downloadURL,
	}
}

func (s *hysteriaService) buildUninstallCommand(node nodeRecord) string {
	commands := []string{
		"set -e",
		"systemctl disable --now " + shellQuote(defaultString(node.AgentServiceName, "mxinhy-agent")) + " >/dev/null 2>&1 || true",
		"rm -f " + shellQuote("/etc/systemd/system/"+defaultString(node.AgentServiceName, "mxinhy-agent")+".service"),
		"rm -f " + shellQuote(defaultString(node.AgentInstallPath, "/usr/local/bin/mxinhy-agent")),
		"rm -f " + shellQuote(defaultString(node.AgentConfigPath, "/etc/mxinhy-agent.json")),
		"systemctl daemon-reload || true",
		"systemctl stop " + shellQuote(defaultString(node.ServiceName, "hysteria-server")) + " >/dev/null 2>&1 || true",
		"bash <(curl -fsSL https://get.hy2.sh/) --remove || true",
		"rm -f " + shellQuote(defaultString(node.ConfigPath, "/etc/hysteria/config.yaml")),
	}
	if normalizeTLSMode(node.TLSMode) == "self_signed" {
		commands = append(commands, "rm -f "+shellQuote(node.TLSCertPath), "rm -f "+shellQuote(node.TLSKeyPath))
	}
	return strings.Join(commands, " && ")
}

func (s *hysteriaService) callTrafficStatsAPI(ctx context.Context, node nodeRecord, path string, method string, payload any) (map[string]any, error) {
	_ = ctx
	secret := strings.TrimSpace(node.TrafficStatsSecret)
	if secret == "" {
		secret = strings.TrimSpace(node.ObfsPassword)
	}
	if strings.TrimSpace(node.TrafficStatsListen) == "" {
		return nil, errors.New("该节点未配置 trafficStats.listen")
	}
	if secret == "" {
		return nil, errors.New("该节点未配置 trafficStats.secret")
	}
	command := fmt.Sprintf("curl -fsSL -X %s -H %s", shellQuote(strings.ToUpper(method)), shellQuote("Authorization: "+secret))
	if payload != nil {
		body, _ := json.Marshal(payload)
		command += " -H " + shellQuote("Content-Type: application/json") + " --data " + shellQuote(string(body))
	}
	command += " " + shellQuote(buildTrafficStatsURL(node.TrafficStatsListen)+path)
	result, err := s.remote.run(node, command, "")
	if err != nil {
		return nil, err
	}
	if intValue(result["exitCode"]) != 0 {
		return result, nil
	}
	result["payload"] = decodeTrafficJSONOutput(toString(result["output"]))
	return result, nil
}

func (s *hysteriaService) syncTrafficUsageForNode(ctx context.Context, node nodeRecord, clear bool) (map[string]any, error) {
	path := "/traffic"
	if clear {
		path = "/traffic?clear=1"
	}
	result, err := s.callTrafficStatsAPI(ctx, node, path, "GET", nil)
	if err != nil {
		return nil, err
	}
	if intValue(result["exitCode"]) != 0 {
		return result, nil
	}
	return s.applyTrafficUsagePayload(ctx, node, mapValue(result["payload"]), result)
}

func (s *hysteriaService) kickClients(ctx context.Context, node nodeRecord, clientIDs []string) (map[string]any, error) {
	if len(clientIDs) == 0 {
		return map[string]any{"command": "", "output": "没有需要踢下线的用户", "exitCode": 0, "clients": []string{}}, nil
	}
	if s.shouldUseAgent(node) {
		return s.runAgentTask(ctx, node, opcodeKickClients, map[string]any{"clients": clientIDs}, 20)
	}
	return s.callTrafficStatsAPI(ctx, node, "/kick", "POST", clientIDs)
}

func (s *hysteriaService) applyTrafficUsagePayload(ctx context.Context, node nodeRecord, payload map[string]any, baseResult map[string]any) (map[string]any, error) {
	suspendedUsers := make([]string, 0)
	for username, raw := range payload {
		stats, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		usedBytes := int64Value(stats["rx"]) + int64Value(stats["tx"])
		if _, err := s.db.ExecContext(ctx, `
                UPDATE hysteria_users
                SET used_bytes = used_bytes + ?,
                    used_gb = FLOOR((used_bytes + ?) / 1024 / 1024 / 1024)
                WHERE node_id = ? AND username = ?`, usedBytes, usedBytes, node.ID, username); err != nil {
			return nil, err
		}
		user, err := s.findStoredUserByNodeAndUsername(ctx, node.ID, username)
		if err != nil || user == nil {
			continue
		}
		nextUsedBytes := user.UsedBytes + usedBytes
		if user.QuotaGB > 0 && nextUsedBytes >= gigabytesToBytes(user.QuotaGB) && normalizeUserStatus(user.Status) != "suspended" {
			if _, err := s.db.ExecContext(ctx, `UPDATE hysteria_users SET status = 'suspended' WHERE id = ?`, user.ID); err == nil {
				suspendedUsers = append(suspendedUsers, username)
			}
		}
	}
	if len(suspendedUsers) > 0 {
		suspendedUsers = uniqueTrimmedStrings(suspendedUsers)
		if s.usesLocalNodeAuth(node) {
			s.queueLocalAuthRefresh(ctx, node, suspendedUsers)
			baseResult["kick"] = map[string]any{
				"clients":  suspendedUsers,
				"output":   "已加入异步同步和踢线队列，Agent 将按顺序更新本地鉴权并断开现有连接",
				"exitCode": 0,
			}
		} else {
			kickResult, _ := s.kickClients(ctx, node, suspendedUsers)
			baseResult["kick"] = map[string]any{
				"clients":  suspendedUsers,
				"output":   toString(kickResult["output"]),
				"exitCode": intValue(kickResult["exitCode"]),
			}
		}
	}
	baseResult["traffic"] = payload
	baseResult["suspendedUsers"] = suspendedUsers
	return baseResult, nil
}

func liveUsedBytesForUser(user userRecord, stats map[string]any) int64 {
	return user.UsedBytes + int64Value(stats["rx"]) + int64Value(stats["tx"])
}

func (s *hysteriaService) sliceListResponse(items []map[string]any, page int, pageSize int) map[string]any {
	page, pageSize, offset := resolvePagination(page, pageSize)
	total := len(items)
	end := offset + pageSize
	if end > total {
		end = total
	}
	if offset > total {
		offset = total
	}
	paged := make([]any, 0, end-offset)
	for _, item := range items[offset:end] {
		paged = append(paged, item)
	}
	return paginatedResult(paged, page, pageSize, total)
}

func formatBytes(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	case bytes < 1024*1024*1024:
		return fmt.Sprintf("%.2f MB", float64(bytes)/1024/1024)
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/1024/1024/1024)
	}
}

func gigabytesToBytes(gb int64) int64 {
	return gb * 1024 * 1024 * 1024
}

func roundBytesToGigabytes(bytes int64, fallback int64) float64 {
	if bytes <= 0 {
		return float64(fallback)
	}
	return math.Round((float64(bytes)/1024/1024/1024)*100) / 100
}

func bytesFromGigabytesFloat(value float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(math.Round(value * 1024 * 1024 * 1024))
}

func buildTrafficStatsURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "http://127.0.0.1:9999"
	}
	if strings.HasPrefix(listen, "http://") || strings.HasPrefix(listen, "https://") {
		return strings.TrimRight(listen, "/")
	}
	if strings.HasPrefix(listen, ":") {
		return "http://127.0.0.1" + listen
	}
	return "http://" + strings.TrimRight(listen, "/")
}

func decodeTrafficJSONOutput(output string) map[string]any {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return map[string]any{}
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(trimmed), &data); err == nil {
		return data
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		_ = json.Unmarshal([]byte(trimmed[start:end+1]), &data)
	}
	if data == nil {
		return map[string]any{}
	}
	return data
}

func buildConnectionURIForNodeUser(node nodeRecord, user userRecord) string {
	host := subscriptionURIHost(node)

	params := url.Values{}
	if normalizeTLSMode(node.TLSMode) == "self_signed" {
		params.Set("insecure", "1")
		if strings.TrimSpace(node.Domain) != "" && node.Domain != node.Host {
			params.Set("sni", node.Domain)
		}
	} else if strings.TrimSpace(node.Domain) != "" {
		params.Set("sni", node.Domain)
	}
	if strings.TrimSpace(user.AuthPassword) != "" && strings.TrimSpace(node.ObfsPassword) != "" {
		params.Set("obfs", "salamander")
		params.Set("obfs-password", node.ObfsPassword)
	}
	if user.SpeedLimitMbps > 0 {
		params.Set("upmbps", fmt.Sprintf("%d", user.SpeedLimitMbps))
		params.Set("downmbps", fmt.Sprintf("%d", user.SpeedLimitMbps))
	}

	query := params.Encode()
	if query != "" {
		query = "?" + query
	}
	label := url.QueryEscape(user.Username + "@" + node.Name)
	return fmt.Sprintf("hy2://%s@%s:%d%s#%s", url.QueryEscape(user.AuthPassword), host, node.ListenPort, query, label)
}

func subscriptionURIHost(node nodeRecord) string {
	host := strings.TrimSpace(node.Domain)
	if host == "" {
		host = strings.TrimSpace(node.Host)
	}
	return host
}

func subscriptionServerHost(node nodeRecord) string {
	return strings.Trim(subscriptionURIHost(node), "[]")
}

func subscriptionBandwidth(node nodeRecord, user userRecord) (int, int) {
	if user.SpeedLimitMbps > 0 {
		return user.SpeedLimitMbps, user.SpeedLimitMbps
	}
	return node.BandwidthUpMbps, node.BandwidthDownMbps
}

func yamlQuote(value string) string {
	return strconv.Quote(value)
}

func isUserSubscribable(user userRecord) bool {
	if normalizeUserStatus(user.Status) != "active" {
		return false
	}
	if user.ExpiresAt.Valid && !user.ExpiresAt.Time.After(time.Now()) {
		return false
	}
	return strings.TrimSpace(user.AuthPassword) != "" && (user.QuotaGB == 0 || user.UsedBytes < gigabytesToBytes(user.QuotaGB))
}

func scanNodeRecord(scanner interface{ Scan(...any) error }) (nodeRecord, error) {
	var record nodeRecord
	var agentEnabled int
	err := scanner.Scan(
		&record.ID, &record.CurrentNode, &record.DeployMode, &record.Name, &record.Host, &record.SSHPort, &record.SSHUsername, &record.SSHAuthType,
		&record.SSHPassword, &record.SSHPrivateKeyPath, &record.SudoPassword, &record.InstallScript, &record.ServiceName,
		&record.ConfigPath, &record.ListenPort, &record.TrafficStatsListen, &record.TrafficStatsSecret, &record.TLSMode,
		&record.TLSCertPath, &record.TLSKeyPath, &record.Domain, &record.ACMEEmail, &record.ObfsPassword, &record.MasqueradeURL,
		&record.BandwidthUpMbps, &record.BandwidthDownMbps, &record.ManageMode, &agentEnabled, &record.AgentReportIntervalSeconds,
		&record.AgentTaskPollIntervalSeconds, &record.AgentInstallPath, &record.AgentConfigPath, &record.AgentServiceName,
		&record.DeletedAt, &record.CreatedAt, &record.UpdatedAt,
	)
	record.AgentEnabled = agentEnabled == 1
	return record, err
}

func scanUserRecord(scanner interface{ Scan(...any) error }) (userRecord, error) {
	var record userRecord
	err := scanner.Scan(&record.ID, &record.PublicID, &record.NodeID, &record.Username, &record.AuthPassword, &record.Status, &record.QuotaGB, &record.UsedGB, &record.UsedBytes, &record.SpeedLimitMbps, &record.ExpiresAt, &record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func paginatedResult(items []any, page int, pageSize int, total int) map[string]any {
	page, pageSize, _ = resolvePagination(page, pageSize)
	return map[string]any{
		"items": items,
		"pagination": map[string]any{
			"page":       page,
			"pageSize":   pageSize,
			"total":      total,
			"totalPages": totalPages(total, pageSize),
		},
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func normalizeTLSMode(value string) string {
	if strings.TrimSpace(value) == "self_signed" {
		return "self_signed"
	}
	return "acme"
}

func normalizeManageMode(value string) string {
	if strings.TrimSpace(value) == "ssh" {
		return "ssh"
	}
	return "agent"
}

func normalizeDeployMode(value string) string {
	if strings.TrimSpace(value) == "local" {
		return "local"
	}
	return "ssh"
}

func normalizeSSHAuthType(value string) string {
	if strings.TrimSpace(value) == "key" {
		return "key"
	}
	return "password"
}

func normalizeUserStatus(value string) string {
	if strings.TrimSpace(value) == "suspended" {
		return "suspended"
	}
	return "active"
}

func normalizeNullableDateTime(value any, fallback sql.NullTime) (any, error) {
	text := strings.TrimSpace(toString(value))
	if text == "" {
		if fallback.Valid {
			return fallback.Time.Format("2006-01-02 15:04:05"), nil
		}
		return nil, nil
	}

	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.Format("2006-01-02 15:04:05"), nil
		}
	}
	return nil, errors.New("过期时间格式不正确")
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return value.String
}

func nullInt64(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func computeTrafficDelta(current int64, previous int64, hasPrevious bool) int64 {
	if current < 0 {
		current = 0
	}
	if !hasPrevious {
		return 0
	}
	if current >= previous {
		return current - previous
	}
	return current
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func boolValue(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func maxInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func boundedInt64(value any, minValue int64, maxValue int64, fallback int64) int64 {
	number := fallback
	switch typed := value.(type) {
	case float64:
		number = int64(typed)
	case int64:
		number = typed
	case int:
		number = int64(typed)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			number = parsed
		}
	}
	if number < minValue {
		return minValue
	}
	if number > maxValue {
		return maxValue
	}
	return number
}

func float64Value(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int64:
		return float64(typed)
	case int:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func boundedFloat64(value any, minValue float64, maxValue float64, fallback float64) float64 {
	number := fallback
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case int:
		number = float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			number = parsed
		}
	}
	if number < minValue {
		return minValue
	}
	if number > maxValue {
		return maxValue
	}
	return number
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func normalizeDBError(err error, fallback string) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate") || strings.Contains(message, "unique") {
		return errors.New("存在重复数据，请检查后重试")
	}
	return errors.New(fallback)
}

func valueOrNil[T any](item *T, getter func(*T) any) any {
	if item == nil {
		return nil
	}
	return getter(item)
}
