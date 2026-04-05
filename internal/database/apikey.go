package database

import (
	"database/sql"
	"time"
)

func (db *DB) CreateAPIKey(k *APIKey) error {
	return db.conn.QueryRow(
		`INSERT INTO api_key (user_id, name, key_hash, prefix, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		k.UserID, k.Name, k.KeyHash, k.Prefix, k.CreatedAt, k.ExpiresAt,
	).Scan(&k.ID)
}

func (db *DB) ListAPIKeys(userID string) ([]APIKey, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, name, prefix, created_at, expires_at, last_used_at
		 FROM api_key WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		k := APIKey{}
		var lastUsedAt sql.NullInt64
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.CreatedAt, &k.ExpiresAt, &lastUsedAt); err != nil {
			return nil, err
		}
		k.LastUsedAt = lastUsedAt.Int64
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// GetAPIKeysByPrefix returns all API keys matching the given prefix.
// There may be multiple keys sharing the same prefix; the caller must
// bcrypt-compare each candidate against the presented raw key.
func (db *DB) GetAPIKeysByPrefix(prefix string) ([]APIKey, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, name, key_hash, prefix, created_at, expires_at, last_used_at
		 FROM api_key WHERE prefix = ?`, prefix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		k := APIKey{}
		var lastUsedAt sql.NullInt64
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.Prefix, &k.CreatedAt, &k.ExpiresAt, &lastUsedAt); err != nil {
			return nil, err
		}
		k.LastUsedAt = lastUsedAt.Int64
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (db *DB) DeleteAPIKey(id, userID string) error {
	_, err := db.conn.Exec(`DELETE FROM api_key WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (db *DB) TouchAPIKey(id string) error {
	_, err := db.conn.Exec(`UPDATE api_key SET last_used_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}
