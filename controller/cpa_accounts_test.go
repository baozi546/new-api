package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
