package browser

// A minimal client for the Chrome DevTools Protocol (CDP), the protocol
// programs like Playwright use to control Chromium.
//
// Chromium is started with --remote-debugging-pipe: it then reads
// commands from file descriptor 3 and writes answers and events to file
// descriptor 4. Each message is one JSON object followed by a zero byte.
// Using a pipe (not a network port) means no other program on the device
// can talk to the browser.
//
// A command looks like {"id": 1, "method": "Page.navigate", "params":
// {...}, "sessionId": "..."}; its answer carries the same id. Events (like
// a new screen picture) have a "method" but no id. The sessionId picks the
// browser tab a command is for (see Session).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// message is any message from the browser: an answer (ID set) or an event
// (Method set).
type message struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("browser error %d: %s", e.Code, e.Message) }

// event is a notification from the browser, passed to the event handler.
type event struct {
	Method    string
	Params    json.RawMessage
	SessionID string
}

// conn is a CDP connection over the browser's pipe.
type conn struct {
	writeMu sync.Mutex
	w       io.Writer

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan message
	closed  error

	// onEvent is called (on the reading goroutine) for every event.
	onEvent func(event)
}

// newConn starts reading from r. Events go to onEvent. The connection
// ends when r returns an error (the browser exited).
func newConn(w io.Writer, r io.Reader, onEvent func(event)) *conn {
	c := &conn{w: w, pending: map[int64]chan message{}, onEvent: onEvent}
	go c.readLoop(r)
	return c
}

// maxMessage limits one message from the browser. Screen pictures are the
// largest (a few hundred KB).
const maxMessage = 16 << 20

func (c *conn) readLoop(r io.Reader) {
	reader := bufio.NewReaderSize(r, 64<<10)
	var err error
	for {
		var data []byte
		data, err = readMessage(reader)
		if err != nil {
			break
		}
		var m message
		if json.Unmarshal(data, &m) != nil {
			continue // not something we understand; ignore it
		}
		if m.ID != 0 {
			c.mu.Lock()
			reply := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if reply != nil {
				reply <- m
			}
			continue
		}
		if m.Method != "" && c.onEvent != nil {
			c.onEvent(event{Method: m.Method, Params: m.Params, SessionID: m.SessionID})
		}
	}
	// The browser is gone: fail every command still waiting.
	c.mu.Lock()
	c.closed = fmt.Errorf("the browser stopped: %w", err)
	for id, reply := range c.pending {
		close(reply)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// readMessage reads up to the next zero byte.
func readMessage(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, err := r.ReadSlice(0)
		out = append(out, chunk...)
		if len(out) > maxMessage {
			return nil, errors.New("message from the browser is too big")
		}
		switch {
		case err == nil:
			return out[:len(out)-1], nil // without the zero byte
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, err
		}
	}
}

// call sends a command and waits for its answer. sessionID is "" for
// commands to the browser itself, or a tab's session. result (if not nil)
// receives the answer's "result" object.
func (c *conn) call(ctx context.Context, sessionID, method string, params, result any) error {
	c.mu.Lock()
	if c.closed != nil {
		c.mu.Unlock()
		return c.closed
	}
	c.nextID++
	id := c.nextID
	reply := make(chan message, 1)
	c.pending[id] = reply
	c.mu.Unlock()

	out := map[string]any{"id": id, "method": method}
	if params != nil {
		out["params"] = params
	}
	if sessionID != "" {
		out["sessionId"] = sessionID
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_, err = c.w.Write(append(data, 0))
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("sending %s to the browser: %w", method, err)
	}

	select {
	case m, ok := <-reply:
		if !ok {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.closed
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %w", method, m.Error)
		}
		if result != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}
