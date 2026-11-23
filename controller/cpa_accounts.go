package controller

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ListCPAAccounts(c *gin.Context) {
	accounts, err := service.ListCPAAccounts(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, accounts)
}

func ChangeCPAAccount(c *gin.Context) {
	var input struct {
		ID       string `json:"id"`
		Pool     string `json:"pool"`
		Limit    *int   `json:"limit"`
		Disabled *bool  `json:"disabled"`
	}
	if c.ShouldBindJSON(&input) != nil || input.ID == "" || len(input.ID) > 512 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	ctx := c.Request.Context()
	items, err := service.ListCPAAccounts(ctx)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var account *service.CPAAccount
	for i := range items {
		if items[i].ID == input.ID {
			account = &items[i]
			break
		}
	}
	if account == nil {
		c.Status(404)
		return
	}
	path := "credentials/status"
	body := map[string]any{"name": input.ID}
	if input.Disabled != nil {
		body["disabled"] = *input.Disabled
	} else {
		if account.Limit == nil {
			common.ApiErrorT(c, "CPA account limits require the matching CPA build")
			return
		}
		if input.Limit == nil || *input.Limit < 0 || *input.Limit > 1000000 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		switch input.Pool {
		case "unassigned", "plus", "pro", "degraded":
		default:
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		path = "credentials/fields"
		body["account_concurrency"] = *input.Limit
		body["account_pool"] = input.Pool
	}
	if err = service.CallCPAManagement(ctx, "PATCH", path, body, nil); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.update", map[string]any{"source": "cpa_account", "account_id": input.ID, "operation": path})
	common.ApiSuccess(c, nil)
}

func CPAAccountLogin(c *gin.Context) {
	identity, ok := pluginLoginIdentity(c)
	if !ok {
		return
	}
	var input struct {
		Flow     string `json:"flow"`
		Callback string `json:"callback"`
	}
	if c.ShouldBindJSON(&input) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	ctx := c.Request.Context()
	match := model.AuthFlowMatch{Purpose: "cpa_account_login", Provider: "codex", UserId: identity.UserID, SessionId: identity.SessionID}
	if input.Flow == "" {
		if input.Callback != "" {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		var start struct {
			URL   string `json:"url"`
			State string `json:"state"`
		}
		if err := service.CallCPAManagement(ctx, "POST", "oauth/auth-url", map[string]any{"provider": "codex"}, &start); err != nil {
			common.ApiError(c, err)
			return
		}
		u, err := url.Parse(start.URL)
		if err != nil || u.Scheme != "https" || u.Host != "auth.openai.com" || u.User != nil || start.State == "" || u.Query().Get("state") != start.State {
			common.ApiErrorT(c, "Invalid authorization URL")
			return
		}
		token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: match.Purpose, Provider: match.Provider, UserId: match.UserId, SessionId: match.SessionId, Payload: start.State, ExpiresAt: time.Now().Add(10 * time.Minute)})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"flow": token, "url": start.URL})
		return
	}
	flow, err := model.GetAuthFlow(input.Flow, match)
	if err != nil {
		common.ApiErrorT(c, "Invalid or expired authorization")
		return
	}
	if input.Callback != "" {
		u, err := url.Parse(strings.TrimSpace(input.Callback))
		if err != nil || u.Query().Get("state") != flow.Payload || u.Query().Get("code") == "" {
			common.ApiErrorT(c, "Invalid callback URL")
			return
		}
		if err := service.CallCPAManagement(ctx, "POST", "oauth/callback", map[string]string{"provider": "codex", "state": flow.Payload, "code": u.Query().Get("code")}, nil); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := service.CallCPAManagement(ctx, http.MethodGet, "oauth/status?state="+url.QueryEscape(flow.Payload), nil, &status); err != nil {
		common.ApiError(c, err)
		return
	}
	if status.Status != "wait" {
		_, err = model.ConsumeAuthFlow(input.Flow, match)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if status.Status == "ok" {
		recordManageAudit(c, "channel.update", map[string]any{"source": "cpa_account_login"})
	}
	common.ApiSuccess(c, status)
}
