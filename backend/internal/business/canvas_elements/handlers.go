package canvas_elements

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"

	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// Handlers exposes the canvas-element endpoints over HTTP.
type Handlers struct{ repo *Repo }

// NewHandlers builds a Handlers bound to repo.
func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

type elementDTO struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	ElementData datatypes.JSON `json:"element_data"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func toDTO(e *models.CanvasElement) elementDTO {
	return elementDTO{
		ID:          e.ID.String(),
		ProjectID:   e.ProjectID.String(),
		ElementData: e.ElementData,
		CreatedAt:   e.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt:   e.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}

// List is `GET /api/projects/:id/canvas-elements`.
func (h *Handlers) List(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	rows, err := h.repo.ListByProject(c.Request.Context(), projectID, user.ID)
	if err != nil {
		if IsNotFound(err) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	items := make([]elementDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toDTO(&rows[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type replaceRequest struct {
	Elements []ElementInput `json:"elements" binding:"required"`
}

// Replace is `PUT /api/projects/:id/canvas-elements`.
//
// Body shape: `{ "elements": [{ "element_data": {...} }, ...] }`.
// An empty array is valid and clears the project's canvas.
func (h *Handlers) Replace(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var req replaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}
	rows, err := h.repo.ReplaceAllForProject(c.Request.Context(), projectID, user.ID, req.Elements)
	if err != nil {
		if IsNotFound(err) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	items := make([]elementDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toDTO(&rows[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
