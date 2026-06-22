package aiimage

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
)

// Handlers serves the async image endpoints (submit / status / result) under
// the authenticated, CSRF-protected /api/ai group.
type Handlers struct {
	svc *Service
}

// NewHandlers constructs the image handlers over the image Service.
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

// submitRequest is the typed core accepted by POST /api/ai/images.
type submitRequest struct {
	Prompt          string         `json:"prompt"`
	Size            string         `json:"size"`
	N               int            `json:"n"`
	ReferenceImage  string         `json:"reference_image"`
	ProviderOptions map[string]any `json:"provider_options"`
}

// Submit handles POST /api/ai/images: validate, create a job, start async work.
func (h *Handlers) Submit(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	var req submitRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	job, err := h.svc.Submit(c.Request.Context(), user.ID, Params{
		Prompt:          req.Prompt,
		Size:            req.Size,
		N:               req.N,
		ReferenceImage:  req.ReferenceImage,
		ProviderOptions: req.ProviderOptions,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to submit"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"request_id": job.ID,
		"status":     job.Status,
	})
}

// Status handles GET /api/ai/images/:id/status.
func (h *Handlers) Status(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	job, err := h.svc.Status(c.Request.Context(), c.Param("id"), user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	body := gin.H{"request_id": job.ID, "status": job.Status}
	if job.Status == StatusFailed && job.Error != "" {
		body["error"] = job.Error
	}
	c.JSON(http.StatusOK, body)
}

// Result handles GET /api/ai/images/:id.
func (h *Handlers) Result(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	job, err := h.svc.Status(c.Request.Context(), c.Param("id"), user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	if job.Status != StatusCompleted {
		c.JSON(http.StatusConflict, gin.H{"error": "not_ready", "status": job.Status})
		return
	}

	images := []Image{}
	if job.Result != nil {
		images = job.Result.Images
	}
	c.JSON(http.StatusOK, gin.H{"images": images})
}
