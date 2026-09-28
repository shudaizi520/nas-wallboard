package truenas

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestClientProbeRejectsInvalidCandidateWithoutConnecting(t *testing.T) {
	client := NewClient(config.TrueNASConfig{URL: "https://nas.local", Username: "reader"}, "secret", nil)
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageFeature {
		t.Fatalf("Probe = %#v", result)
	}
}

const clientTestAPIKey = "1-client-test-secret"
const clientTestUsername = "nas_wallboard"

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func testClient(t *testing.T, handler http.HandlerFunc, logger *slog.Logger) (*Client, context.CancelFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	cfg := config.TrueNASConfig{
		URL:         "ws" + strings.TrimPrefix(server.URL, "http"),
		Username:    clientTestUsername,
		CallTimeout: config.Duration{Duration: 250 * time.Millisecond},
	}
	client := NewClient(cfg, clientTestAPIKey, logger)
	client.reconnectMin = 10 * time.Millisecond
	client.reconnectMax = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Run() did not stop")
		}
	})
	return client, cancel
}

func acceptTestConn(t *testing.T, w http.ResponseWriter, r *http.Request) *websocket.Conn {
	t.Helper()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		t.Errorf("Accept() error = %v", err)
		return nil
	}
	return conn
}

func readTestRequest(t *testing.T, conn *websocket.Conn) testRequest {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var request testRequest
	if err := wsjson.Read(ctx, conn, &request); err != nil {
		t.Errorf("read request: %v", err)
	}
	return request
}

func authenticateTestConn(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	request := readTestRequest(t, conn)
	if request.Method != "auth.login_ex" {
		t.Errorf("first method = %q, want auth.login_ex", request.Method)
	}
	var params []map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Errorf("decode auth params: %v", err)
	}
	if len(params) != 1 || params[0]["mechanism"] != "API_KEY_PLAIN" || params[0]["username"] != clientTestUsername || params[0]["api_key"] != clientTestAPIKey {
		t.Errorf("auth params = %#v", params)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      request.ID,
		"result":  map[string]any{"response_type": "SUCCESS"},
	}); err != nil {
		t.Errorf("write auth response: %v", err)
	}
}

func writeTestResult(t *testing.T, conn *websocket.Conn, id uint64, result any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func TestClientAuthenticatesWithLoginExBeforeCalls(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn := acceptTestConn(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		authenticateTestConn(t, conn)
		request := readTestRequest(t, conn)
		if request.Method != "system.info" {
			t.Errorf("method = %q, want system.info", request.Method)
		}
		writeTestResult(t, conn, request.ID, "ready")
	}
	client, _ := testClient(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result string
	if err := client.Call(ctx, "system.info", nil, &result); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result != "ready" {
		t.Fatalf("result = %q, want ready", result)
	}
}

func TestClientCorrelatesConcurrentResponses(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn := acceptTestConn(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		authenticateTestConn(t, conn)
		first := readTestRequest(t, conn)
		second := readTestRequest(t, conn)
		writeTestResult(t, conn, second.ID, second.Method+"-result")
		writeTestResult(t, conn, first.ID, first.Method+"-result")
	}
	client, _ := testClient(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, method := range []string{"pool.query", "app.query"} {
		method := method
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result string
			if err := client.Call(ctx, method, nil, &result); err != nil {
				t.Errorf("Call(%s) error = %v", method, err)
				return
			}
			mu.Lock()
			results[method] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	if results["pool.query"] != "pool.query-result" || results["app.query"] != "app.query-result" {
		t.Fatalf("results = %#v", results)
	}
}

func TestClientTimesOutAndRemovesPendingCall(t *testing.T) {
	received := make(chan struct{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn := acceptTestConn(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		authenticateTestConn(t, conn)
		_ = readTestRequest(t, conn)
		close(received)
		<-r.Context().Done()
	}
	client, _ := testClient(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := client.Call(ctx, "reporting.netdata_get_data", nil, nil); err == nil {
		t.Fatal("Call() error = nil, want timeout")
	}
	<-received
	client.mu.Lock()
	pending := len(client.pending)
	client.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending calls = %d, want 0", pending)
	}
}

func TestClientReconnectsAfterServerClose(t *testing.T) {
	var connections atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn := acceptTestConn(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		n := connections.Add(1)
		authenticateTestConn(t, conn)
		if n == 1 {
			_ = conn.Close(websocket.StatusGoingAway, "test reconnect")
			return
		}
		request := readTestRequest(t, conn)
		writeTestResult(t, conn, request.ID, "reconnected")
	}
	client, _ := testClient(t, handler, nil)

	deadline := time.Now().Add(time.Second)
	for connections.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result string
	if err := client.Call(ctx, "system.info", nil, &result); err != nil {
		t.Fatalf("Call() after reconnect error = %v", err)
	}
	if result != "reconnected" {
		t.Fatalf("result = %q, want reconnected", result)
	}
}

func TestClientRejectsMalformedAndOversizedFrames(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"malformed", []byte("{not-json")},
		{"oversized", bytes.Repeat([]byte("x"), maxFrameBytes+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				conn := acceptTestConn(t, w, r)
				if conn == nil {
					return
				}
				defer conn.CloseNow()
				authenticateTestConn(t, conn)
				_ = readTestRequest(t, conn)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := conn.Write(ctx, websocket.MessageText, tt.payload); err != nil {
					t.Errorf("write invalid frame: %v", err)
				}
			}
			client, _ := testClient(t, handler, nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := client.Call(ctx, "system.info", nil, nil); err == nil {
				t.Fatal("Call() error = nil, want frame rejection")
			}
		})
	}
}

func TestClientRedactsAPIKeyFromErrors(t *testing.T) {
	var logs lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn := acceptTestConn(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		request := readTestRequest(t, conn)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = wsjson.Write(ctx, conn, map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error": map[string]any{
				"code":    -32001,
				"message": "rejected key " + clientTestAPIKey,
			},
		})
	}
	client, cancel := testClient(t, handler, logger)
	time.Sleep(50 * time.Millisecond)
	cancel()
	if strings.Contains(logs.String(), clientTestAPIKey) {
		t.Fatalf("logs leaked API key: %s", logs.String())
	}
	if got := client.redact("failure: " + clientTestAPIKey); strings.Contains(got, clientTestAPIKey) {
		t.Fatalf("redact() leaked API key: %q", got)
	}
}
