package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Config struct {
	Port                  string
	BaseURL               string
	SecureCookies         bool
	DatabasePath          string
	EncryptionKey         string
	FiatCurrency          string
	RequiredConfirmations int
	TransactionExpiry     time.Duration
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	baseUrl := os.Getenv("BASE_URL")
	if baseUrl == "" {
		baseUrl = "http://localhost:" + port
	}

	fiatCurrency := os.Getenv("FIAT_CURRENCY")
	if fiatCurrency == "" {
		fiatCurrency = "USD"
	}

	confirmations := 10
	if v := os.Getenv("CONFIRMATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			confirmations = n
		}
	}

	transactionExpiry := 1 * time.Hour
	if v := os.Getenv("TRANSACTION_EXPIRY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			transactionExpiry = d
		}
	}

	cfg := Config{
		Port:                  port,
		BaseURL:               baseUrl,
		SecureCookies:         strings.HasPrefix(baseUrl, "https://"),
		DatabasePath:          os.Getenv("DATABASE_PATH"),
		EncryptionKey:         os.Getenv("ENCRYPTION_KEY"),
		FiatCurrency:          fiatCurrency,
		RequiredConfirmations: confirmations,
		TransactionExpiry:     transactionExpiry,
	}

	cfg.validate()
	return cfg
}

func (c *Config) validate() {
	if c.EncryptionKey == "" {
		log.Warn().Msg("ENCRYPTION_KEY is not set, sessions and wallet config will not be secure")
	}
	if c.DatabasePath == "" {
		log.Warn().Msg("DATABASE_PATH is not set, using in-memory database (data will be lost on restart)")
	}
}
