package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCPAAccountLoginBindsCallbackToSession(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuthFlow{}))
	callbacks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/oauth/auth-url":
			fmt.Fprint(w, `{"url":"https://auth.openai.com/authorize?state=state1","state":"state1"}`)
		case "/v8/management/oauth/callback":
			callbacks++
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/v8/management/oauth/status":
			fmt.Fprint(w, `{"status":"ok"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("CPA_MANAGEMENT_URL", server.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	response, raw := callChannelAPI(t, CPAAccountLogin, "POST", "/api/cpa/accounts/login", "{}")
	require.True(t, response.Success, raw)
	var start struct {
		Flow string `json:"flow"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &start))
	require.NotEmpty(t, start.Flow)
	body := fmt.Sprintf(`{"flow":%q,"callback":"http://localhost:1455/auth/callback?state=state1&code=test"}`, start.Flow)
	wrongSession := func(c *gin.Context) { c.Set("session_id", "different-session"); CPAAccountLogin(c) }
	response, _ = callChannelAPI(t, wrongSession, "POST", "/api/cpa/accounts/login", body)
	require.False(t, response.Success)
	require.Zero(t, callbacks)
	response, raw = callChannelAPI(t, CPAAccountLogin, "POST", "/api/cpa/accounts/login", body)
	require.True(t, response.Success, raw)
	require.Equal(t, 1, callbacks)
	response, _ = callChannelAPI(t, CPAAccountLogin, "POST", "/api/cpa/accounts/login", body)
	require.False(t, response.Success)
	require.Equal(t, 1, callbacks)
}

func TestCPAAccountLoginRequiresPoolAcknowledgement(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuthFlow{}))
	ack := ""
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["account_pool"] != "pro" || input["provider"] != "codex" {
			t.Error("pool or provider missing")
		}
		calls++
		fmt.Fprintf(w, `{"url":"https://auth.openai.com/authorize?state=test","state":"test","account_pool":%q}`, ack)
	}))
	defer server.Close()
	t.Setenv("CPA_MANAGEMENT_URL", server.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	response, _ := callChannelAPI(t, CPAAccountLogin, "POST", "/api/cpa/accounts/login", `{"pool":"other"}`)
	require.False(t, response.Success)
	require.Zero(t, calls)
	response, _ = callChannelAPI(t, CPAAccountLogin, "POST", "/api/cpa/accounts/login", `{"pool":"pro"}`)
	require.False(t, response.Success)
	ack = "pro"
	response, raw := callChannelAPI(t, CPAAccountLogin, "POST", "/api/cpa/accounts/login", `{"pool":"pro"}`)
	require.True(t, response.Success, raw)
}

func TestCPAChannelIDsMatchOnlyConfiguredRelayEndpoints(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	t.Setenv("CPA_MANAGEMENT_URL", "http://internal:8317")
	t.Setenv("CPA_RELAY_URLS", "https://example.test/tenant-a")
	for i, endpoint := range []string{"http://internal:8317/v1/", "https://EXAMPLE.test:443/tenant-a/v1", "https://example.test/tenant-b", "https://unrelated.test"} {
		require.NoError(t, model.DB.Create(&model.Channel{Id: i + 1, Name: "channel", Key: "fixture", BaseURL: &endpoint}).Error)
	}
	response, raw := callChannelAPI(t, ListCPAChannelIDs, "GET", "/api/cpa/accounts/channels", "")
	require.True(t, response.Success, raw)
	require.JSONEq(t, `[1,2]`, string(response.Data))
}

func TestCPAChannelCreateShareRenameAndLogin(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuthFlow{}))
	loginPool := ""
	available := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available {
			w.WriteHeader(503)
			return
		}
		if r.Method == "PUT" {
			id := strings.TrimPrefix(r.URL.Path, "/v8/management/channel-pools/")
			fmt.Fprintf(w, `{"id":%q}`, id)
			return
		}
		if r.URL.Path == "/v8/management/oauth/auth-url" {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			loginPool = body["channel_pool_id"]
			fmt.Fprintf(w, `{"state":"s1","url":"https://auth.openai.com/a?state=s1","channel_pool_id":%q}`, loginPool)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("CPA_MANAGEMENT_URL", server.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	create := func(share string) channelAPIResponse {
		body := fmt.Sprintf(`{"mode":"single","share_cpa_pool":%q,"channel":{"name":"same","type":1,"key":"fixture","base_url":%q,"models":"test","group":"default"}}`, share, server.URL)
		res, _ := callChannelAPI(t, AddChannel, "POST", "/api/channel", body)
		return res
	}
	require.True(t, create("").Success)
	require.True(t, create("").Success)
	a, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	b, err := model.GetChannelById(2, true)
	require.NoError(t, err)
	require.Len(t, a.CPAPoolID, 32)
	require.NotEqual(t, a.CPAPoolID, b.CPAPoolID)
	require.Equal(t, server.URL+"/channel-pools/"+a.CPAPoolID, a.GetBaseURL())
	require.True(t, create(a.CPAPoolID).Success)
	shared, err := model.GetChannelById(3, true)
	require.NoError(t, err)
	require.Equal(t, a.CPAPoolID, shared.CPAPoolID)
	resEdit, rawEdit := callChannelAPI(t, UpdateChannel, "PUT", "/api/channel/", fmt.Sprintf(`{"id":2,"share_cpa_pool":%q,"cpa_account_pool":"pro"}`, a.CPAPoolID))
	require.True(t, resEdit.Success, rawEdit)
	b, err = model.GetChannelById(2, true)
	require.NoError(t, err)
	require.Equal(t, a.CPAPoolID, b.CPAPoolID)
	require.Equal(t, server.URL+"/channel-pools/"+a.CPAPoolID+"/categories/pro", b.GetBaseURL())
	resEdit, rawEdit = callChannelAPI(t, UpdateChannel, "PUT", "/api/channel/", `{"id":2,"name":"renamed pro"}`)
	require.True(t, resEdit.Success, rawEdit)
	b, err = model.GetChannelById(2, true)
	require.NoError(t, err)
	require.Equal(t, server.URL+"/channel-pools/"+a.CPAPoolID+"/categories/pro", b.GetBaseURL())
	resEdit, _ = callChannelAPI(t, UpdateChannel, "PUT", "/api/channel/", `{"id":2,"cpa_account_pool":"unknown"}`)
	require.False(t, resEdit.Success)
	resEdit, rawEdit = callChannelAPI(t, UpdateChannel, "PUT", "/api/channel/", `{"id":2,"cpa_account_pool":""}`)
	require.True(t, resEdit.Success, rawEdit)
	b, err = model.GetChannelById(2, true)
	require.NoError(t, err)
	require.Equal(t, a.GetBaseURL(), b.GetBaseURL())
	copied, rawCopy := callChannelAPI(t, CopyChannel, "POST", "/copy/1", "", idParam(1))
	require.True(t, copied.Success, rawCopy)
	copyChannel, err := model.GetChannelById(4, true)
	require.NoError(t, err)
	require.Equal(t, a.CPAPoolID, copyChannel.CPAPoolID)

	require.NoError(t, model.DB.Model(a).Update("name", "renamed").Error)
	res, raw := callChannelAPI(t, CPAAccountLogin, "POST", "/login", `{"channel_id":1}`)
	require.True(t, res.Success, raw)
	require.Equal(t, a.CPAPoolID, loginPool)
	require.False(t, create("invalid").Success)
	available = false
	require.False(t, create("").Success)
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 4, count)
}

func TestCPAImportUsesBoundChannelAndRedactsResponse(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	pool := strings.Repeat("a", 32)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/v8/management/channel-pools/"+pool+"/restore", r.URL.Path)
		require.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"status":"imported","extra":"private-fixture"}`)
	}))
	defer server.Close()
	t.Setenv("CPA_MANAGEMENT_URL", server.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	require.NoError(t, model.DB.Create(&model.Channel{Id: 17, Type: 1, Name: "pro", Key: "fixture", BaseURL: &server.URL, CPAPoolID: pool}).Error)
	for _, id := range []int{0, 999, 17} {
		res, raw := callChannelAPI(t, ImportCPAAccounts, "POST", "/api/cpa/accounts/import", fmt.Sprintf(`{"channel_id":%d,"account":{"type":"codex"}}`, id))
		require.Equal(t, id == 17, res.Success, raw)
		require.NotContains(t, raw, "private-fixture")
	}
	require.Equal(t, 1, calls)
}
