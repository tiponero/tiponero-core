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
	StatusPending        TransactionStatus = "pending"
	StatusMempool        TransactionStatus = "mempool"
	StatusConfirming     TransactionStatus = "confirming"
	StatusPartial      TransactionStatus = "partial"
	StatusConfirmed      TransactionStatus = "confirmed"
	StatusReverted       TransactionStatus = "reverted"
	StatusExpired        TransactionStatus = "expired"
)

// Terminal reports whether no further payment state change is expected.
// A reverted transaction is not terminal: a dropped transfer can return.
func (s TransactionStatus) Terminal() bool {
	switch s {
	case StatusConfirmed, StatusExpired:
		return true
	default:
		return false
	}
}

type Transaction struct {
	ID              string
	UserID          string
	WidgetID        sql.NullString
	Subaddress      string
	SubaddressIndex uint64
	Amount          int64
	ReceivedAmount  int64
	FiatAmount      float64
	FiatCurrency    string
	Status          TransactionStatus
	Confirmations   int
	IsPayment       bool
	DonorName       string
	Note            string
	TxHash          string
	Description     string
	RedirectURL     string
	CallbackURL     string
	Metadata        string
	CreatedAt       int64
	UpdatedAt       int64
	ConfirmedAt     int64
	ExpiresAt       int64
	RevertedAt      int64

	// ConfirmationsRequired overrides the global value when >= 0.
	// Zero means a mempool payment is enough (0-conf).
	ConfirmationsRequired int
	IsTest                bool
}

type TransactionStats struct {
	TotalTransactions int
	TotalAmount       int64
	PendingCount      int
	TodayCount        int
	TodayAmount       int64
}

type TransactionFilter struct {
	Status      TransactionStatus
	Skip        int
	Take        int
	IncludeTest bool
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

	// Confirmations overrides the global confirmation count when >= 0.
	// Zero means a mempool payment is enough (0-conf).
	Confirmations int

	// PriceFiat makes the payment amount a fiat price. The XMR amount is
	// computed and locked when the transaction is created.
	PriceFiat     float64
	PriceCurrency string

	// GoalAmount adds an optional donation progress target.
	GoalAmount   float64
	GoalCurrency string
	GoalDeadline int64
}

type WidgetStats struct {
	TotalTransactions int
	TotalAmount       int64
}

type WalletConfig struct {
	ID             string
	UserID         string
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

// Notification events. A webhook or channel subscribes to a subset.
const (
	EventPaymentCreated        = "payment.created"
	EventPaymentMempool        = "payment.mempool"
	EventPaymentPartial        = "payment.partial"
	EventPaymentConfirming     = "payment.confirming"
	EventPaymentConfirmed      = "payment.confirmed"
	EventPaymentReverted       = "payment.reverted"
	EventPaymentExpired        = "payment.expired"
)

// AllEvents is the default subscription for a new endpoint.
var AllEvents = []string{
	EventPaymentCreated,
	EventPaymentMempool,
	EventPaymentPartial,
	EventPaymentConfirming,
	EventPaymentConfirmed,
	EventPaymentReverted,
	EventPaymentExpired,
}

type Webhook struct {
	ID        string
	UserID    string
	Name      string
	URL       string
	SecretEnc string
	Events    string
	Active    bool
	CreatedAt int64
	UpdatedAt int64
}

type NotificationChannel struct {
	ID        string
	UserID    string
	Type      string
	Name      string
	ConfigEnc string
	Events    string
	Active    bool
	CreatedAt int64
	UpdatedAt int64
}

type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

// NotificationDelivery is one queued webhook call or channel message.
// The outbox lives in the database so a restart does not lose events.
type NotificationDelivery struct {
	ID           string
	UserID       string
	Kind         string // webhook | channel
	TargetID     string
	Event        string
	EventID      string
	Payload      string
	Status       DeliveryStatus
	Attempts     int
	ResponseCode int
	LastError    string
	NextRetryAt  int64
	CreatedAt    int64
	DeliveredAt  int64
}
