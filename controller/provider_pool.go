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
		rows = append(rows, gin.H{"id": channel.Id, "name": channel.Name, "groups": channel.GetGroups(),
			"plugin": gin.H{"key": plugin.Meta.Key, "name": plugin.Meta.Name}, "models": channel.GetModels()})
	}
	available := make([]gin.H, 0)
	for _, plugin := range jsplugin.DefaultRegistry.Generation().Plugins() {
		if plugin.Meta.AccountPool {
			available = append(available, gin.H{"key": plugin.Meta.Key, "name": plugin.Meta.Name})
		}
	}
	common.ApiSuccess(c, gin.H{"channels": rows, "plugins": available})
}

// Administrators may bind installed account-pool providers and manage their
// accounts without receiving permissions to upload executable plugins.
func CreateProviderAccountPool(c *gin.Context) {
	var request struct {
		Plugin   string                       `json:"plugin"`
		Name     string                       `json:"name"`
		Groups   []string                     `json:"groups"`
		Models   []string                     `json:"models"`
		Settings service.ProviderPoolSettings `json:"settings"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if common.DecodeJson(c.Request.Body, &request) != nil || strings.TrimSpace(request.Name) == "" || len(request.Name) > 128 || len(request.Groups) == 0 || len(request.Models) == 0 || !request.Settings.Valid() {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if len(request.Groups) > 32 || len(request.Models) > 256 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	for _, item := range append(append([]string{}, request.Groups...), request.Models...) {
		value := strings.TrimSpace(item)
		if value == "" || len(value) > 128 || strings.Contains(value, ",") || strings.ContainsAny(value, "\r\n") {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
	}
	plugin, ok := jsplugin.DefaultRegistry.Get(request.Plugin)
	if !ok || !plugin.Meta.AccountPool {
		common.ApiErrorT(c, "Account pool plugin is unavailable")
		return
	}
	if !service.ProviderPoolCategoryMatchesGroups(request.Settings.Category, request.Groups) {
		common.ApiErrorT(c, "Account type must match at least one customer group")
		return
	}
	if err := ensureProviderPoolCustomerGroups(request.Groups); err != nil {
		common.ApiError(c, err)
		return
	}
	setting, err := common.Marshal(map[string]string{"task_plugin_key": plugin.Meta.Key})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	settingText, baseURL := string(setting), plugin.Meta.BaseURL
	channel := model.Channel{Type: constant.ChannelTypeTaskPlugin, Name: strings.TrimSpace(request.Name),
		Status: common.ChannelStatusEnabled, Group: strings.Join(request.Groups, ","), Models: strings.Join(request.Models, ","),
		CreatedTime: common.GetTimestamp(), Setting: &settingText, BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeyMode: constant.MultiKeyModePolling,
			ProviderPoolCategory: request.Settings.Category, ProviderPoolConcurrency: request.Settings.Concurrency}}
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
	if err := ensureProviderPoolCustomerGroups([]string{settings.Category}); err != nil {
		common.ApiError(c, err)
		return
	}
	err := model.UpdateProviderPoolSettings(channel.Id, settings.Category, settings.Concurrency)
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
	for _, account := range accounts {
		if account.AccountID == result.AccountID {
			common.ApiSuccess(c, gin.H{"status": "exists", "account_id": account.AccountID})
			return
		}
	}
	_, err = service.StorePluginLoginCredential(channel.Id, plugin.Meta.Key, nil, true, result)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.update", map[string]any{"id": channel.Id, "source": "account_pool_import", "account_id": result.AccountID})
	// Query the real subscription before a Plus/Pro channel can select it.
	refreshErr := ""
	account, err := service.FindProviderPoolAccount(c.Request.Context(), channel, result.AccountID)
	if err == nil {
		queryCtx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
		if err = service.RefreshProviderPoolAccount(queryCtx, account); err != nil {
			refreshErr = "quota_unavailable"
		}
		cancel()
	}
	common.ApiSuccess(c, gin.H{"status": "imported", "account_id": result.AccountID, "quota_error": refreshErr})
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
		Action string `json:"action"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if common.DecodeJson(c.Request.Body, &request) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	switch request.Action {
	case "refresh":
		err = service.RefreshProviderPoolAccount(c.Request.Context(), account)
	case "enable", "disable", "degraded", "restore":
		err = service.UpdateProviderAccount(account.OwnerID, account.AccountID, func(state *model.ProviderAccountState) {
			switch request.Action {
			case "enable":
				state.Disabled = false
			case "disable":
				state.Disabled = true
			case "degraded":
				state.Degraded = true
			case "restore":
				state.Degraded = false
				state.WrongAnswers = 0
				state.Disabled = false
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
	recordManageAudit(c, "channel.update", map[string]any{"id": channel.Id, "source": "account_pool_" + request.Action, "account_id": account.AccountID})
	common.ApiSuccess(c, nil)
}
