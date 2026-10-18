package app

import (
	"sort"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

// ===== 管理员策略告警（供控制台轮询）=====
//
// 与 SLA 巡检（RunPolicySweep）的区别：巡检是"有副作用"的状态机（写 sla_state、派人、发通知），
// 本接口只做只读汇总，把需要人工介入的会话按紧急度排好序返回给控制台。

// 告警类型。
const (
	// PolicyAlertTakeover 表示等待人工接管/首响的会话（waiting / human_requested 且尚无人工首响）。
	PolicyAlertTakeover = "takeover"
	// PolicyAlertSLAFirstResponse 表示已就首响超时告警过的会话（sla_state=first_response）。
	PolicyAlertSLAFirstResponse = "sla_first_response"
	// PolicyAlertSLAIdle 表示已就待回复超时告警过的会话（sla_state=idle）。
	PolicyAlertSLAIdle = "sla_idle"
)

// PolicyAlert 是一条策略告警。除 conversation_id 与 kind 外，其余字段按类型与数据可选。
type PolicyAlert struct {
	ConversationID string `json:"conversation_id"`
	Kind           string `json:"kind"`
	// Minutes 由策略阈值算出（首响阈值秒数向上取整为分钟；空闲阈值本身即分钟）。
	Minutes int `json:"minutes,omitempty"`
	// VisitorName 为访客备注，缺省回退「访客」。
	VisitorName string `json:"visitor_name,omitempty"`
	// QueuePosition 是排队位次（1-based，按 priority DESC, enqueued_at ASC）；不在队列中则为 0。
	QueuePosition int `json:"queue_position,omitempty"`
	// WaitingSeconds 是等待时长：队列中取入队时间，否则取首响计时起点（最后一条访客消息/建会话时间）。
	WaitingSeconds int `json:"waiting_seconds,omitempty"`
	// AssignedAgentName 是负责客服名（未分配为空）。
	AssignedAgentName string `json:"assigned_agent_name,omitempty"`
	// Status 是会话状态（waiting / human_requested / assigned / ai_serving 等）。
	Status string `json:"status,omitempty"`
}

// PolicyAlerts 汇总当前需要人工介入的会话，按紧急度排序：
// takeover 优先，其次 sla_idle，再 sla_first_response；同级按等待/超时时间倒序，最后按会话 ID 升序稳定排序。
// 已结束的会话不产生告警。
func (s *Service) PolicyAlerts(now time.Time) []PolicyAlert {
	now = now.UTC()
	policy := s.store.ServicePolicy()

	// 排队位次与等待时长：QueueList 已按 priority DESC, enqueued_at ASC 排序，
	// 下标 +1 即 1-based 位次（同会话重复入队不会出现，队列以会话 ID 为键）。
	queue := s.store.QueueList()
	queuePosition := make(map[string]int, len(queue))
	queueWaiting := make(map[string]int, len(queue))
	for index, entry := range queue {
		conversationID := strings.TrimSpace(entry.ConversationID)
		if conversationID == "" {
			continue
		}
		if _, exists := queuePosition[conversationID]; exists {
			continue
		}
		queuePosition[conversationID] = index + 1
		queueWaiting[conversationID] = nonNegativeSeconds(now.Sub(entry.EnqueuedAt))
	}

	agentNames := make(map[string]string)
	for _, agent := range s.store.AllAgents() {
		agentNames[agent.ID] = agent.Name
	}

	alerts := make([]PolicyAlert, 0, 8)
	for _, conversation := range s.store.AllConversations() {
		if conversation.Status == domain.ConversationClosed {
			continue
		}
		// 首响计时起点 = 分配时间（近似建会话时间）与最后一条访客消息的较晚者。
		visitorAt := lastVisitorMessageAt(s.store.Messages(conversation.ID))
		waiting := nonNegativeSeconds(now.Sub(firstResponseBase(conversation, visitorAt)))

		if isTakeoverPending(conversation) {
			alert := PolicyAlert{
				ConversationID:    conversation.ID,
				Kind:              PolicyAlertTakeover,
				VisitorName:       visitorDisplayName(conversation),
				WaitingSeconds:    waiting,
				AssignedAgentName: policyAlertAgentName(conversation, agentNames),
				Status:            string(conversation.Status),
			}
			if position, ok := queuePosition[conversation.ID]; ok {
				alert.QueuePosition = position
				alert.WaitingSeconds = queueWaiting[conversation.ID]
			}
			alerts = append(alerts, alert)
		}

		switch conversation.SLAState {
		case domain.ConversationSLAStateFirstResponse:
			alerts = append(alerts, PolicyAlert{
				ConversationID:    conversation.ID,
				Kind:              PolicyAlertSLAFirstResponse,
				Minutes:           minutesFromSeconds(policy.SLAFirstResponseSeconds),
				VisitorName:       visitorDisplayName(conversation),
				WaitingSeconds:    waiting,
				AssignedAgentName: policyAlertAgentName(conversation, agentNames),
				Status:            string(conversation.Status),
			})
		case domain.ConversationSLAStateIdle:
			alerts = append(alerts, PolicyAlert{
				ConversationID:    conversation.ID,
				Kind:              PolicyAlertSLAIdle,
				Minutes:           policy.SLAIdleMinutes,
				VisitorName:       visitorDisplayName(conversation),
				WaitingSeconds:    waiting,
				AssignedAgentName: policyAlertAgentName(conversation, agentNames),
				Status:            string(conversation.Status),
			})
		}
	}

	sort.SliceStable(alerts, func(i, j int) bool {
		left, right := policyAlertUrgency(alerts[i].Kind), policyAlertUrgency(alerts[j].Kind)
		if left != right {
			return left < right
		}
		if alerts[i].WaitingSeconds != alerts[j].WaitingSeconds {
			return alerts[i].WaitingSeconds > alerts[j].WaitingSeconds
		}
		return alerts[i].ConversationID < alerts[j].ConversationID
	})
	return alerts
}

// isTakeoverPending 判断会话是否在等待人工接管：状态为 waiting/human_requested 且尚无人工首响。
func isTakeoverPending(conversation domain.Conversation) bool {
	if conversation.FirstResponseAt != nil {
		return false
	}
	switch conversation.Status {
	case domain.ConversationWaiting, domain.ConversationHumanRequested:
		return true
	default:
		return false
	}
}

// policyAlertUrgency 是排序用的紧急度权重：takeover 最紧急，其次 idle，最后 first_response。
func policyAlertUrgency(kind string) int {
	switch kind {
	case PolicyAlertTakeover:
		return 0
	case PolicyAlertSLAIdle:
		return 1
	case PolicyAlertSLAFirstResponse:
		return 2
	default:
		return 3
	}
}

// policyAlertAgentName 取负责客服名：会话上的派生字段优先，缺失时按 ID 查一次。
func policyAlertAgentName(conversation domain.Conversation, agentNames map[string]string) string {
	if name := strings.TrimSpace(conversation.AssignedAgentName); name != "" {
		return name
	}
	return agentNames[conversation.AssignedAgentID]
}

// nonNegativeSeconds 把时长换算成秒，负数（时钟回拨）归零。
func nonNegativeSeconds(duration time.Duration) int {
	seconds := int(duration.Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

// minutesFromSeconds 把秒向上取整为分钟（至少 1 分钟），用于把首响阈值展示成分钟。
func minutesFromSeconds(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}
