package runtime

import (
	"strings"
	"testing"

	"codexflow/internal/codex"
	"codexflow/internal/store"
)

func TestSessionDetailLimitsTurnsItemsAndLargeText(t *testing.T) {
	record := store.SessionRecord{
		Thread: codex.Thread{
			ID:  "thread-1",
			CWD: "/tmp/codexflow",
		},
		Runtime: store.SessionRuntime{
			LatestDiffByTurn: map[string]string{},
			LatestPlanByTurn: map[string]codex.TurnPlanUpdatedNotification{},
		},
	}

	for turnIdx := 0; turnIdx < 5; turnIdx++ {
		turn := codex.Turn{ID: string(rune('a' + turnIdx)), Status: "completed"}
		for itemIdx := 0; itemIdx < 5; itemIdx++ {
			itemType := "agentMessage"
			item := map[string]any{
				"type": itemType,
				"id":   itemType + "-id",
				"text": strings.Repeat("A", 80),
			}
			if itemIdx == 0 {
				item = map[string]any{
					"type": "userMessage",
					"id":   "user-id",
					"content": []any{
						map[string]any{"type": "text", "text": "first user prompt"},
					},
				}
			}
			turn.Items = append(turn.Items, item)
		}
		record.Thread.Turns = append(record.Thread.Turns, turn)
	}

	detail := toSessionDetailWithOptions(record, 0, nil, sessionDetailOptions{
		TurnLimit:     2,
		ItemLimit:     3,
		TextHeadLimit: 8,
		TextTailLimit: 4,
	})

	if got, want := len(detail.Turns), 2; got != want {
		t.Fatalf("turns = %d, want %d", got, want)
	}
	if got, want := detail.TotalTurns, 5; got != want {
		t.Fatalf("total turns = %d, want %d", got, want)
	}
	if got, want := detail.OmittedTurns, 3; got != want {
		t.Fatalf("omitted turns = %d, want %d", got, want)
	}
	if got, want := detail.TotalItems, 25; got != want {
		t.Fatalf("total items = %d, want %d", got, want)
	}
	if got, want := detail.OmittedItems, 19; got != want {
		t.Fatalf("omitted items = %d, want %d", got, want)
	}
	if !detail.Limited {
		t.Fatalf("detail should be marked limited")
	}

	for _, turn := range detail.Turns {
		if got, want := len(turn.Items), 3; got != want {
			t.Fatalf("turn %s items = %d, want %d", turn.ID, got, want)
		}
		if got, want := turn.TotalItems, 5; got != want {
			t.Fatalf("turn %s total items = %d, want %d", turn.ID, got, want)
		}
		if got, want := turn.OmittedItems, 2; got != want {
			t.Fatalf("turn %s omitted items = %d, want %d", turn.ID, got, want)
		}
		if got := turn.Items[0].Type; got != "userMessage" {
			t.Fatalf("first kept item type = %q, want userMessage", got)
		}
	}

	body := detail.Turns[0].Items[1].Body
	if len(body) >= 80 || !strings.Contains(body, "omitted") {
		t.Fatalf("body was not summarized: %q", body)
	}
}
