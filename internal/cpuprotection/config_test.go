package cpuprotection

import "testing"

func TestThresholdValidation(t *testing.T) {
	cfg, e := Normalize(Config{})
	if e != nil || cfg.Enabled || cfg.HighPercent != 90 || cfg.LowPercent != 70 {
		t.Fatalf("defaults: %+v %v", cfg, e)
	}
	for _, cfg := range []Config{{HighPercent: 70, LowPercent: 70}, {HighPercent: -1}, {LowPercent: 101}, {MaxQuotaPercent: -1}, {MinQuotaPercent: 81, MaxQuotaPercent: 80}} {
		if _, e = Normalize(cfg); e == nil {
			t.Fatalf("accepted invalid config %+v", cfg)
		}
	}
	cfg, e = Normalize(Config{Enabled: true, HighPercent: 95, LowPercent: 65, MaxQuotaPercent: 75, MinQuotaPercent: 15})
	if e != nil || !cfg.Enabled || cfg.HighPercent != 95 || cfg.MaxQuotaPercent != 75 {
		t.Fatalf("changed valid configuration %+v %v", cfg, e)
	}
}
