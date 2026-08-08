package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/anomalyco/stocker-list/internal/db"
)

func TestMain(m *testing.M) {
	m.Run()
}

func setupDB(t *testing.T) *db.Repository {
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
		t.Skipf("Docker not available or postgres container failed to start: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("get mapped port: %v", err)
	}

	dsn := "postgres://testuser:testpass@" + host + ":" + port.Port() + "/testdb?sslmode=disable"
	repo, err := db.NewRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
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

func sampleCompany(symbol string) db.Company {
	return db.Company{
		Symbol:   symbol,
		Name:     symbol + " Corp",
		Exchange: "TSX",
		Currency: "CAD",
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.Migrate(ctx, db.InitSchemaSQL()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestUpsertCompanies_Insert(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	companies := []db.Company{
		sampleCompany("SHOP.TO"),
		sampleCompany("RY.TO"),
	}

	if err := repo.UpsertCompanies(ctx, companies); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	count, err := repo.TrackedCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestUpsertCompanies_Update(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	c := sampleCompany("SHOP.TO")
	if err := repo.UpsertCompanies(ctx, []db.Company{c}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	c.Name = "Shopify Inc. Updated"
	if err := repo.UpsertCompanies(ctx, []db.Company{c}); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	got, err := repo.GetBySymbol(ctx, "SHOP.TO")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Shopify Inc. Updated" {
		t.Errorf("Name = %q, want %q", got.Name, "Shopify Inc. Updated")
	}
}

func TestUpsertCompanies_EmptySlice(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.UpsertCompanies(ctx, nil); err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
}

func TestInsertSymbolStubs(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	stubs := []db.Company{
		{Symbol: "A.TO", Name: "A Corp", Exchange: "TSX", Currency: "CAD"},
		{Symbol: "B.TO", Name: "B Corp", Exchange: "TSX", Currency: "CAD"},
	}
	if err := repo.InsertSymbolStubs(ctx, stubs); err != nil {
		t.Fatalf("insert stubs: %v", err)
	}

	count, err := repo.TrackedCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestInsertSymbolStubs_NoOverwrite(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	full := sampleCompany("A.TO")
	full.Name = "Full Company"
	if err := repo.UpsertCompanies(ctx, []db.Company{full}); err != nil {
		t.Fatalf("upsert full: %v", err)
	}

	stub := db.Company{Symbol: "A.TO", Name: "Stub", Exchange: "TSX", Currency: "CAD"}
	if err := repo.InsertSymbolStubs(ctx, []db.Company{stub}); err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	got, err := repo.GetBySymbol(ctx, "A.TO")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Full Company" {
		t.Errorf("Name = %q, want %q (stub should not overwrite)", got.Name, "Full Company")
	}
}

func TestGetBySymbol(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	c := sampleCompany("SHOP.TO")
	if err := repo.UpsertCompanies(ctx, []db.Company{c}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.GetBySymbol(ctx, "SHOP.TO")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Symbol != "SHOP.TO" {
		t.Errorf("Symbol = %q, want %q", got.Symbol, "SHOP.TO")
	}
	if got.Currency != "CAD" {
		t.Errorf("Currency = %q, want CAD", got.Currency)
	}
}

func TestGetBySymbol_CaseInsensitive(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.UpsertCompanies(ctx, []db.Company{sampleCompany("SHOP.TO")}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.GetBySymbol(ctx, "shop.to")
	if err != nil {
		t.Fatalf("get lowercase: %v", err)
	}
	if got.Symbol != "SHOP.TO" {
		t.Errorf("Symbol = %q, want %q", got.Symbol, "SHOP.TO")
	}
}

func TestGetBySymbol_NotFound(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	_, err := repo.GetBySymbol(ctx, "NOPE.TO")
	if err != db.ErrNotFound {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestList_Pagination(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	var companies []db.Company
	for i := 0; i < 5; i++ {
		companies = append(companies, sampleCompany(
			string(rune('A'+i))+".TO",
		))
	}
	if err := repo.UpsertCompanies(ctx, companies); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	page1, err := repo.List(ctx, "", 3)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len = %d, want 3", len(page1))
	}
	if page1[0].Symbol != "A.TO" || page1[2].Symbol != "C.TO" {
		t.Errorf("page1 symbols = [%s %s %s], want [A.TO B.TO C.TO]",
			page1[0].Symbol, page1[1].Symbol, page1[2].Symbol)
	}

	page2, err := repo.List(ctx, page1[len(page1)-1].Symbol, 3)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
	if page2[0].Symbol != "D.TO" {
		t.Errorf("page2[0].Symbol = %q, want %q", page2[0].Symbol, "D.TO")
	}
}

func TestList_Empty(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	results, err := repo.List(ctx, "", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestTrackedCount(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	count, err := repo.TrackedCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	if err := repo.UpsertCompanies(ctx, []db.Company{
		sampleCompany("A.TO"),
		sampleCompany("B.TO"),
		sampleCompany("C.TO"),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	count, err = repo.TrackedCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestDeleteBySymbols(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.UpsertCompanies(ctx, []db.Company{
		sampleCompany("A.TO"),
		sampleCompany("B.TO"),
		sampleCompany("C.TO"),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	deleted, err := repo.DeleteBySymbols(ctx, []string{"A.TO", "C.TO"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	count, err := repo.TrackedCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	c, err := repo.GetBySymbol(ctx, "B.TO")
	if err != nil {
		t.Fatalf("GetBySymbol: %v", err)
	}
	if c.Symbol != "B.TO" {
		t.Errorf("Symbol = %q, want B.TO", c.Symbol)
	}
}

func TestDeleteBySymbols_Empty(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.UpsertCompanies(ctx, []db.Company{sampleCompany("A.TO")}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	deleted, err := repo.DeleteBySymbols(ctx, nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}

	count, err := repo.TrackedCount(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestAllSymbols(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.UpsertCompanies(ctx, []db.Company{
		sampleCompany("A.TO"),
		sampleCompany("B.TO"),
		sampleCompany("C.TO"),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	symbols, err := repo.AllSymbols(ctx)
	if err != nil {
		t.Fatalf("AllSymbols: %v", err)
	}
	if len(symbols) != 3 {
		t.Fatalf("got %d symbols, want 3", len(symbols))
	}

	set := make(map[string]bool)
	for _, s := range symbols {
		set[s] = true
	}
	for _, want := range []string{"A.TO", "B.TO", "C.TO"} {
		if !set[want] {
			t.Errorf("missing symbol %q", want)
		}
	}
}

func TestAllSymbols_Empty(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	symbols, err := repo.AllSymbols(ctx)
	if err != nil {
		t.Fatalf("AllSymbols: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(symbols))
	}
}

func TestCount(t *testing.T) {
	repo := setupDB(t)
	ctx := context.Background()

	if err := repo.UpsertCompanies(ctx, []db.Company{
		sampleCompany("A.TO"),
		sampleCompany("B.TO"),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	total, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}
