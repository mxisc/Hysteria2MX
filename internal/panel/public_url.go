package panel

import (
	"net/http"
	"strings"
)

func normalizePublicSiteBaseURL(raw string) string {
	baseURL := strings.TrimSpace(raw)
	if baseURL == "" {
		return ""
	}
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/api")
	return strings.TrimRight(baseURL, "/")
}

func normalizePublicAPIBaseURL(raw string) string {
	siteBaseURL := normalizePublicSiteBaseURL(raw)
	if siteBaseURL == "" {
		return ""
	}
	return siteBaseURL + "/api"
}

func resolvePublicSiteBaseURL(request *http.Request, cfg *Config) string {
	if cfg != nil {
		if configured := normalizePublicSiteBaseURL(cfg.PublicAPIBaseURL); configured != "" {
			return configured
		}
	}
	if request == nil {
		return ""
	}
	return normalizePublicSiteBaseURL(resolvePublicBaseURL(request))
}

func resolvePublicAPIBaseURL(request *http.Request, cfg *Config) string {
	if cfg != nil {
		if configured := normalizePublicAPIBaseURL(cfg.PublicAPIBaseURL); configured != "" {
			return configured
		}
	}
	if request == nil {
		return ""
	}
	return normalizePublicAPIBaseURL(resolvePublicBaseURL(request))
}
