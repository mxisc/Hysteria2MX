package panel

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

const (
	settingSiteTitle               = "site_title"
	settingSiteIconURL             = "site_icon_url"
	settingPublicAPIBaseURL        = "public_api_base_url"
	settingLoginBackgroundURL      = "login_background_url"
	settingMockPanelEnabled        = "mock_panel_enabled"
	settingMockNodeCount           = "mock_node_count"
	settingMockUserCount           = "mock_user_count"
	settingMockRunningNodeCount    = "mock_running_node_count"
	settingMockDegradedNodeCount   = "mock_degraded_node_count"
	settingMockStoppedNodeCount    = "mock_stopped_node_count"
	settingMockSuspendedUserCount  = "mock_suspended_user_count"
	settingBruteforceEnabled       = "bruteforce_enabled"
	settingBruteforceMaxAttempts   = "bruteforce_max_attempts"
	settingBruteforceWindowMinutes = "bruteforce_window_minutes"
	settingBruteforceLockMinutes   = "bruteforce_lock_minutes"
	settingSMTPEnabled             = "smtp_enabled"
	settingSMTPHost                = "smtp_host"
	settingSMTPPort                = "smtp_port"
	settingSMTPEncryption          = "smtp_encryption"
	settingSMTPUsername            = "smtp_username"
	settingSMTPPassword            = "smtp_password"
	settingSMTPFromEmail           = "smtp_from_email"
	settingSMTPFromName            = "smtp_from_name"
	settingSMTPNotifyEmail         = "smtp_notify_email"
)

type systemSettingsStore struct {
	db *sql.DB
}

func newSystemSettingsStore(db *sql.DB) *systemSettingsStore {
	return &systemSettingsStore{db: db}
}

func (s *systemSettingsStore) get(ctx context.Context, key string) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, nil
	}
	var value string
	if err := s.db.QueryRowContext(ctx, `SELECT setting_value FROM system_settings WHERE setting_key = ? LIMIT 1`, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (s *systemSettingsStore) set(ctx context.Context, key string, value string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_settings (setting_key, setting_value)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)
	`, key, value)
	return err
}

func (s *systemSettingsStore) setIfMissing(ctx context.Context, key string, value string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_settings (setting_key, setting_value)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE setting_key = system_settings.setting_key
	`, key, value)
	return err
}

func (s *systemSettingsStore) ensureDefaults(ctx context.Context, cfg Config) error {
	defaults := map[string]string{
		settingSiteTitle:               cfg.SiteTitle,
		settingSiteIconURL:             cfg.SiteIconURL,
		settingPublicAPIBaseURL:        normalizePublicAPIBaseURL(cfg.PublicAPIBaseURL),
		settingLoginBackgroundURL:      "",
		settingMockPanelEnabled:        strconv.FormatBool(cfg.MockPanel),
		settingMockNodeCount:           strconv.Itoa(cfg.MockNodeCount),
		settingMockUserCount:           strconv.Itoa(cfg.MockUserCount),
		settingMockRunningNodeCount:    strconv.Itoa(cfg.MockRunningNodeCount),
		settingMockDegradedNodeCount:   strconv.Itoa(cfg.MockDegradedNodeCount),
		settingMockStoppedNodeCount:    strconv.Itoa(cfg.MockStoppedNodeCount),
		settingMockSuspendedUserCount:  strconv.Itoa(cfg.MockSuspendedUserCount),
		settingBruteforceEnabled:       strconv.FormatBool(cfg.BruteforceEnabled),
		settingBruteforceMaxAttempts:   strconv.Itoa(cfg.BruteforceMaxAttempts),
		settingBruteforceWindowMinutes: strconv.Itoa(cfg.BruteforceWindowMinutes),
		settingBruteforceLockMinutes:   strconv.Itoa(cfg.BruteforceLockMinutes),
		settingSMTPEnabled:             strconv.FormatBool(cfg.SMTPEnabled),
		settingSMTPHost:                cfg.SMTPHost,
		settingSMTPPort:                strconv.Itoa(cfg.SMTPPort),
		settingSMTPEncryption:          cfg.SMTPEncryption,
		settingSMTPUsername:            cfg.SMTPUsername,
		settingSMTPPassword:            cfg.SMTPPassword,
		settingSMTPFromEmail:           cfg.SMTPFromEmail,
		settingSMTPFromName:            cfg.SMTPFromName,
		settingSMTPNotifyEmail:         cfg.SMTPNotifyEmail,
	}
	for key, value := range defaults {
		if err := s.setIfMissing(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *systemSettingsStore) applyToConfig(ctx context.Context, cfg *Config) error {
	if s == nil || s.db == nil || cfg == nil {
		return nil
	}

	if value, ok, err := s.get(ctx, settingSiteTitle); err != nil {
		return err
	} else if ok && strings.TrimSpace(value) != "" {
		cfg.SiteTitle = value
	}
	if value, ok, err := s.get(ctx, settingSiteIconURL); err != nil {
		return err
	} else if ok {
		cfg.SiteIconURL = value
	}
	if value, ok, err := s.get(ctx, settingPublicAPIBaseURL); err != nil {
		return err
	} else if ok {
		cfg.PublicAPIBaseURL = normalizePublicAPIBaseURL(value)
	}
	if value, ok, err := s.get(ctx, settingMockPanelEnabled); err != nil {
		return err
	} else if ok {
		cfg.MockPanel = parseStoredBool(value, cfg.MockPanel)
	}
	if value, ok, err := s.get(ctx, settingMockNodeCount); err != nil {
		return err
	} else if ok {
		cfg.MockNodeCount = parseStoredInt(value, cfg.MockNodeCount)
	}
	if value, ok, err := s.get(ctx, settingMockUserCount); err != nil {
		return err
	} else if ok {
		cfg.MockUserCount = parseStoredInt(value, cfg.MockUserCount)
	}
	if value, ok, err := s.get(ctx, settingMockRunningNodeCount); err != nil {
		return err
	} else if ok {
		cfg.MockRunningNodeCount = parseStoredInt(value, cfg.MockRunningNodeCount)
	}
	if value, ok, err := s.get(ctx, settingMockDegradedNodeCount); err != nil {
		return err
	} else if ok {
		cfg.MockDegradedNodeCount = parseStoredInt(value, cfg.MockDegradedNodeCount)
	}
	if value, ok, err := s.get(ctx, settingMockStoppedNodeCount); err != nil {
		return err
	} else if ok {
		cfg.MockStoppedNodeCount = parseStoredInt(value, cfg.MockStoppedNodeCount)
	}
	if value, ok, err := s.get(ctx, settingMockSuspendedUserCount); err != nil {
		return err
	} else if ok {
		cfg.MockSuspendedUserCount = parseStoredInt(value, cfg.MockSuspendedUserCount)
	}
	if value, ok, err := s.get(ctx, settingBruteforceEnabled); err != nil {
		return err
	} else if ok {
		cfg.BruteforceEnabled = parseStoredBool(value, cfg.BruteforceEnabled)
	}
	if value, ok, err := s.get(ctx, settingBruteforceMaxAttempts); err != nil {
		return err
	} else if ok {
		cfg.BruteforceMaxAttempts = parseStoredInt(value, cfg.BruteforceMaxAttempts)
	}
	if value, ok, err := s.get(ctx, settingBruteforceWindowMinutes); err != nil {
		return err
	} else if ok {
		cfg.BruteforceWindowMinutes = parseStoredInt(value, cfg.BruteforceWindowMinutes)
	}
	if value, ok, err := s.get(ctx, settingBruteforceLockMinutes); err != nil {
		return err
	} else if ok {
		cfg.BruteforceLockMinutes = parseStoredInt(value, cfg.BruteforceLockMinutes)
	}
	if value, ok, err := s.get(ctx, settingSMTPEnabled); err != nil {
		return err
	} else if ok {
		cfg.SMTPEnabled = parseStoredBool(value, cfg.SMTPEnabled)
	}
	if value, ok, err := s.get(ctx, settingSMTPHost); err != nil {
		return err
	} else if ok {
		cfg.SMTPHost = value
	}
	if value, ok, err := s.get(ctx, settingSMTPPort); err != nil {
		return err
	} else if ok {
		cfg.SMTPPort = parseStoredInt(value, cfg.SMTPPort)
	}
	if value, ok, err := s.get(ctx, settingSMTPEncryption); err != nil {
		return err
	} else if ok && strings.TrimSpace(value) != "" {
		cfg.SMTPEncryption = value
	}
	if value, ok, err := s.get(ctx, settingSMTPUsername); err != nil {
		return err
	} else if ok {
		cfg.SMTPUsername = value
	}
	if value, ok, err := s.get(ctx, settingSMTPPassword); err != nil {
		return err
	} else if ok {
		cfg.SMTPPassword = value
	}
	if value, ok, err := s.get(ctx, settingSMTPFromEmail); err != nil {
		return err
	} else if ok {
		cfg.SMTPFromEmail = value
	}
	if value, ok, err := s.get(ctx, settingSMTPFromName); err != nil {
		return err
	} else if ok && strings.TrimSpace(value) != "" {
		cfg.SMTPFromName = value
	}
	if value, ok, err := s.get(ctx, settingSMTPNotifyEmail); err != nil {
		return err
	} else if ok {
		cfg.SMTPNotifyEmail = value
	}
	return nil
}

func (s *systemSettingsStore) loginBackgroundURL(ctx context.Context) string {
	value, ok, err := s.get(ctx, settingLoginBackgroundURL)
	if err != nil || !ok {
		return ""
	}
	return value
}

func parseStoredBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseStoredInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}
