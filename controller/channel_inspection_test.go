package controller

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestChannelInspectionsUseProviderPoolAndPublishSafeResults(t *testing.T) {
	const pluginKey = "controller-inspection-pool"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.SystemTask{}))

	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })

	channel := insertPluginChannel(t, pluginKey, "https://chatgpt.com", "", false)
	channel.Name = "Pro account pool"
	channel.Group = "pro"
	channel.ChannelInfo.ProviderPoolCategory = "pro"
	channel.ChannelInfo.ProviderPoolConcurrency = 2
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{
		"name": channel.Name, "group": channel.Group, "channel_info": channel.ChannelInfo,
	}).Error)
	model.InitChannelCache()

	legacyCPA := &model.Channel{
		Id: 19, Type: constant.ChannelTypeOpenAI, Name: "Legacy CPA", Key: "legacy-key",
		Group: "pro", CPAPoolID: "legacy-pool",
	}
	require.NoError(t, model.DB.Create(legacyCPA).Error)

	channels, err := inspectionChannels()
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, channel.Id, channels[0].Id)
	require.Equal(t, fmt.Sprintf("ProviderInspection/%d/settings", channel.Id), inspectionKey(channel, "settings"))

	cfg := inspectionSettings(channel)
	cfg.Model = "inspection-model"
	cfg.Title = "Customer Pro"
	cfg.Published = true
	cfg.Questions = []inspectionQuestion{{Prompt: "private prompt", Answer: "secret expected"}}
	settings, err := common.Marshal(cfg)
	require.NoError(t, err)
	last, err := common.Marshal(inspectionRound{
		Prompt: "private prompt",
		Model:  "inspection-model",
		Time:   100,
		Accounts: []accountMark{{
			ID: "private-account-id", Label: "private@example.test", Time: 100, Result: "passed", Number: 1,
		}},
	})
	require.NoError(t, err)
	common.OptionMap[inspectionKey(channel, "settings")] = string(settings)
	common.OptionMap[inspectionKey(channel, "last")] = string(last)

	customer := &model.User{
		Id: 1, Username: "inspection-customer", Password: "test-only-password",
		Group: "pro", Status: 1,
	}
	require.NoError(t, model.DB.Create(customer).Error)
	public, publicJSON := callChannelAPI(t, CustomerChannelInspections, "GET", "/", "")
	require.True(t, public.Success, publicJSON)
	require.Contains(t, publicJSON, "Customer Pro")
	require.Contains(t, publicJSON, `"groups":["pro"]`)
	require.Contains(t, publicJSON, "passed")
	for _, secret := range []string{"private-account-id", "private@example.test", "private prompt", "secret expected", "inspection-model", "Legacy CPA"} {
		require.NotContains(t, publicJSON, secret)
	}

	admin, adminJSON := callChannelAPI(t, ListChannelInspections, "GET", "/", "")
	require.True(t, admin.Success, adminJSON)
	require.Contains(t, adminJSON, "private@example.test")
	require.Contains(t, adminJSON, "private prompt")
	require.Contains(t, adminJSON, "Pro account pool")
	require.NotContains(t, adminJSON, "Legacy CPA")
}
