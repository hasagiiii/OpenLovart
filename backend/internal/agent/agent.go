// Package agent embeds trpc-agent-go as a library inside the Gin backend. It
// constructs an OpenAI-compatible model, an LLM agent with the design-assistant
// system instruction and a fixed set of server-side tools (generate_image,
// edit_image, web_search), an in-memory session service for per-session
// memory, and a reusable runner. The ChatService facade (Run) is the single
// entry point used by the HTTP chat handler.
package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	"github.com/jiantaoli/openlovart/backend/internal/business/aiimage"
	"github.com/jiantaoli/openlovart/backend/internal/business/websearch"
)

// DefaultSystemInstruction is the design-assistant behaviour carried over from
// the old generate-design route, extended to describe the available tools.
const DefaultSystemInstruction = `You are a professional design assistant embedded in a visual canvas application.
Based on the user's description, provide detailed, specific, and creative design guidance covering layout, colors, typography, and visual elements.

You have tools available:
- generate_image: create a new image from a text prompt when the user asks to make/draw/generate an image.
- edit_image: modify an existing image when the user supplies a reference image to change.
- web_search: look up live, current information from the web when the answer depends on recent or factual data you may not know.

Call a tool only when it clearly helps fulfil the request; otherwise answer directly. After a tool returns, reference its result (image URLs or search findings) in your reply.`

const (
	defaultMaxToolIterations = 5
	defaultToolTimeout       = 3 * time.Minute
	defaultSessionEventLimit = 200
)

// Config configures the agent runtime.
type Config struct {
	// AppName names the runner/session app namespace.
	AppName string
	// ChatModel is the OpenAI-compatible model name.
	ChatModel string
	// OpenAIAPIKey / OpenAIBaseURL configure the model endpoint.
	OpenAIAPIKey  string
	OpenAIBaseURL string
	// SystemInstruction overrides DefaultSystemInstruction when set.
	SystemInstruction string
	// MaxToolIterations caps tool-call iterations per turn (0 → default).
	MaxToolIterations int
	// ToolTimeout bounds each individual tool call (0 → default).
	ToolTimeout time.Duration
	// SessionEventLimit bounds retained events per session (0 → default).
	SessionEventLimit int
}

// Runtime owns the constructed agent runner and the tool dependencies.
type Runtime struct {
	appName     string
	runner      runner.Runner
	images      *aiimage.Service
	search      websearch.Provider
	canvas      CanvasPersister
	toolTimeout time.Duration
}

// New builds the runtime. images is required; search may be nil (web_search is
// then not registered); canvas may be nil (no tool-side persistence).
func New(cfg Config, images *aiimage.Service, search websearch.Provider, canvas CanvasPersister) *Runtime {
	if cfg.AppName == "" {
		cfg.AppName = "openlovart"
	}
	if cfg.SystemInstruction == "" {
		cfg.SystemInstruction = DefaultSystemInstruction
	}
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = defaultMaxToolIterations
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = defaultToolTimeout
	}
	if cfg.SessionEventLimit <= 0 {
		cfg.SessionEventLimit = defaultSessionEventLimit
	}

	rt := &Runtime{
		appName:     cfg.AppName,
		images:      images,
		search:      search,
		canvas:      canvas,
		toolTimeout: cfg.ToolTimeout,
	}

	mdl := openai.New(cfg.ChatModel,
		openai.WithAPIKey(cfg.OpenAIAPIKey),
		openai.WithBaseURL(cfg.OpenAIBaseURL),
	)

	tools := rt.buildTools()

	ag := llmagent.New(cfg.AppName,
		llmagent.WithModel(mdl),
		llmagent.WithInstruction(cfg.SystemInstruction),
		llmagent.WithTools(tools),
		llmagent.WithMaxToolIterations(cfg.MaxToolIterations),
	)

	sess := inmemory.NewSessionService(
		inmemory.WithSessionEventLimit(cfg.SessionEventLimit),
	)

	rt.runner = runner.NewRunner(cfg.AppName, ag, runner.WithSessionService(sess))
	return rt
}

// SearchEnabled reports whether the web_search tool is registered.
func (rt *Runtime) SearchEnabled() bool { return rt.search != nil }

// Run is the ChatService facade. It mints a sessionID when one is not supplied,
// binds the request scope (userID + optional projectID for tool-side canvas
// persistence) into the context, appends the latest user message, and starts
// the agent. It returns the resolved sessionID and the event channel; the
// caller streams or aggregates the events. The supplied ctx governs
// cancellation — a client disconnect cancels the run and in-flight tools.
func (rt *Runtime) Run(
	ctx context.Context,
	userID uuid.UUID,
	sessionID string,
	projectID string,
	message string,
) (string, <-chan *event.Event, error) {
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	scope := &RequestScope{UserID: userID}
	if projectID != "" {
		if pid, err := uuid.Parse(projectID); err == nil {
			scope.ProjectID = pid
		}
	}
	ctx = withScope(ctx, scope)

	ch, err := rt.runner.Run(ctx, userID.String(), sessionID, model.NewUserMessage(message))
	if err != nil {
		return sessionID, nil, err
	}
	return sessionID, ch, nil
}

// Close releases runner-owned resources.
func (rt *Runtime) Close() error {
	if rt.runner == nil {
		return nil
	}
	return rt.runner.Close()
}

// --- request scope (per-turn context) ---

type contextKey struct{ name string }

var scopeKey = &contextKey{"aiagent.scope"}

// RequestScope carries per-turn data the tools need: the authenticated user
// and an optional project to persist generated images into.
type RequestScope struct {
	UserID    uuid.UUID
	ProjectID uuid.UUID // uuid.Nil → no persistence
}

func withScope(ctx context.Context, s *RequestScope) context.Context {
	return context.WithValue(ctx, scopeKey, s)
}

func scopeFromContext(ctx context.Context) *RequestScope {
	if s, ok := ctx.Value(scopeKey).(*RequestScope); ok {
		return s
	}
	return nil
}
