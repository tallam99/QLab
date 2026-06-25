//go:build testunit

// These tests pin the in-process Broker's contract: a published notification reaches
// exactly the subscribers of its pool, unsubscribing stops delivery, a full (slow)
// subscriber is skipped rather than blocking others, and RefreshAll nudges every
// subscriber for its own pool. The Postgres Listener half is exercised end-to-end in
// the integration suite (it needs a real database).
package realtime

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/store"
)

// recv drains one notification from ch without blocking, reporting whether one was
// present — so a test can assert "delivered" or "not delivered" deterministically.
func recv(ch <-chan store.ScheduleNotification) (store.ScheduleNotification, bool) {
	select {
	case n := <-ch:
		return n, true
	default:
		return store.ScheduleNotification{}, false
	}
}

func TestBroker(t *testing.T) {
	poolA := uuid.New()
	poolB := uuid.New()
	labID := uuid.New()

	t.Run("delivers only to subscribers of the published pool", func(t *testing.T) {
		b := NewBroker()
		chA, unsubA := b.Subscribe(poolA)
		defer unsubA()
		chB, unsubB := b.Subscribe(poolB)
		defer unsubB()

		b.Publish(store.ScheduleNotification{LabID: labID, PoolID: poolA, Kind: store.ScheduleChangeClockedOut})

		gotA, okA := recv(chA)
		require.True(t, okA, "subscriber of the published pool should receive it")
		require.Equal(t, store.ScheduleChangeClockedOut, gotA.Kind)
		require.Equal(t, poolA, gotA.PoolID)

		_, okB := recv(chB)
		require.False(t, okB, "subscriber of a different pool should not receive it")
	})

	t.Run("fans out to every subscriber of the same pool", func(t *testing.T) {
		b := NewBroker()
		ch1, unsub1 := b.Subscribe(poolA)
		defer unsub1()
		ch2, unsub2 := b.Subscribe(poolA)
		defer unsub2()

		b.Publish(store.ScheduleNotification{PoolID: poolA, Kind: store.ScheduleChangeSlotCreated})

		_, ok1 := recv(ch1)
		_, ok2 := recv(ch2)
		require.True(t, ok1)
		require.True(t, ok2)
	})

	t.Run("unsubscribe stops delivery and is idempotent", func(t *testing.T) {
		b := NewBroker()
		ch, unsub := b.Subscribe(poolA)

		unsub()
		unsub() // calling twice must not panic
		b.Publish(store.ScheduleNotification{PoolID: poolA, Kind: store.ScheduleChangeCancelled})

		_, ok := recv(ch)
		require.False(t, ok, "an unsubscribed channel should receive nothing")
	})

	t.Run("a full subscriber is skipped, not blocking the publisher", func(t *testing.T) {
		b := NewBroker()
		ch, unsub := b.Subscribe(poolA)
		defer unsub()

		// Publish one more than the buffer can hold; the overflow is dropped (non-blocking
		// send) and Publish still returns rather than deadlocking.
		for range subBuffer + 5 {
			b.Publish(store.ScheduleNotification{PoolID: poolA, Kind: store.ScheduleChangeClockedIn})
		}

		drained := 0
		for {
			if _, ok := recv(ch); !ok {
				break
			}
			drained++
		}
		require.Equal(t, subBuffer, drained, "buffer fills then overflow is dropped")
	})

	t.Run("RefreshAll nudges every subscriber for its own pool", func(t *testing.T) {
		b := NewBroker()
		chA, unsubA := b.Subscribe(poolA)
		defer unsubA()
		chB, unsubB := b.Subscribe(poolB)
		defer unsubB()

		b.RefreshAll()

		gotA, okA := recv(chA)
		require.True(t, okA)
		require.Equal(t, poolA, gotA.PoolID)
		require.Empty(t, gotA.Kind, "a refresh carries no specific kind")

		gotB, okB := recv(chB)
		require.True(t, okB)
		require.Equal(t, poolB, gotB.PoolID)
	})
}
