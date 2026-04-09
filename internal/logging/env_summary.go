package logging

import (
	"sort"
	"strings"
)

// EnvInjectionSummary describes token-like env injection status for diagnostics.
type EnvInjectionSummary struct {
	TokenEnvConfigured bool
	TokenInjected      bool
	InjectedTokenKeys  []string
	EmptyTokenKeys     []string
}

// SummarizeEnvInjection inspects one env map and returns token injection summary.
func SummarizeEnvInjection(env map[string]string) EnvInjectionSummary {
	summary := EnvInjectionSummary{
		InjectedTokenKeys: make([]string, 0),
		EmptyTokenKeys:    make([]string, 0),
	}
	for rawKey, rawValue := range env {
		key := strings.TrimSpace(rawKey)
		if !isTokenLikeEnvKey(key) {
			continue
		}
		summary.TokenEnvConfigured = true
		if strings.TrimSpace(rawValue) == "" {
			summary.EmptyTokenKeys = append(summary.EmptyTokenKeys, key)
			continue
		}
		summary.TokenInjected = true
		summary.InjectedTokenKeys = append(summary.InjectedTokenKeys, key)
	}
	sort.Strings(summary.InjectedTokenKeys)
	sort.Strings(summary.EmptyTokenKeys)
	return summary
}

// isTokenLikeEnvKey matches one env key that likely carries credentials.
func isTokenLikeEnvKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if upper == "" {
		return false
	}
	if strings.Contains(upper, "TOKEN") {
		return true
	}
	if strings.Contains(upper, "API_KEY") {
		return true
	}
	return strings.Contains(upper, "AUTH")
}
