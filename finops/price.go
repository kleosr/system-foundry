package finops

func CostMicros(model string, tokens int) int64 {
	if tokens <= 0 {
		return 0
	}
	per1K := pricePer1K(model)
	return per1K * int64(tokens) / 1000
}

type PriceTable map[string]int64

func (t PriceTable) CostMicros(model string, tokens int) int64 {
	if tokens <= 0 {
		return 0
	}
	per1K, ok := t[model]
	if !ok {
		for _, price := range t {
			per1K = max(per1K, price)
		}
	}
	if per1K == 0 {
		per1K = pricePer1K(model)
	}
	return per1K * int64(tokens) / 1000
}

func pricePer1K(model string) int64 {
	switch model {
	case "primary":
		return 30_000
	case "cheap":
		return 1_000
	default:
		return 30_000
	}
}
