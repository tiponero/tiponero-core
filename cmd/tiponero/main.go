package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tiponero/tiponero-core/internal/auth"
	"github.com/tiponero/tiponero-core/internal/config"
	"github.com/tiponero/tiponero-core/internal/crypto"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/handlers"
	"github.com/tiponero/tiponero-core/internal/monero"
	"github.com/tiponero/tiponero-core/internal/services"
)

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.Kitchen}).
		With().Timestamp().Logger()

	if err := godotenv.Load(); err != nil {
		log.Debug().Err(err).Msg(".env file not loaded (this is normal if using environment variables directly)")
	}
	cfg := config.Load()

	if cfg.EncryptionKey == "change-me-in-production-32-chars" || len(cfg.EncryptionKey) < 32 {
		log.Warn().Msg("ENCRYPTION_KEY is weak or default - set a random 32+ character value in production")
	}

	sessionKey, err := crypto.DeriveKey([]byte(cfg.EncryptionKey), "session")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to derive session key")
	}
	walletEncKey, err := crypto.DeriveKey([]byte(cfg.EncryptionKey), "wallet")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to derive wallet encryption key")
	}

	db, err := database.NewDB(cfg.DatabasePath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
	}
	defer db.Close()
	log.Info().Msg("database initialized")

	if err := seedUser(db); err != nil {
		log.Fatal().Err(err).Msg("failed to seed admin user")
	}

	user, err := db.GetUser()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load user")
	}

	walletCfg, err := db.GetWalletConfig(walletEncKey, user.ID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load wallet config")
	}

	client := monero.NewClient(walletCfg.RPCURL, walletCfg.RPCUser, walletCfg.RPCPassword)
	if walletCfg.WalletFile != "" {
		if err := client.OpenWallet(walletCfg.WalletFile, walletCfg.WalletPassword); err != nil {
			log.Warn().Err(err).Str("file", walletCfg.WalletFile).Msg("failed to open wallet (will retry when RPC becomes available)")
		}
	}
	wallet := monero.NewClientHolder(client)

	authSvc := auth.NewService(hex.EncodeToString(sessionKey), cfg.SecureCookies)
	priceSvc := services.NewPriceService(cfg.FiatCurrency)

	router := handlers.NewRouter(db, authSvc, wallet, priceSvc, cfg.BaseURL, walletEncKey, cfg.RequiredConfirmations, cfg.TransactionExpiry, log.Logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	monitor := services.NewPaymentMonitor(db, wallet, cfg.RequiredConfirmations, log.Logger)
	go monitor.Run(ctx)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Info().Str("addr", server.Addr).Msg("tiponero started")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}
}

func seedUser(db *database.DB) error {
	defaultAdminUsername := "admin"
	defaultAdminPassword := "admin"
	if _, err := db.GetUser(); err == nil {
		return nil
	}

	hash, err := auth.HashPassword(defaultAdminPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().Unix()
	user := &database.User{
		Username:     defaultAdminUsername,
		PasswordHash: hash,
		DisplayName:  defaultAdminUsername,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := db.CreateUser(user); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	log.Info().Str("username", defaultAdminUsername).Msg("admin user created")
	return nil
}
