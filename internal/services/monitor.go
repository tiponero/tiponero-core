package services

import (
	"context"
	"math"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

const (
	pollInterval = 30 * time.Second
)

type transferTotal struct {
	amount        uint64
	confirmations uint64
	txHash        string
}

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

func (m *PaymentMonitor) indexTransfers(result *monero.GetTransfersResult) map[string]transferTotal {
	type transferKey struct {
		address string
		txHash  string
		minor   uint64
		amount  uint64
	}

	unique := make(map[transferKey]monero.Transfer)
	for _, list := range [][]monero.Transfer{result.In, result.Pending, result.Pool} {
		for _, t := range list {
			key := transferKey{address: t.Address, txHash: t.TxHash, minor: t.SubaddrIndex.Minor, amount: t.Amount}
			if existing, ok := unique[key]; !ok || t.Confirmations > existing.Confirmations {
				unique[key] = t
			}
		}
	}

	indexed := make(map[string]transferTotal)
	for _, t := range unique {
		total := indexed[t.Address]
		if total.amount > math.MaxUint64-t.Amount {
			m.log.Error().Str("address", t.Address).Msg("transfer amount sum overflow")
			continue
		}
		total.amount += t.Amount
		if t.Confirmations > total.confirmations {
			total.confirmations = t.Confirmations
			total.txHash = t.TxHash
		}
		indexed[t.Address] = total
	}
	return indexed
}

func (m *PaymentMonitor) updateTransaction(tx database.Transaction, transfer transferTotal) {
	if transfer.amount == 0 {
		return
	}
	if transfer.amount > math.MaxInt64 {
		m.log.Error().Str("transaction_id", tx.ID).Uint64("amount", transfer.amount).Msg("transfer amount exceeds int64")
		return
	}

	newStatus := m.resolveStatus(tx, transfer)
	received := int64(transfer.amount)
	if newStatus == tx.Status && int(transfer.confirmations) == tx.Confirmations && received == tx.ReceivedAmount {
		return
	}

	err := m.db.UpdateTransactionStatus(
		tx.ID,
		newStatus,
		int(transfer.confirmations),
		transfer.txHash,
		received,
	)
	if err != nil {
		m.log.Error().Err(err).Str("transaction_id", tx.ID).Msg("failed to update transaction")
		return
	}

	m.log.Info().
		Str("transaction_id", tx.ID).
		Str("status", string(newStatus)).
		Uint64("confirmations", transfer.confirmations).
		Uint64("received", transfer.amount).
		Msg("transaction updated")
}

func (m *PaymentMonitor) resolveStatus(tx database.Transaction, transfer transferTotal) database.TransactionStatus {
	if tx.Amount > 0 && transfer.amount < uint64(tx.Amount) {
		return database.StatusUnderpaid
	}

	switch {
	case transfer.confirmations >= uint64(m.confirmations):
		return database.StatusConfirmed
	case transfer.confirmations >= 1:
		return database.StatusConfirming
	default:
		return database.StatusMempool
	}
}
