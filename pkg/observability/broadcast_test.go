package observability

import (
	"testing"
	"time"
)

func TestLogBroadcaster_SubscribeAndBroadcast(t *testing.T) {
	broadcaster := NewLogBroadcaster()

	// Subscribe client 1
	client1 := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(client1)

	testMessage := []byte(`{"level":"INFO","msg":"test event"}` + "\n")

	// Write log chunk
	n, err := broadcaster.Write(testMessage)
	if err != nil || n != len(testMessage) {
		t.Fatalf("Broadcaster write failed: %v", err)
	}

	// Verify client 1 receives message over channel
	select {
	case received := <-client1:
		if string(received) != string(testMessage) {
			t.Errorf("Expected %s, got %s", string(testMessage), string(received))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for log broadcast")
	}
}

func TestLogBroadcaster_FullBufferNonBlocking(t *testing.T) {
	broadcaster := NewLogBroadcaster()
	client := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(client)

	// Fill the channel buffer completely (capacity is 100)
	for i := 0; i < 100; i++ {
		client <- []byte("filler log")
	}

	// This write should hit the 'default' non-blocking case and drop the message
	// for this specific client, rather than deadlocking the test.
	testMessage := []byte("overflow log\n")
	n, err := broadcaster.Write(testMessage)

	if err != nil {
		t.Fatalf("Expected write to succeed without blocking, got error: %v", err)
	}
	if n != len(testMessage) {
		t.Errorf("Expected to return length %d, got %d", len(testMessage), n)
	}
}
