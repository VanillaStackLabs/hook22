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
