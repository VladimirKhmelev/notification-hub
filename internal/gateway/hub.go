package gateway

import (
	"context"
	"log"
	"sync"
)

// hub broadcasts messages to all connected WebSocket clients.
type hub struct {
	mu      sync.RWMutex
	clients map[chan<- []byte]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[chan<- []byte]struct{})}
}

func (h *hub) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
	}
}

func (h *hub) broadcast(ctx context.Context, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			log.Println("ws-gateway: client too slow, dropping message")
		}
	}
}
