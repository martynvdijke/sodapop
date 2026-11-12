// Package realtime carries in-process notification signals from the write path
// to live consumers such as the Server-Sent Events stream.
//
// Sodapop runs as one process against SQLite, so a goroutine-safe fan-out is
// enough and needs no broker. Publish never blocks: a connection that cannot
// keep up loses events, and the stream's reconciliation tick re-reads the
// source of truth, so a dropped event costs latency but never correctness.
//
// The bus is deliberately in-process. Running replicas behind a load balancer
// would strand a user on one process and miss signals published by another;
// that deployment would need a shared transport instead.
package realtime

import "sync"

// Event identifies a notification that was just written for a user. It carries
// no content: consumers re-read the row so block, visibility and soft-delete
// rules are applied once, at read time, instead of being duplicated here.
type Event struct {
	UserID         string
	NotificationID string
}

// subscriberBuffer bounds per-connection work so a slow reader cannot stall the
// request that wrote the notification. Excess events are dropped.
const subscriberBuffer = 16

// Bus fans one Event out to every live connection of the same user.
type Bus struct {
	mu     sync.RWMutex
	nextID int
	subs   map[string]map[int]chan Event
}

// New returns a ready bus.
func New() *Bus {
	return &Bus{subs: make(map[string]map[int]chan Event)}
}

// Subscribe returns a channel of events for userID and an idempotent cancel
// function. The channel is closed when cancel runs.
func (b *Bus) Subscribe(userID string) (<-chan Event, func()) {
	if b == nil || userID == "" {
		closed := make(chan Event)
		close(closed)
		return closed, func() {}
	}

	ch := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	if b.subs == nil {
		b.subs = make(map[string]map[int]chan Event)
	}
	b.nextID++
	id := b.nextID
	if b.subs[userID] == nil {
		b.subs[userID] = make(map[int]chan Event)
	}
	b.subs[userID][id] = ch
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			// Taking the write lock keeps Publish (read lock) from sending on a
			// channel this call is about to close.
			b.mu.Lock()
			defer b.mu.Unlock()

			group, ok := b.subs[userID]
			if !ok {
				return
			}
			if _, ok := group[id]; !ok {
				return
			}
			delete(group, id)
			if len(group) == 0 {
				delete(b.subs, userID)
			}
			close(ch)
		})
	}
	return ch, cancel
}

// Publish delivers e to every current subscriber of e.UserID without blocking.
// A full subscriber channel drops the event for that connection.
func (b *Bus) Publish(e Event) {
	if b == nil || e.UserID == "" {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[e.UserID] {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribers reports the number of live connections for userID.
func (b *Bus) Subscribers(userID string) int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[userID])
}
