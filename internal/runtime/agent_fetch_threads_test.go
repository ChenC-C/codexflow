package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"codexflow/internal/codex"
)

type threadListCaptureClient struct {
	params []map[string]any
}

func (c *threadListCaptureClient) Start(context.Context) error { return nil }

func (c *threadListCaptureClient) Reply(context.Context, json.RawMessage, any) error {
	return nil
}

func (c *threadListCaptureClient) Notifications() <-chan codex.Notification {
	return make(chan codex.Notification)
}

func (c *threadListCaptureClient) ServerRequests() <-chan codex.ServerRequest {
	return make(chan codex.ServerRequest)
}

func (c *threadListCaptureClient) StderrLines() <-chan string {
	return make(chan string)
}

func (c *threadListCaptureClient) Call(_ context.Context, method string, params any, result any) error {
	if method != "thread/list" {
		return nil
	}
	c.params = append(c.params, params.(map[string]any))
	*(result.(*codex.ThreadListResponse)) = codex.ThreadListResponse{
		Data: []codex.Thread{{
			ID:        "legacy-thread",
			UpdatedAt: 100,
			Status:    codex.ThreadStatus{Type: "idle"},
		}},
	}
	return nil
}

func TestFetchThreadsRequestsUserVisibleAndLegacySourceKinds(t *testing.T) {
	client := &threadListCaptureClient{}
	agent := &Agent{client: client}

	threads, err := agent.fetchThreads(context.Background())
	if err != nil {
		t.Fatalf("fetch threads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(threads))
	}
	if len(client.params) != 1 {
		t.Fatalf("thread/list calls = %d, want 1", len(client.params))
	}

	want := []string{
		"cli",
		"vscode",
		"exec",
		"appServer",
		"unknown",
	}
	got, ok := client.params[0]["sourceKinds"].([]string)
	if !ok {
		t.Fatalf("sourceKinds param = %#v, want []string", client.params[0]["sourceKinds"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sourceKinds = %#v, want %#v", got, want)
	}

	modelProviders, ok := client.params[0]["modelProviders"].([]string)
	if !ok {
		t.Fatalf("modelProviders param = %#v, want empty []string", client.params[0]["modelProviders"])
	}
	if len(modelProviders) != 0 {
		t.Fatalf("modelProviders = %#v, want empty []string to include every provider", modelProviders)
	}
	if got, want := client.params[0]["useStateDbOnly"], false; got != want {
		t.Fatalf("useStateDbOnly = %#v, want %#v", got, want)
	}
	if got, want := client.params[0]["limit"], 500; got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
}
