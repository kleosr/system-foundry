package reconciliation

import "time"

func NextDelay(attempt int, unit float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	base := 100 * time.Millisecond * time.Duration(1<<shift)
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	if unit < 0 {
		unit = 0
	}
	if unit > 1 {
		unit = 1
	}
	jitter := time.Duration(unit * float64(base/5))
	return base + jitter
}
