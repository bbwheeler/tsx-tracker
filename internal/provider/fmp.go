// Package provider fetches TSX company reference data from an external
// free API. Financial Modeling Prep (https://financialmodelingprep.com)
// was chosen because its free tier exposes both (a) a per-exchange symbol
// list endpoint and (b) a company "profile" endpoint containing CEO,
// sector, industry, market cap, employee count, description, website, and
// headquarters -- exactly what's needed here -- without requiring payment.
//
// The free tier is rate-limited (a small number of requests/day), so this
// client is designed to be called in small batches by the refresher rather
// than all at once; see internal/config for the tunable batch/cap knobs.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Validate checks that the client has the required configuration.
func (c *Client) Validate() error {
	if c.apiKey == "" {
		return ErrMissingAPIKey
	}
	if c.baseURL == "" {
		return ErrMissingBaseURL
	}
	return nil
}

type symbolListEntry struct {
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Exchange string  `json:"exchange"`
}

// ListSymbols returns every symbol FMP has for the TSX exchange, with
// just the cheap fields available from the list endpoint (symbol, name,
// price). Full details are fetched separately per-symbol via Profiles, so
// that the refresher can spread out and rate-limit the expensive calls.
func (c *Client) ListSymbols(ctx context.Context) ([]db.Company, error) {
	u := fmt.Sprintf("%s/stable/stock-list?exchange=TSX&apikey=%s", c.baseURL, url.QueryEscape(c.apiKey))

	var entries []symbolListEntry
	if err := c.getJSON(ctx, u, &entries); err != nil {
		return nil, fmt.Errorf("list TSX symbols: %w", err)
	}

	out := make([]db.Company, 0, len(entries))
	for _, e := range entries {
		if e.Symbol == "" {
			continue
		}
		out = append(out, db.Company{
			Symbol:   e.Symbol,
			Name:     e.Name,
			Exchange: "TSX",
			Price:    e.Price,
			Currency: "CAD",
		})
	}
	return out, nil
}

type profileEntry struct {
	Symbol         string  `json:"symbol"`
	CompanyName    string  `json:"companyName"`
	Price          float64 `json:"price"`
	MktCap         float64 `json:"mktCap"`
	Industry       string  `json:"industry"`
	Sector         string  `json:"sector"`
	Website        string  `json:"website"`
	Description    string  `json:"description"`
	CEO            string  `json:"ceo"`
	City           string  `json:"city"`
	State          string  `json:"state"`
	Country        string  `json:"country"`
	FullTimeEmpStr string  `json:"fullTimeEmployees"`
	Currency       string  `json:"currency"`
	Exchange       string  `json:"exchangeShortName"`
}

// Profiles fetches full company details for the given symbols. FMP
// supports comma-separated batches on this endpoint, so callers should
// chunk `symbols` to config.ProfileBatchSize before calling this to
// control request/response size and stay within rate limits.
func (c *Client) Profiles(ctx context.Context, symbols []string) ([]db.Company, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	joined := strings.Join(symbols, ",")
	u := fmt.Sprintf("%s/stable/profile?symbol=%s&apikey=%s", c.baseURL, url.QueryEscape(joined), url.QueryEscape(c.apiKey))

	var entries []profileEntry
	if err := c.getJSON(ctx, u, &entries); err != nil {
		return nil, fmt.Errorf("fetch profiles for %v: %w", symbols, err)
	}

	out := make([]db.Company, 0, len(entries))
	for _, e := range entries {
		out = append(out, db.Company{
			Symbol:       e.Symbol,
			Name:         e.CompanyName,
			Exchange:     "TSX",
			Sector:       e.Sector,
			Industry:     e.Industry,
			CEO:          e.CEO,
			Description:  e.Description,
			Website:      e.Website,
			Headquarters: joinNonEmpty(", ", e.City, e.State, e.Country),
			Employees:    parseEmployees(e.FullTimeEmpStr),
			MarketCap:    e.MktCap,
			Price:        e.Price,
			Currency:     defaultStr(e.Currency, "CAD"),
		})
	}
	return out, nil
}

func (c *Client) getJSON(ctx context.Context, u string, out any) error {
	if err := c.Validate(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited by upstream API (HTTP 429)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream API returned HTTP %d", resp.StatusCode)
	}

	const maxBodySize = 10 << 20 // 10 MiB
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBodySize))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func parseEmployees(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func defaultStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func joinNonEmpty(sep string, parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, sep)
}
