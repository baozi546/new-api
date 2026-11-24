package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const providerPoolTestPlugin = "export const meta = {apiVersion: 1, key: \"provider-pool-test\", name: \"Provider pool test\", version: \"1.0.0\", author: {name: \"Test\"}, accountPool: true, relay: [{incoming_path: \"/v1/chat/completions\", upstream_path: \"/chat\"}]};\n" +
	"export function login() { return {type: \"fields\", fields: []}; }\n" +
	"export function describeAccount() { return {plan: \"pro\", category: \"pro\", plan_verified: true, windows: [], quota_checked_at: 100, quota_error: \"\"}; }\n"

func registerProviderPoolTestPlugin(t *testing.T) string {
	t.Helper()
	plugin, err := jsplugin.DefaultRegistry.Register(providerPoolTestPlugin, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key)) })
	return plugin.Meta.Key
}

func createProviderPoolTestChannel(t *testing.T, pluginKey, baseURL, name, category string, concurrency int, key string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Type: constant.ChannelTypeTaskPlugin, Key: key, Status: common.ChannelStatusEnabled,
		Name: name, Models: "provider-model", Group: category, BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeyMode: constant.MultiKeyModePolling,
			ProviderPoolCategory: category, ProviderPoolConcurrency: concurrency},
	}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: pluginKey})
	require.NoError(t, channel.Insert())
	return channel
}

func TestProviderPoolSharesAndPinsVerifiedAccount(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "shared-account", Label: "pro@example.test",
		Data: []byte("{\"access_token\":\"secret-account-token\"}"),
	}).Line()
	require.NoError(t, err)
	owner := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example/v1", "Pro owner", "pro", 2, line)
	owner.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"shared-account": {Profile: jsplugin.AccountProfile{
			Plan: "pro", Category: "pro", PlanVerified: true, Windows: []jsplugin.BalanceWindow{},
		}},
	}
	require.NoError(t, model.DB.Model(owner).Update("channel_info", owner.ChannelInfo).Error)
	consumer := createProviderPoolTestChannel(t, pluginKey, "https://UPSTREAM.example/v2", "Pro customer channel", "pro", 3, "")
	plus := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Plus customer channel", "plus", 4, "")
	model.InitChannelCache()

	accounts, err := ListProviderPoolAccounts(context.Background(), consumer)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, owner.Id, accounts[0].OwnerID)
	require.Equal(t, []int{owner.Id, consumer.Id, plus.Id}, []int{accounts[0].Channels[0].ID, accounts[0].Channels[1].ID, accounts[0].Channels[2].ID})
	require.Equal(t, 2, ProviderPoolAccountLimit(accounts[0]))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	key, index, err := SelectProviderPoolAccount(c, consumer)
	require.NoError(t, err)
	require.Equal(t, line, key)
	require.Equal(t, 0, index)
	require.Equal(t, owner.Id, c.GetInt("provider_pool_owner_id"))

	_, _, err = SelectProviderPoolAccount(c, plus)
	require.ErrorContains(t, err, "no available account")
}

func TestProviderPoolDisabledAccountIsExcludedAndCanBeRemoved(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "disabled-account",
		Data: []byte("{\"access_token\":\"secret-account-token\"}"),
	}).Line()
	require.NoError(t, err)
	owner := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Pro owner", "pro", 1, line)
	owner.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"disabled-account": {Disabled: true, Profile: jsplugin.AccountProfile{
			Plan: "pro", Category: "pro", PlanVerified: true, Windows: []jsplugin.BalanceWindow{},
		}},
	}
	require.NoError(t, model.DB.Model(owner).Update("channel_info", owner.ChannelInfo).Error)
	model.InitChannelCache()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, _, err = SelectProviderPoolAccount(c, owner)
	require.ErrorContains(t, err, "no available account")
	accounts, err := ListProviderPoolAccounts(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.NoError(t, RemoveProviderPoolAccount(accounts[0]))
	stored, err := model.GetChannelById(owner.Id, true)
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(stored.Key))
	require.NotContains(t, stored.ChannelInfo.ProviderAccounts, "disabled-account")
}

func TestProviderPoolSettingsMovesCustomerGroupWithoutChangingAccounts(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "settings-account",
		Data: []byte(`{"access_token":"secret-account-token"}`),
	}).Line()
	require.NoError(t, err)
	channel := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Shared pool", "plus", 1, line)
	channel.Group = "svip,plus"
	channel.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"settings-account": {Disabled: true, Health: "wrong_answer"},
	}
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{
		"group": channel.Group, "channel_info": channel.ChannelInfo,
	}).Error)

	require.NoError(t, model.UpdateProviderPoolSettings(channel.Id, "pro", 3))
	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, "svip,pro", stored.Group)
	require.Equal(t, "pro", stored.ChannelInfo.ProviderPoolCategory)
	require.Equal(t, 3, stored.ChannelInfo.ProviderPoolConcurrency)
	require.Equal(t, line, stored.Key)
	require.Equal(t, channel.ChannelInfo.ProviderAccounts, stored.ChannelInfo.ProviderAccounts)
	var abilities []model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	groups := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		groups = append(groups, ability.Group)
	}
	require.ElementsMatch(t, []string{"svip", "pro"}, groups)
}
