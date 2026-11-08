package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/crypto"
	"github.com/tiponero/tiponero-core/internal/database"
)

const (
	deliveryPoll   = 5 * time.Second
	deliveryBatch  = 20
	maxAttempts    = 6
	httpTimeout    = 15 * time.Second
	smtpTimeout    = 20 * time.Second
	deliveryLogAge = 7 * 24 * time.Hour
)

// retrySchedule holds the delay before each retry. The first entry is the
// delay after the first failed attempt.
var retrySchedule = []time.Duration{
	10 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// ChannelConfig is the decrypted configuration of one notification channel.
// One struct covers every provider; unused fields stay empty.
type ChannelConfig struct {
	// Telegram
	BotToken string `json:"bot_token,omitempty"`
	ChatID   string `json:"chat_id,omitempty"`

	// Discord, Slack, ntfy, generic
	URL   string `json:"url,omitempty"`
	Topic string `json:"topic,omitempty"`

	// Email (SMTP, including SendGrid)
	SMTPHost string `json:"smtp_host,omitempty"`
	SMTPPort int    `json:"smtp_port,omitempty"`
	SMTPUser string `json:"smtp_user,omitempty"`
	SMTPPass string `json:"smtp_pass,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`

	// WhatsApp through Twilio
	AccountSID   string `json:"account_sid,omitempty"`
	AuthToken    string `json:"auth_token,omitempty"`
	WhatsAppFrom string `json:"whatsapp_from,omitempty"`
	WhatsAppTo   string `json:"whatsapp_to,omitempty"`
}

// DeliveryWorker drains the notification outbox. It owns every network call,
// so the monitor and the HTTP handlers never block on a slow endpoint.
type DeliveryWorker struct {
	db     *database.DB
	encKey []byte
	client *http.Client
	log    zerolog.Logger
}

func NewDeliveryWorker(db *database.DB, encKey []byte, log zerolog.Logger) *DeliveryWorker {
	return &DeliveryWorker{
		db:     db,
		encKey: encKey,
		client: &http.Client{Timeout: httpTimeout},
		log:    log.With().Str("component", "delivery").Logger(),
	}
}

func (w *DeliveryWorker) Run(ctx context.Context) {
	w.log.Info().Msg("delivery worker started")
	ticker := time.NewTicker(deliveryPoll)
	defer ticker.Stop()

	prune := time.NewTicker(time.Hour)
	defer prune.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("delivery worker stopped")
			return
		case <-ticker.C:
			w.drain()
		case <-prune.C:
			if err := w.db.PruneDeliveries(time.Now().Add(-deliveryLogAge).Unix()); err != nil {
				w.log.Error().Err(err).Msg("failed to prune delivery log")
			}
		}
	}
}

func (w *DeliveryWorker) drain() {
	due, err := w.db.DueDeliveries(time.Now().Unix(), deliveryBatch)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to load due deliveries")
		return
	}

	for i := range due {
		w.process(due[i])
	}
}

func (w *DeliveryWorker) process(d database.NotificationDelivery) {
	code, err := w.deliver(d)
	if err == nil {
		if markErr := w.db.MarkDeliveryDelivered(d.ID, code); markErr != nil {
			w.log.Error().Err(markErr).Str("delivery_id", d.ID).Msg("failed to mark delivery delivered")
		}
		return
	}

	attempt := d.Attempts + 1
	if attempt >= maxAttempts {
		if markErr := w.db.MarkDeliveryFailed(d.ID, code, err.Error()); markErr != nil {
			w.log.Error().Err(markErr).Str("delivery_id", d.ID).Msg("failed to mark delivery failed")
		}
		w.log.Warn().Err(err).Str("delivery_id", d.ID).Str("event", d.Event).Msg("delivery failed permanently")
		return
	}

	delay := retrySchedule[attempt-1]
	next := time.Now().Add(delay).Unix()
	if markErr := w.db.MarkDeliveryRetry(d.ID, code, err.Error(), next); markErr != nil {
		w.log.Error().Err(markErr).Str("delivery_id", d.ID).Msg("failed to schedule delivery retry")
	}
	w.log.Warn().Err(err).Str("delivery_id", d.ID).Int("attempt", attempt).Time("retry_at", time.Unix(next, 0)).Msg("delivery attempt failed")
}

func (w *DeliveryWorker) deliver(d database.NotificationDelivery) (int, error) {
	var event Event
	if err := json.Unmarshal([]byte(d.Payload), &event); err != nil {
		return 0, fmt.Errorf("decode payload: %w", err)
	}

	switch d.Kind {
	case "webhook":
		hook, err := w.db.GetWebhook(d.TargetID, d.UserID)
		if err != nil {
			return 0, fmt.Errorf("load webhook: %w", err)
		}
		secret, err := crypto.Decrypt(w.encKey, hook.SecretEnc)
		if err != nil {
			return 0, fmt.Errorf("decrypt webhook secret: %w", err)
		}
		return w.sendWebhook(hook.URL, secret, event, d.Payload, d.ID)

	case "channel":
		channel, err := w.db.GetChannel(d.TargetID, d.UserID)
		if err != nil {
			return 0, fmt.Errorf("load channel: %w", err)
		}
		raw, err := crypto.Decrypt(w.encKey, channel.ConfigEnc)
		if err != nil {
			return 0, fmt.Errorf("decrypt channel config: %w", err)
		}
		var cfg ChannelConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return 0, fmt.Errorf("decode channel config: %w", err)
		}
		return w.sendChannel(channel.Type, cfg, event)

	default:
		return 0, fmt.Errorf("unknown delivery kind %q", d.Kind)
	}
}

// signPayload returns the value of X-Tiponero-Signature. A receiver rebuilds
// it from the timestamp header and the raw body with its own secret copy.
func signPayload(secret, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sendWebhook signs the body with HMAC-SHA256 over "<timestamp>.<body>".
// A receiver rebuilds the signature with its own copy of the secret.
func (w *DeliveryWorker) sendWebhook(targetURL, secret string, event Event, payload, deliveryID string) (int, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := signPayload(secret, timestamp, []byte(payload))

	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader([]byte(payload)))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Tiponero-Webhook/1")
	req.Header.Set("X-Tiponero-Event", event.Type)
	req.Header.Set("X-Tiponero-Delivery", deliveryID)
	req.Header.Set("X-Tiponero-Timestamp", timestamp)
	req.Header.Set("X-Tiponero-Signature", signature)

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (w *DeliveryWorker) sendChannel(kind string, cfg ChannelConfig, event Event) (int, error) {
	message := event.Message()

	switch kind {
	case "telegram":
		if cfg.BotToken == "" || cfg.ChatID == "" {
			return 0, fmt.Errorf("telegram bot token and chat id are required")
		}
		endpoint := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
		return w.postJSON(endpoint, "", map[string]any{
			"chat_id":                  cfg.ChatID,
			"text":                     message,
			"disable_web_page_preview": true,
		})

	case "discord":
		if cfg.URL == "" {
			return 0, fmt.Errorf("discord webhook url is required")
		}
		return w.postJSON(cfg.URL, "", map[string]any{"content": message})

	case "slack":
		if cfg.URL == "" {
			return 0, fmt.Errorf("slack webhook url is required")
		}
		return w.postJSON(cfg.URL, "", map[string]any{"text": message})

	case "ntfy":
		return w.sendNtfy(cfg, event, message)

	case "generic":
		if cfg.URL == "" {
			return 0, fmt.Errorf("endpoint url is required")
		}
		return w.postRaw(cfg.URL, "", []byte(event.Message()), "text/plain")

	case "email":
		return sendEmail(cfg, "[Tiponero] "+event.Type, message)

	case "whatsapp":
		return sendWhatsApp(cfg, message)

	default:
		return 0, fmt.Errorf("unknown channel type %q", kind)
	}
}

func (w *DeliveryWorker) postJSON(endpoint, auth string, body any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("encode body: %w", err)
	}
	return w.postRaw(endpoint, auth, payload, "application/json")
}

func (w *DeliveryWorker) postRaw(endpoint, auth string, payload []byte, contentType string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.StatusCode, nil
}

func (w *DeliveryWorker) sendNtfy(cfg ChannelConfig, event Event, message string) (int, error) {
	base := cfg.URL
	if base == "" {
		base = "https://ntfy.sh"
	}
	if cfg.Topic == "" {
		return 0, fmt.Errorf("ntfy topic is required")
	}
	endpoint := strings.TrimRight(base, "/") + "/" + url.PathEscape(cfg.Topic)

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(message))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Title", "Tiponero: "+event.Type)
	req.Header.Set("Tags", "money_with_wings")
	req.Header.Set("Content-Type", "text/plain")

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post ntfy: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func sendWhatsApp(cfg ChannelConfig, message string) (int, error) {
	if cfg.AccountSID == "" || cfg.AuthToken == "" {
		return 0, fmt.Errorf("twilio account sid and auth token are required")
	}
	if cfg.WhatsAppFrom == "" || cfg.WhatsAppTo == "" {
		return 0, fmt.Errorf("whatsapp sender and recipient are required")
	}

	form := url.Values{}
	form.Set("From", cfg.WhatsAppFrom)
	form.Set("To", cfg.WhatsAppTo)
	form.Set("Body", message)

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", url.PathEscape(cfg.AccountSID))
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(cfg.AccountSID, cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post twilio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("twilio returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.StatusCode, nil
}

// sendEmail uses SMTP so any provider works: SendGrid, Mailgun, Postmark, or
// a local relay. Port 465 uses implicit TLS, every other port tries STARTTLS.
func sendEmail(cfg ChannelConfig, subject, body string) (int, error) {
	if cfg.SMTPHost == "" || cfg.From == "" || cfg.To == "" {
		return 0, fmt.Errorf("smtp host, from address, and to address are required")
	}

	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}

	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(port))
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}

	var client *smtp.Client
	if port == 465 {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: smtpTimeout}, "tcp", addr, tlsConfig)
		if err != nil {
			return 0, fmt.Errorf("tls dial: %w", err)
		}
		client, err = smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return 0, fmt.Errorf("smtp client: %w", err)
		}
	} else {
		conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
		if err != nil {
			return 0, fmt.Errorf("dial: %w", err)
		}
		client, err = smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return 0, fmt.Errorf("smtp client: %w", err)
		}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return 0, fmt.Errorf("starttls: %w", err)
			}
		}
	}
	defer func() { _ = client.Close() }()

	if cfg.SMTPUser != "" {
		auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return 0, fmt.Errorf("smtp auth: %w", err)
		}
	}

	from := sanitizeHeader(cfg.From)
	to := sanitizeHeader(cfg.To)
	subject = sanitizeHeader(subject)

	if err := client.Mail(from); err != nil {
		return 0, fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return 0, fmt.Errorf("rcpt to: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return 0, fmt.Errorf("data: %w", err)
	}

	msg := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" + body + "\r\n"

	if _, err := writer.Write([]byte(msg)); err != nil {
		return 0, fmt.Errorf("write body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("close body: %w", err)
	}
	_ = client.Quit()

	return 250, nil
}

// sanitizeHeader removes CR and LF so a config value cannot inject a header.
func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(value))
}
