package model

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"gorm.io/gorm"
)

// ProviderAccountState follows the stable account ID across token refreshes and
// key reordering. Manual disable survives inspections and credential refreshes.
type ProviderAccountState struct {
	Disabled     bool                    `json:"disabled"`
	Health       string                  `json:"health,omitempty"`
	CheckedAt    int64                   `json:"checked_at,omitempty"`
	WrongAnswers int                     `json:"wrong_answers,omitempty"`
	Degraded     bool                    `json:"degraded,omitempty"`
	Profile      jsplugin.AccountProfile `json:"profile"`
}

// UpdateProviderPoolSettings changes the account filter and its customer-group
// abilities together, preserving credentials and custom groups.
func UpdateProviderPoolSettings(channelID int, category string, concurrency int) error {
	pollingLock := GetChannelPollingLock(channelID)
	pollingLock.Lock()
	defer pollingLock.Unlock()
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		keyLock := sqliteChannelKeyLock(channelID)
		keyLock.Lock()
		defer keyLock.Unlock()
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ? AND type = ?", channelID, constant.ChannelTypeTaskPlugin).First(&channel).Error; err != nil {
			return err
		}
		if category != "unknown" {
			groups := slices.DeleteFunc(channel.GetGroups(), func(group string) bool {
				return slices.Contains([]string{"plus", "pro", "degraded"}, strings.ToLower(strings.TrimSpace(group)))
			})
			channel.Group = strings.Join(append(groups, category), ",")
		}
		channel.ChannelInfo.ProviderPoolCategory = category
		channel.ChannelInfo.ProviderPoolConcurrency = concurrency
		if err := tx.Model(&channel).Updates(map[string]any{
			"group": channel.Group, "channel_info": channel.ChannelInfo,
		}).Error; err != nil {
			return err
		}
		return channel.UpdateAbilities(tx)
	})
	if err == nil {
		BumpSyncTopic(SyncTopicChannels)
	}
	return err
}
