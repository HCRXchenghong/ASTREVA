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

// 缓存模式常量。
const (
	CacheModeAuto     = "auto"     // 上游自动提示词缓存（OpenAI 系）
	CacheModeExplicit = "explicit" // 需显式 cache_control 标记（Anthropic）
	CacheModeNone     = "none"     // 探测确认不支持或未知
)

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

// ErrNoAPIKey 表示接待/翻译等 AI 能力尚未配置可用的 API Key。
// 调用方必须把它当作真实失败处理（提示管理员或转人工），不能返回伪造的 AI 回复。
var ErrNoAPIKey = errors.New("AI API Key 未配置")

// Reply 根据访客语言生成 AI 客服回复。replyLang 为访客端看到的语言（zh/en/ar）。
// history 为当前会话的历史对话轮次（按会话隔离传入，不得跨会话复用）。
// cacheKey 为会话级缓存路由键（会话 ID），仅在支持缓存的上游生效。
func (c *Client) Reply(ctx context.Context, settings domain.AISettings, contacts domain.ContactSettings, history []HistoryTurn, userText, replyLang, cacheKey string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}
	replyLang = NormalizeLang(replyLang, LangZh)
	if strings.TrimSpace(settings.APIKey) == "" {
		return "", ErrNoAPIKey
	}
	return c.complete(ctx, settings, replyPrompt(settings.SystemPrompt, contacts, replyLang), normalizeHistory(history), userText, cacheKey)
}

// Translate 翻译一条客服对话内容。会先理解口语表达、语气和语境，再输出自然、地道的译文，
// 避免逐字直译。源语言与目标语言相同时直接返回原文。
func (c *Client) Translate(ctx context.Context, settings domain.AISettings, text, sourceLang, targetLang string) (string, error) {
	return c.TranslateWithStyle(ctx, settings, text, sourceLang, targetLang, "")
}

// TranslateWithStyle 与 Translate 相同，但允许附加目标语言风格提示（如"拉美西班牙语""海湾阿拉伯语"）。
// styleHint 为空时行为与 Translate 完全一致。
func (c *Client) TranslateWithStyle(ctx context.Context, settings domain.AISettings, text, sourceLang, targetLang, styleHint string) (string, error) {
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
	systemPrompt := translationPrompt(sourceLang, targetLang)
	if hint := strings.TrimSpace(styleHint); hint != "" {
		systemPrompt += "\n风格要求：" + hint
	}
	// 翻译是单条内容处理，不需要会话历史上下文。
	out, err := c.complete(ctx, settings, systemPrompt, nil, text, "")
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
func (c *Client) complete(ctx context.Context, settings domain.AISettings, systemPrompt string, history []HistoryTurn, userText, cacheKey string) (string, error) {
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
		return c.responses(ctx, settings, systemPrompt, history, userText, cacheKey)
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
	return c.ListModels(ctx, settings)
}

// ListModels 请求上游 ${base_url}/models 并解析 OpenAI 兼容的 data[].id。
// 与 Models 的区别：允许 API Key 为空（本地/内网无鉴权上游），供供应商管理页使用。
func (c *Client) ListModels(ctx context.Context, settings domain.AISettings) ([]string, error) {
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
	if strings.TrimSpace(settings.APIKey) != "" {
		setAPIHeaders(req, settings)
	} else {
		req.Header.Set("Accept", "application/json")
	}
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

func (c *Client) responses(ctx context.Context, settings domain.AISettings, systemPrompt string, history []HistoryTurn, userText, cacheKey string) (string, error) {
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
	// 探测确认支持自动缓存时，用会话 ID 作为缓存路由键：
	// 同一会话的多轮请求会被路由到同一缓存槽，前缀（系统提示词+历史）命中率最高。
	if settings.CacheMode == CacheModeAuto && strings.TrimSpace(cacheKey) != "" {
		body["prompt_cache_key"] = cacheKey
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
	type contentBlock struct {
		Type         string             `json:"type"`
		Text         string             `json:"text"`
		CacheControl *map[string]string `json:"cache_control,omitempty"`
	}
	ephemeral := map[string]string{"type": "ephemeral"}
	// 探测确认支持显式缓存时，把缓存断点打在"系统提示词 + 全部历史"末尾：
	// 下一轮请求的前缀（系统+历史）与本轮完全一致，即可命中缓存（读取仅 1 折计费）。
	useCache := settings.CacheMode == CacheModeExplicit
	system := any(systemPrompt)
	if useCache {
		system = []contentBlock{{Type: "text", Text: systemPrompt, CacheControl: &ephemeral}}
	}
	messages := make([]map[string]any, 0, len(history)+1)
	for _, turn := range history {
		messages = append(messages, map[string]any{"role": turn.Role, "content": turn.Content})
	}
	// Anthropic 要求首条消息必须是 user：若历史以 assistant 开头，裁掉开头连续的 assistant。
	for len(messages) > 0 && messages[0]["role"] == "assistant" {
		messages = messages[1:]
	}
	if useCache && len(messages) > 0 {
		last := messages[len(messages)-1]
		last["content"] = []contentBlock{{Type: "text", Text: fmt.Sprintf("%v", last["content"]), CacheControl: &ephemeral}}
	}
	messages = append(messages, map[string]any{"role": "user", "content": userText})
	body := map[string]any{
		"model":       nonEmpty(settings.Model, "claude-3-5-haiku-latest"),
		"max_tokens":  maxInt(settings.MaxOutputTokens, 512),
		"temperature": settings.Temperature,
		"system":      system,
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

// probeText 生成约 1300 token 的确定性探测文本。
// OpenAI 自动缓存要求前缀 >=1024 token，必须足够长才能触发。
func probeText() string {
	base := "Cache verification paragraph for prompt caching support detection. Stainless steel channel steel supply chain context. "
	return strings.Repeat(base, 96)
}

// ProbeCacheSupport 对单个模型实测是否支持提示词缓存：
// 连续发送两次相同的大前缀请求，第二次若报告缓存命中（OpenAI 自动缓存），
// 或写入缓存成功（Anthropic 显式缓存），即判定支持。
// 返回 CacheModeAuto / CacheModeExplicit / CacheModeNone。
func (c *Client) ProbeCacheSupport(ctx context.Context, settings domain.AISettings, model string) (string, error) {
	settings.Model = model
	if strings.TrimSpace(settings.APIKey) == "" {
		return CacheModeNone, errors.New("AI API key is required")
	}
	apiType := strings.TrimSpace(settings.APIType)
	if apiType == "" {
		apiType = "chat_completions"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	switch apiType {
	case "responses":
		return c.probeResponses(probeCtx, settings)
	case "anthropic_messages":
		return c.probeAnthropic(probeCtx, settings)
	default:
		return c.probeChatCompletions(probeCtx, settings)
	}
}

func (c *Client) probeRequest(ctx context.Context, url string, settings domain.AISettings, body map[string]any) (map[string]any, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, responseError(resp)
	}
	out := map[string]any{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// usageDetail 从 usage 的 *tokens_details 子对象里取字段（兼容各家命名）。
func usageDetail(usage map[string]any, detailKey, field string) float64 {
	details, _ := usage[detailKey].(map[string]any)
	if details == nil {
		return 0
	}
	value, _ := details[field].(float64)
	return value
}

func (c *Client) probeChatCompletions(ctx context.Context, settings domain.AISettings) (string, error) {
	url := normalizeBaseURL(settings.BaseURL) + "/chat/completions"
	text := probeText()
	mkBody := func() map[string]any {
		return map[string]any{
			"model":       nonEmpty(settings.Model, "gpt-4o-mini"),
			"temperature": 0,
			"max_tokens":  1,
			"messages": []map[string]string{
				{"role": "user", "content": text},
			},
		}
	}
	if _, err := c.probeRequest(ctx, url, settings, mkBody()); err != nil {
		return CacheModeNone, err
	}
	time.Sleep(3 * time.Second)
	second, err := c.probeRequest(ctx, url, settings, mkBody())
	if err != nil {
		return CacheModeNone, err
	}
	usage, _ := second["usage"].(map[string]any)
	if usage != nil && usageDetail(usage, "prompt_tokens_details", "cached_tokens") > 0 {
		return CacheModeAuto, nil
	}
	return CacheModeNone, nil
}

func (c *Client) probeResponses(ctx context.Context, settings domain.AISettings) (string, error) {
	url := normalizeBaseURL(settings.BaseURL) + "/responses"
	text := probeText()
	mkBody := func() map[string]any {
		return map[string]any{
			"model":             nonEmpty(settings.Model, "gpt-4o-mini"),
			"temperature":       0,
			"max_output_tokens": 1,
			"input":             text,
		}
	}
	if _, err := c.probeRequest(ctx, url, settings, mkBody()); err != nil {
		return CacheModeNone, err
	}
	time.Sleep(3 * time.Second)
	second, err := c.probeRequest(ctx, url, settings, mkBody())
	if err != nil {
		return CacheModeNone, err
	}
	usage, _ := second["usage"].(map[string]any)
	if usage != nil && usageDetail(usage, "input_tokens_details", "cached_tokens") > 0 {
		return CacheModeAuto, nil
	}
	return CacheModeNone, nil
}

func (c *Client) probeAnthropic(ctx context.Context, settings domain.AISettings) (string, error) {
	url := normalizeBaseURL(settings.BaseURL) + "/messages"
	body := map[string]any{
		"model":      nonEmpty(settings.Model, "claude-3-5-haiku-latest"),
		"max_tokens": 1,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type":          "text",
						"text":          probeText(),
						"cache_control": map[string]string{"type": "ephemeral"},
					},
				},
			},
		},
	}
	out, err := c.probeRequest(ctx, url, settings, body)
	if err != nil {
		return CacheModeNone, err
	}
	usage, _ := out["usage"].(map[string]any)
	if usage != nil {
		if created, _ := usage["cache_creation_input_tokens"].(float64); created > 0 {
			return CacheModeExplicit, nil
		}
	}
	return CacheModeNone, nil
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

// UpstreamError 表示上游返回了非成功 HTTP 状态。
// 保留 StatusCode 供上层判定可重试性/熔断冷却时长；Error() 文案与历史实现完全一致，
// 既有日志与测试断言（status=NNN body=...）不受影响。
type UpstreamError struct {
	StatusCode int
	Body       string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("AI API status=%d body=%s", e.StatusCode, e.Body)
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return &UpstreamError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
}
