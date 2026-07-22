// Package refresher runs a background loop that keeps tracked companies'
// data within the configured refresh interval. Every cycle it syncs the
// full TSX symbol list (adding new symbols, removing delisted ones) and
// refreshes a random subset of eligible companies bounded by the
// configured daily refresh count.
package refresher

import (
	"context"
	"log/slog"
	"strings"
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

	symbols, err := r.discoverSymbols(ctx)
	if err != nil {
		r.log.Error("discovering symbols failed", "error", err)
	} else {
		if err := r.pruneDelisted(ctx, symbols); err != nil {
			r.log.Error("pruning delisted symbols failed", "error", err)
		}
	}

	if err := r.refreshRandom(ctx); err != nil {
		r.log.Error("refreshing random subset failed", "error", err)
	}

	r.log.Info("refresh cycle complete")
}

// discoverSymbols pulls the current TSX symbol list and inserts stub
// rows for any symbol we don't already track. Stub rows get an epoch
// last_updated so they're picked up first by refreshRandom.
func (r *Refresher) discoverSymbols(ctx context.Context) ([]string, error) {
	companies, err := r.provider.ListSymbols(ctx)
	if err != nil {
		return nil, err
	}

	if err := r.repo.InsertSymbolStubs(ctx, companies); err != nil {
		return nil, err
	}

	symbols := make([]string, len(companies))
	for i, c := range companies {
		symbols[i] = c.Symbol
	}
	return symbols, nil
}

// pruneDelisted removes any companies from the database whose symbols
// are not in the current TSX symbol list returned by the provider.
// Comparison is case-insensitive to prevent incorrect deletions from
// casing mismatches between the provider and the database.
func (r *Refresher) pruneDelisted(ctx context.Context, currentSymbols []string) error {
	current := make(map[string]struct{}, len(currentSymbols))
	for _, s := range currentSymbols {
		current[strings.ToUpper(s)] = struct{}{}
	}

	tracked, err := r.repo.AllSymbols(ctx)
	if err != nil {
		return err
	}

	var toDelete []string
	for _, s := range tracked {
		if _, ok := current[strings.ToUpper(s)]; !ok {
			toDelete = append(toDelete, s)
		}
	}

	if len(toDelete) == 0 {
		return nil
	}

	deleted, err := r.repo.DeleteBySymbols(ctx, toDelete)
	if err != nil {
		return err
	}
	r.log.Info("pruned delisted symbols", "count", deleted)
	return nil
}

// refreshRandom fetches full profile data for up to
// cfg.DailyRefreshCount companies whose data is older than
// cfg.RefreshCheckInterval, selected randomly to spread coverage
// evenly over time.
func (r *Refresher) refreshRandom(ctx context.Context) error {
	cutoff := time.Now().Add(-r.cfg.RefreshCheckInterval)

	stale, err := r.repo.StaleSymbols(ctx, cutoff, r.cfg.DailyRefreshCount)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		r.log.Info("no stale companies found")
		return nil
	}
	r.log.Info("refreshing random subset", "count", len(stale))

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
			continue
		}
		if err := r.repo.UpsertCompanies(ctx, profiles); err != nil {
			r.log.Error("upserting profile batch failed", "symbols", batch, "error", err)
			continue
		}
	}

	return nil
}
