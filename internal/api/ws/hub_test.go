package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHub(t *testing.T) {
	h := NewHub()
	assert.NotNil(t, h.clients)
	assert.Empty(t, h.clients)
}

func TestBroadcastToEmptyChannel(t *testing.T) {
	h := NewHub()
	h.Broadcast("nobody-listens", map[string]string{"a": "b"})
	h.Broadcast("default", "hello")
}

func TestBroadcastUnmarshalableValue(t *testing.T) {
	h := NewHub()
	// channels can't be JSON-marshaled; Broadcast must silently skip
	h.Broadcast("default", map[string]chan int{"c": make(chan int)})
}

func TestHubWebSocketRoundTrip(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?channel=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// wait for the server to register the client
	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients["test"]) == 1
	}, time.Second, 10*time.Millisecond)

	payload := map[string]string{"event": "scan.progress", "library_id": "lib-1"}
	h.Broadcast("test", payload)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)

	var got map[string]string
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, payload, got)
}

func TestHubDefaultChannel(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") // no channel param
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients["default"]) == 1
	}, time.Second, 10*time.Millisecond)

	h.Broadcast("default", "ping")
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, `"ping"`, string(data), "payload is JSON-encoded")
}

func TestHubDisconnectRemovesClient(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?channel=bye"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients["bye"]) == 1
	}, time.Second, 10*time.Millisecond)

	conn.Close()

	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients["bye"]) == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBroadcastOnlyReachesTargetChannel(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	connA, _, err := websocket.DefaultDialer.Dial(base+"?channel=a", nil)
	require.NoError(t, err)
	defer connA.Close()
	connB, _, err := websocket.DefaultDialer.Dial(base+"?channel=b", nil)
	require.NoError(t, err)
	defer connB.Close()

	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients["a"]) == 1 && len(h.clients["b"]) == 1
	}, time.Second, 10*time.Millisecond)

	h.Broadcast("a", "only-a")
	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, dataA, err := connA.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, `"only-a"`, string(dataA), "payload is JSON-encoded")

	// channel b must not receive anything; verify it can still get its own broadcast
	h.Broadcast("b", "only-b")
	connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, dataB, err := connB.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, `"only-b"`, string(dataB))
}
