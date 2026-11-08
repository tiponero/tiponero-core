package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
	"github.com/tiponero/tiponero-core/internal/services"
)

// UpdateWidget edits every widget field from the admin panel. The API path and
// this path write the same columns, so both must validate the same rules.
func (h *AdminHandler) UpdateWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	widget, err := h.db.GetWidget(id)
	if err != nil || widget.UserID != userID {
		http.Error(w, "Widget not found", http.StatusNotFound)
		return
	}

	if err := h.populateWidgetFromForm(widget, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateWidgetPrice(widget); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.db.UpdateWidget(widget); err != nil {
		h.log.Error().Err(err).Msg("failed to update widget")
		http.Error(w, "Failed to update widget", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/widgets?toast=updated", http.StatusSeeOther)
}

// populateWidgetFromForm reads every widget field from the admin form. Both
// create and update use it so the two paths cannot drift apart.
func (h *AdminHandler) populateWidgetFromForm(widget *database.Widget, r *http.Request) error {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return fmt.Errorf("widget name is required")
	}

	mode := parseMode(r.FormValue("mode"))
	widget.Name = name
	widget.Mode = mode
	widget.ButtonText = strings.TrimSpace(r.FormValue("button_text"))
	widget.CustomMessage = strings.TrimSpace(r.FormValue("custom_message"))
	widget.ThankYouMessage = strings.TrimSpace(r.FormValue("thank_you_message"))
	widget.PrimaryColor = strings.TrimSpace(r.FormValue("primary_color"))
	widget.Theme = parseTheme(r.FormValue("theme"))
	widget.ShowStats = r.FormValue("show_stats") == "on"
	widget.RedirectURL = strings.TrimSpace(r.FormValue("redirect_url"))

	if widget.ButtonText == "" {
		if mode == database.ModePayment {
			widget.ButtonText = "Pay"
		} else {
			widget.ButtonText = "Donate"
		}
	}
	if widget.CustomMessage == "" {
		widget.CustomMessage = "Your support is appreciated!"
	}
	if widget.ThankYouMessage == "" {
		if mode == database.ModePayment {
			widget.ThankYouMessage = "Thank you for your payment!"
		} else {
			widget.ThankYouMessage = "Thank you for your donation!"
		}
	}
	if widget.PrimaryColor == "" {
		widget.PrimaryColor = "#ff6600"
	} else if !validHexColor.MatchString(widget.PrimaryColor) {
		return fmt.Errorf("invalid color format, expected #RRGGBB")
	}

	widget.Confirmations = parseConfirmations(r.FormValue("confirmations"))

	priceFiat, err := parseFloatField(r.FormValue("price_fiat"))
	if err != nil {
		return fmt.Errorf("fiat price must be a number")
	}
	widget.PriceFiat = priceFiat
	widget.PriceCurrency = strings.ToUpper(strings.TrimSpace(r.FormValue("price_currency")))
	if widget.PriceFiat > 0 {
		if widget.PriceCurrency == "" {
			widget.PriceCurrency = h.price.Currency()
		}
		if !services.IsSupportedCurrency(widget.PriceCurrency) {
			return fmt.Errorf("unsupported currency")
		}
	} else {
		widget.PriceCurrency = ""
	}

	goalAmount, err := parseFloatField(r.FormValue("goal_amount"))
	if err != nil {
		return fmt.Errorf("goal must be a number")
	}
	widget.GoalAmount = goalAmount
	widget.GoalCurrency = strings.ToUpper(strings.TrimSpace(r.FormValue("goal_currency")))
	if widget.GoalCurrency == "" {
		widget.GoalCurrency = "XMR"
	}
	if widget.GoalAmount > 0 && widget.GoalCurrency != "XMR" && !services.IsSupportedCurrency(widget.GoalCurrency) {
		return fmt.Errorf("unsupported goal currency")
	}
	if widget.GoalAmount <= 0 {
		widget.GoalCurrency = "XMR"
		widget.GoalDeadline = 0
	} else {
		widget.GoalDeadline = parseDateField(r.FormValue("goal_deadline"))
	}

	if mode == database.ModePayment {
		if widget.PriceFiat > 0 {
			widget.PresetAmounts = ""
		} else {
			price, err := parseFloatField(r.FormValue("payment_price"))
			if err != nil || price <= 0 {
				return fmt.Errorf("payment mode requires a price")
			}
			b, _ := json.Marshal([]float64{price})
			widget.PresetAmounts = string(b)
		}
		return nil
	}

	presetAmounts := r.FormValue("preset_amounts")
	if presetAmounts == "" {
		widget.PresetAmounts = ""
		return nil
	}
	var amounts []float64
	if err := json.Unmarshal([]byte(presetAmounts), &amounts); err != nil {
		return fmt.Errorf("preset amounts must be a JSON array")
	}
	valid := make([]float64, 0, len(amounts))
	for _, a := range amounts {
		if a > 0 {
			valid = append(valid, a)
		}
	}
	b, _ := json.Marshal(valid)
	widget.PresetAmounts = string(b)
	return nil
}

// validateWidgetPrice keeps a payment widget from existing without a price.
func validateWidgetPrice(widget *database.Widget) error {
	if widget.Mode != database.ModePayment {
		return nil
	}
	if widget.PriceFiat > 0 {
		return nil
	}
	var amounts []float64
	_ = json.Unmarshal([]byte(widget.PresetAmounts), &amounts)
	if len(amounts) != 1 || amounts[0] <= 0 {
		return fmt.Errorf("payment mode requires a price")
	}
	return nil
}

// CreateViewOnlyWallet turns the settings page into a setup wizard. The
// private view key goes straight to monero-wallet-rpc and is never stored.
func (h *AdminHandler) CreateViewOnlyWallet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	rpcURL := monero.NormalizeRPCURL(r.FormValue("rpc_url"))
	rpcUser := strings.TrimSpace(r.FormValue("rpc_user"))
	rpcPassword := r.FormValue("rpc_password")
	address := strings.TrimSpace(r.FormValue("address"))
	viewKey := strings.TrimSpace(r.FormValue("viewkey"))
	filename := strings.TrimSpace(r.FormValue("filename"))
	walletPassword := r.FormValue("wallet_password")

	if rpcURL == "" {
		triggerToast(w, "error", "Set the wallet RPC URL first.")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if address == "" || viewKey == "" || filename == "" {
		triggerToast(w, "error", "Address, view key, and wallet file name are required.")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(viewKey) != 64 {
		triggerToast(w, "error", "The private view key must be 64 hexadecimal characters.")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var restoreHeight uint64
	if v := strings.TrimSpace(r.FormValue("restore_height")); v != "" {
		height, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			triggerToast(w, "error", "Restore height must be a block number.")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		restoreHeight = height
	}

	client := monero.NewClient(rpcURL, rpcUser, rpcPassword)

	// generate_from_keys with no spend key creates a view-only wallet.
	if err := client.GenerateFromKeys(monero.GenerateFromKeysParams{
		Filename:        filename,
		Address:         address,
		ViewKey:         viewKey,
		Password:        walletPassword,
		RestoreHeight:   restoreHeight,
		AutosaveCurrent: true,
	}); err != nil {
		h.log.Error().Err(err).Msg("failed to generate view-only wallet")
		triggerToast(w, "error", "The wallet RPC rejected the request: "+err.Error())
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	if err := client.OpenWallet(filename, walletPassword); err != nil {
		h.log.Error().Err(err).Msg("failed to open generated wallet")
		triggerToast(w, "error", "The wallet was created but could not be opened: "+err.Error())
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	cfg := &database.WalletConfig{
		UserID:         userID,
		RPCURL:         rpcURL,
		RPCUser:        rpcUser,
		RPCPassword:    rpcPassword,
		WalletFile:     filename,
		WalletPassword: walletPassword,
	}
	if err := h.db.SaveWalletConfig(h.walletEncKey, cfg); err != nil {
		h.log.Error().Err(err).Msg("failed to save wallet config after generation")
		triggerToast(w, "error", "The wallet was created but the configuration was not saved.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	h.wallet.Set(client)
	triggerToast(w, "success", "View-only wallet created and connected. Tiponero never stored your view key.")
	w.WriteHeader(http.StatusOK)
}

// MockPay marks a transaction as paid without touching the network. It only
// works when MOCK_PAYMENTS=true, and the row is excluded from stats.
func (h *AdminHandler) MockPay(w http.ResponseWriter, r *http.Request) {
	if !h.mockPayments {
		http.Error(w, "Mock payments are disabled", http.StatusNotFound)
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	tx, err := h.db.GetTransaction(id)
	if err != nil || tx.UserID != userID {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}
	if tx.Status == database.StatusConfirmed {
		http.Redirect(w, r, fmt.Sprintf("/admin/transactions/%s", tx.ID), http.StatusSeeOther)
		return
	}

	required := requiredConfirmationsFor(tx, h.requiredConfirmations)
	received := tx.Amount
	if received == 0 {
		received = 1000000000
	}

	if err := h.db.UpdateTransactionStatus(tx.ID, database.StatusConfirmed, required, "mock", received); err != nil {
		h.log.Error().Err(err).Msg("failed to mock a payment")
		http.Error(w, "Failed to mock payment", http.StatusInternalServerError)
		return
	}

	updated, err := h.db.GetTransaction(tx.ID)
	if err == nil {
		h.dispatcher.EmitAsync(updated, database.EventPaymentConfirmed, required)
	}

	http.Redirect(w, r, fmt.Sprintf("/admin/transactions/%s?toast=mock", tx.ID), http.StatusSeeOther)
}

func parseConfirmations(raw string) int {
	if raw == "" {
		return -1
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	if value < 0 {
		return -1
	}
	if value > 100 {
		return 100
	}
	return value
}

func parseFloatField(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("negative value")
	}
	return value, nil
}

func parseDateField(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return 0
	}
	return t.Unix()
}
