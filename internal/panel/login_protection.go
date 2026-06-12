package panel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type loginProtectionService struct {
	config *Config
	path   string
}

type loginAttemptRecord struct {
	FailedCount     int   `json:"failed_count"`
	WindowStartedAt int64 `json:"window_started_at"`
	LastFailedAt    int64 `json:"last_failed_at"`
	LockedUntil     int64 `json:"locked_until"`
}

func newLoginProtectionService(cfg *Config) (*loginProtectionService, error) {
	path := filepath.Join("storage", "security", "login_attempts.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &loginProtectionService{config: cfg, path: path}, nil
}

func (s *loginProtectionService) assertAllowed(username string, ip string) error {
	if !s.config.BruteforceEnabled {
		return nil
	}

	records, err := s.load()
	if err != nil {
		return err
	}

	record := records[s.key(username, ip)]
	if record.LockedUntil > time.Now().Unix() {
		return errors.New("登录尝试次数过多，请稍后再试")
	}

	return nil
}

func (s *loginProtectionService) recordFailure(username string, ip string) error {
	if !s.config.BruteforceEnabled {
		return nil
	}

	records, err := s.load()
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	windowSeconds := max(60, s.config.BruteforceWindowMinutes*60)
	lockSeconds := max(60, s.config.BruteforceLockMinutes*60)
	key := s.key(username, ip)
	record := records[key]

	if record.WindowStartedAt == 0 || record.WindowStartedAt+int64(windowSeconds) < now {
		record.WindowStartedAt = now
		record.FailedCount = 0
	}

	record.FailedCount++
	record.LastFailedAt = now
	if record.FailedCount >= max(1, s.config.BruteforceMaxAttempts) {
		record.LockedUntil = now + int64(lockSeconds)
	}
	records[key] = record

	return s.save(s.prune(records))
}

func (s *loginProtectionService) recordSuccess(username string, ip string) error {
	records, err := s.load()
	if err != nil {
		return err
	}

	delete(records, s.key(username, ip))
	return s.save(records)
}

func (s *loginProtectionService) key(username string, ip string) string {
	return username + "|" + ip
}

func (s *loginProtectionService) load() (map[string]loginAttemptRecord, error) {
	records := map[string]loginAttemptRecord{}
	body, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return records, nil
		}
		return nil, err
	}
	if len(body) == 0 {
		return records, nil
	}
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *loginProtectionService) save(records map[string]loginAttemptRecord) error {
	body, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, body, 0o600)
}

func (s *loginProtectionService) prune(records map[string]loginAttemptRecord) map[string]loginAttemptRecord {
	result := map[string]loginAttemptRecord{}
	now := time.Now().Unix()
	ttl := int64(max(3600, s.config.BruteforceLockMinutes*120))
	for key, record := range records {
		if record.LockedUntil > now || record.LastFailedAt+ttl > now {
			result[key] = record
		}
	}
	return result
}
