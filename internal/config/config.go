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
	baseUrl := os.Getenv("BASE_URL")

	confirmations := 10
	if v := os.Getenv("CONFIRMATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			confirmations = n
		} else {
			log.Warn().Str("CONFIRMATIONS", v).Msg("invalid value, using default (10)")
		}
	}

	transactionExpiry := 1 * time.Hour
	if v := os.Getenv("TRANSACTION_EXPIRY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			transactionExpiry = d
		} else {
			log.Warn().Str("TRANSACTION_EXPIRY", v).Msg("invalid duration, using default (1h)")
		}
	}

	cfg := Config{
		Port:                  port,
		BaseURL:               baseUrl,
		SecureCookies:         strings.HasPrefix(baseUrl, "https://"),
		DatabasePath:          os.Getenv("DATABASE_PATH"),
		EncryptionKey:         os.Getenv("ENCRYPTION_KEY"),
		FiatCurrency:          os.Getenv("FIAT_CURRENCY"),
		RequiredConfirmations: confirmations,
		TransactionExpiry:     transactionExpiry,
	}

	cfg.validate()
	return cfg
}

func (c *Config) validate() {
	if c.Port == "" {
		log.Warn().Msg("PORT is not set, server may fail to start")
	}
	if c.BaseURL == "" {
		log.Warn().Msg("BASE_URL is not set, widget URLs and badges will be broken")
	}
	if c.EncryptionKey == "" {
		log.Warn().Msg("ENCRYPTION_KEY is not set, sessions and wallet config will not be secure")
	}
	if c.DatabasePath == "" {
		log.Warn().Msg("DATABASE_PATH is not set, using in-memory database (data will be lost on restart)")
	}
	if c.FiatCurrency == "" {
		log.Warn().Msg("FIAT_CURRENCY is not set, fiat conversion will be disabled")
	}
}
