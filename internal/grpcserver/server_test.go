package grpcserver_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/example/tsx-tracker/internal/db"
	"github.com/example/tsx-tracker/internal/grpcserver"

	tsxv1 "github.com/example/tsx-tracker/gen/tsx/v1"
)

func setupGRPC(t *testing.T) (tsxv1.CompanyServiceClient, func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("port: %v", err)
	}

	dsn := "postgres://testuser:testpass@" + host + ":" + port.Port() + "/testdb?sslmode=disable"
	repo, err := db.NewRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := repo.Migrate(ctx, db.InitSchemaSQL()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := grpc.NewServer()
	tsxv1.RegisterCompanyServiceServer(srv, grpcserver.New(repo, log))
	go srv.Serve(lis)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	cleanup := func() {
		conn.Close()
		srv.Stop()
		repo.Close()
		container.Terminate(ctx)
	}

	return tsxv1.NewCompanyServiceClient(conn), cleanup
}

func TestGetCompany_NotFound(t *testing.T) {
	client, cleanup := setupGRPC(t)
	defer cleanup()

	_, err := client.GetCompany(context.Background(), &tsxv1.GetCompanyRequest{
		Symbol: "NOPE",
	})
	if err == nil {
		t.Fatal("expected error for missing symbol")
	}
	if c := status.Code(err); c != codes.NotFound {
		t.Errorf("code = %v, want NotFound", c)
	}
}

func TestGetCompany_EmptySymbol(t *testing.T) {
	client, cleanup := setupGRPC(t)
	defer cleanup()

	_, err := client.GetCompany(context.Background(), &tsxv1.GetCompanyRequest{})
	if err == nil {
		t.Fatal("expected error for empty symbol")
	}
	if c := status.Code(err); c != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", c)
	}
}

func TestListCompanies_Defaults(t *testing.T) {
	client, cleanup := setupGRPC(t)
	defer cleanup()

	resp, err := client.ListCompanies(context.Background(), &tsxv1.ListCompaniesRequest{})
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	if resp.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", resp.TotalCount)
	}
	if len(resp.Companies) != 0 {
		t.Errorf("got %d companies, want 0", len(resp.Companies))
	}
	if resp.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want empty", resp.NextPageToken)
	}
}

func TestListCompanies_PageSizeBounds(t *testing.T) {
	client, cleanup := setupGRPC(t)
	defer cleanup()

	resp, err := client.ListCompanies(context.Background(), &tsxv1.ListCompaniesRequest{
		PageSize: 9999,
	})
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	_ = resp
}

// --- Full integration test with seeded data ---

func TestIntegration_GetAndList(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	t.Cleanup(func() { container.Terminate(ctx) })

	host, _ := container.Host(ctx)
	mappedPort, _ := container.MappedPort(ctx, "5432")
	dsn := "postgres://testuser:testpass@" + host + ":" + mappedPort.Port() + "/testdb?sslmode=disable"

	repo, err := db.NewRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer repo.Close()
	if err := repo.Migrate(ctx, db.InitSchemaSQL()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	companies := []db.Company{
		{Symbol: "AC", Name: "Air Canada", Exchange: "TSX", Currency: "CAD"},
		{Symbol: "ATD", Name: "Alimentation Couche-Tard", Exchange: "TSX", Currency: "CAD"},
		{Symbol: "BNS", Name: "Bank of Nova Scotia", Exchange: "TSX", Currency: "CAD"},
	}
	if err := repo.UpsertCompanies(ctx, companies); err != nil {
		t.Fatalf("seed: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcSrv := grpc.NewServer()
	tsxv1.RegisterCompanyServiceServer(grpcSrv, grpcserver.New(repo, log))
	go grpcSrv.Serve(lis)
	t.Cleanup(func() { grpcSrv.Stop() })

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client := tsxv1.NewCompanyServiceClient(conn)

	t.Run("GetCompany", func(t *testing.T) {
		resp, err := client.GetCompany(ctx, &tsxv1.GetCompanyRequest{Symbol: "ATD"})
		if err != nil {
			t.Fatalf("GetCompany: %v", err)
		}
		if resp.Company.Symbol != "ATD" {
			t.Errorf("Symbol = %q, want ATD", resp.Company.Symbol)
		}
		if resp.Company.Name != "Alimentation Couche-Tard" {
			t.Errorf("Name = %q, want Alimentation Couche-Tard", resp.Company.Name)
		}
		if resp.Company.Currency != "CAD" {
			t.Errorf("Currency = %q, want CAD", resp.Company.Currency)
		}
	})

	t.Run("GetCompany_CaseInsensitive", func(t *testing.T) {
		resp, err := client.GetCompany(ctx, &tsxv1.GetCompanyRequest{Symbol: "ac"})
		if err != nil {
			t.Fatalf("GetCompany: %v", err)
		}
		if resp.Company.Symbol != "AC" {
			t.Errorf("Symbol = %q, want AC", resp.Company.Symbol)
		}
	})

	t.Run("GetCompany_NotFound", func(t *testing.T) {
		_, err := client.GetCompany(ctx, &tsxv1.GetCompanyRequest{Symbol: "ZOO"})
		if c := status.Code(err); c != codes.NotFound {
			t.Errorf("code = %v, want NotFound", c)
		}
	})

	t.Run("GetCompany_EmptySymbol", func(t *testing.T) {
		_, err := client.GetCompany(ctx, &tsxv1.GetCompanyRequest{})
		if c := status.Code(err); c != codes.InvalidArgument {
			t.Errorf("code = %v, want InvalidArgument", c)
		}
	})

	t.Run("ListCompanies_All", func(t *testing.T) {
		resp, err := client.ListCompanies(ctx, &tsxv1.ListCompaniesRequest{PageSize: 10})
		if err != nil {
			t.Fatalf("ListCompanies: %v", err)
		}
		if resp.TotalCount != 3 {
			t.Errorf("TotalCount = %d, want 3", resp.TotalCount)
		}
		if len(resp.Companies) != 3 {
			t.Errorf("len = %d, want 3", len(resp.Companies))
		}
		if resp.NextPageToken != "" {
			t.Errorf("NextPageToken = %q, want empty", resp.NextPageToken)
		}
	})

	t.Run("ListCompanies_Pagination", func(t *testing.T) {
		resp, err := client.ListCompanies(ctx, &tsxv1.ListCompaniesRequest{PageSize: 2})
		if err != nil {
			t.Fatalf("ListCompanies: %v", err)
		}
		if len(resp.Companies) != 2 {
			t.Fatalf("page1 len = %d, want 2", len(resp.Companies))
		}
		if resp.NextPageToken == "" {
			t.Fatal("expected non-empty NextPageToken")
		}

		resp2, err := client.ListCompanies(ctx, &tsxv1.ListCompaniesRequest{
			PageSize:  2,
			PageToken: resp.NextPageToken,
		})
		if err != nil {
			t.Fatalf("ListCompanies page 2: %v", err)
		}
		if len(resp2.Companies) != 1 {
			t.Errorf("page2 len = %d, want 1", len(resp2.Companies))
		}
		if resp2.Companies[0].Symbol != "BNS" {
			t.Errorf("page2 Symbol = %q, want BNS", resp2.Companies[0].Symbol)
		}
		if resp2.NextPageToken != "" {
			t.Errorf("NextPageToken = %q, want empty (last page)", resp2.NextPageToken)
		}
	})

	t.Run("ListCompanies_PageSizeZero", func(t *testing.T) {
		resp, err := client.ListCompanies(ctx, &tsxv1.ListCompaniesRequest{
			PageSize: 0,
		})
		if err != nil {
			t.Fatalf("ListCompanies: %v", err)
		}
		// Should use default page size of 50
		if len(resp.Companies) != 3 {
			t.Errorf("len = %d, want 3", len(resp.Companies))
		}
	})

	t.Run("GetCompany_AllFieldsReturned", func(t *testing.T) {
		resp, err := client.GetCompany(ctx, &tsxv1.GetCompanyRequest{Symbol: "AC"})
		if err != nil {
			t.Fatalf("GetCompany: %v", err)
		}
		c := resp.Company
		if c.Symbol != "AC" {
			t.Errorf("Symbol = %q, want AC", c.Symbol)
		}
		if c.Name != "Air Canada" {
			t.Errorf("Name = %q, want Air Canada", c.Name)
		}
		if c.Exchange != "TSX" {
			t.Errorf("Exchange = %q, want TSX", c.Exchange)
		}
		if c.Currency != "CAD" {
			t.Errorf("Currency = %q, want CAD", c.Currency)
		}
	})

	t.Run("ListCompanies_TotalCountMatches", func(t *testing.T) {
		resp, err := client.ListCompanies(ctx, &tsxv1.ListCompaniesRequest{
			PageSize: 1,
		})
		if err != nil {
			t.Fatalf("ListCompanies: %v", err)
		}
		if resp.TotalCount != 3 {
			t.Errorf("TotalCount = %d, want 3", resp.TotalCount)
		}
		if len(resp.Companies) != 1 {
			t.Errorf("len = %d, want 1", len(resp.Companies))
		}
		if resp.NextPageToken == "" {
			t.Error("NextPageToken should not be empty")
		}
	})
}
