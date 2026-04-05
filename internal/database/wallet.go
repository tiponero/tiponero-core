package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tiponero/tiponero-core/internal/crypto"
)

func (db *DB) WalletExists(userID string) (bool, error) {
	var count int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM wallet WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return false, fmt.Errorf("check wallet exists: %w", err)
	}
	return count > 0, nil
}

func (db *DB) GetWalletConfig(encKey []byte, userID string) (*WalletConfig, error) {
	var cfg WalletConfig
	var rpcURL, rpcUser, encRPCPass, walletFile, encWalletPass sql.NullString

	err := db.conn.QueryRow(`
		SELECT id, user_id, rpc_url, rpc_user, rpc_password, wallet_file, wallet_password, created_at, updated_at
		FROM wallet WHERE user_id = ? LIMIT 1
	`, userID).Scan(&cfg.ID, &cfg.UserID, &rpcURL, &rpcUser, &encRPCPass, &walletFile, &encWalletPass, &cfg.CreatedAt, &cfg.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &WalletConfig{UserID: userID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet config: %w", err)
	}

	cfg.RPCURL = rpcURL.String
	cfg.RPCUser = rpcUser.String
	cfg.WalletFile = walletFile.String

	if cfg.RPCPassword, err = crypto.Decrypt(encKey, encRPCPass.String); err != nil {
		return nil, fmt.Errorf("decrypt rpc_password: %w", err)
	}
	if cfg.WalletPassword, err = crypto.Decrypt(encKey, encWalletPass.String); err != nil {
		return nil, fmt.Errorf("decrypt wallet_password: %w", err)
	}

	return &cfg, nil
}

func (db *DB) CreateWalletConfig(encKey []byte, cfg *WalletConfig) error {
	encRPCPass, err := crypto.Encrypt(encKey, cfg.RPCPassword)
	if err != nil {
		return fmt.Errorf("encrypt rpc_password: %w", err)
	}
	encWalletPass, err := crypto.Encrypt(encKey, cfg.WalletPassword)
	if err != nil {
		return fmt.Errorf("encrypt wallet_password: %w", err)
	}

	now := time.Now().Unix()
	return db.conn.QueryRow(`
		INSERT INTO wallet (user_id, rpc_url, rpc_user, rpc_password, wallet_file, wallet_password, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		cfg.UserID, nullStr(cfg.RPCURL), nullStr(cfg.RPCUser), nullStr(encRPCPass),
		nullStr(cfg.WalletFile), nullStr(encWalletPass), now, now,
	).Scan(&cfg.ID)
}

func (db *DB) SaveWalletConfig(encKey []byte, cfg *WalletConfig) error {
	encRPCPass, err := crypto.Encrypt(encKey, cfg.RPCPassword)
	if err != nil {
		return fmt.Errorf("encrypt rpc_password: %w", err)
	}
	encWalletPass, err := crypto.Encrypt(encKey, cfg.WalletPassword)
	if err != nil {
		return fmt.Errorf("encrypt wallet_password: %w", err)
	}

	now := time.Now().Unix()

	var count int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM wallet WHERE user_id = ?`, cfg.UserID).Scan(&count); err != nil {
		return fmt.Errorf("check wallet row: %w", err)
	}

	if count == 0 {
		_, err = db.conn.Exec(`
			INSERT INTO wallet (user_id, rpc_url, rpc_user, rpc_password, wallet_file, wallet_password, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, cfg.UserID, nullStr(cfg.RPCURL), nullStr(cfg.RPCUser), nullStr(encRPCPass), nullStr(cfg.WalletFile), nullStr(encWalletPass), now, now)
	} else {
		_, err = db.conn.Exec(`
			UPDATE wallet SET
				rpc_url = ?, rpc_user = ?, rpc_password = ?,
				wallet_file = ?, wallet_password = ?,
				updated_at = ?
			WHERE user_id = ?
		`, nullStr(cfg.RPCURL), nullStr(cfg.RPCUser), nullStr(encRPCPass), nullStr(cfg.WalletFile), nullStr(encWalletPass), now, cfg.UserID)
	}
	if err != nil {
		return fmt.Errorf("save wallet config: %w", err)
	}
	return nil
}
