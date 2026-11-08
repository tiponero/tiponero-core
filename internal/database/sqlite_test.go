package database

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedUserAndWidget(t *testing.T, db *DB) (*User, *Widget) {
	t.Helper()
	now := time.Now().Unix()

	user := &User{Username: "admin", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}
	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	widget := &Widget{
		UserID:          user.ID,
		Name:            "Widget",
		Mode:            ModeDonation,
		ButtonText:      "Donate",
		CustomMessage:   "Message",
		ThankYouMessage: "Thanks",
		PrimaryColor:    "#ff6600",
		Theme:           ThemeSystem,
		ShowStats:       true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.CreateWidget(widget); err != nil {
		t.Fatalf("CreateWidget: %v", err)
	}

	return user, widget
}

func TestTransactionReceivedAmount(t *testing.T) {
	db := newTestDB(t)
	user, widget := seedUserAndWidget(t, db)
	now := time.Now().Unix()

	tx := &Transaction{
		UserID:          user.ID,
		WidgetID:        sql.NullString{String: widget.ID, Valid: true},
		Subaddress:      "subaddress",
		SubaddressIndex: 1,
		Amount:          100,
		Status:          StatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       now + 3600,
	}
	if err := db.CreateTransaction(tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	if err := db.UpdateTransactionStatus(tx.ID, StatusConfirmed, 10, "hash", 150); err != nil {
		t.Fatalf("UpdateTransactionStatus: %v", err)
	}

	got, err := db.GetTransaction(tx.ID)
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got.ReceivedAmount != 150 {
		t.Fatalf("ReceivedAmount = %d, want 150", got.ReceivedAmount)
	}
	if got.Status != StatusConfirmed {
		t.Fatalf("Status = %s, want confirmed", got.Status)
	}

	stats, err := db.GetTransactionStats(user.ID)
	if err != nil {
		t.Fatalf("GetTransactionStats: %v", err)
	}
	if stats.TotalAmount != 150 {
		t.Fatalf("TotalAmount = %d, want 150", stats.TotalAmount)
	}
}

func TestStatsFallBackToRequestedAmount(t *testing.T) {
	db := newTestDB(t)
	user, widget := seedUserAndWidget(t, db)
	now := time.Now().Unix()

	tx := &Transaction{
		UserID:          user.ID,
		WidgetID:        sql.NullString{String: widget.ID, Valid: true},
		Subaddress:      "old-subaddress",
		SubaddressIndex: 2,
		Amount:          200,
		Status:          StatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       now + 3600,
	}
	if err := db.CreateTransaction(tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	if err := db.UpdateTransactionStatus(tx.ID, StatusConfirmed, 10, "old-hash", 0); err != nil {
		t.Fatalf("UpdateTransactionStatus: %v", err)
	}

	stats, err := db.GetTransactionStats(user.ID)
	if err != nil {
		t.Fatalf("GetTransactionStats: %v", err)
	}
	if stats.TotalAmount != 200 {
		t.Fatalf("TotalAmount = %d, want 200", stats.TotalAmount)
	}
}

func TestExpirePartial(t *testing.T) {
	db := newTestDB(t)
	user, widget := seedUserAndWidget(t, db)
	now := time.Now().Unix()

	tx := &Transaction{
		UserID:          user.ID,
		WidgetID:        sql.NullString{String: widget.ID, Valid: true},
		Subaddress:      "partial-subaddress",
		SubaddressIndex: 3,
		Amount:          100,
		ReceivedAmount:  50,
		Status:          StatusPartial,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       now - 1,
	}
	if err := db.CreateTransaction(tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	expired, err := db.ExpireOldTransactions()
	if err != nil {
		t.Fatalf("ExpireOldTransactions: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}

	got, err := db.GetTransaction(tx.ID)
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got.Status != StatusExpired {
		t.Fatalf("Status = %s, want expired", got.Status)
	}
}

func TestGetActiveTransactionsIncludesPartial(t *testing.T) {
	db := newTestDB(t)
	user, widget := seedUserAndWidget(t, db)
	now := time.Now().Unix()

	tx := &Transaction{
		UserID:          user.ID,
		WidgetID:        sql.NullString{String: widget.ID, Valid: true},
		Subaddress:      "active-partial-subaddress",
		SubaddressIndex: 4,
		Amount:          100,
		ReceivedAmount:  50,
		Status:          StatusPartial,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       now + 3600,
	}
	if err := db.CreateTransaction(tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	active, err := db.GetActiveTransactions()
	if err != nil {
		t.Fatalf("GetActiveTransactions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active transactions = %d, want 1", len(active))
	}
	if active[0].Status != StatusPartial {
		t.Fatalf("Status = %s, want partial", active[0].Status)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().Unix()

	widget := &Widget{
		UserID:          "missing-user",
		Name:            "Widget",
		Mode:            ModeDonation,
		ButtonText:      "Donate",
		CustomMessage:   "Message",
		ThankYouMessage: "Thanks",
		PrimaryColor:    "#ff6600",
		Theme:           ThemeSystem,
		ShowStats:       true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.CreateWidget(widget); err == nil {
		t.Fatal("CreateWidget with a missing user succeeded, foreign keys are not enabled")
	}
}

func TestEmptyPathStarts(t *testing.T) {
	db, err := NewDB("")
	if err != nil {
		t.Fatalf("NewDB with empty path: %v", err)
	}
	defer func() { _ = db.Close() }()
}
