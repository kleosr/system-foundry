package ratelimit

import "regexp"

var tenantPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func Key(tenantID string) (string, bool) {
	if !tenantPattern.MatchString(tenantID) {
		return "", false
	}
	return "ratelimit:tenant:" + tenantID + ":v1", true
}
