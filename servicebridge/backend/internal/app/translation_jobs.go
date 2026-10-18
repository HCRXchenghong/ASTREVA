package app

import (
	"strings"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
)

// translationJob 描述一次异步翻译：先把消息原文送达，译文就绪后再原地更新。
type translationJob struct {
	conversationID string
	serverMsgID    string
	sourceLang     string
	targetLang     string
	messageType    domain.MessageType
	content        string
	// styleHint 是目标语言的风格提示（方言模式），为空时按标准语翻译。
	styleHint string
	// agentID 非空表示"访客消息→客服"方向：译文就绪后通知该客服。
	agentID string
	// visitorFacing 为真表示"客服消息→访客"方向：译文就绪后投递给访客。
	visitorFacing bool
}

// staticTranslation 处理固定系统文案：命中静态映射时无需调用 AI。
func staticTranslation(sourceLang, targetLang string, messageType domain.MessageType, content string) (map[string]string, bool) {
	sourceLang = ai.NormalizeLang(sourceLang, ai.LangZh)
	targetLang = ai.NormalizeLang(targetLang, ai.LangZh)
	if sourceLang == targetLang || !isTranslatableMessageType(messageType) || strings.TrimSpace(content) == "" {
		return nil, false
	}
	if translated, ok := knownSystemText(content, targetLang); ok {
		return map[string]string{targetLang: translated}, false
	}
	return nil, true
}

// finishTranslation 异步执行翻译并更新消息与两端视图。
func (s *Service) finishTranslation(job translationJob) {
	translations := s.translateMessageWithStyle(job.sourceLang, job.targetLang, job.messageType, job.content, job.styleHint)
	status := "failed"
	if text := strings.TrimSpace(translations[job.targetLang]); text != "" {
		status = "done"
	}
	updated, err := s.store.UpdateMessageTranslations(job.conversationID, job.serverMsgID, translations)
	if err != nil {
		s.logger.Warn("store translation failed", "server_msg_id", job.serverMsgID, "error", err)
		updated, err = s.store.SetMessageLanguageMeta(job.conversationID, job.serverMsgID, job.sourceLang, job.sourceLang, status)
		if err != nil {
			return
		}
	} else if withMeta, metaErr := s.store.SetMessageLanguageMeta(job.conversationID, job.serverMsgID, job.sourceLang, job.sourceLang, status); metaErr == nil {
		updated = withMeta
	}

	if job.visitorFacing {
		// 客服消息方向：访客只看到自己语言的版本；翻译失败时兜底发送原文，保证不丢消息。
		target := ai.NormalizeLang(job.targetLang, ai.LangZh)
		s.hub.SendToVisitor(job.conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(updated, target)})
		s.hub.SendToVisitor(job.conversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "agent", "typing": false}})
		if job.agentID != "" {
			s.hub.SendToAgent(job.agentID, realtime.Event{Event: "message.receive", Data: updated})
		}
		s.hub.BroadcastAdmins(realtime.Event{Event: "message.receive", Data: updated})
		return
	}
	// 访客消息方向：客服/后台拿到中文译文（原文保留在 original_content）。
	view := AgentFacingMessage(updated)
	if job.agentID != "" {
		s.hub.SendToAgent(job.agentID, realtime.Event{Event: "message.receive", Data: view})
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "message.receive", Data: view})
}

// queueTranslation 在需要 AI 翻译时启动异步任务；不需要时返回 false。
func (s *Service) queueTranslation(job translationJob) bool {
	translations, needsAI := staticTranslation(job.sourceLang, job.targetLang, job.messageType, job.content)
	if !needsAI {
		if len(translations) > 0 {
			if updated, err := s.store.UpdateMessageTranslations(job.conversationID, job.serverMsgID, translations); err == nil {
				s.hub.BroadcastAdmins(realtime.Event{Event: "message.receive", Data: updated})
				if job.agentID != "" {
					s.hub.SendToAgent(job.agentID, realtime.Event{Event: "message.receive", Data: AgentFacingMessage(updated)})
				}
			}
		}
		return false
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("translation job panicked", "server_msg_id", job.serverMsgID, "panic", recovered)
			}
		}()
		s.finishTranslation(job)
	}()
	return true
}

// messageTranslationState 根据是否需要翻译给出初始状态，用于落库与前端展示。
func messageTranslationState(sourceLang, targetLang string, messageType domain.MessageType, content string) string {
	sourceLang = ai.NormalizeLang(sourceLang, ai.LangZh)
	targetLang = ai.NormalizeLang(targetLang, ai.LangZh)
	if sourceLang == targetLang || !isTranslatableMessageType(messageType) || strings.TrimSpace(content) == "" {
		return "skipped"
	}
	if _, ok := knownSystemText(content, targetLang); ok {
		return "done"
	}
	return "pending"
}
