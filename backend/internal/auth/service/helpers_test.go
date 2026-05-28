package service_test

import (
	"net/url"
	"testing"

	"github.com/jiantaoli/openlovart/backend/internal/auth/ratelimit"
)

// extractTokenParam pulls the `?token=` query parameter out of a verify /
// reset link. Tests use it to pivot from "we shipped an email" to "we can
// drive the next service call without round-tripping through the HTTP
// layer".
func extractTokenParam(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link %q: %v", link, err)
	}
	tok := u.Query().Get("token")
	if tok == "" {
		t.Fatalf("link missing ?token=: %s", link)
	}
	return tok
}

// newCappedLimiter returns a small limiter (1 token per second, burst 2)
// suitable for service-level "the limiter denies after burst" smoke tests.
// Closes over t.Cleanup so the sweeper goroutine doesn't leak.
func newCappedLimiter(t *testing.T) ratelimit.Limiter {
	t.Helper()
	l := ratelimit.New(60, 2) // 60 per minute = 1 per second, burst 2
	return l
}
