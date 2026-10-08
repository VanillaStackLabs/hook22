package observability

import (
	"os"
	"sync"
)

type LogBroadcaster struct {
	mu      sync.RWMutex
	clients map[chan []byte]bool
}

func NewLogBroadcaster() *LogBroadcaster {
	return &LogBroadcaster{
		clients: make(map[chan []byte]bool),
	}
}

// Write satisfies io.Writer so slog can write to it
func (b *LogBroadcaster) Write(p []byte) (int, error) {
	// 1. Write to standard terminal output
	os.Stdout.Write(p)

	// Make a copy of the buffer to safely pass to channel readers
	buf := make([]byte, len(p))
	copy(buf, p)

	// Broadcast to all active SSE web connections
	b.mu.RLock()
	defer b.mu.RUnlock()
	for clientChan := range b.clients {
		select {
		case clientChan <- buf:
		default: // Non-blocking write if client buffer is full
		}
	}

	return len(p), nil
}

func (b *LogBroadcaster) Subscribe() chan []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 100)
	b.clients[ch] = true
	return ch
}

func (b *LogBroadcaster) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, ch)
	close(ch)
}
