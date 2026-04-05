package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	coingeckoURL  = "https://api.coingecko.com/api/v3/simple/price"
	priceCacheTTL = 5 * time.Minute
)

type PriceService struct {
	client   *http.Client
	currency string

	mu        sync.RWMutex
	price     float64
	fetchedAt time.Time
}

func NewPriceService(fiatCurrency string) *PriceService {
	if fiatCurrency == "" {
		fiatCurrency = "USD"
	}
	return &PriceService{
		client:   &http.Client{Timeout: 10 * time.Second},
		currency: strings.ToLower(fiatCurrency),
	}
}

func (ps *PriceService) Currency() string {
	return strings.ToUpper(ps.currency)
}

func (ps *PriceService) GetXMRPrice() float64 {
	ps.mu.RLock()
	if time.Since(ps.fetchedAt) < priceCacheTTL && ps.price > 0 {
		cached := ps.price
		ps.mu.RUnlock()
		return cached
	}
	ps.mu.RUnlock()

	price, err := ps.fetchPrice()
	if err != nil {
		log.Warn().Err(err).Msg("failed to fetch XMR price from CoinGecko")
		ps.mu.RLock()
		stale := ps.price
		ps.mu.RUnlock()
		return stale
	}

	ps.mu.Lock()
	ps.price = price
	ps.fetchedAt = time.Now()
	ps.mu.Unlock()

	return price
}

func (ps *PriceService) ConvertXMRToFiat(piconero int64) float64 {
	price := ps.GetXMRPrice()
	if price == 0 {
		return 0
	}
	xmr := float64(piconero) / 1e12
	return xmr * price
}

func (ps *PriceService) fetchPrice() (float64, error) {
	url := fmt.Sprintf("%s?ids=monero&vs_currencies=%s", coingeckoURL, ps.currency)

	resp, err := ps.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("coingecko request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("coingecko returned status %d", resp.StatusCode)
	}

	var result map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("coingecko decode: %w", err)
	}

	monero, ok := result["monero"]
	if !ok {
		return 0, fmt.Errorf("coingecko: monero not found in response")
	}

	price, ok := monero[ps.currency]
	if !ok {
		return 0, fmt.Errorf("coingecko: currency %s not found", ps.currency)
	}

	return price, nil
}
