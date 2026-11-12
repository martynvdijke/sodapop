package integrations

import "testing"

func TestSignalWakeCoalescesAndNeverBlocks(t *testing.T) {
	var s Service
	for i := 0; i < 5; i++ {
		s.signalWake()
	}
	if got := len(s.wakeChan()); got != 1 {
		t.Fatalf("pending wakes = %d, want 1", got)
	}
	<-s.wakeChan()
	if got := len(s.wakeChan()); got != 0 {
		t.Fatalf("pending wakes after drain = %d, want 0", got)
	}
}
