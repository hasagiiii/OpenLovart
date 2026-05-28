package handlers

import (
	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/oidc"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/config"
)

// Deps bundles every collaborator the auth handlers need. We pass it by
// value into the per-endpoint constructor functions (Register, Login, …)
// so the call sites stay short and test-friendly.
type Deps struct {
	Cfg          *config.Config
	Service      *service.Service
	Cookies      *cookies.Manager
	GoogleVerif  *oidc.GoogleVerifier
	OIDCSigner   *oidc.StateSigner
}
