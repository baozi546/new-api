package model

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"gorm.io/gorm"
)

// TransferProviderPoolAccount moves one provider credential and its inspection
// and quota state between account-pool channels in one transaction.
func TransferProviderPoolAccount(sourceID, targetID int, pluginKey, accountID string) error {
	if sourceID <= 0 || targetID <= 0 || sourceID == targetID || pluginKey == "" || accountID == "" {
		return errors.New("invalid account transfer")
	}
	channelIDs := []int{sourceID, targetID}
	slices.Sort(channelIDs)
	locks := make([]func(), 0, len(channelIDs)*2)
	for _, channelID := range channelIDs {
		lock := GetChannelPollingLock(channelID)
		lock.Lock()
		locks = append(locks, lock.Unlock)
	}
	defer func() {
		for index := len(locks) - 1; index >= 0; index-- {
			locks[index]()
		}
	}()
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		sqlitePluginLoginLock.Lock()
		locks = append(locks, sqlitePluginLoginLock.Unlock)
		for _, channelID := range channelIDs {
			lock := sqliteChannelKeyLock(channelID)
			lock.Lock()
			locks = append(locks, lock.Unlock)
		}
	}

	var source, target *Channel
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channels []*Channel
		if err := lockForUpdate(tx).Where("type = ?", constant.ChannelTypeTaskPlugin).Order("id").Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			if channel.Id == sourceID {
				source = channel
			}
			if channel.Id == targetID {
				target = channel
			}
		}
		if source == nil || target == nil || source.GetSetting().TaskPluginKey != pluginKey || target.GetSetting().TaskPluginKey != pluginKey || !source.ChannelInfo.IsMultiKey || !target.ChannelInfo.IsMultiKey {
			return errors.New("source and target must use the same account-pool plugin")
		}
		if strings.HasPrefix(strings.TrimSpace(source.Key), "[") || strings.HasPrefix(strings.TrimSpace(target.Key), "[") {
			return errors.New("account-pool channel key format cannot be transferred")
		}

		sourceLines := channelPluginCredentialLines(source)
		targetLines := channelPluginCredentialLines(target)
		sourceIndex := -1
		for index, line := range sourceLines {
			credential, ok := jsplugin.ParseCredential(line)
			if ok && credential.Plugin == pluginKey && credential.AccountID == accountID {
				sourceIndex = index
				break
			}
		}
		if sourceIndex < 0 {
			return errors.New("account does not exist in the source channel")
		}
		if state, ok := source.ChannelInfo.ProviderAccounts[accountID]; ok && state.Profile.PlanVerified {
			category := strings.ToLower(strings.TrimSpace(state.Profile.Category))
			if category != "" && !slices.ContainsFunc(target.GetGroups(), func(group string) bool {
				return strings.ToLower(strings.TrimSpace(group)) == category
			}) {
				return errors.New("target channel does not serve the account subscription group")
			}
		}
		for _, line := range targetLines {
			credential, ok := jsplugin.ParseCredential(line)
			if ok && credential.Plugin == pluginKey && credential.AccountID == accountID {
				return errors.New("account already exists in the target channel")
			}
		}
		for _, channel := range channels {
			if channel.Id == sourceID || channel.GetSetting().TaskPluginKey != pluginKey {
				continue
			}
			for _, line := range channelPluginCredentialLines(channel) {
				credential, ok := jsplugin.ParseCredential(line)
				if ok && credential.Plugin == pluginKey && credential.AccountID == accountID {
					return errors.New("account is already assigned to another channel")
				}
			}
		}

		movedLine := sourceLines[sourceIndex]
		sourceLines = append(sourceLines[:sourceIndex], sourceLines[sourceIndex+1:]...)
		targetLines = append(targetLines, movedLine)
		moveChannelKeyStatus(&source.ChannelInfo, sourceIndex, sourceLines)
		if source.ChannelInfo.MultiKeyPollingIndex > sourceIndex {
			source.ChannelInfo.MultiKeyPollingIndex--
		}
		target.ChannelInfo.MultiKeySize = len(targetLines)
		if target.ChannelInfo.MultiKeyPollingIndex >= len(targetLines) {
			target.ChannelInfo.MultiKeyPollingIndex = 0
		}
		source.Key = strings.Join(sourceLines, "\n")
		target.Key = strings.Join(targetLines, "\n")
		source.Keys, target.Keys = nil, nil
		state, hasState := source.ChannelInfo.ProviderAccounts[accountID]
		if target.ChannelInfo.ProviderAccounts == nil {
			target.ChannelInfo.ProviderAccounts = make(map[string]ProviderAccountState)
		}
		if hasState {
			target.ChannelInfo.ProviderAccounts[accountID] = state
		} else {
			delete(target.ChannelInfo.ProviderAccounts, accountID)
		}
		delete(source.ChannelInfo.ProviderAccounts, accountID)
		if err := tx.Model(source).Updates(map[string]any{"key": source.Key, "channel_info": source.ChannelInfo}).Error; err != nil {
			return err
		}
		return tx.Model(target).Updates(map[string]any{"key": target.Key, "channel_info": target.ChannelInfo}).Error
	})
	if err != nil {
		return err
	}
	CacheUpdateChannel(source)
	CacheUpdateChannel(target)
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

func channelPluginCredentialLines(channel *Channel) []string {
	if strings.TrimSpace(channel.Key) == "" {
		return nil
	}
	return strings.Split(strings.Trim(channel.Key, "\n"), "\n")
}

func moveChannelKeyStatus(info *ChannelInfo, removedIndex int, remaining []string) {
	statuses := make(map[int]int, len(info.MultiKeyStatusList))
	reasons := make(map[int]string, len(info.MultiKeyDisabledReason))
	times := make(map[int]int64, len(info.MultiKeyDisabledTime))
	for oldIndex := range len(remaining) + 1 {
		if oldIndex == removedIndex {
			continue
		}
		newIndex := oldIndex
		if oldIndex > removedIndex {
			newIndex--
		}
		if status, ok := info.MultiKeyStatusList[oldIndex]; ok {
			statuses[newIndex] = status
		}
		if reason, ok := info.MultiKeyDisabledReason[oldIndex]; ok {
			reasons[newIndex] = reason
		}
		if disabledTime, ok := info.MultiKeyDisabledTime[oldIndex]; ok {
			times[newIndex] = disabledTime
		}
	}
	info.MultiKeyStatusList = statuses
	info.MultiKeyDisabledReason = reasons
	info.MultiKeyDisabledTime = times
	info.MultiKeySize = len(remaining)
	if info.MultiKeyPollingIndex >= len(remaining) {
		info.MultiKeyPollingIndex = 0
	}
}
