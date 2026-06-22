// Package aiimage implements the backend image-generation capability: a
// fal-shaped asynchronous contract (submit → poll status → fetch result)
// backed by a provider-abstracted image service. The same service is reused
// by the chat agent's `generate_image` / `edit_image` tools via the
// synchronous Generate/Edit helpers.
package aiimage

import (
	"context"
	"errors"
)

// Status mirrors fal's job-status vocabulary so a future fal-native provider
// is a thin adapter and the wire contract is stable.
type Status string

const (
	StatusInQueue    Status = "IN_QUEUE"
	StatusInProgress Status = "IN_PROGRESS"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
)

// ErrNotFound is returned by the Store for an unknown id or one owned by a
// different user (the two are intentionally indistinguishable).
var ErrNotFound = errors.New("aiimage: not found")

// Image is a single produced image in the provider-agnostic result shape.
type Image struct {
	URL         string `json:"url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
}

// Result is the final output of a generation/edit job.
type Result struct {
	Images []Image `json:"images"`
}

// Params is the typed core accepted by both generation and editing. A
// non-empty ReferenceImage routes the request to image-to-image editing.
// ProviderOptions is an opaque passthrough for vendor-specific parameters.
type Params struct {
	Prompt          string
	Size            string
	N               int
	ReferenceImage  string
	ProviderOptions map[string]any
}

// Provider abstracts the underlying image vendor. The first implementation is
// a fal adapter; the interface keeps the wire contract independent of any one
// vendor and is shared by the async endpoints and the chat tools.
type Provider interface {
	// Generate performs text-to-image generation.
	Generate(ctx context.Context, p Params) (Result, error)
	// Edit performs image-to-image editing given p.ReferenceImage.
	Edit(ctx context.Context, p Params) (Result, error)
}
