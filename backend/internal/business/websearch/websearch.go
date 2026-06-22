// Package websearch implements the backend's live web-search capability behind
// a provider interface. The default (and first) implementation is a Brave
// Search adapter. When no search credential is configured the capability is
// disabled and the chat agent's `web_search` tool is simply not registered.
package websearch

import "context"

// Result is a single search hit in the provider-agnostic shape fed back to the
// model.
type Result struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// Provider abstracts the underlying web-search vendor.
type Provider interface {
	// Search runs a web query and returns the top hits.
	Search(ctx context.Context, query string) ([]Result, error)
}

// Provider name constants recognised by New.
const (
	ProviderBrave = "brave"
)

// New constructs a Provider for the configured provider/key. The second return
// value reports whether web search is enabled: it is false (and the Provider is
// nil) when no API key is configured, signalling the caller to skip registering
// the `web_search` tool.
func New(provider, apiKey string, opts ...BraveOption) (Provider, bool) {
	if apiKey == "" {
		return nil, false
	}
	switch provider {
	case ProviderBrave, "":
		return NewBraveProvider(apiKey, opts...), true
	default:
		// Unknown provider with a key present: fall back to Brave so a
		// misconfigured name does not silently disable search.
		return NewBraveProvider(apiKey, opts...), true
	}
}
