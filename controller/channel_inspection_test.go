package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChannelInspectionScopesRoundsAndCustomerData(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.SystemTask{}))
	original := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = original })
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/credentials":
			_, _ = w.Write([]byte(`{"files":[{"provider":"codex","id":"private-account-id","label":"private@example.test","channel_pool_id":"first"},{"provider":"codex","id":"other","channel_pool_id":"second"},{"provider":"codex","id":"disabled","channel_pool_id":"first","disabled":true}]}`))
		case "/v8/management/credentials/quality-check":
			var in map[string]string
			require.NoError(t, common.DecodeJson(r.Body, &in))
			require.Equal(t, "private-account-id", in["name"])
			require.Equal(t, "first", in["channel_pool_id"])
			prompts = append(prompts, in["question"])
			raw, _ := common.Marshal(map[string]any{"check": map[string]string{"result": "passed", "question": in["question"]}})
			_, _ = w.Write(raw)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("CPA_MANAGEMENT_URL", server.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "unit-credential")
	ch := model.Channel{Id: 17, Type: 1, Name: "CPA Plus", Group: "svip,plus", Key: "example", BaseURL: &server.URL, CPAPoolID: "first"}
	require.NoError(t, model.DB.Create(&ch).Error)
	other := model.Channel{Id: 19, Type: 1, Name: "Hidden", Key: "example", BaseURL: &server.URL, CPAPoolID: "second"}
	require.NoError(t, model.DB.Create(&other).Error)
	cfg := inspectionSettings(&ch)
	cfg.Model = "probe-model"
	cfg.Title = "Customer Plus"
	cfg.Published = true
	cfg.Questions = []inspectionQuestion{{"private prompt 1", "secret expected"}, {"private prompt 2", "example answer"}}
	input, _ := common.Marshal(cfg)
	reply, _ := callChannelAPI(t, SaveChannelInspection, "POST", "/", string(input), idParam(17))
	require.True(t, reply.Success)
	for range 2 {
		require.NoError(t, inspectChannel(ch, inspectionSettings(&ch)))
	}
	require.Equal(t, []string{"private prompt 1", "private prompt 2"}, prompts)
	public, raw := callChannelAPI(t, CustomerChannelInspections, "GET", "/", "")
	require.True(t, public.Success)
	require.Contains(t, raw, "Customer Plus")
	require.Contains(t, raw, "CPA Plus")
	require.Contains(t, raw, `"groups":["svip","plus"]`)
	require.Contains(t, raw, "passed")
	require.Contains(t, raw, "disabled")
	for _, bad := range []string{"private", "example.test", "secret expected", "Hidden", "probe-model", "first", "channel_pool_id", "settings"} {
		require.False(t, strings.Contains(raw, bad), bad)
	}
	admin, raw := callChannelAPI(t, ListChannelInspections, "GET", "/", "")
	require.True(t, admin.Success)
	require.Contains(t, raw, "private@example.test")
	require.Contains(t, raw, "private prompt 2")
	require.Contains(t, raw, "Hidden")
}
