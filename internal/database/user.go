package database

import (
	"database/sql"
	"time"
)

func (db *DB) CreateUser(u *User) error {
	return db.conn.QueryRow(
		`INSERT INTO user (username, password_hash, display_name, bio, avatar_url, totp_secret, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		u.Username, u.PasswordHash, nullStr(u.DisplayName), nullStr(u.Bio), nullStr(u.AvatarURL), nullStr(u.TOTPSecret), u.CreatedAt, u.UpdatedAt,
	).Scan(&u.ID)
}

func (db *DB) GetUser() (*User, error) {
	return db.scanUser(db.conn.QueryRow(
		`SELECT id, username, password_hash, display_name, bio, avatar_url, totp_secret, created_at, updated_at
		 FROM user LIMIT 1`,
	))
}

func (db *DB) GetUserByUsername(username string) (*User, error) {
	return db.scanUser(db.conn.QueryRow(
		`SELECT id, username, password_hash, display_name, bio, avatar_url, totp_secret, created_at, updated_at
		 FROM user WHERE username = ?`, username,
	))
}

func (db *DB) UpdateUser(u *User) error {
	u.UpdatedAt = time.Now().Unix()
	_, err := db.conn.Exec(
		`UPDATE user SET username = ?, display_name = ?, bio = ?, avatar_url = ?, password_hash = ?, totp_secret = ?, updated_at = ?
		 WHERE id = ?`,
		u.Username, nullStr(u.DisplayName), nullStr(u.Bio), nullStr(u.AvatarURL), u.PasswordHash, nullStr(u.TOTPSecret), u.UpdatedAt, u.ID,
	)
	return err
}

func (db *DB) scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	var displayName, bio, avatarURL, totpSecret sql.NullString
	err := row.Scan(
		&u.ID, &u.Username, &u.PasswordHash,
		&displayName, &bio, &avatarURL, &totpSecret, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.DisplayName = displayName.String
	u.Bio = bio.String
	u.AvatarURL = avatarURL.String
	u.TOTPSecret = totpSecret.String
	return u, nil
}
