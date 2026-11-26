package controller

import (
	"net/http"
	"net/http/httptest"
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

func TestProviderPoolTransferRequiresAdministratorAndMovesTheAccount(t *testing.T) {
	const pluginKey = "controller-pool-transfer"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "transfer-account", Data: []byte(`{"access_token":"secret-transfer-token"}`),
	}).Line()
	require.NoError(t, err)
	source := insertPluginChannel(t, pluginKey, "https://chatgpt.com", line, true)
	target := insertPluginChannel(t, pluginKey, "https://chatgpt.com", "", true)
	source.Group, target.Group = "plus,pro", "plus,pro"
	source.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"transfer-account": {Disabled: true, Profile: jsplugin.AccountProfile{Plan: "pro", Category: "pro", PlanVerified: true}},
	}
	require.NoError(t, model.DB.Model(source).Updates(map[string]any{
		"group": source.Group, "channel_info": source.ChannelInfo,
	}).Error)
	require.NoError(t, model.DB.Model(target).Update("group", target.Group).Error)
	model.InitChannelCache()
	body, err := common.Marshal(map[string]any{"action": "transfer", "target_channel_id": target.Id})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 2)
	ctx.Set("role", common.RoleCommonUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/account_pool/accounts/transfer-account", strings.NewReader(string(body)))
	ctx.Params = gin.Params{idParam(source.Id), gin.Param{Key: "account_id", Value: "transfer-account"}}
	OperateProviderAccount(ctx)
	var response channelAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)

	response, raw := callChannelAPI(t, OperateProviderAccount, http.MethodPost,
		"/api/channel/account_pool/accounts/transfer-account", string(body), idParam(source.Id), gin.Param{Key: "account_id", Value: "transfer-account"})
	require.True(t, response.Success, raw)
	storedSource, err := model.GetChannelById(source.Id, true)
	require.NoError(t, err)
	storedTarget, err := model.GetChannelById(target.Id, true)
	require.NoError(t, err)
	require.Empty(t, storedSource.Key)
	require.Equal(t, line, storedTarget.Key)
	require.Empty(t, storedSource.ChannelInfo.ProviderAccounts)
	require.True(t, storedTarget.ChannelInfo.ProviderAccounts["transfer-account"].Disabled)
}

func TestProviderPoolCreatesChannelWithAutomaticGroupsAndModelDiscovery(t *testing.T) {
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

	body := `{"plugin":"controller-pool-create","name":"Subscription pool","settings":{"concurrency":2}}`
	response, raw := callChannelAPI(t, CreateProviderAccountPool, http.MethodPost, "/api/channel/account_pools", body)
	require.True(t, response.Success, raw)
	var created struct { ID int `json:"id"` }
	require.NoError(t, common.Unmarshal(response.Data, &created))
	channel, err := model.GetChannelById(created.ID, true)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeTaskPlugin, channel.Type)
	require.Equal(t, pluginKey, channel.GetSetting().TaskPluginKey)
	require.Equal(t, "https://chatgpt.com", channel.GetBaseURL())
	require.Equal(t, []string{"plus", "pro"}, channel.GetGroups())
	require.Empty(t, channel.GetModels())
	require.Equal(t, 2.5, ratio_setting.GetGroupRatio("plus"))
	require.Equal(t, 2.5, ratio_setting.GetGroupRatio("pro"))

	response, raw = callChannelAPI(t, SaveProviderAccountPool, http.MethodPut, "/api/channel/1/account_pool",
		`{"concurrency":3}`, idParam(created.ID))
	require.True(t, response.Success, raw)
	channel, err = model.GetChannelById(created.ID, true)
	require.NoError(t, err)
	require.Equal(t, []string{"plus", "pro"}, channel.GetGroups())
	require.Equal(t, "unknown", channel.ChannelInfo.ProviderPoolCategory)
	require.Equal(t, 3, channel.ChannelInfo.ProviderPoolConcurrency)
	var abilities []model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", created.ID).Find(&abilities).Error)
	require.Empty(t, abilities)
}
func TestProviderPoolCreationIgnoresLegacyAccountTypeAndModelInputs(t *testing.T) {
	const pluginKey = "controller-pool-groups"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)

	body := `{"plugin":"controller-pool-groups","name":"Automatic pool","groups":["plus"],"models":["manual-model"],"settings":{"category":"pro","concurrency":1}}`
	response, raw := callChannelAPI(t, CreateProviderAccountPool, http.MethodPost, "/api/channel/account_pools", body)
	require.True(t, response.Success, raw)
	var created struct { ID int `json:"id"` }
	require.NoError(t, common.Unmarshal(response.Data, &created))
	channel, err := model.GetChannelById(created.ID, true)
	require.NoError(t, err)
	require.Equal(t, []string{"plus", "pro"}, channel.GetGroups())
	require.Empty(t, channel.GetModels())
}

func TestProviderPoolSetExpiryAndAllAccountsOverview(t *testing.T) {
	const pluginKey = "controller-pool-overview"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)

	line, err := (jsplugin.Credential{
		Plugin:    pluginKey,
		AccountID: "acct-expiry-1",
		Label:     "expiry@example.test",
		Data:      []byte(`{"access_token":"secret-token"}`),
	}).Line()
	require.NoError(t, err)
	ch := insertPluginChannel(t, pluginKey, "https://chatgpt.com", line, true)
	ch.Name, ch.Group = "Overview Pool", "pro"
	ch.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"acct-expiry-1": {
			Profile: jsplugin.AccountProfile{
				Plan: "pro", Category: "pro", PlanVerified: true,
				SubscriptionExpiresAt: 1893456000,
			},
		},
	}
	require.NoError(t, model.DB.Model(ch).Updates(map[string]any{
		"name": ch.Name, "group": ch.Group, "channel_info": ch.ChannelInfo,
	}).Error)
	model.InitChannelCache()

	// Set custom expiry
	setBody := `{"action":"set_expiry","custom_expires_at":1900000000}`
	resp, raw := callChannelAPI(t, OperateProviderAccount, http.MethodPost,
		"/api/channel/1/account_pool/accounts/acct-expiry-1", setBody,
		idParam(ch.Id), gin.Param{Key: "account_id", Value: "acct-expiry-1"})
	require.True(t, resp.Success, raw)
	require.Contains(t, raw, `"custom_expires_at":1900000000`)

	// All-accounts CPA overview endpoint returns account with plugin_key and custom_expires_at
	resp, raw = callChannelAPI(t, GetAllProviderAccountPools, http.MethodGet, "/api/channel/account_pools/accounts", "")
	require.True(t, resp.Success, raw)
	require.Contains(t, raw, "expiry@example.test")
	require.Contains(t, raw, `"plugin_key":"controller-pool-overview"`)
	require.Contains(t, raw, `"custom_expires_at":1900000000`)
	require.NotContains(t, raw, "secret-token")

	// ListProviderAccountPools returns summary counts
	resp, raw = callChannelAPI(t, ListProviderAccountPools, http.MethodGet, "/api/channel/account_pools", "")
	require.True(t, resp.Success, raw)
	require.Contains(t, raw, `"account_count":1`)
	require.Contains(t, raw, `"active_count":1`)

	// Clear custom expiry back to 0
	clearBody := `{"action":"set_expiry","custom_expires_at":0}`
	resp, raw = callChannelAPI(t, OperateProviderAccount, http.MethodPost,
		"/api/channel/1/account_pool/accounts/acct-expiry-1", clearBody,
		idParam(ch.Id), gin.Param{Key: "account_id", Value: "acct-expiry-1"})
	require.True(t, resp.Success, raw)
	stored, err := model.GetChannelById(ch.Id, true)
	require.NoError(t, err)
	require.Equal(t, int64(0), stored.ChannelInfo.ProviderAccounts["acct-expiry-1"].CustomExpiresAt)
}
