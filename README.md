# 📉 Wishlist Price Tracker

A lightweight service that tracks product prices from Australian retailers and sends email notifications when prices drop.

Paste a product URL, enter your email, and the system will monitor the price daily — notifying you when it drops or hits your target price.

## Supported Stores

| Store | Method |
|---|---|
| **Woolworths** | Internal JSON API (session cookie + `/apis/ui/product/detail/{stockcode}`) |
| **Chemist Warehouse** | HTML scraping with embedded JSON price extraction |

Adding a new store takes ~30 lines of Go — just implement the `Store` interface.

## Features

- **Track products** — submit a URL + email, system scrapes name, price, and image
- **Daily price polling** — configurable cron schedule (default: 3 AM UTC)
- **Price history** — stored per day, viewable as a chart (Chart.js) or table
- **Email alerts** — styled HTML emails with product image, price comparison, history table, and embedded PNG chart
- **Digest emails** — multiple price drops = one consolidated email, not N separate ones
- **Notification muting** — once notified, the item is muted; re-enable when ready for new alerts
- **Manual check** — trigger a price check from the UI at any time
- **Zero external dependencies** — single Go binary + SQLite file + one HTML page

## Tech Stack

| Component | Technology |
|---|---|
| Backend | Go 1.22, Gin |
| Database | SQLite (pure-Go via `modernc.org/sqlite`) |
| Scraping | `goquery`, `net/http` |
| Scheduling | `gocron` |
| Charts | `go-chart` (server-side PNG), Chart.js (frontend) |
| Email | SMTP with MIME multipart (HTML + inline images) |
| Frontend | Single HTML file, vanilla JS, no build step |
| Config | `.env` file via `godotenv` |

## Project Structure

```
wishlist-tracker/
├── cmd/
│   ├── server/main.go          # Entry point
│   └── probe/main.go           # CLI tool to test scrapers
├── internal/
│   ├── api/handlers.go         # Gin HTTP handlers
│   ├── chart/chart.go          # PNG chart generation
│   ├── config/config.go        # Env-based configuration
│   ├── db/sqlite.go            # SQLite database layer
│   ├── models/models.go        # Data models
│   ├── notify/email.go         # Email notifications (Notifier interface)
│   ├── scheduler/poller.go     # Daily price polling
│   └── stores/
│       ├── store.go            # Store interface + registry
│       ├── woolworths.go       # Woolworths scraper
│       └── chemistwarehouse.go # Chemist Warehouse scraper
├── web/index.html              # Frontend (single file)
├── Dockerfile                  # Multi-stage Docker build
├── fly.toml                    # Fly.io deployment config
├── .env                        # Local secrets (gitignored)
└── go.mod
```

## Quick Start (Local)

```bash
# Clone
git clone https://github.com/soeviturek/wishlist.git
cd wishlist

# Create .env file with your SMTP credentials
cat > .env << 'EOF'
SERVER_PORT=8080
DATABASE_PATH=./wishlist.db
SCHEDULER_CRON=0 3 * * *
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-gmail-app-password
SMTP_FROM=your-email@gmail.com
EOF

# Run
CGO_ENABLED=0 go run cmd/server/main.go
```

Open http://localhost:8080

## Configuration

All config is via environment variables (or `.env` file):

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP port |
| `DATABASE_PATH` | `./wishlist.db` | SQLite database file path |
| `SCHEDULER_CRON` | `0 3 * * *` | Cron expression for daily polling |
| `SMTP_HOST` | `smtp.gmail.com` | SMTP server |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USERNAME` | | Gmail address |
| `SMTP_PASSWORD` | | Gmail App Password |
| `SMTP_FROM` | | Sender email address |
| `DEBUG` | `false` | Enable verbose logging |

## API Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/items` | Register a product to track |
| `GET` | `/items?email=...` | List tracked items for an email |
| `GET` | `/items/:id/history` | Get price history |
| `GET` | `/items/:id/chart.png` | Get price history as PNG chart |
| `POST` | `/items/:id/check` | Manually trigger a price check |
| `PATCH` | `/items/:id/notify` | Toggle notification mute |
| `DELETE` | `/items/:id` | Remove a tracked item |
| `GET` | `/health` | Health check |

## Deployment

See [DEPLOY.md](DEPLOY.md) for full Azure App Service deployment guide.

## Adding a New Store

Implement the `Store` interface in `internal/stores/`:

```go
type Store interface {
    Match(url string) bool
    Name() string
    GetProduct(url string) (*models.Product, error)
}
```

Then register it in `internal/stores/store.go`:

```go
func init() {
    Register(&ChemistWarehouse{})
    Register(&Woolworths{})
    Register(&YourNewStore{})  // add here
}
```

## License

MIT
