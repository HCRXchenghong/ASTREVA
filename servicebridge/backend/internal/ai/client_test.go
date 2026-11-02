package ai

import (
	"context"
	"encoding/json"
	"errors"
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
	}, domain.ContactSettings{Telephone: "400", WhatsApp: "wa", Telegram: "tg"}, nil, "你好", LangZh, "")
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
	}, domain.ContactSettings{}, nil, "你好", LangZh, "")
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
	}, domain.ContactSettings{}, nil, "你好", LangZh, "")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply != "嵌套文本回复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestAnthropicMessagesReplyAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "anthropic-secret" {
			t.Fatalf("unexpected x-api-key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("unexpected anthropic-version: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("anthropic request must not send bearer header: %q", got)
		}
		writeTestJSON(t, w, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Claude 真实回复"}},
		})
	}))
	defer server.Close()

	reply, err := NewClient().Reply(context.Background(), domain.AISettings{
		APIKey:         "anthropic-secret",
		BaseURL:        server.URL,
		Model:          "claude-3-5-haiku-latest",
		APIType:        "anthropic_messages",
		TimeoutSeconds: 5,
	}, domain.ContactSettings{}, nil, "你好", LangZh, "")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply != "Claude 真实回复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestModelsUsesProtocolHeadersAndDeduplicates(t *testing.T) {
	tests := []struct {
		name          string
		apiType       string
		wantBearer    string
		wantAnthropic string
	}{
		{name: "openai", apiType: "chat_completions", wantBearer: "Bearer model-secret"},
		{name: "responses", apiType: "responses", wantBearer: "Bearer model-secret"},
		{name: "anthropic", apiType: "anthropic_messages", wantAnthropic: "model-secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != test.wantBearer {
					t.Fatalf("unexpected authorization: %q", got)
				}
				if got := r.Header.Get("x-api-key"); got != test.wantAnthropic {
					t.Fatalf("unexpected x-api-key: %q", got)
				}
				if test.apiType == "anthropic_messages" && r.Header.Get("anthropic-version") == "" {
					t.Fatal("anthropic-version header is required")
				}
				writeTestJSON(t, w, map[string]any{
					"data": []map[string]any{{"id": "model-a"}, {"id": "model-b"}, {"id": "model-a"}, {"id": ""}},
				})
			}))
			defer server.Close()
			models, err := NewClient().Models(context.Background(), domain.AISettings{
				APIKey: "model-secret", BaseURL: server.URL, APIType: test.apiType, TimeoutSeconds: 5,
			})
			if err != nil {
				t.Fatalf("models: %v", err)
			}
			if len(models) != 2 || models[0] != "model-a" || models[1] != "model-b" {
				t.Fatalf("unexpected models: %#v", models)
			}
		})
	}
}

func TestReplyWithoutAPIKeyReturnsError(t *testing.T) {
	reply, err := NewClient().Reply(context.Background(), domain.AISettings{}, domain.ContactSettings{Telephone: "400-123"}, nil, "电话是多少", LangZh, "")
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("未配置 API Key 必须返回 ErrNoAPIKey，got reply=%q err=%v", reply, err)
	}
	if reply != "" {
		t.Fatalf("未配置 API Key 不能返回伪造回复：%q", reply)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
