package app

import (
	"context"
	"errors"
	"strings"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

// TranslationPreview 是"发送前译文预览"的结果：只翻译，不落库、不创建消息。
type TranslationPreview struct {
	TargetLang     string `json:"target_lang"`
	SourceLang     string `json:"source_lang"`
	TranslatedText string `json:"translated_text"`
	Dialect        string `json:"dialect"`
}

// dialectStyleHints 把检测到的方言标签映射为目标语言风格提示词。
// 键统一用小写做匹配，输出仍使用可读的中文/英文名称。
var dialectStyleHints = map[string]string{
	"ar-gulf":  "海湾阿拉伯语（Gulf Arabic）",
	"ar-eg":    "埃及阿拉伯语（Egyptian Arabic）",
	"ar-lev":   "黎凡特阿拉伯语（Levantine Arabic）",
	"ar-ma":    "摩洛哥阿拉伯语（Moroccan Arabic）",
	"es-419":   "拉丁美洲西班牙语（Latin American Spanish）",
	"es-ar":    "阿根廷西班牙语（Argentinian Spanish）",
	"es-es":    "西班牙本土西班牙语（Castilian Spanish）",
	"pt-br":    "巴西葡萄牙语（Brazilian Portuguese）",
	"pt-pt":    "欧洲葡萄牙语（European Portuguese）",
	"zh-hant":  "繁体中文（Traditional Chinese）",
	"yue-hant": "粤语（Cantonese，繁体书写）",
	"en-gb":    "英式英语（British English）",
	"en-us":    "美式英语（American English）",
}

// agentPreferences 读取客服偏好并填充默认值；账号不存在时按默认偏好处理，
// 避免历史数据或已删除账号影响消息投递。
func (s *Service) agentPreferences(agentID string) domain.AgentPreferences {
	if strings.TrimSpace(agentID) == "" {
		return domain.AgentPreferences{}.WithDefaults()
	}
	preferences, err := s.store.AgentPreferences(agentID)
	if err != nil {
		s.logger.Warn("load agent preferences failed", "agent_id", agentID, "error", err)
		return domain.AgentPreferences{}.WithDefaults()
	}
	return preferences.WithDefaults()
}

// prefBool 把可空布尔偏好按默认 true 解释。
func prefBool(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

// effectiveDialectTag 返回与目标语言匹配的方言标签。
// conversation.DetectedLang 可能保存 BCP-47 标签（如 es-419），基础语言码则不算方言。
func effectiveDialectTag(conversation domain.Conversation, targetLang string) string {
	tag := strings.TrimSpace(conversation.DetectedLang)
	if !strings.Contains(tag, "-") {
		return ""
	}
	if ai.NormalizeLang(tag, "") != ai.NormalizeLang(targetLang, ai.LangZh) {
		return ""
	}
	return tag
}

// dialectStyleHint 查表返回风格提示；未收录的方言返回空（回落到标准语翻译）。
func dialectStyleHint(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return ""
	}
	return dialectStyleHints[strings.ToLower(strings.TrimSpace(tag))]
}

// translationStyleHint 计算一次翻译应附加的风格提示：
// 仅当客服开启方言模式且会话检测到的方言与目标语言一致时生效。
func (s *Service) translationStyleHint(preferences domain.AgentPreferences, conversation domain.Conversation, targetLang string) string {
	if preferences.DialectMode != domain.DialectModeDialect {
		return ""
	}
	return dialectStyleHint(effectiveDialectTag(conversation, targetLang))
}

// TranslatePreview 为客服生成发送前译文预览。
// 目标语言使用会话语言策略，源语言使用内容识别结果；source==target 时原样返回。
// 该路径只调用翻译模型，不创建任何消息。
func (s *Service) TranslatePreview(ctx context.Context, agentID, conversationID, content string) (TranslationPreview, error) {
	if strings.TrimSpace(content) == "" {
		return TranslationPreview{}, store.ErrInvalidInput
	}
	conversation, ok := s.store.Conversation(conversationID)
	if !ok {
		return TranslationPreview{}, store.ErrNotFound
	}
	targetLang := conversationReplyLang(conversation)
	detection := ai.DetectLanguage(content)
	sourceLang := ai.NormalizeLang(detection.Code, ai.LangZh)
	preferences := s.agentPreferences(agentID)
	dialect := effectiveDialectTag(conversation, targetLang)
	preview := TranslationPreview{
		TargetLang:     targetLang,
		SourceLang:     sourceLang,
		TranslatedText: content,
		Dialect:        dialect,
	}
	if sourceLang == targetLang {
		return preview, nil
	}
	styleHint := ""
	if preferences.DialectMode == domain.DialectModeDialect {
		styleHint = dialectStyleHint(dialect)
	}
	translateCtx, cancel := context.WithTimeout(ctx, translationWindow)
	defer cancel()
	// 翻译能力优先使用其绑定的供应商；白名单校验失败时直接返回错误，不发出请求。
	// 绑定多个供应商时按优先级依次尝试，失败自动切下一个。
	translated, err := s.doWithFailover(translateCtx, domain.AICapabilityTranslation, func(candidate domain.AISettings) (string, error) {
		return s.ai.TranslateWithStyle(translateCtx, candidate, content, sourceLang, targetLang, styleHint)
	})
	if err != nil {
		return TranslationPreview{}, err
	}
	if strings.TrimSpace(translated) == "" {
		return TranslationPreview{}, errors.New("empty translation response")
	}
	preview.TranslatedText = translated
	return preview, nil
}
