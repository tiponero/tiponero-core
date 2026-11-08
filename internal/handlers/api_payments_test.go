package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

func newAPITestHandler(t *testing.T) (*APIHandler, *database.DB, *database.User, *database.Widget) {
	t.Helper()
	db, user, widget := newWidgetTestDB(t, database.ModePayment, `[0.1]`)
	srv := newFakeWalletRPC(t)
	return NewAPIHandler(Deps{
		DB:                    db,
		Wallet:                monero.NewClientHolder(monero.NewClient(srv.URL, "", "")),
		Price:                 fakePrice{},
		HostURL:               "http://example.test",
		RequiredConfirmations: 10,
		TransactionExpiry:     time.Hour,
		Log:                   zerolog.Nop(),
	}), db, user, widget
}

func postTransaction(t *testing.T, h *APIHandler, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	req = req.WithContext(auth.ContextWithUserID(context.Background(), userID))
	rec := httptest.NewRecorder()
	h.CreateTransaction(rec, req)
	return rec
}

func TestCreateTransactionWithoutWidget(t *testing.T) {
	h, _, user, _ := newAPITestHandler(t)

	rec := postTransaction(t, h, user.ID, `{"amount_xmr":0.25,"description":"Order 7"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if payload.Data["widget_id"] != "" {
		t.Fatalf("widget_id = %v, want empty", payload.Data["widget_id"])
	}
	if payload.Data["amount"].(float64) != 250000000000 {
		t.Fatalf("amount = %v", payload.Data["amount"])
	}
	if payload.Data["status"] != "pending" {
		t.Fatalf("status = %v", payload.Data["status"])
	}
	if payload.Data["confirmations_required"].(float64) != 10 {
		t.Fatalf("confirmations_required = %v", payload.Data["confirmations_required"])
	}
	if !strings.Contains(payload.Data["payment_url"].(string), "/pay/") {
		t.Fatalf("payment_url = %v", payload.Data["payment_url"])
	}
}

func TestCreateTransactionRejectsBothAmounts(t *testing.T) {
	h, _, user, _ := newAPITestHandler(t)

	rec := postTransaction(t, h, user.ID, `{"amount_xmr":0.1,"amount_fiat":5}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestCreateTransactionRejectsUnknownWidget(t *testing.T) {
	h, _, user, _ := newAPITestHandler(t)

	rec := postTransaction(t, h, user.ID, `{"widget_id":"does-not-exist","amount_xmr":0.1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCreateTransactionUsesWidgetPreset(t *testing.T) {
	h, _, user, widget := newAPITestHandler(t)

	rec := postTransaction(t, h, user.ID, `{"widget_id":"`+widget.ID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload.Data["amount"].(float64) != 100000000000 {
		t.Fatalf("amount = %v, want the 0.1 XMR preset", payload.Data["amount"])
	}
	if payload.Data["widget_id"] != widget.ID {
		t.Fatalf("widget_id = %v", payload.Data["widget_id"])
	}
}

func TestCreateTransactionRejectsBadConfirmations(t *testing.T) {
	h, _, user, _ := newAPITestHandler(t)

	rec := postTransaction(t, h, user.ID, `{"amount_xmr":0.1,"confirmations":500}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestCreateTransactionAcceptsZeroConf(t *testing.T) {
	h, db, user, _ := newAPITestHandler(t)

	rec := postTransaction(t, h, user.ID, `{"amount_xmr":0.1,"confirmations":0}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	tx, err := db.GetTransaction(payload.Data["id"].(string))
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if tx.ConfirmationsRequired != 0 {
		t.Fatalf("ConfirmationsRequired = %d, want 0", tx.ConfirmationsRequired)
	}
}
