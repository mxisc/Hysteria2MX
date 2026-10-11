package cpuprotection

import "errors"

type Config struct {
	Enabled         bool    `json:"enabled"`
	HighPercent     float64 `json:"high_percent"`
	LowPercent      float64 `json:"low_percent"`
	MaxQuotaPercent float64 `json:"max_quota_percent"`
	MinQuotaPercent float64 `json:"min_quota_percent"`
}

func Normalize(c Config) (Config, error) {
	if c.HighPercent == 0 {
		c.HighPercent = 90
	}
	if c.LowPercent == 0 {
		c.LowPercent = 70
	}
	if c.MaxQuotaPercent == 0 {
		c.MaxQuotaPercent = 80
	}
	if c.MinQuotaPercent == 0 {
		c.MinQuotaPercent = 20
	}
	if c.HighPercent > 100 || c.HighPercent <= c.LowPercent || c.LowPercent <= 0 || c.MaxQuotaPercent > 100 || c.MaxQuotaPercent <= 0 || c.MinQuotaPercent <= 0 || c.MinQuotaPercent > c.MaxQuotaPercent {
		return c, errors.New("CPU保护参数无效：恢复阈值须低于触发阈值，配额范围须在0到100之间")
	}
	return c, nil
}
