package service

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestChannelCapacityCancelAndRelease(t *testing.T) {
	original := common.OptionMap
	common.OptionMap = map[string]string{ChannelLimitKey(71): "1"}
	t.Cleanup(func() { common.OptionMap = original })
	release, err := AcquireChannelSlot(t.Context(), 71)
	require.NoError(t, err)
	other, err := AcquireChannelSlot(t.Context(), 72)
	require.NoError(t, err)
	other()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err = AcquireChannelSlot(ctx, 71)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	release()
	again, err := AcquireChannelSlot(t.Context(), 71)
	require.NoError(t, err)
	again()
}
