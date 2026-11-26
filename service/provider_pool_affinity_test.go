package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupTwoAccountPoolChannel(t *testing.T, pluginKey string) (*model.Channel, string, string) {
	t.Helper()
	line1, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "acct-1", Label: "a1@example.test",
		Data: []byte(`{"access_token":"tok-1"}`),
	}).Line()
	require.NoError(t, err)
	line2, err := (jsplugin.Credential{
		Plugin: pluginKey, AccountID: "acct-2", Label: "a2@example.test",
		Data: []byte(`{"access_token":"tok-2"}`),
	}).Line()
	require.NoError(t, err)

	channel := createProviderPoolTestChannel(t, pluginKey, "https://upstream.example", "Pro Pool", "pro", 3, strings.Join([]string{line1, line2}, "\n"))
	channel.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{
		"acct-1": {Profile: jsplugin.AccountProfile{Plan: "pro", Category: "pro", PlanVerified: true}},
		"acct-2": {Profile: jsplugin.AccountProfile{Plan: "pro", Category: "pro", PlanVerified: true}},
	}
	require.NoError(t, model.DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error)
	model.InitChannelCache()
	return channel, line1, line2
}

func enableTestChannelAffinity(t *testing.T) {
	t.Helper()
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)
	previous := *setting
	setting.Enabled = true
	setting.DefaultTTLSeconds = 600
	setting.MaxEntries = 1000
	clearProviderPoolAffinityCacheAll()
	t.Cleanup(func() {
		clearProviderPoolAffinityCacheAll()
		*setting = previous
	})
}

func newPoolRequestContext(sessionKey string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("group", "pro")
	if sessionKey != "" {
		setChannelAffinityContext(c, channelAffinityMeta{
			CacheKey:   channelAffinityCacheNamespace + ":rule:" + sessionKey,
			TTLSeconds: 600,
			RuleName:   "rule",
		})
	}
	return c
}

func TestSelectProviderPoolAccountSticksToSessionAccountAndFallsBackWhenDisabled(t *testing.T) {
	useProviderPluginDatabase(t)
	enableTestChannelAffinity(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	channel, line1, line2 := setupTwoAccountPoolChannel(t, pluginKey)

	// First request for session-a picks one account and binds it.
	c1 := newPoolRequestContext("session-a")
	firstKey, _, err := SelectProviderPoolAccount(c1, channel)
	require.NoError(t, err)
	firstAccount := c1.GetString("provider_pool_account_id")
	require.NotEmpty(t, firstAccount)
	RecordChannelAffinity(c1, channel.Id)

	// Subsequent requests for the same session stay on the same account,
	// even if another account is idle or the bound account has active load.
	firstSlotKey := ProviderPoolAccountKey(pluginKey, firstAccount)
	_, releaseFirst, err := AcquireProviderAccountSlot(context.Background(), firstSlotKey, 3)
	require.NoError(t, err)
	defer releaseFirst()

	for range 3 {
		cNext := newPoolRequestContext("session-a")
		nextKey, _, err := SelectProviderPoolAccount(cNext, channel)
		require.NoError(t, err)
		require.Equal(t, firstKey, nextKey)
		require.Equal(t, firstAccount, cNext.GetString("provider_pool_account_id"))
	}

	// A fixed inspection request does not alter session affinity.
	cInspect := newPoolRequestContext("session-a")
	otherAccount := "acct-2"
	otherLine := line2
	if firstAccount == "acct-2" {
		otherAccount = "acct-1"
		otherLine = line1
	}
	cInspect.Set("provider_pool_fixed_account", otherAccount)
	inspectKey, _, err := SelectProviderPoolAccount(cInspect, channel)
	require.NoError(t, err)
	require.Equal(t, otherLine, inspectKey)

	cAfterInspect := newPoolRequestContext("session-a")
	afterKey, _, err := SelectProviderPoolAccount(cAfterInspect, channel)
	require.NoError(t, err)
	require.Equal(t, firstKey, afterKey)

	// Disabling the bound account triggers automatic fallback and rebinding.
	require.NoError(t, UpdateProviderAccount(channel.Id, firstAccount, func(state *model.ProviderAccountState) {
		state.Disabled = true
	}))
	updatedChannel, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	cFallback := newPoolRequestContext("session-a")
	fallbackKey, _, err := SelectProviderPoolAccount(cFallback, updatedChannel)
	require.NoError(t, err)
	require.Equal(t, otherLine, fallbackKey)
	require.Equal(t, otherAccount, cFallback.GetString("provider_pool_account_id"))

	// Even if the original account is re-enabled later, session-a stays bound to the new account.
	require.NoError(t, UpdateProviderAccount(channel.Id, firstAccount, func(state *model.ProviderAccountState) {
		state.Disabled = false
	}))
	reEnabledChannel, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	cRebound := newPoolRequestContext("session-a")
	reboundKey, _, err := SelectProviderPoolAccount(cRebound, reEnabledChannel)
	require.NoError(t, err)
	require.Equal(t, otherLine, reboundKey)
	require.Equal(t, otherAccount, cRebound.GetString("provider_pool_account_id"))
}

func TestSelectProviderPoolAccountPrefersIdleAccountsForOtherUsers(t *testing.T) {
	useProviderPluginDatabase(t)
	enableTestChannelAffinity(t)
	pluginKey := registerProviderPoolTestPlugin(t)
	channel, _, _ := setupTwoAccountPoolChannel(t, pluginKey)

	// User A takes a slot on acct-1.
	key1 := ProviderPoolAccountKey(pluginKey, "acct-1")
	key2 := ProviderPoolAccountKey(pluginKey, "acct-2")
	_, releaseA, err := AcquireProviderAccountSlot(context.Background(), key1, 3)
	require.NoError(t, err)
	defer releaseA()

	// Multiple new users without existing affinity should prefer idle acct-2
	// instead of piling onto busy acct-1.
	for _, session := range []string{"user-b", "user-c", ""} {
		c := newPoolRequestContext(session)
		_, _, err := SelectProviderPoolAccount(c, channel)
		require.NoError(t, err)
		require.Equal(t, "acct-2", c.GetString("provider_pool_account_id"))
	}

	// Now acct-2 acquires two concurrent slots (active: acct-1=1, acct-2=2).
	// A new user should prefer the less-loaded acct-1.
	_, releaseB1, err := AcquireProviderAccountSlot(context.Background(), key2, 3)
	require.NoError(t, err)
	defer releaseB1()
	_, releaseB2, err := AcquireProviderAccountSlot(context.Background(), key2, 3)
	require.NoError(t, err)
	defer releaseB2()

	cNew := newPoolRequestContext("user-d")
	_, _, err = SelectProviderPoolAccount(cNew, channel)
	require.NoError(t, err)
	require.Equal(t, "acct-1", cNew.GetString("provider_pool_account_id"))
}
