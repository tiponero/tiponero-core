package database

import "testing"

func TestJoinAndSplitEvents(t *testing.T) {
	joined := JoinEvents([]string{"payment.confirmed", " payment.created ", "payment.confirmed", ""})
	if joined != "payment.confirmed,payment.created" {
		t.Fatalf("joined = %q", joined)
	}

	parts := SplitEvents(joined)
	if len(parts) != 2 {
		t.Fatalf("split returned %d parts", len(parts))
	}
	if !Subscribes(joined, EventPaymentConfirmed) {
		t.Fatal("expected subscription to payment.confirmed")
	}
	if Subscribes(joined, EventPaymentExpired) {
		t.Fatal("unexpected subscription to payment.expired")
	}
}

func TestSplitEventsEmpty(t *testing.T) {
	if got := SplitEvents("   "); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
