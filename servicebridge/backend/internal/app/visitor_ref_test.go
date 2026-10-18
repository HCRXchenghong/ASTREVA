package app

import (
	"context"
	"strings"
	"testing"

	"customer-service/backend/internal/domain"
)

// ===== 画像键（官网 visitor_ref）链路测试 =====
//
// 覆盖：带 visitor_ref 建会话时画像立即以该 ref 为键创建；消息采集累加到同一个画像；
// 不带/非法/超长 ref 时回落引擎 visitor_id，行为与历史一致；画像关闭时不采集。

// TestVisitorRefConversationCreatesProfileImmediately 覆盖建会话即建画像（同步、visits=1）。
func TestVisitorRefConversationCreatesProfileImmediately(t *testing.T) {
	service, st := newVisitorProfileService(t)
	const ref = "av_site_ref_0001"

	session, err := service.CreateVisitorConversationWithRef("203.0.113.77", "web", "es", "direct", "", ref)
	if err != nil {
		t.Fatalf("带 visitor_ref 建会话失败：%v", err)
	}
	if session.Conversation.ProfileVisitorID != ref {
		t.Fatalf("会话画像键 = %q，want %q", session.Conversation.ProfileVisitorID, ref)
	}
	if session.Conversation.ProfileKey() != ref {
		t.Fatalf("ProfileKey() = %q，want %q", session.Conversation.ProfileKey(), ref)
	}
	// 同步 upsert：响应返回时画像已存在且 visits=1（不依赖异步采集）。
	profile, ok := st.VisitorProfile(ref)
	if !ok {
		t.Fatal("带 visitor_ref 建会话后画像应立即存在")
	}
	if profile.Visits != 1 {
		t.Fatalf("建会话后 visits = %d，want 1", profile.Visits)
	}
	if profile.LastSeenAt == nil {
		t.Fatal("建会话后 last_seen_at 应已写入")
	}
	// 引擎 visitor_id 不应产生第二个画像。
	if _, ok := st.VisitorProfile(session.Conversation.VisitorID); ok {
		t.Fatalf("画像不应落在引擎 visitor_id 上：%s", session.Conversation.VisitorID)
	}
}

// TestVisitorRefTrackVisitorMessageUsesSameProfile 覆盖消息采集累加到 ref 键画像。
func TestVisitorRefTrackVisitorMessageUsesSameProfile(t *testing.T) {
	service, st := newVisitorProfileService(t)
	const ref = "av_site_ref_0002"

	session, err := service.CreateVisitorConversationWithRef("203.0.113.78", "web", "es", "direct", "", ref)
	if err != nil {
		t.Fatalf("带 visitor_ref 建会话失败：%v", err)
	}
	message := storeVisitorMessage(t, st, session.Conversation.ID, "pf_ref_msg_1", "¿Cuánto cuesta el panel solar?", "es")
	service.trackVisitorMessage(session.Conversation, message, "es")

	profile := waitForVisitorProfile(t, st, ref, func(profile domain.VisitorProfile) bool {
		return profile.Languages["es"] >= 1
	})
	if profile.Visits != 1 {
		t.Fatalf("消息采集不应再次累计 visits：%d", profile.Visits)
	}
	if _, ok := st.VisitorProfile(session.Conversation.VisitorID); ok {
		t.Fatal("消息采集不应在引擎 visitor_id 上建画像")
	}
}

// TestVisitorRefMissingOrInvalidFallsBack 覆盖缺失/非法/超长 ref 一律忽略且不改行为。
func TestVisitorRefMissingOrInvalidFallsBack(t *testing.T) {
	service, st := newVisitorProfileService(t)

	cases := []struct {
		name string
		ref  string
	}{
		{name: "缺失", ref: ""},
		{name: "含非法字符", ref: "av bad/ref!!"},
		{name: "超长", ref: strings.Repeat("a", 65)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			session, err := service.CreateVisitorConversationWithRef("203.0.113.79", "web", "zh", "direct", "", item.ref)
			if err != nil {
				t.Fatalf("建会话不应因 visitor_ref 非法而失败：%v", err)
			}
			if session.Conversation.ProfileVisitorID != "" {
				t.Fatalf("非法 visitor_ref 应被清洗为空串：%q", session.Conversation.ProfileVisitorID)
			}
			if session.Conversation.ProfileKey() != session.Conversation.VisitorID {
				t.Fatalf("画像键应回落引擎 visitor_id：%q", session.Conversation.ProfileKey())
			}
			if item.ref != "" {
				if _, ok := st.VisitorProfile(item.ref); ok {
					t.Fatalf("非法 visitor_ref 不应创建画像：%q", item.ref)
				}
			}
		})
	}
}

// TestVisitorRefDisabledProfileNotCollected 覆盖画像关闭时带 ref 也不采集。
func TestVisitorRefDisabledProfileNotCollected(t *testing.T) {
	service, st := newVisitorProfileService(t)
	disabled := false
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("关闭画像失败：%v", err)
	}
	const ref = "av_site_ref_disabled"
	session, err := service.CreateVisitorConversationWithRef("203.0.113.80", "web", "zh", "direct", "", ref)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	service.trackVisitorVisit(session, nil)
	service.trackVisitorMessage(session.Conversation, domain.Message{
		ServerMsgID: "pf_ref_disabled", ConversationID: session.Conversation.ID,
		SenderType: domain.SenderVisitor, MessageType: domain.MessageText, Content: "你好",
	}, "zh")
	if _, ok := st.VisitorProfile(ref); ok {
		t.Fatal("画像关闭时不应采集任何画像")
	}
	if _, ok := st.VisitorProfile(session.Conversation.VisitorID); ok {
		t.Fatal("画像关闭时不应在引擎 visitor_id 上采集画像")
	}
}

// TestVisitorRefConversationLookupByProfileKey 覆盖按画像键反查会话（画像详情/语言窗口依赖）。
func TestVisitorRefConversationLookupByProfileKey(t *testing.T) {
	service, st := newVisitorProfileService(t)
	const ref = "av_site_ref_0003"
	session, err := service.CreateVisitorConversationWithRef("203.0.113.81", "web", "ar", "direct", "", ref)
	if err != nil {
		t.Fatalf("带 visitor_ref 建会话失败：%v", err)
	}
	// 同步 upsert 后直接读详情：会话列表必须能按 ref 找回该会话。
	if _, _, err := service.applyVisitorMessage(context.Background(), st.VisitorProfileSettings(), session.Conversation,
		storeVisitorMessage(t, st, session.Conversation.ID, "pf_ref_msg_2", "كم السعر؟", "ar"), "ar"); err != nil {
		t.Fatalf("画像消息更新失败：%v", err)
	}
	conversations := st.ConversationsForVisitor(ref)
	if len(conversations) != 1 || conversations[0].ID != session.Conversation.ID {
		t.Fatalf("按画像键反查会话不符：%+v", conversations)
	}
}
