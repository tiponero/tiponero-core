package database

import (
	"strings"
	"time"
)

// SplitEvents turns the stored event list into a slice.
func SplitEvents(events string) []string {
	if strings.TrimSpace(events) == "" {
		return nil
	}
	parts := strings.Split(events, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// JoinEvents stores an event list in the canonical form.
func JoinEvents(events []string) string {
	seen := make(map[string]bool, len(events))
	out := make([]string, 0, len(events))
	for _, e := range events {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return strings.Join(out, ",")
}

// Subscribes reports whether an endpoint wants the given event.
func Subscribes(events, event string) bool {
	for _, e := range SplitEvents(events) {
		if e == event {
			return true
		}
	}
	return false
}

func (db *DB) CreateWebhook(w *Webhook) error {
	return db.conn.QueryRow(
		`INSERT INTO webhook (user_id, name, url, secret_enc, events, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		w.UserID, w.Name, w.URL, w.SecretEnc, w.Events, w.Active, w.CreatedAt, w.UpdatedAt,
	).Scan(&w.ID)
}

func (db *DB) GetWebhook(id, userID string) (*Webhook, error) {
	w := &Webhook{}
	err := db.conn.QueryRow(
		`SELECT id, user_id, name, url, secret_enc, events, active, created_at, updated_at
		 FROM webhook WHERE id = ? AND user_id = ?`, id, userID,
	).Scan(&w.ID, &w.UserID, &w.Name, &w.URL, &w.SecretEnc, &w.Events, &w.Active, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (db *DB) ListWebhooks(userID string) ([]Webhook, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, name, url, secret_enc, events, active, created_at, updated_at
		 FROM webhook WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Webhook
	for rows.Next() {
		w := Webhook{}
		if err := rows.Scan(&w.ID, &w.UserID, &w.Name, &w.URL, &w.SecretEnc, &w.Events, &w.Active, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListActiveWebhooks returns every active webhook for the user.
func (db *DB) ListActiveWebhooks(userID string) ([]Webhook, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, name, url, secret_enc, events, active, created_at, updated_at
		 FROM webhook WHERE user_id = ? AND active = 1`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Webhook
	for rows.Next() {
		w := Webhook{}
		if err := rows.Scan(&w.ID, &w.UserID, &w.Name, &w.URL, &w.SecretEnc, &w.Events, &w.Active, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (db *DB) UpdateWebhook(w *Webhook) error {
	w.UpdatedAt = time.Now().Unix()
	_, err := db.conn.Exec(
		`UPDATE webhook SET name = ?, url = ?, events = ?, active = ?, updated_at = ?
		 WHERE id = ? AND user_id = ?`,
		w.Name, w.URL, w.Events, w.Active, w.UpdatedAt, w.ID, w.UserID,
	)
	return err
}

// UpdateWebhookSecret replaces the signing secret. The plaintext never leaves
// the caller: only the encrypted value is stored.
func (db *DB) UpdateWebhookSecret(id, userID, secretEnc string) error {
	_, err := db.conn.Exec(
		`UPDATE webhook SET secret_enc = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		secretEnc, time.Now().Unix(), id, userID,
	)
	return err
}

func (db *DB) DeleteWebhook(id, userID string) error {
	_, err := db.conn.Exec(`DELETE FROM webhook WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (db *DB) CreateChannel(c *NotificationChannel) error {
	return db.conn.QueryRow(
		`INSERT INTO notification_channel (user_id, type, name, config_enc, events, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		c.UserID, c.Type, c.Name, c.ConfigEnc, c.Events, c.Active, c.CreatedAt, c.UpdatedAt,
	).Scan(&c.ID)
}

func (db *DB) GetChannel(id, userID string) (*NotificationChannel, error) {
	c := &NotificationChannel{}
	err := db.conn.QueryRow(
		`SELECT id, user_id, type, name, config_enc, events, active, created_at, updated_at
		 FROM notification_channel WHERE id = ? AND user_id = ?`, id, userID,
	).Scan(&c.ID, &c.UserID, &c.Type, &c.Name, &c.ConfigEnc, &c.Events, &c.Active, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (db *DB) ListChannels(userID string) ([]NotificationChannel, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, type, name, config_enc, events, active, created_at, updated_at
		 FROM notification_channel WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []NotificationChannel
	for rows.Next() {
		c := NotificationChannel{}
		if err := rows.Scan(&c.ID, &c.UserID, &c.Type, &c.Name, &c.ConfigEnc, &c.Events, &c.Active, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) ListActiveChannels(userID string) ([]NotificationChannel, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, type, name, config_enc, events, active, created_at, updated_at
		 FROM notification_channel WHERE user_id = ? AND active = 1`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []NotificationChannel
	for rows.Next() {
		c := NotificationChannel{}
		if err := rows.Scan(&c.ID, &c.UserID, &c.Type, &c.Name, &c.ConfigEnc, &c.Events, &c.Active, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) UpdateChannel(c *NotificationChannel) error {
	c.UpdatedAt = time.Now().Unix()
	_, err := db.conn.Exec(
		`UPDATE notification_channel SET type = ?, name = ?, config_enc = ?, events = ?, active = ?, updated_at = ?
		 WHERE id = ? AND user_id = ?`,
		c.Type, c.Name, c.ConfigEnc, c.Events, c.Active, c.UpdatedAt, c.ID, c.UserID,
	)
	return err
}

func (db *DB) DeleteChannel(id, userID string) error {
	_, err := db.conn.Exec(`DELETE FROM notification_channel WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (db *DB) EnqueueDelivery(d *NotificationDelivery) error {
	return db.conn.QueryRow(
		`INSERT INTO notification_delivery (user_id, kind, target_id, event, event_id, payload, status, attempts,
		 response_code, last_error, next_retry_at, created_at, delivered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		d.UserID, d.Kind, d.TargetID, d.Event, d.EventID, d.Payload, d.Status, d.Attempts,
		d.ResponseCode, nullStr(d.LastError), d.NextRetryAt, d.CreatedAt, d.DeliveredAt,
	).Scan(&d.ID)
}

// DueDeliveries returns queued deliveries whose retry time has passed.
func (db *DB) DueDeliveries(now int64, limit int) ([]NotificationDelivery, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, kind, target_id, event, event_id, payload, status, attempts,
		 response_code, COALESCE(last_error, ''), next_retry_at, created_at, delivered_at
		 FROM notification_delivery
		 WHERE status = ? AND next_retry_at <= ?
		 ORDER BY next_retry_at ASC LIMIT ?`,
		DeliveryPending, now, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []NotificationDelivery
	for rows.Next() {
		d := NotificationDelivery{}
		if err := rows.Scan(&d.ID, &d.UserID, &d.Kind, &d.TargetID, &d.Event, &d.EventID, &d.Payload, &d.Status,
			&d.Attempts, &d.ResponseCode, &d.LastError, &d.NextRetryAt, &d.CreatedAt, &d.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) MarkDeliveryDelivered(id string, code int) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(
		`UPDATE notification_delivery SET status = ?, attempts = attempts + 1, response_code = ?,
		 last_error = NULL, delivered_at = ? WHERE id = ?`,
		DeliveryDelivered, code, now, id,
	)
	return err
}

func (db *DB) MarkDeliveryRetry(id string, code int, lastErr string, nextRetry int64) error {
	_, err := db.conn.Exec(
		`UPDATE notification_delivery SET attempts = attempts + 1, response_code = ?, last_error = ?,
		 next_retry_at = ? WHERE id = ?`,
		code, nullStr(lastErr), nextRetry, id,
	)
	return err
}

func (db *DB) MarkDeliveryFailed(id string, code int, lastErr string) error {
	_, err := db.conn.Exec(
		`UPDATE notification_delivery SET status = ?, attempts = attempts + 1, response_code = ?, last_error = ?
		 WHERE id = ?`,
		DeliveryFailed, code, nullStr(lastErr), id,
	)
	return err
}

func (db *DB) ListDeliveries(userID string, limit int) ([]NotificationDelivery, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, kind, target_id, event, event_id, payload, status, attempts,
		 response_code, COALESCE(last_error, ''), next_retry_at, created_at, delivered_at
		 FROM notification_delivery WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []NotificationDelivery
	for rows.Next() {
		d := NotificationDelivery{}
		if err := rows.Scan(&d.ID, &d.UserID, &d.Kind, &d.TargetID, &d.Event, &d.EventID, &d.Payload, &d.Status,
			&d.Attempts, &d.ResponseCode, &d.LastError, &d.NextRetryAt, &d.CreatedAt, &d.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PruneDeliveries keeps the delivery log small: delivered rows older than the
// cutoff are deleted. Failed rows stay for inspection.
func (db *DB) PruneDeliveries(cutoff int64) error {
	_, err := db.conn.Exec(
		`DELETE FROM notification_delivery WHERE status = ? AND delivered_at > 0 AND delivered_at < ?`,
		DeliveryDelivered, cutoff,
	)
	return err
}

// DeliveryCounts returns pending and failed totals for the admin summary.
func (db *DB) DeliveryCounts(userID string) (pending, failed int, err error) {
	err = db.conn.QueryRow(
		`SELECT
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		 FROM notification_delivery WHERE user_id = ?`,
		DeliveryPending, DeliveryFailed, userID,
	).Scan(&pending, &failed)
	return pending, failed, err
}
