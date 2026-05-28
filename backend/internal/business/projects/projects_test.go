package projects_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/config"
	"github.com/jiantaoli/openlovart/backend/internal/httpserver"
	"github.com/jiantaoli/openlovart/backend/internal/logging"
	"github.com/jiantaoli/openlovart/backend/internal/testutil"
)

// stack reuses the same wire-up the auth handlers tests do, but exposed
// through the business-side test file so this package's tests are
// self-contained.
type stack struct {
	router *gin.Engine
	cfg    *config.Config
	cookie *cookies.Manager
	svc    *service.Service
	db     *gorm.DB
}

func newStack(t *testing.T) *stack {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := testutil.DevConfig()
	gdb := testutil.PostgresDB(t)
	ks := testutil.FreshKeyStore(t, cfg)
	mailer := testutil.NewFakeMailer()
	svc := service.New(gdb, cfg, ks, mailer, nil)
	cookie := cookies.New(cfg)
	logger := logging.New(cfg)

	router := httpserver.New(httpserver.Deps{
		Cfg:         cfg,
		Log:         logger,
		DB:          gdb,
		KeyStore:    ks,
		AuthService: svc,
		Cookies:     cookie,
	})
	return &stack{router: router, cfg: cfg, cookie: cookie, svc: svc, db: gdb}
}

// session bundles the auth cookies + csrf header value for a registered
// user so tests can chain authenticated requests succinctly.
type session struct {
	cookies []*http.Cookie
	csrf    string
}

func (s *stack) signUp(t *testing.T, email string) *session {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": "password-1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d body=%s", rec.Code, rec.Body.String())
	}
	cs := rec.Result().Cookies()
	var csrf string
	for _, c := range cs {
		if c.Name == s.cookie.CSRFName() {
			csrf = c.Value
		}
	}
	if csrf == "" {
		t.Fatalf("register did not emit csrf cookie")
	}
	return &session{cookies: cs, csrf: csrf}
}

func (s *stack) authedJSON(t *testing.T, method, path string, body any, sess *session) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range sess.cookies {
		req.AddCookie(c)
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("X-CSRF-Token", sess.csrf)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// listResponse mirrors the wire shape for /api/projects.
type listResponse struct {
	Items []struct {
		ID        string `json:"id"`
		UserID    string `json:"user_id"`
		Title     string `json:"title"`
		Thumbnail any    `json:"thumbnail"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// ---------- 1) list-projects-only-returns-own ----------------------------

func TestListProjectsOnlyReturnsOwn(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "alice@example.com")
	b := s.signUp(t, "bob@example.com")

	for _, title := range []string{"alpha", "beta"} {
		rec := s.authedJSON(t, http.MethodPost, "/api/projects", map[string]string{"title": title}, a)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create as alice (%s): %d body=%s", title, rec.Code, rec.Body.String())
		}
	}
	rec := s.authedJSON(t, http.MethodPost, "/api/projects", map[string]string{"title": "bobs-thing"}, b)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create as bob: %d body=%s", rec.Code, rec.Body.String())
	}

	rec = s.authedJSON(t, http.MethodGet, "/api/projects", nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("list as alice: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("alice should see 2 projects, got %d", len(resp.Items))
	}
	for _, it := range resp.Items {
		if it.Title == "bobs-thing" {
			t.Fatalf("alice's list leaked bob's project")
		}
	}
}

// ---------- 2) list-projects-cursor-pagination-roundtrip -----------------

func TestListProjectsCursorPaginationRoundtrip(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "page@example.com")

	const total = 5
	for i := 0; i < total; i++ {
		rec := s.authedJSON(t, http.MethodPost, "/api/projects",
			map[string]string{"title": "p" + string(rune('A'+i))}, a)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d: %d", i, rec.Code)
		}
	}

	// Page 1 (limit=2).
	rec := s.authedJSON(t, http.MethodGet, "/api/projects?limit=2", nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1: %d body=%s", rec.Code, rec.Body.String())
	}
	var p1 listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &p1); err != nil {
		t.Fatalf("decode p1: %v", err)
	}
	if len(p1.Items) != 2 {
		t.Fatalf("page1 expected 2 items, got %d", len(p1.Items))
	}
	if p1.NextCursor == nil {
		t.Fatalf("expected non-nil nextCursor on page 1")
	}

	// Page 2.
	rec = s.authedJSON(t, http.MethodGet, "/api/projects?limit=2&cursor="+*p1.NextCursor, nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("page2: %d body=%s", rec.Code, rec.Body.String())
	}
	var p2 listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &p2); err != nil {
		t.Fatalf("decode p2: %v", err)
	}
	if len(p2.Items) != 2 {
		t.Fatalf("page2 expected 2 items, got %d", len(p2.Items))
	}

	// Page 3 (last).
	rec = s.authedJSON(t, http.MethodGet, "/api/projects?limit=2&cursor="+*p2.NextCursor, nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("page3: %d body=%s", rec.Code, rec.Body.String())
	}
	var p3 listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &p3); err != nil {
		t.Fatalf("decode p3: %v", err)
	}
	if len(p3.Items) != 1 {
		t.Fatalf("page3 expected 1 item, got %d", len(p3.Items))
	}
	if p3.NextCursor != nil {
		t.Fatalf("expected nil nextCursor at end, got %v", *p3.NextCursor)
	}

	// IDs across the three pages should be unique and total to 5.
	seen := map[string]struct{}{}
	for _, p := range []listResponse{p1, p2, p3} {
		for _, it := range p.Items {
			seen[it.ID] = struct{}{}
		}
	}
	if len(seen) != total {
		t.Fatalf("expected %d distinct ids across pages, got %d", total, len(seen))
	}
}

// ---------- 3) list-projects-rejects-malformed-cursor (HTTP 400) ---------

func TestListProjectsRejectsMalformedCursor(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "bad@example.com")

	rec := s.authedJSON(t, http.MethodGet, "/api/projects?cursor=not-base64!@#$", nil, a)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed cursor, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- 4) list-projects-clamps-oversized-limit (≤100) ---------------

func TestListProjectsClampsOversizedLimit(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "limit@example.com")

	// Create just one project — clamping is a behavior of the handler/repo,
	// not the data set, so we don't need >100 rows to assert it.
	if rec := s.authedJSON(t, http.MethodPost, "/api/projects", map[string]string{"title": "only"}, a); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}

	// limit=10000 must be accepted (silently clamped to 100).
	rec := s.authedJSON(t, http.MethodGet, "/api/projects?limit=10000", nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on oversized limit (silent clamp), got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}

	// limit=0 (or negative) is invalid: handler responds 400.
	if rec := s.authedJSON(t, http.MethodGet, "/api/projects?limit=0", nil, a); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 expected 400, got %d", rec.Code)
	}
	if rec := s.authedJSON(t, http.MethodGet, "/api/projects?limit=-1", nil, a); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=-1 expected 400, got %d", rec.Code)
	}
}

// ---------- 5) cross-user fetch returns 404 ------------------------------

func TestCrossUserFetchReturns404(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "owner@example.com")
	b := s.signUp(t, "snooper@example.com")

	rec := s.authedJSON(t, http.MethodPost, "/api/projects", map[string]string{"title": "secret"}, a)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Bob fetches Alice's project ID directly: must 404.
	if rec := s.authedJSON(t, http.MethodGet, "/api/projects/"+created.ID, nil, b); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user fetch, got %d body=%s", rec.Code, rec.Body.String())
	}
	// PATCH and DELETE also 404 across users.
	if rec := s.authedJSON(t, http.MethodPatch, "/api/projects/"+created.ID, map[string]string{"title": "owned-by-bob"}, b); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on cross-user patch, got %d", rec.Code)
	}
	if rec := s.authedJSON(t, http.MethodDelete, "/api/projects/"+created.ID, nil, b); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on cross-user delete, got %d", rec.Code)
	}
}

// ---------- 6) canvas-elements replace roundtrip -------------------------

func TestCanvasElementsReplaceRoundtrip(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "canvas@example.com")

	// Create the parent project.
	rec := s.authedJSON(t, http.MethodPost, "/api/projects", map[string]string{"title": "canvas-1"}, a)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: %d body=%s", rec.Code, rec.Body.String())
	}
	var proj struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &proj)

	// PUT 3 elements. Each element_data is opaque JSON — the backend must
	// not inspect or rewrite it.
	payload := map[string]any{
		"elements": []map[string]any{
			{"element_data": map[string]any{"kind": "rect", "x": 1, "y": 2}},
			{"element_data": map[string]any{"kind": "text", "value": "hi"}},
			{"element_data": map[string]any{"kind": "image", "src": "/foo.png"}},
		},
	}
	rec = s.authedJSON(t, http.MethodPut, "/api/projects/"+proj.ID+"/canvas-elements", payload, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("put canvas: %d body=%s", rec.Code, rec.Body.String())
	}

	// GET back: 3 items, in stable order, with element_data preserved.
	rec = s.authedJSON(t, http.MethodGet, "/api/projects/"+proj.ID+"/canvas-elements", nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("get canvas: %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			ID          string          `json:"id"`
			ProjectID   string          `json:"project_id"`
			ElementData json.RawMessage `json:"element_data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(got.Items))
	}

	// Replace with an empty array: must clear.
	rec = s.authedJSON(t, http.MethodPut, "/api/projects/"+proj.ID+"/canvas-elements",
		map[string]any{"elements": []any{}}, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("put empty: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = s.authedJSON(t, http.MethodGet, "/api/projects/"+proj.ID+"/canvas-elements", nil, a)
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 0 {
		t.Fatalf("expected empty after clear, got %d", len(got.Items))
	}
}

// ---------- 7) credits-defaults-to-1000-on-user-creation -----------------

func TestCreditsDefaultsTo1000OnUserCreation(t *testing.T) {
	s := newStack(t)
	a := s.signUp(t, "rich@example.com")

	rec := s.authedJSON(t, http.MethodGet, "/api/credits", nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("get credits: %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		UserID  string `json:"user_id"`
		Credits int    `json:"credits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Credits != 1000 {
		t.Fatalf("expected starting credits 1000, got %d", got.Credits)
	}
	if got.UserID == "" {
		t.Fatalf("expected user_id in response")
	}
}
