package database

import "database/sql"

type User struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  string
	Bio          string
	AvatarURL    string
	TOTPSecret   string
	CreatedAt    int64
	UpdatedAt    int64
}

type TransactionStatus string

const (
	StatusPending    TransactionStatus = "pending"
	StatusMempool    TransactionStatus = "mempool"
	StatusConfirming TransactionStatus = "confirming"
	StatusConfirmed  TransactionStatus = "confirmed"
	StatusExpired    TransactionStatus = "expired"
)

type Transaction struct {
	ID              string
	UserID          string
	WidgetID        sql.NullString
	Subaddress      string
	SubaddressIndex uint64
	Amount          int64
	FiatAmount      float64
	FiatCurrency    string
	Status          TransactionStatus
	Confirmations   int
	IsPayment       bool
	DonorName       string
	Note            string
	TxHash          string
	CreatedAt       int64
	UpdatedAt       int64
	ConfirmedAt     int64
	ExpiresAt       int64
}

type TransactionStats struct {
	TotalTransactions int
	TotalAmount       int64
	PendingCount      int
	TodayCount        int
	TodayAmount       int64
}

type TransactionFilter struct {
	Status TransactionStatus
	Skip   int
	Take   int
}

type Pagination struct {
	Page       int
	Skip       int
	Take       int
	Total      int
	TotalPages int
}

type WidgetMode string

const (
	ModeDonation WidgetMode = "donation"
	ModePayment  WidgetMode = "payment"
)

type WidgetTheme string

const (
	ThemeSystem WidgetTheme = "system"
	ThemeLight  WidgetTheme = "light"
	ThemeDark   WidgetTheme = "dark"
)

type Widget struct {
	ID              string
	UserID          string
	Name            string
	Mode            WidgetMode
	PresetAmounts   string
	ButtonText      string
	CustomMessage   string
	ThankYouMessage string
	PrimaryColor    string
	Theme           WidgetTheme
	ShowStats       bool
	RedirectURL     string
	CreatedAt       int64
	UpdatedAt       int64
}

type WidgetStats struct {
	TotalTransactions int
	TotalAmount       int64
}

type WalletConfig struct {
	ID             string
	RPCURL         string
	RPCUser        string
	RPCPassword    string
	WalletFile     string
	WalletPassword string
	CreatedAt      int64
	UpdatedAt      int64
}

type APIKey struct {
	ID         string
	UserID     string
	Name       string
	KeyHash    string
	Prefix     string
	CreatedAt  int64
	ExpiresAt  int64
	LastUsedAt int64
}
