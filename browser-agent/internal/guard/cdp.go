package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// commandTimeout bounds one unanswered command. Losing the channel itself is
	// detected by the read loop, so this only covers a browser that is still
	// connected but too busy to answer; the guard still fails closed.
	commandTimeout = 30 * time.Second
	writeTimeout   = 10 * time.Second
)

// cdpError is the error object of one DevTools command response.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string {
	return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message)
}

// cdpMessage is one decoded DevTools JSON-RPC frame: commands and command
// responses carry an id, events carry a method instead.
type cdpMessage struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	Error     *cdpError       `json:"error"`
}

// cdpClient is the minimal DevTools JSON-RPC client the guard needs: it pairs
// every command response with its request id, hands events to a single handler
// in arrival order and reports every transport failure as fatal. A guard that
// can no longer see the browser must not let that browser keep running.
type cdpResponse struct {
	result json.RawMessage
	err    error
}

type cdpClient struct {
	conn    *websocket.Conn
	handler func(ctx context.Context, sessionID, method string, params json.RawMessage) error

	writeMu sync.Mutex

	idMu   sync.Mutex
	nextID int64

	pendingMu      sync.Mutex
	pending        map[int64]chan error
	pendingResults map[int64]chan cdpResponse

	events *eventQueue

	done     chan struct{}
	errOnce  sync.Once
	fatalErr error
}

func newCDPClient(conn *websocket.Conn, handler func(context.Context, string, string, json.RawMessage) error) *cdpClient {
	return &cdpClient{
		conn:           conn,
		handler:        handler,
		pending:        make(map[int64]chan error),
		pendingResults: make(map[int64]chan cdpResponse),
		events:         newEventQueue(),
		done:           make(chan struct{}),
	}
}

// start runs the read, event and shutdown loops. All of them stop when the
// channel fails or the caller's context is done.
func (c *cdpClient) start(ctx context.Context) {
	go c.readLoop(ctx)
	go c.eventLoop(ctx)
	go c.closeEventsOnCancel(ctx)
}

// closeEventsOnCancel releases the event loop when the caller's context ends,
// so a cancelled guard never waits for another event.
func (c *cdpClient) closeEventsOnCancel(ctx context.Context) {
	select {
	case <-ctx.Done():
		c.events.close()
	case <-c.done:
	}
}

// call sends one DevTools command and waits for its response. sessionID is
// empty for browser level commands. Errors from a command response are returned
// as they are; the caller decides whether that result is fatal.
func (c *cdpClient) call(ctx context.Context, sessionID, method string, params any) error {
	payload := struct {
		ID        int64           `json:"id"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params,omitempty"`
		SessionID string          `json:"sessionId,omitempty"`
	}{Method: method, SessionID: sessionID}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode %s params: %w", method, err)
		}
		payload.Params = raw
	}
	id := c.newID()
	payload.ID = id
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s command: %w", method, err)
	}

	reply := make(chan error, 1)
	c.pendingMu.Lock()
	c.pending[id] = reply
	c.pendingMu.Unlock()

	if err := c.write(data); err != nil {
		c.removePending(id)
		c.fail(fmt.Errorf("send %s command: %w", method, err))
		return fmt.Errorf("send %s command: %w", method, err)
	}

	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()
	select {
	case err := <-reply:
		return err
	case <-timer.C:
		c.removePending(id)
		return fmt.Errorf("%s command timed out", method)
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-c.done:
		return c.fatalError()
	}
}

// notify writes one command without waiting for its response. It is used for
// dispatch-only commands whose response carries no decision and whose renderer
// latency would otherwise gate the command loop. The write still happens on the
// single CDP channel, so ordering and the one-consumer rule are unchanged; the
// eventual response is simply discarded by deliver.
func (c *cdpClient) notify(sessionID, method string, params any) error {
	payload := struct {
		ID        int64           `json:"id"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params,omitempty"`
		SessionID string          `json:"sessionId,omitempty"`
	}{Method: method, SessionID: sessionID}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode %s params: %w", method, err)
		}
		payload.Params = raw
	}
	payload.ID = c.newID()
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s command: %w", method, err)
	}
	if err := c.write(data); err != nil {
		c.fail(fmt.Errorf("send %s command: %w", method, err))
		return fmt.Errorf("send %s command: %w", method, err)
	}
	return nil
}

// callResult is the result-bearing form of call. It uses the same websocket as
// every other command, so navigation state stays on the single existing CDP
// consumer.
func (c *cdpClient) callResult(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	payload := struct {
		ID        int64           `json:"id"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params,omitempty"`
		SessionID string          `json:"sessionId,omitempty"`
	}{Method: method, SessionID: sessionID}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("encode %s params: %w", method, err)
		}
		payload.Params = raw
	}
	id := c.newID()
	payload.ID = id
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s command: %w", method, err)
	}

	reply := make(chan cdpResponse, 1)
	c.pendingMu.Lock()
	c.pendingResults[id] = reply
	c.pendingMu.Unlock()

	if err := c.write(data); err != nil {
		c.removePendingResult(id)
		c.fail(fmt.Errorf("send %s command: %w", method, err))
		return nil, fmt.Errorf("send %s command: %w", method, err)
	}

	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()
	select {
	case response := <-reply:
		return response.result, response.err
	case <-timer.C:
		c.removePendingResult(id)
		return nil, fmt.Errorf("%s command timed out", method)
	case <-ctx.Done():
		c.removePendingResult(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.fatalError()
	}
}

func (c *cdpClient) newID() int64 {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.nextID++
	return c.nextID
}

func (c *cdpClient) write(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *cdpClient) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *cdpClient) removePendingResult(id int64) {
	c.pendingMu.Lock()
	delete(c.pendingResults, id)
	c.pendingMu.Unlock()
}

func (c *cdpClient) readLoop(ctx context.Context) {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.fail(fmt.Errorf("cdp connection lost: %w", err))
			return
		}
		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.fail(fmt.Errorf("cdp protocol error: %w", err))
			return
		}
		if msg.ID != 0 {
			c.deliver(msg)
			continue
		}
		if msg.Method == "" {
			continue
		}
		if !c.events.push(msg) {
			return
		}
	}
}

func (c *cdpClient) deliver(msg cdpMessage) {
	c.pendingMu.Lock()
	reply := c.pending[msg.ID]
	delete(c.pending, msg.ID)
	resultReply := c.pendingResults[msg.ID]
	delete(c.pendingResults, msg.ID)
	c.pendingMu.Unlock()

	if reply != nil {
		if msg.Error != nil {
			reply <- error(msg.Error)
		} else {
			reply <- nil
		}
	}
	if resultReply != nil {
		response := cdpResponse{result: msg.Result}
		if msg.Error != nil {
			response.err = error(msg.Error)
		}
		resultReply <- response
	}
}

// eventLoop hands queued events to the handler on a single goroutine so events
// keep their arrival order.
func (c *cdpClient) eventLoop(ctx context.Context) {
	for {
		msg, ok := c.events.pop()
		if !ok {
			return
		}
		if err := c.handler(ctx, msg.SessionID, msg.Method, msg.Params); err != nil {
			c.fail(err)
			return
		}
	}
}

func (c *cdpClient) fail(err error) {
	c.errOnce.Do(func() {
		c.fatalErr = err
		close(c.done)
		c.events.close()
	})
}

// fatalError reports why the CDP channel stopped. Only call it after done is
// closed or from call, which returns right after observing done.
func (c *cdpClient) fatalError() error {
	if c.fatalErr == nil {
		return fmt.Errorf("cdp channel closed")
	}
	return c.fatalErr
}

// eventQueue is the unbounded FIFO queue between the read loop and the event
// handler. The read loop is the only goroutine that can deliver a command
// response, so it must never block on a queued event: a bounded queue that was
// full while the handler waited for a command response deadlocked the client
// until that command timed out, and the guard then stopped the runtime (fail
// closed). Events are queued instead of dropped because a dropped
// Fetch.requestPaused event would leave that request paused forever, and
// arrival order is preserved for the single handler goroutine.
type eventQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []cdpMessage
	closed bool
}

func newEventQueue() *eventQueue {
	q := &eventQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push appends one event and reports whether the queue still accepts events. It
// never blocks.
func (q *eventQueue) push(msg cdpMessage) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.items = append(q.items, msg)
	q.cond.Signal()
	return true
}

// pop returns the oldest queued event, waiting until one arrives. It reports
// false once the queue is closed and drained.
func (q *eventQueue) pop() (cdpMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return cdpMessage{}, false
	}
	msg := q.items[0]
	q.items[0] = cdpMessage{}
	q.items = q.items[1:]
	return msg, true
}

// close stops the queue. Events that are already queued stay readable.
func (q *eventQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
}
