package refresher_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/example/tsx-tracker/internal/config"
	"github.com/example/tsx-tracker/internal/db"
	"github.com/example/tsx-tracker/internal/provider"
	"github.com/example/tsx-tracker/internal/refresher"
)

func setupRepo(t *testing.T) *db.Repository {
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

	host, _ := container.Host(ctx)
	mappedPort, _ := container.MappedPort(ctx, "5432")
	dsn := "postgres://testuser:testpass@" + host + ":" + mappedPort.Port() + "/testdb?sslmode=disable"

	repo, err := db.NewRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := repo.Migrate(ctx, db.InitSchemaSQL()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() {
		repo.Close()
		container.Terminate(ctx)
	})

	return repo
}

func TestTick_WithRealRepo(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var listSymbolsCalled, profilesCalled bool

	fmp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/symbol/TSX":
			listSymbolsCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "SHOP.TO", "name": "Shopify", "price": 85.0, "exchange": "TSX"},
			})
		case "/api/v3/profile/SHOP.TO":
			profilesCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"symbol": "SHOP.TO", "companyName": "Shopify Inc.",
					"price": 85.0, "mktCap": 109e9, "sector": "Technology",
					"industry": "Software", "ceo": "Tobi Lutke",
					"description": "Commerce platform", "website": "https://shopify.com",
					"city": "Ottawa", "state": "Ontario", "country": "Canada",
					"fullTimeEmployees": "20000", "currency": "CAD",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fmp.Close()

	cfg := &config.Config{
		FMPAPIKey:                    "test",
		FMPBaseURL:                   fmp.URL,
		StalenessThreshold:           24 * time.Hour,
		RefreshCheckInterval:         time.Hour,
		MaxCompaniesPerRefreshCycle:  100,
		ProfileBatchSize:             3,
		MaxTrackedCompanies:          0,
	}

	p := provider.NewClient(fmp.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	go func() {
		ref.Run(ctx)
	}()

	time.Sleep(2 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	if !listSymbolsCalled {
		t.Error("ListSymbols was not called")
	}
	if !profilesCalled {
		t.Error("Profiles was not called")
	}

	vctx := context.Background()
	count, err := repo.TrackedCount(vctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("TrackedCount = %d, want 1", count)
	}

	c, err := repo.GetBySymbol(vctx, "SHOP.TO")
	if err != nil {
		t.Fatalf("GetBySymbol: %v", err)
	}
	if c.Name != "Shopify Inc." {
		t.Errorf("Name = %q, want Shopify Inc.", c.Name)
	}
	if c.CEO != "Tobi Lutke" {
		t.Errorf("CEO = %q, want Tobi Lutke", c.CEO)
	}
}

func TestTick_MaxTrackedCompaniesCap(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.UpsertCompanies(ctx, []db.Company{
		{Symbol: "A.TO", Name: "A", Exchange: "TSX", Price: 1, Currency: "CAD", LastUpdated: time.Now()},
		{Symbol: "B.TO", Name: "B", Exchange: "TSX", Price: 2, Currency: "CAD", LastUpdated: time.Now()},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var listSymbolsCalled bool
	fmp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/symbol/TSX" {
			listSymbolsCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "C.TO", "name": "New Corp", "price": 10.0, "exchange": "TSX"},
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fmp.Close()

	cfg := &config.Config{
		FMPAPIKey:                    "test",
		FMPBaseURL:                   fmp.URL,
		StalenessThreshold:           24 * time.Hour,
		RefreshCheckInterval:         time.Hour,
		MaxCompaniesPerRefreshCycle:  100,
		ProfileBatchSize:             3,
		MaxTrackedCompanies:          2,
	}

	p := provider.NewClient(fmp.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	go ref.Run(ctx)
	time.Sleep(2 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	if !listSymbolsCalled {
		t.Error("ListSymbols was not called")
	}

	vctx := context.Background()
	count, err := repo.TrackedCount(vctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("TrackedCount = %d, want 2 (cap enforced)", count)
	}
}

func TestTick_StaleRefresh(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.InsertSymbolStubs(ctx, []db.Company{
		{Symbol: "STALE.TO", Name: "Stale", Exchange: "TSX", Price: 1, Currency: "CAD"},
	}); err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	fmp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/symbol/TSX":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{})
		case "/api/v3/profile/STALE.TO":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"symbol": "STALE.TO", "companyName": "Stale Corp",
					"price": 5.0, "mktCap": 1e6, "sector": "Industrials",
					"industry": "Manufacturing", "ceo": "Bob",
					"description": "A stale company", "website": "https://stale.com",
					"city": "Calgary", "state": "Alberta", "country": "Canada",
					"fullTimeEmployees": "50", "currency": "CAD",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fmp.Close()

	cfg := &config.Config{
		FMPAPIKey:                    "test",
		FMPBaseURL:                   fmp.URL,
		StalenessThreshold:           24 * time.Hour,
		RefreshCheckInterval:         time.Hour,
		MaxCompaniesPerRefreshCycle:  100,
		ProfileBatchSize:             3,
		MaxTrackedCompanies:          0,
	}

	p := provider.NewClient(fmp.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	go ref.Run(ctx)
	time.Sleep(3 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	vctx := context.Background()
	c, err := repo.GetBySymbol(vctx, "STALE.TO")
	if err != nil {
		t.Fatalf("GetBySymbol: %v", err)
	}
	if c.Name != "Stale Corp" {
		t.Errorf("Name = %q, want Stale Corp (should be refreshed)", c.Name)
	}
	if c.CEO != "Bob" {
		t.Errorf("CEO = %q, want Bob", c.CEO)
	}
	if time.Since(c.LastUpdated) > 1*time.Minute {
		t.Errorf("LastUpdated = %v, should be recent", c.LastUpdated)
	}
}

func TestTick_ProfilesAPIError(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.InsertSymbolStubs(ctx, []db.Company{
		{Symbol: "ERR.TO", Name: "Error Corp", Exchange: "TSX", Price: 1, Currency: "CAD"},
	}); err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	fmp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/symbol/TSX":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{})
		case "/api/v3/profile/ERR.TO":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fmp.Close()

	cfg := &config.Config{
		FMPAPIKey:                    "test",
		FMPBaseURL:                   fmp.URL,
		StalenessThreshold:           24 * time.Hour,
		RefreshCheckInterval:         time.Hour,
		MaxCompaniesPerRefreshCycle:  100,
		ProfileBatchSize:             3,
		MaxTrackedCompanies:          0,
	}

	p := provider.NewClient(fmp.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	go ref.Run(ctx)
	time.Sleep(2 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	vctx := context.Background()
	count, err := repo.TrackedCount(vctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("TrackedCount = %d, want 1", count)
	}
}
