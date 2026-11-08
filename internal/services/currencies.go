package services

import "strings"

// SupportedCurrencies is the list the admin panel offers for fiat prices and
// goals. Every entry is a code that CoinGecko and CoinMarketCap both quote
// against XMR.
var SupportedCurrencies = []string{
	"USD", "EUR", "GBP", "CHF", "CAD", "AUD", "NZD",
	"JPY", "CNY", "HKD", "SGD", "KRW", "INR", "TWD",
	"BRL", "MXN", "ARS", "CLP", "COP", "PEN",
	"ZAR", "NGN", "KES", "EGP",
	"TRY", "RUB", "UAH", "PLN", "CZK", "HUF", "RON", "BGN",
	"SEK", "NOK", "DKK", "ISK",
	"ILS", "AED", "SAR", "QAR", "THB", "IDR", "MYR", "PHP", "VND",
}

// IsSupportedCurrency reports whether the code is one of the offered fiat
// currencies. XMR is handled separately because it is not fiat.
func IsSupportedCurrency(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, c := range SupportedCurrencies {
		if c == code {
			return true
		}
	}
	return false
}
