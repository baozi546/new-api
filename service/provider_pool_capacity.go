package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

type providerAccountCapacity struct {
	used    int
	changed chan struct{}
}

var providerCapacityMu sync.Mutex
var providerCapacities = map[string]*providerAccountCapacity{}

// Redis time avoids node clock skew. Every slot has its own renewable lease,
// so a dead process releases capacity without allowing live streams to expire.
const providerSlotAcquireScript = `
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then return 0 end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[3]), ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1`

const providerSlotRenewScript = `
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end
local t = redis.call('TIME')
redis.call('ZADD', KEYS[1], t[1] * 1000 + math.floor(t[2] / 1000) + tonumber(ARGV[2]), ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1`

// AcquireProviderAccountSlot holds capacity until release, including response
// streaming. The returned context cancels upstream work if its lease is lost.
func AcquireProviderAccountSlot(ctx context.Context, accountKey string, limit int) (context.Context, func(), error) {
	if limit < 1 {
		return nil, nil, errors.New("account concurrency must be positive")
	}
	if common.RedisEnabled && common.RDB != nil {
		return acquireRedisProviderSlot(ctx, accountKey, limit)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		providerCapacityMu.Lock()
		state := providerCapacities[accountKey]
		if state == nil {
			state = &providerAccountCapacity{changed: make(chan struct{})}
			providerCapacities[accountKey] = state
		}
		if state.used < limit {
			state.used++
			providerCapacityMu.Unlock()
			var once sync.Once
			release := func() {
				once.Do(func() {
					providerCapacityMu.Lock()
					defer providerCapacityMu.Unlock()
					state.used--
					close(state.changed)
					state.changed = make(chan struct{})
					if state.used == 0 {
						delete(providerCapacities, accountKey)
					}
				})
			}
			stopRelease := context.AfterFunc(ctx, release)
			return ctx, func() {
				stopRelease()
				release()
			}, nil
		}
		changed := state.changed
		providerCapacityMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-changed:
		}
	}
}

func acquireRedisProviderSlot(ctx context.Context, accountKey string, limit int) (context.Context, func(), error) {
	client := common.RDB
	key, lease := "provider-account-slot:"+accountKey, uuid.NewString()
	const ttl = 3 * time.Minute
	for {
		acquired, err := client.Eval(ctx, providerSlotAcquireScript, []string{key}, lease, limit, ttl.Milliseconds()).Int()
		if err != nil {
			return nil, nil, err
		}
		if acquired == 1 {
			break
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	leaseCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			<-done
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			_ = client.ZRem(releaseCtx, key, lease).Err()
		})
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(leaseCtx, 5*time.Second)
				renewed, err := client.Eval(renewCtx, providerSlotRenewScript, []string{key}, lease, ttl.Milliseconds()).Int()
				renewCancel()
				if err != nil || renewed != 1 {
					cancel()
					return
				}
			}
		}
	}()
	stopRelease := context.AfterFunc(leaseCtx, release)
	return leaseCtx, func() {
		stopRelease()
		release()
	}, nil
}

// A shared account obeys the strictest limit of channels eligible to use it.
func ProviderPoolAccountLimit(account ProviderPoolAccount) int {
	limit := 10000
	for _, channel := range account.Channels {
		settings := ProviderPoolSettings{Category: channel.Category, Concurrency: channel.Concurrency}
		if ProviderAccountMatches(account, settings.Category) {
			limit = min(limit, max(1, settings.Concurrency))
		}
	}
	return limit
}
