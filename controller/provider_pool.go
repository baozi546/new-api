package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func providerAccountHasIssue(account service.ProviderPoolAccount) bool {
	return account.State.Disabled ||
		account.NeedsLogin ||
		account.State.Degraded ||
		account.Profile.QuotaError != "" ||
		(account.State.Health != "" && account.State.Health != "passed")
}

func ListProviderAccountPools(c *gin.Context) {
	var channels []*model.Channel
	if err := model.DB.Where("type = ?", constant.ChannelTypeTaskPlugin).Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	rows := make([]gin.H, 0)
	for _, channel := range channels {
		plugin, ok := service.PluginOfChannel(channel)
		if !ok || !plugin.Meta.AccountPool {
			continue
		}
		accounts, _ := service.ListProviderPoolAccounts(c.Request.Context(), channel)
		activeCount, problemCount, disabledCount := 0, 0, 0
		for _, acct := range accounts {
			if acct.State.Disabled {
				disabledCount++
			}
			if providerAccountHasIssue(acct) {
				problemCount++
			} else {
				activeCount++
			}
		}
		rows = append(rows, gin.H{
			"id":                   channel.Id,
			"name":                 channel.Name,
			"groups":               channel.GetGroups(),
			"plugin":               gin.H{"key": plugin.Meta.Key, "name": plugin.Meta.Name, "supports_reset_quota": plugin.HasProviderHook("resetQuota")},
			"models":               channel.GetModels(),
			"supports_reset_quota": plugin.HasProviderHook("resetQuota"),
			"account_count":        len(accounts),
			"active_count":         activeCount,
			"problem_count":        problemCount,
			"disabled_count":       disabledCount,
		})
	}
	available := make([]gin.H, 0)
	for _, plugin := range jsplugin.DefaultRegistry.Generation().Plugins() {
		if plugin.Meta.AccountPool {
			available = append(available, gin.H{
				"key":                  plugin.Meta.Key,
				"name":                 plugin.Meta.Name,
				"supports_reset_quota": plugin.HasProviderHook("resetQuota"),
			})
		}
	}
	common.ApiSuccess(c, gin.H{"channels": rows, "plugins": available})
}

func GetAllProviderAccountPools(c *gin.Context) {
	var channels []*model.Channel
	if err := model.DB.Where("type = ?", constant.ChannelTypeTaskPlugin).Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pluginFilter := strings.TrimSpace(c.Query("plugin"))
	items := make([]service.ProviderPoolAccount, 0)
	for _, channel := range channels {
		plugin, ok := service.PluginOfChannel(channel)
		if !ok || !plugin.Meta.AccountPool {
			continue
		}
		if pluginFilter != "" && plugin.Meta.Key != pluginFilter {
			continue
		}
		accounts, err := service.ListProviderPoolAccounts(c.Request.Context(), channel, true)
		if err != nil {
			continue
		}
		items = append(items, accounts...)
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

// Administrators may bind installed account-pool providers and manage their
// accounts without receiving permissions to upload executable plugins.
func CreateProviderAccountPool(c *gin.Context) {
	var request struct {
		Plugin   string                       `json:"plugin"`
		Name     string                       `json:"name"`
		Groups   []string                     `json:"groups"`
		Settings service.ProviderPoolSettings `json:"settings"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if common.DecodeJson(c.Request.Body, &request) != nil || strings.TrimSpace(request.Name) == "" || len(request.Name) > 128 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if request.Settings.Concurrency == 0 {
		request.Settings.Concurrency = 1
	}
	if !request.Settings.Valid() {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	plugin, ok := jsplugin.DefaultRegistry.Get(request.Plugin)
	if !ok || !plugin.Meta.AccountPool {
		common.ApiErrorT(c, "Account pool plugin is unavailable")
		return
	}
	var groups []string
	if plugin.Meta.Key == "gpt-account-pool" || strings.Contains(strings.ToLower(plugin.Meta.BaseURL), "chatgpt.com") {
		groups = []string{"plus", "pro"}
	} else {
		seen := map[string]bool{}
		for _, raw := range request.Groups {
			g := strings.TrimSpace(raw)
			if g == "" || seen[g] {
				continue
			}
			seen[g] = true
			groups = append(groups, g)
		}
		if len(groups) == 0 {
			groups = []string{"default"}
		}
	}
	if err := ensureProviderPoolCustomerGroups(groups); err != nil {
		common.ApiError(c, err)
		return
	}
	setting, err := common.Marshal(map[string]string{"task_plugin_key": plugin.Meta.Key})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	settingText, baseURL := string(setting), plugin.Meta.BaseURL
	models := append([]string{}, plugin.Meta.Models...)
	channel := model.Channel{Type: constant.ChannelTypeTaskPlugin, Name: strings.TrimSpace(request.Name),
		Status: common.ChannelStatusEnabled, Group: strings.Join(groups, ","), Models: strings.Join(models, ","),
		CreatedTime: common.GetTimestamp(), Setting: &settingText, BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeyMode: constant.MultiKeyModePolling,
			ProviderPoolCategory: "unknown", ProviderPoolConcurrency: request.Settings.Concurrency}}
	if err := validateChannel(&channel, true); err != nil {
		common.ApiError(c, err)
		return
	}
	channels := []model.Channel{channel}
	if err := model.BatchInsertChannels(channels); err != nil {
		common.ApiError(c, err)
		return
	}
	id := channels[0].Id
	model.InitChannelCache()
	recordManageAudit(c, "channel.create", map[string]any{"id": id, "name": channel.Name, "source": "account_pool"})
	common.ApiSuccess(c, gin.H{"id": id})
}

// Plus and Pro account groups use the existing svip billing multiplier.
// Merge them into stored options so existing custom ratios remain unchanged.
func ensureProviderPoolCustomerGroups(groups []string) error {
	ratios := ratio_setting.GetGroupRatioCopy()
	usableGroups := setting.GetUserUsableGroupsCopy()
	svipRatio := ratio_setting.GetGroupRatio("svip")
	configuredRatios := map[string]float64{}
	common.OptionMapRWMutex.RLock()
	configuredRatiosJSON := common.OptionMap["GroupRatio"]
	common.OptionMapRWMutex.RUnlock()
	if configuredRatiosJSON != "" {
		if err := common.UnmarshalJsonStr(configuredRatiosJSON, &configuredRatios); err != nil {
			return err
		}
	}
	changed := false
	descriptions := map[string]string{
		"plus":     "Plus account group",
		"pro":      "Pro account group",
		"degraded": "Reduced quality account group",
	}
	for _, group := range groups {
		group = strings.TrimSpace(group)
		description, ok := descriptions[group]
		if !ok {
			continue
		}
		if _, ok := configuredRatios[group]; !ok {
			ratios[group], changed = svipRatio, true
		}
		if _, ok := usableGroups[group]; !ok {
			usableGroups[group], changed = description, true
		}
	}
	if !changed {
		return nil
	}
	ratioJSON, err := common.Marshal(ratios)
	if err != nil {
		return err
	}
	groupsJSON, err := common.Marshal(usableGroups)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		"GroupRatio":       string(ratioJSON),
		"UserUsableGroups": string(groupsJSON),
	})
}

func StartProviderPoolLogin(c *gin.Context) {
	if providerPoolTarget(c) == nil {
		return
	}
	StartPluginLogin(c)
}

func ContinueProviderPoolLogin(c *gin.Context) {
	if providerPoolTarget(c) == nil {
		return
	}
	ContinuePluginLogin(c)
}

func providerPoolTarget(c *gin.Context) *model.Channel {
	id, ok := pluginLoginChannelID(c)
	if !ok {
		return nil
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil || !service.IsProviderPool(channel) {
		common.ApiErrorT(c, "Channel does not use an account pool plugin")
		return nil
	}
	return channel
}

func GetProviderAccountPool(c *gin.Context) {
	channel := providerPoolTarget(c)
	if channel == nil {
		return
	}
	accounts, err := service.ListProviderPoolAccounts(c.Request.Context(), channel, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if accounts == nil {
		accounts = []service.ProviderPoolAccount{}
	}
	common.ApiSuccess(c, gin.H{"items": accounts, "settings": service.GetProviderPoolSettings(channel.Id)})
}

func SaveProviderAccountPool(c *gin.Context) {
	channel := providerPoolTarget(c)
	if channel == nil {
		return
	}
	var settings service.ProviderPoolSettings
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if common.DecodeJson(c.Request.Body, &settings) != nil || !settings.Valid() {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	err := model.UpdateProviderPoolSettings(channel.Id, settings.Concurrency)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.update", map[string]any{"id": channel.Id, "source": "account_pool_settings"})
	common.ApiSuccess(c, settings)
}

func ImportProviderAccount(c *gin.Context) {
	channel := providerPoolTarget(c)
	if channel == nil {
		return
	}
	plugin, _ := service.PluginOfChannel(channel)
	if !plugin.HasProviderHook("importCredential") {
		common.ApiErrorT(c, "Account import is not supported")
		return
	}
	var request struct {
		Account json.RawMessage `json:"account"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, jsplugin.MaxCredentialBytes+4096)
	if common.DecodeJson(c.Request.Body, &request) != nil || len(request.Account) == 0 {
		common.ApiErrorT(c, "Invalid account JSON")
		return
	}
	hookCtx := service.PluginCredentialHookContext(c.Request.Context(), plugin, channel.GetBaseURL(), channel.GetSetting().Proxy)
	result, err := plugin.CallImportCredential(hookCtx, jsplugin.CredentialContext{BaseURL: channel.GetBaseURL(), Config: plugin.ChannelConfigFor(channel.GetOtherSettings().PluginConfig)}, request.Account)
	if err != nil {
		common.ApiErrorT(c, "Invalid account credential")
		return
	}
	if result.AccountID == "" {
		common.ApiErrorT(c, "Account ID is required")
		return
	}
	accounts, err := service.ListProviderPoolAccounts(c.Request.Context(), channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	importStatus := "imported"
	for _, account := range accounts {
		if account.AccountID == result.AccountID {
			importStatus = "exists"
			break
		}
	}
	if importStatus == "imported" {
		if _, err = service.StorePluginLoginCredential(channel.Id, plugin.Meta.Key, nil, true, result); err != nil {
			common.ApiError(c, err)
			return
		}
		model.InitChannelCache()
		recordManageAudit(c, "channel.update", map[string]any{"id": channel.Id, "source": "account_pool_import", "account_id": result.AccountID})
	}
	refreshErr := ""
	modelsError := ""
	modelsUpdated := 0
	channel, err = model.GetChannelById(channel.Id, true)
	if err != nil {
		refreshErr = "quota_unavailable"
		if plugin.Meta.Key == "gpt-account-pool" {
			modelsError = "model_discovery_unavailable"
		}
	} else {
		account, findErr := service.FindProviderPoolAccount(c.Request.Context(), channel, result.AccountID)
		if findErr != nil {
			refreshErr = "quota_unavailable"
		} else {
			queryCtx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
			if err = service.RefreshProviderPoolAccount(queryCtx, account); err != nil {
				refreshErr = "quota_unavailable"
			}
			cancel()
		}
		if plugin.Meta.Key == "gpt-account-pool" {
			channel, err = model.GetChannelById(channel.Id, true)
			if err == nil {
				account, err = service.FindProviderPoolAccount(c.Request.Context(), channel, result.AccountID)
			}
			if err == nil {
				modelCtx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
				discovered, discoverErr := service.FetchGPTAccountPoolModels(modelCtx, account)
				cancel()
				if discoverErr != nil || len(discovered) == 0 {
					modelsError = "model_discovery_unavailable"
				} else {
					var changed bool
					changed, err = model.MergeChannelModels(channel.Id, discovered)
					if err != nil {
						modelsError = "model_discovery_unavailable"
					} else {
						modelsUpdated = len(discovered)
						if changed {
							model.InitChannelCache()
						}
					}
				}
			} else {
				modelsError = "model_discovery_unavailable"
			}
		}
	}
	common.ApiSuccess(c, gin.H{"status": importStatus, "account_id": result.AccountID, "quota_error": refreshErr,
		"models_error": modelsError, "models_updated": modelsUpdated})
}

func OperateProviderAccount(c *gin.Context) {
	channel := providerPoolTarget(c)
	if channel == nil {
		return
	}
	account, err := service.FindProviderPoolAccount(c.Request.Context(), channel, c.Param("account_id"))
	if err != nil {
		common.ApiErrorT(c, "Account does not exist in this pool")
		return
	}
	var request struct {
		Action          string `json:"action"`
		TargetChannelID int    `json:"target_channel_id"`
		CustomExpiresAt *int64 `json:"custom_expires_at"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if common.DecodeJson(c.Request.Body, &request) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	switch request.Action {
	case "inspect":
		mark, testedModel, inspectErr := inspectSingleProviderPoolAccount(c.Request.Context(), *channel, account)
		if inspectErr != nil {
			common.ApiErrorT(c, inspectErr.Error())
			return
		}
		recordManageAudit(c, "channel.test", map[string]any{
			"id":         channel.Id,
			"source":     "account_pool_inspect",
			"account_id": account.AccountID,
			"result":     mark.Result,
		})
		common.ApiSuccess(c, gin.H{
			"account_id": account.AccountID,
			"model":      testedModel,
			"result":     mark.Result,
			"reason":     mark.Reason,
			"time":       mark.Time,
		})
		return
	case "reset_quota":
		res, resetErr := service.ResetProviderPoolAccountQuota(c.Request.Context(), account)
		if resetErr != nil {
			common.ApiErrorT(c, resetErr.Error())
			return
		}
		recordManageAudit(c, "channel.update", map[string]any{
			"id":         channel.Id,
			"source":     "account_pool_reset_quota",
			"account_id": account.AccountID,
			"code":       res.Code,
		})
		common.ApiSuccess(c, gin.H{
			"account_id": account.AccountID,
			"code":       res.Code,
			"message":    res.Message,
		})
		return
	case "set_expiry":
		if request.CustomExpiresAt == nil || *request.CustomExpiresAt < 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		targetExpiry := *request.CustomExpiresAt
		err = service.UpdateProviderAccount(account.OwnerID, account.AccountID, func(state *model.ProviderAccountState) {
			state.CustomExpiresAt = targetExpiry
		})
		if err != nil {
			common.ApiErrorT(c, "Account operation failed")
			return
		}
		recordManageAudit(c, "channel.update", map[string]any{
			"id":                channel.Id,
			"source":            "account_pool_set_expiry",
			"account_id":        account.AccountID,
			"custom_expires_at": targetExpiry,
		})
		common.ApiSuccess(c, gin.H{
			"account_id":        account.AccountID,
			"custom_expires_at": targetExpiry,
		})
		return
	case "transfer":
		if role := c.GetInt("role"); role != common.RoleAdminUser && role != common.RoleRootUser {
			common.ApiErrorT(c, "Unauthorized, insufficient privileges")
			return
		}
		err = service.TransferProviderPoolAccount(channel.Id, request.TargetChannelID, account.AccountID)
	case "refresh":
		err = service.RefreshProviderPoolAccount(c.Request.Context(), account)
	case "enable", "disable", "restore":
		err = service.UpdateProviderAccount(account.OwnerID, account.AccountID, func(state *model.ProviderAccountState) {
			switch request.Action {
			case "enable":
				state.Disabled = false
			case "disable":
				state.Disabled = true
			case "restore":
				state.Degraded = false
				state.WrongAnswers = 0
				state.Disabled = false
				state.LastError = ""
			}
		})
	case "remove":
		plugin, _ := service.PluginOfChannel(account.Owner)
		service.RevokePluginCredentials(plugin.Meta.Key, account.Owner.GetBaseURL(), account.Owner.GetSetting().Proxy,
			account.OwnerID, plugin.ChannelConfigFor(account.Owner.GetOtherSettings().PluginConfig), []string{account.Line})
		err = service.RemoveProviderPoolAccount(account)
	default:
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err != nil {
		common.ApiErrorT(c, "Account operation failed")
		return
	}
	audit := map[string]any{"id": channel.Id, "source": "account_pool_" + request.Action, "account_id": account.AccountID}
	if request.Action == "transfer" {
		audit["target_channel_id"] = request.TargetChannelID
	}
	recordManageAudit(c, "channel.update", audit)
	common.ApiSuccess(c, nil)
}
