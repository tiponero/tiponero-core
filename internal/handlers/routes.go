package handlers

import (
	"context"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
	"github.com/tiponero/tiponero-core/internal/services"
	"github.com/tiponero/tiponero-core/static"
)

// Deps holds the shared dependencies of every HTTP handler. One struct keeps
// the router readable as the feature set grows.
type Deps struct {
	DB                    *database.DB
	Auth                  *auth.Service
	Wallet                *monero.ClientHolder
	Price                 PriceConverter
	Dispatcher            *services.Dispatcher
	HostURL               string
	WalletEncKey          []byte
	NotifyEncKey          []byte
	RequiredConfirmations int
	TransactionExpiry     time.Duration
	MockPayments          bool
	TrustProxy            bool
	Log                   zerolog.Logger
}

func NewRouter(d Deps) *chi.Mux {
	admin := NewAdminHandler(d)
	widget := NewWidgetHandler(d)
	api := NewAPIHandler(d)
	apiAuth := auth.NewAPIKeyAuthenticator(d.DB)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(csrfCookie(strings.HasPrefix(d.HostURL, "https://")))
	r.Use(rateLimit(d.TrustProxy, d.Log))

	staticFS, _ := fs.Sub(static.Assets, ".")
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	})

	r.Route("/admin", func(r chi.Router) {
		r.Use(csrfProtect)
		r.Get("/login", admin.LoginPage)
		r.Post("/login", admin.Login)
		r.Post("/login/totp", admin.VerifyTOTP)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireAuth)
			r.Post("/logout", admin.Logout)
			r.Get("/", admin.Dashboard)
			r.Get("/transactions", admin.Transactions)
			r.Get("/transactions/export", admin.ExportCSV)
			r.Get("/transactions/{id}", admin.TransactionDetail)
			r.Post("/transactions/{id}/mock-pay", admin.MockPay)
			r.Get("/settings", admin.SettingsPage)
			r.Post("/settings/profile", admin.UpdateProfile)
			r.Post("/settings/password", admin.UpdatePassword)
			r.Post("/settings/wallet", admin.UpdateWallet)
			r.Post("/settings/totp/setup", admin.TOTPSetup)
			r.Post("/settings/totp/confirm", admin.TOTPConfirm)
			r.Post("/settings/totp/disable", admin.TOTPDisable)
			r.Get("/widgets", admin.WidgetList)
			r.Post("/widgets", admin.CreateWidget)
			r.Post("/widgets/{id}/update", admin.UpdateWidget)
			r.Post("/widgets/{id}/delete", admin.DeleteWidget)
			r.Post("/settings/api-keys", admin.CreateAPIKey)
			r.Post("/settings/api-keys/{id}/delete", admin.DeleteAPIKey)
			r.Get("/notifications", admin.NotificationsPage)
			r.Post("/notifications/webhooks", admin.CreateWebhook)
			r.Post("/notifications/webhooks/{id}/update", admin.UpdateWebhook)
			r.Post("/notifications/webhooks/{id}/delete", admin.DeleteWebhook)
			r.Post("/notifications/webhooks/{id}/rotate", admin.RotateWebhookSecret)
			r.Post("/notifications/webhooks/{id}/test", admin.TestWebhook)
			r.Post("/notifications/channels", admin.CreateChannel)
			r.Post("/notifications/channels/{id}/update", admin.UpdateChannel)
			r.Post("/notifications/channels/{id}/delete", admin.DeleteChannel)
			r.Post("/notifications/channels/{id}/test", admin.TestChannel)
		})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(apiAuth.RequireAPIKey)
		r.Get("/widgets", api.ListWidgets)
		r.Post("/widgets", api.CreateWidget)
		r.Get("/widgets/{id}", api.GetWidget)
		r.Patch("/widgets/{id}", api.UpdateWidget)
		r.Delete("/widgets/{id}", api.DeleteWidget)
		r.Get("/transactions", api.ListTransactions)
		r.Post("/transactions", api.CreateTransaction)
		r.Get("/transactions/{id}", api.GetTransaction)
		r.Get("/user", api.GetUser)
		r.Patch("/user", api.UpdateUser)
		r.Get("/wallet", api.GetWallet)
		r.Post("/wallet", api.CreateWallet)
		r.Patch("/wallet", api.UpdateWallet)
		r.Get("/stats", api.GetStats)
		r.Get("/keys", api.ListAPIKeys)
		r.Delete("/keys/{id}", api.DeleteAPIKey)
		r.Get("/webhooks", api.ListWebhooks)
		r.Post("/webhooks", api.CreateWebhook)
		r.Delete("/webhooks/{id}", api.DeleteWebhook)
	})

	r.Route("/widget", func(r chi.Router) {
		r.Get("/{widgetID}", widget.Home)
		r.Get("/{widgetID}/badge.svg", widget.Badge)
		r.Post("/{widgetID}/donate", widget.CreateTransaction)
		r.Get("/{widgetID}/donation/{donationID}/status", widget.TransactionStatus)
		r.Get("/qr/{donationID}", widget.QRCode)
	})

	// Standalone payment pages serve transactions that have no widget.
	r.Route("/pay", func(r chi.Router) {
		r.Get("/{transactionID}", widget.StandalonePayment)
		r.Get("/{transactionID}/status", widget.StandaloneStatus)
		r.Get("/{transactionID}/qr", widget.StandaloneQRCode)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})

	return r
}

func render(ctx context.Context, w io.Writer, c templ.Component, log zerolog.Logger) {
	if err := c.Render(ctx, w); err != nil {
		log.Error().Err(err).Msg("failed to render template")
	}
}
