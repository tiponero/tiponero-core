package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func NewDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL: %w", err)
	}

	db := &DB{conn: conn}

	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
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
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
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
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS "transaction" (
		id TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id TEXT NOT NULL REFERENCES user(id),
		widget_id TEXT REFERENCES widget(id) ON DELETE SET NULL,
		subaddress TEXT NOT NULL UNIQUE,
		subaddress_index INTEGER NOT NULL,
		amount INTEGER,
		received_amount INTEGER NOT NULL DEFAULT 0,
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
		user_id         TEXT NOT NULL REFERENCES user(id),
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

	CREATE TABLE IF NOT EXISTS webhook (
		id         TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id    TEXT NOT NULL REFERENCES user(id),
		name       TEXT NOT NULL,
		url        TEXT NOT NULL,
		secret_enc TEXT NOT NULL,
		events     TEXT NOT NULL,
		active     INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_webhook_user ON webhook(user_id);

	CREATE TABLE IF NOT EXISTS notification_channel (
		id         TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id    TEXT NOT NULL REFERENCES user(id),
		type       TEXT NOT NULL,
		name       TEXT NOT NULL,
		config_enc TEXT NOT NULL,
		events     TEXT NOT NULL,
		active     INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_channel_user ON notification_channel(user_id);

	CREATE TABLE IF NOT EXISTS notification_delivery (
		id            TEXT PRIMARY KEY DEFAULT ` + uuidv4 + `,
		user_id       TEXT NOT NULL REFERENCES user(id),
		kind          TEXT NOT NULL,
		target_id     TEXT NOT NULL,
		event         TEXT NOT NULL,
		event_id      TEXT NOT NULL,
		payload       TEXT NOT NULL,
		status        TEXT NOT NULL DEFAULT 'pending',
		attempts      INTEGER NOT NULL DEFAULT 0,
		response_code INTEGER NOT NULL DEFAULT 0,
		last_error    TEXT,
		next_retry_at INTEGER NOT NULL,
		created_at    INTEGER NOT NULL,
		delivered_at  INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_delivery_due ON notification_delivery(status, next_retry_at);
	CREATE INDEX IF NOT EXISTS idx_delivery_target ON notification_delivery(target_id);
	`

	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}

	return nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// sqliteDSN adds per-connection pragmas to the database path. The SQLite
// foreign_keys pragma is not persistent, so it must be set on every connection.
func sqliteDSN(path string) string {
	if path == "" {
		return ""
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_foreign_keys=on"
}

// migrate adds columns that CREATE TABLE IF NOT EXISTS cannot add to an
// existing database. Every step is idempotent, so it runs on each start.
func (db *DB) migrate() error {
	columns := map[string]map[string]string{
		"widget": {
			"confirmations":  "INTEGER NOT NULL DEFAULT -1",
			"price_fiat":     "REAL NOT NULL DEFAULT 0",
			"price_currency": "TEXT NOT NULL DEFAULT ''",
			"goal_amount":    "REAL NOT NULL DEFAULT 0",
			"goal_currency":  "TEXT NOT NULL DEFAULT 'XMR'",
			"goal_deadline":  "INTEGER NOT NULL DEFAULT 0",
		},
		"transaction": {
			"received_amount":        "INTEGER NOT NULL DEFAULT 0",
			"description":            "TEXT",
			"redirect_url":           "TEXT",
			"callback_url":           "TEXT",
			"metadata":               "TEXT",
			"confirmations_required": "INTEGER NOT NULL DEFAULT -1",
			"is_test":                "INTEGER NOT NULL DEFAULT 0",
			"reverted_at":            "INTEGER NOT NULL DEFAULT 0",
		},
	}

	// Rename the old status. Underpaid and partial were the same state, and
	// two names for one state confused the API and the UI.
	if _, err := db.conn.Exec(`UPDATE "transaction" SET status = 'partial' WHERE status = 'underpaid'`); err != nil {
		return fmt.Errorf("rename underpaid status: %w", err)
	}
	if _, err := db.conn.Exec(`UPDATE "transaction" SET status = 'expired' WHERE status = 'expired_partial'`); err != nil {
		return fmt.Errorf("merge expired_partial into expired: %w", err)
	}

	for table, wanted := range columns {
		existing, err := db.tableColumns(table)
		if err != nil {
			return err
		}
		for name, definition := range wanted {
			if existing[name] {
				continue
			}
			stmt := fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, table, name, definition)
			if _, err := db.conn.Exec(stmt); err != nil {
				return fmt.Errorf("add %s.%s: %w", table, name, err)
			}
		}
	}

	return nil
}

func (db *DB) tableColumns(table string) (map[string]bool, error) {
	rows, err := db.conn.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return nil, fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan %s schema: %w", table, err)
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
