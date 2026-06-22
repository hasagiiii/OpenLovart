package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultBraveBaseURL is the Brave Search API web-search endpoint.
const defaultBraveBaseURL = "https://api.search.brave.com/res/v1/web/search"

// defaultBraveCount caps the number of results requested per query.
const defaultBraveCount = 5

// braveProvider implements Provider against the Brave Search API.
type braveProvider struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	count      int
}

// BraveOption configures a braveProvider.
type BraveOption func(*braveProvider)

// WithBraveHTTPClient injects an HTTP client (test seam).
func WithBraveHTTPClient(c *http.Client) BraveOption {
	return func(p *braveProvider) { p.httpClient = c }
}

// WithBraveBaseURL overrides the Brave endpoint (test seam).
func WithBraveBaseURL(u string) BraveOption {
	return func(p *braveProvider) { p.baseURL = u }
}

// WithBraveCount sets how many results to request.
func WithBraveCount(n int) BraveOption {
	return func(p *braveProvider) {
		if n > 0 {
			p.count = n
		}
	}
}

// NewBraveProvider constructs a Brave Search provider keyed by apiKey.
func NewBraveProvider(apiKey string, opts ...BraveOption) Provider {
	p := &braveProvider{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    defaultBraveBaseURL,
		apiKey:     apiKey,
		count:      defaultBraveCount,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// braveResponse maps the subset of the Brave web-search payload we consume.
type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// Search implements Provider.
func (p *braveProvider) Search(ctx context.Context, query string) ([]Result, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("websearch: empty query")
	}

	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("q", q)
	params.Set("count", strconv.Itoa(p.count))
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("websearch: brave search failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	var out braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("websearch: decode brave response: %w", err)
	}

	results := make([]Result, 0, len(out.Web.Results))
	for _, r := range out.Web.Results {
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}
	return results, nil
}
