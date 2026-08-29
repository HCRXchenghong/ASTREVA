package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestChatCompletionsIncludesHistory 验证历史轮次按顺序进入请求，且当前消息在最后。
func TestChatCompletionsIncludesHistory(t *testing.T) {
	var gotMessages []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Messages []map[string]string `json:"messages"`
		}
		json.Unmarshal(body, &payload)
		gotMessages = payload.Messages
		writeTestJSON(t, w, map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		})
	}))
	defer server.Close()

	history := []HistoryTurn{
		{Role: "user", Content: "你们有 304 槽钢吗"},
		{Role: "assistant", Content: "有的，304 槽钢现货供应。"},
	}
	_, err := NewClient().Reply(context.Background(), domain.AISettings{
		APIKey:         "sk-test",
		BaseURL:        server.URL,
		Model:          "gpt-4o-mini",
		APIType:        "chat_completions",
		TimeoutSeconds: 5,
	}, domain.ContactSettings{}, history, "那 201 呢？", LangZh)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if len(gotMessages) != 4 {
		t.Fatalf("expected 4 messages (system+2 history+current), got %d", len(gotMessages))
	}
	if gotMessages[0]["role"] != "system" {
		t.Fatalf("first message must be system, got %q", gotMessages[0]["role"])
	}
	if gotMessages[1]["role"] != "user" || gotMessages[1]["content"] != "你们有 304 槽钢吗" {
		t.Fatalf("history[0] mismatch: %v", gotMessages[1])
	}
	if gotMessages[2]["role"] != "assistant" || gotMessages[2]["content"] != "有的，304 槽钢现货供应。" {
		t.Fatalf("history[1] mismatch: %v", gotMessages[2])
	}
	if gotMessages[3]["role"] != "user" || gotMessages[3]["content"] != "那 201 呢？" {
		t.Fatalf("current message mismatch: %v", gotMessages[3])
	}
}

// TestResponsesIncludesHistory 验证 responses API 的 input 为消息数组并含历史。
func TestResponsesIncludesHistory(t *testing.T) {
	var gotInput []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Input []map[string]string `json:"input"`
		}
		json.Unmarshal(body, &payload)
		gotInput = payload.Input
		writeTestJSON(t, w, map[string]any{"output_text": "ok"})
	}))
	defer server.Close()

	history := []HistoryTurn{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	_, err := NewClient().Reply(context.Background(), domain.AISettings{
		APIKey:         "sk-test",
		BaseURL:        server.URL,
		Model:          "gpt-5",
		APIType:        "responses",
		TimeoutSeconds: 5,
	}, domain.ContactSettings{}, history, "what did I just say?", LangEn)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if len(gotInput) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(gotInput))
	}
	if gotInput[2]["content"] != "what did I just say?" {
		t.Fatalf("current input mismatch: %v", gotInput[2])
	}
}

// TestNormalizeHistoryMergesAdjacent 验证相邻同角色轮次合并、空轮次丢弃。
func TestNormalizeHistoryMergesAdjacent(t *testing.T) {
	out := normalizeHistory([]HistoryTurn{
		{Role: "user", Content: "a"},
		{Role: "user", Content: "b"},
		{Role: "assistant", Content: ""},
		{Role: "assistant", Content: "c"},
		{Role: "system", Content: "bad"},
	})
	if len(out) != 2 {
		t.Fatalf("expected 2 turns, got %d: %+v", len(out), out)
	}
	if out[0].Content != "a\nb" {
		t.Fatalf("merge failed: %q", out[0].Content)
	}
	if out[1].Content != "c" {
		t.Fatalf("unexpected: %q", out[1].Content)
	}
}
