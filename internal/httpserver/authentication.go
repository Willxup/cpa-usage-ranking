package httpserver

import (
	"errors"
	"net/http"

	"cpa-usage-ranking/internal/auth"
	"cpa-usage-ranking/internal/clients"
	"github.com/gin-gonic/gin"
)

const authenticatedClientKey = "cpa-usage-ranking.authenticated-client"

// ClientAuthentication is an internal building block, not a login/session API.
func ClientAuthentication(authenticator *auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := authenticator.Authenticate(c.Request.Context(), c.Request.Header)
		switch {
		case errors.Is(err, auth.ErrInvalidCredential):
			c.Header("WWW-Authenticate", "Bearer")
			writeError(c, http.StatusUnauthorized, "invalid_credential", "Invalid client credential")
		case errors.Is(err, auth.ErrClientDisabled):
			writeError(c, http.StatusForbidden, "client_disabled", "Client is disabled")
		case err != nil:
			writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		default:
			c.Set(authenticatedClientKey, client)
			c.Next()
		}
	}
}

// AuthenticatedClient reads only middleware-established identity, never input IDs.
func AuthenticatedClient(c *gin.Context) (clients.Client, bool) {
	value, ok := c.Get(authenticatedClientKey)
	if !ok {
		return clients.Client{}, false
	}
	client, ok := value.(clients.Client)
	return client, ok
}
