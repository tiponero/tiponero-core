package database

import (
	"database/sql"
	"time"
)

func (db *DB) CreateTransaction(t *Transaction) error {
	return db.conn.QueryRow(
		`INSERT INTO "transaction" (user_id, widget_id, subaddress, subaddress_index,
		 amount, received_amount, fiat_amount, fiat_currency, status, confirmations,
		 is_payment, donor_name, note, tx_hash, description, redirect_url, callback_url, metadata,
		 confirmations_required, is_test, created_at, updated_at, confirmed_at, expires_at, reverted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		t.UserID, t.WidgetID, t.Subaddress, t.SubaddressIndex,
		t.Amount, t.ReceivedAmount, t.FiatAmount, nullStr(t.FiatCurrency), t.Status, t.Confirmations,
		t.IsPayment, nullStr(t.DonorName), nullStr(t.Note), nullStr(t.TxHash),
		nullStr(t.Description), nullStr(t.RedirectURL), nullStr(t.CallbackURL), nullStr(t.Metadata),
		t.ConfirmationsRequired, t.IsTest,
		t.CreatedAt, t.UpdatedAt, t.ConfirmedAt, t.ExpiresAt, t.RevertedAt,
	).Scan(&t.ID)
}

func (db *DB) GetTransaction(id string) (*Transaction, error) {
	return db.scanTransaction(db.conn.QueryRow(
		`SELECT `+transactionColumns+` FROM "transaction" WHERE id = ?`, id,
	))
}

func (db *DB) GetTransactionBySubaddress(subaddress string) (*Transaction, error) {
	return db.scanTransaction(db.conn.QueryRow(
		`SELECT `+transactionColumns+` FROM "transaction" WHERE subaddress = ?`, subaddress,
	))
}

func (db *DB) CountTransactions(userID string, f TransactionFilter) (int, error) {
	query := `SELECT COUNT(*) FROM "transaction" WHERE user_id = ?`
	args := []any{userID}

	if f.Status != "" {
		query += ` AND status = ?`
		args = append(args, f.Status)
	}
	if !f.IncludeTest {
		query += ` AND is_test = 0`
	}

	var count int
	err := db.conn.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (db *DB) ListTransactions(userID string, f TransactionFilter) ([]Transaction, error) {
	query := `SELECT ` + transactionColumns + ` FROM "transaction" WHERE user_id = ?`
	args := []any{userID}

	if f.Status != "" {
		query += ` AND status = ?`
		args = append(args, f.Status)
	}
	if !f.IncludeTest {
		query += ` AND is_test = 0`
	}

	query += ` ORDER BY created_at DESC`

	if f.Take > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Take)
	}
	if f.Skip > 0 {
		query += ` OFFSET ?`
		args = append(args, f.Skip)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var transactions []Transaction
	for rows.Next() {
		t, err := db.scanTransactionRow(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, *t)
	}
	return transactions, rows.Err()
}

// GetActiveTransactions returns every transaction that can still change.
// A reverted transaction stays active because a dropped transfer can return.
func (db *DB) GetActiveTransactions() ([]Transaction, error) {
	rows, err := db.conn.Query(
		`SELECT `+transactionColumns+` FROM "transaction" WHERE status IN (?, ?, ?, ?, ?)`,
		StatusPending, StatusMempool, StatusConfirming, StatusPartial, StatusReverted,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var transactions []Transaction
	for rows.Next() {
		t, err := db.scanTransactionRow(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, *t)
	}
	return transactions, rows.Err()
}

func (db *DB) UpdateTransactionStatus(id string, status TransactionStatus, confirmations int, txHash string, receivedAmount int64) error {
	now := time.Now().Unix()
	var confirmedAt, revertedAt any
	switch status {
	case StatusConfirmed:
		confirmedAt = now
	case StatusReverted:
		revertedAt = now
	}

	_, err := db.conn.Exec(
		`UPDATE "transaction" SET status = ?, confirmations = ?, tx_hash = ?, received_amount = ?, updated_at = ?,
		 confirmed_at = COALESCE(?, confirmed_at), reverted_at = COALESCE(?, reverted_at)
		 WHERE id = ?`,
		status, confirmations, nullStr(txHash), receivedAmount, now, confirmedAt, revertedAt, id,
	)
	return err
}

// ExpireOldTransactions closes unpaid requests. The received amount stays on
// the row, so the merchant still sees a partial payment after expiry.
func (db *DB) ExpireOldTransactions() (int64, error) {
	now := time.Now().Unix()
	result, err := db.conn.Exec(
		`UPDATE "transaction" SET status = ?, updated_at = ?
		 WHERE status IN (?, ?) AND expires_at < ?`,
		StatusExpired, now, StatusPending, StatusPartial, now,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) GetTransactionStats(userID string) (*TransactionStats, error) {
	todayStart := time.Now().Truncate(24 * time.Hour).Unix()
	stats := &TransactionStats{}

	err := db.conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(`+effectiveAmount+`), 0)
		 FROM "transaction" WHERE user_id = ? AND status = ? AND is_test = 0`,
		userID, StatusConfirmed,
	).Scan(&stats.TotalTransactions, &stats.TotalAmount)
	if err != nil {
		return nil, err
	}

	err = db.conn.QueryRow(
		`SELECT COUNT(*) FROM "transaction" WHERE user_id = ? AND status IN (?, ?, ?, ?, ?) AND is_test = 0`,
		userID, StatusPending, StatusMempool, StatusConfirming, StatusPartial, StatusReverted,
	).Scan(&stats.PendingCount)
	if err != nil {
		return nil, err
	}

	err = db.conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(`+effectiveAmount+`), 0)
		 FROM "transaction" WHERE user_id = ? AND status = ? AND confirmed_at >= ? AND is_test = 0`,
		userID, StatusConfirmed, todayStart,
	).Scan(&stats.TodayCount, &stats.TodayAmount)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

func (db *DB) GetWidgetStats(widgetID string) (*WidgetStats, error) {
	stats := &WidgetStats{}
	err := db.conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(`+effectiveAmount+`), 0)
		 FROM "transaction" WHERE widget_id = ? AND status = ? AND is_test = 0`,
		widgetID, StatusConfirmed,
	).Scan(&stats.TotalTransactions, &stats.TotalAmount)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// GetWidgetConfirmedTotal returns the confirmed total in piconero, used by a
// donation goal. Test transactions never count.
func (db *DB) GetWidgetConfirmedTotal(widgetID string) (int64, error) {
	var total int64
	err := db.conn.QueryRow(
		`SELECT COALESCE(SUM(`+effectiveAmount+`), 0)
		 FROM "transaction" WHERE widget_id = ? AND status = ? AND is_test = 0`,
		widgetID, StatusConfirmed,
	).Scan(&total)
	return total, err
}

// GetWidgetConfirmedTotalSince limits a goal to transactions confirmed after a
// deadline start. A zero since value counts every confirmed transaction.
func (db *DB) GetWidgetConfirmedTotalSince(widgetID string, since int64) (int64, error) {
	if since <= 0 {
		return db.GetWidgetConfirmedTotal(widgetID)
	}
	var total int64
	err := db.conn.QueryRow(
		`SELECT COALESCE(SUM(`+effectiveAmount+`), 0)
		 FROM "transaction" WHERE widget_id = ? AND status = ? AND is_test = 0 AND confirmed_at >= ?`,
		widgetID, StatusConfirmed, since,
	).Scan(&total)
	return total, err
}

const transactionColumns = `id, user_id, widget_id, subaddress, subaddress_index,
	amount, received_amount, fiat_amount, fiat_currency, status, confirmations,
	is_payment, donor_name, note, tx_hash, description, redirect_url, callback_url, metadata,
	confirmations_required, is_test, created_at, updated_at, confirmed_at, expires_at, reverted_at`

// effectiveAmount uses the received amount when the monitor recorded one.
// Rows confirmed by an older version have received_amount = 0, so the
// requested amount is the best available value.
const effectiveAmount = `COALESCE(NULLIF(received_amount, 0), amount)`

type scannable interface {
	Scan(dest ...any) error
}

func (db *DB) scanTransaction(row *sql.Row) (*Transaction, error) {
	return scanTransaction(row)
}

func (db *DB) scanTransactionRow(row scannable) (*Transaction, error) {
	return scanTransaction(row)
}

func scanTransaction(row scannable) (*Transaction, error) {
	t := &Transaction{}
	var fiatCurrency, donorName, note, txHash sql.NullString
	var description, redirectURL, callbackURL, metadata sql.NullString
	err := row.Scan(
		&t.ID, &t.UserID, &t.WidgetID, &t.Subaddress, &t.SubaddressIndex,
		&t.Amount, &t.ReceivedAmount, &t.FiatAmount, &fiatCurrency,
		&t.Status, &t.Confirmations,
		&t.IsPayment, &donorName, &note, &txHash,
		&description, &redirectURL, &callbackURL, &metadata,
		&t.ConfirmationsRequired, &t.IsTest,
		&t.CreatedAt, &t.UpdatedAt, &t.ConfirmedAt, &t.ExpiresAt, &t.RevertedAt,
	)
	if err != nil {
		return nil, err
	}
	t.FiatCurrency = fiatCurrency.String
	t.DonorName = donorName.String
	t.Note = note.String
	t.TxHash = txHash.String
	t.Description = description.String
	t.RedirectURL = redirectURL.String
	t.CallbackURL = callbackURL.String
	t.Metadata = metadata.String
	return t, nil
}

// GetExpiringTransactions returns the rows that ExpireOldTransactions will
// close, so the caller can emit one event per row.
func (db *DB) GetExpiringTransactions(now int64) ([]Transaction, error) {
	rows, err := db.conn.Query(
		`SELECT `+transactionColumns+` FROM "transaction"
		 WHERE status IN (?, ?) AND expires_at < ?`,
		StatusPending, StatusPartial, now,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var transactions []Transaction
	for rows.Next() {
		t, err := db.scanTransactionRow(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, *t)
	}
	return transactions, rows.Err()
}

// GetRevertCandidates returns 0-conf confirmations recent enough that a
// mempool drop is still a real possibility.
func (db *DB) GetRevertCandidates(since int64) ([]Transaction, error) {
	rows, err := db.conn.Query(
		`SELECT `+transactionColumns+` FROM "transaction"
		 WHERE status = ? AND confirmations_required = 0 AND confirmed_at >= ?`,
		StatusConfirmed, since,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var transactions []Transaction
	for rows.Next() {
		t, err := db.scanTransactionRow(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, *t)
	}
	return transactions, rows.Err()
}
