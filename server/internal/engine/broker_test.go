package engine

import (
	"sync"
	"testing"
)

func drain(ch <-chan []byte) (n int, open bool) {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return n, false
			}
			n++
		default:
			return n, true
		}
	}
}

// A subscriber that falls behind must find out (channel closed) instead of
// silently missing frames such as the final turn_done.
func TestBrokerClosesSlowSubscriber(t *testing.T) {
	b := NewBroker()
	slow, unsubSlow := b.Subscribe("r")
	fast, unsubFast := b.Subscribe("r")
	defer unsubFast()

	for i := 0; i < brokerBuffer; i++ {
		b.Publish("r", []byte("acp"))
		if n, open := drain(fast); n != 1 || !open {
			t.Fatalf("fast subscriber frame %d: n=%d open=%v", i, n, open)
		}
	}
	b.Publish("r", []byte("turn_done"))

	n, open := drain(slow)
	if open || n != brokerBuffer {
		t.Fatalf("slow subscriber: got %d frames open=%v, want %d then closed", n, open, brokerBuffer)
	}
	if n, open := drain(fast); n != 1 || !open {
		t.Fatalf("fast subscriber must still get turn_done: n=%d open=%v", n, open)
	}
	unsubSlow() // must not double-close
	b.Publish("r", []byte("after"))
	if n, open := drain(fast); n != 1 || !open {
		t.Fatalf("fast subscriber after slow left: n=%d open=%v", n, open)
	}
}

func TestBrokerConcurrentPublishUnsubscribe(t *testing.T) {
	b := NewBroker()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		_, unsub := b.Subscribe("r")
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < brokerBuffer*2; j++ {
				b.Publish("r", []byte("x"))
			}
		}()
		go func() {
			defer wg.Done()
			unsub()
			unsub()
		}()
	}
	wg.Wait()
}
