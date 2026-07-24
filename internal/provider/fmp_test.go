package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListSymbols_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stable/stock-list" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("exchange") != "TSX" {
			t.Errorf("unexpected exchange: %s", r.URL.Query().Get("exchange"))
		}
		if r.URL.Query().Get("apikey") != "test-key" {
			t.Errorf("unexpected apikey: %s", r.URL.Query().Get("apikey"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]symbolListEntry{
			{Symbol: "SHOP.TO", Name: "Shopify Inc.", Price: 85.50, Exchange: "TSX"},
			{Symbol: "RY.TO", Name: "Royal Bank", Price: 132.00, Exchange: "TSX"},
			{Symbol: "", Name: "Empty Symbol", Price: 1.0, Exchange: "TSX"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	symbols, err := c.ListSymbols(context.Background())
	if err != nil {
		t.Fatalf("ListSymbols: %v", err)
	}
	if len(symbols) != 2 {
		t.Fatalf("got %d symbols, want 2 (empty symbol filtered)", len(symbols))
	}
	if symbols[0].Symbol != "SHOP.TO" {
		t.Errorf("symbols[0].Symbol = %q, want %q", symbols[0].Symbol, "SHOP.TO")
	}
	if symbols[0].Name != "Shopify Inc." {
		t.Errorf("symbols[0].Name = %q, want %q", symbols[0].Name, "Shopify Inc.")
	}
	if symbols[0].Exchange != "TSX" {
		t.Errorf("symbols[0].Exchange = %q, want %q", symbols[0].Exchange, "TSX")
	}
	if symbols[0].Currency != "CAD" {
		t.Errorf("symbols[0].Currency = %q, want %q", symbols[0].Currency, "CAD")
	}
	if symbols[0].Price != 85.50 {
		t.Errorf("symbols[0].Price = %f, want 85.50", symbols[0].Price)
	}
}

func TestListSymbols_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	symbols, err := c.ListSymbols(context.Background())
	if err != nil {
		t.Fatalf("ListSymbols: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(symbols))
	}
}

func TestListSymbols_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListSymbols(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestListSymbols_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListSymbols(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 429")
	}
}

func TestProfiles_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stable/profile" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("symbol") != "SHOP.TO,RY.TO" {
			t.Errorf("unexpected symbol param: %s", r.URL.Query().Get("symbol"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]profileEntry{
			{
				Symbol:         "SHOP.TO",
				CompanyName:    "Shopify Inc.",
				Price:          85.50,
				MktCap:         109e9,
				Industry:       "Software",
				Sector:         "Technology",
				Website:        "https://shopify.com",
				Description:    "A commerce company",
				CEO:            "Tobi Lutke",
				City:           "Ottawa",
				State:          "Ontario",
				Country:        "Canada",
				FullTimeEmpStr: "20000",
				Currency:       "CAD",
				Exchange:       "TSX",
			},
			{
				Symbol:         "RY.TO",
				CompanyName:    "Royal Bank of Canada",
				Price:          132.00,
				MktCap:         190e9,
				Industry:       "Banks",
				Sector:         "Financial Services",
				Website:        "https://rbc.com",
				Description:    "A bank",
				CEO:            "Dave McKay",
				City:           "Toronto",
				State:          "Ontario",
				Country:        "Canada",
				FullTimeEmpStr: "80000",
				Currency:       "CAD",
				Exchange:       "TSX",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	profiles, err := c.Profiles(context.Background(), []string{"SHOP.TO", "RY.TO"})
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(profiles))
	}

	p := profiles[0]
	if p.Symbol != "SHOP.TO" {
		t.Errorf("Symbol = %q, want %q", p.Symbol, "SHOP.TO")
	}
	if p.Name != "Shopify Inc." {
		t.Errorf("Name = %q, want %q", p.Name, "Shopify Inc.")
	}
	if p.CEO != "Tobi Lutke" {
		t.Errorf("CEO = %q, want %q", p.CEO, "Tobi Lutke")
	}
	if p.Sector != "Technology" {
		t.Errorf("Sector = %q, want %q", p.Sector, "Technology")
	}
	if p.Headquarters != "Ottawa, Ontario, Canada" {
		t.Errorf("Headquarters = %q, want %q", p.Headquarters, "Ottawa, Ontario, Canada")
	}
	if p.Employees != 20000 {
		t.Errorf("Employees = %d, want 20000", p.Employees)
	}
	if p.MarketCap != 109e9 {
		t.Errorf("MarketCap = %f, want %f", p.MarketCap, 109e9)
	}
}

func TestProfiles_EmptySymbols(t *testing.T) {
	c := NewClient("http://unused", "key")
	profiles, err := c.Profiles(context.Background(), nil)
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if profiles != nil {
		t.Errorf("got %v, want nil for empty input", profiles)
	}
}

func TestProfiles_SkipsEmptyEmployees(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]profileEntry{
			{
				Symbol:         "TEST.TO",
				CompanyName:    "Test Co",
				FullTimeEmpStr: "",
				Currency:       "",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	profiles, err := c.Profiles(context.Background(), []string{"TEST.TO"})
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if profiles[0].Employees != 0 {
		t.Errorf("Employees = %d, want 0 for empty string", profiles[0].Employees)
	}
	if profiles[0].Currency != "CAD" {
		t.Errorf("Currency = %q, want fallback %q", profiles[0].Currency, "CAD")
	}
}

func TestParseEmployees(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"20000", 20000},
		{"0", 0},
		{"", 0},
		{"  ", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		if got := parseEmployees(tt.input); got != tt.want {
			t.Errorf("parseEmployees(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDefaultStr(t *testing.T) {
	tests := []struct {
		input, fallback, want string
	}{
		{"value", "fb", "value"},
		{"", "fb", "fb"},
		{"  ", "fb", "fb"},
	}
	for _, tt := range tests {
		if got := defaultStr(tt.input, tt.fallback); got != tt.want {
			t.Errorf("defaultStr(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
		}
	}
}

func TestJoinNonEmpty(t *testing.T) {
	tests := []struct {
		sep   string
		parts []string
		want  string
	}{
		{", ", []string{"A", "B", "C"}, "A, B, C"},
		{", ", []string{"A", "", "C"}, "A, C"},
		{", ", []string{"", "", ""}, ""},
		{", ", []string{}, ""},
	}
	for _, tt := range tests {
		if got := joinNonEmpty(tt.sep, tt.parts...); got != tt.want {
			t.Errorf("joinNonEmpty(%q, %v) = %q, want %q", tt.sep, tt.parts, got, tt.want)
		}
	}
}

func TestProfiles_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.Profiles(context.Background(), []string{"TEST.TO"})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestProfiles_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.Profiles(context.Background(), []string{"TEST.TO"})
	if err == nil {
		t.Fatal("expected error for HTTP 429")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should mention rate limiting: %v", err)
	}
}

func TestListSymbols_RateLimited_ErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListSymbols(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 429")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should mention rate limiting: %v", err)
	}
}

func TestProfiles_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json{{{"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.Profiles(context.Background(), []string{"TEST.TO"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestListSymbols_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[invalid"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListSymbols(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestListSymbols_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]symbolListEntry{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.ListSymbols(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestProfiles_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]profileEntry{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Profiles(ctx, []string{"TEST.TO"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestNewClient_Validation(t *testing.T) {
	// Empty base URL should still create a client (validation happens at request time)
	c := NewClient("", "key")
	if c == nil {
		t.Fatal("expected non-nil client")
	}

	// Empty API key should still create a client
	c = NewClient("http://localhost", "")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestListSymbols_EmptyResponse_AllSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// All symbols are empty
		json.NewEncoder(w).Encode([]symbolListEntry{
			{Symbol: "", Name: "Empty1", Price: 1.0, Exchange: "TSX"},
			{Symbol: "", Name: "Empty2", Price: 2.0, Exchange: "TSX"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	symbols, err := c.ListSymbols(context.Background())
	if err != nil {
		t.Fatalf("ListSymbols: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("got %d symbols, want 0 (all empty)", len(symbols))
	}
}

func TestProfiles_MissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a profile with most fields missing
		json.NewEncoder(w).Encode([]profileEntry{
			{
				Symbol:  "TEST.TO",
				Price:   10.0,
				MktCap:  1e6,
				City:    "Toronto",
				// No State, Country, FullTimeEmpStr, Currency
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	profiles, err := c.Profiles(context.Background(), []string{"TEST.TO"})
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(profiles))
	}

	p := profiles[0]
	if p.Headquarters != "Toronto" {
		t.Errorf("Headquarters = %q, want %q", p.Headquarters, "Toronto")
	}
	if p.Employees != 0 {
		t.Errorf("Employees = %d, want 0 for missing", p.Employees)
	}
	if p.Currency != "CAD" {
		t.Errorf("Currency = %q, want fallback %q", p.Currency, "CAD")
	}
}
