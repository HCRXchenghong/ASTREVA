package app

import "customer-service/backend/internal/domain"

func VisitorFacingMessage(msg domain.Message) domain.Message {
	if msg.RevokedAt == nil {
		return msg
	}
	msg.MessageType = domain.MessageRevoked
	msg.Content = "对方撤回了一条消息"
	msg.RevokedByKind = ""
	msg.RevokedByID = ""
	return msg
}

func VisitorFacingMessages(messages []domain.Message) []domain.Message {
	result := make([]domain.Message, len(messages))
	for i, msg := range messages {
		result[i] = VisitorFacingMessage(msg)
	}
	return result
}
