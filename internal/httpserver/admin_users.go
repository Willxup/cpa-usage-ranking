package httpserver

import (
	"net/http"
	"strconv"
	"time"

	"cpa-usage-ranking/internal/admin"
	"cpa-usage-ranking/internal/clients"
	"github.com/gin-gonic/gin"
)

type adminUser struct {
	ID            string    `json:"id"`
	DisplayName   string    `json:"display_name"`
	AvatarID      string    `json:"avatar_id"`
	Disabled      bool      `json:"disabled"`
	RankingHidden bool      `json:"ranking_hidden"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *adminHTTP) ban(c *gin.Context) {
	id, err := positiveAdminID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	var input struct {
		Disabled *bool `json:"disabled"`
	}
	if !decodeAdmin(c, &input) {
		return
	}
	if input.Disabled == nil {
		h.adminError(c, admin.ErrInput)
		return
	}
	if err := h.service.SetDisabled(c.Request.Context(), adminIdentity(c), id, *input.Disabled); err != nil {
		h.adminError(c, err)
		return
	}
	if h.options.RequestRefresh != nil {
		h.options.RequestRefresh()
	}
	h.logger.Info("admin action", "actor", adminIdentity(c).ID, "action", "set_ban", "target", c.Param("id"), "disabled", *input.Disabled, "result", "saved")
	c.JSON(http.StatusOK, gin.H{"disabled": *input.Disabled, "leaderboards": "refresh_requested"})
}

func (h *adminHTTP) rankingVisibility(c *gin.Context) {
	id, err := positiveAdminID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	var input struct {
		Hidden *bool `json:"hidden"`
	}
	if !decodeAdmin(c, &input) {
		return
	}
	if input.Hidden == nil {
		h.adminError(c, admin.ErrInput)
		return
	}
	if err := h.service.SetRankingHidden(c.Request.Context(), adminIdentity(c), id, *input.Hidden); err != nil {
		h.adminError(c, err)
		return
	}
	if h.options.RequestRefresh != nil {
		h.options.RequestRefresh()
	}
	h.logger.Info("admin action", "actor", adminIdentity(c).ID, "action", "set_ranking_visibility", "target", c.Param("id"), "hidden", *input.Hidden, "result", "saved")
	c.JSON(http.StatusOK, gin.H{"hidden": *input.Hidden, "leaderboards": "refresh_requested"})
}

func (h *adminHTTP) users(c *gin.Context) {
	query := clients.ListQuery{Limit: 50, Name: c.Query("name")}
	var err error
	if value, ok := c.GetQuery("id"); ok {
		query.ID, err = positiveAdminID(value)
		if err != nil {
			h.adminError(c, err)
			return
		}
	}
	if value, ok := c.GetQuery("after"); ok {
		query.After, err = positiveAdminID(value)
		if err != nil {
			h.adminError(c, err)
			return
		}
	}
	if value, ok := c.GetQuery("limit"); ok {
		query.Limit, err = strconv.Atoi(value)
		if err != nil || query.Limit < 1 || query.Limit > 100 {
			h.adminError(c, admin.ErrInput)
			return
		}
	}
	if values, ok := c.Request.URL.Query()["disabled"]; ok {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			h.adminError(c, admin.ErrInput)
			return
		}
		value := values[0] == "true"
		query.Disabled = &value
	}
	if values, ok := c.Request.URL.Query()["hidden"]; ok {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			h.adminError(c, admin.ErrInput)
			return
		}
		value := values[0] == "true"
		query.Hidden = &value
	}
	for key, values := range c.Request.URL.Query() {
		if len(values) != 1 || values[0] == "" || (key != "id" && key != "name" && key != "after" && key != "disabled" && key != "hidden" && key != "limit") {
			h.adminError(c, admin.ErrInput)
			return
		}
	}
	page, err := h.service.Users(c.Request.Context(), query)
	if err != nil {
		if err == clients.ErrQuery {
			err = admin.ErrInput
		}
		h.adminError(c, err)
		return
	}
	users := make([]adminUser, 0, len(page.Clients))
	for _, client := range page.Clients {
		users = append(users, adminUser{ID: strconv.FormatInt(client.ID, 10), DisplayName: client.DisplayName, AvatarID: strconv.FormatInt(client.AvatarID, 10), Disabled: client.Disabled, RankingHidden: client.RankingHidden, CreatedAt: client.CreatedAt})
	}
	next := ""
	if page.NextID != 0 {
		next = strconv.FormatInt(page.NextID, 10)
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "next_cursor": next})
}
