package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

// FetchGPTAccountPoolModels discovers the models available to an assigned OAuth
// account through the existing Codex model endpoint.
func FetchGPTAccountPoolModels(ctx context.Context, account ProviderPoolAccount) ([]string, error) {
	if account.Owner == nil || account.Owner.GetSetting().TaskPluginKey != "gpt-account-pool" {
		return nil, fmt.Errorf("account does not belong to the GPT account pool")
	}
	credential, ok := jsplugin.ParseCredential(account.Line)
	if !ok {
		return nil, fmt.Errorf("account credential is invalid")
	}
	var oauthKey CodexOAuthKey
	if err := common.Unmarshal(credential.Data, &oauthKey); err != nil {
		return nil, fmt.Errorf("account OAuth data is invalid")
	}
	channelSettings := account.Owner.GetSetting()
	versionClient, err := GetHttpClientWithProxy(channelSettings.Proxy)
	if err != nil {
		return nil, err
	}
	clientVersion, err := GetLatestCodexClientVersion(ctx, versionClient)
	if err != nil {
		return nil, err
	}
	client, err := GetHttpClientWithProxySettings(channelSettings.Proxy, channelSettings)
	if err != nil {
		return nil, err
	}
	status, models, err := FetchCodexModels(ctx, client, account.Owner.GetBaseURL(), &oauthKey, clientVersion)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("model endpoint returned status %d", status)
	}
	return models, nil
}
