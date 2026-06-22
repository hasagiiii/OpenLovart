package agent

import (
	"context"

	"github.com/google/uuid"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/jiantaoli/openlovart/backend/internal/business/aiimage"
)

// Tool names exposed to the model (and surfaced in tool-activity events).
const (
	ToolGenerateImage = "generate_image"
	ToolEditImage     = "edit_image"
	ToolWebSearch     = "web_search"
)

// --- tool I/O shapes (schemas auto-generated from struct tags) ---

type generateImageInput struct {
	Prompt string `json:"prompt" jsonschema:"required,description=Text prompt describing the image to generate"`
	Size   string `json:"size,omitempty" jsonschema:"description=Optional image size, e.g. 1024x1024"`
	N      int    `json:"n,omitempty" jsonschema:"description=Number of images to generate (default 1)"`
}

type editImageInput struct {
	Prompt         string `json:"prompt" jsonschema:"required,description=Instruction describing how to edit the reference image"`
	ReferenceImage string `json:"reference_image" jsonschema:"required,description=URL or data URI of the source image to edit"`
	Size           string `json:"size,omitempty" jsonschema:"description=Optional output image size, e.g. 1024x1024"`
}

type imageOut struct {
	URL             string `json:"url"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	ContentType     string `json:"content_type"`
	CanvasElementID string `json:"canvas_element_id,omitempty"`
}

type imagesOutput struct {
	Images []imageOut `json:"images"`
}

type webSearchInput struct {
	Query string `json:"query" jsonschema:"required,description=The web search query"`
}

type searchHit struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type webSearchOutput struct {
	Results []searchHit `json:"results"`
}

// buildTools constructs the registered tool set. web_search is only included
// when a search provider is configured.
func (rt *Runtime) buildTools() []tool.Tool {
	tools := []tool.Tool{
		function.NewFunctionTool(
			rt.generateImage,
			function.WithName(ToolGenerateImage),
			function.WithDescription("Generate a new image from a text prompt (text-to-image)."),
		),
		function.NewFunctionTool(
			rt.editImage,
			function.WithName(ToolEditImage),
			function.WithDescription("Edit an existing image given a reference image and an instruction (image-to-image)."),
		),
	}
	if rt.search != nil {
		tools = append(tools, function.NewFunctionTool(
			rt.webSearch,
			function.WithName(ToolWebSearch),
			function.WithDescription("Search the web for current or factual information and return the top results."),
		))
	}
	return tools
}

func (rt *Runtime) generateImage(ctx context.Context, in generateImageInput) (imagesOutput, error) {
	tctx, cancel := context.WithTimeout(ctx, rt.toolTimeout)
	defer cancel()

	res, err := rt.images.Generate(tctx, aiimage.Params{
		Prompt: in.Prompt,
		Size:   in.Size,
		N:      in.N,
	})
	if err != nil {
		return imagesOutput{}, err
	}
	return rt.persistResult(tctx, res), nil
}

func (rt *Runtime) editImage(ctx context.Context, in editImageInput) (imagesOutput, error) {
	tctx, cancel := context.WithTimeout(ctx, rt.toolTimeout)
	defer cancel()

	res, err := rt.images.Edit(tctx, aiimage.Params{
		Prompt:         in.Prompt,
		Size:           in.Size,
		ReferenceImage: in.ReferenceImage,
	})
	if err != nil {
		return imagesOutput{}, err
	}
	return rt.persistResult(tctx, res), nil
}

// persistResult maps the provider result to the tool output, persisting each
// image as a canvas element when the request scope carries an owned project.
// Persistence is best-effort: a failure (e.g. non-owned project) just omits
// the element id, the URL is still returned.
func (rt *Runtime) persistResult(ctx context.Context, res aiimage.Result) imagesOutput {
	scope := scopeFromContext(ctx)
	out := imagesOutput{Images: make([]imageOut, 0, len(res.Images))}
	for i, img := range res.Images {
		o := imageOut{
			URL:         img.URL,
			Width:       img.Width,
			Height:      img.Height,
			ContentType: img.ContentType,
		}
		if rt.canvas != nil && scope != nil && scope.ProjectID != uuid.Nil {
			if id, err := rt.canvas.PersistImage(ctx, scope.UserID, scope.ProjectID, img, i); err == nil {
				o.CanvasElementID = id
			}
		}
		out.Images = append(out.Images, o)
	}
	return out
}

func (rt *Runtime) webSearch(ctx context.Context, in webSearchInput) (webSearchOutput, error) {
	tctx, cancel := context.WithTimeout(ctx, rt.toolTimeout)
	defer cancel()

	hits, err := rt.search.Search(tctx, in.Query)
	if err != nil {
		return webSearchOutput{}, err
	}
	out := webSearchOutput{Results: make([]searchHit, 0, len(hits))}
	for _, h := range hits {
		out.Results = append(out.Results, searchHit{
			Title:       h.Title,
			URL:         h.URL,
			Description: h.Description,
		})
	}
	return out, nil
}
