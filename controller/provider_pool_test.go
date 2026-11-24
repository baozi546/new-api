package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const providerPoolImportPlugin = "export const meta = {apiVersion: 1, key: \"%KEY%\", name: \"Pool import test\", version: \"1.0.0\", author: {name: \"Test\"}, baseUrl: \"https://chatgpt.com\", accountPool: true, relay: [{incoming_path: \"/v1/chat/completions\", upstream_path: \"/chat\"}]};\n" +
	"export function login() { return {type: \"fields\", fields: []}; }\n" +
	"export function importCredential(ctx, input) { return {credential: {access_token: input.access_token, refresh_token: input.refresh_token}, headers: {Authorization: \"Bearer \" + input.access_token}, accountId: input.account_id, label: input.email}; }\n" +
	"export function describeAccount() { return {plan: \"pro\", category: \"pro\", plan_verified: true, windows: [{name: {en: \"Usage\", zh: \"额度\"}, used: 50, limit: 100, unit: \"%\", resetAt: 1893456000}], quota_checked_at: 100, quota_error: \"\"}; }\n" +
	"export function scheduled() { return {next: 300, balance: {windows: [{name: {en: \"Usage\", zh: \"额度\"}, used: 50, limit: 100, unit: \"%\", resetAt: 1893456000}]}}; }\n"

func registerProviderPoolImportPlugin(t *testing.T, key string) {
	t.Helper()
	source := strings.ReplaceAll(providerPoolImportPlugin, "%KEY%", key)
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
}

func TestProviderPoolImportStoresAccountAndReturnsOnlyQuotaSummary(t *testing.T) {
	const pluginKey = "controller-pool-import"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)

	channel := insertPluginChannel(t, pluginKey, "https://chatgpt.com", "", true)
	channel.Name, channel.Group = "Pro pool", "pro"
	channel.ChannelInfo.ProviderPoolCategory = "pro"
	channel.ChannelInfo.ProviderPoolConcurrency = 2
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{
		"name": channel.Name, "group": channel.Group, "channel_info": channel.ChannelInfo,
	}).Error)
	model.InitChannelCache()

	body, err := common.Marshal(map[string]any{"account": map[string]any{
		"type": "codex", "access_token": "secret-access-token", "refresh_token": "secret-refresh-token",
		"account_id": "pro-account", "email": "pro@example.test",
	}})
	require.NoError(t, err)
	response, raw := callChannelAPI(t, ImportProviderAccount, http.MethodPost, "/api/channel/1/account_pool/import", string(body), idParam(channel.Id))
	require.True(t, response.Success, raw)
	require.NotContains(t, raw, "secret-access-token")
	require.NotContains(t, raw, "secret-refresh-token")
	require.Contains(t, raw, "\"quota_error\":\"\"")

	response, raw = callChannelAPI(t, GetProviderAccountPool, http.MethodGet, "/api/channel/1/account_pool", "", idParam(channel.Id))
	require.True(t, response.Success, raw)
	require.Contains(t, raw, "pro@example.test")
	require.Contains(t, raw, "\"used\":50")
	require.Contains(t, raw, "\"concurrency\":2")
	require.NotContains(t, raw, "secret-access-token")
	require.NotContains(t, raw, "secret-refresh-token")

	action, err := common.Marshal(map[string]string{"action": "disable"})
	require.NoError(t, err)
	response, raw = callChannelAPI(t, OperateProviderAccount, http.MethodPost,
		"/api/channel/1/account_pool/accounts/pro-account", string(action),
		idParam(channel.Id), gin.Param{Key: "account_id", Value: "pro-account"})
	require.True(t, response.Success, raw)
	response, raw = callChannelAPI(t, GetProviderAccountPool, http.MethodGet, "/api/channel/1/account_pool", "", idParam(channel.Id))
	require.True(t, response.Success, raw)
	require.Contains(t, raw, "\"disabled\":true")
	require.NotContains(t, raw, "secret-access-token")
}

func TestProviderPoolCreatesDedicatedPluginChannelAndRelinksCustomerGroup(t *testing.T) {
	const pluginKey = "controller-pool-create"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.SystemTask{}))
	previousOptions := common.OptionMap
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousGroups := setting.UserUsableGroups2JSONString()
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		common.OptionMap = previousOptions
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"svip":2.5}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))

	body := ` {"plugin":"controller-pool-create","name":"Subscription pool","groups":["plus"],"models":["pool-model"],"settings":{"category":"plus","concurrency":2}}`
	response, raw := callChannelAPI(t, CreateProviderAccountPool, http.MethodPost, "/api/channel/account_pools", body)
	require.True(t, response.Success, raw)
	var created struct {
		ID int `json:"id"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &created))
	channel, err := model.GetChannelById(created.ID, true)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeTaskPlugin, channel.Type)
	require.Equal(t, pluginKey, channel.GetSetting().TaskPluginKey)
	require.Equal(t, "https://chatgpt.com", channel.GetBaseURL())
	require.Equal(t, 2.5, ratio_setting.GetGroupRatio("plus"))

	response, raw = callChannelAPI(t, SaveProviderAccountPool, http.MethodPut, "/api/channel/1/account_pool",
		`{"category":"pro","concurrency":3}`, idParam(created.ID))
	require.True(t, response.Success, raw)
	channel, err = model.GetChannelById(created.ID, true)
	require.NoError(t, err)
	require.Equal(t, []string{"pro"}, channel.GetGroups())
	require.Equal(t, "pro", channel.ChannelInfo.ProviderPoolCategory)
	require.Equal(t, 3, channel.ChannelInfo.ProviderPoolConcurrency)
	require.Equal(t, 2.5, ratio_setting.GetGroupRatio("pro"))
	var abilities []model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", created.ID).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	require.Equal(t, "pro", abilities[0].Group)
}

func TestProviderPoolRejectsConflictingSubscriptionGroups(t *testing.T) {
	const pluginKey = "controller-pool-groups"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)

	body := `{"plugin":"controller-pool-groups","name":"Invalid pool","groups":["plus","pro"],"models":["pool-model"],"settings":{"category":"pro","concurrency":1}}`
	response, raw := callChannelAPI(t, CreateProviderAccountPool, http.MethodPost, "/api/channel/account_pools", body)
	require.False(t, response.Success, raw)
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.Zero(t, count)
}
