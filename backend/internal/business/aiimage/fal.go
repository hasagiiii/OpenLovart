package aiimage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"
)

// defaultFalBaseURL is fal's queue API root. Submit POSTs to
// "{base}/{model}"; the submit response carries absolute status/response URLs
// that we then poll and fetch.
const defaultFalBaseURL = "https://queue.fal.run"

const defaultFalPollInterval = 1 * time.Second

// falProvider is the fal queue-API adapter implementing Provider. Generation
// and editing share one submit→poll→fetch flow; a non-empty reference image is
// forwarded to the model as `image_url`, which routes fal to image-to-image.
type falProvider struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	model        string
	pollInterval time.Duration
}

// FalOption configures a falProvider.
type FalOption func(*falProvider)

// WithFalHTTPClient injects an HTTP client (test seam / custom transport).
func WithFalHTTPClient(c *http.Client) FalOption {
	return func(p *falProvider) { p.httpClient = c }
}

// WithFalBaseURL overrides the fal queue base URL (test seam).
func WithFalBaseURL(u string) FalOption {
	return func(p *falProvider) { p.baseURL = strings.TrimRight(u, "/") }
}

// WithFalPollInterval sets the status poll interval.
func WithFalPollInterval(d time.Duration) FalOption {
	return func(p *falProvider) { p.pollInterval = d }
}

// NewFalProvider constructs a fal Provider for the given model (e.g.
// "fal-ai/flux/dev"). apiKey is the FAL key.
func NewFalProvider(apiKey, model string, opts ...FalOption) Provider {
	p := &falProvider{
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		baseURL:      defaultFalBaseURL,
		apiKey:       apiKey,
		model:        strings.Trim(model, "/"),
		pollInterval: defaultFalPollInterval,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Generate implements Provider (text-to-image).
func (p *falProvider) Generate(ctx context.Context, params Params) (Result, error) {
	return p.run(ctx, params)
}

// Edit implements Provider (image-to-image). The reference image is forwarded
// as `image_url`; callers ensure params.ReferenceImage is non-empty.
func (p *falProvider) Edit(ctx context.Context, params Params) (Result, error) {
	return p.run(ctx, params)
}

// falSubmitResponse is fal's queue-submit acknowledgement.
type falSubmitResponse struct {
	RequestID   string `json:"request_id"`
	Status      string `json:"status"`
	StatusURL   string `json:"status_url"`
	ResponseURL string `json:"response_url"`
}

// falStatusResponse is the queue status poll body.
type falStatusResponse struct {
	Status      string `json:"status"`
	ResponseURL string `json:"response_url"`
	Error       string `json:"error"`
}

// falImage is fal's per-image result entry.
type falImage struct {
	URL         string `json:"url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
}

// falResultResponse is the final queue result body.
type falResultResponse struct {
	Images []falImage `json:"images"`
}

// run executes the full submit → poll → fetch flow.
func (p *falProvider) run(ctx context.Context, params Params) (Result, error) {
	sub, err := p.submit(ctx, params)
	if err != nil {
		return Result{}, err
	}

	responseURL, err := p.poll(ctx, sub)
	if err != nil {
		return Result{}, err
	}

	return p.fetch(ctx, responseURL)
}

// submit POSTs the generation request to the fal queue.
func (p *falProvider) submit(ctx context.Context, params Params) (falSubmitResponse, error) {
	payload := map[string]any{}
	maps.Copy(payload, params.ProviderOptions)
	payload["prompt"] = params.Prompt
	if params.Size != "" {
		payload["image_size"] = params.Size
	}
	if params.N > 0 {
		payload["num_images"] = params.N
	}
	if params.ReferenceImage != "" {
		payload["image_url"] = params.ReferenceImage
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return falSubmitResponse{}, err
	}

	url := p.baseURL + "/" + p.model
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return falSubmitResponse{}, err
	}
	p.setHeaders(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return falSubmitResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return falSubmitResponse{}, fmt.Errorf("aiimage: fal submit failed: %s", statusError(resp))
	}

	var out falSubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return falSubmitResponse{}, fmt.Errorf("aiimage: decode fal submit: %w", err)
	}
	return out, nil
}

// poll loops on the status URL until COMPLETED (returning the response URL) or
// FAILED, honouring ctx cancellation/timeout.
func (p *falProvider) poll(ctx context.Context, sub falSubmitResponse) (string, error) {
	statusURL := sub.StatusURL
	responseURL := sub.ResponseURL
	if statusURL == "" {
		// Nothing to poll; assume the response URL is ready.
		if responseURL == "" {
			return "", fmt.Errorf("aiimage: fal returned no status_url")
		}
		return responseURL, nil
	}

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return "", err
		}
		p.setHeaders(req)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		st, err := decodeStatus(resp)
		if err != nil {
			return "", err
		}

		if st.ResponseURL != "" {
			responseURL = st.ResponseURL
		}

		switch Status(strings.ToUpper(st.Status)) {
		case StatusCompleted:
			if responseURL == "" {
				return "", fmt.Errorf("aiimage: fal completed without response_url")
			}
			return responseURL, nil
		case StatusFailed:
			msg := st.Error
			if msg == "" {
				msg = "fal job failed"
			}
			return "", fmt.Errorf("aiimage: %s", msg)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

// fetch retrieves and maps the final result.
func (p *falProvider) fetch(ctx context.Context, responseURL string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, responseURL, nil)
	if err != nil {
		return Result{}, err
	}
	p.setHeaders(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("aiimage: fal result failed: %s", statusError(resp))
	}

	var out falResultResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("aiimage: decode fal result: %w", err)
	}

	images := make([]Image, 0, len(out.Images))
	for _, im := range out.Images {
		images = append(images, Image{
			URL:         im.URL,
			Width:       im.Width,
			Height:      im.Height,
			ContentType: im.ContentType,
		})
	}
	if len(images) == 0 {
		return Result{}, fmt.Errorf("aiimage: fal returned no images")
	}
	return Result{Images: images}, nil
}

func (p *falProvider) setHeaders(req *http.Request) {
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Key "+p.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// decodeStatus reads a status response body, mapping HTTP errors.
func decodeStatus(resp *http.Response) (falStatusResponse, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return falStatusResponse{}, fmt.Errorf("aiimage: fal status failed: %s", statusError(resp))
	}
	var st falStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return falStatusResponse{}, fmt.Errorf("aiimage: decode fal status: %w", err)
	}
	return st, nil
}

// statusError extracts a short error string from a non-2xx response.
func statusError(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return resp.Status
	}
	return fmt.Sprintf("%s: %s", resp.Status, msg)
}
