package httpserver

import (
	"errors"
	"net/http"

	"cpa-usage-ranking/internal/auth"
	"cpa-usage-ranking/internal/clients"
	"github.com/gin-gonic/gin"
)

func MountWithdraw(router *gin.Engine, repo *clients.Repository, requestRefresh func()) {
	mountExact(router, http.MethodPost, "/v1/clients/withdraw", func(c *gin.Context) {
		hash, err := auth.BearerHash(c.Request.Header)
		if err != nil {
			writeInvalidCredential(c)
			return
		}
		if c.Request.ContentLength != 0 || c.Request.URL.RawQuery != "" {
			writeError(c, http.StatusBadRequest, "invalid_request", "Withdrawal accepts no body or query")
			return
		}
		changed, err := repo.Withdraw(c.Request.Context(), hash)
		switch {
		case errors.Is(err, clients.ErrNotFound):
			writeInvalidCredential(c)
		case err != nil:
			writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		default:
			// 确认退出只代表落库。重复退出不打断正在进行的缓存刷新。
			if changed && requestRefresh != nil {
				requestRefresh()
			}
			c.Status(http.StatusNoContent)
		}
	})
}
