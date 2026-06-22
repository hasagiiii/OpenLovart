// Package httpserver assembles the Gin engine: middleware stack and routes.
// It does not own the underlying net/http.Server lifecycle (that lives in
// cmd/server/main.go) so the engine can be reused in tests without binding
// to a real listener.
package httpserver

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	authhandlers "github.com/jiantaoli/openlovart/backend/internal/auth/handlers"
	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
	"github.com/jiantaoli/openlovart/backend/internal/auth/oidc"
	"github.com/jiantaoli/openlovart/backend/internal/auth/ratelimit"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/business/aichat"
	"github.com/jiantaoli/openlovart/backend/internal/business/aiimage"
	canvas "github.com/jiantaoli/openlovart/backend/internal/business/canvas_elements"
	"github.com/jiantaoli/openlovart/backend/internal/business/credits"
	"github.com/jiantaoli/openlovart/backend/internal/business/projects"
	"github.com/jiantaoli/openlovart/backend/internal/config"
	"github.com/jiantaoli/openlovart/backend/internal/httpserver/handlers"
	mw "github.com/jiantaoli/openlovart/backend/internal/middleware"
)

// Deps bundles every collaborator the router needs. Constructed in
// cmd/server/main.go.
type Deps struct {
	Cfg         *config.Config
	Log         *slog.Logger
	DB          *gorm.DB
	KeyStore    *jwt.KeyStore
	AuthService *service.Service
	Cookies     *cookies.Manager
	GoogleVerif *oidc.GoogleVerifier
	OIDCSigner  *oidc.StateSigner

	// AI generation collaborators. ChatRuntime drives the chat endpoint;
	// ImageService backs the async image endpoints. Both may be nil in tests
	// that do not exercise the AI routes (the routes are then not mounted).
	// ChatRuntime is an interface so tests can inject a fake event stream;
	// *agent.Runtime satisfies it.
	ChatRuntime  aichat.ChatRunner
	ImageService *aiimage.Service

	// Per-route limiters. nil values disable that limiter (useful in tests).
	LoginLimiter        ratelimit.Limiter
	RegisterLimiter     ratelimit.Limiter
	ForgotLimiter       ratelimit.Limiter
	OIDCCallbackLimiter ratelimit.Limiter
	VerifyResendLimiter ratelimit.Limiter
}

// New constructs a configured *gin.Engine using the supplied dependencies.
// The caller is expected to wrap it inside an *http.Server.
func New(d Deps) *gin.Engine {
	if d.Cfg.IsDev() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	// Globals applied to every route. Order matters: RequestID must come
	// before SlogAccessLog so the access log can read it; CORS short-circuits
	// preflights and must be before any auth-specific guard.
	engine.Use(gin.Recovery())
	engine.Use(mw.RequestID())
	engine.Use(mw.SlogAccessLog(d.Log))
	if d.Cfg.FrontendOrigin != "" {
		engine.Use(mw.CORS(d.Cfg.FrontendOrigin))
	}

	// Health probes. /healthz is the canonical name; /api/health is a mirror
	// so the same path works through the Next.js dev rewrite without forcing
	// a separate rewrite entry.
	engine.GET("/healthz", handlers.Health(d.DB))
	engine.GET("/api/health", handlers.Health(d.DB))

	// Auth handler dependency bundle.
	authDeps := authhandlers.Deps{
		Cfg:         d.Cfg,
		Service:     d.AuthService,
		Cookies:     d.Cookies,
		GoogleVerif: d.GoogleVerif,
		OIDCSigner:  d.OIDCSigner,
	}

	// /api/auth — public + cookie-protected endpoints. CSRF middleware is
	// applied selectively below; the JWKS endpoint and other unauthenticated
	// reads are mounted ahead of any guard so they are never blocked.
	authGroup := engine.Group("/api/auth")
	{
		// Publicly cacheable: the JWKS document. Mounted FIRST and without
		// any auth/CSRF middleware so the frontend can fetch it both at
		// boot and on cold-start cache misses.
		authGroup.GET("/.well-known/jwks.json", authhandlers.JWKS(d.KeyStore))

		// Unauthenticated POSTs that mutate state but do NOT ride a cookie:
		// register, login, forgot-password, verify-email (idempotent token
		// consumption), oidc start, oidc callback. They are exempt from
		// CSRF because they do not present an access cookie; rate-limit per
		// IP / per email instead.
		authGroup.POST("/register",
			optionalLimit(d.RegisterLimiter, mw.PerIP),
			authhandlers.Register(authDeps),
		)
		authGroup.POST("/login",
			optionalLimit(d.LoginLimiter, mw.PerIP),
			authhandlers.Login(authDeps),
		)
		authGroup.POST("/forgot-password",
			optionalLimit(d.ForgotLimiter, mw.PerIP),
			authhandlers.ForgotPassword(authDeps),
		)
		authGroup.POST("/reset-password", authhandlers.ResetPassword(authDeps))
		authGroup.GET("/verify-email", authhandlers.VerifyEmail(authDeps))
		authGroup.POST("/verify-email", authhandlers.VerifyEmail(authDeps))

		authGroup.GET("/oidc/google/start", authhandlers.OIDCGoogleStart(authDeps))
		authGroup.GET("/oidc/google/callback",
			optionalLimit(d.OIDCCallbackLimiter, mw.PerIP),
			authhandlers.OIDCGoogleCallback(authDeps),
		)

		// Refresh: the call itself uses the refresh cookie (Path-scoped to
		// /api/auth/refresh), not the access cookie, so it is not subject to
		// the CSRF middleware (which only fires on access-cookie traffic).
		authGroup.POST("/refresh", authhandlers.Refresh(authDeps))

		// Logout: best-effort. Apply RequireAuth so a stale browser tab
		// still gets cookies cleared, but tolerate no-token via the handler.
		authGroup.POST("/logout", authhandlers.Logout(authDeps))

		// Authenticated reads/mutations under /api/auth.
		secured := authGroup.Group("")
		secured.Use(authmw.RequireAuth(d.DB, d.KeyStore, d.Cookies))
		secured.Use(mw.CSRF(mw.CSRFConfig{Cookies: d.Cookies}))
		{
			secured.GET("/me", authhandlers.Me(authDeps))
			secured.POST("/verify-email/resend",
				optionalLimit(d.VerifyResendLimiter, mw.PerIP),
				authhandlers.VerifyEmailResend(authDeps),
			)
		}
	}

	// /api — business endpoints. All require auth + CSRF (cookie path).
	api := engine.Group("/api")
	api.Use(authmw.RequireAuth(d.DB, d.KeyStore, d.Cookies))
	api.Use(mw.CSRF(mw.CSRFConfig{Cookies: d.Cookies}))
	{
		projectRepo := projects.NewRepo(d.DB)
		projectH := projects.NewHandlers(projectRepo)
		api.GET("/projects", projectH.List)
		api.POST("/projects", projectH.Create)
		api.GET("/projects/:id", projectH.Get)
		api.PATCH("/projects/:id", projectH.Update)
		api.DELETE("/projects/:id", projectH.Delete)

		canvasRepo := canvas.NewRepo(d.DB)
		canvasH := canvas.NewHandlers(canvasRepo)
		api.GET("/projects/:id/canvas-elements", canvasH.List)
		api.PUT("/projects/:id/canvas-elements", canvasH.Replace)

		creditsRepo := credits.NewRepo(d.DB)
		creditsH := credits.NewHandlers(creditsRepo)
		api.GET("/credits", creditsH.Get)

		// AI generation endpoints. Mounted only when their collaborators are
		// wired (tests that don't exercise AI can leave them nil).
		if d.ChatRuntime != nil {
			chatH := aichat.NewHandlers(d.ChatRuntime, d.Log)
			api.POST("/ai/chat/completions", chatH.Completions)
		}
		if d.ImageService != nil {
			imageH := aiimage.NewHandlers(d.ImageService)
			api.POST("/ai/images", imageH.Submit)
			api.GET("/ai/images/:id/status", imageH.Status)
			api.GET("/ai/images/:id", imageH.Result)
		}
	}

	return engine
}

// optionalLimit wires a rate-limiter only when one is supplied, so tests can
// disable specific limiters by passing nil without restructuring routes.
func optionalLimit(l ratelimit.Limiter, extract mw.KeyExtractor) gin.HandlerFunc {
	if l == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return mw.RateLimit(l, extract)
}
