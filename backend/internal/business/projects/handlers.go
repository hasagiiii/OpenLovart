package projects

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// Pagination knobs.
const (
	defaultLimit = 20
	maxLimit     = 100
)

// Handlers wires the Repo into HTTP. Construct one with NewHandlers and
// mount each method on the router (see internal/httpserver/router.go).
type Handlers struct {
	repo *Repo
}

// NewHandlers builds a Handlers bound to repo.
func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// projectDTO is the wire shape returned by the single-object endpoints.
// Field names match the previous Supabase-shaped response so the frontend
// did not need a global rename for unrelated keys.
type projectDTO struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	Title     string  `json:"title"`
	Thumbnail *string `json:"thumbnail"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func toDTO(p *models.Project) projectDTO {
	return projectDTO{
		ID:        p.ID.String(),
		UserID:    p.UserID.String(),
		Title:     p.Title,
		Thumbnail: p.Thumbnail,
		CreatedAt: p.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt: p.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}

// List is `GET /api/projects`. Cursor-paginated; response shape is
// `{items: [...], nextCursor: "..."|null}`.
func (h *Handlers) List(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	limit := defaultLimit
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_limit"})
			return
		}
		if n > maxLimit {
			n = maxLimit
		}
		limit = n
	}

	cursor, err := DecodeCursor(c.Query("cursor"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_cursor"})
		return
	}

	rows, next, err := h.repo.ListByUser(c.Request.Context(), user.ID, limit, cursor)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	items := make([]projectDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toDTO(&rows[i]))
	}

	var nextStr *string
	if next != nil {
		s := EncodeCursor(next.UpdatedAt, next.ID)
		nextStr = &s
	}
	c.JSON(http.StatusOK, gin.H{
		"items":      items,
		"nextCursor": nextStr,
	})
}

// Get is `GET /api/projects/:id`.
func (h *Handlers) Get(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	p, err := h.repo.Get(c.Request.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, toDTO(p))
}

type createRequest struct {
	Title     string  `json:"title" binding:"required,min=1,max=200"`
	Thumbnail *string `json:"thumbnail" binding:"omitempty,max=2048"`
}

// Create is `POST /api/projects`.
func (h *Handlers) Create(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}
	p, err := h.repo.Create(c.Request.Context(), user.ID, req.Title, req.Thumbnail)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusCreated, toDTO(p))
}

type updateRequest struct {
	Title     *string `json:"title" binding:"omitempty,min=1,max=200"`
	Thumbnail *string `json:"thumbnail" binding:"omitempty,max=2048"`
}

// Update is `PATCH /api/projects/:id`.
func (h *Handlers) Update(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}
	p, err := h.repo.Update(c.Request.Context(), id, user.ID, UpdatePatch{
		Title:     req.Title,
		Thumbnail: req.Thumbnail,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, toDTO(p))
}

// Delete is `DELETE /api/projects/:id`.
func (h *Handlers) Delete(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	if err := h.repo.Delete(c.Request.Context(), id, user.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
