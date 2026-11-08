package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/database"
)

// Event is the JSON body that a webhook receives and that a chat channel
// renders as text. The shape is stable: add fields, never rename them.
type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"event"`
	CreatedAt int64     `json:"created_at"`
	Data      EventData `json:"data"`
}

type EventData struct {
	TransactionID         string          `json:"transaction_id"`
	WidgetID              string          `json:"widget_id,omitempty"`
	Status                string          `json:"status"`
	Subaddress            string          `json:"subaddress"`
	Amount                int64           `json:"amount"`
	AmountXMR             string          `json:"amount_xmr"`
	ReceivedAmount        int64           `json:"received_amount"`
	ReceivedAmountXMR     string          `json:"received_amount_xmr"`
	RemainingAmount       int64           `json:"remaining_amount"`
	RemainingAmountXMR    string          `json:"remaining_amount_xmr"`
	FiatAmount            float64         `json:"fiat_amount,omitempty"`
	FiatCurrency          string          `json:"fiat_currency,omitempty"`
	Confirmations         int             `json:"confirmations"`
	RequiredConfirmations int             `json:"required_confirmations"`
	IsPayment             bool            `json:"is_payment"`
	IsTest                bool            `json:"is_test"`
	Description           string          `json:"description,omitempty"`
	DonorName             string          `json:"donor_name,omitempty"`
	Note                  string          `json:"note,omitempty"`
	TxHash                string          `json:"tx_hash,omitempty"`
	Metadata              json.RawMessage `json:"metadata,omitempty"`
	CreatedAt             int64           `json:"created_at"`
	ExpiresAt             int64           `json:"expires_at"`
	ConfirmedAt           int64           `json:"confirmed_at,omitempty"`
	PaymentURL            string          `json:"payment_url,omitempty"`
}

func newEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func xmrString(piconero int64) string {
	return strconv.FormatFloat(float64(piconero)/1e12, 'f', -1, 64)
}

// BuildEvent converts a transaction row into an event payload.
func BuildEvent(tx *database.Transaction, eventType string, requiredConfirmations int, hostURL string) Event {
	remaining := tx.Amount - tx.ReceivedAmount
	if remaining < 0 {
		remaining = 0
	}

	data := EventData{
		TransactionID:         tx.ID,
		WidgetID:              tx.WidgetID.String,
		Status:                string(tx.Status),
		Subaddress:            tx.Subaddress,
		Amount:                tx.Amount,
		AmountXMR:             xmrString(tx.Amount),
		ReceivedAmount:        tx.ReceivedAmount,
		ReceivedAmountXMR:     xmrString(tx.ReceivedAmount),
		RemainingAmount:       remaining,
		RemainingAmountXMR:    xmrString(remaining),
		FiatAmount:            tx.FiatAmount,
		FiatCurrency:          tx.FiatCurrency,
		Confirmations:         tx.Confirmations,
		RequiredConfirmations: requiredConfirmations,
		IsPayment:             tx.IsPayment,
		IsTest:                tx.IsTest,
		Description:           tx.Description,
		DonorName:             tx.DonorName,
		Note:                  tx.Note,
		TxHash:                tx.TxHash,
		CreatedAt:             tx.CreatedAt,
		ExpiresAt:             tx.ExpiresAt,
		ConfirmedAt:           tx.ConfirmedAt,
	}

	if tx.Metadata != "" && json.Valid([]byte(tx.Metadata)) {
		data.Metadata = json.RawMessage(tx.Metadata)
	}
	if hostURL != "" {
		data.PaymentURL = fmt.Sprintf("%s/pay/%s", hostURL, tx.ID)
	}

	return Event{
		ID:        newEventID(),
		Type:      eventType,
		CreatedAt: time.Now().Unix(),
		Data:      data,
	}
}

// Message renders a short human-readable line for chat and email channels.
func (e Event) Message() string {
	amount := e.Data.AmountXMR + " XMR"
	if e.Data.Amount == 0 {
		amount = e.Data.ReceivedAmountXMR + " XMR"
	}

	label := "Donation"
	if e.Data.IsPayment {
		label = "Payment"
	}

	var head string
	switch e.Type {
	case database.EventPaymentCreated:
		head = "New " + label + " request"
	case database.EventPaymentMempool:
		head = label + " seen in mempool"
	case database.EventPaymentPartial:
		head = label + " partial"
	case database.EventPaymentConfirming:
		head = label + " confirming"
	case database.EventPaymentConfirmed:
		head = label + " confirmed"
	case database.EventPaymentReverted:
		head = label + " reverted"
	case database.EventPaymentExpired:
		head = label + " request expired"
	default:
		head = e.Type
	}

	msg := fmt.Sprintf("%s: %s", head, amount)
	if e.Data.FiatAmount > 0 {
		msg += fmt.Sprintf(" (~%.2f %s)", e.Data.FiatAmount, e.Data.FiatCurrency)
	}
	if e.Type == database.EventPaymentPartial {
		msg += fmt.Sprintf(". Received %s, remaining %s", e.Data.ReceivedAmountXMR, e.Data.RemainingAmountXMR)
	}
	if e.Data.Description != "" {
		msg += "\n" + e.Data.Description
	}
	if e.Data.PaymentURL != "" {
		msg += "\n" + e.Data.PaymentURL
	}
	return msg
}

// Dispatcher writes one queued delivery per subscribed endpoint. It never
// sends anything itself: the worker owns the network calls.
type Dispatcher struct {
	db      *database.DB
	log     zerolog.Logger
	hostURL string
}

func NewDispatcher(db *database.DB, hostURL string, log zerolog.Logger) *Dispatcher {
	return &Dispatcher{db: db, log: log.With().Str("component", "dispatcher").Logger(), hostURL: hostURL}
}

// Emit queues an event for every endpoint that subscribes to it.
func (d *Dispatcher) Emit(tx *database.Transaction, eventType string, requiredConfirmations int) {
	if d == nil || d.db == nil || tx == nil {
		return
	}

	event := BuildEvent(tx, eventType, requiredConfirmations, d.hostURL)
	payload, err := json.Marshal(event)
	if err != nil {
		d.log.Error().Err(err).Str("event", eventType).Msg("failed to marshal event")
		return
	}

	now := time.Now().Unix()

	webhooks, err := d.db.ListActiveWebhooks(tx.UserID)
	if err != nil {
		d.log.Error().Err(err).Msg("failed to list webhooks")
	}
	for _, w := range webhooks {
		if !database.Subscribes(w.Events, eventType) {
			continue
		}
		delivery := &database.NotificationDelivery{
			UserID:      tx.UserID,
			Kind:        "webhook",
			TargetID:    w.ID,
			Event:       eventType,
			EventID:     event.ID,
			Payload:     string(payload),
			Status:      database.DeliveryPending,
			NextRetryAt: now,
			CreatedAt:   now,
		}
		if err := d.db.EnqueueDelivery(delivery); err != nil {
			d.log.Error().Err(err).Str("webhook_id", w.ID).Msg("failed to enqueue webhook delivery")
		}
	}

	channels, err := d.db.ListActiveChannels(tx.UserID)
	if err != nil {
		d.log.Error().Err(err).Msg("failed to list channels")
	}
	for _, c := range channels {
		if !database.Subscribes(c.Events, eventType) {
			continue
		}
		delivery := &database.NotificationDelivery{
			UserID:      tx.UserID,
			Kind:        "channel",
			TargetID:    c.ID,
			Event:       eventType,
			EventID:     event.ID,
			Payload:     string(payload),
			Status:      database.DeliveryPending,
			NextRetryAt: now,
			CreatedAt:   now,
		}
		if err := d.db.EnqueueDelivery(delivery); err != nil {
			d.log.Error().Err(err).Str("channel_id", c.ID).Msg("failed to enqueue channel delivery")
		}
	}
}

// EmitAsync runs Emit in the background so a slow database cannot stall the
// payment monitor.
func (d *Dispatcher) EmitAsync(tx *database.Transaction, eventType string, requiredConfirmations int) {
	if d == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				d.log.Error().Interface("panic", r).Str("event", eventType).Msg("dispatcher panic")
			}
		}()
		d.Emit(tx, eventType, requiredConfirmations)
	}()
}
