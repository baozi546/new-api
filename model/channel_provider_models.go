package model

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// MergeChannelModels stores newly discovered upstream model names and refreshes
// the channel abilities in the same transaction.
func MergeChannelModels(channelID int, discovered []string) (bool, error) {
	cleanModels := make([]string, 0, len(discovered))
	for _, name := range discovered {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 255 || strings.ContainsAny(name, ",\r\n") || slices.Contains(cleanModels, name) {
			continue
		}
		cleanModels = append(cleanModels, name)
	}
	if len(cleanModels) == 0 {
		return false, nil
	}

	pollingLock := GetChannelPollingLock(channelID)
	pollingLock.Lock()
	defer pollingLock.Unlock()
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		keyLock := sqliteChannelKeyLock(channelID)
		keyLock.Lock()
		defer keyLock.Unlock()
	}

	var channel Channel
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
			return err
		}
		models := channel.GetModels()
		for _, name := range cleanModels {
			if slices.Contains(models, name) {
				continue
			}
			models = append(models, name)
			changed = true
		}
		if !changed {
			return nil
		}
		channel.Models = strings.Join(models, ",")
		if err := tx.Model(&channel).Update("models", channel.Models).Error; err != nil {
			return err
		}
		return channel.UpdateAbilities(tx)
	})
	if err != nil || !changed {
		return changed, err
	}
	CacheUpdateChannel(&channel)
	BumpSyncTopic(SyncTopicChannels)
	return true, nil
}
