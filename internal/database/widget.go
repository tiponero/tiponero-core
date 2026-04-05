package database

import "database/sql"

func (db *DB) CreateWidget(w *Widget) error {
	return db.conn.QueryRow(
		`INSERT INTO widget (user_id, name, mode, preset_amounts, button_text, custom_message, thank_you_message, primary_color, theme, show_stats, redirect_url, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		w.UserID, w.Name, w.Mode, nullStr(w.PresetAmounts), w.ButtonText, w.CustomMessage, w.ThankYouMessage, w.PrimaryColor, w.Theme, w.ShowStats, nullStr(w.RedirectURL), w.CreatedAt,
	).Scan(&w.ID)
}

func (db *DB) GetWidget(id string) (*Widget, error) {
	return db.scanWidget(db.conn.QueryRow(
		`SELECT `+widgetColumns+` FROM widget WHERE id = ?`, id,
	))
}

func (db *DB) ListWidgets(userID string) ([]Widget, error) {
	rows, err := db.conn.Query(
		`SELECT `+widgetColumns+` FROM widget WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var widgets []Widget
	for rows.Next() {
		w := Widget{}
		var presetAmounts, redirectURL sql.NullString
		err := rows.Scan(
			&w.ID, &w.UserID, &w.Name, &w.Mode, &presetAmounts,
			&w.ButtonText, &w.CustomMessage, &w.ThankYouMessage, &w.PrimaryColor, &w.Theme, &w.ShowStats, &redirectURL, &w.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		w.PresetAmounts = presetAmounts.String
		w.RedirectURL = redirectURL.String
		widgets = append(widgets, w)
	}
	return widgets, rows.Err()
}

func (db *DB) DeleteWidget(id, userID string) error {
	_, err := db.conn.Exec(`DELETE FROM widget WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (db *DB) UpdateWidget(w *Widget) error {
	_, err := db.conn.Exec(
		`UPDATE widget SET name = ?, mode = ?, preset_amounts = ?, button_text = ?, custom_message = ?,
		 thank_you_message = ?, primary_color = ?, theme = ?, show_stats = ?, redirect_url = ?
		 WHERE id = ? AND user_id = ?`,
		w.Name, w.Mode, nullStr(w.PresetAmounts), w.ButtonText, w.CustomMessage,
		w.ThankYouMessage, w.PrimaryColor, w.Theme, w.ShowStats, nullStr(w.RedirectURL),
		w.ID, w.UserID,
	)
	return err
}

const widgetColumns = `id, user_id, name, mode, preset_amounts, button_text, custom_message, thank_you_message, primary_color, theme, show_stats, redirect_url, created_at`

func (db *DB) scanWidget(row *sql.Row) (*Widget, error) {
	w := &Widget{}
	var presetAmounts, redirectURL sql.NullString
	err := row.Scan(
		&w.ID, &w.UserID, &w.Name, &w.Mode, &presetAmounts,
		&w.ButtonText, &w.CustomMessage, &w.ThankYouMessage, &w.PrimaryColor, &w.Theme, &w.ShowStats, &redirectURL, &w.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	w.PresetAmounts = presetAmounts.String
	w.RedirectURL = redirectURL.String
	return w, nil
}
