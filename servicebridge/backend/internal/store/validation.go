package store

import (
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
	if strings.HasPrefix(value, "/uploads/") {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}
