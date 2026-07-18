package audit

import (
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(token|secret|authorization|bearer)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)recovery[-_ ]?code\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)client[_-]?secret\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)api[_-]?key\s*[:=]\s*\S+`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), // JWT-shaped
}

var secretKeywords = []string{
	"BEGIN PRIVATE KEY",
	"BEGIN RSA PRIVATE KEY",
	"BEGIN OPENSSH PRIVATE KEY",
}

func containsSecretMarkers(s string) bool {
	if s == "" {
		return false
	}
	upper := strings.ToUpper(s)
	for _, k := range secretKeywords {
		if strings.Contains(upper, k) {
			return true
		}
	}
	for _, re := range secretPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// redactMessage strips known secret patterns from free-text messages.
func redactMessage(s string) string {
	out := s
	for _, re := range secretPatterns {
		out = re.ReplaceAllString(out, "[REDACTED]")
	}
	for _, k := range secretKeywords {
		if strings.Contains(strings.ToUpper(out), k) {
			return "[REDACTED]"
		}
	}
	return out
}

// SanitizeMessage is the exported redaction helper for call sites that still
// construct Event values directly.
func SanitizeMessage(s string) string {
	return redactMessage(s)
}
