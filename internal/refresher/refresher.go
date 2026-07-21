// Package refresher runs a background loop that keeps tracked companies'
// data within the configured staleness threshold (<= 24h per the service
// requirement), while respecting the upstream free-tier API's rate limits
// by only refreshing a bounded number of companies per cycle.
package refresher

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/tsx-tracker/internal/config"
	"github.com/example/tsx-tracker/internal/db"
	"github.com/example/tsx-tracker/internal/provider"
)

type Refresher struct {
	cfg      *config.Config
	repo     *db.Repository
	provider *provider.Client
	log      *slog.Logger
}

func New(cfg *config.Config, repo *db.Repository, p *provider.Client, log *slog.Logger) *Refresher {
	return &Refresher{cfg: cfg, repo: repo, provider: p, log: log}
}

// Run blocks, performing an immediate sync and then repeating every
// cfg.RefreshCheckInterval, until ctx is cancelled.
func (r *Refresher) Run(ctx context.Context) {
	r.tick(ctx)

	ticker := time.NewTicker(r.cfg.RefreshCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Refresher) tick(ctx context.Context) {
	r.log.Info("refresh cycle starting")

	if err := r.discoverNewSymbols(ctx); err != nil {
		r.log.Error("discovering symbols failed", "error", err)
		// Continue anyway -- we can still refresh whatever's already tracked.
	}

	if err := r.refreshStale(ctx); err != nil {
		r.log.Error("refreshing stale companies failed", "error", err)
	}

	r.log.Info("refresh cycle complete")
}

// discoverNewSymbols pulls the current TSX symbol list and inserts stub
// rows (symbol/name/price only) for any symbol we don't already track,
// up to cfg.MaxTrackedCompanies. Stub rows get an epoch last_updated so
// they're picked up first by refreshStale.
func (r *Refresher) discoverNewSymbols(ctx context.Context) error {
	symbols, err := r.provider.ListSymbols(ctx)
	if err != nil {
		return err
	}

	if r.cfg.MaxTrackedCompanies > 0 {
		tracked, err := r.repo.TrackedCount(ctx)
		if err != nil {
			return err
		}
		room := r.cfg.MaxTrackedCompanies - tracked
		if room <= 0 {
			r.log.Info("tracked-company cap reached, skipping symbol discovery",
				"max_tracked_companies", r.cfg.MaxTrackedCompanies)
			return nil
		}
		if room < len(symbols) {
			symbols = symbols[:room]
		}
	}

	return r.repo.InsertSymbolStubs(ctx, symbols)
}

// refreshStale fetches full profile data for up to
// cfg.MaxCompaniesPerRefreshCycle companies whose data is older than
// cfg.StalenessThreshold, in batches of cfg.ProfileBatchSize per upstream
// call.
func (r *Refresher) refreshStale(ctx context.Context) error {
	cutoff := time.Now().Add(-r.cfg.StalenessThreshold)

	stale, err := r.repo.StaleSymbols(ctx, cutoff, r.cfg.MaxCompaniesPerRefreshCycle)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		r.log.Info("no stale companies found")
		return nil
	}
	r.log.Info("refreshing stale companies", "count", len(stale))

	batchSize := r.cfg.ProfileBatchSize
	if batchSize <= 0 {
		batchSize = 1
	}

	for i := 0; i < len(stale); i += batchSize {
		end := i + batchSize
		if end > len(stale) {
			end = len(stale)
		}
		batch := stale[i:end]

		profiles, err := r.provider.Profiles(ctx, batch)
		if err != nil {
			r.log.Error("fetching profile batch failed", "symbols", batch, "error", err)
			continue // don't let one bad batch abort the whole cycle
		}
		if err := r.repo.UpsertCompanies(ctx, profiles); err != nil {
			r.log.Error("upserting profile batch failed", "symbols", batch, "error", err)
			continue
		}
	}

	return nil
}
