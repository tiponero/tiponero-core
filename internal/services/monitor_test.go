package services

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/tiponero/tiponero-core/internal/database"
	"github.com/tiponero/tiponero-core/internal/monero"
)

func TestIndexTransfersSumsAndDeduplicates(t *testing.T) {
	m := &PaymentMonitor{log: zerolog.Nop()}
	result := &monero.GetTransfersResult{
		In: []monero.Transfer{
			{Address: "A", Amount: 40, Confirmations: 2, TxHash: "t1"},
			{Address: "A", Amount: 60, Confirmations: 5, TxHash: "t2"},
		},
		Pending: []monero.Transfer{
			{Address: "A", Amount: 40, Confirmations: 2, TxHash: "t1"},
			{Address: "B", Amount: 100, Confirmations: 1, TxHash: "t3"},
		},
		Pool: []monero.Transfer{
			{Address: "B", Amount: 100, Confirmations: 1, TxHash: "t3"},
		},
	}

	got := m.indexTransfers(result)

	if got["A"].amount != 100 {
		t.Fatalf("A amount = %d, want 100", got["A"].amount)
	}
	if got["A"].confirmations != 5 {
		t.Fatalf("A confirmations = %d, want 5", got["A"].confirmations)
	}
	if got["A"].txHash != "t2" {
		t.Fatalf("A tx hash = %q, want t2", got["A"].txHash)
	}
	if got["B"].amount != 100 {
		t.Fatalf("B amount = %d, want 100", got["B"].amount)
	}
	if len(got) != 2 {
		t.Fatalf("indexed addresses = %d, want 2", len(got))
	}
}

func TestResolveStatus(t *testing.T) {
	m := &PaymentMonitor{confirmations: 10}
	tests := []struct {
		name      string
		requested int64
		amount    uint64
		confirms  uint64
		want      database.TransactionStatus
	}{
		{name: "underpaid in mempool", requested: 100, amount: 50, confirms: 0, want: database.StatusUnderpaid},
		{name: "underpaid with confirmations", requested: 100, amount: 50, confirms: 20, want: database.StatusUnderpaid},
		{name: "exact in mempool", requested: 100, amount: 100, confirms: 0, want: database.StatusMempool},
		{name: "exact confirming", requested: 100, amount: 100, confirms: 1, want: database.StatusConfirming},
		{name: "overpaid confirming", requested: 100, amount: 150, confirms: 9, want: database.StatusConfirming},
		{name: "exact confirmed", requested: 100, amount: 100, confirms: 10, want: database.StatusConfirmed},
		{name: "open donation in mempool", requested: 0, amount: 1, confirms: 0, want: database.StatusMempool},
		{name: "open donation confirmed", requested: 0, amount: 1, confirms: 10, want: database.StatusConfirmed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := database.Transaction{Amount: tt.requested}
			transfer := transferTotal{amount: tt.amount, confirmations: tt.confirms}
			if got := m.resolveStatus(tx, transfer); got != tt.want {
				t.Fatalf("resolveStatus(requested=%d, amount=%d, confirms=%d) = %s, want %s", tt.requested, tt.amount, tt.confirms, got, tt.want)
			}
		})
	}
}

func TestIndexTransfersKeepsMultipleOutputsOfOneTransaction(t *testing.T) {
	m := &PaymentMonitor{log: zerolog.Nop()}
	result := &monero.GetTransfersResult{
		In: []monero.Transfer{
			{Address: "C", Amount: 10, Confirmations: 1, TxHash: "t4", SubaddrIndex: monero.SubaddrIndex{Minor: 1}},
			{Address: "C", Amount: 20, Confirmations: 1, TxHash: "t4", SubaddrIndex: monero.SubaddrIndex{Minor: 2}},
		},
	}

	got := m.indexTransfers(result)
	if got["C"].amount != 30 {
		t.Fatalf("C amount = %d, want 30", got["C"].amount)
	}
}
