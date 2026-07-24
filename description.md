# tsx-tracker

## Overall Purpose

tsx-tracker is a Go-based microservice that maintains an up-to-date database of companies listed on the Toronto Stock Exchange (TSX). It automatically fetches, stores, and serves company data via a gRPC API. Every 24 hours it syncs the full TSX symbol list — adding new listings and removing delisted companies.

## System Description

### Data Collection & Discovery

The system discovers TSX-listed companies through the official TMX company directory — a free, public JSON API provided by the Toronto Stock Exchange itself. No API key is required. The service queries `https://www.tsx.com/json/company-directory/search/tsx/*` to get all TSX symbols in a single request.

### Background Refresher

A background process runs every 24 hours to:

1. **Discover new symbols** - Pulls the full TSX symbol list from TMX and inserts any unknown companies
2. **Prune delisted symbols** - Removes any companies from the database whose symbols are no longer on the TSX

### Data Storage

Company data is persisted in PostgreSQL with the following schema:

- **Primary key**: stock symbol (unique, case-insensitive lookups)
- **Fields**: name, exchange, currency, last_updated

### gRPC API

The service exposes a `CompanyService` with two RPCs:

1. **GetCompany** - Retrieves a single company by stock symbol (case-insensitive). Returns NOT_FOUND for untracked symbols.

2. **ListCompanies** - Returns a paginated list of companies with:
   - Keyset pagination (cursor-based via symbol)
   - Configurable page size (default: 50, max: 500)
   - Total count of companies

### Configuration

All runtime settings are configurable via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_PORT` | 50051 | gRPC server port |
| `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` | various | PostgreSQL connection |
| `REFRESH_CHECK_INTERVAL` | 24h | How often the symbol list is synced |

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
