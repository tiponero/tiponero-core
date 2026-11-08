package widget

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/tiponero/tiponero-core/internal/database"
)

func renderStatus(t *testing.T, tx *database.Transaction, required int, message string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Status(tx, required, message).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render status: %v", err)
	}
	return buf.String()
}

func TestStatusUsesConfiguredConfirmations(t *testing.T) {
	tx := &database.Transaction{
		ID:            "tx",
		WidgetID:      sql.NullString{String: "widget", Valid: true},
		Status:        database.StatusConfirming,
		Confirmations: 3,
		Amount:        100,
	}
	html := renderStatus(t, tx, 5, "")

	if !strings.Contains(html, "3/5 confirmations") {
		t.Fatalf("missing configured confirmation text:\n%s", html)
	}
	if !strings.Contains(html, "width: 60%") {
		t.Fatalf("missing 60 percent progress bar:\n%s", html)
	}
}

func TestStatusRendersThankYouMessage(t *testing.T) {
	tx := &database.Transaction{
		ID:       "tx",
		WidgetID: sql.NullString{String: "widget", Valid: true},
		Status:   database.StatusConfirmed,
		Amount:   100,
	}
	html := renderStatus(t, tx, 10, "Custom thanks")

	if !strings.Contains(html, "Custom thanks") {
		t.Fatalf("missing custom thank you message:\n%s", html)
	}
}

func TestStatusRendersUnderpaid(t *testing.T) {
	tx := &database.Transaction{
		ID:             "tx",
		WidgetID:       sql.NullString{String: "widget", Valid: true},
		Status:         database.StatusUnderpaid,
		Amount:         100000000000,
		ReceivedAmount: 50000000000,
	}
	html := renderStatus(t, tx, 10, "")

	if !strings.Contains(html, "Payment amount is too low") {
		t.Fatalf("missing underpaid message:\n%s", html)
	}
	if !strings.Contains(html, "Required 0.1 XMR. Received 0.05 XMR.") {
		t.Fatalf("missing amount details:\n%s", html)
	}
}
