package websearch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewDisabledWithoutKey(t *testing.T) {
	p, enabled := New(ProviderBrave, "")
	if enabled || p != nil {
		t.Fatalf("expected disabled provider when key absent, got enabled=%v p=%v", enabled, p)
	}
}

func TestNewEnabledWithKey(t *testing.T) {
	p, enabled := New(ProviderBrave, "key")
	if !enabled || p == nil {
		t.Fatalf("expected enabled provider when key present")
	}
}

func TestNewDefaultsToBraveForEmptyProvider(t *testing.T) {
	p, enabled := New("", "key")
	if !enabled || p == nil {
		t.Fatalf("empty provider with key should enable Brave")
	}
}

func TestBraveSearchMapsResults(t *testing.T) {
	var gotToken, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Subscription-Token")
		gotQuery = r.URL.Query().Get("q")
		io.WriteString(w, `{"web":{"results":[
			{"title":"Go","url":"https://go.dev","description":"The Go language"},
			{"title":"Brave","url":"https://brave.com","description":"Browser"}
		]}}`)
	}))
	defer srv.Close()

	p := NewBraveProvider("tok", WithBraveBaseURL(srv.URL), WithBraveHTTPClient(srv.Client()))
	results, err := p.Search(context.Background(), "golang")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "Go" || results[0].URL != "https://go.dev" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if gotToken != "tok" {
		t.Fatalf("token header = %q, want tok", gotToken)
	}
	if gotQuery != "golang" {
		t.Fatalf("query = %q, want golang", gotQuery)
	}
}

func TestBraveSearchEmptyQuery(t *testing.T) {
	p := NewBraveProvider("tok")
	if _, err := p.Search(context.Background(), "   "); err == nil {
		t.Fatalf("expected error for empty query")
	}
}

func TestBraveSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()

	p := NewBraveProvider("tok", WithBraveBaseURL(srv.URL), WithBraveHTTPClient(srv.Client()))
	if _, err := p.Search(context.Background(), "x"); err == nil {
		t.Fatalf("expected error on non-2xx")
	}
}
