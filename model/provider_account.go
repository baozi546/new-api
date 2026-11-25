package model

import (

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

// UpdateProviderPoolSettings changes the per-account concurrency cap without
// changing channel customer groups or the account assignments.
func UpdateProviderPoolSettings(channelID int, concurrency int) error {
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
		channel.ChannelInfo.ProviderPoolConcurrency = concurrency
		return tx.Model(&channel).Update("channel_info", channel.ChannelInfo).Error
	})
	if err == nil {
		BumpSyncTopic(SyncTopicChannels)
	}
	return err
}
