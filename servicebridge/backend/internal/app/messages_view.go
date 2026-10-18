package app

import (
	"strings"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
)

// VisitorFacingMessage 保留历史行为：对撤回消息做脱敏。
func VisitorFacingMessage(msg domain.Message) domain.Message {
	return VisitorFacingMessageForLang(msg, ai.LangZh)
}

// VisitorFacingMessages 保留历史行为。
func VisitorFacingMessages(messages []domain.Message) []domain.Message {
	return VisitorFacingMessagesForLang(messages, ai.LangZh)
}

// VisitorFacingMessageForLang 生成面向访客的消息视图：
//   - 隐藏仅客服端可见的转接系统消息；
//   - 撤回消息用访客语言脱敏；
//   - 内容替换为访客语言译文（若有），并清空翻译字段。
func VisitorFacingMessageForLang(msg domain.Message, lang string) domain.Message {
	lang = ai.NormalizeLang(lang, ai.LangZh)
	if msg.SenderType == domain.SenderSystem && msg.MessageType == domain.MessageHandoff {
		return domain.Message{ServerMsgID: msg.ServerMsgID, ConversationID: msg.ConversationID, HiddenFromVisitor: true}
	}
	if msg.RevokedAt != nil {
		msg.MessageType = domain.MessageRevoked
		msg.Content = revokedTextForLang(lang)
		msg.RevokedByKind = ""
		msg.RevokedByID = ""
	} else if translated := strings.TrimSpace(msg.Translations[lang]); translated != "" && translated != msg.Content {
		msg.Content = translated
	}
	msg.Translations = nil
	msg.OriginalContent = ""
	// "回答依据"是客服/后台的内部追溯信息，不下发给访客。
	msg.KnowledgeEntryIDs = nil
	return msg
}

// VisitorFacingMessagesForLang 过滤并本地化一批访客侧消息。
func VisitorFacingMessagesForLang(messages []domain.Message, lang string) []domain.Message {
	result := make([]domain.Message, 0, len(messages))
	for _, msg := range messages {
		visible := VisitorFacingMessageForLang(msg, lang)
		if visible.HiddenFromVisitor {
			continue
		}
		result = append(result, visible)
	}
	return result
}

// AgentFacingMessage 生成面向客服/后台的消息视图：默认展示中文译文，原文放入 OriginalContent 供「查看原文」。
func AgentFacingMessage(msg domain.Message) domain.Message {
	if msg.RevokedAt != nil {
		// 客服/后台侧保留撤回消息的原文与撤回信息，便于追溯。
		return msg
	}
	if translated := strings.TrimSpace(msg.Translations[ai.LangZh]); translated != "" && translated != msg.Content {
		msg.OriginalContent = msg.Content
		msg.Content = translated
	}
	msg.Translations = nil
	return msg
}

// AgentFacingMessages 批量生成客服/后台视图。
func AgentFacingMessages(messages []domain.Message) []domain.Message {
	result := make([]domain.Message, len(messages))
	for i, msg := range messages {
		result[i] = AgentFacingMessage(msg)
	}
	return result
}

func revokedTextForLang(lang string) string {
	switch lang {
	case ai.LangEn:
		return "A message was recalled."
	case ai.LangAr:
		return "تم سحب رسالة."
	default:
		return "对方撤回了一条消息"
	}
}

// localizedCopy 返回把内容替换为指定语言译文的消息副本（不修改原消息）。
func localizedCopy(msg domain.Message, lang string) domain.Message {
	if translated := strings.TrimSpace(msg.Translations[lang]); translated != "" && translated != msg.Content {
		msg.Content = translated
	}
	return msg
}
