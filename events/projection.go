package events

import "slices"

type OrderStatus string

const (
	StatusUnknown   OrderStatus = ""
	StatusPlaced    OrderStatus = "placed"
	StatusPaid      OrderStatus = "paid"
	StatusCancelled OrderStatus = "cancelled"
)

type OrderView struct {
	TenantID string      `json:"tenantId"`
	OrderID  string      `json:"orderId"`
	Status   OrderStatus `json:"status"`
	Applied  []string    `json:"applied"`
}

func statusFor(kind Kind) OrderStatus {
	switch kind {
	case KindPlaced:
		return StatusPlaced
	case KindPaid:
		return StatusPaid
	case KindCancelled:
		return StatusCancelled
	default:
		return StatusUnknown
	}
}

func precedence(status OrderStatus) int {
	switch status {
	case StatusPlaced:
		return 1
	case StatusPaid:
		return 2
	case StatusCancelled:
		return 3
	default:
		return 0
	}
}

func Fold(view OrderView, event OrderEvent) (OrderView, bool) {
	if slices.Contains(view.Applied, event.ID) {
		return view, false
	}
	next := OrderView{
		TenantID: event.TenantID,
		OrderID:  event.OrderID,
		Status:   view.Status,
		Applied:  append(slices.Clone(view.Applied), event.ID),
	}
	if candidate := statusFor(event.Kind); precedence(candidate) > precedence(next.Status) {
		next.Status = candidate
	}
	return next, true
}
