package rest

import (
	"context"
	"sync"
)

// Long-lived connection registry: SSE log streams register here so server
// shutdown can cancel them before http.Shutdown waits for active handlers
// (http.Shutdown neither interrupts in-flight requests nor cancels their
// contexts). The registry is intentionally transport-agnostic: the
// WebSocket sync hub can join later by registering a cancel func that
// closes its connections.

var (
	longConnsMu     sync.Mutex
	longConns       = make(map[int]context.CancelFunc)
	longConnSeq     int
	longConnsClosed bool
)

// RegisterLongConn stores a connection's cancel func and returns an
// unregister func to call when the connection ends. Once shutdown has
// begun (CancelLongConns ran), every late registration is cancelled
// immediately instead of being stored — otherwise streams created between
// CancelLongConns and the listener close would outlive shutdown.
func RegisterLongConn(cancel context.CancelFunc) func() {
	longConnsMu.Lock()
	if longConnsClosed {
		longConnsMu.Unlock()
		cancel()
		return func() {}
	}
	longConnSeq++
	id := longConnSeq
	longConns[id] = cancel
	longConnsMu.Unlock()
	return func() {
		longConnsMu.Lock()
		delete(longConns, id)
		longConnsMu.Unlock()
	}
}

// CancelLongConns cancels every registered long connection. Called at
// server shutdown so http.Shutdown never waits for open streams.
func CancelLongConns() {
	longConnsMu.Lock()
	longConnsClosed = true
	cancels := make([]context.CancelFunc, 0, len(longConns))
	for _, cancel := range longConns {
		cancels = append(cancels, cancel)
	}
	longConns = make(map[int]context.CancelFunc)
	longConnsMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// ActiveLongConns reports how many long connections are registered.
func ActiveLongConns() int {
	longConnsMu.Lock()
	defer longConnsMu.Unlock()
	return len(longConns)
}
