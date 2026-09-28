package truenas

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"github.com/coder/websocket"
)

const (
	maxFrameBytes = 1 << 20
	pingInterval  = 30 * time.Second
	pingTimeout   = 10 * time.Second
	idleTimeout   = 90 * time.Second
)

type Caller interface {
	Call(context.Context, string, []any, any) error
}

type Client struct {
	cfg    config.TrueNASConfig
	apiKey string
	logger *slog.Logger

	mu      sync.Mutex
	conn    *websocket.Conn
	ready   bool
	pending map[uint64]chan callResult
	nextID  uint64
	changed chan struct{}

	writeMu sync.Mutex
	running atomic.Bool

	reconnectMin time.Duration
	reconnectMax time.Duration
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type callResult struct {
	result json.RawMessage
	rpcErr *rpcError
	err    error
}

func NewClient(cfg config.TrueNASConfig, apiKey string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CallTimeout.Duration <= 0 {
		cfg.CallTimeout.Duration = 10 * time.Second
	}
	return &Client{
		cfg:          cfg,
		apiKey:       apiKey,
		logger:       logger,
		pending:      make(map[uint64]chan callResult),
		changed:      make(chan struct{}),
		reconnectMin: time.Second,
		reconnectMax: 30 * time.Second,
	}
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	return Probe(ctx, c.cfg, c.apiKey)
}

func (c *Client) Run(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return errors.New("TrueNAS client is already running")
	}
	defer c.running.Store(false)

	backoff := c.reconnectMin
	for {
		if ctx.Err() != nil {
			return nil
		}

		authenticated, err := c.runSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			c.logger.Warn("TrueNAS connection unavailable", "error", c.redact(err.Error()))
		}

		if authenticated {
			backoff = c.reconnectMin
		} else {
			backoff = min(backoff*2, c.reconnectMax)
		}
		delay := jitter(backoff)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (c *Client) runSession(ctx context.Context) (bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout.Duration)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.cfg.InsecureSkipVerify, // User-controlled for local self-signed TrueNAS certificates.
	}
	httpClient := &http.Client{Transport: transport}
	conn, _, err := websocket.Dial(dialCtx, c.cfg.URL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return false, fmt.Errorf("dial WebSocket: %w", err)
	}
	conn.SetReadLimit(maxFrameBytes)
	c.setConnection(conn)
	defer c.disconnect(conn, errors.New("TrueNAS connection closed"))

	readerErr := make(chan error, 1)
	go func() { readerErr <- c.readLoop(ctx, conn) }()

	authCtx, authCancel := context.WithTimeout(ctx, c.cfg.CallTimeout.Duration)
	var authResult json.RawMessage
	err = c.callOnConnection(authCtx, conn, "auth.login_ex", []any{map[string]any{
		"mechanism": "API_KEY_PLAIN",
		"username":  c.cfg.Username,
		"api_key":   c.apiKey,
	}}, &authResult)
	authCancel()
	if err != nil {
		return false, fmt.Errorf("authenticate: %w", err)
	}
	if !authenticationSucceeded(authResult) {
		return false, errors.New("authenticate: server did not report success")
	}
	c.markReady(conn)

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case err := <-readerErr:
			return true, err
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, pingTimeout)
			c.writeMu.Lock()
			err := conn.Ping(pingCtx)
			c.writeMu.Unlock()
			pingCancel()
			if err != nil {
				return true, fmt.Errorf("ping: %w", err)
			}
		}
	}
}

func (c *Client) Call(ctx context.Context, method string, params []any, result any) error {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout.Duration)
	defer cancel()

	for {
		c.mu.Lock()
		if c.ready && c.conn != nil {
			conn := c.conn
			c.mu.Unlock()
			return c.callOnConnection(callCtx, conn, method, params, result)
		}
		changed := c.changed
		c.mu.Unlock()

		select {
		case <-callCtx.Done():
			return callCtx.Err()
		case <-changed:
		}
	}
}

func (c *Client) callOnConnection(ctx context.Context, conn *websocket.Conn, method string, params []any, out any) error {
	if params == nil {
		params = []any{}
	}
	responseCh := make(chan callResult, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.pending[id] = responseCh
	c.mu.Unlock()

	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		c.removePending(id)
		return fmt.Errorf("encode %s request: %w", method, err)
	}
	c.writeMu.Lock()
	err = conn.Write(ctx, websocket.MessageText, payload)
	c.writeMu.Unlock()
	if err != nil {
		c.removePending(id)
		return fmt.Errorf("write %s request: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case response := <-responseCh:
		if response.err != nil {
			return response.err
		}
		if response.rpcErr != nil {
			message := c.redact(response.rpcErr.Message)
			return fmt.Errorf("TrueNAS %s failed (%d): %s", method, response.rpcErr.Code, message)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(response.result, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		readCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		messageType, payload, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("read WebSocket frame: %w", err)
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			return errors.New("read WebSocket frame: unsupported message type")
		}

		var response rpcResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			return errors.New("decode JSON-RPC response: malformed frame")
		}
		if response.ID == 0 {
			continue
		}

		c.mu.Lock()
		responseCh := c.pending[response.ID]
		delete(c.pending, response.ID)
		c.mu.Unlock()
		if responseCh != nil {
			responseCh <- callResult{result: response.Result, rpcErr: response.Error}
		}
	}
}

func (c *Client) setConnection(conn *websocket.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.ready = false
	c.notifyLocked()
	c.mu.Unlock()
}

func (c *Client) markReady(conn *websocket.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.ready = true
		c.notifyLocked()
	}
	c.mu.Unlock()
}

func (c *Client) disconnect(conn *websocket.Conn, cause error) {
	_ = conn.CloseNow()

	c.mu.Lock()
	if c.conn != conn {
		c.mu.Unlock()
		return
	}
	c.conn = nil
	c.ready = false
	pending := c.pending
	c.pending = make(map[uint64]chan callResult)
	c.notifyLocked()
	c.mu.Unlock()

	for _, responseCh := range pending {
		responseCh <- callResult{err: cause}
	}
}

func (c *Client) removePending(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) notifyLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *Client) redact(message string) string {
	if c.apiKey == "" {
		return message
	}
	return strings.ReplaceAll(message, c.apiKey, "[REDACTED]")
}

func authenticationSucceeded(raw json.RawMessage) bool {
	var direct bool
	if json.Unmarshal(raw, &direct) == nil && direct {
		return true
	}
	var response struct {
		ResponseType string `json:"response_type"`
	}
	return json.Unmarshal(raw, &response) == nil && strings.EqualFold(response.ResponseType, "SUCCESS")
}

func jitter(duration time.Duration) time.Duration {
	if duration <= 0 {
		return 0
	}
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(duration) * factor)
}
