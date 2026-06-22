package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jiantaoli/openlovart/backend/internal/business/aiimage"
	"github.com/jiantaoli/openlovart/backend/internal/business/websearch"
)

// stubProvider is a minimal aiimage.Provider for runtime tool tests.
type stubProvider struct {
	res aiimage.Result
	err error
}

func (s stubProvider) Generate(context.Context, aiimage.Params) (aiimage.Result, error) {
	return s.res, s.err
}
func (s stubProvider) Edit(context.Context, aiimage.Params) (aiimage.Result, error) {
	return s.res, s.err
}

// recordingPersister captures PersistImage calls.
type recordingPersister struct {
	calls int
	id    string
}

func (p *recordingPersister) PersistImage(_ context.Context, _, _ uuid.UUID, _ aiimage.Image, _ int) (string, error) {
	p.calls++
	return p.id, nil
}

func newTestRuntime(t *testing.T, search websearch.Provider, canvas CanvasPersister) *Runtime {
	t.Helper()
	imgSvc := aiimage.NewService(stubProvider{res: aiimage.Result{Images: []aiimage.Image{{URL: "https://img/1", Width: 100, Height: 200, ContentType: "image/png"}}}}, aiimage.NewMemoryStore())
	return New(Config{AppName: "test", ChatModel: "m", OpenAIAPIKey: "k", OpenAIBaseURL: "http://x"}, imgSvc, search, canvas)
}

func TestSearchEnabledReflectsProvider(t *testing.T) {
	rt := newTestRuntime(t, nil, nil)
	if rt.SearchEnabled() {
		t.Fatalf("expected search disabled when provider nil")
	}

	prov, _ := websearch.New(websearch.ProviderBrave, "key")
	rt2 := newTestRuntime(t, prov, nil)
	if !rt2.SearchEnabled() {
		t.Fatalf("expected search enabled when provider set")
	}
}

func TestBuildToolsRegistersSearchOnlyWhenEnabled(t *testing.T) {
	rt := newTestRuntime(t, nil, nil)
	if got := len(rt.buildTools()); got != 2 {
		t.Fatalf("tools without search = %d, want 2", got)
	}

	prov, _ := websearch.New(websearch.ProviderBrave, "key")
	rt2 := newTestRuntime(t, prov, nil)
	if got := len(rt2.buildTools()); got != 3 {
		t.Fatalf("tools with search = %d, want 3", got)
	}
}

func TestGenerateImageToolPersistsWhenProjectScoped(t *testing.T) {
	persister := &recordingPersister{id: "elem-1"}
	rt := newTestRuntime(t, nil, persister)

	scope := &RequestScope{UserID: uuid.New(), ProjectID: uuid.New()}
	ctx := withScope(context.Background(), scope)

	out, err := rt.generateImage(ctx, generateImageInput{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("generateImage: %v", err)
	}
	if len(out.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(out.Images))
	}
	if out.Images[0].URL != "https://img/1" {
		t.Fatalf("url = %q", out.Images[0].URL)
	}
	if persister.calls != 1 {
		t.Fatalf("persist calls = %d, want 1", persister.calls)
	}
	if out.Images[0].CanvasElementID != "elem-1" {
		t.Fatalf("element id = %q, want elem-1", out.Images[0].CanvasElementID)
	}
}

func TestGenerateImageToolSkipsPersistWithoutProject(t *testing.T) {
	persister := &recordingPersister{id: "elem-1"}
	rt := newTestRuntime(t, nil, persister)

	// scope present but no project id → no persistence.
	ctx := withScope(context.Background(), &RequestScope{UserID: uuid.New()})
	out, err := rt.generateImage(ctx, generateImageInput{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("generateImage: %v", err)
	}
	if persister.calls != 0 {
		t.Fatalf("persist calls = %d, want 0", persister.calls)
	}
	if out.Images[0].CanvasElementID != "" {
		t.Fatalf("element id = %q, want empty", out.Images[0].CanvasElementID)
	}
}

func TestEditImageToolUsesEditPath(t *testing.T) {
	rt := newTestRuntime(t, nil, nil)
	out, err := rt.editImage(context.Background(), editImageInput{Prompt: "blue", ReferenceImage: "https://in/ref.png"})
	if err != nil {
		t.Fatalf("editImage: %v", err)
	}
	if len(out.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(out.Images))
	}
}

func TestWebSearchToolMapsResults(t *testing.T) {
	rt := newTestRuntime(t, fakeSearch{hits: []websearch.Result{{Title: "T", URL: "u", Description: "d"}}}, nil)
	out, err := rt.webSearch(context.Background(), webSearchInput{Query: "go"})
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "T" {
		t.Fatalf("unexpected results: %+v", out.Results)
	}
}

func TestWebSearchToolPropagatesError(t *testing.T) {
	rt := newTestRuntime(t, fakeSearch{err: errors.New("down")}, nil)
	if _, err := rt.webSearch(context.Background(), webSearchInput{Query: "go"}); err == nil {
		t.Fatalf("expected error from search provider")
	}
}

type fakeSearch struct {
	hits []websearch.Result
	err  error
}

func (f fakeSearch) Search(context.Context, string) ([]websearch.Result, error) {
	return f.hits, f.err
}
