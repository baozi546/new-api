package controller

import (
	"fmt"
	"strings"
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
			ID: "private-account-id", Label: "private@example.test", Time: 100, Reason: "502, Invalid upstream response event", Result: "inconclusive", Number: 1,
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
	require.Contains(t, publicJSON, "inconclusive")
	for _, secret := range []string{"private-account-id", "private@example.test", "private prompt", "secret expected", "inspection-model", "Invalid upstream response event", "Legacy CPA"} {
		require.NotContains(t, publicJSON, secret)
	}

	admin, adminJSON := callChannelAPI(t, ListChannelInspections, "GET", "/", "")
	require.True(t, admin.Success, adminJSON)
	require.Contains(t, adminJSON, "private-account-id")
	require.Contains(t, adminJSON, "private@example.test")
	require.Contains(t, adminJSON, "private prompt")
	require.Contains(t, adminJSON, "Invalid upstream response event")
	require.Contains(t, adminJSON, "Pro account pool")
	require.NotContains(t, adminJSON, "Legacy CPA")
}

func TestInspectionAnswerMatchingAndMismatchReason(t *testing.T) {
	require.True(t, inspectionAnswerMatches("21", "21"))
	require.True(t, inspectionAnswerMatches(" 21。\n", "21"))
	require.True(t, inspectionAnswerMatches("**21**.", "21"))
	require.True(t, inspectionAnswerMatches("`21`", "21"))
	require.True(t, inspectionAnswerMatches("「高市早苗」。", "高市早苗"))
	require.False(t, inspectionAnswerMatches("19", "21"))
	require.False(t, inspectionAnswerMatches("答案是21", "21"))
	require.False(t, inspectionAnswerMatches("", "21"))

	require.Equal(t, `expected "21", got "19"`, inspectionMismatchReason("21", " 19\n"))
	require.Equal(t, `expected "21", got "(empty)"`, inspectionMismatchReason("21", "   "))
	longReply := strings.Repeat("字", 300)
	reason := inspectionMismatchReason("21", longReply)
	require.True(t, strings.HasPrefix(reason, `expected "21", got "`))
	require.Contains(t, reason, "…")
}

func TestStandardChannelsCanEnrollUnenrollAndExtractMultiProviderReplies(t *testing.T) {
	const pluginKey = "controller-inspection-multi"
	setupProviderPluginControllerTest(t, pluginKey)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(pluginKey))
	registerProviderPoolImportPlugin(t, pluginKey)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.SystemTask{}))

	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })

	claudeCh := &model.Channel{
		Id: 31, Type: constant.ChannelTypeAnthropic, Name: "Claude Official", Key: "sk-ant-test",
		Group: "default", Models: "claude-sonnet-4-5", Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(claudeCh).Error)
	model.InitChannelCache()

	// Before enrollment, Claude channel appears in candidates and not in inspection list.
	resp, raw := callChannelAPI(t, ListInspectionCandidates, "GET", "/candidates", "")
	require.True(t, resp.Success, raw)
	require.Contains(t, raw, "Claude Official")

	resp, raw = callChannelAPI(t, ListChannelInspections, "GET", "/", "")
	require.True(t, resp.Success, raw)
	require.NotContains(t, raw, "Claude Official")

	// Enroll standard Claude channel.
	resp, raw = callChannelAPI(t, EnrollChannelInspection, "POST", "/31/enroll", "{}", idParam(claudeCh.Id))
	require.True(t, resp.Success, raw)

	resp, raw = callChannelAPI(t, ListChannelInspections, "GET", "/", "")
	require.True(t, resp.Success, raw)
	require.Contains(t, raw, "Claude Official")
	require.Contains(t, raw, `"is_pool":false`)

	// Unenroll standard Claude channel.
	resp, raw = callChannelAPI(t, UnenrollChannelInspection, "DELETE", "/31/enroll", "", idParam(claudeCh.Id))
	require.True(t, resp.Success, raw)

	resp, raw = callChannelAPI(t, ListChannelInspections, "GET", "/", "")
	require.True(t, resp.Success, raw)
	require.NotContains(t, raw, "Claude Official")

	// Multi-provider response reply text extraction.
	txt, ok := extractInspectionReplyText([]byte(`{"choices":[{"message":{"content":"703"}}]}`))
	require.True(t, ok)
	require.Equal(t, "703", txt)

	txt, ok = extractInspectionReplyText([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":"137"}]}}]}`))
	require.True(t, ok)
	require.Equal(t, "137", txt)

	txt, ok = extractInspectionReplyText([]byte(`{"content":[{"type":"text","text":"13"}]}`))
	require.True(t, ok)
	require.Equal(t, "13", txt)

	txt, ok = extractInspectionReplyText([]byte(`{"candidates":[{"content":{"parts":[{"text":"21"}]}}]}`))
	require.True(t, ok)
	require.Equal(t, "21", txt)

	txt, ok = extractInspectionReplyText([]byte(`{"output_text":"42"}`))
	require.True(t, ok)
	require.Equal(t, "42", txt)
}
