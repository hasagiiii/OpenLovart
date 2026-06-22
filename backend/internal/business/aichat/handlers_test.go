package aichat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/httpserver"
	"github.com/jiantaoli/openlovart/backend/internal/logging"
	"github.com/jiantaoli/openlovart/backend/internal/testutil"
)

// fakeRunner implements aichat.ChatRunner with a scripted event stream.
type fakeRunner struct {
	events      []*event.Event
	mintSession string
	gotProject  string
	gotMessage  string
	gotSession  string
}

func (f *fakeRunner) Run(_ context.Context, _ uuid.UUID, sessionID, projectID, message string) (string, <-chan *event.Event, error) {
	f.gotProject = projectID
	f.gotMessage = message
	f.gotSession = sessionID
	sid := sessionID
	if sid == "" {
		sid = f.mintSession
	}
	ch := make(chan *event.Event, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return sid, ch, nil
}

func textEvent(delta string) *event.Event {
	return &event.Event{Response: &model.Response{
		Object:  model.ObjectTypeChatCompletionChunk,
		Choices: []model.Choice{{Index: 0, Delta: model.Message{Content: delta}}},
	}}
}

func toolCallEvent(id, name, args string) *event.Event {
	return &event.Event{Response: &model.Response{Choices: []model.Choice{{
		Message: model.Message{
			Role:      model.RoleAssistant,
			ToolCalls: []model.ToolCall{{ID: id, Type: "function", Function: model.FunctionDefinitionParam{Name: name, Arguments: []byte(args)}}},
		},
	}}}}
}

func toolResultEvent(name, content string) *event.Event {
	return &event.Event{Response: &model.Response{Choices: []model.Choice{{
		Message: model.Message{Role: model.RoleTool, ToolName: name, Content: content},
	}}}}
}

type aiStack struct {
	router *gin.Engine
	cookie *cookies.Manager
	runner *fakeRunner
	db     *gorm.DB
}

func newAIStack(t *testing.T, fr *fakeRunner) *aiStack {
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
		ChatRuntime: fr,
	})
	return &aiStack{router: router, cookie: cookie, runner: fr, db: gdb}
}

type session struct {
	cookies []*http.Cookie
	csrf    string
}

func (s *aiStack) signUp(t *testing.T, email string) *session {
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

func (s *aiStack) post(t *testing.T, path string, body any, sess *session) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
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

func TestChatUnauthenticated(t *testing.T) {
	s := newAIStack(t, &fakeRunner{})
	rec := s.post(t, "/api/ai/chat/completions", map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatMissingMessages(t *testing.T) {
	s := newAIStack(t, &fakeRunner{})
	a := s.signUp(t, "chat-empty@example.com")
	rec := s.post(t, "/api/ai/chat/completions", map[string]any{"messages": []any{}}, a)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatNonStreamContent(t *testing.T) {
	fr := &fakeRunner{mintSession: "sess-new", events: []*event.Event{textEvent("Hello "), textEvent("world")}}
	s := newAIStack(t, fr)
	a := s.signUp(t, "chat-ns@example.com")

	rec := s.post(t, "/api/ai/chat/completions", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "say hi"}},
	}, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Object    string `json:"object"`
		SessionID string `json:"session_id"`
		Choices   []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Fatalf("object = %q", resp.Object)
	}
	if resp.SessionID != "sess-new" {
		t.Fatalf("session_id = %q, want minted sess-new", resp.SessionID)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "Hello world" {
		t.Fatalf("content = %+v", resp.Choices)
	}
}

func TestChatSessionIDPassthrough(t *testing.T) {
	fr := &fakeRunner{mintSession: "should-not-use", events: []*event.Event{textEvent("ok")}}
	s := newAIStack(t, fr)
	a := s.signUp(t, "chat-sid@example.com")

	rec := s.post(t, "/api/ai/chat/completions", map[string]any{
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"session_id": "existing-123",
		"project_id": "proj-abc",
	}, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if fr.gotSession != "existing-123" {
		t.Fatalf("runner got session %q, want existing-123", fr.gotSession)
	}
	if fr.gotProject != "proj-abc" {
		t.Fatalf("runner got project %q, want proj-abc", fr.gotProject)
	}
}

func TestChatNonStreamToolActivity(t *testing.T) {
	fr := &fakeRunner{mintSession: "s1", events: []*event.Event{
		toolCallEvent("call-1", "generate_image", `{"prompt":"a cat"}`),
		toolResultEvent("generate_image", `{"images":[{"url":"https://img/1","canvas_element_id":"el-1"}]}`),
		textEvent("Here is your cat."),
	}}
	s := newAIStack(t, fr)
	a := s.signUp(t, "chat-tool@example.com")

	rec := s.post(t, "/api/ai/chat/completions", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "draw a cat"}},
	}, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ToolActivity []struct {
			Object string          `json:"object"`
			Name   string          `json:"name"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		} `json:"tool_activity"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.ToolActivity) != 2 {
		t.Fatalf("tool_activity = %d, want 2: %+v", len(resp.ToolActivity), resp.ToolActivity)
	}
	if resp.ToolActivity[0].Object != "tool.call" || resp.ToolActivity[0].Name != "generate_image" {
		t.Fatalf("call entry = %+v", resp.ToolActivity[0])
	}
	if resp.ToolActivity[1].Object != "tool.result" || !strings.Contains(string(resp.ToolActivity[1].Result), "el-1") {
		t.Fatalf("result entry = %+v", resp.ToolActivity[1])
	}
	if resp.Choices[0].Message.Content != "Here is your cat." {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
}

func TestChatToolFailureDoesNotAbort(t *testing.T) {
	fr := &fakeRunner{mintSession: "s1", events: []*event.Event{
		toolCallEvent("call-1", "web_search", `{"query":"x"}`),
		toolResultEvent("web_search", "search backend unavailable"), // non-JSON → error
		textEvent("I could not search, but here is what I know."),
	}}
	s := newAIStack(t, fr)
	a := s.signUp(t, "chat-toolfail@example.com")

	rec := s.post(t, "/api/ai/chat/completions", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "latest news"}},
	}, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite tool failure, got %d", rec.Code)
	}
	var resp struct {
		ToolActivity []struct {
			Object string `json:"object"`
			Error  string `json:"error"`
		} `json:"tool_activity"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Choices[0].Message.Content == "" {
		t.Fatalf("expected a reply after tool failure")
	}
	var sawErr bool
	for _, ta := range resp.ToolActivity {
		if ta.Object == "tool.result" && ta.Error != "" {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("expected a tool.result with error, got %+v", resp.ToolActivity)
	}
}

func TestChatStreaming(t *testing.T) {
	fr := &fakeRunner{mintSession: "stream-1", events: []*event.Event{
		toolCallEvent("c1", "generate_image", `{"prompt":"x"}`),
		toolResultEvent("generate_image", `{"images":[{"url":"https://img/1"}]}`),
		textEvent("Done"),
	}}
	s := newAIStack(t, fr)
	a := s.signUp(t, "chat-stream@example.com")

	rec := s.post(t, "/api/ai/chat/completions", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "draw"}},
		"stream":   true,
	}, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	if rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("missing X-Accel-Buffering: no")
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"object":"session"`, `"session_id":"stream-1"`,
		`"object":"tool.call"`, `"object":"tool.result"`,
		`"object":"chat.completion.chunk"`, `"content":"Done"`,
		"data: [DONE]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream body missing %q\n---\n%s", want, body)
		}
	}
}
