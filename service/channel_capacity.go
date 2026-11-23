package service

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func inspectionOption(key string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[key]
}
func ChannelLimitKey(id int) string { return "CPAChannelConcurrency/" + strconv.Itoa(id) }

type channelCapacity struct {
	used    int
	changed chan struct{}
}

var capacityMu sync.Mutex
var capacities = map[int]*channelCapacity{}

// A slot covers one upstream attempt, including the lifetime of a streaming response.
func AcquireChannelSlot(ctx context.Context, id int) (func(), error) {
	key := ChannelLimitKey(id)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		capacityMu.Lock()
		limit, _ := strconv.Atoi(inspectionOption(key))
		state := capacities[id]
		if state == nil {
			state = &channelCapacity{changed: make(chan struct{})}
			capacities[id] = state
		}
		if limit <= 0 || state.used < limit {
			state.used++
			capacityMu.Unlock()
			return func() {
				capacityMu.Lock()
				defer capacityMu.Unlock()
				state.used--
				close(state.changed)
				state.changed = make(chan struct{})
				if state.used == 0 {
					delete(capacities, id)
				}
			}, nil
		}
		changed := state.changed
		capacityMu.Unlock()
		// Re-read the option even when an administrator raises the limit mid-stream.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		case <-time.After(time.Second):
		}
	}
}
