package database

import (
	"database/sql"
	"time"
)

func (db *DB) CreateTransaction(t *Transaction) error {
	return db.conn.QueryRow(
		`INSERT INTO "transaction" (user_id, widget_id, subaddress, subaddress_index,
		 amount, fiat_amount, fiat_currency, status, confirmations,
		 is_payment, donor_name, note, tx_hash, created_at, updated_at, confirmed_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		t.UserID, t.WidgetID, t.Subaddress, t.SubaddressIndex,
		t.Amount, t.FiatAmount, nullStr(t.FiatCurrency), t.Status, t.Confirmations,
		t.IsPayment, nullStr(t.DonorName), nullStr(t.Note), nullStr(t.TxHash), t.CreatedAt, t.UpdatedAt, t.ConfirmedAt, t.ExpiresAt,
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

func (db *DB) CountTransactions(userID string, status TransactionStatus) (int, error) {
	query := `SELECT COUNT(*) FROM "transaction" WHERE user_id = ?`
	args := []any{userID}

	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
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
	defer rows.Close()

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

func (db *DB) GetActiveTransactions() ([]Transaction, error) {
	rows, err := db.conn.Query(
		`SELECT `+transactionColumns+` FROM "transaction" WHERE status IN (?, ?, ?)`,
		StatusPending, StatusMempool, StatusConfirming,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

func (db *DB) UpdateTransactionStatus(id string, status TransactionStatus, confirmations int, txHash string) error {
	now := time.Now().Unix()
	var confirmedAt any
	if status == StatusConfirmed {
		confirmedAt = now
	}

	_, err := db.conn.Exec(
		`UPDATE "transaction" SET status = ?, confirmations = ?, tx_hash = ?, updated_at = ?, confirmed_at = COALESCE(?, confirmed_at)
		 WHERE id = ?`,
		status, confirmations, nullStr(txHash), now, confirmedAt, id,
	)
	return err
}

func (db *DB) ExpireOldTransactions() (int64, error) {
	now := time.Now().Unix()
	result, err := db.conn.Exec(
		`UPDATE "transaction" SET status = ?, updated_at = ? WHERE status = ? AND expires_at < ?`,
		StatusExpired, now, StatusPending, now,
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
		`SELECT COUNT(*), COALESCE(SUM(amount), 0)
		 FROM "transaction" WHERE user_id = ? AND status = ?`,
		userID, StatusConfirmed,
	).Scan(&stats.TotalTransactions, &stats.TotalAmount)
	if err != nil {
		return nil, err
	}

	err = db.conn.QueryRow(
		`SELECT COUNT(*) FROM "transaction" WHERE user_id = ? AND status IN (?, ?, ?)`,
		userID, StatusPending, StatusMempool, StatusConfirming,
	).Scan(&stats.PendingCount)
	if err != nil {
		return nil, err
	}

	err = db.conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(amount), 0)
		 FROM "transaction" WHERE user_id = ? AND status = ? AND confirmed_at >= ?`,
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
		`SELECT COUNT(*), COALESCE(SUM(amount), 0)
		 FROM "transaction" WHERE widget_id = ? AND status = ?`,
		widgetID, StatusConfirmed,
	).Scan(&stats.TotalTransactions, &stats.TotalAmount)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

const transactionColumns = `id, user_id, widget_id, subaddress, subaddress_index,
	amount, fiat_amount, fiat_currency, status, confirmations,
	is_payment, donor_name, note, tx_hash, created_at, updated_at, confirmed_at, expires_at`

func (db *DB) scanTransaction(row *sql.Row) (*Transaction, error) {
	t := &Transaction{}
	var fiatCurrency, donorName, note, txHash sql.NullString
	err := row.Scan(
		&t.ID, &t.UserID, &t.WidgetID, &t.Subaddress, &t.SubaddressIndex,
		&t.Amount, &t.FiatAmount, &fiatCurrency,
		&t.Status, &t.Confirmations,
		&t.IsPayment, &donorName, &note, &txHash,
		&t.CreatedAt, &t.UpdatedAt, &t.ConfirmedAt, &t.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	t.FiatCurrency = fiatCurrency.String
	t.DonorName = donorName.String
	t.Note = note.String
	t.TxHash = txHash.String
	return t, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (db *DB) scanTransactionRow(row scannable) (*Transaction, error) {
	t := &Transaction{}
	var fiatCurrency, donorName, note, txHash sql.NullString
	err := row.Scan(
		&t.ID, &t.UserID, &t.WidgetID, &t.Subaddress, &t.SubaddressIndex,
		&t.Amount, &t.FiatAmount, &fiatCurrency,
		&t.Status, &t.Confirmations,
		&t.IsPayment, &donorName, &note, &txHash,
		&t.CreatedAt, &t.UpdatedAt, &t.ConfirmedAt, &t.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	t.FiatCurrency = fiatCurrency.String
	t.DonorName = donorName.String
	t.Note = note.String
	t.TxHash = txHash.String
	return t, nil
}
