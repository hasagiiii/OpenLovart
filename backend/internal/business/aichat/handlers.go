// Package aichat implements the OpenAI-shaped chat endpoint
// (POST /api/ai/chat/completions). It bridges the trpc-agent-go event stream
// (via the agent.Runtime facade) to either a single chat.completion JSON
// object or an SSE stream of chat.completion.chunk events plus structured
// tool-activity events. Per-session memory and tool-side canvas persistence are
// handled inside the runtime; this handler owns request validation, the wire
// contract, and streaming/cancellation.
package aichat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
)

// ChatRunner is the slice of the agent runtime the handler depends on. It is an
// interface so tests can inject a fake event stream without a live model.
// *agent.Runtime satisfies it.
type ChatRunner interface {
	Run(ctx context.Context, userID uuid.UUID, sessionID, projectID, message string) (string, <-chan *event.Event, error)
}

// Handlers serves the chat endpoint.
type Handlers struct {
	rt  ChatRunner
	log *slog.Logger
}

// NewHandlers constructs the chat handlers over the agent runtime.
func NewHandlers(rt ChatRunner, log *slog.Logger) *Handlers {
	return &Handlers{rt: rt, log: log}
}

// --- request shape (OpenAI-ish) ---

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	SessionID string        `json:"session_id"`
	ProjectID string        `json:"project_id"`
	// `user` is intentionally ignored; the user is the authenticated session.
}

// --- response shapes ---

type respMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respChoice struct {
	Index        int         `json:"index"`
	Message      respMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// toolActivity captures a tool.call or tool.result for the non-streaming body
// and the streaming side-channel.
type toolActivity struct {
	Object    string          `json:"object"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type chatCompletionResponse struct {
	ID           string         `json:"id"`
	Object       string         `json:"object"`
	Created      int64          `json:"created"`
	Model        string         `json:"model"`
	SessionID    string         `json:"session_id"`
	Choices      []respChoice   `json:"choices"`
	ToolActivity []toolActivity `json:"tool_activity,omitempty"`
}

// Completions handles POST /api/ai/chat/completions.
func (h *Handlers) Completions(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages is required"})
		return
	}

	// Per D9 the session store is the source of truth for history; the client
	// sends the latest user message (plus the session_id). Use the last
	// message's content as the turn input.
	message := strings.TrimSpace(req.Messages[len(req.Messages)-1].Content)
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "last message content is empty"})
		return
	}

	ctx := c.Request.Context()
	sessionID, events, err := h.rt.Run(ctx, user.ID, req.SessionID, req.ProjectID, message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start chat"})
		return
	}

	if req.Stream {
		h.stream(c, sessionID, req.Model, events)
		return
	}
	h.aggregate(c, sessionID, req.Model, events)
}

// aggregate consumes the full event stream and returns one chat.completion.
func (h *Handlers) aggregate(c *gin.Context, sessionID, modelName string, events <-chan *event.Event) {
	var (
		activity []toolActivity
		seenCall = map[string]bool{}
	)
	sink := eventSink{
		onToolCall: func(id, name string, args json.RawMessage) {
			if !markSeen(seenCall, id, name) {
				return
			}
			activity = append(activity, toolActivity{Object: "tool.call", Name: name, Arguments: args})
		},
		onToolResult: func(name string, result json.RawMessage, errStr string) {
			activity = append(activity, toolActivity{Object: "tool.result", Name: name, Result: result, Error: errStr})
		},
	}

	text, err := consume(c.Request.Context(), events, sink)
	if err != nil {
		// The run failed before producing a usable reply.
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, chatCompletionResponse{
		ID:        "chatcmpl-" + sessionID,
		Object:    model.ObjectTypeChatCompletion,
		Created:   time.Now().Unix(),
		Model:     modelName,
		SessionID: sessionID,
		Choices: []respChoice{{
			Index:        0,
			Message:      respMessage{Role: string(model.RoleAssistant), Content: text},
			FinishReason: "stop",
		}},
		ToolActivity: activity,
	})
}

// stream consumes the event stream and writes SSE.
func (h *Handlers) stream(c *gin.Context, sessionID, modelName string, events <-chan *event.Event) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// Disable proxy/Nginx buffering so chunks flush to the client immediately.
	c.Header("X-Accel-Buffering", "no")

	flusher, canFlush := c.Writer.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// First event hands the client the resolved session id for follow-up turns.
	writeSSE(c, map[string]any{"object": "session", "session_id": sessionID})
	flush()

	seenCall := map[string]bool{}
	sink := eventSink{
		onText: func(delta string) {
			writeSSE(c, gin.H{
				"object":     model.ObjectTypeChatCompletionChunk,
				"session_id": sessionID,
				"model":      modelName,
				"choices": []gin.H{{
					"index": 0,
					"delta": gin.H{"content": delta},
				}},
			})
			flush()
		},
		onToolCall: func(id, name string, args json.RawMessage) {
			if !markSeen(seenCall, id, name) {
				return
			}
			writeSSE(c, toolActivity{Object: "tool.call", Name: name, Arguments: args})
			flush()
		},
		onToolResult: func(name string, result json.RawMessage, errStr string) {
			writeSSE(c, toolActivity{Object: "tool.result", Name: name, Result: result, Error: errStr})
			flush()
		},
	}

	if _, err := consume(c.Request.Context(), events, sink); err != nil {
		// Surface a terminal error event; the text stream already flushed
		// whatever arrived before the failure.
		writeSSE(c, gin.H{"object": model.ObjectTypeError, "error": err.Error()})
		flush()
	}

	// Standard OpenAI stream terminator.
	c.Writer.WriteString("data: [DONE]\n\n")
	flush()
}

// writeSSE marshals v and writes it as a single SSE `data:` frame.
func writeSSE(c *gin.Context, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.Writer.WriteString("data: ")
	c.Writer.Write(b)
	c.Writer.WriteString("\n\n")
}

// markSeen records (id|name) and reports whether this is the first sighting.
func markSeen(seen map[string]bool, id, name string) bool {
	key := id
	if key == "" {
		key = name
	}
	if seen[key] {
		return false
	}
	seen[key] = true
	return true
}

// --- event consumer ---

type eventSink struct {
	onText       func(delta string)
	onToolCall   func(id, name string, args json.RawMessage)
	onToolResult func(name string, result json.RawMessage, errStr string)
}

// consume drives the event channel, invoking sink callbacks for text deltas,
// tool calls, and tool results, and returns the aggregated assistant text. It
// honours ctx cancellation (client disconnect) and drains on cancel. A model-
// level error (Response.Error) aborts with an error; a tool-level error is
// surfaced via onToolResult and does NOT abort the turn.
func consume(ctx context.Context, events <-chan *event.Event, sink eventSink) (string, error) {
	var (
		sb       strings.Builder
		sawDelta bool
		fullText string
		runErr   error
	)

	for {
		select {
		case <-ctx.Done():
			// Drain remaining events without blocking so the producer can exit.
			drain(events)
			return sb.String(), context.Canceled
		case ev, ok := <-events:
			if !ok {
				if sawDelta {
					return sb.String(), runErr
				}
				return fullText, runErr
			}
			if ev == nil || ev.Response == nil {
				continue
			}
			if ev.Response.Error != nil {
				runErr = errors.New(ev.Response.Error.Message)
				// Keep draining so the runner goroutine can finish.
				continue
			}
			for i := range ev.Response.Choices {
				ch := ev.Response.Choices[i]

				// Tool-call requests (assistant message carrying tool_calls).
				for _, tc := range ch.Message.ToolCalls {
					if tc.Function.Name == "" {
						continue
					}
					if sink.onToolCall != nil {
						sink.onToolCall(tc.ID, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
					}
				}

				// Tool result (a tool-role message with the tool output).
				if ch.Message.Role == model.RoleTool && sink.onToolResult != nil {
					result, errStr := splitToolContent(ch.Message.Content)
					sink.onToolResult(ch.Message.ToolName, result, errStr)
				}

				// Streaming text delta.
				if ch.Delta.Content != "" {
					sawDelta = true
					sb.WriteString(ch.Delta.Content)
					if sink.onText != nil {
						sink.onText(ch.Delta.Content)
					}
				}

				// Final consolidated assistant content (used only if no deltas
				// were streamed, e.g. a non-streaming model).
				if ch.Message.Role == model.RoleAssistant && ch.Message.Content != "" {
					fullText = ch.Message.Content
				}
			}
		}
	}
}

// splitToolContent decides whether the tool message content is a JSON result or
// a plain error string.
func splitToolContent(content string) (json.RawMessage, string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, ""
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), ""
	}
	return nil, trimmed
}

// drain consumes any buffered events without processing them.
func drain(events <-chan *event.Event) {
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		default:
			return
		}
	}
}
