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

func testConfig(finnhubURL string) *config.Config {
	return &config.Config{
		FinnhubAPIKey:        "test",
		FinnhubBaseURL:       finnhubURL,
		RefreshCheckInterval: 24 * time.Hour,
	}
}

func TestTick_WithRealRepo(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var listSymbolsCalled bool

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			listSymbolsCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "SHOP", "description": "Shopify Inc.", "type": "Common Stock"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
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
	if count != 1 {
		t.Errorf("TrackedCount = %d, want 1", count)
	}

	c, err := repo.GetBySymbol(vctx, "SHOP")
	if err != nil {
		t.Fatalf("GetBySymbol: %v", err)
	}
	if c.Name != "Shopify Inc." {
		t.Errorf("Name = %q, want Shopify Inc.", c.Name)
	}
}

func TestTick_PruneDelisted(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.UpsertCompanies(ctx, []db.Company{
		{Symbol: "A", Name: "Active Corp", Exchange: "TSX", Price: 10, Currency: "CAD", LastUpdated: time.Now()},
		{Symbol: "B", Name: "Delisted Corp", Exchange: "TSX", Price: 5, Currency: "CAD", LastUpdated: time.Now()},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "A", "description": "Active Corp", "type": "Common Stock"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	go ref.Run(ctx)
	time.Sleep(2 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	vctx := context.Background()

	_, err := repo.GetBySymbol(vctx, "B")
	if err != db.ErrNotFound {
		t.Errorf("B should have been pruned, got err = %v", err)
	}

	c, err := repo.GetBySymbol(vctx, "A")
	if err != nil {
		t.Fatalf("A should still exist: %v", err)
	}
	if c.Name != "Active Corp" {
		t.Errorf("Name = %q, want Active Corp", c.Name)
	}
}

func TestTick_SymbolsAPIError(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
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
	if count != 0 {
		t.Errorf("TrackedCount = %d, want 0", count)
	}
}

func TestTick_ContextCancellation(t *testing.T) {
	repo := setupRepo(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		ref.Run(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestTick_MultipleSymbols(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "SHOP", "description": "Shopify Inc.", "type": "Common Stock"},
				{"symbol": "RY", "description": "Royal Bank", "type": "Common Stock"},
				{"symbol": "TD", "description": "Toronto-Dominion Bank", "type": "Common Stock"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
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
	if count != 3 {
		t.Errorf("TrackedCount = %d, want 3", count)
	}

	for _, sym := range []string{"SHOP", "RY", "TD"} {
		_, err := repo.GetBySymbol(vctx, sym)
		if err != nil {
			t.Errorf("GetBySymbol(%s): %v", sym, err)
		}
	}
}

func TestTick_IdempotentSymbols(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.InsertSymbolStubs(ctx, []db.Company{
		{Symbol: "EXISTING", Name: "Existing Corp", Exchange: "TSX", Price: 10, Currency: "CAD", LastUpdated: time.Now()},
	}); err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "EXISTING", "description": "Existing Corp", "type": "Common Stock"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
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
		t.Errorf("TrackedCount = %d, want 1 (should not duplicate)", count)
	}
}

func TestTick_SymbolSync_CaseInsensitive(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.InsertSymbolStubs(ctx, []db.Company{
		{Symbol: "SYM", Name: "Symbol Corp", Exchange: "TSX", Price: 10, Currency: "CAD"},
	}); err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "sym", "description": "Symbol Corp", "type": "Common Stock"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
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

func TestTick_SymbolSync_EmptyList(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.InsertSymbolStubs(ctx, []db.Company{
		{Symbol: "WILL_BE_GONE", Name: "Gone Corp", Exchange: "TSX", Price: 10, Currency: "CAD"},
	}); err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
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
	if count != 0 {
		t.Errorf("TrackedCount = %d, want 0", count)
	}
}

func TestTick_SymbolSync_PrunesAndAdds(t *testing.T) {
	repo := setupRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := repo.UpsertCompanies(ctx, []db.Company{
		{Symbol: "KEEP", Name: "Keep Corp", Exchange: "TSX", Price: 10, Currency: "CAD", LastUpdated: time.Now()},
		{Symbol: "DROP", Name: "Drop Corp", Exchange: "TSX", Price: 20, Currency: "CAD", LastUpdated: time.Now()},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stock/symbol" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "KEEP", "description": "Keep Corp", "type": "Common Stock"},
				{"symbol": "ADD", "description": "Add Corp", "type": "Common Stock"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer finnhub.Close()

	cfg := testConfig(finnhub.URL)
	p := provider.NewClient(finnhub.URL, "test")
	ref := refresher.New(cfg, repo, p, log)

	go ref.Run(ctx)
	time.Sleep(2 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	vctx := context.Background()

	_, err := repo.GetBySymbol(vctx, "DROP")
	if err != db.ErrNotFound {
		t.Errorf("DROP should have been pruned, got err = %v", err)
	}

	count, err := repo.TrackedCount(vctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("TrackedCount = %d, want 2", count)
	}

	for _, sym := range []string{"KEEP", "ADD"} {
		_, err := repo.GetBySymbol(vctx, sym)
		if err != nil {
			t.Errorf("GetBySymbol(%s): %v", sym, err)
		}
	}
}
