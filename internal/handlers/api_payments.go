package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/crypto"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/services"
)

const (
	maxMetadataBytes = 4096
	minExpiry        = 60
	maxExpiry        = 30 * 24 * 3600
)

// CreateTransaction creates a payment request. A widget is optional: without
// one the request is standalone and gets its own hosted payment page.
func (h *APIHandler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var body struct {
		WidgetID      string          `json:"widget_id"`
		Amount        *float64        `json:"amount"`
		AmountXMR     *float64        `json:"amount_xmr"`
		AmountFiat    *float64        `json:"amount_fiat"`
		Currency      string          `json:"currency"`
		Description   string          `json:"description"`
		DonorName     string          `json:"donor_name"`
		Note          string          `json:"note"`
		RedirectURL   string          `json:"redirect_url"`
		Metadata      json.RawMessage `json:"metadata"`
		ExpiresIn     int64           `json:"expires_in"`
		Confirmations *int            `json:"confirmations"`
		IsTest        bool            `json:"is_test"`
	}
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var widget *database.Widget
	if body.WidgetID != "" {
		found, err := h.db.GetWidget(body.WidgetID)
		if err != nil || found.UserID != userID {
			apiError(w, http.StatusNotFound, "widget not found")
			return
		}
		widget = found
	}

	if body.RedirectURL != "" {
		if err := validateEndpointURL(body.RedirectURL); err != nil {
			apiError(w, http.StatusBadRequest, "redirect_url is not valid: "+err.Error())
			return
		}
	}

	if len(body.Metadata) > maxMetadataBytes {
		apiError(w, http.StatusBadRequest, "metadata is larger than 4 KiB")
		return
	}
	if len(body.Metadata) > 0 && !json.Valid(body.Metadata) {
		apiError(w, http.StatusBadRequest, "metadata is not valid JSON")
		return
	}

	amount, fiatAmount, fiatCurrency, err := h.resolveAmount(body.Amount, body.AmountXMR, body.AmountFiat, body.Currency, widget)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	confirmations := -1
	if body.Confirmations != nil {
		confirmations = *body.Confirmations
		if confirmations < 0 || confirmations > 100 {
			apiError(w, http.StatusUnprocessableEntity, "confirmations must be between 0 and 100")
			return
		}
	}

	expiry := h.transactionExpiry
	if body.ExpiresIn != 0 {
		if body.ExpiresIn < minExpiry || body.ExpiresIn > maxExpiry {
			apiError(w, http.StatusUnprocessableEntity, fmt.Sprintf("expires_in must be between %d and %d seconds", minExpiry, maxExpiry))
			return
		}
		expiry = time.Duration(body.ExpiresIn) * time.Second
	}

	redirect := body.RedirectURL
	if redirect == "" && widget != nil {
		redirect = widget.RedirectURL
	}

	isPayment := amount > 0
	if widget != nil {
		isPayment = widget.Mode == database.ModePayment
	}

	tx, err := createPayment(h.db, h.wallet, paymentRequest{
		UserID:                userID,
		Widget:                widget,
		Amount:                amount,
		FiatAmount:            fiatAmount,
		FiatCurrency:          fiatCurrency,
		IsPayment:             isPayment,
		Description:           strings.TrimSpace(body.Description),
		DonorName:             strings.TrimSpace(body.DonorName),
		Note:                  strings.TrimSpace(body.Note),
		RedirectURL:           redirect,
		Metadata:              string(body.Metadata),
		ConfirmationsRequired: confirmations,
		Expiry:                expiry,
		IsTest:                body.IsTest,
	})
	if err != nil {
		if err == errWalletUnavailable || strings.Contains(err.Error(), errWalletUnavailable.Error()) {
			apiError(w, http.StatusServiceUnavailable, "wallet is not configured")
			return
		}
		h.log.Error().Err(err).Msg("api: failed to create transaction")
		apiError(w, http.StatusInternalServerError, "failed to create transaction")
		return
	}

	required := requiredConfirmationsFor(tx, h.requiredConfirmations)
	h.dispatcher.EmitAsync(tx, database.EventPaymentCreated, required)

	writeJSON(w, http.StatusCreated, map[string]any{"data": h.transactionResponse(tx, required)})
}

func (h *APIHandler) resolveAmount(amount, amountXMR, amountFiat *float64, currency string, widget *database.Widget) (int64, float64, string, error) {
	xmrValue := amount
	if xmrValue == nil {
		xmrValue = amountXMR
	}

	switch {
	case xmrValue != nil && amountFiat != nil:
		return 0, 0, "", fmt.Errorf("send either amount_xmr or amount_fiat, not both")

	case xmrValue != nil:
		if *xmrValue <= 0 {
			return 0, 0, "", fmt.Errorf("amount_xmr must be greater than zero")
		}
		piconero := int64(*xmrValue*1e12 + 0.5)
		fiat, _ := h.price.ConvertXMRToFiatIn(piconero, h.price.Currency())
		return piconero, fiat, h.price.Currency(), nil

	case amountFiat != nil:
		if *amountFiat <= 0 {
			return 0, 0, "", fmt.Errorf("amount_fiat must be greater than zero")
		}
		code := strings.ToUpper(strings.TrimSpace(currency))
		if code == "" {
			code = h.price.Currency()
		}
		piconero, _, err := h.price.ConvertFiatToPiconero(*amountFiat, code)
		if err != nil {
			return 0, 0, "", fmt.Errorf("cannot convert %s to XMR: %w", code, err)
		}
		return piconero, *amountFiat, code, nil
	}

	// No amount supplied. A payment widget keeps its configured price.
	if widget != nil && widget.Mode == database.ModePayment {
		if widget.PriceFiat > 0 {
			code := widget.PriceCurrency
			if code == "" {
				code = h.price.Currency()
			}
			piconero, _, err := h.price.ConvertFiatToPiconero(widget.PriceFiat, code)
			if err != nil {
				return 0, 0, "", fmt.Errorf("cannot convert the widget price: %w", err)
			}
			return piconero, widget.PriceFiat, code, nil
		}
		preset, ok := widgetPaymentAmount(widget)
		if !ok {
			return 0, 0, "", fmt.Errorf("the payment widget has no valid price")
		}
		fiat, _ := h.price.ConvertXMRToFiatIn(preset, h.price.Currency())
		return preset, fiat, h.price.Currency(), nil
	}

	// A donation widget or a standalone request may be open-ended.
	return 0, 0, "", nil
}

func (h *APIHandler) transactionResponse(tx *database.Transaction, required int) map[string]any {
	out := map[string]any{
		"id":                     tx.ID,
		"widget_id":              tx.WidgetID.String,
		"status":                 string(tx.Status),
		"subaddress":             tx.Subaddress,
		"amount":                 tx.Amount,
		"amount_xmr":             xmrString(tx.Amount),
		"received_amount":        tx.ReceivedAmount,
		"received_amount_xmr":    xmrString(tx.ReceivedAmount),
		"remaining_amount":       remainingAmount(tx),
		"remaining_amount_xmr":   xmrString(remainingAmount(tx)),
		"fiat_amount":            tx.FiatAmount,
		"fiat_currency":          tx.FiatCurrency,
		"description":            tx.Description,
		"confirmations":          tx.Confirmations,
		"confirmations_required": required,
		"is_payment":             tx.IsPayment,
		"is_test":                tx.IsTest,
		"created_at":             tx.CreatedAt,
		"expires_at":             tx.ExpiresAt,
		"monero_uri":             services.MoneroURI(tx.Subaddress, tx.Amount),
	}
	if tx.WidgetID.Valid {
		out["payment_url"] = fmt.Sprintf("%s/widget/%s", h.hostURL, tx.WidgetID.String)
		out["qr_url"] = fmt.Sprintf("%s/widget/qr/%s", h.hostURL, tx.ID)
		out["status_url"] = fmt.Sprintf("%s/widget/%s/donation/%s/status", h.hostURL, tx.WidgetID.String, tx.ID)
	} else {
		out["payment_url"] = fmt.Sprintf("%s/pay/%s", h.hostURL, tx.ID)
		out["qr_url"] = fmt.Sprintf("%s/pay/%s/qr", h.hostURL, tx.ID)
		out["status_url"] = fmt.Sprintf("%s/pay/%s/status", h.hostURL, tx.ID)
	}
	return out
}

func xmrString(piconero int64) string {
	return fmt.Sprintf("%.12f", float64(piconero)/1e12)
}

func (h *APIHandler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	hooks, err := h.db.ListWebhooks(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to list webhooks")
		apiError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}

	out := make([]map[string]any, 0, len(hooks))
	for _, hook := range hooks {
		out = append(out, map[string]any{
			"id":         hook.ID,
			"name":       hook.Name,
			"url":        hook.URL,
			"events":     database.SplitEvents(hook.Events),
			"active":     hook.Active,
			"created_at": hook.CreatedAt,
			"updated_at": hook.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *APIHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var body struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if strings.TrimSpace(body.Name) == "" {
		apiError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validateEndpointURL(body.URL); err != nil {
		apiError(w, http.StatusBadRequest, "url is not valid: "+err.Error())
		return
	}

	events := body.Events
	if len(events) == 0 {
		events = database.AllEvents
	}
	for _, e := range events {
		if !knownEvent(e) {
			apiError(w, http.StatusBadRequest, "unknown event: "+e)
			return
		}
	}

	secret := randomToken()
	secretEnc, err := crypto.Encrypt(h.notifyEncKey, secret)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to encrypt webhook secret")
		apiError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	now := time.Now().Unix()
	hook := &database.Webhook{
		UserID:    userID,
		Name:      strings.TrimSpace(body.Name),
		URL:       body.URL,
		SecretEnc: secretEnc,
		Events:    database.JoinEvents(events),
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.db.CreateWebhook(hook); err != nil {
		h.log.Error().Err(err).Msg("api: failed to create webhook")
		apiError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"id":     hook.ID,
		"name":   hook.Name,
		"url":    hook.URL,
		"events": database.SplitEvents(hook.Events),
		"secret": secret,
		"note":   "Store this secret now. It is not shown again.",
	}})
}

func (h *APIHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.db.DeleteWebhook(id, userID); err != nil {
		h.log.Error().Err(err).Msg("api: failed to delete webhook")
		apiError(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func knownEvent(event string) bool {
	for _, e := range database.AllEvents {
		if e == event {
			return true
		}
	}
	return false
}
