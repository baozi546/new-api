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

func TestProviderPoolAccountsStayAssignedToTheirChannelAndMatchCustomerGroup(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "assigned-account", Label: "[REDACTED]",
		Data: []byte("{\"access_token\":\"secret-account-token\"}"),
	}).Line()
	require.NoError(t, err)
	owner := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example/v1", "Pro owner", "pro", 2, line)
	owner.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"assigned-account": {Profile: jsplugin.AccountProfile{
			Plan: "pro", Category: "pro", PlanVerified: true, Windows: []jsplugin.BalanceWindow{},
		}},
	}
	require.NoError(t, model.DB.Model(owner).Update("channel_info", owner.ChannelInfo).Error)
	consumer := createProviderPoolTestChannel(t, pluginKey, "https://UPSTREAM.example/v2", "Pro customer channel", "pro", 3, "")
	plus := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Plus customer channel", "plus", 4, "")
	model.InitChannelCache()

	accounts, err := ListProviderPoolAccounts(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, owner.Id, accounts[0].OwnerID)
	require.Equal(t, owner.Id, accounts[0].Channels[0].ID)
	require.Equal(t, 2, ProviderPoolAccountLimit(accounts[0]))
	accounts, err = ListProviderPoolAccounts(context.Background(), consumer)
	require.NoError(t, err)
	require.Empty(t, accounts)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("group", "pro")
	key, index, err := SelectProviderPoolAccount(c, owner)
	require.NoError(t, err)
	require.Equal(t, line, key)
	require.Equal(t, 0, index)
	require.Equal(t, owner.Id, c.GetInt("provider_pool_owner_id"))

	_, _, err = SelectProviderPoolAccount(c, consumer)
	require.ErrorContains(t, err, "no available account")
	c.Set("group", "plus")
	_, _, err = SelectProviderPoolAccount(c, owner)
	require.ErrorContains(t, err, "no available account")
	_, _, err = SelectProviderPoolAccount(c, plus)
	require.ErrorContains(t, err, "no available account")
}
func TestProviderPoolKeepsLegacySubscriptionSelectionForCustomGroups(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "legacy-pro-account",
		Data: []byte(`{"access_token":"legacy-token"}`),
	}).Line()
	require.NoError(t, err)
	channel := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Legacy SVIP channel", "svip", 1, line)
	channel.ChannelInfo.ProviderPoolCategory = "pro"
	channel.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"legacy-pro-account": {Profile: jsplugin.AccountProfile{Plan: "pro", Category: "pro", PlanVerified: true}},
	}
	require.NoError(t, model.DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error)
	model.InitChannelCache()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("group", "svip")
	_, _, err = SelectProviderPoolAccount(c, channel)
	require.NoError(t, err)
	c.Set("group", "plus")
	_, _, err = SelectProviderPoolAccount(c, channel)
	require.ErrorContains(t, err, "no available account")
}

func TestProviderPoolTransferMovesCredentialAndPreservesQuotaState(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	line, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "transfer-account", Label: "transfer@example.com",
		Data: []byte(`{"access_token":"secret-transfer-token"}`),
	}).Line()
	require.NoError(t, err)
	source := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Plus source", "plus,pro", 2, line)
	source.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"transfer-account": {
			Disabled: true,
			Health: "wrong_answer",
			CheckedAt: 123,
			WrongAnswers: 2,
			Profile: jsplugin.AccountProfile{
				Plan: "pro", Category: "pro", PlanVerified: true,
				QuotaCheckedAt: 122,
				Windows: []jsplugin.BalanceWindow{{Name: map[string]string{"en": "5h"}, Used: 25, Limit: 100, Unit: "%", ResetAt: 999}},
			},
		},
	}
	require.NoError(t, model.DB.Model(source).Update("channel_info", source.ChannelInfo).Error)
	target := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Pro target", "plus,pro", 3, "")
	model.InitChannelCache()

	require.NoError(t, TransferProviderPoolAccount(source.Id, target.Id, "transfer-account"))
	source, err = model.GetChannelById(source.Id, true)
	require.NoError(t, err)
	target, err = model.GetChannelById(target.Id, true)
	require.NoError(t, err)
	sourceAccounts, err := ListProviderPoolAccounts(context.Background(), source)
	require.NoError(t, err)
	require.Empty(t, sourceAccounts)
	targetAccounts, err := ListProviderPoolAccounts(context.Background(), target)
	require.NoError(t, err)
	require.Len(t, targetAccounts, 1)
	account := targetAccounts[0]
	require.Equal(t, target.Id, account.OwnerID)
	require.Equal(t, line, account.Line)
	require.True(t, account.State.Disabled)
	require.Equal(t, "wrong_answer", account.State.Health)
	require.Equal(t, 2, account.State.WrongAnswers)
	require.Equal(t, int64(122), account.Profile.QuotaCheckedAt)
	require.Equal(t, 25.0, account.Profile.Windows[0].Used)

	storedSource, err := model.GetChannelById(source.Id, true)
	require.NoError(t, err)
	storedTarget, err := model.GetChannelById(target.Id, true)
	require.NoError(t, err)
	require.Empty(t, storedSource.ChannelInfo.ProviderAccounts)
	targetState, ok := storedTarget.ChannelInfo.ProviderAccounts["transfer-account"]
	require.True(t, ok)
	require.True(t, targetState.Disabled)
	require.Equal(t, "wrong_answer", targetState.Health)
	require.Equal(t, int64(122), targetState.Profile.QuotaCheckedAt)
	require.Empty(t, storedSource.Key)
	require.Equal(t, line, storedTarget.Key)
}

func TestMergeChannelModelsAddsDiscoveredModelsAndAbilities(t *testing.T) {
	useProviderPluginDatabase(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	channel := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Pool", "plus,pro", 1, "")
	changed, err := model.MergeChannelModels(channel.Id, []string{" model-a ", "model-b", "model-a", "bad,model", strings.Repeat("x", 256)})
	require.NoError(t, err)
	require.True(t, changed)
	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, []string{"provider-model", "model-a", "model-b"}, stored.GetModels())
	var abilities []model.Ability
	require.NoError(t, model.DB.Where("channel_id = ? AND model IN ?", channel.Id, []string{"model-a", "model-b"}).Order("group").Find(&abilities).Error)
	require.Len(t, abilities, 4)

	changed, err = model.MergeChannelModels(channel.Id, []string{"model-a", "model-b"})
	require.NoError(t, err)
	require.False(t, changed)
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

	// Manual single-account inspection can explicitly force-select a disabled account
	// to check whether upstream risk controls have been lifted.
	cForce, _ := gin.CreateTestContext(httptest.NewRecorder())
	cForce.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	cForce.Set("group", "pro")
	cForce.Set("provider_pool_fixed_account", "disabled-account")
	cForce.Set("provider_pool_force_inspect", true)
	forcedLine, _, err := SelectProviderPoolAccount(cForce, owner)
	require.NoError(t, err)
	require.Equal(t, line, forcedLine)
	revalidated, err := RevalidateProviderPoolAccount(cForce, owner.Id)
	require.NoError(t, err)
	require.Equal(t, "disabled-account", revalidated.AccountID)

	accounts, err := ListProviderPoolAccounts(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.NoError(t, RemoveProviderPoolAccount(accounts[0]))
	stored, err := model.GetChannelById(owner.Id, true)
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(stored.Key))
	require.NotContains(t, stored.ChannelInfo.ProviderAccounts, "disabled-account")
}

func TestProviderPoolSettingsOnlyChangesConcurrency(t *testing.T) {
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

	require.NoError(t, model.UpdateProviderPoolSettings(channel.Id, 3))
	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, "svip,plus", stored.Group)
	require.Equal(t, "plus", stored.ChannelInfo.ProviderPoolCategory)
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

func TestProviderAccountMatchesSupportsNonGPTProviders(t *testing.T) {
	claudeAcct := ProviderPoolAccount{
		PluginKey: "claude-account-pool",
		Profile:   jsplugin.AccountProfile{Plan: "max", Category: "max", PlanVerified: true},
	}
	require.True(t, ProviderAccountMatches(claudeAcct, "max", ""))
	require.True(t, ProviderAccountMatches(claudeAcct, "", ""))
	require.False(t, ProviderAccountMatches(claudeAcct, "team", ""))

	genericAcct := ProviderPoolAccount{
		PluginKey: "gemini-account-pool",
		Profile:   jsplugin.AccountProfile{Plan: "standard", Category: "default", PlanVerified: false},
	}
	require.True(t, ProviderAccountMatches(genericAcct, "default", ""))
	require.True(t, ProviderAccountMatches(genericAcct, "svip", ""))

	// GPT account pools retain strict Plus/Pro isolation.
	gptPlus := ProviderPoolAccount{
		PluginKey: "gpt-account-pool",
		Profile:   jsplugin.AccountProfile{Plan: "plus", Category: "plus", PlanVerified: true},
	}
	require.True(t, ProviderAccountMatches(gptPlus, "plus", ""))
	require.False(t, ProviderAccountMatches(gptPlus, "pro", ""))
	require.True(t, ProviderAccountMatches(gptPlus, "vip", ""))
}

func TestResetProviderPoolAccountQuotaCallsPluginHookAndRefreshes(t *testing.T) {
	useProviderPluginDatabase(t)
	const pluginKey = "provider-pool-reset-test"
	const resetPlugin = `export const meta = {
  apiVersion: 1,
  key: "provider-pool-reset-test",
  name: { en: "Reset test pool", zh: "重置测试号池" },
  version: "1.0.0",
  author: { name: "Test" },
  accountPool: true,
  relay: [{ incoming_path: "/v1/chat/completions", upstream_path: "/chat" }],
};
export function login() { return { type: "fields", fields: [] }; }
export function resetQuota() { return { code: "reset", message: "Quota reset applied" }; }
export function scheduled(ctx) {
  const next = Object.assign({}, ctx.credential || {}, {
    plan: "pro",
    plan_verified: true,
    subscription_expires_at: 1893456000,
    reset_credits: { available_count: 0, applicable_available_count: 0, credits: [] },
    quota_checked_at: 200,
  });
  return { next: 300, credential: next };
}
export function describeAccount(ctx) {
  const d = ctx.credential || {};
  return {
    plan: d.plan || "pro",
    category: "pro",
    plan_verified: true,
    subscription_expires_at: d.subscription_expires_at || 0,
    supports_reset_credits: true,
    reset_credits: d.reset_credits || { available_count: 1, applicable_available_count: 1, credits: [] },
    windows: [],
    quota_checked_at: d.quota_checked_at || 100,
    quota_error: "",
  };
}`
	_, err := jsplugin.DefaultRegistry.Register(resetPlugin, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey)) })

	line, err := (jsplugin.Credential{
		Plugin:    pluginKey,
		AccountID: "reset-acct-1",
		Label:     "reset@example.test",
		Data:      []byte(`{"access_token":"tok"}`),
	}).Line()
	require.NoError(t, err)
	channel := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Reset pool", "pro", 1, line)
	channel.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"reset-acct-1": {
			Health:    "rate_limited",
			LastError: "HTTP 429",
			Profile: jsplugin.AccountProfile{
				Plan: "pro", Category: "pro", PlanVerified: true,
				SupportsResetCredits: true,
				ResetCredits:         &jsplugin.AccountResetCredits{AvailableCount: 1, ApplicableAvailableCount: 1},
			},
		},
	}
	require.NoError(t, model.DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error)
	model.InitChannelCache()

	account, err := FindProviderPoolAccount(context.Background(), channel, "reset-acct-1")
	require.NoError(t, err)
	require.True(t, account.SupportsResetQuota)

	res, err := ResetProviderPoolAccountQuota(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "reset", res.Code)

	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	state := stored.ChannelInfo.ProviderAccounts["reset-acct-1"]
	require.Empty(t, state.Health)
	require.Empty(t, state.LastError)
	require.Equal(t, int64(1893456000), state.Profile.SubscriptionExpiresAt)
	require.NotNil(t, state.Profile.ResetCredits)
	require.Equal(t, 0, state.Profile.ResetCredits.AvailableCount)
}
