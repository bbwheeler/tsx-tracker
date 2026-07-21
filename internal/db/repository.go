// Package db implements persistence for tracked companies against
// PostgreSQL. Postgres was chosen (over MongoDB/Cassandra) because company
// records are a fixed, relational schema with the need for fast exact-match
// lookups by symbol and simple filtered/paginated scans by sector -- a
// classic relational access pattern that doesn't benefit from a
// schemaless document store or a wide-column store built for massive
// write throughput.
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("company not found")

// Company is the internal domain representation of a tracked TSX company.
// The gRPC layer maps this to/from the generated protobuf message so that
// the persistence layer has no dependency on generated code.
type Company struct {
	Symbol       string
	Name         string
	Exchange     string
	Sector       string
	Industry     string
	CEO          string
	Description  string
	Website      string
	Headquarters string
	Employees    int64
	MarketCap    float64
	Price        float64
	Currency     string
	LastUpdated  time.Time
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(ctx context.Context, dsn string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() {
	r.pool.Close()
}

// Migrate applies the schema in migrationsSQL (typically read from the
// migrations/ directory at process startup). It's a minimal idempotent
// migrator suitable for this service; use a dedicated tool (golang-migrate,
// atlas, etc.) if the schema grows more complex.
func (r *Repository) Migrate(ctx context.Context, migrationSQL string) error {
	_, err := r.pool.Exec(ctx, migrationSQL)
	if err != nil {
		return fmt.Errorf("run migration: %w", err)
	}
	return nil
}

// UpsertCompanies inserts or updates a batch of companies in a single
// transaction, refreshing last_updated to now().
func (r *Repository) UpsertCompanies(ctx context.Context, companies []Company) error {
	if len(companies) == 0 {
		return nil
	}

	const colsPerRow = 13
	args := make([]any, 0, len(companies)*colsPerRow)
	var b strings.Builder
	b.WriteString(`INSERT INTO companies (
		symbol, name, exchange, sector, industry, ceo, description,
		website, headquarters, employees, market_cap, price, currency, last_updated
	) VALUES `)

	for i, c := range companies {
		if i > 0 {
			b.WriteByte(',')
		}
		n := i * colsPerRow
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d, now())",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9, n+10, n+11, n+12, n+13)
		args = append(args,
			c.Symbol, c.Name, c.Exchange, c.Sector, c.Industry, c.CEO,
			c.Description, c.Website, c.Headquarters, c.Employees,
			c.MarketCap, c.Price, c.Currency,
		)
	}

	b.WriteString(` ON CONFLICT (symbol) DO UPDATE SET
		name = EXCLUDED.name,
		exchange = EXCLUDED.exchange,
		sector = EXCLUDED.sector,
		industry = EXCLUDED.industry,
		ceo = EXCLUDED.ceo,
		description = EXCLUDED.description,
		website = EXCLUDED.website,
		headquarters = EXCLUDED.headquarters,
		employees = EXCLUDED.employees,
		market_cap = EXCLUDED.market_cap,
		price = EXCLUDED.price,
		currency = EXCLUDED.currency,
		last_updated = now()
	`)

	if _, err := r.pool.Exec(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("upsert companies: %w", err)
	}
	return nil
}

// InsertSymbolStubs ensures a row exists (with just symbol/name/exchange)
// for every given symbol, without touching existing rows. This lets the
// refresher discover "new to us" symbols from the cheap symbol-list
// endpoint immediately, and backfill full profile data over subsequent
// refresh cycles.
func (r *Repository) InsertSymbolStubs(ctx context.Context, stubs []Company) error {
	if len(stubs) == 0 {
		return nil
	}

	const colsPerRow = 5
	args := make([]any, 0, len(stubs)*colsPerRow)
	var b strings.Builder
	b.WriteString(`INSERT INTO companies (symbol, name, exchange, price, currency, last_updated) VALUES `)

	for i, s := range stubs {
		if i > 0 {
			b.WriteByte(',')
		}
		n := i * colsPerRow
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d, TIMESTAMPTZ '1970-01-01')", n+1, n+2, n+3, n+4, n+5)
		args = append(args, s.Symbol, s.Name, s.Exchange, s.Price, s.Currency)
	}
	b.WriteString(` ON CONFLICT (symbol) DO NOTHING`)

	if _, err := r.pool.Exec(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("insert stubs: %w", err)
	}
	return nil
}

// StaleSymbols returns up to `limit` symbols whose last_updated is older
// than `olderThan` (this includes brand-new stub rows, which are seeded
// with an epoch timestamp so they're always picked up first).
func (r *Repository) StaleSymbols(ctx context.Context, olderThan time.Time, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT symbol FROM companies WHERE last_updated < $1 ORDER BY last_updated ASC LIMIT $2`,
		olderThan, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query stale symbols: %w", err)
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		symbols = append(symbols, s)
	}
	return symbols, rows.Err()
}

// TrackedCount returns how many companies currently exist in the table.
func (r *Repository) TrackedCount(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM companies`).Scan(&n)
	return n, err
}

// GetBySymbol fetches a single company. Symbol match is case-insensitive.
func (r *Repository) GetBySymbol(ctx context.Context, symbol string) (*Company, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT symbol, name, exchange, sector, industry, ceo, description,
		       website, headquarters, employees, market_cap, price, currency, last_updated
		FROM companies WHERE upper(symbol) = upper($1)
	`, symbol)

	c, err := scanCompanyRows(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// List returns a page of companies ordered by symbol, optionally filtered
// by sector, using keyset pagination on symbol (afterSymbol is exclusive).
func (r *Repository) List(ctx context.Context, sectorFilter, afterSymbol string, pageSize int) ([]Company, error) {
	var (
		rows pgx.Rows
		err  error
	)

	base := `
		SELECT symbol, name, exchange, sector, industry, ceo, description,
		       website, headquarters, employees, market_cap, price, currency, last_updated
		FROM companies
	`
	var conds []string
	var args []any
	argN := 1

	if sectorFilter != "" {
		conds = append(conds, fmt.Sprintf("lower(sector) = lower($%d)", argN))
		args = append(args, sectorFilter)
		argN++
	}
	if afterSymbol != "" {
		conds = append(conds, fmt.Sprintf("symbol > $%d", argN))
		args = append(args, afterSymbol)
		argN++
	}
	if len(conds) > 0 {
		base += " WHERE " + strings.Join(conds, " AND ")
	}
	base += fmt.Sprintf(" ORDER BY symbol ASC LIMIT $%d", argN)
	args = append(args, pageSize)

	rows, err = r.pool.Query(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()

	var out []Company
	for rows.Next() {
		c, err := scanCompanyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CountBySector returns the total number of companies matching an optional
// sector filter (empty string = all companies). Used for total_count in
// list responses.
func (r *Repository) CountBySector(ctx context.Context, sectorFilter string) (int, error) {
	var n int
	var err error
	if sectorFilter == "" {
		err = r.pool.QueryRow(ctx, `SELECT count(*) FROM companies`).Scan(&n)
	} else {
		err = r.pool.QueryRow(ctx, `SELECT count(*) FROM companies WHERE lower(sector) = lower($1)`, sectorFilter).Scan(&n)
	}
	return n, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCompanyRows(row rowScanner) (*Company, error) {
	var c Company
	err := row.Scan(
		&c.Symbol, &c.Name, &c.Exchange, &c.Sector, &c.Industry, &c.CEO,
		&c.Description, &c.Website, &c.Headquarters, &c.Employees,
		&c.MarketCap, &c.Price, &c.Currency, &c.LastUpdated,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
