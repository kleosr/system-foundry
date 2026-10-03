package auth

import "regexp"

var (
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	phonePattern  = regexp.MustCompile(`\b(?:\+?1[-.\s]?)?(?:\(?\d{3}\)?[-.\s]?)\d{3}[-.\s]?\d{4}\b`)
	ssnPattern    = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	cardPattern   = regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)
	bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-._~+/]+=*`)
)

func Redact(text string) string {
	text = bearerPattern.ReplaceAllString(text, "[redacted-secret]")
	text = emailPattern.ReplaceAllString(text, "[redacted-email]")
	text = ssnPattern.ReplaceAllString(text, "[redacted-ssn]")
	text = phonePattern.ReplaceAllString(text, "[redacted-phone]")
	text = cardPattern.ReplaceAllString(text, "[redacted-card]")
	return text
}
