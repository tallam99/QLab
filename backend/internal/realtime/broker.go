// Package realtime fans pool-schedule changes out to live subscribers. A single
// Listener per process holds a dedicated Postgres connection and turns
// ScheduleNotifyChannel notifications (emitted transactionally by the store) into
// in-process Broker deliveries; the SSE stream handler subscribes to the Broker for
// the pool it is watching. Postgres LISTEN/NOTIFY is the cross-instance hop, so a
// write on any instance reaches subscribers on every instance (decision 0010).
package realtime

import (
	"sync"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/store"
)

// subBuffer sizes each subscriber's channel. Sends are non-blocking, so the buffer
// only smooths brief bursts; a subscriber that stays behind drops notifications
// (see Subscribe).
const subBuffer = 16

// Broker is the in-process fan-out: pool id -> the set of subscriber channels
// watching that pool. Safe for concurrent use.
type Broker struct {
	mu     sync.Mutex
	subs   map[uuid.UUID]map[int]chan store.ScheduleNotification
	nextID int
}

// NewBroker returns an empty broker ready to take subscriptions.
func NewBroker() *Broker {
	return &Broker{subs: make(map[uuid.UUID]map[int]chan store.ScheduleNotification)}
}

// Subscribe registers interest in one pool's schedule changes and returns a receive
// channel plus an unsubscribe func the caller MUST invoke (defer it) to release the
// subscription. The channel is buffered and every send to it is non-blocking: a
// subscriber that falls behind drops notifications rather than stalling the listener
// or a writer. That is safe because a notification only means "the schedule
// changed, recompute it" — a dropped one is superseded by any later one, and the
// reader recomputes current state regardless. The channel is deliberately never
// closed (a concurrent Publish could otherwise race a close and panic); the reader
// stops by selecting on its own context, and the unsubscribed channel is GC'd.
func (b *Broker) Subscribe(poolID uuid.UUID) (<-chan store.ScheduleNotification, func()) {
	ch := make(chan store.ScheduleNotification, subBuffer)

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	subs := b.subs[poolID]
	if subs == nil {
		subs = make(map[int]chan store.ScheduleNotification)
		b.subs[poolID] = subs
	}
	subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			if subs := b.subs[poolID]; subs != nil {
				delete(subs, id)
				if len(subs) == 0 {
					delete(b.subs, poolID)
				}
			}
			b.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

// Publish delivers a notification to every current subscriber of its pool. Sends are
// non-blocking (see Subscribe). Safe for concurrent use.
func (b *Broker) Publish(n store.ScheduleNotification) {
	b.mu.Lock()
	chans := make([]chan store.ScheduleNotification, 0, len(b.subs[n.PoolID]))
	for _, ch := range b.subs[n.PoolID] {
		chans = append(chans, ch)
	}
	b.mu.Unlock()

	// Send outside the lock; non-blocking so one stuck subscriber can't delay the rest.
	for _, ch := range chans {
		select {
		case ch <- n:
		default:
		}
	}
}

// RefreshAll nudges every current subscriber with a kind-less notification for its
// own pool. The listener calls it on each (re)connect so any change missed while the
// Postgres connection was down is closed by a fresh recompute on the subscriber side.
func (b *Broker) RefreshAll() {
	type target struct {
		poolID uuid.UUID
		ch     chan store.ScheduleNotification
	}
	b.mu.Lock()
	targets := make([]target, 0)
	for poolID, subs := range b.subs {
		for _, ch := range subs {
			targets = append(targets, target{poolID: poolID, ch: ch})
		}
	}
	b.mu.Unlock()

	for _, t := range targets {
		select {
		case t.ch <- store.ScheduleNotification{PoolID: t.poolID}:
		default:
		}
	}
}
