package codex

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestReadLoopDispatchesLargeJSONRPCLine(t *testing.T) {
	t.Parallel()

	client := NewClient("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.pending["7"] = make(chan responseEnvelope, 1)

	payload := strings.Repeat("x", 20*1024*1024)
	line := `{"jsonrpc":"2.0","id":7,"result":{"payload":"` + payload + `"}}` + "\n"
	done := make(chan struct{})

	go func() {
		client.readLoop(bytes.NewBufferString(line))
		close(done)
	}()

	select {
	case reply := <-client.pending["7"]:
		if reply.Err != nil {
			t.Fatalf("unexpected RPC error: %v", reply.Err)
		}
		if len(reply.Result) < len(payload) {
			t.Fatalf("large result was not dispatched intact: got %d bytes", len(reply.Result))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("large JSON-RPC line was not dispatched")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("read loop did not finish after EOF")
	}
}

func TestCallReturnsContextErrorWhenNoReaderResponse(t *testing.T) {
	t.Parallel()

	client := NewClient("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.stdin = nopWriteCloser{Writer: io.Discard}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	if err := client.Call(ctx, "thread/resume", map[string]any{"threadId": "missing"}, nil); err == nil {
		t.Fatal("expected context timeout")
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error {
	return nil
}
