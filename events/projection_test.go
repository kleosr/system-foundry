package events

import (
	"testing"
	"time"
)

func orderEvent(id string, kind Kind) OrderEvent {
	return OrderEvent{ID: id, TenantID: "acme", OrderID: "o-1", Kind: kind, Occurred: time.Unix(1_700_000_000, 0).UTC()}
}

func TestFoldIsOrderIndependent(t *testing.T) {
	placed, paid, cancelled := orderEvent("e1", KindPlaced), orderEvent("e2", KindPaid), orderEvent("e3", KindCancelled)
	for _, order := range [][]OrderEvent{
		{placed, paid, cancelled},
		{cancelled, paid, placed},
		{paid, cancelled, placed},
	} {
		var view OrderView
		for _, event := range order {
			view, _ = Fold(view, event)
		}
		if view.Status != StatusCancelled || len(view.Applied) != 3 {
			t.Fatalf("order %v -> %+v", order, view)
		}
	}
}

func TestFoldIgnoresRedelivery(t *testing.T) {
	view, changed := Fold(OrderView{}, orderEvent("e1", KindPaid))
	if !changed || view.Status != StatusPaid {
		t.Fatalf("first = %+v", view)
	}
	again, changed := Fold(view, orderEvent("e1", KindPaid))
	if changed || len(again.Applied) != 1 {
		t.Fatalf("redelivery changed view: %+v", again)
	}
}
