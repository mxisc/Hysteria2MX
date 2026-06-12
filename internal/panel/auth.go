package panel

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	loginChallengeCookie = "mxinhy_login_challenge"
	sessionTTL           = 12 * time.Hour
	loginChallengeTTL    = 120 * time.Second
)

var adminUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

type authService struct {
	db  *sql.DB
	cfg *Config
}

type authUser struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Role        string   `json:"role"`
	Status      string   `json:"status"`
	Permissions []string `json:"permissions,omitempty"`
}

type signedSession struct {
	AdminID   int64 `json:"admin_id"`
	ExpiresAt int64 `json:"expires_at"`
}

type loginChallenge struct {
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"expiresAt"`
}

func newAuthService(db *sql.DB, cfg *Config) *authService {
	return &authService{db: db, cfg: cfg}
}

func (s *authService) issueLoginChallenge(writer http.ResponseWriter) (loginChallenge, error) {
	nonce, err := randomHex(16)
	if err != nil {
		return loginChallenge{}, err
	}

	challenge := loginChallenge{
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(loginChallengeTTL).Unix(),
	}

	if err := writeSignedCookie(writer, s.cfg.SessionName, loginChallengeCookie, challenge, s.cfg.EncryptionKey, loginChallengeTTL); err != nil {
		return loginChallenge{}, err
	}

	return challenge, nil
}

func (s *authService) consumeLoginChallenge(writer http.ResponseWriter, request *http.Request, nonce string) (string, error) {
	var challenge loginChallenge
	if err := readSignedCookie(request, loginChallengeCookie, s.cfg.EncryptionKey, &challenge); err != nil {
		return "", errors.New("登录 challenge 不存在，请刷新页面后重试")
	}

	clearCookie(writer, loginChallengeCookie)

	if challenge.Nonce == "" || !hmac.Equal([]byte(challenge.Nonce), []byte(nonce)) {
		return "", errors.New("登录 challenge 校验失败，请刷新页面后重试")
	}
	if challenge.ExpiresAt < time.Now().Unix() {
		return "", errors.New("登录 challenge 已过期，请刷新页面后重试")
	}

	return challenge.Nonce, nil
}

func (s *authService) attempt(ctx context.Context, writer http.ResponseWriter, username string, password string) (authUser, error) {
	if s.db == nil {
		return authUser{}, errors.New("数据库不可用")
	}
	if !adminUsernamePattern.MatchString(username) {
		return authUser{}, errors.New("用户名仅支持英文和数字")
	}

	const query = `SELECT id, username, password_hash, display_name, role, status FROM admin_users WHERE username = ? LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, username)

	var (
		user         authUser
		passwordHash string
	)
	if err := row.Scan(&user.ID, &user.Username, &passwordHash, &user.DisplayName, &user.Role, &user.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authUser{}, errors.New("用户名或密码不正确")
		}
		return authUser{}, err
	}

	if !verifyPassword(passwordHash, password) {
		return authUser{}, errors.New("用户名或密码不正确")
	}
	if normalizeAdminStatus(user.Status) != "active" {
		return authUser{}, errors.New("当前管理员账号已被停用")
	}

	user.Role = normalizeRole(user.Role)
	user.Status = normalizeAdminStatus(user.Status)
	user.Permissions = permissionsForRole(user.Role)

	if _, err := s.db.ExecContext(ctx, `UPDATE admin_users SET last_login_at = NOW() WHERE id = ?`, user.ID); err != nil {
		return authUser{}, err
	}

	session := signedSession{
		AdminID:   user.ID,
		ExpiresAt: time.Now().Add(sessionTTL).Unix(),
	}
	if err := writeSignedCookie(writer, s.cfg.SessionName, s.cfg.SessionName, session, s.cfg.EncryptionKey, sessionTTL); err != nil {
		return authUser{}, err
	}

	return user, nil
}

func (s *authService) currentUser(ctx context.Context, request *http.Request) (*authUser, error) {
	if s.db == nil {
		return nil, nil
	}
	var session signedSession
	if err := readSignedCookie(request, s.cfg.SessionName, s.cfg.EncryptionKey, &session); err != nil {
		return nil, nil
	}
	if session.ExpiresAt < time.Now().Unix() {
		return nil, nil
	}

	const query = `SELECT id, username, display_name, role, status FROM admin_users WHERE id = ? LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, session.AdminID)

	var user authUser
	if err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.Role = normalizeRole(user.Role)
	user.Status = normalizeAdminStatus(user.Status)
	if user.Status != "active" {
		return nil, nil
	}
	user.Permissions = permissionsForRole(user.Role)
	return &user, nil
}

func (s *authService) logout(writer http.ResponseWriter) {
	clearCookie(writer, s.cfg.SessionName)
	clearCookie(writer, loginChallengeCookie)
}

func normalizeRole(role string) string {
	switch strings.TrimSpace(role) {
	case "super_admin", "operator", "auditor", "viewer":
		return strings.TrimSpace(role)
	default:
		return "viewer"
	}
}

func normalizeAdminStatus(status string) string {
	if strings.TrimSpace(status) == "disabled" {
		return "disabled"
	}
	return "active"
}

func permissionsForRole(role string) []string {
	switch normalizeRole(role) {
	case "super_admin":
		return []string{"*"}
	case "operator":
		return []string{"panel.view", "node.view", "node.manage", "user.view", "user.manage", "config.view", "config.manage", "service.view", "service.manage", "logs.view", "traffic.sync", "audit.view", "appearance.manage", "notification.manage", "system.upgrade", "admin.manage", "job.view"}
	case "auditor":
		return []string{"panel.view", "node.view", "user.view", "config.view", "service.view", "logs.view", "audit.view", "job.view"}
	default:
		return []string{"panel.view", "node.view", "user.view", "config.view", "service.view"}
	}
}

func userCan(user *authUser, permission string) bool {
	if user == nil {
		return false
	}
	for _, item := range user.Permissions {
		if item == "*" || item == permission {
			return true
		}
	}
	return false
}

func writeSignedCookie(writer http.ResponseWriter, sessionName string, name string, payload any, secret string, ttl time.Duration) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	signature := signCookiePayload(body, secret)
	value := base64.RawURLEncoding.EncodeToString(body) + "." + signature
	http.SetCookie(writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	})
	if sessionName != "" {
		_ = sessionName
	}
	return nil
}

func readSignedCookie(request *http.Request, name string, secret string, destination any) error {
	cookie, err := request.Cookie(name)
	if err != nil {
		return err
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return errors.New("invalid cookie format")
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(signCookiePayload(body, secret)), []byte(parts[1])) {
		return errors.New("invalid cookie signature")
	}

	return json.Unmarshal(body, destination)
}

func clearCookie(writer http.ResponseWriter, name string) {
	http.SetCookie(writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func signCookiePayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(bytesLength int) (string, error) {
	buffer := make([]byte, bytesLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func requireUser(ctx context.Context, auth *authService, writer http.ResponseWriter, request *http.Request) (*authUser, bool) {
	user, err := auth.currentUser(ctx, request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "未登录或会话已过期")
		return nil, false
	}
	if user == nil {
		writeError(writer, http.StatusUnauthorized, "未登录或会话已过期")
		return nil, false
	}
	return user, true
}

func requirePermission(user *authUser, permission string, writer http.ResponseWriter) bool {
	if !userCan(user, permission) {
		writeError(writer, http.StatusForbidden, "没有权限执行该操作")
		return false
	}
	return true
}

func verifyPassword(hash string, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) < 4 {
		return false
	}
	return ComparePassword(hash, password) == nil
}

func hashPassword(password string) (string, error) {
	return GeneratePassword(password)
}

func validateAdminUsername(username string) error {
	if !adminUsernamePattern.MatchString(username) {
		return errors.New("管理员用户名仅支持英文和数字")
	}
	return nil
}

func sanitizeAuthUser(user authUser) authUser {
	user.Permissions = permissionsForRole(user.Role)
	return user
}

func httpSafeError(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return fallback
	}
	return message
}

func ensureHTTPSOrLocal(request *http.Request) bool {
	return request.TLS != nil || strings.HasPrefix(request.Host, "127.0.0.1") || strings.HasPrefix(request.Host, "localhost")
}

func parseDisplayName(username string) string {
	if username == "" {
		return "System Admin"
	}
	return fmt.Sprintf("%s", username)
}
