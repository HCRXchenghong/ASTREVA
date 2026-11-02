package store

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"customer-service/backend/internal/domain"
)

const (
	maxAccountRunes       = 64
	maxNameRunes          = 64
	maxGroupRunes         = 64
	maxSourceRunes        = 128
	maxRemarkRunes        = 128
	maxClientMsgIDRunes   = 128
	maxMessageRunes       = 4000
	maxContactRunes       = 128
	maxImageURLRunes      = 512
	maxKeywordRunes       = 80
	maxKeywordReplyRunes  = 1000
	maxQuickReplyRunes    = 80
	maxEntryReplyRunes    = 1000
	maxRatingCommentRunes = 500
	maxSystemPromptRunes  = 8000
	// maxProfileVisitorIDRunes 是画像键（官网 visitor_ref）的长度上限。
	maxProfileVisitorIDRunes = 64
)

func cleanRequiredText(value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || runeLen(value) > maxRunes {
		return "", ErrInvalidInput
	}
	return value, nil
}

func cleanOptionalText(value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if runeLen(value) > maxRunes {
		return "", ErrInvalidInput
	}
	return value, nil
}

// cleanProfileVisitorID 清洗官网埋点写入的画像键（visitor_ref）：
// 去首尾空白，超长（>64 字符）或含非 [A-Za-z0-9_.-] 字符时返回空串。
// 调用方把空串视为"未提供"，直接忽略该字段，绝不因此拒绝建会话。
func cleanProfileVisitorID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || runeLen(value) > maxProfileVisitorIDRunes {
		return ""
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
		default:
			return ""
		}
	}
	return value
}

func validateAgentStatus(status domain.AgentStatus) error {
	switch status {
	case domain.AgentOnline, domain.AgentBusy, domain.AgentOffline:
		return nil
	default:
		return ErrInvalidInput
	}
}

func validateAgentInput(agent domain.Agent, requireAccount bool) error {
	if requireAccount {
		if _, err := cleanRequiredText(agent.Account, maxAccountRunes); err != nil {
			return err
		}
	}
	if _, err := cleanRequiredText(agent.Name, maxNameRunes); err != nil {
		return err
	}
	if _, err := cleanOptionalText(agent.Group, maxGroupRunes); err != nil {
		return err
	}
	if agent.MaxConversations < 0 || agent.MaxConversations > 200 {
		return ErrInvalidInput
	}
	if agent.Status != "" {
		return validateAgentStatus(agent.Status)
	}
	return nil
}

func validateTemporaryPassword(password string) error {
	return validateNewPassword(password)
}

func validateKeywordRule(rule domain.KeywordRule) error {
	if _, err := cleanRequiredText(rule.Keyword, maxKeywordRunes); err != nil {
		return err
	}
	if _, err := cleanRequiredText(rule.Reply, maxKeywordReplyRunes); err != nil {
		return err
	}
	if _, err := cleanOptionalText(rule.QuickReplyText, maxQuickReplyRunes); err != nil {
		return err
	}
	switch strings.TrimSpace(rule.MatchType) {
	case "", "contains", "exact", "prefix":
	default:
		return ErrInvalidInput
	}
	switch strings.TrimSpace(rule.Action) {
	case "", "text", "telephone", "whatsapp", "telegram", "handoff":
	default:
		return ErrInvalidInput
	}
	if rule.Priority < -1000 || rule.Priority > 1000 {
		return ErrInvalidInput
	}
	return nil
}

func validateAISettings(next domain.AISettings) error {
	if next.Mode != "" {
		switch next.Mode {
		case domain.AIModeHumanFirst, domain.AIModeAlwaysAI, domain.AIModeManualOnly:
		default:
			return ErrInvalidInput
		}
	}
	if next.APIType != "" {
		switch strings.TrimSpace(next.APIType) {
		case "chat_completions", "responses", "anthropic_messages":
		default:
			return ErrInvalidInput
		}
	}
	if strings.TrimSpace(next.BaseURL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(next.BaseURL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return ErrInvalidInput
		}
	}
	if _, err := cleanOptionalText(next.Model, 128); err != nil {
		return err
	}
	if _, err := cleanOptionalText(next.APIKey, 2048); err != nil {
		return err
	}
	if _, err := cleanOptionalText(next.SystemPrompt, maxSystemPromptRunes); err != nil {
		return err
	}
	if next.Temperature < 0 || next.Temperature > 2 {
		return ErrInvalidInput
	}
	if next.MaxOutputTokens < 0 || next.MaxOutputTokens > 8192 {
		return ErrInvalidInput
	}
	if next.TimeoutSeconds < 0 || next.TimeoutSeconds > 120 {
		return ErrInvalidInput
	}
	return nil
}

// ValidateAISettingsForRequest validates connection-preview fields without persisting them.
func ValidateAISettingsForRequest(next domain.AISettings) error {
	return validateAISettings(next)
}

func validateBusinessHours(next domain.BusinessHours) error {
	if _, err := cleanOptionalText(next.Timezone, 64); err != nil {
		return err
	}
	if strings.TrimSpace(next.Timezone) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(next.Timezone)); err != nil {
			return ErrInvalidInput
		}
	}
	if strings.TrimSpace(next.Start) != "" {
		if _, err := time.Parse("15:04", strings.TrimSpace(next.Start)); err != nil {
			return ErrInvalidInput
		}
	}
	if strings.TrimSpace(next.End) != "" {
		if _, err := time.Parse("15:04", strings.TrimSpace(next.End)); err != nil {
			return ErrInvalidInput
		}
	}
	if _, err := cleanOptionalText(next.OfflineReply, maxEntryReplyRunes); err != nil {
		return err
	}
	return nil
}

func validateContactSettings(next domain.ContactSettings) error {
	for _, value := range []string{next.Telephone, next.WhatsApp, next.Telegram} {
		if _, err := cleanOptionalText(value, maxContactRunes); err != nil {
			return err
		}
	}
	if _, err := cleanOptionalText(next.EntryReply, maxEntryReplyRunes); err != nil {
		return err
	}
	return nil
}

func validateRatingInput(tags []string, comment string) error {
	if _, err := cleanOptionalText(comment, maxRatingCommentRunes); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := cleanOptionalText(tag, 32); err != nil {
			return err
		}
	}
	return nil
}

func validateClientMsgID(value string) error {
	_, err := cleanOptionalText(value, maxClientMsgIDRunes)
	return err
}

const (
	maxFirebaseValueRunes = 512
	maxPushSecretRunes    = 20000
	maxAPNsTopicRunes     = 256
)

const (
	maxAIProviderNameRunes  = 64
	maxAIProviderURLRunes   = 512
	maxAIProviderKeyRunes   = 2048
	maxAIModelRunes         = 128
	maxAIProviderModels     = 200
	maxAIAllowedHostsRunes  = 512
	maxAIBindingProviderID  = 64
	maxAIBindingParamsBytes = 8192
)

// normalizeAIProvider 归一化并校验供应商配置：补默认值、去重模型、校验白名单格式。
// apiKey 由调用方单独处理，不在此函数内。
func normalizeAIProvider(next domain.AIProvider) (domain.AIProvider, error) {
	next.Name = strings.TrimSpace(next.Name)
	if _, err := cleanRequiredText(next.Name, maxAIProviderNameRunes); err != nil {
		return domain.AIProvider{}, err
	}
	next.Kind = strings.ToLower(strings.TrimSpace(next.Kind))
	if next.Kind == "" {
		next.Kind = domain.AIProviderKindText
	}
	if !domain.ValidAIProviderKind(next.Kind) {
		return domain.AIProvider{}, ErrInvalidInput
	}
	next.APIType = strings.ToLower(strings.TrimSpace(next.APIType))
	if next.APIType == "" {
		if next.Kind == domain.AIProviderKindImage {
			next.APIType = domain.AIAPITypeImages
		} else {
			next.APIType = domain.AIAPITypeChatCompletions
		}
	}
	if !domain.ValidAIAPIType(next.APIType) {
		return domain.AIProvider{}, ErrInvalidInput
	}
	// 供应商类型与协议类型必须匹配：图像供应商只能走 images，文本供应商不能走 images。
	if next.Kind == domain.AIProviderKindImage && next.APIType != domain.AIAPITypeImages {
		return domain.AIProvider{}, ErrInvalidInput
	}
	if next.Kind == domain.AIProviderKindText && next.APIType == domain.AIAPITypeImages {
		return domain.AIProvider{}, ErrInvalidInput
	}
	next.BaseURL = strings.TrimSpace(next.BaseURL)
	if _, err := cleanRequiredText(next.BaseURL, maxAIProviderURLRunes); err != nil {
		return domain.AIProvider{}, err
	}
	parsed, err := url.Parse(next.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return domain.AIProvider{}, ErrInvalidInput
	}
	models := make([]string, 0, len(next.Models))
	seen := map[string]bool{}
	for _, model := range next.Models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		if runeLen(model) > maxAIModelRunes {
			return domain.AIProvider{}, ErrInvalidInput
		}
		seen[model] = true
		models = append(models, model)
		if len(models) > maxAIProviderModels {
			return domain.AIProvider{}, ErrInvalidInput
		}
	}
	next.Models = models
	if _, err := cleanOptionalText(next.DefaultModel, maxAIModelRunes); err != nil {
		return domain.AIProvider{}, err
	}
	if next.TimeoutMS == 0 {
		next.TimeoutMS = 30000
	}
	if next.TimeoutMS < 1000 || next.TimeoutMS > 300000 {
		return domain.AIProvider{}, ErrInvalidInput
	}
	next.AllowedHosts = domain.NormalizeAllowedHosts(next.AllowedHosts)
	if runeLen(next.AllowedHosts) > maxAIAllowedHostsRunes || !domain.ValidateAllowedHosts(next.AllowedHosts) {
		return domain.AIProvider{}, ErrInvalidInput
	}
	if next.SortOrder < -10000 || next.SortOrder > 10000 {
		return domain.AIProvider{}, ErrInvalidInput
	}
	return next, nil
}

// validateAIProviderAPIKey 校验明文密钥长度。
func validateAIProviderAPIKey(value string) error {
	_, err := cleanOptionalText(value, maxAIProviderKeyRunes)
	return err
}

// normalizeAIBinding 归一化并校验能力绑定。
func normalizeAIBinding(next domain.AIBinding) (domain.AIBinding, error) {
	next.Capability = strings.ToLower(strings.TrimSpace(next.Capability))
	if !domain.ValidAICapability(next.Capability) {
		return domain.AIBinding{}, ErrInvalidInput
	}
	next.ProviderID = strings.TrimSpace(next.ProviderID)
	if len([]rune(next.ProviderID)) > maxAIBindingProviderID {
		return domain.AIBinding{}, ErrInvalidInput
	}
	if _, err := cleanOptionalText(next.Model, maxAIModelRunes); err != nil {
		return domain.AIBinding{}, err
	}
	if next.Params == nil {
		next.Params = map[string]any{}
	}
	raw, err := json.Marshal(next.Params)
	if err != nil || len(raw) > maxAIBindingParamsBytes {
		return domain.AIBinding{}, ErrInvalidInput
	}
	return next, nil
}

// normalizeAIBindingTarget 归一化并校验能力候选供应商：
// 能力必须合法、provider_id 非空且长度受限、model 长度受限、priority 负数归零。
// 是否要求供应商存在/启用/类型匹配由 HTTP 层整体校验（存储层保持与 ai_bindings 一致的宽松度）。
func normalizeAIBindingTarget(next domain.AIBindingTarget) (domain.AIBindingTarget, error) {
	next.Capability = strings.ToLower(strings.TrimSpace(next.Capability))
	if !domain.ValidAICapability(next.Capability) {
		return domain.AIBindingTarget{}, ErrInvalidInput
	}
	next.ProviderID = strings.TrimSpace(next.ProviderID)
	if next.ProviderID == "" || len([]rune(next.ProviderID)) > maxAIBindingProviderID {
		return domain.AIBindingTarget{}, ErrInvalidInput
	}
	if _, err := cleanOptionalText(next.Model, maxAIModelRunes); err != nil {
		return domain.AIBindingTarget{}, err
	}
	if next.Priority < 0 {
		next.Priority = 0
	}
	return next, nil
}

// normalizePushPlatform 归一化设备平台：令牌为空时平台也为空，其余只允许 android/ios。
func normalizePushPlatform(platform, deviceToken string) string {
	if strings.TrimSpace(deviceToken) == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(platform), string(domain.PushPlatformIOS)) {
		return string(domain.PushPlatformIOS)
	}
	return string(domain.PushPlatformAndroid)
}

// validatePushSettings 校验后台可配置的推送参数长度与枚举值。
func validatePushSettings(next domain.PushSettings) error {
	fields := []struct {
		value string
		max   int
	}{
		{next.FirebaseAPIKey, maxFirebaseValueRunes},
		{next.FirebaseAppID, maxFirebaseValueRunes},
		{next.FirebaseMessagingSenderID, maxFirebaseValueRunes},
		{next.FirebaseProjectID, maxFirebaseValueRunes},
		{next.FirebaseStorageBucket, maxFirebaseValueRunes},
		{next.FirebaseVapidKey, maxFirebaseValueRunes},
		{next.APNsKeyID, maxFirebaseValueRunes},
		{next.APNsTeamID, maxFirebaseValueRunes},
		{next.APNsTopic, maxAPNsTopicRunes},
		{next.FCMServiceAccountJSON, maxPushSecretRunes},
		{next.FCMAccessToken, maxPushSecretRunes},
		{next.APNsKeyP8, maxPushSecretRunes},
	}
	for _, field := range fields {
		if _, err := cleanOptionalText(field.value, field.max); err != nil {
			return err
		}
	}
	env := strings.ToLower(strings.TrimSpace(next.APNsEnvironment))
	if env != "" && env != "sandbox" && env != "production" {
		return ErrInvalidInput
	}
	return nil
}

func isSafePublicURL(value string) bool {
	// 图片/语音消息只允许客服引擎自己托管的 /uploads/ 相对路径。
	// 这样管理端浏览器不会因为访客可控的 URL 去请求任意内网/第三方地址，
	// 同时也避免开放 API 调用方把外部图片注入会话。
	if !strings.HasPrefix(value, "/uploads/") {
		return false
	}
	if strings.Contains(value, "..") || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	return true
}

const (
	maxKnowledgeBaseNameRunes    = 100
	maxKnowledgeDescriptionRunes = 500
	maxKnowledgeTitleRunes       = 200
	maxKnowledgeContentRunes     = 20000
	maxKnowledgeCategoryRunes    = 100
	maxKnowledgeKeywords         = 50
	maxKnowledgeKeywordRunes     = 100
	maxKnowledgeBaseIDRunes      = 64
)

// normalizeKnowledgeBase 归一化并校验知识库。
func normalizeKnowledgeBase(next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	name, err := cleanRequiredText(next.Name, maxKnowledgeBaseNameRunes)
	if err != nil {
		return domain.KnowledgeBase{}, err
	}
	description, err := cleanOptionalText(next.Description, maxKnowledgeDescriptionRunes)
	if err != nil {
		return domain.KnowledgeBase{}, err
	}
	if next.SortOrder < -10000 || next.SortOrder > 10000 {
		return domain.KnowledgeBase{}, ErrInvalidInput
	}
	next.Name = name
	next.Description = description
	return next, nil
}

// normalizeKnowledgeEntry 归一化并校验知识条目：关键词去空去重、正文必填。
func normalizeKnowledgeEntry(next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	baseID, err := cleanRequiredText(next.BaseID, maxKnowledgeBaseIDRunes)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	next.BaseID = baseID
	title, err := cleanOptionalText(next.Title, maxKnowledgeTitleRunes)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	next.Title = title
	content, err := cleanRequiredText(next.Content, maxKnowledgeContentRunes)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	next.Content = content
	category, err := cleanOptionalText(next.Category, maxKnowledgeCategoryRunes)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	next.Category = category
	keywords := make([]string, 0, len(next.Keywords))
	seen := map[string]bool{}
	for _, keyword := range next.Keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" || seen[keyword] {
			continue
		}
		if runeLen(keyword) > maxKnowledgeKeywordRunes {
			return domain.KnowledgeEntry{}, ErrInvalidInput
		}
		seen[keyword] = true
		keywords = append(keywords, keyword)
		if len(keywords) > maxKnowledgeKeywords {
			return domain.KnowledgeEntry{}, ErrInvalidInput
		}
	}
	next.Keywords = keywords
	if next.Priority < -1000 || next.Priority > 1000 {
		return domain.KnowledgeEntry{}, ErrInvalidInput
	}
	return next, nil
}

// normalizeKnowledgeSettings 归一化并校验知识库配置：显式非法取值一律拒绝，不静默改写。
func normalizeKnowledgeSettings(next domain.KnowledgeSettings) (domain.KnowledgeSettings, error) {
	raw := strings.TrimSpace(next.UnknownAction)
	if raw != "" && !domain.ValidKnowledgeUnknownAction(raw) {
		return domain.KnowledgeSettings{}, ErrInvalidInput
	}
	if next.ScoreThreshold < 0 || next.ScoreThreshold > 1 {
		return domain.KnowledgeSettings{}, ErrInvalidInput
	}
	if next.MaxEntries < 0 || next.MaxEntries > 20 {
		return domain.KnowledgeSettings{}, ErrInvalidInput
	}
	out := domain.NormalizeKnowledgeSettings(next)
	if _, err := cleanOptionalText(out.RefuseReply, maxKeywordReplyRunes); err != nil {
		return domain.KnowledgeSettings{}, err
	}
	return out, nil
}

// normalizeAILimits 归一化并校验 AI 限额配置。
// 数值按 domain.NormalizeAILimits 做负数归零与上限收敛（存储层保持健壮），
// 提示文案额外做长度校验，避免超长文本反复落库/翻译。
func normalizeAILimits(next domain.AILimits) (domain.AILimits, error) {
	out := domain.NormalizeAILimits(next)
	exceed, err := cleanOptionalText(out.ExceedMessage, maxKeywordReplyRunes)
	if err != nil {
		return domain.AILimits{}, err
	}
	out.ExceedMessage = exceed
	return out, nil
}

// cleanKnowledgeBaseIDs 清洗客服限定的知识库 ID 列表：去空、去重、限长。
func cleanKnowledgeBaseIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}
