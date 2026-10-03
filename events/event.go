package events

import (
	"errors"
	"regexp"
	"time"
)

const MaxPayload = 8192

type Kind string

const (
	KindPlaced    Kind = "order.placed"
	KindPaid      Kind = "order.paid"
	KindCancelled Kind = "order.cancelled"
)

var ErrDuplicate = errors.New("duplicate event")
var ErrInvalid = errors.New("invalid event")

type OrderEvent struct {
	ID       string
	TenantID string
	OrderID  string
	Kind     Kind
	Occurred time.Time
	Payload  []byte
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (e OrderEvent) Validate() error {
	if e.ID == "" || len(e.ID) > 128 || e.Occurred.IsZero() {
		return ErrInvalid
	}
	if !tokenPattern.MatchString(e.TenantID) || !tokenPattern.MatchString(e.OrderID) {
		return ErrInvalid
	}
	switch e.Kind {
	case KindPlaced, KindPaid, KindCancelled:
	default:
		return ErrInvalid
	}
	if len(e.Payload) > MaxPayload {
		return ErrInvalid
	}
	return nil
}

func Subject(tenantID string, kind Kind) string {
	return "orders." + tenantID + "." + string(kind)
}
