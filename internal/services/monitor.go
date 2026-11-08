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

	// revertWindow is how long a 0-conf confirmation stays reversible. A
	// mempool transfer that disappears inside this window is reported as
	// reverted. Older rows are left alone: get_transfers does not return
	// unbounded history, so absence is not proof there.
	revertWindow = 2 * time.Hour
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
	dispatcher    *Dispatcher
	log           zerolog.Logger
}

func NewPaymentMonitor(db *database.DB, wallet *monero.ClientHolder, confirmations int, dispatcher *Dispatcher, log zerolog.Logger) *PaymentMonitor {
	return &PaymentMonitor{
		db:            db,
		wallet:        wallet,
		confirmations: confirmations,
		dispatcher:    dispatcher,
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
	m.expire()

	active, err := m.db.GetActiveTransactions()
	if err != nil {
		m.log.Error().Err(err).Msg("failed to get active transactions")
		return
	}

	// Recently confirmed rows stay under watch so a 0-conf payment that
	// leaves the mempool can be reported as reverted.
	candidates, err := m.db.GetRevertCandidates(time.Now().Add(-revertWindow).Unix())
	if err != nil {
		m.log.Error().Err(err).Msg("failed to get revert candidates")
	}

	seen := make(map[string]bool, len(active))
	watched := make([]database.Transaction, 0, len(active)+len(candidates))
	for _, tx := range active {
		seen[tx.ID] = true
		watched = append(watched, tx)
	}
	for _, tx := range candidates {
		if !seen[tx.ID] {
			watched = append(watched, tx)
		}
	}

	if len(watched) == 0 {
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

	for _, tx := range watched {
		transfer, found := transfersByAddr[tx.Subaddress]
		if !found {
			m.maybeRevert(tx)
			continue
		}
		m.updateTransaction(tx, transfer)
	}
}

func (m *PaymentMonitor) expire() {
	now := time.Now().Unix()
	expiring, err := m.db.GetExpiringTransactions(now)
	if err != nil {
		m.log.Error().Err(err).Msg("failed to load expiring transactions")
		return
	}

	expired, err := m.db.ExpireOldTransactions()
	if err != nil {
		m.log.Error().Err(err).Msg("failed to expire old transactions")
		return
	}
	if expired == 0 {
		return
	}
	m.log.Info().Int64("count", expired).Msg("expired stale transactions")

	for i := range expiring {
		tx, err := m.db.GetTransaction(expiring[i].ID)
		if err != nil {
			continue
		}
		m.dispatcher.EmitAsync(tx, database.EventPaymentExpired, m.requiredFor(*tx))
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

	required := m.requiredFor(tx)
	newStatus := resolveStatus(tx, transfer, required)
	received := int64(transfer.amount)
	statusChanged := newStatus != tx.Status

	if !statusChanged && int(transfer.confirmations) == tx.Confirmations && received == tx.ReceivedAmount {
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

	// Emit on every status change, and again on each new partial transfer.
	// A confirmed row that only gains confirmations stays quiet.
	shouldEmit := statusChanged || newStatus == database.StatusPartial
	if !shouldEmit {
		return
	}

	updated, err := m.db.GetTransaction(tx.ID)
	if err != nil {
		m.log.Error().Err(err).Str("transaction_id", tx.ID).Msg("failed to reload transaction for event")
		return
	}
	m.dispatcher.EmitAsync(updated, eventForStatus(newStatus), required)
}

// maybeRevert reports a 0-conf payment that left the mempool.
func (m *PaymentMonitor) maybeRevert(tx database.Transaction) {
	if tx.Status != database.StatusConfirmed && tx.Status != database.StatusReverted {
		return
	}
	if m.requiredFor(tx) != 0 {
		return
	}

	reference := tx.ConfirmedAt
	if tx.RevertedAt > reference {
		reference = tx.RevertedAt
	}
	if reference == 0 || time.Now().Unix()-reference > int64(revertWindow.Seconds()) {
		return
	}
	if tx.Status == database.StatusReverted {
		return
	}

	if err := m.db.UpdateTransactionStatus(tx.ID, database.StatusReverted, tx.Confirmations, tx.TxHash, tx.ReceivedAmount); err != nil {
		m.log.Error().Err(err).Str("transaction_id", tx.ID).Msg("failed to mark transaction reverted")
		return
	}

	m.log.Warn().Str("transaction_id", tx.ID).Msg("0-conf payment left the mempool, marked reverted")

	updated, err := m.db.GetTransaction(tx.ID)
	if err != nil {
		return
	}
	m.dispatcher.EmitAsync(updated, database.EventPaymentReverted, 0)
}

// requiredFor returns the confirmation count for one transaction. A per-row
// value of -1 means "use the global default"; 0 means 0-conf.
func (m *PaymentMonitor) requiredFor(tx database.Transaction) int {
	if tx.ConfirmationsRequired >= 0 {
		return tx.ConfirmationsRequired
	}
	return m.confirmations
}

func resolveStatus(tx database.Transaction, transfer transferTotal, required int) database.TransactionStatus {
	if tx.Amount > 0 && transfer.amount < uint64(tx.Amount) {
		return database.StatusPartial
	}

	switch {
	case required <= 0:
		// 0-conf: a mempool transfer is enough. The row stays reversible
		// until the window in revertWindow passes.
		return database.StatusConfirmed
	case transfer.confirmations >= uint64(required):
		return database.StatusConfirmed
	case transfer.confirmations >= 1:
		return database.StatusConfirming
	default:
		return database.StatusMempool
	}
}

func eventForStatus(status database.TransactionStatus) string {
	switch status {
	case database.StatusMempool:
		return database.EventPaymentMempool
	case database.StatusConfirming:
		return database.EventPaymentConfirming
	case database.StatusPartial:
		return database.EventPaymentPartial
	case database.StatusConfirmed:
		return database.EventPaymentConfirmed
	case database.StatusReverted:
		return database.EventPaymentReverted
	default:
		return database.EventPaymentConfirming
	}
}
