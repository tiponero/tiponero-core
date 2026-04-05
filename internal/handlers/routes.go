package handlers

import (
	"context"
	"io"
	"io/fs"
	"net/http"
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

func NewRouter(db *database.DB, authSvc *auth.Service, wallet *monero.ClientHolder, price *services.PriceService, hostURL string, walletEncKey []byte, requiredConfirmations int, transactionExpiry time.Duration, log zerolog.Logger) *chi.Mux {
	admin := NewAdminHandler(db, authSvc, wallet, walletEncKey, hostURL, requiredConfirmations, log)
	widget := NewWidgetHandler(db, wallet, price, hostURL, transactionExpiry, log)
	api := NewAPIHandler(db, wallet, walletEncKey, log)
	apiAuth := auth.NewAPIKeyAuthenticator(db)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	staticFS, _ := fs.Sub(static.Assets, ".")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	})

	r.Route("/admin", func(r chi.Router) {
		r.Get("/login", admin.LoginPage)
		r.Post("/login", admin.Login)
		r.Post("/login/totp", admin.VerifyTOTP)

		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireAuth)
			r.Post("/logout", admin.Logout)
			r.Get("/", admin.Dashboard)
			r.Get("/transactions", admin.Transactions)
			r.Get("/transactions/export", admin.ExportCSV)
			r.Get("/transactions/{id}", admin.TransactionDetail)
			r.Get("/settings", admin.SettingsPage)
			r.Post("/settings/profile", admin.UpdateProfile)
			r.Post("/settings/password", admin.UpdatePassword)
			r.Post("/settings/wallet", admin.UpdateWallet)
			r.Post("/settings/totp/setup", admin.TOTPSetup)
			r.Post("/settings/totp/confirm", admin.TOTPConfirm)
			r.Post("/settings/totp/disable", admin.TOTPDisable)
			r.Get("/widgets", admin.WidgetList)
			r.Post("/widgets", admin.CreateWidget)
			r.Post("/widgets/{id}/delete", admin.DeleteWidget)
			r.Post("/settings/api-keys", admin.CreateAPIKey)
			r.Post("/settings/api-keys/{id}/delete", admin.DeleteAPIKey)
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
		r.Get("/transactions/{id}", api.GetTransaction)
		r.Get("/user", api.GetUser)
		r.Patch("/user", api.UpdateUser)
		r.Get("/wallet", api.GetWallet)
		r.Post("/wallet", api.CreateWallet)
		r.Patch("/wallet", api.UpdateWallet)
		r.Get("/stats", api.GetStats)
		r.Get("/keys", api.ListAPIKeys)
		r.Delete("/keys/{id}", api.DeleteAPIKey)
	})

	r.Route("/widget", func(r chi.Router) {
		r.Get("/{widgetID}", widget.Home)
		r.Get("/{widgetID}/badge.svg", widget.Badge)
		r.Post("/{widgetID}/donate", widget.CreateTransaction)
		r.Get("/{widgetID}/donation/{donationID}/status", widget.TransactionStatus)
		r.Get("/qr/{donationID}", widget.QRCode)
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
