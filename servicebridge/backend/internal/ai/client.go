package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 90 * time.Second},
	}
}

// HistoryTurn 是会话历史中的一轮对话（按时间顺序），仅用于当前会话的上下文。
type HistoryTurn struct {
	Role    string // "user" 或 "assistant"
	Content string
}

// normalizeHistory 去掉空轮次、合并相邻同角色轮次，保证满足各家 API 的消息序列要求。
func normalizeHistory(history []HistoryTurn) []HistoryTurn {
	out := make([]HistoryTurn, 0, len(history))
	for _, turn := range history {
		content := strings.TrimSpace(turn.Content)
		if content == "" {
			continue
		}
		if turn.Role != "user" && turn.Role != "assistant" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == turn.Role {
			out[n-1].Content += "\n" + content
			continue
		}
		out = append(out, HistoryTurn{Role: turn.Role, Content: content})
	}
	return out
}

// Reply 根据访客语言生成 AI 客服回复。replyLang 为访客端看到的语言（zh/en/ar）。
// history 为当前会话的历史对话轮次（按会话隔离传入，不得跨会话复用）。
func (c *Client) Reply(ctx context.Context, settings domain.AISettings, contacts domain.ContactSettings, history []HistoryTurn, userText, replyLang string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}
	replyLang = NormalizeLang(replyLang, LangZh)
	if strings.TrimSpace(settings.APIKey) == "" {
		return fallbackReply(userText, contacts, replyLang), nil
	}
	return c.complete(ctx, settings, replyPrompt(settings.SystemPrompt, contacts, replyLang), normalizeHistory(history), userText)
}

// Translate 翻译一条客服对话内容。会先理解口语表达、语气和语境，再输出自然、地道的译文，
// 避免逐字直译。源语言与目标语言相同时直接返回原文。
func (c *Client) Translate(ctx context.Context, settings domain.AISettings, text, sourceLang, targetLang string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	sourceLang = NormalizeLang(sourceLang, LangZh)
	targetLang = NormalizeLang(targetLang, LangZh)
	if sourceLang == targetLang {
		return text, nil
	}
	if strings.TrimSpace(settings.APIKey) == "" {
		return "", errors.New("AI API key is required for translation")
	}
	// 翻译是单条内容处理，不需要会话历史上下文。
	out, err := c.complete(ctx, settings, translationPrompt(sourceLang, targetLang), nil, text)
	if err != nil {
		return "", err
	}
	out = cleanTranslatedText(out)
	if out == "" {
		return "", errors.New("empty translation response")
	}
	return out, nil
}

// complete 调用配置的上游模型（chat_completions / responses / anthropic_messages），返回文本结果。
func (c *Client) complete(ctx context.Context, settings domain.AISettings, systemPrompt string, history []HistoryTurn, userText string) (string, error) {
	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	apiType := strings.TrimSpace(settings.APIType)
	if apiType == "" {
		apiType = "chat_completions"
	}
	switch apiType {
	case "responses":
		return c.responses(ctx, settings, systemPrompt, history, userText)
	case "anthropic_messages":
		return c.anthropicMessages(ctx, settings, systemPrompt, history, userText)
	case "chat_completions":
		return c.chatCompletions(ctx, settings, systemPrompt, history, userText)
	}
	return "", fmt.Errorf("unsupported AI API type: %s", apiType)
}

func (c *Client) Models(ctx context.Context, settings domain.AISettings) ([]string, error) {
	if strings.TrimSpace(settings.APIKey) == "" {
		return nil, errors.New("AI API key is required")
	}
	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalizeBaseURL(settings.BaseURL)+"/models", nil)
	if err != nil {
		return nil, err
	}
	setAPIHeaders(req, settings)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, responseError(resp)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(out.Data))
	seen := map[string]bool{}
	for _, item := range out.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("upstream returned no models")
	}
	return models, nil
}

func (c *Client) chatCompletions(ctx context.Context, settings domain.AISettings, systemPrompt string, history []HistoryTurn, userText string) (string, error) {
	baseURL := normalizeBaseURL(settings.BaseURL)
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
	}
	for _, turn := range history {
		messages = append(messages, map[string]string{"role": turn.Role, "content": turn.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": userText})
	body := map[string]any{
		"model":       nonEmpty(settings.Model, "gpt-4o-mini"),
		"temperature": settings.Temperature,
		"messages":    messages,
	}
	if settings.MaxOutputTokens > 0 {
		body["max_tokens"] = settings.MaxOutputTokens
	}

	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", responseError(resp)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("empty AI response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (c *Client) responses(ctx context.Context, settings domain.AISettings, systemPrompt string, history []HistoryTurn, userText string) (string, error) {
	baseURL := normalizeBaseURL(settings.BaseURL)
	input := make([]map[string]string, 0, len(history)+1)
	for _, turn := range history {
		input = append(input, map[string]string{"role": turn.Role, "content": turn.Content})
	}
	input = append(input, map[string]string{"role": "user", "content": userText})
	body := map[string]any{
		"model":        nonEmpty(settings.Model, "gpt-4o-mini"),
		"instructions": systemPrompt,
		"input":        input,
		"temperature":  settings.Temperature,
	}
	if settings.MaxOutputTokens > 0 {
		body["max_output_tokens"] = settings.MaxOutputTokens
	}

	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", responseError(resp)
	}

	var out struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.OutputText) != "" {
		return strings.TrimSpace(out.OutputText), nil
	}
	for _, item := range out.Output {
		for _, content := range item.Content {
			if strings.TrimSpace(content.Text) != "" {
				return strings.TrimSpace(content.Text), nil
			}
		}
	}
	return "", errors.New("empty AI response")
}

func (c *Client) anthropicMessages(ctx context.Context, settings domain.AISettings, systemPrompt string, history []HistoryTurn, userText string) (string, error) {
	messages := make([]map[string]string, 0, len(history)+1)
	for _, turn := range history {
		messages = append(messages, map[string]string{"role": turn.Role, "content": turn.Content})
	}
	// Anthropic 要求首条消息必须是 user：若历史以 assistant 开头，裁掉开头连续的 assistant。
	for len(messages) > 0 && messages[0]["role"] == "assistant" {
		messages = messages[1:]
	}
	messages = append(messages, map[string]string{"role": "user", "content": userText})
	body := map[string]any{
		"model":       nonEmpty(settings.Model, "claude-3-5-haiku-latest"),
		"max_tokens":  maxInt(settings.MaxOutputTokens, 512),
		"temperature": settings.Temperature,
		"system":      systemPrompt,
		"messages":    messages,
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeBaseURL(settings.BaseURL)+"/messages", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", responseError(resp)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, item := range out.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text), nil
		}
	}
	return "", errors.New("empty AI response")
}

func setAPIHeaders(req *http.Request, settings domain.AISettings) {
	if strings.TrimSpace(settings.APIType) == "anthropic_messages" {
		req.Header.Set("x-api-key", settings.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+settings.APIKey)
	}
	req.Header.Set("Accept", "application/json")
}

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func normalizeBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "https://api.openai.com/v1"
	}
	return value
}

func nonEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// replyPrompt 在系统提示词基础上补充联系方式，并要求 AI 用访客语言回复。
func replyPrompt(prompt string, contacts domain.ContactSettings, lang string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "你是公司的在线客服助手，请礼貌、简洁地回答访客问题。"
	}
	langName := LangName(lang)
	return fmt.Sprintf("%s\n\n重要：无论访客使用什么语言，你必须始终使用%s回复访客。\n公司联系方式：电话 %s，WhatsApp %s，Telegram %s。", prompt, langName, contacts.Telephone, contacts.WhatsApp, contacts.Telegram)
}

// translationPrompt 生成口语化、地道的翻译指令。
func translationPrompt(sourceLang, targetLang string) string {
	sourceName := LangName(sourceLang)
	targetName := LangName(targetLang)
	return fmt.Sprintf("你是一名精通%s和%s的在线客服场景翻译专家。请把用户发来的一条在线客服对话消息翻译成%s。\n要求：\n1. 先完整理解原文的口语表达、语气、意图和语境，再组织译文，严禁逐字直译。\n2. 译文必须像母语者日常交流一样自然、地道、礼貌，符合%s地区客服场景的表达习惯。\n3. 保留数字、订单号、链接、联系方式、品牌名等关键信息，不要增加或删减内容。\n4. 只输出译文本身，不要输出任何解释、引号、前缀或多余内容。", sourceName, targetName, targetName, targetName)
}

// cleanTranslatedText 去掉模型译文外常见的引号、代码块或多余空白。
func cleanTranslatedText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if len(text) >= 2 {
		if (strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"")) ||
			(strings.HasPrefix(text, "“") && strings.HasSuffix(text, "”")) ||
			(strings.HasPrefix(text, "«") && strings.HasSuffix(text, "»")) {
			text = strings.TrimSpace(text[1 : len(text)-1])
		}
	}
	return text
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("AI API status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// fallbackReply 在未配置 AI API Key 时按访客语言返回兜底回复。
func fallbackReply(text string, contacts domain.ContactSettings, lang string) string {
	lowerText := strings.ToLower(text)
	switch lang {
	case LangEn:
		switch {
		case strings.Contains(lowerText, "phone") || strings.Contains(lowerText, "call"):
			return "Support phone: " + contacts.Telephone + ". You can tap the call button to reach us directly."
		case strings.Contains(lowerText, "whatsapp"):
			return "Official WhatsApp: " + contacts.WhatsApp + ". You can copy the number and add us."
		case strings.Contains(lowerText, "telegram"):
			return "Official Telegram: " + contacts.Telegram + ". You can copy the ID and add us."
		default:
			return "I've received your question — feel free to add more details. Our support team will follow up with you shortly."
		}
	case LangAr:
		switch {
		case strings.Contains(lowerText, "هاتف") || strings.Contains(lowerText, "اتصال") || strings.Contains(lowerText, "phone"):
			return "هاتف الدعم: " + contacts.Telephone + "، يمكنك الضغط على زر الاتصال للتواصل معنا مباشرة."
		case strings.Contains(lowerText, "whatsapp") || strings.Contains(lowerText, "واتساب"):
			return "واتساب الرسمي: " + contacts.WhatsApp + "، يمكنك نسخ الرقم وإضافته."
		case strings.Contains(lowerText, "telegram") || strings.Contains(lowerText, "تيليجرام") || strings.Contains(lowerText, "تليجرام"):
			return "تيليجرام الرسمي: " + contacts.Telegram + "، يمكنك نسخ المعرف وإضافته."
		default:
			return "استلمنا سؤالك، يمكنك إضافة المزيد من التفاصيل، وسيتابع فريق الدعم معك قريبًا."
		}
	default:
		switch {
		case strings.Contains(text, "电话"):
			return "客服电话：" + contacts.Telephone + "，您可以点击下方电话联系按钮直接拨打。"
		case strings.Contains(lowerText, "whatsapp"):
			return "官方 WhatsApp：" + contacts.WhatsApp + "，您可以长按复制添加。"
		case strings.Contains(lowerText, "telegram"):
			return "官方 Telegram：" + contacts.Telegram + "，您可以长按复制添加。"
		default:
			return "我已收到您的问题。您可以继续补充细节，人工客服会尽快为您跟进。"
		}
	}
}
