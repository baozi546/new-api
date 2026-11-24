package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	redis "github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestAcquireProviderAccountSlotUsesRedisLease(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not configured")
	}
	previousClient, previousEnabled := common.RDB, common.RedisEnabled
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(context.Background()).Err())
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = previousClient, previousEnabled
		require.NoError(t, client.Close())
	})

	accountKey := ProviderPoolAccountKey("capacity-test", "redis-account")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	leaseContext, release, err := AcquireProviderAccountSlot(ctx, accountKey, 1)
	require.NoError(t, err)
	require.NoError(t, leaseContext.Err())
	defer release()
	require.EqualValues(t, 1, client.ZCard(ctx, "provider-account-slot:"+accountKey).Val())

	cancelled, cancelWait := context.WithCancel(ctx)
	cancelWait()
	_, _, err = AcquireProviderAccountSlot(cancelled, accountKey, 1)
	require.ErrorIs(t, err, context.Canceled)

	release()
	nextContext, nextRelease, err := AcquireProviderAccountSlot(ctx, accountKey, 1)
	require.NoError(t, err)
	require.NoError(t, nextContext.Err())
	nextRelease()
	require.EqualValues(t, 0, client.ZCard(ctx, "provider-account-slot:"+accountKey).Val())
}
