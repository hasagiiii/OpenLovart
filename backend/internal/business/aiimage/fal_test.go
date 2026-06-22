package aiimage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// falTestServer wires a minimal fal queue (submit → status → result) backed by
// httptest, returning self-referential status/response URLs.
type falTestServer struct {
	server     *httptest.Server
	statusBody string
	resultBody string
	submitCode int
	lastSubmit map[string]any
	authHeader string
}

func newFalTestServer(t *testing.T) *falTestServer {
	t.Helper()
	fs := &falTestServer{
		submitCode: http.StatusOK,
		resultBody: `{"images":[{"url":"https://cdn.fal/out.png","width":1024,"height":1024,"content_type":"image/png"}]}`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		body := fs.statusBody
		if body == "" {
			body = `{"status":"COMPLETED","response_url":"` + fs.server.URL + `/result"}`
		}
		io.WriteString(w, body)
	})
	mux.HandleFunc("/result", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fs.resultBody)
	})
	mux.HandleFunc("/fal-ai/test/model", func(w http.ResponseWriter, r *http.Request) {
		fs.authHeader = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &fs.lastSubmit)
		w.WriteHeader(fs.submitCode)
		if fs.submitCode != http.StatusOK {
			io.WriteString(w, `{"detail":"bad request"}`)
			return
		}
		io.WriteString(w, `{"request_id":"req-1","status":"IN_QUEUE","status_url":"`+fs.server.URL+`/status","response_url":"`+fs.server.URL+`/result"}`)
	})
	fs.server = httptest.NewServer(mux)
	t.Cleanup(fs.server.Close)
	return fs
}

func (fs *falTestServer) provider() Provider {
	return NewFalProvider("test-key", "fal-ai/test/model",
		WithFalBaseURL(fs.server.URL),
		WithFalHTTPClient(fs.server.Client()),
		WithFalPollInterval(time.Millisecond),
	)
}

func TestFalGenerateHappyPath(t *testing.T) {
	fs := newFalTestServer(t)
	p := fs.provider()

	res, err := p.Generate(context.Background(), Params{Prompt: "a cat", Size: "1024x1024", N: 1})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(res.Images))
	}
	img := res.Images[0]
	if img.URL != "https://cdn.fal/out.png" || img.Width != 1024 || img.ContentType != "image/png" {
		t.Fatalf("unexpected image: %+v", img)
	}
	if fs.authHeader != "Key test-key" {
		t.Fatalf("auth header = %q, want %q", fs.authHeader, "Key test-key")
	}
	if fs.lastSubmit["prompt"] != "a cat" || fs.lastSubmit["image_size"] != "1024x1024" {
		t.Fatalf("submit payload missing fields: %+v", fs.lastSubmit)
	}
	if _, ok := fs.lastSubmit["image_url"]; ok {
		t.Fatalf("generate must not send image_url: %+v", fs.lastSubmit)
	}
}

func TestFalEditForwardsReferenceImage(t *testing.T) {
	fs := newFalTestServer(t)
	p := fs.provider()

	_, err := p.Edit(context.Background(), Params{Prompt: "make it blue", ReferenceImage: "https://in/ref.png"})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if fs.lastSubmit["image_url"] != "https://in/ref.png" {
		t.Fatalf("edit must forward image_url, got: %+v", fs.lastSubmit)
	}
}

func TestFalFailureStatusMapsToError(t *testing.T) {
	fs := newFalTestServer(t)
	fs.statusBody = `{"status":"FAILED","error":"content policy"}`
	p := fs.provider()

	_, err := p.Generate(context.Background(), Params{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "content policy") {
		t.Fatalf("err = %v, want failure containing 'content policy'", err)
	}
}

func TestFalSubmitHTTPErrorMapsToError(t *testing.T) {
	fs := newFalTestServer(t)
	fs.submitCode = http.StatusBadRequest
	p := fs.provider()

	_, err := p.Generate(context.Background(), Params{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "submit failed") {
		t.Fatalf("err = %v, want submit failure", err)
	}
}

func TestFalContextCancellationStopsPolling(t *testing.T) {
	fs := newFalTestServer(t)
	fs.statusBody = `{"status":"IN_PROGRESS"}` // never completes
	p := fs.provider()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := p.Generate(ctx, Params{Prompt: "x"})
	if err == nil {
		t.Fatalf("expected context error, got nil")
	}
}
