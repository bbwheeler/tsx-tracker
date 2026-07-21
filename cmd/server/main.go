// Command tsx-tracker runs a service that keeps an up-to-date (<=24h
// stale) record of TSX-listed companies in PostgreSQL, and exposes them
// over gRPC.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/example/tsx-tracker/internal/config"
	"github.com/example/tsx-tracker/internal/db"
	"github.com/example/tsx-tracker/internal/grpcserver"
	"github.com/example/tsx-tracker/internal/provider"
	"github.com/example/tsx-tracker/internal/refresher"

	tsxv1 "github.com/example/tsx-tracker/gen/tsx/v1"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := db.NewRepository(ctx, cfg.PostgresDSN())
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer repo.Close()

	if err := repo.Migrate(ctx, db.InitSchemaSQL()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info("database ready")

	fmpClient := provider.NewClient(cfg.FMPBaseURL, cfg.FMPAPIKey)

	// Background loop that keeps company data within the staleness
	// threshold (<=24h). Runs an immediate sync on startup, then on
	// cfg.RefreshCheckInterval.
	ref := refresher.New(cfg, repo, fmpClient, log)
	go ref.Run(ctx)

	// gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.GRPCPort, err)
	}

	grpcSrv := grpc.NewServer()
	tsxv1.RegisterCompanyServiceServer(grpcSrv, grpcserver.New(repo, log))
	reflection.Register(grpcSrv) // enables grpcurl/evans without a local .proto copy

	go func() {
		<-ctx.Done()
		log.Info("shutting down gRPC server")
		grpcSrv.GracefulStop()
	}()

	log.Info("gRPC server listening", "port", cfg.GRPCPort)
	if err := grpcSrv.Serve(lis); err != nil {
		return fmt.Errorf("grpc serve: %w", err)
	}
	return nil
}
