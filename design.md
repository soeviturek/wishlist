# Wishlist Price Tracker

A lightweight service that tracks product prices from Australian retailer URLs and sends email notifications when prices drop.

## Features

- **Track product prices** via product URLs (Chemist Warehouse, Woolworths)
- **Daily polling** — automatically checks prices once a day
- **Price history** — stores every recorded price
- **Email alerts** — notifies when prices drop or reach a target price
- **Spam prevention** — avoids duplicate notifications for the same price

## Quick Start

### Prerequisites

- Go 1.22+
- GCC (required for SQLite via cgo) — on Windows, install [TDM-GCC](https://jmeubank.github.io/tdm-gcc/) or [MSYS2](https://www.msys2.org/)

### Setup

```bash
# Clone and enter the project
cd wishlist

# Install dependencies
go mod tidy

# Copy and configure environment variables
cp .env.example .env
# Edit .env with your SMTP credentials

# Run the server
go run ./cmd/server
```

The server starts on `http://localhost:8080`.

### API Usage

#### Register a product
```bash
curl -X POST http://localhost:8080/items \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@email.com",
    "url": "https://www.chemistwarehouse.com.au/buy/12345/product-name",
    "target_price": 15.00
  }'
```

**Response:**
```json
{
  "item_id": "uuid",
  "name": "Fish Oil 1000mg",
  "current_price": 18.50
}
```

#### List tracked products
```bash
curl "http://localhost:8080/items?email=user@email.com"
```

#### View price history
```bash
curl http://localhost:8080/items/{id}/history
```

**Response:**
```json
[
  { "date": "2026-03-01", "price": 22.00 },
  { "date": "2026-03-02", "price": 20.00 },
  { "date": "2026-03-03", "price": 17.00 }
]
```

#### Manual price check
```bash
curl -X POST http://localhost:8080/items/{id}/check
```

#### Delete a tracked item
```bash
curl -X DELETE http://localhost:8080/items/{id}
```

#### Health check
```bash
curl http://localhost:8080/health
```

## Project Structure

```
wishlist/
├── cmd/server/main.go           # Entry point
├── internal/
│   ├── api/handlers.go          # Gin HTTP handlers
│   ├── config/config.go         # Configuration from env vars
│   ├── db/sqlite.go             # SQLite database layer
│   ├── models/models.go         # Data models & request/response types
│   ├── notify/email.go          # SMTP email notifications
│   ├── scheduler/poller.go      # Daily price polling (gocron)
│   └── stores/
│       ├── store.go             # Store interface & registry
│       ├── chemistwarehouse.go  # Chemist Warehouse scraper
│       └── woolworths.go        # Woolworths scraper
├── go.mod
├── config.yaml
├── .env.example
└── README.md
```

## Adding a New Store Scraper

Create a new file in `internal/stores/` (~30 lines):

```go
package stores

import (
    "strings"
    "wishlist-tracker/internal/models"
)

type MyStore struct{}

func (s *MyStore) Match(url string) bool {
    return strings.Contains(strings.ToLower(url), "mystore.com.au")
}

func (s *MyStore) Name() string {
    return "My Store"
}

func (s *MyStore) GetProduct(url string) (*models.Product, error) {
    // Fetch and parse the page...
    return &models.Product{Name: "Product", Price: 9.99}, nil
}
```

Then register it in `internal/stores/store.go`:
```go
func init() {
    Register(&ChemistWarehouse{})
    Register(&Woolworths{})
    Register(&MyStore{})        // ← add here
}
```

## Configuration

All settings are read from environment variables:

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP server port |
| `DATABASE_PATH` | `./wishlist.db` | SQLite database file path |
| `SCHEDULER_CRON` | `0 3 * * *` | Polling cron (daily at 3 AM UTC) |
| `SMTP_HOST` | `smtp.gmail.com` | SMTP server host |
| `SMTP_PORT` | `587` | SMTP server port |
| `SMTP_USERNAME` | — | SMTP login |
| `SMTP_PASSWORD` | — | SMTP password (use app password for Gmail) |
| `SMTP_FROM` | — | Sender email address |
| `DEBUG` | `false` | Enable verbose logging |

## Technology Stack

- **Go** — backend language
- **Gin** — HTTP framework
- **SQLite** — database (via go-sqlite3)
- **goquery** — HTML scraping
- **gocron** — job scheduler
- **SMTP** — email notifications
