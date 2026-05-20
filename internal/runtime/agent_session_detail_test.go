package runtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"codexflow/internal/codex"
	"codexflow/internal/store"
)

type blockingCodexClient struct {
	started chan struct{}
	release chan struct{}

	mu    sync.Mutex
	calls int
}

func newBlockingCodexClient() *blockingCodexClient {
	return &blockingCodexClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *blockingCodexClient) Start(context.Context) error { return nil }

func (c *blockingCodexClient) Notifications() <-chan codex.Notification {
	return make(chan codex.Notification)
}

func (c *blockingCodexClient) ServerRequests() <-chan codex.ServerRequest {
	return make(chan codex.ServerRequest)
}

func (c *blockingCodexClient) StderrLines() <-chan string {
	return make(chan string)
}

func (c *blockingCodexClient) Reply(context.Context, json.RawMessage, any) error {
	return nil
}

func (c *blockingCodexClient) Call(ctx context.Context, method string, _ any, result any) error {
	if method != "thread/read" {
		return nil
	}

	c.mu.Lock()
	c.calls++
	if c.calls == 1 {
		close(c.started)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
	}

	if response, ok := result.(*codex.ThreadReadResponse); ok {
		response.Thread = codex.Thread{
			ID:            "thread-1",
			ModelProvider: "OpenAI",
			CreatedAt:     100,
			UpdatedAt:     200,
			Status:        codex.ThreadStatus{Type: "active"},
			CWD:           "/tmp/codexflow",
		}
	}
	return nil
}

func (c *blockingCodexClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestSessionDetailCoalescesConcurrentThreadReads(t *testing.T) {
	sessionStore, err := store.New(nil)
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	sessionStore.ReplaceSessions([]codex.Thread{{
		ID:            "thread-1",
		ModelProvider: "OpenAI",
		CreatedAt:     100,
		UpdatedAt:     200,
		Status:        codex.ThreadStatus{Type: "active"},
		CWD:           "/tmp/codexflow",
	}}, map[string]bool{"thread-1": true})

	client := newBlockingCodexClient()
	agent := &Agent{
		logger: slog.Default(),
		client: client,
		store:  sessionStore,
		broker: NewBroker(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if _, err := agent.SessionDetail(ctx, "thread-1"); err != nil {
				t.Errorf("SessionDetail returned error: %v", err)
			}
		}()
	}

	select {
	case <-client.started:
	case <-ctx.Done():
		t.Fatalf("first thread/read did not start: %v", ctx.Err())
	}

	time.Sleep(50 * time.Millisecond)
	if got, want := client.callCount(), 1; got != want {
		t.Fatalf("thread/read calls while first request is in flight = %d, want %d", got, want)
	}

	close(client.release)
	wg.Wait()

	if got, want := client.callCount(), 1; got != want {
		t.Fatalf("thread/read calls after concurrent requests = %d, want %d", got, want)
	}
}
