package chaos

type Sample struct {
	Bad   int64
	Total int64
}

func BurnRate(sample Sample, objective float64) float64 {
	if sample.Total <= 0 || objective <= 0 || objective >= 1 {
		return 0
	}
	budget := 1 - objective
	errorRate := float64(sample.Bad) / float64(sample.Total)
	return errorRate / budget
}

func ShouldPage(shortRate, longRate float64) bool {
	return shortRate > 14.4 && longRate > 1
}
