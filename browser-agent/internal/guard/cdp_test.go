package guard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeEvent sends one Fetch.requestPaused event with a sequence index.
func writeEvent(conn *websocket.Conn, index int) error {
	payload, err := json.Marshal(map[string]any{
		"method": "Fetch.requestPaused",
		"params": map[string]any{"index": index},
	})
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// TestCDPClientDeliversResponsesWhileEventsFlood pins the fix for a deadlock
// that stopped a live runtime: the read loop is the only goroutine that can
// deliver a command response, so a bounded event queue that filled up while the
// handler waited for a response left that response unread until the command
// timed out, and the guard then stopped the runtime (fail closed). The flood is
// larger than the buffer that used to deadlock the client.
func TestCDPClientDeliversResponsesWhileEventsFlood(t *testing.T) {
	const flood = 4096

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// One event starts the handler, which then blocks on a command while
		// the browser keeps flooding events, which is what a provider page load
		// does.
		if err := writeEvent(conn, 0); err != nil {
			return
		}
		var command cdpMessage
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := json.Unmarshal(data, &command); err != nil || command.Method == "" {
				continue
			}
			break
		}
		for index := 1; index < flood; index++ {
			if err := writeEvent(conn, index); err != nil {
				return
			}
		}
		reply, err := json.Marshal(map[string]any{"id": command.ID, "result": map[string]any{}})
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, reply)
		// Keep the channel open until the test closes it.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var (
		mu        sync.Mutex
		indices   []int
		flooding  = make(chan struct{})
		floodOnce sync.Once
	)
	var client *cdpClient
	client = newCDPClient(conn, func(ctx context.Context, _ string, _ string, params json.RawMessage) error {
		var event struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(params, &event); err != nil {
			return err
		}
		mu.Lock()
		indices = append(indices, event.Index)
		first := len(indices) == 1
		mu.Unlock()
		if first {
			floodOnce.Do(func() { close(flooding) })
			return client.call(ctx, "", "Flood.window", nil)
		}
		return nil
	})
	client.start(ctx)

	select {
	case <-flooding:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never started; the flood scenario did not run")
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		handled := len(indices)
		mu.Unlock()
		if handled >= flood {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d events reached the handler without a response timeout", handled, flood)
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-client.done:
		t.Fatalf("cdp client stopped while flooding: %v", client.fatalError())
	default:
	}

	mu.Lock()
	got := append([]int(nil), indices...)
	mu.Unlock()
	require.Len(t, got, flood)
	for index, value := range got {
		if !assert.Equal(t, index, value, "events must keep their arrival order") {
			break
		}
	}
}
