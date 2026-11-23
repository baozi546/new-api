package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCPAPoolImportDashboardRoles(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UserSession{}, &model.AuditLog{}, &model.Channel{}))
	cpaCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cpaCalls++; fmt.Fprint(w, `{"status":"imported"}`) }))
	defer upstream.Close()
	t.Setenv("CPA_MANAGEMENT_URL", upstream.URL)
	t.Setenv("CPA_MANAGEMENT_TOKEN", "fixture")
	require.NoError(t, model.DB.Create(&model.Channel{Id: 12, Name: "pool", Type: 1, BaseURL: &upstream.URL, CPAPoolID: strings.Repeat("a", 32), Key: "fixture"}).Error)
	engine := gin.New()
	SetApiRouter(engine)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		u := model.User{Username: fmt.Sprintf("restore-%d", role), Role: role, AffCode: fmt.Sprintf("r%d", role), Status: common.UserStatusEnabled}
		require.NoError(t, model.DB.Create(&u).Error)
		session, err := service.CreateLoginSession(u.Id, "password", "127.0.0.1", "fixture")
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "https://example.test/api/cpa/accounts/import", strings.NewReader(`{"channel_id":12,"account":{"type":"codex"}}`))
		req.Header.Set("Origin", "https://example.test")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if role == common.RoleCommonUser {
			require.Equal(t, 403, w.Code)
			require.Zero(t, cpaCalls)
		} else {
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), `"status":"imported"`)
		}
	}
	require.Equal(t, 2, cpaCalls)
}
