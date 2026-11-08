package services

import (
	"strings"
	"testing"

	"github.com/tiponero/tiponero-core/internal/database"
)

func TestSignPayloadMatchesReceiverComputation(t *testing.T) {
	// Expected value computed independently with Python hmac:
	// hmac.new(b"whsec_test", b"1700000000." + body, sha256).hexdigest()
	got := signPayload("whsec_test", "1700000000", []byte(`{"event":"payment.confirmed"}`))
	want := "sha256=b475b4a778fb9972c6ca81c4c80579c37f8909f7e8a2dabdb3d108802032ebef"
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestSignPayloadChangesWithBody(t *testing.T) {
	a := signPayload("s", "1", []byte(`{"a":1}`))
	b := signPayload("s", "1", []byte(`{"a":2}`))
	if a == b {
		t.Fatal("different bodies produced the same signature")
	}
}

func TestEventMessageIncludesRemainingAmount(t *testing.T) {
	tx := &database.Transaction{
		ID:             "tx",
		Status:         database.StatusPartial,
		Amount:         100000000000,
		ReceivedAmount: 40000000000,
		IsPayment:      true,
	}
	event := BuildEvent(tx, database.EventPaymentPartial, 10, "https://pay.example")
	msg := event.Message()

	if event.Data.RemainingAmount != 60000000000 {
		t.Fatalf("remaining = %d, want 60000000000", event.Data.RemainingAmount)
	}
	for _, want := range []string{"partial", "0.04", "0.06", "https://pay.example/pay/tx"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not contain %q", msg, want)
		}
	}
}
