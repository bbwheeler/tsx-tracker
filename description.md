# tsx-tracker

## Overall Purpose

tsx-tracker is a Go-based microservice that maintains an up-to-date database of companies listed on the Toronto Stock Exchange (TSX). It automatically fetches, stores, and serves company profile data (CEO, sector, industry, market cap, financials, etc.) via a gRPC API. Every 24 hours it syncs the full TSX symbol list (adding new listings, removing delisted companies) and refreshes a random subset of companies with complete profile data.

## System Description

### Data Collection & Discovery

The system discovers TSX-listed companies through the Financial Modeling Prep (FMP) free-tier API. It automatically identifies new symbols and creates "stub" records containing basic information (symbol, name, price). Stub rows are prioritized for full profile enrichment because they have an epoch `last_updated` timestamp, making them always eligible for refresh.

### Background Refresher

A background process runs every 24 hours to:

1. **Discover new symbols** - Pulls the full TSX symbol list from FMP and inserts any unknown companies as stub rows
2. **Prune delisted symbols** - Removes any companies from the database whose symbols are no longer on the TSX
3. **Refresh a random subset** - Selects a random sample of companies that haven't been refreshed within the last 24 hours (bounded by the daily refresh count) and fetches their complete profile data in rate-limited batches
4. **Handle API limits** - Respects FMP's free-tier rate limits by:
   - Limiting profile fetches per cycle (default: 50)
   - Batching requests (default: 3 symbols per API call)

Over multiple days, the random selection ensures all companies eventually get refreshed.

### Data Storage

Company data is persisted in PostgreSQL with the following schema:

- **Primary key**: stock symbol (unique, case-insensitive lookups)
- **Indexed fields**: sector (for filtered queries), last_updated (for staleness checks)
- **Company fields**: name, exchange, sector, industry, CEO, description, website, headquarters, employees, market_cap, price, currency, last_updated

### gRPC API

The service exposes a `CompanyService` with two RPCs:

1. **GetCompany** - Retrieves a single company by stock symbol (case-insensitive). Returns NOT_FOUND for untracked symbols.

2. **ListCompanies** - Returns a paginated list of companies with:
   - Keyset pagination (cursor-based via symbol)
   - Configurable page size (default: 50, max: 500)
   - Optional sector filtering (case-insensitive exact match)
   - Total count of matching companies

### Configuration

All runtime settings are configurable via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_PORT` | 50051 | gRPC server port |
| `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` | various | PostgreSQL connection |
| `FMP_API_KEY` | required | Financial Modeling Prep API key |
| `REFRESH_CHECK_INTERVAL` | 24h | How often the symbol list is synced and companies are refreshed |
| `DAILY_REFRESH_COUNT` | 50 | Random companies refreshed per cycle |
| `PROFILE_BATCH_SIZE` | 3 | Symbols per upstream API call |

### Deployment

- Runs as a single Go binary
- Includes container support (Containerfile, docker-compose.yml)
- Generates gRPC code via protobuf/buf
- Supports graceful shutdown via OS signals
- Includes gRPC reflection for tooling compatibility

### Error Handling

- Continues operation if individual refresh cycles fail
- Logs errors without crashing the service
- Returns appropriate gRPC status codes (InvalidArgument, NotFound, Internal)
