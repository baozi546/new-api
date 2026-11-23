package controller

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// The destination comes from the channel binding, never the backup. Only the
// fixed restore status is returned to the browser or written to the audit log.
func ImportCPAAccounts(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 160<<10)
	var input struct {
		ChannelID int             `json:"channel_id"`
		Account   json.RawMessage `json:"account"`
	}
	if c.ShouldBindJSON(&input) != nil || input.ChannelID <= 0 || len(input.Account) == 0 || len(input.Account) > 128<<10 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	ch, err := model.GetChannelById(input.ChannelID, false)
	if err != nil || !matchesCPAChannel(ch) || len(ch.CPAPoolID) != 32 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if _, err = hex.DecodeString(ch.CPAPoolID); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var result struct {
		Status string `json:"status"`
	}
	err = service.CallCPAManagement(c.Request.Context(), "POST", "channel-pools/"+ch.CPAPoolID+"/restore", input.Account, &result)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	switch result.Status {
	case "imported", "exists", "other_pool", "invalid":
	default:
		common.ApiErrorT(c, "Invalid CPA management response")
		return
	}
	recordManageAudit(c, "channel.update", map[string]any{"source": "cpa_account_import", "channel_id": ch.Id, "result": result.Status})
	common.ApiSuccess(c, result)
}
