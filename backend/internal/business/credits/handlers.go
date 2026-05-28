package credits

import (
	"net/http"

	"github.com/gin-gonic/gin"

	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
)

// Handlers exposes the credits endpoint.
type Handlers struct{ repo *Repo }

// NewHandlers builds a Handlers bound to repo.
func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// Get is `GET /api/credits`.
func (h *Handlers) Get(c *gin.Context) {
	user, ok := authmw.CurrentUser(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	row, err := h.repo.GetByUser(c.Request.Context(), user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":    row.UserID.String(),
		"credits":    row.Credits,
		"created_at": row.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"updated_at": row.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	})
}
