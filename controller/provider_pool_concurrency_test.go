package controller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestProviderPoolRequestCancellationReleasesSlot(t *testing.T) {
	account := service.ProviderPoolAccountKey("provider", "cancellation-account")
	ctx, cancel := context.WithCancel(context.Background())
	_, release, err := service.AcquireProviderAccountSlot(ctx, account, 1)
	require.NoError(t, err)
	defer release()

	var acquired atomic.Bool
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, nextRelease, waitErr := service.AcquireProviderAccountSlot(context.Background(), account, 1)
		if waitErr != nil {
			return
		}
		acquired.Store(true)
		nextRelease()
	}()

	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancelled account request kept its concurrency slot")
	}
	require.True(t, acquired.Load())
}
