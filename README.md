# Stock Rewards Portfolio Service

![Go](https://img.shields.io/badge/Go-1.21-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-12%2B-336791?logo=postgresql&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)

A Go REST API for managing employee stock rewards with double-entry ledger accounting, idempotent event processing, and Indian-market fee calculations.

---

## Table of Contents

- [Features](#features)
- [Tech Stack](#tech-stack)
- [How It Works](#how-it-works)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Project Structure](#project-structure)
- [Limitations](#limitations)
- [License](#license)

---

## Features

- **Double-entry accounting** — every reward creates three balanced ledger entries (STOCK credit, CASH debit, FEE credit); an imbalance check runs before each commit
- **Idempotent reward processing** — duplicate `event_id` submissions are detected and silently ignored
- **Indian-market fee calculation** — brokerage (0.03%, min 20 INR), STT (0.025%), GST (18% on brokerage), exchange charges (0.00325%), SEBI charges (0.0001%), stamp duty (0.003%)
- **Background price fetcher** — runs on a configurable interval (default 1 h), stores prices per symbol in PostgreSQL
- **Historical portfolio valuation** — returns INR value for each of the last 30 days, falling back to the latest known price when historical data is unavailable
- **Graceful shutdown** — handles SIGINT/SIGTERM cleanly

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.21 |
| HTTP framework | Gin |
| Database | PostgreSQL 12+ |
| SQL client | sqlx + lib/pq |
| Decimal arithmetic | shopspring/decimal |
| Logging | logrus (JSON) |
| UUIDs | google/uuid |
| Config | godotenv |

---

## How It Works

### Architecture

```
cmd/server/main.go
    └── handler (HTTP request/response)
        └── service (business logic)
            └── repository (database access)
                └── models (domain types)
```

### Ledger logic

Every `POST /api/v1/reward` creates three ledger entries inside a single database transaction:

| Entry | Type | Debit | Credit |
|-------|------|-------|--------|
| Stock acquired | STOCK | — | price × quantity |
| Cash out | CASH | price × quantity + fees | — |
| Fees collected | FEE | — | total fees |

`Total Debit = Total Credit` is verified before committing; a mismatch rolls back the transaction.

### Fee calculation

Computed in [`pkg/fees/calculator.go`](pkg/fees/calculator.go):

```
Brokerage   = max(transaction_value × 0.03%, 20 INR)
STT         = transaction_value × 0.025%
GST         = brokerage × 18%
Exchange    = transaction_value × 0.00325%
SEBI        = transaction_value × 0.0001%
Stamp duty  = transaction_value × 0.003%
Total fees  = sum of all above
```

---

## Prerequisites

- Go 1.21+
- PostgreSQL 12+
- `make` (optional, for convenience commands)

---

## Installation

```bash
# 1. Clone
git clone <repo-url>
cd stock-rewards-portfolio-service

# 2. Generate go.sum and download dependencies
go mod tidy

# 3. Create and migrate the database
createdb assignment
psql assignment < migrations/001_initial_schema.sql

# 4. Configure environment
cp env.example .env
# Edit .env with your database credentials

# 5. Run
go run cmd/server/main.go
```

Or using `make`:

```bash
make setup    # go mod download
make migrate  # run migrations
make run      # start the server
```

---

## Configuration

Copy `env.example` to `.env` and set values:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `GIN_MODE` | `release` | Gin mode (`debug` / `release`) |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | Database user |
| `DB_PASSWORD` | `postgres` | Database password |
| `DB_NAME` | `assignment` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode |
| `PRICE_API_URL` | — | Reserved for a future real price API |
| `PRICE_FETCH_INTERVAL` | `1h` | How often the price fetcher runs |

---

## Usage

### Health check

```
GET /health
```

```json
{"status": "ok"}
```

### Record a stock reward

```
POST /api/v1/reward
Content-Type: application/json

{
  "user_id":      "550e8400-e29b-41d4-a716-446655440000",
  "stock_symbol": "RELIANCE",
  "quantity":     1.25,
  "timestamp":    "2025-01-15T10:30:00Z",
  "event_id":     "660e8400-e29b-41d4-a716-446655440000"
}
```

**201 Created**
```json
{"message": "Reward processed successfully", "event_id": "660e8400-..."}
```

Submitting the same `event_id` again returns 200 without creating a duplicate.

### Get today's rewards (IST)

```
GET /api/v1/today-stocks/{userId}
```

```json
[
  {"stock_symbol": "RELIANCE", "quantity": 1.25, "timestamp": "...", "event_id": "..."}
]
```

### Get 30-day historical INR valuation

```
GET /api/v1/historical-inr/{userId}
```

```json
[
  {"date": "2025-01-14", "inr_value": 15432.25},
  {"date": "2025-01-15", "inr_value": 16211.90}
]
```

### Get today's stats

```
GET /api/v1/stats/{userId}
```

```json
{
  "today_shares_by_stock":    {"RELIANCE": 1.25, "TCS": 0.5},
  "current_portfolio_value":  4375.00
}
```

### Get full portfolio holdings

```
GET /api/v1/portfolio/{userId}
```

```json
[
  {"stock_symbol": "RELIANCE", "total_quantity": 1.25, "current_price": 2500.00, "current_value": 3125.00}
]
```

A ready-to-import Postman collection is included at [`postman_collection.json`](postman_collection.json).

---

## Project Structure

```
stock-rewards-portfolio-service/
├── cmd/server/
│   └── main.go                     # entrypoint, router setup, graceful shutdown
├── internal/
│   ├── config/config.go            # env var loading
│   ├── database/postgres.go        # sqlx connection
│   ├── handler/
│   │   ├── portfolio_handler.go    # GET endpoints
│   │   └── reward_handler.go       # POST /reward
│   ├── middleware/logger.go        # request logging
│   ├── models/                     # domain types (reward, ledger, stock_price, user)
│   ├── repository/                 # database queries (one file per model)
│   ├── scheduler/price_fetcher.go  # background job
│   └── service/
│       ├── portfolio_service.go    # holdings & historical valuation
│       ├── price_service.go        # mock price generation
│       └── reward_service.go       # reward processing & ledger writes
├── migrations/
│   └── 001_initial_schema.sql      # tables, indexes, ledger audit function
├── pkg/fees/
│   └── calculator.go               # Indian-market fee formulas
├── env.example
├── Makefile
└── postman_collection.json
```

---

## Limitations

- **Mock price data** — the price service generates synthetic prices with ±5% random variation for five hardcoded symbols (RELIANCE, TCS, INFY, HDFCBANK, ICICIBANK). The `PRICE_API_URL` config key is loaded but no real market API is called; integrating a live feed is future work.
- **No authentication** — all API endpoints are publicly accessible; no API key, JWT, or session mechanism is implemented.
- **Fixed symbol set** — only the five symbols seeded in the price service can be used; rewards for other symbols will fail to find a price.
- **Manual migrations** — there is no migration-runner tool (e.g., golang-migrate); apply `migrations/001_initial_schema.sql` by hand with `psql`.
- **No `go.sum`** — the checksum file is absent from the repository. Run `go mod tidy` before building to generate it.
- **No tests** — no unit or integration tests are included in the current codebase.

---

## License

Released under the [MIT License](LICENSE).
