package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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

type WidgetHandler struct {
	db                *database.DB
	wallet            *monero.ClientHolder
	price             *services.PriceService
	log               zerolog.Logger
	hostURL           string
	transactionExpiry time.Duration
}

func NewWidgetHandler(db *database.DB, wallet *monero.ClientHolder, price *services.PriceService, hostURL string, transactionExpiry time.Duration, log zerolog.Logger) *WidgetHandler {
	return &WidgetHandler{db: db, wallet: wallet, price: price, hostURL: hostURL, transactionExpiry: transactionExpiry, log: log.With().Str("component", "widget").Logger()}
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

	render(r.Context(), w, widgetviews.Home(widget, user), h.log)
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

	rawAmount := r.FormValue("custom_amount")
	if rawAmount == "" {
		rawAmount = r.FormValue("amount")
	}
	amountRequested := parseXMRToPiconero(r.FormValue("amount"), r.FormValue("custom_amount"))
	if rawAmount != "" && amountRequested == 0 {
		h.log.Warn().Str("raw_amount", rawAmount).Msg("unparseable amount, treating as open donation")
	}

	labelBytes := make([]byte, 16)
	_, _ = rand.Read(labelBytes)
	txLabel := fmt.Sprintf("tx-%s", hex.EncodeToString(labelBytes))
	address, addrIndex, err := h.wallet.Get().CreateAddress(txLabel)
	if err != nil {
		if errors.Is(err, monero.ErrNotConfigured) {
			h.log.Warn().Msg("transaction attempt but wallet RPC is not configured")
			render(r.Context(), w, widgetviews.Error(widget, "Payments are not available yet. The wallet has not been configured."), h.log)
		} else {
			h.log.Error().Err(err).Msg("wallet RPC: create_address failed")
			render(r.Context(), w, widgetviews.Error(widget, "Wallet is currently unavailable. Please try again later."), h.log)
		}
		return
	}

	isPayment := widget.Mode == database.ModePayment

	now := time.Now()
	tx := &database.Transaction{
		UserID:          user.ID,
		WidgetID:        sql.NullString{String: widget.ID, Valid: true},
		Subaddress:      address,
		SubaddressIndex: addrIndex,
		Amount:          amountRequested,
		Status:          database.StatusPending,
		IsPayment:       isPayment,
		DonorName:       r.FormValue("donor_name"),
		Note:            r.FormValue("note"),
		CreatedAt:       now.Unix(),
		UpdatedAt:       now.Unix(),
		ExpiresAt:       now.Add(h.transactionExpiry).Unix(),
	}

	if amountRequested > 0 {
		tx.FiatAmount = h.price.ConvertXMRToFiat(amountRequested)
		tx.FiatCurrency = h.price.Currency()
	}

	if err := h.db.CreateTransaction(tx); err != nil {
		h.log.Error().Err(err).Msg("failed to persist transaction")
		http.Error(w, "Failed to create transaction", http.StatusInternalServerError)
		return
	}

	render(r.Context(), w, widgetviews.Payment(tx, widget.Theme, widget.PrimaryColor, widget.RedirectURL), h.log)
}

func (h *WidgetHandler) TransactionStatus(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("widgetID")
	donationID := r.PathValue("donationID")

	tx, err := h.db.GetTransaction(donationID)
	if err != nil || !tx.WidgetID.Valid || tx.WidgetID.String != widgetID {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	if tx.Status == database.StatusConfirmed {
		widget, wErr := h.db.GetWidget(widgetID)
		if wErr == nil && widget.RedirectURL != "" {
			w.Header().Set("HX-Redirect", widget.RedirectURL)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	render(r.Context(), w, widgetviews.Status(tx), h.log)
}

func (h *WidgetHandler) QRCode(w http.ResponseWriter, r *http.Request) {
	donationID := r.PathValue("donationID")

	tx, err := h.db.GetTransaction(donationID)
	if err != nil {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	png, err := services.GenerateQRCode(tx.Subaddress, tx.Amount)
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

func parseXMRToPiconero(amount, customAmount string) int64 {
	raw := customAmount
	if raw == "" {
		raw = amount
	}
	xmr, err := strconv.ParseFloat(raw, 64)
	if err != nil || xmr <= 0 {
		return 0
	}
	return int64(math.Round(xmr * 1e12))
}
