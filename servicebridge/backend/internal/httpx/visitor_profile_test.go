package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ===== 访客画像（P5）HTTP 契约测试 =====

// newVisitorProfileRouter 构建可访问底层存储的画像测试路由。
func newVisitorProfileRouter(t *testing.T) (http.Handler, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	_, _ = st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(config.Config{Env: "test"}, slog.Default(), service), st
}

// seedVisitorProfile 写入一条画像（绕过采集链路，直接构造列表/详情数据）。
func seedVisitorProfile(t *testing.T, st store.Store, visitorID string, mutate func(*domain.VisitorProfile)) {
	t.Helper()
	if _, err := st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		mutate(profile)
		return nil
	}); err != nil {
		t.Fatalf("写入画像 %s 失败：%v", visitorID, err)
	}
}

// settingsOf 从响应里取出 settings 对象。
func settingsOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	settings, ok := payload["settings"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 settings：%#v", payload)
	}
	return settings
}

// findProfileInList 在列表响应里按访客 ID 找到画像对象（列表顺序按更新时间倒序，不假设下标）。
func findProfileInList(t *testing.T, profiles []any, visitorID string) map[string]any {
	t.Helper()
	for _, item := range profiles {
		profile, _ := item.(map[string]any)
		if profile["visitor_id"] == visitorID {
			return profile
		}
	}
	t.Fatalf("列表里找不到访客 %s：%#v", visitorID, profiles)
	return nil
}

// TestAdminVisitorProfileSettings 覆盖配置读写、双键名、密钥掩码语义与越权。
func TestAdminVisitorProfileSettings(t *testing.T) {
	router, _ := newVisitorProfileRouter(t)

	// 未登录与非管理员一律 403。
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profile/settings", "", nil, http.StatusForbidden)
	agentToken := loginForToken(t, router, "/api/agent/login", "admin", "123456")
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profile/settings", agentToken, nil, http.StatusForbidden)
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", agentToken,
		map[string]any{"enabled": true}, http.StatusForbidden)

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	initial := settingsOf(t, doJSON(t, router, http.MethodGet, "/api/admin/visitor-profile/settings", adminToken, nil, http.StatusOK))
	// 契约键名必须是 geoip_db_path（不是 geoip_dir），且不泄漏明文密钥。
	if _, ok := initial["geoip_db_path"]; !ok {
		t.Fatalf("响应缺少 geoip_db_path：%#v", initial)
	}
	if _, leaked := initial["third_party_key"]; leaked {
		t.Fatalf("响应不应包含明文密钥：%#v", initial)
	}
	if initial["enabled"] != true || initial["show_full_ip"] != true {
		t.Fatalf("默认开关不符：%#v", initial)
	}

	// 用控制台契约键 geoip_db_path 写入。
	patched := settingsOf(t, doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{
			"geoip_db_path":             "/data/geolite2",
			"third_party_url":           "https://ip.example.com/{ip}",
			"third_party_key":           "secret-key-9876",
			"third_party_timeout_ms":    1500,
			"cache_ttl_hours":           12,
			"ai_summary_every_messages": 5,
			"ai_summary_enabled":        true,
			"show_full_ip":              false,
			"enabled":                   true,
		}, http.StatusOK))
	if patched["geoip_db_path"] != "/data/geolite2" {
		t.Fatalf("geoip_db_path 未保存：%#v", patched)
	}
	if patched["third_party_key_masked"] != "****9876" {
		t.Fatalf("密钥掩码不符：%#v", patched["third_party_key_masked"])
	}
	if patched["show_full_ip"] != false || patched["cache_ttl_hours"] != float64(12) {
		t.Fatalf("局部更新结果不符：%#v", patched)
	}

	// 别名键 geoip_dir 同样接受。
	alias := settingsOf(t, doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"geoip_dir": "/data/geolite2-alt"}, http.StatusOK))
	if alias["geoip_db_path"] != "/data/geolite2-alt" {
		t.Fatalf("geoip_dir 别名未生效：%#v", alias)
	}

	// 不传 third_party_key = 不改；传 "" = 清除。
	unchanged := settingsOf(t, doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"cache_ttl_hours": 6}, http.StatusOK))
	if unchanged["third_party_key_masked"] != "****9876" {
		t.Fatalf("不传密钥不应清除：%#v", unchanged["third_party_key_masked"])
	}
	cleared := settingsOf(t, doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"third_party_key": ""}, http.StatusOK))
	if cleared["third_party_key_masked"] != "" {
		t.Fatalf("空串应清除密钥：%#v", cleared["third_party_key_masked"])
	}

	// 非法取值 400。
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"third_party_timeout_ms": 10}, http.StatusBadRequest)
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"cache_ttl_hours": 0}, http.StatusBadRequest)
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"ai_summary_every_messages": 0}, http.StatusBadRequest)
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"third_party_url": "https://ip.example.com/lookup"}, http.StatusBadRequest)
}

// TestAdminVisitorProfilesListDetailAndRefresh 覆盖列表筛选、分页、详情、refresh 与关闭语义。
func TestAdminVisitorProfilesListDetailAndRefresh(t *testing.T) {
	router, st := newVisitorProfileRouter(t)
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	// 未登录 / 客服 token：403。
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles", "", nil, http.StatusForbidden)
	agentToken := loginForToken(t, router, "/api/agent/login", "admin", "123456")
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles", agentToken, nil, http.StatusForbidden)

	seedVisitorProfile(t, st, "vis_http_a", func(profile *domain.VisitorProfile) {
		profile.Country = "沙特阿拉伯"
		profile.CountryCode = "SA"
		profile.City = "利雅得"
		profile.Timezone = "Asia/Riyadh"
		profile.NationalityGuess = "SA"
		profile.NationalityConfidence = 0.8
		profile.NationalityBasis = []string{domain.GeoBasisCDN, domain.GeoBasisTimezone}
		profile.Languages = map[string]int{"ar": 12, "en": 3}
		profile.Dialect = "ar-Gulf"
		profile.CodeSwitching = true
		profile.Summary = "咨询太阳能项目报价"
		profile.Tags = []string{"高意向"}
		profile.Interests = []string{"太阳能"}
		profile.Evidence = []domain.VisitorProfileEvidence{{MessageID: "msg_ev_1", Text: "كم السعر؟"}}
		profile.Visits = 2
		profile.LastPath = "/ar/pricing"
		profile.IPLast = "203.0.113.9"
	})
	seedVisitorProfile(t, st, "vis_http_b", func(profile *domain.VisitorProfile) {
		profile.Country = "美国"
		profile.CountryCode = "US"
		profile.City = "山景城"
		profile.Summary = "咨询 API 文档"
		profile.Tags = []string{"技术"}
		profile.Languages = map[string]int{"en": 4}
		profile.IPLast = "198.51.100.7"
	})

	// 列表：契约字段齐全。
	list := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles", adminToken, nil, http.StatusOK)
	if total, _ := list["total"].(float64); total != 2 {
		t.Fatalf("total = %v，want 2", list["total"])
	}
	profiles, ok := list["profiles"].([]any)
	if !ok || len(profiles) != 2 {
		t.Fatalf("profiles 结构不符：%#v", list["profiles"])
	}
	first := findProfileInList(t, profiles, "vis_http_a")
	for _, key := range []string{"visitor_id", "country", "region", "city", "timezone",
		"nationality_guess", "nationality_confidence", "nationality_basis", "languages",
		"dialect", "code_switching", "summary", "tags", "interests", "visits", "last_path",
		"last_seen_at", "ip_last"} {
		if _, exists := first[key]; !exists {
			t.Fatalf("列表项缺少契约字段 %q：%#v", key, first)
		}
	}
	if first["country"] != "沙特阿拉伯" || first["nationality_guess"] != "SA" {
		t.Fatalf("列表字段值不符：%#v", first)
	}
	if basis, _ := first["nationality_basis"].([]any); len(basis) != 2 || basis[0] != domain.GeoBasisCDN {
		t.Fatalf("nationality_basis 不符：%#v", first["nationality_basis"])
	}

	// 筛选：国家码 / 标签 / 关键词。
	filtered := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles?country=SA", adminToken, nil, http.StatusOK)
	if total, _ := filtered["total"].(float64); total != 1 {
		t.Fatalf("country 筛选 total = %v，want 1", filtered["total"])
	}
	byTag := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles?tag=%E6%8A%80%E6%9C%AF", adminToken, nil, http.StatusOK)
	if total, _ := byTag["total"].(float64); total != 1 {
		t.Fatalf("tag 筛选 total = %v，want 1", byTag["total"])
	}
	byQuery := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles?q=198.51.100.7", adminToken, nil, http.StatusOK)
	if total, _ := byQuery["total"].(float64); total != 1 {
		t.Fatalf("q 筛选 total = %v，want 1", byQuery["total"])
	}

	// 分页：limit/offset 生效，total 不受分页影响。
	page := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles?limit=1", adminToken, nil, http.StatusOK)
	if total, _ := page["total"].(float64); total != 2 {
		t.Fatalf("分页 total = %v，want 2", page["total"])
	}
	pageItems, _ := page["profiles"].([]any)
	if len(pageItems) != 1 {
		t.Fatalf("limit=1 应只返回 1 条：%#v", page["profiles"])
	}
	pageFirstID, _ := pageItems[0].(map[string]any)["visitor_id"].(string)
	page2 := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles?limit=1&offset=1", adminToken, nil, http.StatusOK)
	second, _ := page2["profiles"].([]any)
	if len(second) != 1 {
		t.Fatalf("offset=1 应返回 1 条：%#v", page2["profiles"])
	}
	secondID, _ := second[0].(map[string]any)["visitor_id"].(string)
	if secondID == "" || secondID == pageFirstID {
		t.Fatalf("offset=1 应返回另一条：first=%q second=%q", pageFirstID, secondID)
	}
	// 详情：evidence 与 conversations 字段存在。
	detail := doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles/vis_http_a", adminToken, nil, http.StatusOK)
	profile, _ := detail["profile"].(map[string]any)
	if profile == nil {
		t.Fatalf("详情缺少 profile：%#v", detail)
	}
	evidence, _ := profile["evidence"].([]any)
	if len(evidence) != 1 {
		t.Fatalf("evidence 不符：%#v", profile["evidence"])
	}
	entry, _ := evidence[0].(map[string]any)
	if entry["message_id"] != "msg_ev_1" || entry["text"] != "كم السعر؟" {
		t.Fatalf("evidence 内容不符：%#v", entry)
	}
	if _, exists := profile["conversations"]; !exists {
		t.Fatalf("详情缺少 conversations：%#v", profile)
	}
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles/vis_not_exists", adminToken, nil, http.StatusNotFound)

	// refresh：用第三方 IP 库桩重算地理，验证"立即重算"生效。
	stub, calls := newVisitorGeoHTTPStub(t)
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"third_party_url": stub.URL + "/ip/{ip}"}, http.StatusOK)
	refreshed := doJSON(t, router, http.MethodPost, "/api/admin/visitor-profiles/vis_http_a/refresh", adminToken, map[string]any{}, http.StatusOK)
	updated, _ := refreshed["profile"].(map[string]any)
	if updated["country"] != "Saudi Arabia" || updated["country_code"] != "SA" {
		t.Fatalf("refresh 未重算地理：%#v", updated)
	}
	if updated["nationality_guess"] != "SA" {
		t.Fatalf("refresh 未重算国籍：%#v", updated["nationality_guess"])
	}
	if got := atomic.LoadInt32(calls); got == 0 {
		t.Fatal("refresh 应请求第三方 IP 库")
	}
	doJSON(t, router, http.MethodPost, "/api/admin/visitor-profiles/vis_not_exists/refresh", adminToken, map[string]any{}, http.StatusNotFound)

	// enabled=false：列表/详情/refresh 一律 404（不采集不返回）。
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"enabled": false}, http.StatusOK)
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles", adminToken, nil, http.StatusNotFound)
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profiles/vis_http_a", adminToken, nil, http.StatusNotFound)
	doJSON(t, router, http.MethodPost, "/api/admin/visitor-profiles/vis_http_a/refresh", adminToken, map[string]any{}, http.StatusNotFound)
	// 配置接口本身仍可访问（否则无法重新开启）。
	doJSON(t, router, http.MethodGet, "/api/admin/visitor-profile/settings", adminToken, nil, http.StatusOK)
}

// newVisitorGeoHTTPStub 起一个第三方 IP 库桩（HTTP 层测试用）。
func newVisitorGeoHTTPStub(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success", "countryCode": "SA", "country": "Saudi Arabia",
			"regionName": "Riyadh", "city": "Riyadh", "timezone": "Asia/Riyadh",
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestAgentConversationProfileAccess 覆盖客服会话侧画像卡的鉴权与 404 语义。
func TestAgentConversationProfileAccess(t *testing.T) {
	router, st := newVisitorProfileRouter(t)
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	// 创建两个客服并上线，让新建会话自动分配给 agent1。
	created := doJSON(t, router, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account": "kf_profile_1", "password": "AgentPass-123456", "name": "画像客服一",
		"group": "售前组", "max_conversations": 5,
	}, http.StatusCreated)
	agent1ID := created["agent"].(map[string]any)["id"].(string)
	doJSON(t, router, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account": "kf_profile_2", "password": "AgentPass-123456", "name": "画像客服二",
		"group": "售前组", "max_conversations": 5,
	}, http.StatusCreated)

	agent1Token := loginForToken(t, router, "/api/agent/login", "kf_profile_1", "AgentPass-123456")
	agent2Token := loginForToken(t, router, "/api/agent/login", "kf_profile_2", "AgentPass-123456")
	doJSON(t, router, http.MethodPost, "/api/agent/status", agent1Token, map[string]any{"status": "online"}, http.StatusOK)

	// 为了让"无画像 404"可确定复现：先关闭画像再建会话（不采集画像），随后重新开启。
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"enabled": false}, http.StatusOK)
	session := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"lang": "ar"}, http.StatusCreated)
	conversation, _ := session["conversation"].(map[string]any)
	conversationID, _ := conversation["id"].(string)
	visitorID, _ := conversation["visitor_id"].(string)
	if conversationID == "" || visitorID == "" {
		t.Fatalf("建会话响应不符：%#v", session)
	}
	if assigned, _ := conversation["assigned_agent_id"].(string); assigned != agent1ID {
		t.Fatalf("会话应分配给 agent1：%#v", conversation)
	}
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"enabled": true}, http.StatusOK)

	path := "/api/agent/conversations/" + conversationID + "/profile"
	// 未登录 403；其它客服 403；访客 token 也按越权 403。
	doJSON(t, router, http.MethodGet, path, "", nil, http.StatusForbidden)
	doJSON(t, router, http.MethodGet, path, agent2Token, nil, http.StatusForbidden)
	if visitorToken, _ := session["token"].(string); visitorToken != "" {
		doJSON(t, router, http.MethodGet, path, visitorToken, nil, http.StatusForbidden)
	}
	// 尚无画像：404（不是 200 空体）。
	doJSON(t, router, http.MethodGet, path, agent1Token, nil, http.StatusNotFound)

	// 写入画像后：负责客服可读，且 show_full_ip=false 时 IP 脱敏。
	seedVisitorProfile(t, st, visitorID, func(profile *domain.VisitorProfile) {
		profile.Country = "沙特阿拉伯"
		profile.CountryCode = "SA"
		profile.City = "利雅得"
		profile.Languages = map[string]int{"ar": 3}
		profile.Summary = "咨询报价"
		profile.Tags = []string{"高意向"}
		profile.IPLast = "203.0.113.9"
		profile.NationalityGuess = "SA"
		profile.NationalityConfidence = 0.6
		profile.NationalityBasis = []string{domain.GeoBasisCDN}
	})
	payload := doJSON(t, router, http.MethodGet, path, agent1Token, nil, http.StatusOK)
	profile, _ := payload["profile"].(map[string]any)
	if profile["visitor_id"] != visitorID || profile["ip_last"] != "203.0.113.9" {
		t.Fatalf("客服侧画像不符：%#v", profile)
	}

	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"show_full_ip": false}, http.StatusOK)
	masked := doJSON(t, router, http.MethodGet, path, agent1Token, nil, http.StatusOK)
	maskedProfile, _ := masked["profile"].(map[string]any)
	if maskedProfile["ip_last"] != "203.0.113.*" {
		t.Fatalf("关闭完整 IP 后应脱敏：%#v", maskedProfile["ip_last"])
	}

	// 画像关闭：客服侧 404。
	doJSON(t, router, http.MethodPatch, "/api/admin/visitor-profile/settings", adminToken,
		map[string]any{"enabled": false}, http.StatusOK)
	doJSON(t, router, http.MethodGet, path, agent1Token, nil, http.StatusNotFound)
}

// TestVisitorProfileConversationCreationFeedsProfile 覆盖建会话（CDN 头）→ 画像的完整链路。
func TestVisitorProfileConversationCreationFeedsProfile(t *testing.T) {
	router, st := newVisitorProfileRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/visitor/conversations", strings.NewReader(`{"lang":"zh"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("CF-IPCountry", "CN")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("建会话 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var session map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&session); err != nil {
		t.Fatalf("解析建会话响应失败：%v", err)
	}
	conversation, _ := session["conversation"].(map[string]any)
	visitorID, _ := conversation["visitor_id"].(string)

	// 采集是异步的：轮询等待画像落地。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if profile, ok := st.VisitorProfile(visitorID); ok && profile.Visits > 0 {
			if profile.CountryCode != "CN" || profile.Country != "中国" {
				t.Fatalf("CDN 国家码未写入画像：%+v", profile)
			}
			if profile.Languages["zh"] != 1 {
				t.Fatalf("初始语言未计数：%+v", profile.Languages)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待画像采集超时")
}
