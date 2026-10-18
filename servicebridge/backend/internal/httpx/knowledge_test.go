package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newKnowledgeUpstream 起一个 OpenAI 兼容上游桩，返回固定回复并统计命中次数。
func newKnowledgeUpstream(t *testing.T, reply string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// newKnowledgeRouter 构建可访问底层 store 的测试路由（关闭营业时间限制）。
func newKnowledgeRouter(t *testing.T) (http.Handler, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	_, _ = st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(config.Config{Env: "test"}, slog.Default(), service), st
}

// TestAdminKnowledgeCRUDAndSearch 覆盖知识库/条目 CRUD、检索预览、导入与检索配置。
func TestAdminKnowledgeCRUDAndSearch(t *testing.T) {
	router, _ := newKnowledgeRouter(t)
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	// 未登录不可访问。
	doJSON(t, router, http.MethodGet, "/api/admin/knowledge/bases", "", nil, http.StatusForbidden)

	created := doJSON(t, router, http.MethodPost, "/api/admin/knowledge/bases", adminToken, map[string]any{
		"name":        "产品知识库",
		"description": "产品资料",
		"sort_order":  1,
	}, http.StatusCreated)
	base, ok := created["base"].(map[string]any)
	if !ok {
		t.Fatalf("base 字段缺失：%#v", created)
	}
	baseID, _ := base["id"].(string)
	if baseID == "" || base["enabled"] != true {
		t.Fatalf("知识库创建结果不符：%#v", base)
	}

	list := doJSON(t, router, http.MethodGet, "/api/admin/knowledge/bases", adminToken, nil, http.StatusOK)
	if bases, _ := list["bases"].([]any); len(bases) != 1 {
		t.Fatalf("知识库列表不符：%#v", list)
	}

	patched := doJSON(t, router, http.MethodPatch, "/api/admin/knowledge/bases/"+baseID, adminToken, map[string]any{
		"name":    "产品知识库-改名",
		"enabled": true,
	}, http.StatusOK)
	if patched["base"].(map[string]any)["name"] != "产品知识库-改名" {
		t.Fatalf("知识库更新失败：%#v", patched)
	}

	entryOut := doJSON(t, router, http.MethodPost, "/api/admin/knowledge/entries", adminToken, map[string]any{
		"base_id":  baseID,
		"title":    "排水沟槽",
		"content":  "不锈钢排水沟槽支持定制长度与颜色。",
		"keywords": []string{"排水沟槽", "不锈钢"},
		"category": "产品",
		"priority": 10,
	}, http.StatusCreated)
	entry, ok := entryOut["entry"].(map[string]any)
	if !ok {
		t.Fatalf("entry 字段缺失：%#v", entryOut)
	}
	entryID, _ := entry["id"].(string)
	if entryID == "" || entry["enabled"] != true {
		t.Fatalf("知识条目创建结果不符：%#v", entry)
	}

	entries := doJSON(t, router, http.MethodGet, "/api/admin/knowledge/entries?base_id="+baseID+"&q=排水", adminToken, nil, http.StatusOK)
	if items, _ := entries["entries"].([]any); len(items) != 1 {
		t.Fatalf("条目列表不符：%#v", entries)
	}
	// 分页参数同样生效：offset 越界时返回空数组。
	paged := doJSON(t, router, http.MethodGet, "/api/admin/knowledge/entries?limit=1&offset=5", adminToken, nil, http.StatusOK)
	if items, ok := paged["entries"].([]any); !ok || len(items) != 0 {
		t.Fatalf("条目分页不符：%#v", paged)
	}

	search := doJSON(t, router, http.MethodPost, "/api/admin/knowledge/search", adminToken, map[string]any{
		"query": "排水沟槽",
	}, http.StatusOK)
	hits, _ := search["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("检索预览应命中 1 条：%#v", search)
	}
	hit := hits[0].(map[string]any)
	if hit["entry_id"] != entryID || hit["base_id"] != baseID {
		t.Fatalf("检索命中字段不符：%#v", hit)
	}
	if score, _ := hit["score"].(float64); score <= 0 {
		t.Fatalf("检索命中应带正分数：%#v", hit)
	}
	// 空 query 必须 400。
	doJSON(t, router, http.MethodPost, "/api/admin/knowledge/search", adminToken, map[string]any{"query": "  "}, http.StatusBadRequest)

	imported := doJSON(t, router, http.MethodPost, "/api/admin/knowledge/import", adminToken, map[string]any{
		"base_id": baseID,
		"entries": []map[string]any{
			{"title": "保修政策", "content": "整机保修一年。", "priority": 1},
			{"title": "发货时间", "content": "现货 48 小时内发出。"},
		},
	}, http.StatusOK)
	if imported["imported"] != float64(2) {
		t.Fatalf("导入数量不符：%#v", imported)
	}
	// 导入到不存在的库必须 404。
	doJSON(t, router, http.MethodPost, "/api/admin/knowledge/import", adminToken, map[string]any{
		"base_id": "kb_missing",
		"entries": []map[string]any{{"content": "内容"}},
	}, http.StatusNotFound)

	settings := doJSON(t, router, http.MethodGet, "/api/admin/knowledge/settings", adminToken, nil, http.StatusOK)
	if settings["unknown_action"] != domain.KnowledgeUnknownHandoff || settings["enabled"] != true {
		t.Fatalf("默认检索配置不符：%#v", settings)
	}
	updated := doJSON(t, router, http.MethodPatch, "/api/admin/knowledge/settings", adminToken, map[string]any{
		"unknown_action":  "refuse",
		"refuse_reply":    "抱歉，我暂时无法回答。",
		"score_threshold": 0.5,
		"max_entries":     5,
	}, http.StatusOK)
	if updated["unknown_action"] != "refuse" || updated["score_threshold"] != 0.5 {
		t.Fatalf("检索配置更新失败：%#v", updated)
	}
	doJSON(t, router, http.MethodPatch, "/api/admin/knowledge/settings", adminToken, map[string]any{
		"unknown_action": "unknown",
	}, http.StatusBadRequest)
	doJSON(t, router, http.MethodPatch, "/api/admin/knowledge/settings", adminToken, map[string]any{
		"score_threshold": 2.5,
	}, http.StatusBadRequest)

	// 条目局部更新与删除。
	entryPatched := doJSON(t, router, http.MethodPatch, "/api/admin/knowledge/entries/"+entryID, adminToken, map[string]any{
		"content": "不锈钢排水沟槽支持定制长度、颜色与图纸生产。",
		"enabled": false,
	}, http.StatusOK)
	if entryPatched["entry"].(map[string]any)["enabled"] != false {
		t.Fatalf("条目更新失败：%#v", entryPatched)
	}
	doJSON(t, router, http.MethodDelete, "/api/admin/knowledge/entries/"+entryID, adminToken, nil, http.StatusOK)
	doJSON(t, router, http.MethodDelete, "/api/admin/knowledge/bases/"+baseID, adminToken, nil, http.StatusOK)
	doJSON(t, router, http.MethodDelete, "/api/admin/knowledge/bases/"+baseID, adminToken, nil, http.StatusNotFound)
}

// TestAgentSuggestReplyOverHTTP 覆盖客服推荐接口：生成候选、不落库、关闭开关返回空。
func TestAgentSuggestReplyOverHTTP(t *testing.T) {
	upstream, calls := newKnowledgeUpstream(t, `["不锈钢排水沟槽支持定制长度。"]`)
	router, st := newKnowledgeRouter(t)
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           domain.AIModeManualOnly,
		BaseURL:        upstream.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		APIType:        domain.AIAPITypeChatCompletions,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	agentToken := loginAgentPreferencesFixture(t, router, "kf_knowledge")
	agents := st.AllAgents()
	agentID := ""
	for _, item := range agents {
		if item.Account == "kf_knowledge" {
			agentID = item.ID
		}
	}
	if agentID == "" {
		t.Fatal("未找到测试客服")
	}

	base, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "产品知识库", Enabled: true})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	if _, err := st.CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID: base.ID, Title: "排水沟槽", Content: "不锈钢排水沟槽支持定制长度与颜色。", Keywords: []string{"排水沟槽"}, Enabled: true,
	}); err != nil {
		t.Fatalf("create knowledge entry: %v", err)
	}

	visitor := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "test"}, http.StatusCreated)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)
	if _, err := st.AddVisitorMessage(conversationID, "vm1", "排水沟槽怎么卖？", domain.MessageText, nil); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	doJSON(t, router, http.MethodPost, "/api/admin/conversations/"+conversationID+"/transfer", adminToken, map[string]any{
		"agent_id": agentID,
	}, http.StatusOK)

	before := len(st.Messages(conversationID))
	out := doJSON(t, router, http.MethodPost, "/api/agent/suggest-reply", agentToken, map[string]any{
		"conversation_id": conversationID,
	}, http.StatusOK)
	suggestions, _ := out["suggestions"].([]any)
	if len(suggestions) != 1 {
		t.Fatalf("应返回 1 条候选：%#v", out)
	}
	first := suggestions[0].(map[string]any)
	if first["text"] != "不锈钢排水沟槽支持定制长度。" {
		t.Fatalf("候选内容不符：%#v", first)
	}
	if ids, _ := first["entry_ids"].([]any); len(ids) != 1 {
		t.Fatalf("候选应带知识条目 ID：%#v", first)
	}
	if score, _ := first["score"].(float64); score <= 0 {
		t.Fatalf("候选应带命中分数：%#v", first)
	}
	if after := len(st.Messages(conversationID)); after != before {
		t.Fatalf("推荐接口不应产生任何消息：before=%d after=%d", before, after)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("应调用一次上游，实际 %d", atomic.LoadInt32(calls))
	}

	// 关闭知识库推荐开关：返回空数组且不再调用上游。
	doJSON(t, router, http.MethodPatch, "/api/agent/preferences", agentToken, map[string]any{
		"knowledge_suggest_enabled": false,
	}, http.StatusOK)
	out = doJSON(t, router, http.MethodPost, "/api/agent/suggest-reply", agentToken, map[string]any{
		"conversation_id": conversationID,
	}, http.StatusOK)
	if suggestions, _ := out["suggestions"].([]any); len(suggestions) != 0 {
		t.Fatalf("关闭开关后应返回空数组：%#v", out)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("关闭开关后不应再调用上游，实际 %d", atomic.LoadInt32(calls))
	}

	// 管理员令牌不能调用客服接口；未知会话 404。
	doJSON(t, router, http.MethodPost, "/api/agent/suggest-reply", adminToken, map[string]any{
		"conversation_id": conversationID,
	}, http.StatusForbidden)
	doJSON(t, router, http.MethodPost, "/api/agent/suggest-reply", agentToken, map[string]any{
		"conversation_id": "conv_missing",
	}, http.StatusNotFound)
}

// TestAdminAgentPreferencesKnowledgeFields：管理员可设置客服的知识库推荐开关与可用库。
func TestAdminAgentPreferencesKnowledgeFields(t *testing.T) {
	router, st := newKnowledgeRouter(t)
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	created := doJSON(t, router, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           "kf_kb_admin",
		"password":          "AgentPass-123456",
		"name":              "知识库客服",
		"group":             "售前组",
		"max_conversations": 5,
	}, http.StatusCreated)
	agentID := created["agent"].(map[string]any)["id"].(string)

	base, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "产品知识库", Enabled: true})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}

	patched := doJSON(t, router, http.MethodPatch, "/api/admin/agents/"+agentID+"/preferences", adminToken, map[string]any{
		"knowledge_suggest_enabled": false,
		"knowledge_base_ids":        []string{base.ID},
	}, http.StatusOK)
	preferences := patched["preferences"].(map[string]any)
	if preferences["knowledge_suggest_enabled"] != false {
		t.Fatalf("推荐开关未更新：%#v", preferences)
	}
	ids, _ := preferences["knowledge_base_ids"].([]any)
	if len(ids) != 1 || ids[0] != base.ID {
		t.Fatalf("可用库未更新：%#v", preferences)
	}

	// 不存在的知识库必须 400。
	doJSON(t, router, http.MethodPatch, "/api/admin/agents/"+agentID+"/preferences", adminToken, map[string]any{
		"knowledge_base_ids": []string{"kb_missing"},
	}, http.StatusBadRequest)
}
