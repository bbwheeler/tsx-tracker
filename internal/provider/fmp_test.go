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
		if r.URL.Path != "/stock/symbol" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("exchange") != "TO" {
			t.Errorf("unexpected exchange: %s", r.URL.Query().Get("exchange"))
		}
		if r.URL.Query().Get("token") != "test-key" {
			t.Errorf("unexpected token: %s", r.URL.Query().Get("token"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]symbolEntry{
			{Symbol: "SHOP", Description: "Shopify Inc.", Type: "Common Stock"},
			{Symbol: "RY", Description: "Royal Bank", Type: "Common Stock"},
			{Symbol: "", Description: "Empty Symbol", Type: "Common Stock"},
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
	if symbols[0].Symbol != "SHOP" {
		t.Errorf("symbols[0].Symbol = %q, want %q", symbols[0].Symbol, "SHOP")
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
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should mention rate limiting: %v", err)
	}
}

func TestListSymbols_EmptyAPIKey(t *testing.T) {
	c := NewClient("http://unused", "")
	_, err := c.ListSymbols(context.Background())
	if err == nil {
		t.Fatal("expected error for empty API key")
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
		json.NewEncoder(w).Encode([]symbolEntry{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.ListSymbols(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
