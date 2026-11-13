package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"cpa-usage-ranking/internal/admin"
	"github.com/gin-gonic/gin"
)

func adminIdentity(c *gin.Context) admin.Identity {
	return c.MustGet("admin_session").(admin.Session).Identity
}

func (h *adminHTTP) ownerOnly(c *gin.Context) {
	if !adminIdentity(c).Owner {
		writeError(c, http.StatusForbidden, "owner_required", "Owner permission required")
		return
	}
	c.Next()
}

func (h *adminHTTP) accounts(c *gin.Context) {
	owner, accounts, err := h.service.Accounts(c.Request.Context(), adminIdentity(c))
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"owner": owner, "accounts": accounts})
}

func (h *adminHTTP) createAccount(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeAdmin(c, &input) {
		return
	}
	account, err := h.service.CreateAccount(c.Request.Context(), adminIdentity(c), input.Username, input.Password)
	if err != nil {
		h.adminError(c, err)
		return
	}
	h.logger.Info("admin action", "actor", adminIdentity(c).ID, "action", "create_account", "target", account.ID, "result", "ok")
	c.JSON(http.StatusCreated, account)
}

func (h *adminHTTP) deleteAccount(c *gin.Context) {
	if c.Param("id") == "owner" {
		h.adminError(c, admin.ErrForbidden)
		return
	}
	id, err := positiveAdminID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	if err := h.service.DeleteAccount(c.Request.Context(), adminIdentity(c), id); err != nil {
		h.adminError(c, err)
		return
	}
	h.logger.Info("admin action", "actor", adminIdentity(c).ID, "action", "delete_account", "target", c.Param("id"), "result", "ok")
	c.Status(http.StatusNoContent)
}

func positiveAdminID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, admin.ErrInput
	}
	return id, nil
}

func (h *adminHTTP) adminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, admin.ErrInput):
		writeError(c, http.StatusBadRequest, "invalid_request", "Invalid request")
	case errors.Is(err, admin.ErrSession):
		writeError(c, http.StatusUnauthorized, "session_required", "Administrator login required")
	case errors.Is(err, admin.ErrForbidden):
		writeError(c, http.StatusForbidden, "permission_denied", "Permission denied")
	case errors.Is(err, admin.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "Target not found")
	case errors.Is(err, admin.ErrConflict):
		writeError(c, http.StatusConflict, "username_unavailable", "Username unavailable")
	default:
		h.logger.Error("admin action failed", "actor", adminIdentity(c).ID, "action", c.Request.Method+" "+c.FullPath(), "result", "error")
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
	}
}
