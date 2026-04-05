package database

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func NewDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	db := &DB{conn: conn}

	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

func (db *DB) initSchema() error {
	const uuidv4 = `(lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab',abs(random())%4+1,1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))))`

	schema := `
	CREATE TABLE IF NOT EXISTS user (
		id TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name TEXT,
		bio TEXT,
		avatar_url TEXT,
		totp_secret TEXT,
		created_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS widget (
		id TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id TEXT NOT NULL REFERENCES user(id),
		name TEXT NOT NULL,
		mode TEXT NOT NULL DEFAULT 'donation',
		theme TEXT NOT NULL DEFAULT 'system',
		show_stats INTEGER NOT NULL DEFAULT 1,
		preset_amounts TEXT,
		button_text TEXT DEFAULT 'Donate',
		custom_message TEXT DEFAULT 'Your support is appreciated!',
		thank_you_message TEXT DEFAULT 'Thank you for your donation!',
		primary_color TEXT DEFAULT '#ff6600',
		redirect_url TEXT,
		created_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS "transaction" (
		id TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id TEXT NOT NULL REFERENCES user(id),
		widget_id TEXT REFERENCES widget(id) ON DELETE SET NULL,
		subaddress TEXT NOT NULL UNIQUE,
		subaddress_index INTEGER NOT NULL,
		amount INTEGER,
		fiat_amount REAL,
		fiat_currency TEXT,
		status TEXT NOT NULL DEFAULT 'pending',
		confirmations INTEGER DEFAULT 0,
		is_payment INTEGER NOT NULL DEFAULT 0,
		donor_name TEXT,
		note TEXT,
		tx_hash TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		confirmed_at INTEGER,
		expires_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_transaction_status ON "transaction"(status);
	CREATE INDEX IF NOT EXISTS idx_transaction_subaddress ON "transaction"(subaddress);
	CREATE INDEX IF NOT EXISTS idx_transaction_user ON "transaction"(user_id);
	CREATE INDEX IF NOT EXISTS idx_transaction_widget ON "transaction"(widget_id);
	CREATE INDEX IF NOT EXISTS idx_widget_user ON widget(user_id);

	CREATE TABLE IF NOT EXISTS wallet (
		id              TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		rpc_url         TEXT,
		rpc_user        TEXT,
		rpc_password    TEXT,
		wallet_file     TEXT,
		wallet_password TEXT,
		created_at      INTEGER NOT NULL,
		updated_at      INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS api_key (
		id           TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id      TEXT NOT NULL REFERENCES user(id),
		name         TEXT NOT NULL,
		key_hash     TEXT NOT NULL UNIQUE,
		prefix       TEXT NOT NULL,
		created_at   INTEGER NOT NULL,
		expires_at   INTEGER NOT NULL,
		last_used_at INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_api_key_prefix ON api_key(prefix);
	CREATE INDEX IF NOT EXISTS idx_api_key_user ON api_key(user_id);
	`

	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}

	return nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
