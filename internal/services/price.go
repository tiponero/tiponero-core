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
	coingeckoURL     = "https://api.coingecko.com/api/v3/simple/price"
	coinmarketcapURL = "https://pro-api.coinmarketcap.com/v1/cryptocurrency/quotes/latest"
	priceCacheTTL    = 5 * time.Minute
)

type cachedRate struct {
	price     float64
	fetchedAt time.Time
}

type PriceService struct {
	client          *http.Client
	defaultCurrency string
	cmcAPIKey       string

	mu    sync.RWMutex
	rates map[string]cachedRate
}

func NewPriceService(fiatCurrency, cmcAPIKey string) *PriceService {
	if fiatCurrency == "" {
		fiatCurrency = "USD"
	}
	return &PriceService{
		client:          &http.Client{Timeout: 10 * time.Second},
		defaultCurrency: strings.ToUpper(fiatCurrency),
		cmcAPIKey:       cmcAPIKey,
		rates:           make(map[string]cachedRate),
	}
}

func (ps *PriceService) Currency() string {
	return ps.defaultCurrency
}

// Rate returns the price of one XMR in the given fiat currency. A cached
// value is used for five minutes. When every provider fails, the last known
// value is returned so a payment page does not break.
func (ps *PriceService) Rate(currency string) (float64, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = ps.defaultCurrency
	}

	ps.mu.RLock()
	cached, ok := ps.rates[currency]
	ps.mu.RUnlock()
	if ok && time.Since(cached.fetchedAt) < priceCacheTTL && cached.price > 0 {
		return cached.price, nil
	}

	price, err := ps.fetchCoinGecko(currency)
	if err != nil {
		log.Warn().Err(err).Str("currency", currency).Msg("coingecko price fetch failed, trying coinmarketcap")
		price, err = ps.fetchCoinMarketCap(currency)
	}
	if err != nil {
		ps.mu.RLock()
		stale := ps.rates[currency].price
		ps.mu.RUnlock()
		if stale > 0 {
			log.Warn().Err(err).Str("currency", currency).Msg("using stale XMR price")
			return stale, nil
		}
		return 0, err
	}

	ps.mu.Lock()
	ps.rates[currency] = cachedRate{price: price, fetchedAt: time.Now()}
	ps.mu.Unlock()

	return price, nil
}

func (ps *PriceService) GetXMRPrice() float64 {
	price, err := ps.Rate(ps.defaultCurrency)
	if err != nil {
		return 0
	}
	return price
}

func (ps *PriceService) ConvertXMRToFiat(piconero int64) float64 {
	price := ps.GetXMRPrice()
	if price == 0 {
		return 0
	}
	return float64(piconero) / 1e12 * price
}

func (ps *PriceService) ConvertXMRToFiatIn(piconero int64, currency string) (float64, error) {
	price, err := ps.Rate(currency)
	if err != nil {
		return 0, err
	}
	return float64(piconero) / 1e12 * price, nil
}

// ConvertFiatToPiconero converts a fiat price into piconero and returns the
// rate that was used, so the caller can lock it on the transaction.
func (ps *PriceService) ConvertFiatToPiconero(amount float64, currency string) (int64, float64, error) {
	if amount <= 0 {
		return 0, 0, fmt.Errorf("amount must be greater than zero")
	}
	price, err := ps.Rate(currency)
	if err != nil {
		return 0, 0, err
	}
	if price <= 0 {
		return 0, 0, fmt.Errorf("invalid XMR rate for %s", currency)
	}
	piconero := amount / price * 1e12
	if piconero < 1 {
		return 0, price, fmt.Errorf("amount is too small")
	}
	if piconero > 9.2e18 {
		return 0, price, fmt.Errorf("amount is too large")
	}
	return int64(piconero + 0.5), price, nil
}

func (ps *PriceService) fetchCoinGecko(currency string) (float64, error) {
	url := fmt.Sprintf("%s?ids=monero&vs_currencies=%s", coingeckoURL, strings.ToLower(currency))

	resp, err := ps.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("coingecko request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	price, ok := monero[strings.ToLower(currency)]
	if !ok || price <= 0 {
		return 0, fmt.Errorf("coingecko: currency %s not found", currency)
	}
	return price, nil
}

func (ps *PriceService) fetchCoinMarketCap(currency string) (float64, error) {
	if ps.cmcAPIKey == "" {
		return 0, fmt.Errorf("coinmarketcap api key is not configured")
	}

	url := fmt.Sprintf("%s?symbol=XMR&convert=%s", coinmarketcapURL, strings.ToUpper(currency))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("coinmarketcap request: %w", err)
	}
	req.Header.Set("X-CMC_PRO_API_KEY", ps.cmcAPIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := ps.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("coinmarketcap request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("coinmarketcap returned status %d", resp.StatusCode)
	}

	var result struct {
		Data map[string]struct {
			Quote map[string]struct {
				Price float64 `json:"price"`
			} `json:"quote"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("coinmarketcap decode: %w", err)
	}

	xmr, ok := result.Data["XMR"]
	if !ok {
		return 0, fmt.Errorf("coinmarketcap: XMR not found in response")
	}
	quote, ok := xmr.Quote[strings.ToUpper(currency)]
	if !ok || quote.Price <= 0 {
		return 0, fmt.Errorf("coinmarketcap: currency %s not found", currency)
	}
	return quote.Price, nil
}
