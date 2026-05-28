package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	"github.com/jiantaoli/openlovart/backend/internal/auth/ratelimit"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/config"
	"github.com/jiantaoli/openlovart/backend/internal/httpserver"
	"github.com/jiantaoli/openlovart/backend/internal/logging"
	"github.com/jiantaoli/openlovart/backend/internal/testutil"
)

// stack is the bundle every handler test consumes. The router is the
// real production engine — same middleware order, same handler wiring —
// just with an in-memory limiter (or none) and a fake mailer.
type stack struct {
	router *gin.Engine
	cfg    *config.Config
	cookie *cookies.Manager
	ks     *jwt.KeyStore
	svc    *service.Service
	mailer *testutil.FakeMailer
	db     *gorm.DB
}

func newStack(t *testing.T, opts ...stackOpt) *stack {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := testutil.DevConfig()
	gdb := testutil.PostgresDB(t)
	ks := testutil.FreshKeyStore(t, cfg)
	mailer := testutil.NewFakeMailer()
	svc := service.New(gdb, cfg, ks, mailer, nil)
	cookie := cookies.New(cfg)
	logger := logging.New(cfg)

	deps := httpserver.Deps{
		Cfg:         cfg,
		Log:         logger,
		DB:          gdb,
		KeyStore:    ks,
		AuthService: svc,
		Cookies:     cookie,
		// Default: limiters disabled. Individual tests can override via opts.
	}
	for _, o := range opts {
		o(&deps)
	}
	router := httpserver.New(deps)
	return &stack{router: router, cfg: cfg, cookie: cookie, ks: ks, svc: svc, mailer: mailer, db: gdb}
}

type stackOpt func(*httpserver.Deps)

// withLoginLimiter installs a tight limiter so the rate-limit case can
// hit it deterministically.
func withLoginLimiter(perMin float64, burst int) stackOpt {
	return func(d *httpserver.Deps) {
		d.LoginLimiter = ratelimit.New(perMin, burst)
	}
}

// ---------- helpers ------------------------------------------------------

// jsonReq builds a JSON request without setting any cookies.
func jsonReq(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// do runs req through the router and returns the captured response.
func (s *stack) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// register is a tiny convenience that performs the full register flow and
// returns the cookies the server set, so subsequent tests can re-use them.
func (s *stack) register(t *testing.T, email, pw string) []*http.Cookie {
	t.Helper()
	rec := s.do(jsonReq(http.MethodPost, "/api/auth/register", map[string]string{
		"email":    email,
		"password": pw,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("register: status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()
}

// findCookie returns the first cookie whose Name matches.
func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// attachCookies copies all Cookies from the slice onto req.
func attachCookies(req *http.Request, ck []*http.Cookie) {
	for _, c := range ck {
		req.AddCookie(c)
	}
}

// ---------- 1) CSRF rejection on unsafe cookie-auth request --------------

func TestCSRFRejectionOnUnsafeCookieRequest(t *testing.T) {
	s := newStack(t)
	ck := s.register(t, "csrf-rej@example.com", "password-1234")

	// POST /api/auth/verify-email/resend is a cookie-authed mutation. With
	// the access cookie present but no X-CSRF-Token header, CSRF middleware
	// must respond 403.
	req := jsonReq(http.MethodPost, "/api/auth/verify-email/resend", nil)
	attachCookies(req, ck)
	rec := s.do(req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 csrf, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Same request with the matching X-CSRF-Token header should pass CSRF.
	csrf := findCookie(ck, s.cookie.CSRFName())
	if csrf == nil {
		t.Fatalf("expected csrf cookie on register response")
	}
	req2 := jsonReq(http.MethodPost, "/api/auth/verify-email/resend", nil)
	attachCookies(req2, ck)
	req2.Header.Set("X-CSRF-Token", csrf.Value)
	rec2 := s.do(req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected resend ok with valid csrf, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

// ---------- 2) Bearer-auth bypasses CSRF ---------------------------------

func TestBearerAuthBypassesCSRF(t *testing.T) {
	s := newStack(t)
	ck := s.register(t, "bearer@example.com", "password-1234")
	access := findCookie(ck, s.cookie.AccessName())
	if access == nil {
		t.Fatalf("missing access cookie")
	}

	// Same mutation, but presented via Authorization: Bearer instead of the
	// access cookie. CSRF middleware should pass through.
	req := jsonReq(http.MethodPost, "/api/auth/verify-email/resend", nil)
	req.Header.Set("Authorization", "Bearer "+access.Value)
	rec := s.do(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected bearer-authed mutation to pass without csrf header, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- 3) 401 on missing/expired access -----------------------------

func TestUnauthenticatedReturns401(t *testing.T) {
	s := newStack(t)

	// No cookies: /api/auth/me should be 401.
	req := jsonReq(http.MethodGet, "/api/auth/me", nil)
	rec := s.do(req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated me, got %d", rec.Code)
	}

	// Forge an expired access JWT by issuing with a 1-nanosecond TTL.
	tokExpired, _, _, err := jwt.IssueAccess(s.ks, mustUUID(t), "ghost@example.com", false, time.Nanosecond)
	if err != nil {
		t.Fatalf("issue expired: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	req2 := jsonReq(http.MethodGet, "/api/auth/me", nil)
	req2.AddCookie(&http.Cookie{Name: s.cookie.AccessName(), Value: tokExpired})
	rec2 := s.do(req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired access cookie, got %d", rec2.Code)
	}
}

// ---------- 4) 429 + Retry-After on rate limit ---------------------------

func TestLoginRateLimited(t *testing.T) {
	// Tight limiter: 60/min == 1/s, burst 1. Two rapid attempts ⇒ second blocked.
	s := newStack(t, withLoginLimiter(60, 1))

	body := map[string]string{"email": "x@example.com", "password": "wrong"}
	// First attempt: limiter passes; service returns 401.
	rec := s.do(jsonReq(http.MethodPost, "/api/auth/login", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt: expected 401 (limiter passes, creds bad), got %d body=%s", rec.Code, rec.Body.String())
	}
	// Second attempt: limiter denies → 429 with Retry-After.
	rec2 := s.do(jsonReq(http.MethodPost, "/api/auth/login", body))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second attempt: expected 429, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	retry := rec2.Header().Get("Retry-After")
	if retry == "" {
		t.Fatalf("expected Retry-After header on 429")
	}
	if n, err := strconv.Atoi(retry); err != nil || n < 1 {
		t.Fatalf("expected positive integer Retry-After, got %q", retry)
	}
}

// ---------- 5) /me does NOT set csrf cookie ------------------------------

func TestMeDoesNotSetCSRFCookie(t *testing.T) {
	s := newStack(t)
	ck := s.register(t, "me@example.com", "password-1234")
	csrf := findCookie(ck, s.cookie.CSRFName())
	if csrf == nil {
		t.Fatalf("expected csrf cookie from register")
	}

	req := jsonReq(http.MethodGet, "/api/auth/me", nil)
	attachCookies(req, ck)
	rec := s.do(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, set := range rec.Result().Cookies() {
		if set.Name == s.cookie.CSRFName() {
			t.Fatalf("/me must NOT issue the csrf cookie; got %+v", set)
		}
	}
}

// ---------- 6) login/refresh/register DO set csrf cookie -----------------

func TestCredentialEndpointsSetCSRFCookie(t *testing.T) {
	s := newStack(t)

	// Register
	rec := s.do(jsonReq(http.MethodPost, "/api/auth/register", map[string]string{
		"email": "credev@example.com", "password": "password-1234",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d body=%s", rec.Code, rec.Body.String())
	}
	if findCookie(rec.Result().Cookies(), s.cookie.CSRFName()) == nil {
		t.Fatalf("register did not emit csrf cookie")
	}

	// Login
	rec = s.do(jsonReq(http.MethodPost, "/api/auth/login", map[string]string{
		"email": "credev@example.com", "password": "password-1234",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d body=%s", rec.Code, rec.Body.String())
	}
	loginCookies := rec.Result().Cookies()
	if findCookie(loginCookies, s.cookie.CSRFName()) == nil {
		t.Fatalf("login did not emit csrf cookie")
	}
	refresh := findCookie(loginCookies, s.cookie.RefreshName())
	if refresh == nil {
		t.Fatalf("login did not emit refresh cookie")
	}

	// Refresh — the refresh cookie scopes its Path to /api/auth/refresh, so
	// we re-attach it manually onto the refresh request.
	req := jsonReq(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refresh.Name, Value: refresh.Value})
	rec = s.do(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d body=%s", rec.Code, rec.Body.String())
	}
	if findCookie(rec.Result().Cookies(), s.cookie.CSRFName()) == nil {
		t.Fatalf("refresh did not emit csrf cookie")
	}
}

// ---------- helpers (file-scoped) ----------------------------------------

// mustUUID returns a deterministic non-zero uuid for tests that need a
// subject claim but don't care about identity.
func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse("11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	return id
}
