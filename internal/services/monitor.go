package services

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

const (
	pollInterval = 30 * time.Second
)

type PaymentMonitor struct {
	db            *database.DB
	wallet        *monero.ClientHolder
	confirmations int
	log           zerolog.Logger
}

func NewPaymentMonitor(db *database.DB, wallet *monero.ClientHolder, confirmations int, log zerolog.Logger) *PaymentMonitor {
	return &PaymentMonitor{
		db:            db,
		wallet:        wallet,
		confirmations: confirmations,
		log:           log.With().Str("component", "monitor").Logger(),
	}
}

func (m *PaymentMonitor) Run(ctx context.Context) {
	m.log.Info().Msg("payment monitor started")
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.log.Info().Msg("payment monitor stopped")
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

func (m *PaymentMonitor) poll() {
	if expired, err := m.db.ExpireOldTransactions(); err != nil {
		m.log.Error().Err(err).Msg("failed to expire old transactions")
	} else if expired > 0 {
		m.log.Info().Int64("count", expired).Msg("expired stale transactions")
	}

	active, err := m.db.GetActiveTransactions()
	if err != nil {
		m.log.Error().Err(err).Msg("failed to get active transactions")
		return
	}
	if len(active) == 0 {
		return
	}

	client := m.wallet.Get()
	if client.URL() == "" {
		m.log.Debug().Msg("skipping transfer poll: wallet RPC not configured")
		return
	}

	transfers, err := client.GetTransfers()
	if err != nil {
		m.log.Error().Err(err).Msg("failed to get transfers from wallet")
		return
	}

	transfersByAddr := m.indexTransfers(transfers)

	for _, tx := range active {
		transfer, found := transfersByAddr[tx.Subaddress]
		if !found {
			continue
		}
		m.updateTransaction(tx, transfer)
	}
}

func (m *PaymentMonitor) indexTransfers(result *monero.GetTransfersResult) map[string]monero.Transfer {
	indexed := make(map[string]monero.Transfer)
	for _, list := range [][]monero.Transfer{result.In, result.Pending, result.Pool} {
		for _, t := range list {
			if existing, ok := indexed[t.Address]; !ok || t.Confirmations > existing.Confirmations {
				indexed[t.Address] = t
			}
		}
	}
	return indexed
}

func (m *PaymentMonitor) updateTransaction(tx database.Transaction, transfer monero.Transfer) {
	newStatus := m.resolveStatus(transfer.Confirmations)
	if newStatus == tx.Status && int(transfer.Confirmations) == tx.Confirmations {
		return
	}

	err := m.db.UpdateTransactionStatus(
		tx.ID,
		newStatus,
		int(transfer.Confirmations),
		transfer.TxHash,
	)
	if err != nil {
		m.log.Error().Err(err).Str("transaction_id", tx.ID).Msg("failed to update transaction")
		return
	}

	m.log.Info().
		Str("transaction_id", tx.ID).
		Str("status", string(newStatus)).
		Uint64("confirmations", transfer.Confirmations).
		Msg("transaction updated")
}

func (m *PaymentMonitor) resolveStatus(confirmations uint64) database.TransactionStatus {
	switch {
	case confirmations >= uint64(m.confirmations):
		return database.StatusConfirmed
	case confirmations >= 1:
		return database.StatusConfirming
	default:
		return database.StatusMempool
	}
}
