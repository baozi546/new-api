package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
)

type ProviderPoolSettings struct {
	Category    string `json:"category"`
	Concurrency int    `json:"concurrency"`
}

var providerPoolCursors sync.Map

func GetProviderPoolSettings(channelID int) ProviderPoolSettings {
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		return ProviderPoolSettings{Category: "unknown", Concurrency: 1}
	}
	category := channel.ChannelInfo.ProviderPoolCategory
	if category == "" {
		category = "unknown"
		groups := channel.GetGroups()
		if len(groups) == 1 && slices.Contains([]string{"plus", "pro", "degraded"}, groups[0]) {
			category = groups[0]
		}
	}
	return ProviderPoolSettings{Category: category, Concurrency: max(1, channel.ChannelInfo.ProviderPoolConcurrency)}
}

func (s ProviderPoolSettings) Valid() bool {
	return slices.Contains([]string{"plus", "pro", "degraded", "unknown"}, s.Category) && s.Concurrency >= 1 && s.Concurrency <= 10000
}

// ProviderPoolCategoryMatchesGroups keeps the public customer group aligned
// with the actual subscription type selected by the channel.
func ProviderPoolCategoryMatchesGroups(category string, groups []string) bool {
	if category == "" || category == "unknown" {
		return true
	}
	matched := false
	for _, group := range groups {
		group = strings.ToLower(strings.TrimSpace(group))
		if slices.Contains([]string{"plus", "pro", "degraded"}, group) {
			if group != category {
				return false
			}
			matched = true
		}
	}
	return matched
}

func IsProviderPool(channel *model.Channel) bool {
	plugin, ok := PluginOfChannel(channel)
	return ok && plugin.Meta.AccountPool
}

// ProviderPoolAccount retains ownership server-side. Public never serializes a
// credential or its headers. Account IDs remain stable when tokens rotate.
type ProviderPoolAccount struct {
	Owner      *model.Channel             `json:"-"`
	Line       string                     `json:"-"`
	Index      int                        `json:"index"`
	OwnerID    int                        `json:"owner_id"`
	AccountID  string                     `json:"account_id"`
	Label      string                     `json:"label"`
	ExpiresAt  int64                      `json:"expires_at"`
	NeedsLogin bool                       `json:"needs_login"`
	State      model.ProviderAccountState `json:"state"`
	Profile    jsplugin.AccountProfile    `json:"profile"`
	Channels   []ProviderPoolChannel      `json:"channels"`
}

type ProviderPoolChannel struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Groups      []string `json:"groups"`
	Category    string   `json:"category"`
	Concurrency int      `json:"concurrency"`
}

func providerPoolIdentity(channel *model.Channel, pluginKey string) string {
	parsed, err := url.Parse(strings.TrimSpace(channel.GetBaseURL()))
	if err != nil {
		return pluginKey + "\x00" + strings.TrimRight(channel.GetBaseURL(), "/")
	}
	return pluginKey + "\x00" + strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func ListProviderPoolAccounts(ctx context.Context, channel *model.Channel, includeUncachedProfiles ...bool) ([]ProviderPoolAccount, error) {
	plugin, ok := PluginOfChannel(channel)
	if !ok || !plugin.Meta.AccountPool {
		return nil, errors.New("channel does not use an account pool plugin")
	}
	var channels []*model.Channel
	if err := model.DB.Where("type = ?", constant.ChannelTypeTaskPlugin).Order("id").Find(&channels).Error; err != nil {
		return nil, err
	}
	identity := providerPoolIdentity(channel, plugin.Meta.Key)
	var accounts []ProviderPoolAccount
	var shared []ProviderPoolChannel
	for _, owner := range channels {
		if owner.GetSetting().TaskPluginKey != plugin.Meta.Key || providerPoolIdentity(owner, plugin.Meta.Key) != identity {
			continue
		}
		shared = append(shared, ProviderPoolChannel{ID: owner.Id, Name: owner.Name, Groups: owner.GetGroups(),
			Category: owner.ChannelInfo.ProviderPoolCategory, Concurrency: max(1, owner.ChannelInfo.ProviderPoolConcurrency)})
		for index, line := range pluginCredentialKeyLines(owner) {
			credential, ok := jsplugin.ParseCredential(line)
			if !ok || credential.Plugin != plugin.Meta.Key || credential.AccountID == "" {
				continue
			}
			state := owner.ChannelInfo.ProviderAccounts[credential.AccountID]
			profile := state.Profile
			if len(includeUncachedProfiles) > 0 && includeUncachedProfiles[0] && (profile.Plan == "" || profile.Windows == nil) {
				described, err := plugin.CallDescribeAccount(ctx, jsplugin.CredentialContext{BaseURL: owner.GetBaseURL(), Credential: credential.Data})
				if err != nil {
					return nil, errors.New("account profile could not be read")
				}
				profile = described
			}
			needsLogin := credential.NeedsLogin || profile.QuotaError == "login_required" || state.Health == "login_required" || state.Health == "account_banned"
			accounts = append(accounts, ProviderPoolAccount{Owner: owner, Line: line, Index: index, OwnerID: owner.Id,
				AccountID: credential.AccountID, Label: credential.Label, ExpiresAt: credential.ExpiresAt,
				NeedsLogin: needsLogin, State: state, Profile: profile})
		}
	}
	for i := range accounts {
		accounts[i].Channels = shared
	}
	return accounts, nil
}

func ProviderAccountMatches(account ProviderPoolAccount, category string) bool {
	if category == "degraded" {
		return account.State.Degraded
	}
	if account.State.Degraded {
		return false
	}
	return account.Profile.PlanVerified && account.Profile.Category == category
}

func ProviderAccountUsable(account ProviderPoolAccount) bool {
	return !account.State.Disabled && !account.NeedsLogin && account.State.Health != "account_banned"
}

// SelectProviderPoolAccount uses an exact account on inspections. Ordinary
// requests poll only accounts of the channel's configured subscription type.
func SelectProviderPoolAccount(c *gin.Context, channel *model.Channel) (string, int, error) {
	accounts, err := ListProviderPoolAccounts(c.Request.Context(), channel)
	if err != nil {
		return "", 0, err
	}
	category := GetProviderPoolSettings(channel.Id).Category
	fixed := c.GetString("provider_pool_fixed_account")
	eligible := make([]ProviderPoolAccount, 0, len(accounts))
	for _, account := range accounts {
		if !ProviderAccountMatches(account, category) || !ProviderAccountUsable(account) || fixed != "" && account.AccountID != fixed {
			continue
		}
		eligible = append(eligible, account)
	}
	if len(eligible) == 0 {
		return "", 0, errors.New("no available account for this account pool category")
	}
	cursor, _ := providerPoolCursors.LoadOrStore(channel.Id, &atomic.Uint64{})
	index := (cursor.(*atomic.Uint64).Add(1) - 1) % uint64(len(eligible))
	account := eligible[index]
	c.Set("provider_pool_account", account)
	c.Set("provider_pool_owner_id", account.OwnerID)
	c.Set("provider_pool_account_id", account.AccountID)
	c.Set("provider_pool_plugin", channel.GetSetting().TaskPluginKey)
	return account.Line, account.Index, nil
}

func ProviderCredentialOwner(c *gin.Context, fallback int) int {
	if id := c.GetInt("provider_pool_owner_id"); id > 0 {
		return id
	}
	return fallback
}

func ProviderPoolAccountKey(plugin, accountID string) string {
	sum := sha256.Sum256([]byte(plugin + "\x00" + accountID))
	return hex.EncodeToString(sum[:])
}

// UpdateProviderAccount always locates the account again under the row lock.
// Refreshes, imports and key deletion cannot redirect a stale index to a peer.
func UpdateProviderAccount(ownerID int, accountID string, update func(*model.ProviderAccountState)) error {
	stored, err := model.UpdateChannelLocked(ownerID, func(channel *model.Channel) error {
		found := false
		for _, line := range pluginCredentialKeyLines(channel) {
			credential, ok := jsplugin.ParseCredential(line)
			if ok && credential.AccountID == accountID {
				found = true
				break
			}
		}
		if !found {
			return ErrPluginCredentialRemoved
		}
		if channel.ChannelInfo.ProviderAccounts == nil {
			channel.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{}
		}
		state := channel.ChannelInfo.ProviderAccounts[accountID]
		update(&state)
		channel.ChannelInfo.ProviderAccounts[accountID] = state
		return nil
	})
	if err == nil {
		model.CacheUpdateChannel(stored)
	}
	return err
}

func MarkProviderPoolLoginRequired(ownerID int, line string) error {
	return MarkProviderPoolProblem(ownerID, line, 401)
}

func MarkProviderPoolProblem(ownerID int, line string, status int) error {
	credential, ok := jsplugin.ParseCredential(line)
	if !ok {
		return ErrPluginCredentialRemoved
	}
	stored, err := model.UpdateChannelLocked(ownerID, func(channel *model.Channel) error {
		for _, currentLine := range pluginCredentialKeyLines(channel) {
			current, ok := jsplugin.ParseCredential(currentLine)
			if !ok || current.Plugin != credential.Plugin || current.AccountID != credential.AccountID {
				continue
			}
			if !maps.Equal(current.Headers, credential.Headers) {
				return nil
			}
			if channel.ChannelInfo.ProviderAccounts == nil {
				channel.ChannelInfo.ProviderAccounts = map[string]model.ProviderAccountState{}
			}
			state := channel.ChannelInfo.ProviderAccounts[credential.AccountID]
			state.Health = "account_banned"
			if status == 401 {
				state.Health = "login_required"
			}
			state.CheckedAt = time.Now().Unix()
			channel.ChannelInfo.ProviderAccounts[credential.AccountID] = state
			return nil
		}
		return ErrPluginCredentialRemoved
	})
	if err == nil {
		model.CacheUpdateChannel(stored)
	}
	return err
}

// RevalidateProviderPoolAccount runs after a queued request obtains capacity.
// Administrative disable, removal and a new login take effect before sending.
func RevalidateProviderPoolAccount(c *gin.Context, channelID int) (ProviderPoolAccount, error) {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return ProviderPoolAccount{}, err
	}
	account, err := FindProviderPoolAccount(c.Request.Context(), channel, c.GetString("provider_pool_account_id"))
	if err != nil {
		return ProviderPoolAccount{}, err
	}
	if !ProviderAccountUsable(account) || !ProviderAccountMatches(account, GetProviderPoolSettings(channelID).Category) {
		return ProviderPoolAccount{}, errors.New("account is no longer available")
	}
	c.Set("provider_pool_owner_id", account.OwnerID)
	return account, nil
}

func RefreshProviderPoolAccount(ctx context.Context, account ProviderPoolAccount) error {
	plugin, ok := PluginOfChannel(account.Owner)
	if !ok || !plugin.HasProviderHook("scheduled") {
		return errors.New("account quota refresh is not supported")
	}
	_, _, _, err := runScheduledPlugin(ctx, account.Owner, plugin, account.Index, account.Line, "manual")
	return err
}

func FindProviderPoolAccount(ctx context.Context, channel *model.Channel, accountID string) (ProviderPoolAccount, error) {
	accounts, err := ListProviderPoolAccounts(ctx, channel)
	if err != nil {
		return ProviderPoolAccount{}, err
	}
	for _, account := range accounts {
		if account.AccountID == accountID {
			return account, nil
		}
	}
	return ProviderPoolAccount{}, fmt.Errorf("account does not exist in this pool")
}

func RemoveProviderPoolAccount(account ProviderPoolAccount) error {
	stored, err := model.UpdateChannelLocked(account.OwnerID, func(channel *model.Channel) error {
		lines := pluginCredentialKeyLines(channel)
		kept := make([]string, 0, len(lines))
		statuses, reasons, times := map[int]int{}, map[int]string{}, map[int]int64{}
		removed := false
		for index, line := range lines {
			if credential, ok := jsplugin.ParseCredential(line); ok && credential.AccountID == account.AccountID {
				removed = true
				continue
			}
			next := len(kept)
			kept = append(kept, line)
			if value, ok := channel.ChannelInfo.MultiKeyStatusList[index]; ok {
				statuses[next] = value
			}
			if value, ok := channel.ChannelInfo.MultiKeyDisabledReason[index]; ok {
				reasons[next] = value
			}
			if value, ok := channel.ChannelInfo.MultiKeyDisabledTime[index]; ok {
				times[next] = value
			}
		}
		if !removed {
			return ErrPluginCredentialRemoved
		}
		channel.Key = strings.Join(kept, "\n")
		info := &channel.ChannelInfo
		info.MultiKeySize = len(kept)
		info.MultiKeyStatusList, info.MultiKeyDisabledReason, info.MultiKeyDisabledTime = statuses, reasons, times
		delete(info.ProviderAccounts, account.AccountID)
		return nil
	})
	if err == nil {
		model.CacheUpdateChannel(stored)
	}
	return err
}
