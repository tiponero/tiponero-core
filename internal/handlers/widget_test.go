package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

type fakePrice struct{}

func (fakePrice) ConvertXMRToFiat(int64) float64 { return 0 }
func (fakePrice) Currency() string               { return "USD" }

func newWidgetTestDB(t *testing.T, mode database.WidgetMode, presets string) (*database.DB, *database.User, *database.Widget) {
	t.Helper()

	db, err := database.NewDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().Unix()
	user := &database.User{
		Username:     "admin",
		PasswordHash: "hash",
		DisplayName:  "Creator",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	widget := &database.Widget{
		UserID:          user.ID,
		Name:            "Widget",
		Mode:            mode,
		PresetAmounts:   presets,
		ButtonText:      "Pay",
		CustomMessage:   "Message",
		ThankYouMessage: "Thanks",
		PrimaryColor:    "#ff6600",
		Theme:           database.ThemeSystem,
		ShowStats:       true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.CreateWidget(widget); err != nil {
		t.Fatalf("CreateWidget: %v", err)
	}

	return db, user, widget
}

func newFakeWalletRPC(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":"0","result":{"address":"fake-subaddress","address_index":1}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postDonation(t *testing.T, h *WidgetHandler, widgetID string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/widget/"+widgetID+"/donate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("widgetID", widgetID)

	rec := httptest.NewRecorder()
	h.CreateTransaction(rec, req)
	return rec
}

func TestCreateTransactionEnforcesPaymentPrice(t *testing.T) {
	db, user, widget := newWidgetTestDB(t, database.ModePayment, `[0.1]`)
	srv := newFakeWalletRPC(t)
	h := NewWidgetHandler(db, monero.NewClientHolder(monero.NewClient(srv.URL, "", "")), fakePrice{}, "http://example.test", 10, time.Hour, zerolog.Nop())

	rec := postDonation(t, h, widget.ID, url.Values{
		"amount":        {"0.001"},
		"custom_amount": {"0.001"},
		"donor_name":    {"Alice"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	txs, err := db.ListTransactions(user.ID, database.TransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("transactions = %d, want 1", len(txs))
	}
	if txs[0].Amount != 100000000000 {
		t.Fatalf("amount = %d, want 100000000000", txs[0].Amount)
	}
	if txs[0].ReceivedAmount != 0 {
		t.Fatalf("received amount = %d, want 0", txs[0].ReceivedAmount)
	}
}

func TestCreateTransactionKeepsCustomDonationAmount(t *testing.T) {
	db, user, widget := newWidgetTestDB(t, database.ModeDonation, `[0.01,0.05]`)
	srv := newFakeWalletRPC(t)
	h := NewWidgetHandler(db, monero.NewClientHolder(monero.NewClient(srv.URL, "", "")), fakePrice{}, "http://example.test", 10, time.Hour, zerolog.Nop())

	rec := postDonation(t, h, widget.ID, url.Values{"amount": {"0.25"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	txs, err := db.ListTransactions(user.ID, database.TransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("transactions = %d, want 1", len(txs))
	}
	if txs[0].Amount != 250000000000 {
		t.Fatalf("amount = %d, want 250000000000", txs[0].Amount)
	}
}

func TestCreateTransactionRejectsBrokenPaymentWidget(t *testing.T) {
	db, user, widget := newWidgetTestDB(t, database.ModePayment, ``)
	srv := newFakeWalletRPC(t)
	h := NewWidgetHandler(db, monero.NewClientHolder(monero.NewClient(srv.URL, "", "")), fakePrice{}, "http://example.test", 10, time.Hour, zerolog.Nop())

	rec := postDonation(t, h, widget.ID, url.Values{"amount": {"0.1"}})
	if !strings.Contains(rec.Body.String(), "This payment widget is not configured correctly.") {
		t.Fatalf("missing configuration error; body: %s", rec.Body.String())
	}

	txs, err := db.ListTransactions(user.ID, database.TransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(txs) != 0 {
		t.Fatalf("transactions = %d, want 0", len(txs))
	}
}

func TestParseXMRToPiconero(t *testing.T) {
	tests := []struct {
		name         string
		amount       string
		customAmount string
		want         int64
	}{
		{name: "preset", amount: "0.1", want: 100000000000},
		{name: "custom wins", amount: "0.1", customAmount: "0.25", want: 250000000000},
		{name: "empty", want: 0},
		{name: "invalid", amount: "abc", want: 0},
		{name: "nan", amount: "NaN", want: 0},
		{name: "infinity", amount: "Inf", want: 0},
		{name: "overflow", amount: "1e30", want: 0},
		{name: "zero", amount: "0", want: 0},
		{name: "negative", amount: "-1", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseXMRToPiconero(tt.amount, tt.customAmount)
			if got != tt.want {
				t.Fatalf("parseXMRToPiconero(%q, %q) = %d, want %d", tt.amount, tt.customAmount, got, tt.want)
			}
		})
	}
}

func TestWidgetPaymentAmount(t *testing.T) {
	tests := []struct {
		name    string
		presets string
		want    int64
		ok      bool
	}{
		{name: "single", presets: `[0.05]`, want: 50000000000, ok: true},
		{name: "smallest unit", presets: `[0.000000000001]`, want: 1, ok: true},
		{name: "two values", presets: `[0.05,0.1]`, want: 0, ok: false},
		{name: "zero", presets: `[0]`, want: 0, ok: false},
		{name: "overflow", presets: `[1e30]`, want: 0, ok: false},
		{name: "negative", presets: `[-1]`, want: 0, ok: false},
		{name: "bad json", presets: `not-json`, want: 0, ok: false},
		{name: "empty", presets: ``, want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := widgetPaymentAmount(&database.Widget{PresetAmounts: tt.presets})
			if got != tt.want || ok != tt.ok {
				t.Fatalf("widgetPaymentAmount(%q) = (%d, %v), want (%d, %v)", tt.presets, got, ok, tt.want, tt.ok)
			}
		})
	}
}
