package aiimage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/business/aiimage"
	"github.com/jiantaoli/openlovart/backend/internal/httpserver"
	"github.com/jiantaoli/openlovart/backend/internal/logging"
	"github.com/jiantaoli/openlovart/backend/internal/testutil"
)

// stubProvider is a controllable aiimage.Provider. When block is non-nil the
// Generate/Edit calls wait on it, letting tests observe the IN_PROGRESS state
// before completion.
type stubProvider struct {
	mu        sync.Mutex
	genCalls  int
	editCalls int
	result    aiimage.Result
	err       error
	block     chan struct{}
}

func (p *stubProvider) Generate(_ context.Context, _ aiimage.Params) (aiimage.Result, error) {
	p.mu.Lock()
	p.genCalls++
	p.mu.Unlock()
	if p.block != nil {
		<-p.block
	}
	return p.result, p.err
}

func (p *stubProvider) Edit(_ context.Context, _ aiimage.Params) (aiimage.Result, error) {
	p.mu.Lock()
	p.editCalls++
	p.mu.Unlock()
	if p.block != nil {
		<-p.block
	}
	return p.result, p.err
}

func (p *stubProvider) edits() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.editCalls
}

type imgStack struct {
	router *gin.Engine
	cookie *cookies.Manager
	db     *gorm.DB
}

func newImgStack(t *testing.T, provider aiimage.Provider) *imgStack {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := testutil.DevConfig()
	gdb := testutil.PostgresDB(t)
	ks := testutil.FreshKeyStore(t, cfg)
	mailer := testutil.NewFakeMailer()
	svc := service.New(gdb, cfg, ks, mailer, nil)
	cookie := cookies.New(cfg)
	logger := logging.New(cfg)

	imageSvc := aiimage.NewService(provider, aiimage.NewMemoryStore())

	router := httpserver.New(httpserver.Deps{
		Cfg:          cfg,
		Log:          logger,
		DB:           gdb,
		KeyStore:     ks,
		AuthService:  svc,
		Cookies:      cookie,
		ImageService: imageSvc,
	})
	return &imgStack{router: router, cookie: cookie, db: gdb}
}

type session struct {
	cookies []*http.Cookie
	csrf    string
}

func (s *imgStack) signUp(t *testing.T, email string) *session {
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
	return &session{cookies: cs, csrf: csrf}
}

func (s *imgStack) do(t *testing.T, method, path string, body any, sess *session) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
		req.Header.Set("X-CSRF-Token", sess.csrf)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// submit posts a generation request and returns the request_id.
func (s *imgStack) submit(t *testing.T, body any, sess *session) string {
	t.Helper()
	rec := s.do(t, http.MethodPost, "/api/ai/images", body, sess)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit: expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit: %v", err)
	}
	if resp.Status != "IN_QUEUE" {
		t.Fatalf("submit status = %q, want IN_QUEUE", resp.Status)
	}
	if resp.RequestID == "" {
		t.Fatalf("submit returned empty request_id")
	}
	return resp.RequestID
}

// waitStatus polls the status endpoint until it reaches want (or times out).
func (s *imgStack) waitStatus(t *testing.T, id string, sess *session, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec := s.do(t, http.MethodGet, "/api/ai/images/"+id+"/status", nil, sess)
		if rec.Code == http.StatusOK {
			var r struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &r)
			if r.Status == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for status %q on %s", want, id)
}

func TestImageSubmitMissingPrompt(t *testing.T) {
	s := newImgStack(t, &stubProvider{})
	a := s.signUp(t, "img-noprompt@example.com")
	rec := s.do(t, http.MethodPost, "/api/ai/images", map[string]any{"size": "1024x1024"}, a)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestImageSubmitUnauthenticated(t *testing.T) {
	s := newImgStack(t, &stubProvider{})
	rec := s.do(t, http.MethodPost, "/api/ai/images", map[string]any{"prompt": "a cat"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestImageGenerateHappyPath(t *testing.T) {
	provider := &stubProvider{result: aiimage.Result{Images: []aiimage.Image{
		{URL: "https://img/1", Width: 1024, Height: 1024, ContentType: "image/png"},
	}}}
	s := newImgStack(t, provider)
	a := s.signUp(t, "img-happy@example.com")

	id := s.submit(t, map[string]any{"prompt": "a cat"}, a)
	s.waitStatus(t, id, a, "COMPLETED")

	rec := s.do(t, http.MethodGet, "/api/ai/images/"+id, nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("result: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Images []aiimage.Image `json:"images"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(resp.Images) != 1 || resp.Images[0].URL != "https://img/1" {
		t.Fatalf("images = %+v", resp.Images)
	}
}

func TestImageEditPath(t *testing.T) {
	provider := &stubProvider{result: aiimage.Result{Images: []aiimage.Image{{URL: "https://img/edited"}}}}
	s := newImgStack(t, provider)
	a := s.signUp(t, "img-edit@example.com")

	id := s.submit(t, map[string]any{
		"prompt":          "make it blue",
		"reference_image": "https://img/source",
	}, a)
	s.waitStatus(t, id, a, "COMPLETED")

	if provider.edits() != 1 {
		t.Fatalf("expected Edit to be called once, got %d", provider.edits())
	}
	rec := s.do(t, http.MethodGet, "/api/ai/images/"+id, nil, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("result: expected 200, got %d", rec.Code)
	}
}

func TestImageStatusNonOwner404(t *testing.T) {
	provider := &stubProvider{result: aiimage.Result{Images: []aiimage.Image{{URL: "https://img/1"}}}}
	s := newImgStack(t, provider)
	owner := s.signUp(t, "img-owner@example.com")
	other := s.signUp(t, "img-other@example.com")

	id := s.submit(t, map[string]any{"prompt": "a cat"}, owner)

	rec := s.do(t, http.MethodGet, "/api/ai/images/"+id+"/status", nil, other)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner status: expected 404, got %d", rec.Code)
	}
	rec = s.do(t, http.MethodGet, "/api/ai/images/"+id, nil, other)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner result: expected 404, got %d", rec.Code)
	}
	rec = s.do(t, http.MethodGet, "/api/ai/images/does-not-exist/status", nil, owner)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status: expected 404, got %d", rec.Code)
	}
}

func TestImageResultBeforeComplete409(t *testing.T) {
	provider := &stubProvider{
		result: aiimage.Result{Images: []aiimage.Image{{URL: "https://img/1"}}},
		block:  make(chan struct{}),
	}
	s := newImgStack(t, provider)
	a := s.signUp(t, "img-409@example.com")

	id := s.submit(t, map[string]any{"prompt": "a cat"}, a)

	// Provider is blocked → job is not COMPLETED; result must be 409.
	rec := s.do(t, http.MethodGet, "/api/ai/images/"+id, nil, a)
	if rec.Code != http.StatusConflict {
		t.Fatalf("result-before-complete: expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Error  string `json:"error"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &conflict)
	if conflict.Error != "not_ready" {
		t.Fatalf("conflict error = %q, want not_ready", conflict.Error)
	}

	close(provider.block) // let it finish
	s.waitStatus(t, id, a, "COMPLETED")
}

func TestImageFailurePath(t *testing.T) {
	provider := &stubProvider{err: context.DeadlineExceeded}
	s := newImgStack(t, provider)
	a := s.signUp(t, "img-fail@example.com")

	id := s.submit(t, map[string]any{"prompt": "a cat"}, a)
	s.waitStatus(t, id, a, "FAILED")

	rec := s.do(t, http.MethodGet, "/api/ai/images/"+id+"/status", nil, a)
	var resp struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "FAILED" || resp.Error == "" {
		t.Fatalf("failed status = %+v, want FAILED with error", resp)
	}

	// Result on a failed job is not ready → 409.
	rec = s.do(t, http.MethodGet, "/api/ai/images/"+id, nil, a)
	if rec.Code != http.StatusConflict {
		t.Fatalf("failed result: expected 409, got %d", rec.Code)
	}
}
