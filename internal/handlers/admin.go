package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
	"github.com/tiponero/tiponero-core/internal/services"
	adminviews "github.com/tiponero/tiponero-core/internal/views/admin"
	"github.com/tiponero/tiponero-core/internal/views/components"
	"golang.org/x/crypto/bcrypt"
)

var validHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type AdminHandler struct {
	db                    *database.DB
	auth                  *auth.Service
	wallet                *monero.ClientHolder
	walletEncKey          []byte
	log                   zerolog.Logger
	hostURL               string
	requiredConfirmations int
}

func NewAdminHandler(db *database.DB, authSvc *auth.Service, wallet *monero.ClientHolder, walletEncKey []byte, hostURL string, requiredConfirmations int, log zerolog.Logger) *AdminHandler {
	return &AdminHandler{
		db:                    db,
		auth:                  authSvc,
		wallet:                wallet,
		walletEncKey:          walletEncKey,
		hostURL:               hostURL,
		requiredConfirmations: requiredConfirmations,
		log:                   log.With().Str("component", "admin").Logger(),
	}
}

func (h *AdminHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	render(r.Context(), w, adminviews.Login(""), h.log)
}

func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := h.db.GetUserByUsername(username)
	if err != nil || !auth.CheckPassword(user.PasswordHash, password) {
		render(r.Context(), w, adminviews.Login("Invalid username or password"), h.log)
		return
	}

	if user.TOTPSecret != "" {
		if err := h.auth.SetPendingTOTP(w, r, user.ID); err != nil {
			h.log.Error().Err(err).Msg("failed to set pending TOTP session")
		}
		render(r.Context(), w, adminviews.TOTPVerify(""), h.log)
		return
	}

	if err := h.auth.CreateSession(w, r, user.ID); err != nil {
		h.log.Error().Err(err).Msg("failed to create session")
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *AdminHandler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.auth.GetPendingTOTP(r)
	if !ok || userID == "" {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		if err := h.auth.ClearPendingTOTP(w, r); err != nil {
			h.log.Error().Err(err).Msg("failed to clear pending TOTP session")
		}
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	code := r.FormValue("code")
	if !services.ValidateTOTPCode(user.TOTPSecret, code) {
		render(r.Context(), w, adminviews.TOTPVerify("Invalid code. Please try again."), h.log)
		return
	}

	if err := h.auth.ClearPendingTOTP(w, r); err != nil {
		h.log.Error().Err(err).Msg("failed to clear pending TOTP session")
	}
	if err := h.auth.CreateSession(w, r, user.ID); err != nil {
		h.log.Error().Err(err).Msg("failed to create session after TOTP")
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.auth.DestroySession(w, r); err != nil {
		h.log.Error().Err(err).Msg("failed to destroy session")
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *AdminHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	stats, err := h.db.GetTransactionStats(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load transaction stats")
		http.Error(w, "Failed to load stats", http.StatusInternalServerError)
		return
	}

	recent, err := h.db.ListTransactions(userID, database.TransactionFilter{Take: 10})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load recent transactions")
		http.Error(w, "Failed to load transactions", http.StatusInternalServerError)
		return
	}

	status := h.wallet.Status()
	render(r.Context(), w, adminviews.Dashboard(stats, recent, status), h.log)
}

func (h *AdminHandler) Transactions(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	statusFilter := r.URL.Query().Get("status")

	const perPage = 10
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	status := database.TransactionStatus(statusFilter)

	total, err := h.db.CountTransactions(userID, status)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to count transactions")
		http.Error(w, "Failed to load transactions", http.StatusInternalServerError)
		return
	}

	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	transactions, err := h.db.ListTransactions(userID, database.TransactionFilter{
		Status: status,
		Take:   perPage,
		Skip:   (page - 1) * perPage,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load transactions list")
		http.Error(w, "Failed to load transactions", http.StatusInternalServerError)
		return
	}

	pag := database.Pagination{
		Page:       page,
		Skip:       (page - 1) * perPage,
		Take:       perPage,
		Total:      total,
		TotalPages: totalPages,
	}

	render(r.Context(), w, adminviews.Transactions(transactions, statusFilter, pag, h.wallet.Status()), h.log)
}

func (h *AdminHandler) TransactionDetail(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	txID := r.PathValue("id")

	tx, err := h.db.GetTransaction(txID)
	if err != nil || tx.UserID != userID {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	var widget *database.Widget
	if tx.WidgetID.Valid {
		widget, _ = h.db.GetWidget(tx.WidgetID.String)
	}

	render(r.Context(), w, adminviews.TransactionDetail(tx, widget, h.requiredConfirmations, h.wallet.Status()), h.log)
}

func (h *AdminHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	transactions, err := h.db.ListTransactions(userID, database.TransactionFilter{})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load transactions for CSV export")
		http.Error(w, "Failed to load transactions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=transactions.csv")

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"ID", "Type", "Amount (XMR)", "Fiat Amount", "Fiat Currency", "Status", "Donor", "Note", "TX Hash", "Created", "Confirmed"})

	for _, t := range transactions {
		txType := "donation"
		if t.IsPayment {
			txType = "payment"
		}
		_ = writer.Write([]string{
			t.ID,
			txType,
			components.FormatXMR(t.Amount),
			fmt.Sprintf("%.2f", t.FiatAmount),
			t.FiatCurrency,
			string(t.Status),
			t.DonorName,
			t.Note,
			t.TxHash,
			time.Unix(t.CreatedAt, 0).Format(time.RFC3339),
			formatOptionalTime(t.ConfirmedAt),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		h.log.Error().Err(err).Msg("csv export flush failed")
	}
}

func (h *AdminHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		h.log.Error().Err(err).Msg("failed to load user for settings page")
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	walletCfg, err := h.db.GetWalletConfig(h.walletEncKey)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load wallet config")
		http.Error(w, "Failed to load wallet config", http.StatusInternalServerError)
		return
	}

	apiKeys, err := h.db.ListAPIKeys(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load api keys")
		apiKeys = nil
	}

	status := h.wallet.Status()
	render(r.Context(), w, adminviews.Settings(user, walletCfg, status, apiKeys), h.log)
}

func (h *AdminHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		h.log.Error().Err(err).Msg("failed to load user for profile update")
		triggerToast(w, "error", "User not found.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	user.Username = r.FormValue("username")
	user.DisplayName = r.FormValue("display_name")
	user.Bio = r.FormValue("bio")
	user.AvatarURL = r.FormValue("avatar_url")
	if err := h.db.UpdateUser(user); err != nil {
		h.log.Error().Err(err).Msg("failed to save profile")
		triggerToast(w, "error", "Failed to save profile.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	triggerToast(w, "success", "Profile updated successfully.")
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		h.log.Error().Err(err).Msg("failed to load user for password update")
		triggerToast(w, "error", "User not found.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	if !auth.CheckPassword(user.PasswordHash, currentPassword) {
		triggerToast(w, "error", "Current password is incorrect.")
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to hash new password")
		triggerToast(w, "error", "Failed to hash password.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	user.PasswordHash = hash
	if err := h.db.UpdateUser(user); err != nil {
		h.log.Error().Err(err).Msg("failed to save password")
		triggerToast(w, "error", "Failed to save password.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	triggerToast(w, "success", "Password updated successfully.")
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) UpdateWallet(w http.ResponseWriter, r *http.Request) {
	walletCfg := &database.WalletConfig{
		RPCURL:         r.FormValue("rpc_url"),
		RPCUser:        r.FormValue("rpc_user"),
		RPCPassword:    r.FormValue("rpc_password"),
		WalletFile:     r.FormValue("wallet_file"),
		WalletPassword: r.FormValue("wallet_password"),
	}

	if err := h.db.SaveWalletConfig(h.walletEncKey, walletCfg); err != nil {
		h.log.Error().Err(err).Msg("failed to save wallet config")
		triggerToast(w, "error", "Failed to save wallet config.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	newClient := monero.NewClient(walletCfg.RPCURL, walletCfg.RPCUser, walletCfg.RPCPassword)
	if walletCfg.WalletFile != "" {
		if err := newClient.OpenWallet(walletCfg.WalletFile, walletCfg.WalletPassword); err != nil {
			h.log.Warn().Err(err).Msg("failed to open wallet after config update")
		}
	}
	h.wallet.Set(newClient)

	triggerToast(w, "success", "Wallet configuration updated successfully.")
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		h.log.Error().Err(err).Msg("failed to load user for TOTP setup")
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	if user.TOTPSecret != "" {
		walletCfg, _ := h.db.GetWalletConfig(h.walletEncKey)
		status := h.wallet.Status()
		apiKeys, _ := h.db.ListAPIKeys(userID)
		render(r.Context(), w, adminviews.Settings(user, walletCfg, status, apiKeys), h.log)
		return
	}

	secret, qrPNG, err := services.GenerateTOTPSecret(user.Username)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to generate TOTP secret")
		http.Error(w, "Failed to generate TOTP secret", http.StatusInternalServerError)
		return
	}

	if err := h.auth.SetPendingTOTPSecret(w, r, secret); err != nil {
		h.log.Error().Err(err).Msg("failed to set pending TOTP secret session")
	}
	qrDataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG)
	render(r.Context(), w, adminviews.TOTPSetup(secret, qrDataURI, "", h.wallet.Status()), h.log)
}

func (h *AdminHandler) TOTPConfirm(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		h.log.Error().Err(err).Msg("failed to load user for TOTP confirm")
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	secret, ok := h.auth.GetPendingTOTPSecret(r)
	if !ok || secret == "" {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	code := r.FormValue("code")

	if !services.ValidateTOTPCode(secret, code) {
		qrPNG, _ := services.RegenerateTOTPQR(user.Username, secret)
		qrDataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG)
		render(r.Context(), w, adminviews.TOTPSetup(secret, qrDataURI, "Invalid code. Please try again.", h.wallet.Status()), h.log)
		return
	}

	if err := h.auth.ClearPendingTOTPSecret(w, r); err != nil {
		h.log.Error().Err(err).Msg("failed to clear pending TOTP secret session")
	}
	user.TOTPSecret = secret
	if err := h.db.UpdateUser(user); err != nil {
		h.log.Error().Err(err).Msg("failed to save TOTP secret")
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (h *AdminHandler) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	user, err := h.db.GetUser()
	if err != nil || user.ID != userID {
		h.log.Error().Err(err).Msg("failed to load user for TOTP disable")
		triggerToast(w, "error", "User not found.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	code := r.FormValue("code")
	if !services.ValidateTOTPCode(user.TOTPSecret, code) {
		triggerToast(w, "error", "Invalid 2FA code. Could not disable.")
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	user.TOTPSecret = ""
	if err := h.db.UpdateUser(user); err != nil {
		h.log.Error().Err(err).Msg("failed to disable TOTP")
		triggerToast(w, "error", "Failed to save.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	triggerToast(w, "success", "Two-factor authentication has been disabled.")
	w.Header().Set("HX-Redirect", "/admin/settings")
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) WidgetList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	widgets, err := h.db.ListWidgets(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load widgets")
		http.Error(w, "Failed to load widgets", http.StatusInternalServerError)
		return
	}

	render(r.Context(), w, adminviews.Widgets(widgets, h.hostURL, h.wallet.Status()), h.log)
}

func (h *AdminHandler) CreateWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	mode := parseMode(r.FormValue("mode"))

	widget := &database.Widget{
		UserID:          userID,
		Name:            r.FormValue("name"),
		Mode:            mode,
		ButtonText:      r.FormValue("button_text"),
		CustomMessage:   r.FormValue("custom_message"),
		PrimaryColor:    r.FormValue("primary_color"),
		Theme:           parseTheme(r.FormValue("theme")),
		ShowStats:       r.FormValue("show_stats") == "on",
		RedirectURL:     strings.TrimSpace(r.FormValue("redirect_url")),
		ThankYouMessage: "Thank you for your donation!",
		CreatedAt:       time.Now().Unix(),
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
	if widget.PrimaryColor == "" {
		widget.PrimaryColor = "#ff6600"
	} else if !validHexColor.MatchString(widget.PrimaryColor) {
		http.Error(w, "Invalid color format, expected #RRGGBB", http.StatusBadRequest)
		return
	}

	if mode == database.ModePayment {
		presetAmounts := r.FormValue("preset_amounts")
		var amounts []float64
		if err := json.Unmarshal([]byte(presetAmounts), &amounts); err != nil || len(amounts) != 1 || amounts[0] <= 0 {
			http.Error(w, "Payment mode requires exactly one preset amount greater than zero", http.StatusBadRequest)
			return
		}
		widget.PresetAmounts = presetAmounts
	} else {
		presetAmounts := r.FormValue("preset_amounts")
		if presetAmounts != "" {
			var amounts []float64
			if err := json.Unmarshal([]byte(presetAmounts), &amounts); err == nil {
				valid := make([]float64, 0, len(amounts))
				for _, a := range amounts {
					if a > 0 {
						valid = append(valid, a)
					}
				}
				if len(valid) > 0 {
					b, _ := json.Marshal(valid)
					widget.PresetAmounts = string(b)
				}
			}
		}
	}

	if err := h.db.CreateWidget(widget); err != nil {
		h.log.Error().Err(err).Msg("failed to create widget")
		http.Error(w, "Failed to create widget", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/widgets?toast=created", http.StatusSeeOther)
}

func (h *AdminHandler) DeleteWidget(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	widgetID := r.PathValue("id")
	if err := h.db.DeleteWidget(widgetID, userID); err != nil {
		h.log.Error().Err(err).Str("widget_id", widgetID).Msg("failed to delete widget")
		http.Error(w, "Failed to delete widget", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/widgets?toast=deleted", http.StatusSeeOther)
}

func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		triggerToast(w, "error", "API key name is required.")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	expiryDays, _ := strconv.Atoi(r.FormValue("expiry_days"))
	if expiryDays < 1 || expiryDays > 365 {
		expiryDays = 365
	}

	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		h.log.Error().Err(err).Msg("failed to generate random bytes for api key")
		triggerToast(w, "error", "Failed to generate API key.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rawKey := "tip_" + hex.EncodeToString(randomBytes)
	prefix := rawKey[4:12]

	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to hash api key")
		triggerToast(w, "error", "Failed to generate API key.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	now := time.Now()
	key := &database.APIKey{
		UserID:    userID,
		Name:      name,
		KeyHash:   string(hash),
		Prefix:    prefix,
		CreatedAt: now.Unix(),
		ExpiresAt: now.AddDate(0, 0, expiryDays).Unix(),
	}

	if err := h.db.CreateAPIKey(key); err != nil {
		h.log.Error().Err(err).Msg("failed to create api key")
		triggerToast(w, "error", "Failed to create API key.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, `<div class="mt-4 p-4 rounded-xl bg-primary-container/10 border border-tertiary" x-data="{ copied: false }"`+
		` x-init="setTimeout(function(){ $el.scrollIntoView({ behavior: 'smooth', block: 'center' }) }, 100)">`+
		`<div class="flex items-center justify-between mb-2">`+
		`<span class="text-xs font-mono uppercase tracking-widest text-primary-container">New API Key — Copy it now, it won't be shown again</span>`+
		`</div>`+
		`<div class="flex items-center gap-2">`+
		`<code class="flex-1 text-sm font-mono text-on-surface bg-surface-container-lowest px-3 py-2 rounded-lg break-all select-all" x-ref="keyval">%s</code>`+
		`<button type="button" @click="navigator.clipboard.writeText($refs.keyval.textContent.trim()); copied = true; setTimeout(function(){ copied = false }, 2000)"`+
		` class="shrink-0 text-xs font-mono uppercase tracking-widest px-3 py-2 rounded-md transition-colors cursor-pointer"`+
		` :class="copied ? 'bg-primary-container/20 text-primary-container' : 'bg-surface-container-high text-on-surface-variant hover:bg-surface-bright'"`+
		` x-text="copied ? 'Copied!' : 'Copy'"></button>`+
		`</div></div>`, rawKey)

	expiresFormatted := time.Unix(key.ExpiresAt, 0).Format("Jan 2, 2006")
	fmt.Fprintf(w, `<div id="api-key-list" hx-swap-oob="afterbegin">`+
		`<div id="api-key-%s" class="flex items-center justify-between p-3 rounded-lg bg-surface-container-low" x-data="{ confirmDelete: false }">`+
		`<div class="flex items-center gap-4 min-w-0"><div class="min-w-0">`+
		`<p class="text-sm font-bold text-on-surface truncate">%s</p>`+
		`<div class="flex items-center gap-3 text-xs text-on-surface-variant font-mono">`+
		`<span>tip_%s...</span><span>&middot;</span><span>Expires %s</span>`+
		`</div></div></div>`+
		`<div class="flex items-center gap-1 shrink-0">`+
		`<template x-if="!confirmDelete">`+
		`<button type="button" @click="confirmDelete = true" class="flex items-center gap-1 text-error text-xs font-mono uppercase tracking-widest leading-none hover:bg-error-container/20 px-2 py-1 rounded-md transition-colors cursor-pointer">`+
		`<span class="material-symbols-outlined text-sm">delete</span>Delete</button></template>`+
		`<template x-if="confirmDelete"><div class="flex items-center gap-1">`+
		`<span class="text-on-surface-variant text-xs font-mono leading-none">Sure?</span>`+
		`<button type="button" @click="confirmDelete = false" class="text-on-surface-variant text-xs font-mono uppercase tracking-widest leading-none hover:bg-surface-container-high px-2 py-1 rounded-md transition-colors cursor-pointer">Cancel</button>`+
		`<form class="flex" method="POST" action="/admin/settings/api-keys/%s/delete">`+
		`<button type="submit" class="text-error text-xs font-mono uppercase tracking-widest leading-none hover:bg-error-container/20 px-2 py-1 rounded-md transition-colors font-bold cursor-pointer">Delete</button>`+
		`</form></div></template></div></div></div>`,
		key.ID, name, prefix, expiresFormatted, key.ID)

	fmt.Fprint(w, `<p id="api-key-empty" hx-swap-oob="outerHTML"></p>`)
}

func (h *AdminHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	keyID := r.PathValue("id")

	if err := h.db.DeleteAPIKey(keyID, userID); err != nil {
		h.log.Error().Err(err).Msg("failed to delete api key")
		http.Error(w, "Failed to delete API key", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func formatOptionalTime(unix int64) string {
	if unix == 0 {
		return ""
	}
	return time.Unix(unix, 0).Format(time.RFC3339)
}

func parseTheme(val string) database.WidgetTheme {
	switch database.WidgetTheme(val) {
	case database.ThemeLight:
		return database.ThemeLight
	case database.ThemeDark:
		return database.ThemeDark
	default:
		return database.ThemeSystem
	}
}

func parseMode(val string) database.WidgetMode {
	switch database.WidgetMode(val) {
	case database.ModePayment:
		return database.ModePayment
	default:
		return database.ModeDonation
	}
}
