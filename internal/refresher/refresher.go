// Package refresher runs a background loop that keeps the tracked TSX
// symbol list up to date. Every cycle it syncs the full list (adding
// new symbols, removing delisted ones).
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

	r.log.Info("refresh cycle complete")
}

// discoverSymbols pulls the current TSX symbol list and inserts stub
// rows for any symbol we don't already track.
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
