package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"customer-service/backend/internal/domain"
)

func TestChatCompletionsReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		writeTestJSON(t, w, map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "您好，测试回复。"}},
			},
		})
	}))
	defer server.Close()

	reply, err := NewClient().Reply(context.Background(), domain.AISettings{
		APIKey:          "sk-test",
		BaseURL:         server.URL,
		Model:           "gpt-4o-mini",
		APIType:         "chat_completions",
		Temperature:     0.7,
		MaxOutputTokens: 128,
		TimeoutSeconds:  5,
	}, domain.ContactSettings{Phone: "400", Wechat: "wx", QQ: "qq"}, "你好")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply != "您好，测试回复。" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestResponsesReplyParsesOutputText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeTestJSON(t, w, map[string]any{
			"output_text": "Responses 文本回复",
		})
	}))
	defer server.Close()

	reply, err := NewClient().Reply(context.Background(), domain.AISettings{
		APIKey:         "sk-test",
		BaseURL:        server.URL,
		Model:          "gpt-4o-mini",
		APIType:        "responses",
		TimeoutSeconds: 5,
	}, domain.ContactSettings{}, "你好")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply != "Responses 文本回复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestResponsesReplyParsesNestedOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"output": []map[string]any{
				{
					"content": []map[string]any{
						{"text": "嵌套文本回复"},
					},
				},
			},
		})
	}))
	defer server.Close()

	reply, err := NewClient().Reply(context.Background(), domain.AISettings{
		APIKey:         "sk-test",
		BaseURL:        server.URL,
		Model:          "gpt-4o-mini",
		APIType:        "responses",
		TimeoutSeconds: 5,
	}, domain.ContactSettings{}, "你好")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply != "嵌套文本回复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestFallbackReplyWithoutAPIKey(t *testing.T) {
	reply, err := NewClient().Reply(context.Background(), domain.AISettings{}, domain.ContactSettings{Phone: "400-123"}, "电话是多少")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply != "客服电话：400-123，您可以点击下方电话联系按钮直接拨打。" {
		t.Fatalf("unexpected fallback reply: %q", reply)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
