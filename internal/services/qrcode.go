package services

import (
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

func GenerateQRCode(address string, amountPiconero int64) ([]byte, error) {
	uri := "monero:" + address
	if amountPiconero > 0 {
		xmr := float64(amountPiconero) / 1e12
		uri += fmt.Sprintf("?tx_amount=%.12f", xmr)
	}
	return qrcode.Encode(uri, qrcode.Medium, 256)
}
