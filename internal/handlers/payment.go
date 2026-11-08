package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

// paymentRequest holds everything needed to create one payment request. The
// widget form and the REST API both build this and share the same code path.
type paymentRequest struct {
	UserID                string
	Widget                *database.Widget
	Amount                int64
	FiatAmount            float64
	FiatCurrency          string
	IsPayment             bool
	Description           string
	DonorName             string
	Note                  string
	RedirectURL           string
	CallbackURL           string
	Metadata              string
	ConfirmationsRequired int
	Expiry                time.Duration
	IsTest                bool
}

var errWalletUnavailable = errors.New("wallet unavailable")

func createPayment(db *database.DB, wallet *monero.ClientHolder, p paymentRequest) (*database.Transaction, error) {
	labelBytes := make([]byte, 16)
	_, _ = rand.Read(labelBytes)
	label := fmt.Sprintf("tx-%s", hex.EncodeToString(labelBytes))

	address, addrIndex, err := wallet.Get().CreateAddress(label)
	if err != nil {
		if errors.Is(err, monero.ErrNotConfigured) {
			return nil, fmt.Errorf("%w: the wallet has not been configured", errWalletUnavailable)
		}
		return nil, fmt.Errorf("create subaddress: %w", err)
	}

	if p.Expiry <= 0 {
		p.Expiry = time.Hour
	}
	if p.ConfirmationsRequired < -1 {
		p.ConfirmationsRequired = -1
	}

	now := time.Now()
	tx := &database.Transaction{
		UserID:                p.UserID,
		Subaddress:            address,
		SubaddressIndex:       addrIndex,
		Amount:                p.Amount,
		FiatAmount:            p.FiatAmount,
		FiatCurrency:          p.FiatCurrency,
		Status:                database.StatusPending,
		IsPayment:             p.IsPayment,
		DonorName:             p.DonorName,
		Note:                  p.Note,
		Description:           p.Description,
		RedirectURL:           p.RedirectURL,
		CallbackURL:           p.CallbackURL,
		Metadata:              p.Metadata,
		ConfirmationsRequired: p.ConfirmationsRequired,
		IsTest:                p.IsTest,
		CreatedAt:             now.Unix(),
		UpdatedAt:             now.Unix(),
		ExpiresAt:             now.Add(p.Expiry).Unix(),
	}

	if p.Widget != nil {
		tx.WidgetID = sql.NullString{String: p.Widget.ID, Valid: true}
		tx.IsPayment = p.Widget.Mode == database.ModePayment
	}

	if err := db.CreateTransaction(tx); err != nil {
		return nil, fmt.Errorf("persist transaction: %w", err)
	}

	return tx, nil
}

// remainingAmount returns what the payer still owes. A zero requested amount
// (an open donation) never has a remainder.
func remainingAmount(tx *database.Transaction) int64 {
	if tx.Amount <= 0 {
		return 0
	}
	remaining := tx.Amount - tx.ReceivedAmount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// requiredConfirmationsFor resolves the per-transaction override against the
// global default.
func requiredConfirmationsFor(tx *database.Transaction, global int) int {
	if tx.ConfirmationsRequired >= 0 {
		return tx.ConfirmationsRequired
	}
	return global
}

// widgetRequiredConfirmations resolves the per-widget override.
func widgetRequiredConfirmations(w *database.Widget, global int) int {
	if w != nil && w.Confirmations >= 0 {
		return w.Confirmations
	}
	return global
}
