// Package provider fetches TSX ticker symbols from Finnhub's free API.
// Finnhub provides a stock-symbols-by-exchange endpoint that returns
// all listed symbols for a given exchange. This is the only data source
// the service needs — just an up-to-date list of TSX ticker symbols.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/example/tsx-tracker/internal/db"
)

var (
	ErrMissingAPIKey  = errors.New("API key is required")
	ErrMissingBaseURL = errors.New("base URL is required")
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type symbolEntry struct {
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// ListSymbols returns every stock symbol listed on the Toronto Stock
// Exchange (TSX). Finnhub uses exchange code "TO" for TSX.
func (c *Client) ListSymbols(ctx context.Context) ([]db.Company, error) {
	if c.apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	u := fmt.Sprintf("%s/stock/symbol?exchange=TO&token=%s", c.baseURL, url.QueryEscape(c.apiKey))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list TSX symbols: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited by upstream API (HTTP 429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream API returned HTTP %d", resp.StatusCode)
	}

	const maxBodySize = 10 << 20
	var entries []symbolEntry
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBodySize))
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	out := make([]db.Company, 0, len(entries))
	for _, e := range entries {
		if e.Symbol == "" {
			continue
		}
		out = append(out, db.Company{
			Symbol:   e.Symbol,
			Name:     e.Description,
			Exchange: "TSX",
			Currency: "CAD",
		})
	}
	return out, nil
}
