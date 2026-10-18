package store

import (
	"errors"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestMemoryKnowledgeBaseAndEntryCRUD 覆盖知识库/条目的增删改查与级联删除。
func TestMemoryKnowledgeBaseAndEntryCRUD(t *testing.T) {
	s := NewMemoryStore(Options{})

	if bases := s.KnowledgeBases(); len(bases) != 0 {
		t.Fatalf("初始知识库应为空：%+v", bases)
	}

	base, err := s.CreateKnowledgeBase(domain.KnowledgeBase{Name: "产品知识库", Description: "产品资料", Enabled: true, SortOrder: 2})
	if err != nil {
		t.Fatalf("创建知识库失败：%v", err)
	}
	if base.ID == "" || base.CreatedAt.IsZero() {
		t.Fatalf("知识库 ID/创建时间未填充：%+v", base)
	}
	if _, err := s.CreateKnowledgeBase(domain.KnowledgeBase{Name: "   "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空名称应返回 ErrInvalidInput，实际 %v", err)
	}

	other, err := s.CreateKnowledgeBase(domain.KnowledgeBase{Name: "售后知识库", Enabled: true, SortOrder: 1})
	if err != nil {
		t.Fatalf("创建第二个知识库失败：%v", err)
	}
	bases := s.KnowledgeBases()
	if len(bases) != 2 || bases[0].ID != other.ID {
		t.Fatalf("知识库应按 sort_order 排序：%+v", bases)
	}

	renamed := base
	renamed.Name = "产品知识库-改名"
	renamed.Description = "新描述"
	renamed.Enabled = false
	updated, err := s.UpdateKnowledgeBase(base.ID, renamed)
	if err != nil {
		t.Fatalf("更新知识库失败：%v", err)
	}
	if updated.Name != "产品知识库-改名" || updated.Enabled || updated.CreatedAt != base.CreatedAt {
		t.Fatalf("知识库更新结果不符合预期：%+v", updated)
	}

	entry, err := s.CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID:   base.ID,
		Title:    "排水沟槽",
		Content:  "不锈钢排水沟槽支持定制长度。",
		Keywords: []string{"排水沟槽", "排水沟槽", " 不锈钢 "},
		Category: "产品",
		Priority: 10,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("创建知识条目失败：%v", err)
	}
	if len(entry.Keywords) != 2 || entry.Keywords[0] != "排水沟槽" || entry.Keywords[1] != "不锈钢" {
		t.Fatalf("关键词应去空去重：%+v", entry.Keywords)
	}
	if _, err := s.CreateKnowledgeEntry(domain.KnowledgeEntry{BaseID: base.ID, Content: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空正文应返回 ErrInvalidInput，实际 %v", err)
	}
	if _, err := s.CreateKnowledgeEntry(domain.KnowledgeEntry{BaseID: "kb_missing", Content: "内容"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("所属库不存在应返回 ErrNotFound，实际 %v", err)
	}

	second, err := s.CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID: other.ID, Title: "保修政策", Content: "整机保修一年。", Priority: 5, Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建第二条知识条目失败：%v", err)
	}

	all := s.KnowledgeEntries("", "", 0, 0)
	if len(all) != 2 || all[0].ID != entry.ID {
		t.Fatalf("后台列表应按优先级降序：%+v", all)
	}
	byBase := s.KnowledgeEntries(other.ID, "", 0, 0)
	if len(byBase) != 1 || byBase[0].ID != second.ID {
		t.Fatalf("按库过滤失败：%+v", byBase)
	}
	byQuery := s.KnowledgeEntries("", "排水沟槽", 0, 0)
	if len(byQuery) != 1 || byQuery[0].ID != entry.ID {
		t.Fatalf("关键词过滤失败：%+v", byQuery)
	}
	if paged := s.KnowledgeEntries("", "", 1, 1); len(paged) != 1 || paged[0].ID != second.ID {
		t.Fatalf("分页失败：%+v", paged)
	}

	patched := entry
	patched.Content = "不锈钢排水沟槽支持定制长度与颜色。"
	patched.Enabled = false
	updatedEntry, err := s.UpdateKnowledgeEntry(entry.ID, patched)
	if err != nil {
		t.Fatalf("更新知识条目失败：%v", err)
	}
	if updatedEntry.Enabled || updatedEntry.CreatedAt != entry.CreatedAt {
		t.Fatalf("条目更新结果不符合预期：%+v", updatedEntry)
	}

	// 检索候选只包含启用中的条目与启用中的库。
	if candidates := s.KnowledgeEntriesForBases(nil); len(candidates) != 1 || candidates[0].ID != second.ID {
		t.Fatalf("候选集应只含启用条目：%+v", candidates)
	}

	deleted, err := s.DeleteKnowledgeEntry(second.ID)
	if err != nil || deleted.ID != second.ID {
		t.Fatalf("删除条目失败：%v %+v", err, deleted)
	}
	if _, err := s.DeleteKnowledgeEntry(second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}

	// 删除知识库应级联删除其条目。
	if _, err := s.DeleteKnowledgeBase(base.ID); err != nil {
		t.Fatalf("删除知识库失败：%v", err)
	}
	if _, ok := s.KnowledgeEntry(entry.ID); ok {
		t.Fatal("删除知识库后条目应被级联删除")
	}
	if _, ok := s.KnowledgeBase(base.ID); ok {
		t.Fatal("删除后仍能读到知识库")
	}
	if _, err := s.DeleteKnowledgeBase(base.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除知识库应返回 ErrNotFound，实际 %v", err)
	}
}

// TestMemoryKnowledgeEntriesForBasesRestriction 校验按客服限定知识库的检索范围。
func TestMemoryKnowledgeEntriesForBasesRestriction(t *testing.T) {
	s := NewMemoryStore(Options{})
	first, err := s.CreateKnowledgeBase(domain.KnowledgeBase{Name: "库一", Enabled: true})
	if err != nil {
		t.Fatalf("创建知识库失败：%v", err)
	}
	second, err := s.CreateKnowledgeBase(domain.KnowledgeBase{Name: "库二", Enabled: true})
	if err != nil {
		t.Fatalf("创建知识库失败：%v", err)
	}
	disabled, err := s.CreateKnowledgeBase(domain.KnowledgeBase{Name: "已停用库", Enabled: false})
	if err != nil {
		t.Fatalf("创建知识库失败：%v", err)
	}
	for _, baseID := range []string{first.ID, second.ID, disabled.ID} {
		if _, err := s.CreateKnowledgeEntry(domain.KnowledgeEntry{BaseID: baseID, Content: "排水沟槽", Enabled: true}); err != nil {
			t.Fatalf("创建条目失败：%v", err)
		}
	}

	// 空 = 全部启用库（停用库不参与）。
	if got := s.KnowledgeEntriesForBases(nil); len(got) != 2 {
		t.Fatalf("空参数应返回全部启用库条目，实际 %d", len(got))
	}
	// 限定单库。
	if got := s.KnowledgeEntriesForBases([]string{first.ID}); len(got) != 1 || got[0].BaseID != first.ID {
		t.Fatalf("限定单库失败：%+v", got)
	}
	// 指定了无效/停用库时不得回退到全部库。
	if got := s.KnowledgeEntriesForBases([]string{disabled.ID}); len(got) != 0 {
		t.Fatalf("停用库不应返回条目：%+v", got)
	}
	if got := s.KnowledgeEntriesForBases([]string{"kb_missing"}); len(got) != 0 {
		t.Fatalf("不存在的库不应返回条目：%+v", got)
	}
}

// TestMemoryKnowledgeSettingsDefaultsAndUpdate 校验配置默认值与非法取值拒绝。
func TestMemoryKnowledgeSettingsDefaultsAndUpdate(t *testing.T) {
	s := NewMemoryStore(Options{})
	settings := s.KnowledgeSettings()
	if settings.UnknownAction != domain.KnowledgeUnknownHandoff {
		t.Fatalf("默认库外处理应为 handoff，实际 %q", settings.UnknownAction)
	}
	if settings.RefuseReply != domain.DefaultKnowledgeRefuseReply {
		t.Fatalf("默认拒答文案不符：%q", settings.RefuseReply)
	}
	if settings.ScoreThreshold != domain.DefaultKnowledgeScoreThreshold || settings.MaxEntries != domain.DefaultKnowledgeMaxEntries || !settings.Enabled {
		t.Fatalf("默认配置不符：%+v", settings)
	}

	if _, err := s.UpdateKnowledgeSettings(domain.KnowledgeSettings{UnknownAction: "unknown"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法 unknown_action 应被拒绝，实际 %v", err)
	}
	if _, err := s.UpdateKnowledgeSettings(domain.KnowledgeSettings{UnknownAction: "refuse", ScoreThreshold: 2}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法 score_threshold 应被拒绝，实际 %v", err)
	}

	updated, err := s.UpdateKnowledgeSettings(domain.KnowledgeSettings{
		UnknownAction:  domain.KnowledgeUnknownRefuse,
		RefuseReply:    "抱歉，我暂时无法回答这个问题。",
		ScoreThreshold: 0.5,
		MaxEntries:     5,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("更新知识库配置失败：%v", err)
	}
	if updated.UnknownAction != domain.KnowledgeUnknownRefuse || updated.ScoreThreshold != 0.5 || updated.MaxEntries != 5 {
		t.Fatalf("配置更新结果不符：%+v", updated)
	}
	if updated.UpdatedAt == nil {
		t.Fatal("更新时间未填充")
	}
	if again := s.KnowledgeSettings(); again.RefuseReply != "抱歉，我暂时无法回答这个问题。" {
		t.Fatalf("配置未持久化：%+v", again)
	}
}

// TestMemoryRequestHumanHandoff 校验静默转人工：改状态 + 分配客服 + 不产生消息。
func TestMemoryRequestHumanHandoff(t *testing.T) {
	s := NewMemoryStore(Options{})
	openBusinessForTest(t, s)
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.61", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	before := len(s.Messages(conversationID))

	conversation, err := s.RequestHumanHandoff(conversationID)
	if err != nil {
		t.Fatalf("request human handoff: %v", err)
	}
	if conversation.Status != domain.ConversationHumanRequested {
		t.Fatalf("状态应为 human_requested，实际 %s", conversation.Status)
	}
	if conversation.AssignedAgentID != agent.ID {
		t.Fatalf("应静默分配在线客服，实际 %q", conversation.AssignedAgentID)
	}
	if after := len(s.Messages(conversationID)); after != before {
		t.Fatalf("转人工不应产生消息：before=%d after=%d", before, after)
	}
	if _, err := s.RequestHumanHandoff("conv_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的会话应返回 ErrNotFound，实际 %v", err)
	}
}

// TestMemoryAddAIMessageWithKnowledge 校验 knowledge_entry_ids 的读写往返。
func TestMemoryAddAIMessageWithKnowledge(t *testing.T) {
	s := NewMemoryStore(Options{})
	if _, err := s.UpdateAISettings(domain.AISettings{Enabled: true, Mode: domain.AIModeAlwaysAI}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.62", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID

	msg, _, err := s.AddAIMessageWithKnowledge(conversationID, "依据资料回答", nil, []string{"ke_1", "ke_1", "ke_2"})
	if err != nil {
		t.Fatalf("add ai message: %v", err)
	}
	if len(msg.KnowledgeEntryIDs) != 2 || msg.KnowledgeEntryIDs[0] != "ke_1" {
		t.Fatalf("回答依据应去重：%+v", msg.KnowledgeEntryIDs)
	}
	stored := s.Messages(conversationID)
	last := stored[len(stored)-1]
	if len(last.KnowledgeEntryIDs) != 2 || last.KnowledgeEntryIDs[1] != "ke_2" {
		t.Fatalf("落库的回答依据不符：%+v", last.KnowledgeEntryIDs)
	}

	// 未携带知识依据时字段为空，保持既有行为。
	plain, _, err := s.AddAIMessage(conversationID, "普通回答", nil)
	if err != nil {
		t.Fatalf("add plain ai message: %v", err)
	}
	if len(plain.KnowledgeEntryIDs) != 0 {
		t.Fatalf("普通回答不应带回答依据：%+v", plain.KnowledgeEntryIDs)
	}
}
