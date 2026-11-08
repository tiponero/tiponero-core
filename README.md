# Tiponero

A self-hosted Monero payments engine. Single Go binary with embedded SQLite, HTMX frontend, and per-transaction subaddress generation for privacy-preserving payments and donations.

[![Support Tiponero](https://demo.tiponero.org/widget/dda5980a-157f-4fed-8b19-97b4940ef808/badge.svg)](https://demo.tiponero.org/widget/dda5980a-157f-4fed-8b19-97b4940ef808)

## Documentation

- [Documentation index](./docs/README.md)
- [System architecture](./docs/architecture.md): processes, protocols, deployment, secrets
- [Data model](./docs/data-model.md): SQLite tables, columns, relations, indexes
- [Flows and business logic](./docs/flows.md): setup, payment flow, state machine, API auth

## Features

- **Developer REST API** at `/api/v1` with bearer token authentication via named API keys
- **Payment requests without a widget**: `POST /api/v1/transactions` creates a charge, returns a subaddress, a `monero:` URI, and a hosted `/pay/<id>` page
- **Unique subaddresses** per transaction via Monero wallet RPC
- **Embeddable widgets** with customizable preset amounts, button text, and messages
- **Real-time payment tracking** with background polling (pending, mempool, confirming, partial, confirmed, reverted)
- **Partial payment top-up**: a partial request shows the remaining amount, a fresh QR code, and stays open until it expires
- **Per-widget and per-request confirmations**, including `0` for mempool-only (0-conf) payments
- **Fiat pricing**: a payment widget can hold a fiat price, and the XMR amount is locked when the request is created
- **Donation goals** with a progress bar on the widget
- **Signed webhooks** with HMAC-SHA256, encrypted secrets, retries for 12 hours, and a delivery log
- **Notification channels**: Telegram, Discord, Slack, ntfy, WhatsApp through Twilio, generic JSON, and email over SMTP (SendGrid and others)
- **Admin dashboard** with transaction stats, filtering, widget editing, and CSV export
- **TOTP two-factor authentication** for admin login (compatible with any authenticator app)
- **Fiat conversion** with automatic XMR price snapshots at transaction time (CoinGecko, CoinMarketCap fallback, configurable currency)
- **Dark mode** with system/light/dark per-widget theme override
- **Embeddable SVG badge** (`/widget/<id>/badge.svg`) with QR code and live transaction stats
- **Self-hosted assets**: fonts and icons are served from `/static`, so the browser contacts no third party
- **Guided view-only wallet creation** from the admin panel, without the Monero CLI
- **CSRF protection and per-IP rate limiting** on the admin and API surfaces
- **Configurable transaction expiry** via `TRANSACTION_EXPIRY` env var
- **Single binary** deployment, all static assets embedded via `go:embed`
- **SQLite** with WAL mode, no external database needed

## Tech Stack

Go, Chi router, templ, HTMX 2.x, Alpine.js 3.x, Tailwind CSS 3, SQLite, bcrypt, gorilla/sessions

## Requirements

- A running `monero-wallet-rpc` instance (connects to your Monero daemon)
- **Either** Nix (recommended, handles all build dependencies) **or** the following installed manually:
  - Go 1.25+
  - [templ](https://templ.guide) CLI
  - [Tailwind CSS](https://tailwindcss.com) CLI (v3)

## Quick Start

### 1. Configure

```bash
cp .env.example .env
```

Edit `.env` with your desired port, database path, encryption key, and fiat currency. Wallet RPC connection is configured through the admin UI after first login (see [Wallet RPC Setup](#wallet-rpc-setup)).

### 2. Build and run

**With Docker Compose** (recommended):

```bash
docker compose up -d
```

The database is persisted in a named Docker volume. To rebuild after code changes:

```bash
docker compose up -d --build
```

**With Nix** (no other dependencies needed):

```bash
nix build
./result/bin/tiponero
```

**With Make** (requires Go; the Makefile fetches templ and Tailwind CSS through the `tools` module):

```bash
make build
./bin/tiponero
```

**Development mode** (builds and runs in one step):

```bash
make dev
```

### 3. Use

The admin panel is at `http://localhost:8080/admin`. On first startup, a default admin user is created with username `admin` and password `admin`.

> **Warning**: Change the default password immediately after your first login via Settings > Profile.

Create a widget in the admin panel, then share its public URL (`/widget/<id>`). Configure your Monero wallet RPC connection in Settings > Wallet (see [Wallet RPC Setup](#wallet-rpc-setup)).

#### Widget modes

- **Donation mode**: Donors choose from preset amounts or enter a custom amount. Optionally provide their name and a note. Suitable for tips, donations, and open-ended contributions.
- **Payment mode**: A single fixed price is shown (no custom amount, no donor name field). After confirmation, the user can be automatically redirected to a URL you configure. Suitable for purchases, invoices, and checkout flows.

## Configuration

All configuration is via environment variables (or `.env` file):

| Variable | Description | Default |
|---|---|---|
| `PORT` | HTTP listen port | `8080` |
| `BASE_URL` | Public base URL (enables secure cookies when set to `https://`) | `http://localhost:${PORT}` |
| `DATABASE_PATH` | SQLite database file path | `./tiponero.db` |
| `ENCRYPTION_KEY` | Master key for AES-GCM encryption of wallet credentials (32+ chars) | `change-me-in-production-32-chars` |
| `FIAT_CURRENCY` | Fiat currency for price conversion (e.g. `USD`, `EUR`, `GBP`) | `USD` |
| `CONFIRMATIONS` | Default block confirmations before a payment is final. A widget or a payment request can override it, including `0` | `10` |
| `TRANSACTION_EXPIRY` | How long a transaction stays active before expiring (Go duration) | `1h` |
| `COINMARKETCAP_API_KEY` | Optional fallback price provider when CoinGecko fails | unset |
| `MOCK_PAYMENTS` | Adds a "Simulate payment" button in the admin panel. Test rows are excluded from stats | `false` |
| `TRUST_PROXY` | Trust `X-Forwarded-For` from a reverse proxy for rate limiting | `false` |

Wallet RPC credentials are **not** set via environment variables. They are configured through the admin UI and stored AES-GCM encrypted in the database (see below).

Missing or invalid environment variables are logged as warnings on startup. The application will still start using default values.

## Wallet RPC Setup

Tiponero connects to a running `monero-wallet-rpc` instance to generate subaddresses and monitor payments. The connection is configured entirely through the admin panel:

1. Log in to the admin panel at `/admin`
2. Go to **Settings > Wallet**
3. Enter the base URL of your `monero-wallet-rpc` (e.g. `http://127.0.0.1:18088`), digest auth username, and password. Do not add `/json_rpc`; Tiponero appends it. Port 18088 is the port Monero reserves for Wallet RPC
4. Optionally provide a wallet filename and wallet password if you need Tiponero to call `open_wallet` on startup
5. Save -- Tiponero will test the connection and show the RPC status in the navigation bar

The RPC password and wallet password are encrypted at rest using AES-256-GCM with keys derived (via HKDF) from your `ENCRYPTION_KEY`. The RPC URL and username are stored in plaintext.

## Development

Enter a Nix dev shell with all tools (go, gopls, templ, tailwindcss, sqlite):

```bash
nix develop
```

From there you can use the Makefile targets:

| Target | Description |
|---|---|
| `make generate` | Compile `.templ` files to Go |
| `make css` | Build Tailwind CSS |
| `make build` | generate + css + go build |
| `make dev` | Hot-reload via Air (watches `.go`, `.templ`, `.css`; proxy on `:8081`) |
| `make lint` | Run golangci-lint |
| `make clean` | Remove build artifacts |

## Project Structure

```
cmd/tiponero/main.go              Entry point, wiring, graceful shutdown
internal/
  config/                        Typed env config with validation
  crypto/                        AES-GCM encryption, HKDF key derivation
  database/                      SQLite schema, models, repositories
  monero/                        Wallet RPC client (digest auth)
  auth/                          bcrypt, sessions, middleware, TOTP 2FA, API key auth
  services/                      Payment monitor, QR codes, TOTP, fiat price
  handlers/                      HTTP handlers, Chi router, REST API
  views/
    layouts/                     Admin and widget page shells
    admin/                       Admin templates (dashboard, transactions, settings, widgets, 2FA setup)
    widget/                      Public widget templates (home, payment, status)
    components/                  Shared components (stats card, transaction row)
static/                          Embedded assets (CSS, HTMX, Alpine.js)
```

## Payment requests

`POST /api/v1/transactions` creates a charge. Send `amount_xmr` or `amount_fiat`, not both.

```bash
curl -X POST https://tiponero.example/api/v1/transactions \
  -H "Authorization: Bearer tip_..." \
  -H "Content-Type: application/json" \
  -d '{
    "amount_fiat": 12.50,
    "currency": "USD",
    "description": "Order #1042",
    "confirmations": 0,
    "expires_in": 1800,
    "redirect_url": "https://shop.example/thanks",
    "metadata": {"order_id": "1042"}
  }'
```

The response contains `id`, `subaddress`, `amount`, `amount_xmr`, `monero_uri`, `payment_url`, `qr_url`, `status_url`, and `expires_at`. Send the payer to `payment_url`.

Without `widget_id` the request is standalone and uses `/pay/<id>`. With `widget_id` the widget supplies defaults, and an amount in the request still wins.

## Webhooks and notifications

Open **Notifications** in the admin panel to add a webhook or a chat channel. Every endpoint subscribes to individual events:

`payment.created`, `payment.mempool`, `payment.partial`, `payment.confirming`, `payment.confirmed`, `payment.reverted`, `payment.expired`.

A webhook receives a JSON body and three headers:

| Header | Meaning |
|---|---|
| `X-Tiponero-Event` | The event name |
| `X-Tiponero-Delivery` | A unique delivery ID, safe for deduplication |
| `X-Tiponero-Timestamp` | Unix seconds used in the signature |
| `X-Tiponero-Signature` | `sha256=` + HMAC-SHA256 of `<timestamp>.<raw body>` |

Verify the signature with the secret shown once at creation:

```python
import hmac, hashlib
expected = "sha256=" + hmac.new(secret.encode(), timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, header)
```

Secrets are stored encrypted with a key derived from `ENCRYPTION_KEY`, never hashed, because HMAC needs the raw value on every delivery. Rotating the secret in the admin panel invalidates the old one immediately.

Failed deliveries retry six times over about 15 hours. The delivery log in the admin panel shows the response code and the last error.

## Transaction Flow

1. User visits a widget page (`/widget/<id>`)
2. **Donation mode**: selects a preset amount or enters a custom one, optionally provides name and note. **Payment mode**: sees the fixed price and optionally enters a reference note.
3. Server calls `monero-wallet-rpc` `create_address` to generate a unique subaddress. In payment mode, the server uses the widget preset as the requested amount.
4. If a specific amount is requested and fiat conversion is available, the current XMR price is snapshotted and stored with the transaction
5. User sees a QR code and address; page polls for payment status via HTMX
6. Background monitor polls `get_transfers`, sums transfers per subaddress, and updates status through `pending -> mempool -> confirming -> confirmed`. A transfer below the requested amount sets `partial`
7. On confirmation: shows the widget thank-you message, or automatically redirects to the given redirect URL if specified. The widget also posts a `tiponero:status` message to the parent window
8. A partial payment keeps the request open and shows the remaining amount with an updated QR code
9. Transactions expire after the configured `TRANSACTION_EXPIRY` (default 1 hour) if no payment is detected. The received amount stays on the record, so the merchant still sees a partial payment after expiry

## Two-Factor Authentication

TOTP 2FA can be enabled from **Settings** in the admin panel. It is compatible with any standard authenticator app (Google Authenticator, Authy, Aegis, etc.).

- **Enable**: Settings > "Enable 2FA" > scan QR code > enter verification code to confirm
- **Login with 2FA**: after entering your password, a second screen prompts for the 6-digit code
- **Disable**: Settings > enter current 2FA code > "Disable 2FA"

2FA is optional. When not enabled, login works with password only.

## Developer API

Tiponero exposes a REST API at `/api/v1` for programmatic access. All endpoints require a bearer token.

### Authentication

```
Authorization: Bearer tip_<32 hex chars>
```

API keys are created in **Settings > API Keys** in the admin panel. Each key has a name and an expiration date (max 1 year). The raw key is shown once on creation -- store it securely. Keys are stored bcrypt-hashed in the database.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/widgets` | List all widgets |
| `POST` | `/api/v1/widgets` | Create widget |
| `GET` | `/api/v1/widgets/{id}` | Get widget |
| `PATCH` | `/api/v1/widgets/{id}` | Partial update widget |
| `DELETE` | `/api/v1/widgets/{id}` | Delete widget |
| `GET` | `/api/v1/transactions` | List transactions (`?status=`, `?page=`, `?limit=`) |
| `POST` | `/api/v1/transactions` | Create a payment request. `widget_id` is optional |
| `GET` | `/api/v1/transactions/{id}` | Get transaction |
| `GET` | `/api/v1/webhooks` | List webhooks |
| `POST` | `/api/v1/webhooks` | Create a webhook. The signing secret is returned once |
| `DELETE` | `/api/v1/webhooks/{id}` | Delete a webhook |
| `GET` | `/api/v1/user` | Get user profile |
| `PATCH` | `/api/v1/user` | Update user (display_name, bio, avatar_url) |
| `POST` | `/api/v1/wallet` | Create wallet config (max 1) |
| `GET` | `/api/v1/wallet` | Get wallet config (no passwords) |
| `PATCH` | `/api/v1/wallet` | Update wallet config |
| `GET` | `/api/v1/stats` | Transaction statistics |
| `GET` | `/api/v1/keys` | List API keys |
| `DELETE` | `/api/v1/keys/{id}` | Delete API key |

### Response Conventions

- **Read** endpoints return `{"data": { ... }}` (single) or `{"data": [ ... ]}` (list)
- **Create** endpoints return `{"id": "<uuid>"}` with status `201`
- **Delete** endpoints return `{"deleted": "<uuid>"}` with status `200`
- **Errors** return `{"error": "message"}` with the appropriate HTTP status code
- List transactions supports pagination: `{"data": [...], "pagination": {"page": 1, "limit": 20, "total": 42, "total_pages": 3}}`

## License

MIT
