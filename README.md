# tsx-tracker

A Go service that tracks companies listed on the Toronto Stock Exchange
(TSX) in a PostgreSQL database, keeps their data fresh, and exposes it
over gRPC. Every 24 hours it syncs the full TSX symbol list — adding new
listings and removing delisted companies.

## Architecture

```
cmd/server/main.go        wires everything together, starts the gRPC server
internal/config           env-var configuration (DB, refresh cadence)
internal/db               Postgres repository (schema, upsert, query, pagination)
internal/provider         TMX company directory client (TSX symbol list, no API key)
internal/refresher        background loop syncing symbols + pruning delisted
internal/grpcserver       gRPC service implementation
proto/tsx/v1/tsx.proto    gRPC API definition
gen/tsx/v1                generated protobuf/gRPC Go code (run `make proto` first)
migrations/0001_init.sql  Postgres schema (embedded in internal/db/)
```

### Why PostgreSQL

The task allows Cassandra, MongoDB, or Postgres. Company records here are a
fixed, relational schema (symbol, name, exchange, currency) with one
access pattern: exact-match lookup by primary key (symbol). That's a
textbook relational workload — Postgres gives strong consistency, indexed
lookups, and simple upserts (`ON CONFLICT`) with far less operational
overhead than running Cassandra (built for massive write throughput across
many nodes) or MongoDB (best when the schema is genuinely variable/nested)
for this use case. The DB connection is fully configurable via env vars
(`DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`).

### Where the data comes from

The service uses the **official TMX company directory** — a free, public
JSON API provided by the Toronto Stock Exchange itself. No API key is
required.

The endpoint `https://www.tsx.com/json/company-directory/search/tsx/*`
returns all TSX-listed companies in a single request. The service queries
this once per sync cycle to build the complete symbol list.

Every cycle the full TSX symbol list is synced — new listings are added
and delisted symbols are removed.

## Running it

### 1. Generate the gRPC code
```
cp .env.example .env   # edit DB credentials
make proto             # requires buf (https://buf.build), or:
make proto-protoc      # requires local protoc + protoc-gen-go + protoc-gen-go-grpc
```

### 3a. Run with Docker Compose (recommended)
```
export DB_USER=postgres
export DB_PASSWORD=your_password
make docker-up
```
This starts Postgres and the service together; the Containerfile generates
the gRPC code and builds the binary in a multi-stage build.

### 3b. Deploy with Podman (Debian Linux)

Install Podman:
```
sudo apt update
sudo apt install -y podman podman-compose
```

Build and run:
```
export DB_USER=postgres
export DB_PASSWORD=your_password
podman-compose up -d --build
```

To stop and remove:
```
podman-compose down -v
```

To view logs:
```
podman-compose logs -f
```

### 3c. Deploy with Podman Quadlet (rootless, Debian)

Quadlet lets you manage Podman containers as systemd user services — the
container starts on boot without root.

**Prerequisites — install Podman and set up lingering:**
```
sudo apt update
sudo apt install -y podman systemd-container
sudo loginctl enable-linger $USER
```

Lingering lets systemd user services (including this container) run
without an active login session.

**External Postgres:** The service connects to a PostgreSQL instance
configured via environment variables. Make sure a Postgres server is
reachable at the host/port you specify in the env file (default
`DB_HOST=192.168.1.31:5432`). The database `tsx_tracker` must exist.

**1. Clone the repo and install:**
```
git clone https://github.com/youruser/tsx-tracker.git ~/tsx-tracker
cd ~/tsx-tracker
make quadlet-install
```

This copies the Quadlet unit file to `~/.config/containers/systemd/`,
installs the env file to `~/.config/tsx-tracker/.env.podman`, and runs
`systemctl --user daemon-reload`.

**2. Edit the environment file** with your credentials:
```
$EDITOR ~/.config/tsx-tracker/.env.podman
```

Fill in `DB_USER` and `DB_PASSWORD`. See
`.env.podman` in the repo for all available settings.

**3. Build the container image:**
```
make quadlet-build
```

Or equivalently:
```
podman build -t localhost/tsx-tracker:latest .
```

**4. Start the service:**
```
systemctl --user start tsx-tracker.service
```

The `WantedBy=default.target` in the `.container` file ensures the
service starts automatically on boot.

**5. Check status and logs:**
```
systemctl --user status tsx-tracker
podman logs tsx-tracker
```

**Stopping:**
```
systemctl --user stop tsx-tracker
```

To prevent auto-start on boot, remove the wants symlink:
```
rm ~/.config/systemd/user/default.target.wants/tsx-tracker.service
systemctl --user daemon-reload
```

**Updating:** pull new code, rebuild, and restart:
```
cd ~/tsx-tracker
git pull
make quadlet-build
systemctl --user restart tsx-tracker
```

### 3d. Run locally
```
# start a local Postgres, then:
export $(cat .env | xargs)
make run
```

The service listens on `:50051` (configurable via `GRPC_PORT`) and
registers gRPC reflection, so you can explore/call it with `grpcurl`
without needing the `.proto` file locally:

```
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 tsx.v1.CompanyService/ListCompanies
grpcurl -plaintext -d '{"symbol": "SHOP.TO"}' localhost:50051 tsx.v1.CompanyService/GetCompany
```

## gRPC API

```protobuf
service CompanyService {
  rpc ListCompanies(ListCompaniesRequest) returns (ListCompaniesResponse);
  rpc GetCompany(GetCompanyRequest) returns (GetCompanyResponse);
}
```

- `ListCompanies` — paginated (keyset pagination via `page_token`, default
  page size 50, max 500).
- `GetCompany` — fetch one company by `symbol` (case-insensitive). Returns
  a `NOT_FOUND` gRPC status if the symbol isn't tracked.

See `proto/tsx/v1/tsx.proto` for full message definitions, including the
`Company` message (symbol, name, exchange, currency).

## Notes / next steps for production use

- This ships a minimal `Migrate()` that runs the schema SQL idempotently
  on startup; for a real production system, use a proper migration tool
  (golang-migrate, atlas, etc.) once the schema evolves.
- Add TLS/auth to the gRPC server before exposing it outside a trusted
  network — it currently runs in plaintext for simplicity.
- Consider adding the standard gRPC health-checking protocol
  (`grpc_health_v1`) for orchestrator liveness/readiness probes.
- `go.mod` lists direct dependencies; run `make tidy` (`go mod tidy`) after
  generating the proto code to resolve exact versions and populate
  `go.sum` for your environment.
