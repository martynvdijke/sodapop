package realtime

import (
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("subscription closed unexpectedly")
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func TestPublishReachesEverySubscriberOfTheUser(t *testing.T) {
	bus := New()
	first, cancelFirst := bus.Subscribe("alice")
	defer cancelFirst()
	second, cancelSecond := bus.Subscribe("alice")
	defer cancelSecond()
	other, cancelOther := bus.Subscribe("bob")
	defer cancelOther()

	bus.Publish(Event{UserID: "alice", NotificationID: "n1"})

	for _, ch := range []<-chan Event{first, second} {
		ev := receive(t, ch)
		if ev.UserID != "alice" || ev.NotificationID != "n1" {
			t.Fatalf("event = %+v, want alice/n1", ev)
		}
	}

	select {
	case ev := <-other:
		t.Fatalf("bob received alice's event: %+v", ev)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPublishDropsInsteadOfBlockingWhenReaderIsSlow(t *testing.T) {
	bus := New()
	ch, cancel := bus.Subscribe("alice")
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < subscriberBuffer*4; i++ {
			bus.Publish(Event{UserID: "alice", NotificationID: "n"})
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}

	if got := len(ch); got != subscriberBuffer {
		t.Fatalf("buffered events = %d, want %d", got, subscriberBuffer)
	}
}

func TestCancelClosesAndIsIdempotent(t *testing.T) {
	bus := New()
	ch, cancel := bus.Subscribe("alice")
	cancel()
	cancel()

	if _, ok := <-ch; ok {
		t.Fatal("channel still open after cancel")
	}
	if got := bus.Subscribers("alice"); got != 0 {
		t.Fatalf("subscribers = %d, want 0", got)
	}
}

func TestSubscribeWithoutUserIsClosed(t *testing.T) {
	bus := New()
	ch, cancel := bus.Subscribe("")
	defer cancel()
	if _, ok := <-ch; ok {
		t.Fatal("anonymous subscription should be closed")
	}
}

func TestNilBusIsSafe(t *testing.T) {
	var bus *Bus
	ch, cancel := bus.Subscribe("alice")
	defer cancel()
	if _, ok := <-ch; ok {
		t.Fatal("nil bus should return a closed channel")
	}
	bus.Publish(Event{UserID: "alice"})
	if bus.Subscribers("alice") != 0 {
		t.Fatal("nil bus reported subscribers")
	}
}
