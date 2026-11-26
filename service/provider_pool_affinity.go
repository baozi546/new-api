package service

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const providerPoolAffinityCacheNamespace = "new-api:provider_pool_affinity:v1"

var (
	providerPoolAffinityCacheOnce sync.Once
	providerPoolAffinityCache     *cachex.HybridCache[string]
)

func getProviderPoolAffinityCache() *cachex.HybridCache[string] {
	providerPoolAffinityCacheOnce.Do(func() {
		setting := operation_setting.GetChannelAffinitySetting()
		capacity := 0
		defaultTTLSeconds := 0
		if setting != nil {
			capacity = setting.MaxEntries
			defaultTTLSeconds = setting.DefaultTTLSeconds
		}
		if capacity <= 0 {
			capacity = 100_000
		}
		if defaultTTLSeconds <= 0 {
			defaultTTLSeconds = 3600
		}
		providerPoolAffinityCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace(providerPoolAffinityCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, capacity).
					WithTTL(time.Duration(defaultTTLSeconds) * time.Second).
					WithJanitor().
					Build()
			},
		})
	})
	return providerPoolAffinityCache
}

// providerPoolAffinityKey derives a channel-scoped account affinity key from
// the existing channel affinity session key and TTL on the request context.
func providerPoolAffinityKey(c *gin.Context, channelID int) (string, time.Duration, bool) {
	if c == nil || channelID <= 0 {
		return "", 0, false
	}
	if RequestPolicy(c).SessionMode == "off" {
		return "", 0, false
	}
	setting := operation_setting.GetChannelAffinitySetting()
	if setting == nil || !setting.Enabled {
		return "", 0, false
	}
	cacheKey, ttlSeconds, ok := getChannelAffinityContext(c)
	if !ok || cacheKey == "" {
		return "", 0, false
	}
	suffix := strings.TrimPrefix(cacheKey, channelAffinityCacheNamespace+":")
	if suffix == "" {
		return "", 0, false
	}
	if ttlSeconds <= 0 {
		ttlSeconds = setting.DefaultTTLSeconds
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	return suffix + ":ch:" + strconv.Itoa(channelID), time.Duration(ttlSeconds) * time.Second, true
}

func getPreferredProviderPoolAccount(c *gin.Context, channelID int) (string, bool) {
	key, _, ok := providerPoolAffinityKey(c, channelID)
	if !ok {
		return "", false
	}
	accountID, found, err := getProviderPoolAffinityCache().Get(key)
	if err != nil || !found || strings.TrimSpace(accountID) == "" {
		return "", false
	}
	return accountID, true
}

func setProviderPoolAccountAffinity(c *gin.Context, channelID int, accountID string) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return
	}
	key, ttl, ok := providerPoolAffinityKey(c, channelID)
	if !ok {
		return
	}
	if err := getProviderPoolAffinityCache().SetWithTTL(key, accountID, ttl); err != nil {
		common.SysError(common.LogText("provider pool affinity cache set failed: key=%s, err=%v", key, err))
	}
}

func recordProviderPoolAccountAffinity(c *gin.Context, channelID int) {
	if c == nil || channelID <= 0 {
		return
	}
	accountID := strings.TrimSpace(c.GetString("provider_pool_account_id"))
	if accountID == "" {
		return
	}
	setProviderPoolAccountAffinity(c, channelID, accountID)
}

func clearCurrentProviderPoolAccountAffinity(c *gin.Context) {
	if c == nil {
		return
	}
	channelID := c.GetInt("channel_id")
	if channelID <= 0 {
		channelID = c.GetInt("provider_pool_owner_id")
	}
	key, _, ok := providerPoolAffinityKey(c, channelID)
	if !ok {
		return
	}
	_, _ = getProviderPoolAffinityCache().DeleteMany([]string{key})
}

func clearProviderPoolAffinityCacheAll() {
	cache := getProviderPoolAffinityCache()
	keys, err := cache.Keys()
	if err != nil || len(keys) == 0 {
		return
	}
	_, _ = cache.DeleteMany(keys)
}

func clearProviderPoolAffinityCacheByPrefix(prefix string) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return
	}
	_, _ = getProviderPoolAffinityCache().DeleteByPrefix(prefix)
}
