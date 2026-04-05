package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

type APIHandler struct {
	db           *database.DB
	wallet       *monero.ClientHolder
	walletEncKey []byte
	log          zerolog.Logger
}

func NewAPIHandler(db *database.DB, wallet *monero.ClientHolder, walletEncKey []byte, log zerolog.Logger) *APIHandler {
	return &APIHandler{
		db:           db,
		wallet:       wallet,
		walletEncKey: walletEncKey,
		log:          log.With().Str("component", "api").Logger(),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

type widgetJSON struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Mode            string    `json:"mode"`
	PresetAmounts   []float64 `json:"preset_amounts"`
	ButtonText      string    `json:"button_text"`
	CustomMessage   string    `json:"custom_message"`
	ThankYouMessage string    `json:"thank_you_message"`
	PrimaryColor    string    `json:"primary_color"`
	Theme           string    `json:"theme"`
	ShowStats       bool      `json:"show_stats"`
	RedirectURL     string    `json:"redirect_url"`
	CreatedAt       int64     `json:"created_at"`
	UpdatedAt       int64     `json:"updated_at"`
}

func toWidgetJSON(w *database.Widget) widgetJSON {
	var presets []float64
	if w.PresetAmounts != "" {
		json.Unmarshal([]byte(w.PresetAmounts), &presets)
	}
	if presets == nil {
		presets = []float64{}
	}
	return widgetJSON{
		ID:              w.ID,
		Name:            w.Name,
		Mode:            string(w.Mode),
		PresetAmounts:   presets,
		ButtonText:      w.ButtonText,
		CustomMessage:   w.CustomMessage,
		ThankYouMessage: w.ThankYouMessage,
		PrimaryColor:    w.PrimaryColor,
		Theme:           string(w.Theme),
		ShowStats:       w.ShowStats,
		RedirectURL:     w.RedirectURL,
		CreatedAt:       w.CreatedAt,
		UpdatedAt:       w.UpdatedAt,
	}
}

type transactionJSON struct {
	ID            string  `json:"id"`
	WidgetID      string  `json:"widget_id"`
	Subaddress    string  `json:"subaddress"`
	Amount        int64   `json:"amount"`
	AmountXMR     string  `json:"amount_xmr"`
	FiatAmount    float64 `json:"fiat_amount"`
	FiatCurrency  string  `json:"fiat_currency"`
	Status        string  `json:"status"`
	Confirmations int     `json:"confirmations"`
	IsPayment     bool    `json:"is_payment"`
	DonorName     string  `json:"donor_name"`
	Note          string  `json:"note"`
	TxHash        string  `json:"tx_hash"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
	ConfirmedAt   int64   `json:"confirmed_at"`
	ExpiresAt     int64   `json:"expires_at"`
}

func toTransactionJSON(t *database.Transaction) transactionJSON {
	xmr := strconv.FormatFloat(float64(t.Amount)/1e12, 'f', -1, 64)
	return transactionJSON{
		ID:            t.ID,
		WidgetID:      t.WidgetID.String,
		Subaddress:    t.Subaddress,
		Amount:        t.Amount,
		AmountXMR:     xmr,
		FiatAmount:    t.FiatAmount,
		FiatCurrency:  t.FiatCurrency,
		Status:        string(t.Status),
		Confirmations: t.Confirmations,
		IsPayment:     t.IsPayment,
		DonorName:     t.DonorName,
		Note:          t.Note,
		TxHash:        t.TxHash,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
		ConfirmedAt:   t.ConfirmedAt,
		ExpiresAt:     t.ExpiresAt,
	}
}

type userJSON struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
	Has2FA      bool   `json:"has_2fa"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func toUserJSON(u *database.User) userJSON {
	return userJSON{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		Has2FA:      u.TOTPSecret != "",
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

type walletJSON struct {
	ID         string `json:"id"`
	RPCURL     string `json:"rpc_url"`
	RPCUser    string `json:"rpc_user"`
	WalletFile string `json:"wallet_file"`
	Connected  bool   `json:"connected"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type statsJSON struct {
	TotalTransactions int    `json:"total_transactions"`
	TotalAmount       int64  `json:"total_amount"`
	TotalAmountXMR    string `json:"total_amount_xmr"`
	PendingCount      int    `json:"pending_count"`
	TodayCount        int    `json:"today_count"`
	TodayAmount       int64  `json:"today_amount"`
	TodayAmountXMR    string `json:"today_amount_xmr"`
}

type apiKeyJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  int64  `json:"created_at"`
	ExpiresAt  int64  `json:"expires_at"`
	LastUsedAt int64  `json:"last_used_at"`
}

func toAPIKeyJSON(k *database.APIKey) apiKeyJSON {
	return apiKeyJSON{
		ID:         k.ID,
		Name:       k.Name,
		Prefix:     "tip_" + k.Prefix + "...",
		CreatedAt:  k.CreatedAt,
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
	}
}

type paginationJSON struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func (h *APIHandler) ListWidgets(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	widgets, err := h.db.ListWidgets(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to list widgets")
		apiError(w, http.StatusInternalServerError, "failed to list widgets")
		return
	}

	out := make([]widgetJSON, len(widgets))
	for i := range widgets {
		out[i] = toWidgetJSON(&widgets[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *APIHandler) GetWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	widget, err := h.db.GetWidget(id)
	if err != nil {
		apiError(w, http.StatusNotFound, "widget not found")
		return
	}
	if widget.UserID != userID {
		apiError(w, http.StatusNotFound, "widget not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": toWidgetJSON(widget)})
}

func (h *APIHandler) CreateWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var body struct {
		Name            string    `json:"name"`
		Mode            string    `json:"mode"`
		PresetAmounts   []float64 `json:"preset_amounts"`
		ButtonText      string    `json:"button_text"`
		CustomMessage   string    `json:"custom_message"`
		ThankYouMessage string    `json:"thank_you_message"`
		PrimaryColor    string    `json:"primary_color"`
		Theme           string    `json:"theme"`
		ShowStats       *bool     `json:"show_stats"`
		RedirectURL     string    `json:"redirect_url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Name == "" {
		apiError(w, http.StatusBadRequest, "name is required")
		return
	}

	mode := parseMode(body.Mode)

	widget := &database.Widget{
		UserID:          userID,
		Name:            body.Name,
		Mode:            mode,
		ButtonText:      body.ButtonText,
		CustomMessage:   body.CustomMessage,
		ThankYouMessage: body.ThankYouMessage,
		PrimaryColor:    body.PrimaryColor,
		Theme:           parseTheme(body.Theme),
		RedirectURL:     body.RedirectURL,
	}

	if body.ShowStats != nil {
		widget.ShowStats = *body.ShowStats
	} else {
		widget.ShowStats = true
	}

	if widget.ButtonText == "" {
		if mode == database.ModePayment {
			widget.ButtonText = "Pay"
		} else {
			widget.ButtonText = "Donate"
		}
	}
	if widget.CustomMessage == "" {
		widget.CustomMessage = "Your support is appreciated!"
	}
	if widget.ThankYouMessage == "" {
		widget.ThankYouMessage = "Thank you for your donation!"
	}
	if widget.PrimaryColor == "" {
		widget.PrimaryColor = "#ff6600"
	} else if !validHexColor.MatchString(widget.PrimaryColor) {
		apiError(w, http.StatusBadRequest, "invalid color format, expected #RRGGBB")
		return
	}

	if mode == database.ModePayment {
		if len(body.PresetAmounts) != 1 || body.PresetAmounts[0] <= 0 {
			apiError(w, http.StatusBadRequest, "payment mode requires exactly one preset amount greater than zero")
			return
		}
		b, _ := json.Marshal(body.PresetAmounts)
		widget.PresetAmounts = string(b)
	} else if len(body.PresetAmounts) > 0 {
		valid := make([]float64, 0, len(body.PresetAmounts))
		for _, a := range body.PresetAmounts {
			if a > 0 {
				valid = append(valid, a)
			}
		}
		if len(valid) > 0 {
			b, _ := json.Marshal(valid)
			widget.PresetAmounts = string(b)
		}
	}

	widget.CreatedAt = time.Now().Unix()
	widget.UpdatedAt = widget.CreatedAt

	if err := h.db.CreateWidget(widget); err != nil {
		h.log.Error().Err(err).Msg("api: failed to create widget")
		apiError(w, http.StatusInternalServerError, "failed to create widget")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": widget.ID})
}

func (h *APIHandler) UpdateWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	existing, err := h.db.GetWidget(id)
	if err != nil {
		apiError(w, http.StatusNotFound, "widget not found")
		return
	}
	if existing.UserID != userID {
		apiError(w, http.StatusNotFound, "widget not found")
		return
	}

	var body map[string]json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if v, ok := body["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			existing.Name = s
		}
	}
	if v, ok := body["mode"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			existing.Mode = parseMode(s)
		}
	}
	if v, ok := body["preset_amounts"]; ok {
		var amounts []float64
		if json.Unmarshal(v, &amounts) == nil {
			if existing.Mode == database.ModePayment {
				if len(amounts) != 1 || amounts[0] <= 0 {
					apiError(w, http.StatusBadRequest, "payment mode requires exactly one preset amount greater than zero")
					return
				}
			}
			valid := make([]float64, 0, len(amounts))
			for _, a := range amounts {
				if a > 0 {
					valid = append(valid, a)
				}
			}
			if len(valid) > 0 {
				b, _ := json.Marshal(valid)
				existing.PresetAmounts = string(b)
			} else {
				existing.PresetAmounts = ""
			}
		}
	}
	if v, ok := body["button_text"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			existing.ButtonText = s
		}
	}
	if v, ok := body["custom_message"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			existing.CustomMessage = s
		}
	}
	if v, ok := body["thank_you_message"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			existing.ThankYouMessage = s
		}
	}
	if v, ok := body["primary_color"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if !validHexColor.MatchString(s) {
				apiError(w, http.StatusBadRequest, "invalid color format, expected #RRGGBB")
				return
			}
			existing.PrimaryColor = s
		}
	}
	if v, ok := body["theme"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			existing.Theme = parseTheme(s)
		}
	}
	if v, ok := body["show_stats"]; ok {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			existing.ShowStats = b
		}
	}
	if v, ok := body["redirect_url"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			existing.RedirectURL = s
		}
	}

	if existing.Mode == database.ModePayment {
		var amounts []float64
		if existing.PresetAmounts != "" {
			json.Unmarshal([]byte(existing.PresetAmounts), &amounts)
		}
		if len(amounts) != 1 || amounts[0] <= 0 {
			apiError(w, http.StatusBadRequest, "payment mode requires exactly one preset amount greater than zero")
			return
		}
	}

	if err := h.db.UpdateWidget(existing); err != nil {
		h.log.Error().Err(err).Msg("api: failed to update widget")
		apiError(w, http.StatusInternalServerError, "failed to update widget")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": toWidgetJSON(existing)})
}

func (h *APIHandler) DeleteWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.db.DeleteWidget(id, userID); err != nil {
		h.log.Error().Err(err).Msg("api: failed to delete widget")
		apiError(w, http.StatusInternalServerError, "failed to delete widget")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (h *APIHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	status := database.TransactionStatus(r.URL.Query().Get("status"))

	total, err := h.db.CountTransactions(userID, status)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to count transactions")
		apiError(w, http.StatusInternalServerError, "failed to list transactions")
		return
	}

	skip := (page - 1) * limit
	txs, err := h.db.ListTransactions(userID, database.TransactionFilter{
		Status: status,
		Skip:   skip,
		Take:   limit,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to list transactions")
		apiError(w, http.StatusInternalServerError, "failed to list transactions")
		return
	}

	out := make([]transactionJSON, len(txs))
	for i := range txs {
		out[i] = toTransactionJSON(&txs[i])
	}

	totalPages := (total + limit - 1) / limit
	writeJSON(w, http.StatusOK, map[string]any{
		"data": out,
		"pagination": paginationJSON{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

func (h *APIHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	tx, err := h.db.GetTransaction(id)
	if err != nil {
		apiError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if tx.UserID != userID {
		apiError(w, http.StatusNotFound, "transaction not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": toTransactionJSON(tx)})
}

func (h *APIHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	user, err := h.db.GetUser()
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to get user")
		apiError(w, http.StatusInternalServerError, "failed to get user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": toUserJSON(user)})
}

func (h *APIHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	user, err := h.db.GetUser()
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to get user")
		apiError(w, http.StatusInternalServerError, "failed to get user")
		return
	}

	var body map[string]json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if v, ok := body["display_name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			user.DisplayName = s
		}
	}
	if v, ok := body["bio"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			user.Bio = s
		}
	}
	if v, ok := body["avatar_url"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			user.AvatarURL = s
		}
	}

	if err := h.db.UpdateUser(user); err != nil {
		h.log.Error().Err(err).Msg("api: failed to update user")
		apiError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": toUserJSON(user)})
}

func (h *APIHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	exists, err := h.db.WalletExists()
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to check wallet existence")
		apiError(w, http.StatusInternalServerError, "failed to create wallet")
		return
	}
	if exists {
		apiError(w, http.StatusConflict, "wallet already exists")
		return
	}

	var body struct {
		RPCURL         string `json:"rpc_url"`
		RPCUser        string `json:"rpc_user"`
		RPCPassword    string `json:"rpc_password"`
		WalletFile     string `json:"wallet_file"`
		WalletPassword string `json:"wallet_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	cfg := &database.WalletConfig{
		RPCURL:         body.RPCURL,
		RPCUser:        body.RPCUser,
		RPCPassword:    body.RPCPassword,
		WalletFile:     body.WalletFile,
		WalletPassword: body.WalletPassword,
	}

	if err := h.db.CreateWalletConfig(h.walletEncKey, cfg); err != nil {
		h.log.Error().Err(err).Msg("api: failed to create wallet")
		apiError(w, http.StatusInternalServerError, "failed to create wallet")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": cfg.ID})
}

func (h *APIHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.db.GetWalletConfig(h.walletEncKey)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to get wallet config")
		apiError(w, http.StatusInternalServerError, "failed to get wallet config")
		return
	}

	status := h.wallet.Status()
	writeJSON(w, http.StatusOK, map[string]any{"data": walletJSON{
		ID:         cfg.ID,
		RPCURL:     cfg.RPCURL,
		RPCUser:    cfg.RPCUser,
		WalletFile: cfg.WalletFile,
		Connected:  status.Connected,
		CreatedAt:  cfg.CreatedAt,
		UpdatedAt:  cfg.UpdatedAt,
	}})
}

func (h *APIHandler) UpdateWallet(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.db.GetWalletConfig(h.walletEncKey)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to get wallet config")
		apiError(w, http.StatusInternalServerError, "failed to get wallet config")
		return
	}

	var body map[string]json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if v, ok := body["rpc_url"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			cfg.RPCURL = s
		}
	}
	if v, ok := body["rpc_user"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			cfg.RPCUser = s
		}
	}
	if v, ok := body["wallet_file"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			cfg.WalletFile = s
		}
	}

	if err := h.db.SaveWalletConfig(h.walletEncKey, cfg); err != nil {
		h.log.Error().Err(err).Msg("api: failed to save wallet config")
		apiError(w, http.StatusInternalServerError, "failed to update wallet config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": walletJSON{
		ID:         cfg.ID,
		RPCURL:     cfg.RPCURL,
		RPCUser:    cfg.RPCUser,
		WalletFile: cfg.WalletFile,
		Connected:  h.wallet.Status().Connected,
		CreatedAt:  cfg.CreatedAt,
		UpdatedAt:  cfg.UpdatedAt,
	}})
}

func (h *APIHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	stats, err := h.db.GetTransactionStats(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to get stats")
		apiError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": statsJSON{
		TotalTransactions: stats.TotalTransactions,
		TotalAmount:       stats.TotalAmount,
		TotalAmountXMR:    strconv.FormatFloat(float64(stats.TotalAmount)/1e12, 'f', -1, 64),
		PendingCount:      stats.PendingCount,
		TodayCount:        stats.TodayCount,
		TodayAmount:       stats.TodayAmount,
		TodayAmountXMR:    strconv.FormatFloat(float64(stats.TodayAmount)/1e12, 'f', -1, 64),
	}})
}

func (h *APIHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	keys, err := h.db.ListAPIKeys(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("api: failed to list api keys")
		apiError(w, http.StatusInternalServerError, "failed to list api keys")
		return
	}

	out := make([]apiKeyJSON, len(keys))
	for i := range keys {
		out[i] = toAPIKeyJSON(&keys[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *APIHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.db.DeleteAPIKey(id, userID); err != nil {
		h.log.Error().Err(err).Msg("api: failed to delete api key")
		apiError(w, http.StatusInternalServerError, "failed to delete api key")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}
