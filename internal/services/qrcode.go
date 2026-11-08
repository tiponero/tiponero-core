package services

import (
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// MoneroURI builds a monero: payment URI. Wallet apps read tx_amount as XMR.
func MoneroURI(address string, amountPiconero int64) string {
	uri := "monero:" + address
	if amountPiconero > 0 {
		xmr := float64(amountPiconero) / 1e12
		uri += fmt.Sprintf("?tx_amount=%.12f", xmr)
	}
	return uri
}

func GenerateQRCode(address string, amountPiconero int64) ([]byte, error) {
	return qrcode.Encode(MoneroURI(address, amountPiconero), qrcode.Medium, 256)
}
