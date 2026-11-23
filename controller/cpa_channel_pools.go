package controller

import (
	"context"
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func matchesCPAChannel(ch *model.Channel) bool {
	return ch.BaseURL != nil && ch.Type == constant.ChannelTypeOpenAI && slices.Contains(service.CPARelayEndpoints(), service.CPAChannelEndpoint(*ch.BaseURL))
}

func provisionCPAChannel(ctx context.Context, ch *model.Channel, share string) error {
	ch.CPAPoolID = ""
	if category := ch.CPAAccountCategory(); category != "" && category != "plus" && category != "pro" && category != "degraded" && category != "unassigned" {
		return errors.New("Invalid CPA account category")
	}
	if !matchesCPAChannel(ch) {
		if share != "" || ch.CPAAccountCategory() != "" {
			return errors.New("selected CPA pool requires a CPA channel")
		}
		return nil
	}
	id := share
	if id == "" {
		id = common.GetUUID()
	} else {
		var sources []model.Channel
		if err := model.DB.WithContext(ctx).Select("type", "base_url").Where("cpa_pool_id = ?", id).Find(&sources).Error; err != nil {
			return err
		}
		if !slices.ContainsFunc(sources, func(source model.Channel) bool { return matchesCPAChannel(&source) }) {
			return errors.New("unknown CPA channel pool")
		}
	}
	var reply struct {
		ID string `json:"id"`
	}
	if err := service.CallCPAManagement(ctx, "PUT", "channel-pools/"+id, nil, &reply); err != nil {
		return err
	}
	if reply.ID != id {
		return errors.New("CPA did not confirm the channel pool")
	}
	ch.CPAPoolID = id
	return nil
}

func ListCPAChannelBindings(c *gin.Context) {
	var rows []model.Channel
	if err := model.DB.WithContext(c.Request.Context()).Select("id", "name", "type", "base_url", "cpa_pool_id", "cpa_account_pool").Where("base_url IS NOT NULL AND base_url <> ?", "").Order("id").Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type binding struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Pool     string `json:"pool"`
		Category string `json:"category"`
	}
	bindings := make([]binding, 0)
	endpoints := service.CPARelayEndpoints()
	for _, row := range rows {
		if slices.Contains(endpoints, service.CPAChannelEndpoint(*row.BaseURL)) {
			bindings = append(bindings, binding{row.Id, row.Name, row.CPAPoolID, row.CPAAccountCategory()})
		}
	}
	common.ApiSuccess(c, gin.H{"endpoints": endpoints, "channels": bindings})
}
