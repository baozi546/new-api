package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
)

type ProviderPoolSettings struct {
	Concurrency int `json:"concurrency"`
}

var providerPoolCursors sync.Map

func GetProviderPoolSettings(channelID int) ProviderPoolSettings {
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		return ProviderPoolSettings{Concurrency: 1}
	}
	return ProviderPoolSettings{Concurrency: max(1, channel.ChannelInfo.ProviderPoolConcurrency)}
}

func (s ProviderPoolSettings) Valid() bool {
	return s.Concurrency >= 1 && s.Concurrency <= 10000
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
	Concurrency int      `json:"concurrency"`
}

func ListProviderPoolAccounts(ctx context.Context, channel *model.Channel, includeUncachedProfiles ...bool) ([]ProviderPoolAccount, error) {
	plugin, ok := PluginOfChannel(channel)
	if !ok || !plugin.Meta.AccountPool {
		return nil, errors.New("channel does not use an account pool plugin")
	}
	if channel.GetSetting().TaskPluginKey != plugin.Meta.Key {
		return nil, errors.New("channel plugin changed while loading its accounts")
	}
	var accounts []ProviderPoolAccount
	assignedChannel := ProviderPoolChannel{ID: channel.Id, Name: channel.Name, Groups: channel.GetGroups(),
		Concurrency: max(1, channel.ChannelInfo.ProviderPoolConcurrency)}
	for index, line := range pluginCredentialKeyLines(channel) {
		credential, ok := jsplugin.ParseCredential(line)
		if !ok || credential.Plugin != plugin.Meta.Key || credential.AccountID == "" {
			continue
		}
		state := channel.ChannelInfo.ProviderAccounts[credential.AccountID]
		profile := state.Profile
		if len(includeUncachedProfiles) > 0 && includeUncachedProfiles[0] && (profile.Plan == "" || profile.Windows == nil) {
			described, err := plugin.CallDescribeAccount(ctx, jsplugin.CredentialContext{BaseURL: channel.GetBaseURL(), Credential: credential.Data})
			if err != nil {
				return nil, errors.New("account profile could not be read")
			}
			profile = described
		}
		needsLogin := credential.NeedsLogin || profile.QuotaError == "login_required" || state.Health == "login_required" || state.Health == "account_banned"
		accounts = append(accounts, ProviderPoolAccount{Owner: channel, Line: line, Index: index, OwnerID: channel.Id,
			AccountID: credential.AccountID, Label: credential.Label, ExpiresAt: credential.ExpiresAt,
			NeedsLogin: needsLogin, State: state, Profile: profile, Channels: []ProviderPoolChannel{assignedChannel}})
	}
	return accounts, nil
}

func ProviderAccountMatches(account ProviderPoolAccount, customerGroup, legacyCategory string) bool {
	customerGroup = strings.ToLower(strings.TrimSpace(customerGroup))
	if customerGroup != "plus" && customerGroup != "pro" {
		legacyCategory = strings.ToLower(strings.TrimSpace(legacyCategory))
		if legacyCategory == "plus" || legacyCategory == "pro" {
			customerGroup = legacyCategory
		}
	}
	if customerGroup == "plus" || customerGroup == "pro" {
		return !account.State.Degraded && account.Profile.PlanVerified && account.Profile.Category == customerGroup
	}
	return !account.State.Degraded && account.Profile.PlanVerified &&
		(account.Profile.Category == "plus" || account.Profile.Category == "pro")
}

func ProviderAccountUsable(account ProviderPoolAccount) bool {
	return !account.State.Disabled && !account.NeedsLogin && account.State.Health != "account_banned"
}

// SelectProviderPoolAccount uses an exact account on inspections. Ordinary
// requests first honor session affinity (reusing the channel affinity session
// key within TTL so a conversation stays on the same healthy account). When no
// bound account is available, it prefers the least-loaded (idle) eligible
// accounts and breaks ties with round-robin polling.
func SelectProviderPoolAccount(c *gin.Context, channel *model.Channel) (string, int, error) {
	accounts, err := ListProviderPoolAccounts(c.Request.Context(), channel)
	if err != nil {
		return "", 0, err
	}
	customerGroup := c.GetString("group")
	fixed := c.GetString("provider_pool_fixed_account")
	eligible := make([]ProviderPoolAccount, 0, len(accounts))
	for _, account := range accounts {
		if !ProviderAccountMatches(account, customerGroup, channel.ChannelInfo.ProviderPoolCategory) || !ProviderAccountUsable(account) || fixed != "" && account.AccountID != fixed {
			continue
		}
		eligible = append(eligible, account)
	}
	if len(eligible) == 0 {
		return "", 0, errors.New("no available account matching the customer group")
	}
	var account ProviderPoolAccount
	selected := false
	if fixed == "" {
		if preferredID, ok := getPreferredProviderPoolAccount(c, channel.Id); ok {
			for _, candidate := range eligible {
				if candidate.AccountID == preferredID {
					account = candidate
					selected = true
					break
				}
			}
		}
	}
	if !selected {
		candidates := eligible
		if len(eligible) > 1 {
			pluginKey := channel.GetSetting().TaskPluginKey
			accountKeys := make([]string, len(eligible))
			for i, candidate := range eligible {
				accountKeys[i] = ProviderPoolAccountKey(pluginKey, candidate.AccountID)
			}
			activeSlots := ProviderAccountActiveSlots(c.Request.Context(), accountKeys)
			minActive := activeSlots[accountKeys[0]]
			for i := 1; i < len(eligible); i++ {
				if active := activeSlots[accountKeys[i]]; active < minActive {
					minActive = active
				}
			}
			leastLoaded := make([]ProviderPoolAccount, 0, len(eligible))
			for i, candidate := range eligible {
				if activeSlots[accountKeys[i]] == minActive {
					leastLoaded = append(leastLoaded, candidate)
				}
			}
			candidates = leastLoaded
		}
		cursor, _ := providerPoolCursors.LoadOrStore(channel.Id, &atomic.Uint64{})
		index := (cursor.(*atomic.Uint64).Add(1) - 1) % uint64(len(candidates))
		account = candidates[index]
		if fixed == "" {
			setProviderPoolAccountAffinity(c, channel.Id, account.AccountID)
		}
	}
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
			state.LastError = fmt.Sprintf("HTTP %d: account was rejected by upstream", status)
			if status == 401 {
				state.Health = "login_required"
				state.LastError = "HTTP 401: account login is required"
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
	if !ProviderAccountUsable(account) || !ProviderAccountMatches(account, c.GetString("group"), channel.ChannelInfo.ProviderPoolCategory) {
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

// TransferProviderPoolAccount moves an account only between channels bound to
// the same account-pool plugin, preserving the account's quota and health data.
func TransferProviderPoolAccount(sourceID, targetID int, accountID string) error {
	source, err := model.GetChannelById(sourceID, true)
	if err != nil || !IsProviderPool(source) {
		return errors.New("source channel does not use an account pool")
	}
	target, err := model.GetChannelById(targetID, true)
	if err != nil || !IsProviderPool(target) {
		return errors.New("target channel does not use an account pool")
	}
	pluginKey := source.GetSetting().TaskPluginKey
	if target.GetSetting().TaskPluginKey != pluginKey {
		return errors.New("source and target use different account-pool plugins")
	}
	return model.TransferProviderPoolAccount(sourceID, targetID, pluginKey, accountID)
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
