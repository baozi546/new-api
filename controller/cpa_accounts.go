package controller

import (
	"net/http"
	"net/url"
	"os"
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

func ListCPAChannelIDs(c *gin.Context) {
	ids := make([]int, 0)
	endpoint := service.CPAChannelEndpoint(os.Getenv("CPA_MANAGEMENT_URL"))
	if endpoint == "" {
		common.ApiSuccess(c, ids)
		return
	}
	endpoints := map[string]bool{endpoint: true}
	for _, alias := range strings.Split(os.Getenv("CPA_RELAY_URLS"), ",") {
		if normalized := service.CPAChannelEndpoint(alias); normalized != "" {
			endpoints[normalized] = true
		}
	}
	var channels []struct {
		Id      int
		BaseURL string `gorm:"column:base_url"`
	}
	err := model.DB.WithContext(c.Request.Context()).Model(&model.Channel{}).Select("id", "base_url").Where("base_url IS NOT NULL AND base_url <> ?", "").Order("id").Find(&channels).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, ch := range channels {
		if endpoints[service.CPAChannelEndpoint(ch.BaseURL)] {
			ids = append(ids, ch.Id)
		}
	}
	common.ApiSuccess(c, ids)
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
		if !account.PoolManagedLimit {
			body["account_concurrency"] = *input.Limit
		}
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
		Flow      string `json:"flow"`
		Callback  string `json:"callback"`
		ChannelID int    `json:"channel_id"`
		Pool      string `json:"pool"`
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
		body := map[string]string{"provider": "codex"}
		if input.ChannelID != 0 {
			ch, err := model.GetChannelById(input.ChannelID, false)
			if err != nil || ch.CPAPoolID == "" || !matchesCPAChannel(ch) {
				common.ApiErrorT(c, "Invalid parameters")
				return
			}
			body["channel_pool_id"] = ch.CPAPoolID
		}
		if input.Pool != "" {
			switch input.Pool {
			case "unassigned", "plus", "pro", "degraded":
				body["account_pool"] = input.Pool
			default:
				common.ApiErrorT(c, "Invalid parameters")
				return
			}
		}
		var start struct {
			URL         string `json:"url"`
			State       string `json:"state"`
			Pool        string `json:"account_pool"`
			ChannelPool string `json:"channel_pool_id"`
		}
		if err := service.CallCPAManagement(ctx, "POST", "oauth/auth-url", body, &start); err != nil {
			common.ApiError(c, err)
			return
		}
		if (input.Pool != "" && start.Pool != input.Pool) || start.ChannelPool != body["channel_pool_id"] {
			common.ApiErrorT(c, "Selecting a pool at sign-in requires the matching CPA build")
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
	if input.Pool != "" || input.ChannelID != 0 {
		common.ApiErrorT(c, "Invalid parameters")
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
		Error  string `json:"error"`
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
	if status.Error == "Account already belongs to another channel pool" {
		common.ApiSuccess(c, gin.H{"status": "pool_conflict"})
		return
	}
	common.ApiSuccess(c, gin.H{"status": status.Status})
}

func ChannelPoolAccountLimit(c *gin.Context) {
	ch := inspectionTarget(c)
	if ch == nil {
		return
	}
	path := "channel-pools/" + ch.CPAPoolID + "/account-limit"
	var result struct {
		Limit *int `json:"limit"`
	}
	method := http.MethodGet
	var body any
	if c.Request.Method == http.MethodPost {
		var in struct {
			Limit *int `json:"limit"`
		}
		if c.ShouldBindJSON(&in) != nil || in.Limit == nil || *in.Limit < 0 || *in.Limit > 1000000 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		body = in
		method = http.MethodPut
	}
	if err := service.CallCPAManagement(c.Request.Context(), method, path, body, &result); err != nil {
		common.ApiError(c, err)
		return
	}
	if method == http.MethodPut {
		recordManageAudit(c, "channel.update", map[string]any{"source": "pool_account_limit", "channel_id": ch.Id})
	}
	common.ApiSuccess(c, result)
}
