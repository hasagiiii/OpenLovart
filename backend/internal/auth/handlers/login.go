package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type loginRequest struct {
	Email    string `json:"email" binding:"required,email,max=320"`
	Password string `json:"password" binding:"required,min=1,max=256"`
}

// Login authenticates the user and writes the credential cookies. Returns
// 401 invalid_credentials on every failure path (anti-enumeration).
func Login(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeErr(c, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		user, issued, err := d.Service.Login(
			c.Request.Context(),
			req.Email, req.Password,
			c.Request.UserAgent(), c.ClientIP(),
		)
		if err != nil {
			status, code := mapServiceError(err)
			writeErr(c, status, code, "")
			return
		}
		setCredentialCookies(c, d, issued)
		c.JSON(http.StatusOK, gin.H{
			"ok":   true,
			"user": presentUser(user),
		})
	}
}
