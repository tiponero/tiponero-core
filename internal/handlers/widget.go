package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
	"github.com/tiponero/tiponero-core/internal/services"
	widgetviews "github.com/tiponero/tiponero-core/internal/views/widget"
)

type PriceConverter interface {
	ConvertXMRToFiat(int64) float64
	ConvertXMRToFiatIn(int64, string) (float64, error)
	ConvertFiatToPiconero(float64, string) (int64, float64, error)
	Currency() string
}

type WidgetHandler struct {
	db                    *database.DB
	wallet                *monero.ClientHolder
	price                 PriceConverter
	dispatcher            *services.Dispatcher
	log                   zerolog.Logger
	hostURL               string
	requiredConfirmations int
	transactionExpiry     time.Duration
	mockPayments          bool
}

func NewWidgetHandler(d Deps) *WidgetHandler {
	return &WidgetHandler{
		db:                    d.DB,
		wallet:                d.Wallet,
		price:                 d.Price,
		dispatcher:            d.Dispatcher,
		hostURL:               d.HostURL,
		requiredConfirmations: d.RequiredConfirmations,
		transactionExpiry:     d.TransactionExpiry,
		log:                   d.Log.With().Str("component", "widget").Logger(),
		mockPayments:          d.MockPayments,
	}
}

func (h *WidgetHandler) Home(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("widgetID")

	widget, err := h.db.GetWidget(widgetID)
	if err != nil {
		http.Error(w, "Widget not found", http.StatusNotFound)
		return
	}

	user, err := h.db.GetUser()
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load creator")
		http.Error(w, "Creator not found", http.StatusInternalServerError)
		return
	}

	goalReached, goalTarget := h.goalProgress(widget)
	render(r.Context(), w, widgetviews.Home(widget, user, goalReached, goalTarget), h.log)
}

func (h *WidgetHandler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("widgetID")

	widget, err := h.db.GetWidget(widgetID)
	if err != nil {
		http.Error(w, "Widget not found", http.StatusNotFound)
		return
	}

	user, err := h.db.GetUser()
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load creator")
		http.Error(w, "Creator not found", http.StatusInternalServerError)
		return
	}

	amountRequested := int64(0)
	fiatAmount := 0.0
	fiatCurrency := ""

	if widget.Mode == database.ModePayment {
		switch {
		case widget.PriceFiat > 0:
			// The widget price is fiat. Lock the rate now so the payer sees
			// the same number the merchant asked for.
			currency := widget.PriceCurrency
			if currency == "" {
				currency = h.price.Currency()
			}
			piconero, _, err := h.price.ConvertFiatToPiconero(widget.PriceFiat, currency)
			if err != nil {
				h.log.Error().Err(err).Msg("failed to convert widget fiat price")
				render(r.Context(), w, widgetviews.Error(widget, "The price is not available right now. Please try again later."), h.log)
				return
			}
			amountRequested = piconero
			fiatAmount = widget.PriceFiat
			fiatCurrency = currency
		default:
			preset, ok := widgetPaymentAmount(widget)
			if !ok {
				h.log.Error().Str("widget_id", widgetID).Msg("payment widget has no valid preset amount")
				render(r.Context(), w, widgetviews.Error(widget, "This payment widget is not configured correctly."), h.log)
				return
			}
			amountRequested = preset
			fiatAmount = h.price.ConvertXMRToFiat(preset)
			fiatCurrency = h.price.Currency()
		}
	} else {
		amountRequested = parseXMRToPiconero(r.FormValue("amount"), r.FormValue("custom_amount"))
		if amountRequested > 0 {
			fiatAmount = h.price.ConvertXMRToFiat(amountRequested)
			fiatCurrency = h.price.Currency()
		}
	}

	required := widgetRequiredConfirmations(widget, h.requiredConfirmations)

	tx, err := createPayment(h.db, h.wallet, paymentRequest{
		UserID:                user.ID,
		Widget:                widget,
		Amount:                amountRequested,
		FiatAmount:            fiatAmount,
		FiatCurrency:          fiatCurrency,
		IsPayment:             widget.Mode == database.ModePayment,
		DonorName:             r.FormValue("donor_name"),
		Note:                  r.FormValue("note"),
		ConfirmationsRequired: required,
		Expiry:                h.transactionExpiry,
	})
	if err != nil {
		if errors.Is(err, errWalletUnavailable) {
			h.log.Warn().Msg("transaction attempt but wallet RPC is not configured")
			render(r.Context(), w, widgetviews.Error(widget, "Payments are not available yet. The wallet has not been configured."), h.log)
		} else {
			h.log.Error().Err(err).Msg("failed to create transaction")
			render(r.Context(), w, widgetviews.Error(widget, "Wallet is currently unavailable. Please try again later."), h.log)
		}
		return
	}

	h.dispatcher.EmitAsync(tx, database.EventPaymentCreated, required)

	render(r.Context(), w, widgetviews.Payment(tx, widget, required), h.log)
}

func (h *WidgetHandler) TransactionStatus(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("widgetID")
	donationID := r.PathValue("donationID")

	widget, err := h.db.GetWidget(widgetID)
	if err != nil {
		http.Error(w, "Widget not found", http.StatusNotFound)
		return
	}

	tx, err := h.db.GetTransaction(donationID)
	if err != nil || !tx.WidgetID.Valid || tx.WidgetID.String != widgetID {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	redirect := widget.RedirectURL
	if tx.RedirectURL != "" {
		redirect = tx.RedirectURL
	}
	if tx.Status == database.StatusConfirmed && redirect != "" {
		w.Header().Set("HX-Redirect", redirect)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	render(r.Context(), w, widgetviews.Status(tx, requiredConfirmationsFor(tx, h.requiredConfirmations), widget.ThankYouMessage), h.log)
}

func (h *WidgetHandler) QRCode(w http.ResponseWriter, r *http.Request) {
	donationID := r.PathValue("donationID")

	tx, err := h.db.GetTransaction(donationID)
	if err != nil {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	amount := tx.Amount
	if tx.Status == database.StatusPartial {
		amount = remainingAmount(tx)
	}

	png, err := services.GenerateQRCode(tx.Subaddress, amount)
	if err != nil {
		h.log.Error().Err(err).Str("transaction_id", donationID).Msg("failed to generate QR code")
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err = w.Write(png); err != nil {
		h.log.Error().Err(err).Str("transaction_id", donationID).Msg("failed to write QR code response")
	}
}

func (h *WidgetHandler) Badge(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("widgetID")

	widget, err := h.db.GetWidget(widgetID)
	if err != nil {
		http.Error(w, "Widget not found", http.StatusNotFound)
		return
	}

	var stats *database.WidgetStats
	if widget.ShowStats {
		stats, err = h.db.GetWidgetStats(widgetID)
		if err != nil {
			h.log.Error().Err(err).Msg("failed to load widget stats for badge")
			stats = &database.WidgetStats{}
		}
	}

	widgetURL := fmt.Sprintf("%s/widget/%s", h.hostURL, widgetID)
	svg, err := services.GenerateBadgeSVG(services.BadgeParams{
		Widget:    widget,
		Stats:     stats,
		WidgetURL: widgetURL,
	})
	if err != nil {
		h.log.Error().Err(err).Str("widget_id", widgetID).Msg("failed to generate badge SVG")
		http.Error(w, "Failed to generate badge", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if _, err = w.Write(svg); err != nil {
		h.log.Error().Err(err).Str("widget_id", widgetID).Msg("failed to write badge response")
	}
}

// goalProgress converts a donation goal into piconero so the widget can draw a
// progress bar. A goal in fiat uses the current rate.
func (h *WidgetHandler) goalProgress(widget *database.Widget) (int64, int64) {
	if widget.GoalAmount <= 0 {
		return 0, 0
	}

	target := int64(widget.GoalAmount * 1e12)
	if widget.GoalCurrency != "" && widget.GoalCurrency != "XMR" {
		converted, _, err := h.price.ConvertFiatToPiconero(widget.GoalAmount, widget.GoalCurrency)
		if err != nil {
			return 0, 0
		}
		target = converted
	}

	reached, err := h.db.GetWidgetConfirmedTotal(widget.ID)
	if err != nil {
		return 0, target
	}
	return reached, target
}

func parseXMRToPiconero(amount, customAmount string) int64 {
	raw := customAmount
	if raw == "" {
		raw = amount
	}
	xmr, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(xmr) || math.IsInf(xmr, 0) || xmr <= 0 {
		return 0
	}
	piconero := math.Round(xmr * 1e12)
	if piconero > float64(math.MaxInt64) {
		return 0
	}
	return int64(piconero)
}

func widgetPaymentAmount(w *database.Widget) (int64, bool) {
	var amounts []float64
	if err := json.Unmarshal([]byte(w.PresetAmounts), &amounts); err != nil || len(amounts) != 1 {
		return 0, false
	}
	xmr := amounts[0]
	if math.IsNaN(xmr) || math.IsInf(xmr, 0) || xmr <= 0 {
		return 0, false
	}
	piconero := math.Round(xmr * 1e12)
	if piconero > float64(math.MaxInt64) {
		return 0, false
	}
	return int64(piconero), true
}

// standaloneWidget builds presentation defaults for a transaction that has no
// widget. It is never stored.
func standaloneWidget(tx *database.Transaction) *database.Widget {
	thankYou := "Thank you for your donation!"
	if tx.IsPayment {
		thankYou = "Thank you for your payment!"
	}
	return &database.Widget{
		ID:              tx.ID,
		Mode:            database.ModeDonation,
		Theme:           database.ThemeSystem,
		PrimaryColor:    "#ff6600",
		RedirectURL:     tx.RedirectURL,
		ThankYouMessage: thankYou,
	}
}

func (h *WidgetHandler) StandalonePayment(w http.ResponseWriter, r *http.Request) {
	tx, err := h.db.GetTransaction(r.PathValue("transactionID"))
	if err != nil {
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}

	required := requiredConfirmationsFor(tx, h.requiredConfirmations)
	render(r.Context(), w, widgetviews.Payment(tx, standaloneWidget(tx), required), h.log)
}

func (h *WidgetHandler) StandaloneStatus(w http.ResponseWriter, r *http.Request) {
	tx, err := h.db.GetTransaction(r.PathValue("transactionID"))
	if err != nil {
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}

	if tx.Status == database.StatusConfirmed && tx.RedirectURL != "" {
		w.Header().Set("HX-Redirect", tx.RedirectURL)
	}

	required := requiredConfirmationsFor(tx, h.requiredConfirmations)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	render(r.Context(), w, widgetviews.Status(tx, required, standaloneWidget(tx).ThankYouMessage), h.log)
}

func (h *WidgetHandler) StandaloneQRCode(w http.ResponseWriter, r *http.Request) {
	tx, err := h.db.GetTransaction(r.PathValue("transactionID"))
	if err != nil {
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}

	amount := tx.Amount
	if tx.Status == database.StatusPartial {
		amount = remainingAmount(tx)
	}

	png, err := services.GenerateQRCode(tx.Subaddress, amount)
	if err != nil {
		h.log.Error().Err(err).Str("transaction_id", tx.ID).Msg("failed to generate QR code")
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err = w.Write(png); err != nil {
		h.log.Error().Err(err).Str("transaction_id", tx.ID).Msg("failed to write QR code response")
	}
}
