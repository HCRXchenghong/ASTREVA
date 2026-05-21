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
	maxPushTokenRunes     = 512
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
	case "", "contains", "exact":
	default:
		return ErrInvalidInput
	}
	switch strings.TrimSpace(rule.Action) {
	case "", "text", "phone", "wechat", "handoff":
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
		case "chat_completions", "responses":
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
	if next.NoReplyTimeoutSeconds < 0 || next.NoReplyTimeoutSeconds > 3600 {
		return ErrInvalidInput
	}
	return nil
}

func validateBusinessHours(next domain.BusinessHours) error {
	if _, err := cleanOptionalText(next.Timezone, 64); err != nil {
		return err
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
	return nil
}

func validateContactSettings(next domain.ContactSettings) error {
	for _, value := range []string{next.Phone, next.Wechat, next.QQ} {
		if _, err := cleanOptionalText(value, maxContactRunes); err != nil {
			return err
		}
	}
	if _, err := cleanOptionalText(next.EntryReply, maxEntryReplyRunes); err != nil {
		return err
	}
	for _, value := range []string{next.WechatImageURL, next.QQImageURL} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if runeLen(value) > maxImageURLRunes || !isSafePublicURL(value) {
			return ErrInvalidInput
		}
	}
	if normalized := normalizeReplyType(next.WechatReplyType); strings.TrimSpace(next.WechatReplyType) != "" && normalized == "" {
		return ErrInvalidInput
	}
	if normalized := normalizeReplyType(next.QQReplyType); strings.TrimSpace(next.QQReplyType) != "" && normalized == "" {
		return ErrInvalidInput
	}
	return nil
}

func validatePushDevice(device domain.PushDevice) error {
	if _, err := cleanRequiredText(device.Token, maxPushTokenRunes); err != nil {
		return err
	}
	if _, err := cleanRequiredText(device.Platform, 32); err != nil {
		return err
	}
	if _, err := cleanOptionalText(device.Provider, 32); err != nil {
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
