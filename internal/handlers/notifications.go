package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/crypto"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/services"
	adminviews "github.com/tiponero/tiponero-core/internal/views/admin"
)

var channelTypes = []string{"telegram", "discord", "slack", "ntfy", "whatsapp", "email", "generic"}

func (h *AdminHandler) NotificationsPage(w http.ResponseWriter, r *http.Request) {
	h.renderNotifications(w, r, "", "")
}

func (h *AdminHandler) renderNotifications(w http.ResponseWriter, r *http.Request, newSecret, toast string) {
	variant := "success"
	if strings.HasPrefix(toast, "Error: ") {
		variant = "error"
		toast = strings.TrimPrefix(toast, "Error: ")
	}

	userID := auth.UserIDFromContext(r.Context())

	webhooks, err := h.db.ListWebhooks(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load webhooks")
		http.Error(w, "Failed to load webhooks", http.StatusInternalServerError)
		return
	}

	channels, err := h.db.ListChannels(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load channels")
		http.Error(w, "Failed to load channels", http.StatusInternalServerError)
		return
	}

	deliveries, err := h.db.ListDeliveries(userID, 25)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to load deliveries")
		deliveries = nil
	}

	pending, failed, err := h.db.DeliveryCounts(userID)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to count deliveries")
	}

	configs := make(map[string]services.ChannelConfig, len(channels))
	for _, c := range channels {
		raw, err := crypto.Decrypt(h.notifyEncKey, c.ConfigEnc)
		if err != nil {
			continue
		}
		var cfg services.ChannelConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			continue
		}
		// Secrets stay out of the HTML. The edit form shows a placeholder.
		cfg.BotToken = ""
		cfg.AuthToken = ""
		cfg.SMTPPass = ""
		configs[c.ID] = cfg
	}

	render(r.Context(), w, adminviews.Notifications(adminviews.NotificationsData{
		Webhooks:       webhooks,
		Channels:       channels,
		ChannelConfigs: configs,
		Deliveries:     deliveries,
		Pending:        pending,
		Failed:         failed,
		Events:         database.AllEvents,
		ChannelTypes:   channelTypes,
		NewSecret:      newSecret,
		Toast:          toast,
		ToastVariant:   variant,
	}), h.log)
}

func (h *AdminHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	name := strings.TrimSpace(r.FormValue("name"))
	target := strings.TrimSpace(r.FormValue("url"))
	events := r.Form["events"]

	if name == "" {
		h.renderNotifications(w, r, "", "Error: Give the webhook a name.")
		return
	}
	if err := validateEndpointURL(target); err != nil {
		h.renderNotifications(w, r, "", "Error: The webhook URL is not valid: "+err.Error())
		return
	}
	if len(events) == 0 {
		h.renderNotifications(w, r, "", "Error: Select at least one event.")
		return
	}

	secret := randomToken()
	secretEnc, err := crypto.Encrypt(h.notifyEncKey, secret)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encrypt webhook secret")
		http.Error(w, "Failed to create webhook", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	hook := &database.Webhook{
		UserID:    userID,
		Name:      name,
		URL:       target,
		SecretEnc: secretEnc,
		Events:    database.JoinEvents(events),
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.db.CreateWebhook(hook); err != nil {
		h.log.Error().Err(err).Msg("failed to create webhook")
		http.Error(w, "Failed to create webhook", http.StatusInternalServerError)
		return
	}

	h.renderNotifications(w, r, secret, "Webhook created. Copy the signing secret now. It is not shown again.")
}

func (h *AdminHandler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	hook, err := h.db.GetWebhook(id, userID)
	if err != nil {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	target := strings.TrimSpace(r.FormValue("url"))
	events := r.Form["events"]

	if name == "" || len(events) == 0 {
		h.renderNotifications(w, r, "", "Error: A webhook needs a name and at least one event.")
		return
	}
	if err := validateEndpointURL(target); err != nil {
		h.renderNotifications(w, r, "", "Error: The webhook URL is not valid: "+err.Error())
		return
	}

	hook.Name = name
	hook.URL = target
	hook.Events = database.JoinEvents(events)
	hook.Active = r.FormValue("active") == "on"

	if err := h.db.UpdateWebhook(hook); err != nil {
		h.log.Error().Err(err).Msg("failed to update webhook")
		http.Error(w, "Failed to update webhook", http.StatusInternalServerError)
		return
	}

	h.renderNotifications(w, r, "", "Webhook updated.")
}

func (h *AdminHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if err := h.db.DeleteWebhook(r.PathValue("id"), userID); err != nil {
		h.log.Error().Err(err).Msg("failed to delete webhook")
		http.Error(w, "Failed to delete webhook", http.StatusInternalServerError)
		return
	}
	h.renderNotifications(w, r, "", "Webhook deleted.")
}

func (h *AdminHandler) RotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	hook, err := h.db.GetWebhook(id, userID)
	if err != nil {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	secret := randomToken()
	secretEnc, err := crypto.Encrypt(h.notifyEncKey, secret)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encrypt webhook secret")
		http.Error(w, "Failed to rotate secret", http.StatusInternalServerError)
		return
	}
	if err := h.db.UpdateWebhookSecret(hook.ID, userID, secretEnc); err != nil {
		h.log.Error().Err(err).Msg("failed to rotate webhook secret")
		http.Error(w, "Failed to rotate secret", http.StatusInternalServerError)
		return
	}

	h.renderNotifications(w, r, secret, "New signing secret generated. Copy it now. The old secret stops working immediately.")
}

func (h *AdminHandler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	hook, err := h.db.GetWebhook(id, userID)
	if err != nil {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	h.enqueueTest(userID, "webhook", hook.ID)
	h.renderNotifications(w, r, "", "Test delivery queued. Check the delivery log in a few seconds.")
}

func (h *AdminHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	kind := strings.TrimSpace(r.FormValue("type"))
	name := strings.TrimSpace(r.FormValue("name"))
	events := r.Form["events"]

	if !validChannelType(kind) {
		h.renderNotifications(w, r, "", "Error: Choose a supported channel type.")
		return
	}
	if name == "" {
		name = kind
	}
	if len(events) == 0 {
		h.renderNotifications(w, r, "", "Error: Select at least one event.")
		return
	}

	cfg := channelConfigFromForm(r, services.ChannelConfig{})
	if err := validateChannelConfig(kind, cfg); err != nil {
		h.renderNotifications(w, r, "", "Error: Channel configuration is incomplete: "+err.Error())
		return
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "Failed to create channel", http.StatusInternalServerError)
		return
	}
	configEnc, err := crypto.Encrypt(h.notifyEncKey, string(raw))
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encrypt channel config")
		http.Error(w, "Failed to create channel", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	channel := &database.NotificationChannel{
		UserID:    userID,
		Type:      kind,
		Name:      name,
		ConfigEnc: configEnc,
		Events:    database.JoinEvents(events),
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.db.CreateChannel(channel); err != nil {
		h.log.Error().Err(err).Msg("failed to create channel")
		http.Error(w, "Failed to create channel", http.StatusInternalServerError)
		return
	}

	h.renderNotifications(w, r, "", "Channel added.")
}

func (h *AdminHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	channel, err := h.db.GetChannel(id, userID)
	if err != nil {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	kind := strings.TrimSpace(r.FormValue("type"))
	if !validChannelType(kind) {
		kind = channel.Type
	}

	existing := services.ChannelConfig{}
	if raw, err := crypto.Decrypt(h.notifyEncKey, channel.ConfigEnc); err == nil {
		_ = json.Unmarshal([]byte(raw), &existing)
	}

	cfg := channelConfigFromForm(r, existing)
	if err := validateChannelConfig(kind, cfg); err != nil {
		h.renderNotifications(w, r, "", "Error: Channel configuration is incomplete: "+err.Error())
		return
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "Failed to update channel", http.StatusInternalServerError)
		return
	}
	configEnc, err := crypto.Encrypt(h.notifyEncKey, string(raw))
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encrypt channel config")
		http.Error(w, "Failed to update channel", http.StatusInternalServerError)
		return
	}

	channel.Type = kind
	channel.Name = strings.TrimSpace(r.FormValue("name"))
	if channel.Name == "" {
		channel.Name = kind
	}
	channel.ConfigEnc = configEnc
	channel.Events = database.JoinEvents(r.Form["events"])
	channel.Active = r.FormValue("active") == "on"

	if err := h.db.UpdateChannel(channel); err != nil {
		h.log.Error().Err(err).Msg("failed to update channel")
		http.Error(w, "Failed to update channel", http.StatusInternalServerError)
		return
	}

	h.renderNotifications(w, r, "", "Channel updated.")
}

func (h *AdminHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if err := h.db.DeleteChannel(r.PathValue("id"), userID); err != nil {
		h.log.Error().Err(err).Msg("failed to delete channel")
		http.Error(w, "Failed to delete channel", http.StatusInternalServerError)
		return
	}
	h.renderNotifications(w, r, "", "Channel deleted.")
}

func (h *AdminHandler) TestChannel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	id := r.PathValue("id")

	channel, err := h.db.GetChannel(id, userID)
	if err != nil {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	h.enqueueTest(userID, "channel", channel.ID)
	h.renderNotifications(w, r, "", "Test message queued. Check the delivery log in a few seconds.")
}

// enqueueTest puts a synthetic event on the outbox so the operator can verify
// an endpoint without waiting for a real payment.
func (h *AdminHandler) enqueueTest(userID, kind, targetID string) {
	now := time.Now().Unix()
	tx := &database.Transaction{
		ID:          "test-" + randomToken()[:8],
		UserID:      userID,
		Status:      database.StatusConfirmed,
		Amount:      50000000000,
		IsPayment:   true,
		Description: "Test event from the Tiponero admin panel",
		CreatedAt:   now,
		ExpiresAt:   now + 3600,
	}
	event := services.BuildEvent(tx, database.EventPaymentConfirmed, h.requiredConfirmations, h.hostURL)
	event.Type = "webhook.test"

	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	delivery := &database.NotificationDelivery{
		UserID:      userID,
		Kind:        kind,
		TargetID:    targetID,
		Event:       event.Type,
		EventID:     event.ID,
		Payload:     string(payload),
		Status:      database.DeliveryPending,
		NextRetryAt: now,
		CreatedAt:   now,
	}
	if err := h.db.EnqueueDelivery(delivery); err != nil {
		h.log.Error().Err(err).Msg("failed to enqueue test delivery")
	}
}

func validateEndpointURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("cannot parse the URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("the host is missing")
	}
	return nil
}

func validChannelType(kind string) bool {
	for _, t := range channelTypes {
		if t == kind {
			return true
		}
	}
	return false
}

// channelConfigFromForm reads every channel field. Empty secret fields keep
// the previous value so the operator does not have to paste a token again.
func channelConfigFromForm(r *http.Request, existing services.ChannelConfig) services.ChannelConfig {
	cfg := existing

	setString := func(target *string, field string) {
		if v := strings.TrimSpace(r.FormValue(field)); v != "" {
			*target = v
		}
	}
	setSecret := func(target *string, field string) {
		if v := strings.TrimSpace(r.FormValue(field)); v != "" {
			*target = v
		}
	}

	switch strings.TrimSpace(r.FormValue("type")) {
	case "telegram":
		setSecret(&cfg.BotToken, "bot_token")
		setString(&cfg.ChatID, "chat_id")
	case "discord":
		setString(&cfg.URL, "discord_url")
		setString(&cfg.URL, "url")
	case "slack":
		setString(&cfg.URL, "slack_url")
		setString(&cfg.URL, "url")
	case "generic":
		setString(&cfg.URL, "generic_url")
		setString(&cfg.URL, "url")
	case "ntfy":
		setString(&cfg.URL, "ntfy_url")
		setString(&cfg.URL, "url")
		setString(&cfg.Topic, "ntfy_topic")
		setString(&cfg.Topic, "topic")
	case "whatsapp":
		setString(&cfg.AccountSID, "account_sid")
		setSecret(&cfg.AuthToken, "auth_token")
		setString(&cfg.WhatsAppFrom, "whatsapp_from")
		setString(&cfg.WhatsAppTo, "whatsapp_to")
	case "email":
		setString(&cfg.SMTPHost, "smtp_host")
		setString(&cfg.SMTPUser, "smtp_user")
		setSecret(&cfg.SMTPPass, "smtp_pass")
		setString(&cfg.From, "from")
		setString(&cfg.To, "to")
		if v := strings.TrimSpace(r.FormValue("smtp_port")); v != "" {
			if port, err := strconv.Atoi(v); err == nil {
				cfg.SMTPPort = port
			}
		}
	}

	return cfg
}

func validateChannelConfig(kind string, cfg services.ChannelConfig) error {
	switch kind {
	case "telegram":
		if cfg.BotToken == "" {
			return fmt.Errorf("bot token is required")
		}
		if cfg.ChatID == "" {
			return fmt.Errorf("chat id is required")
		}
	case "discord", "slack", "generic":
		if cfg.URL == "" {
			return fmt.Errorf("webhook URL is required")
		}
		return validateEndpointURL(cfg.URL)
	case "ntfy":
		if cfg.Topic == "" {
			return fmt.Errorf("topic is required")
		}
	case "whatsapp":
		if cfg.AccountSID == "" || cfg.AuthToken == "" || cfg.WhatsAppFrom == "" || cfg.WhatsAppTo == "" {
			return fmt.Errorf("account sid, auth token, sender, and recipient are required")
		}
	case "email":
		if cfg.SMTPHost == "" || cfg.From == "" || cfg.To == "" {
			return fmt.Errorf("SMTP host, from address, and to address are required")
		}
	}
	return nil
}
