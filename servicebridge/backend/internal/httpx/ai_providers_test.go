package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestAdminAIProviderLifecycleAndSecret 覆盖供应商 CRUD、密钥掩码、服务间 /secret 与绑定校验。
func TestAdminAIProviderLifecycleAndSecret(t *testing.T) {
	router, st := newAgentPreferencesRouter(t)

	// 未登录不能访问任何供应商接口。
	doJSON(t, router, http.MethodGet, "/api/admin/ai-providers", "", nil, http.StatusForbidden)

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	created := doJSON(t, router, http.MethodPost, "/api/admin/ai-providers", adminToken, map[string]any{
		"name":          "OpenAI 主账号",
		"kind":          "text",
		"api_type":      "chat_completions",
		"base_url":      "https://api.openai.com/v1",
		"api_key":       "sk-test-1234567890",
		"models":        []string{"gpt-x", "gpt-y"},
		"allowed_hosts": "api.openai.com",
		"enabled":       true,
	}, http.StatusCreated)
	provider := created["provider"].(map[string]any)
	providerID, _ := provider["id"].(string)
	if providerID == "" {
		t.Fatalf("provider id missing: %#v", created)
	}
	if _, leaked := provider["api_key"]; leaked {
		t.Fatalf("响应不应包含 api_key 明文字段：%#v", provider)
	}
	if provider["api_key_masked"] != "sk-t****7890" {
		t.Fatalf("api_key_masked = %v, want sk-t****7890", provider["api_key_masked"])
	}
	if strings.Contains(recString(created), "sk-test-1234567890") {
		t.Fatalf("响应泄露了完整密钥：%s", recString(created))
	}

	list := doJSON(t, router, http.MethodGet, "/api/admin/ai-providers", adminToken, nil, http.StatusOK)
	providers, _ := list["providers"].([]any)
	if len(providers) != 1 || providers[0].(map[string]any)["api_key_masked"] != "sk-t****7890" {
		t.Fatalf("供应商列表不符合预期：%#v", list)
	}

	// 只改名称：密钥保持不变。
	doJSON(t, router, http.MethodPatch, "/api/admin/ai-providers/"+providerID, adminToken, map[string]any{
		"name": "OpenAI 备用账号",
	}, http.StatusOK)
	afterRename, ok := st.AIProvider(providerID)
	if !ok {
		t.Fatal("provider missing after patch")
	}
	if afterRename.APIKey != "sk-test-1234567890" {
		t.Fatalf("未传 api_key 时不应改动密钥：%q", afterRename.APIKey)
	}

	// 显式传空串：清除密钥。
	cleared := doJSON(t, router, http.MethodPatch, "/api/admin/ai-providers/"+providerID, adminToken, map[string]any{
		"api_key": "",
	}, http.StatusOK)
	if cleared["provider"].(map[string]any)["api_key_masked"] != "" {
		t.Fatalf("清除密钥后掩码应为空：%#v", cleared)
	}

	// 更新为新密钥：掩码随之变化。
	doJSON(t, router, http.MethodPatch, "/api/admin/ai-providers/"+providerID, adminToken, map[string]any{
		"api_key": "sk-new-key-abcdefgh",
	}, http.StatusOK)

	// 服务间 /secret：仅管理员可读，返回解密后的配置。
	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai-providers/"+providerID+"/secret", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/secret status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Astreva-Service-Only") != "true" {
		t.Fatalf("/secret 缺少服务间标记头：%v", rec.Header())
	}
	var secret map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&secret); err != nil {
		t.Fatalf("decode /secret: %v", err)
	}
	if secret["api_key"] != "sk-new-key-abcdefgh" {
		t.Fatalf("/secret api_key = %v", secret["api_key"])
	}
	if secret["base_url"] != "https://api.openai.com/v1" || secret["allowed_hosts"] != "api.openai.com" {
		t.Fatalf("/secret 配置不完整：%#v", secret)
	}
	doJSON(t, router, http.MethodGet, "/api/admin/ai-providers/"+providerID+"/secret", "", nil, http.StatusForbidden)

	// /resolve 是服务间同一实现的兼容路径。
	resolved := doJSON(t, router, http.MethodGet, "/api/admin/ai-providers/"+providerID+"/resolve", adminToken, nil, http.StatusOK)
	if resolved["api_key"] != "sk-new-key-abcdefgh" {
		t.Fatalf("/resolve api_key = %v", resolved["api_key"])
	}

	// 绑定：合法能力可以保存。
	bound := doJSON(t, router, http.MethodPut, "/api/admin/ai-bindings", adminToken, map[string]any{
		"bindings": []map[string]any{
			{"capability": "translation", "provider_id": providerID, "model": "gpt-x"},
		},
	}, http.StatusOK)
	bindings, _ := bound["bindings"].([]any)
	if len(bindings) != 1 || bindings[0].(map[string]any)["capability"] != "translation" {
		t.Fatalf("绑定结果不符合预期：%#v", bound)
	}
	readBindings := doJSON(t, router, http.MethodGet, "/api/admin/ai-bindings", adminToken, nil, http.StatusOK)
	if got, _ := readBindings["bindings"].([]any); len(got) != 1 {
		t.Fatalf("读取绑定不符合预期：%#v", readBindings)
	}

	// 非法能力、类型不匹配、已禁用供应商都必须 400。
	doJSON(t, router, http.MethodPut, "/api/admin/ai-bindings", adminToken, map[string]any{
		"bindings": []map[string]any{{"capability": "unknown", "provider_id": providerID}},
	}, http.StatusBadRequest)
	doJSON(t, router, http.MethodPut, "/api/admin/ai-bindings", adminToken, map[string]any{
		"bindings": []map[string]any{{"capability": "image", "provider_id": providerID}},
	}, http.StatusBadRequest)
	doJSON(t, router, http.MethodPatch, "/api/admin/ai-providers/"+providerID, adminToken, map[string]any{
		"enabled": false,
	}, http.StatusOK)
	doJSON(t, router, http.MethodPut, "/api/admin/ai-bindings", adminToken, map[string]any{
		"bindings": []map[string]any{{"capability": "translation", "provider_id": providerID}},
	}, http.StatusBadRequest)

	// 删除供应商后其绑定被一并删除，运行时回退。
	doJSON(t, router, http.MethodDelete, "/api/admin/ai-providers/"+providerID, adminToken, nil, http.StatusOK)
	if _, ok := st.AIProvider(providerID); ok {
		t.Fatal("删除后仍能读到供应商")
	}
	if _, ok := st.AIBindings()["translation"]; ok {
		t.Fatal("删除供应商后不应残留绑定")
	}
}

// TestAdminAIProviderFetchModelsWritesBack：一键获取模型并写回 provider.models。
func TestAdminAIProviderFetchModelsWritesBack(t *testing.T) {
	router, st := newAgentPreferencesRouter(t)
	var authHeader string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gpt-x"}, {"id": "gpt-y"}},
		})
	}))
	t.Cleanup(stub.Close)

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	created := doJSON(t, router, http.MethodPost, "/api/admin/ai-providers", adminToken, map[string]any{
		"name":     "模型列表供应商",
		"kind":     "text",
		"api_type": "chat_completions",
		"base_url": stub.URL,
		"api_key":  "models-key",
		"enabled":  true,
	}, http.StatusCreated)
	providerID := created["provider"].(map[string]any)["id"].(string)

	result := doJSON(t, router, http.MethodPost, "/api/admin/ai-providers/"+providerID+"/models", adminToken, nil, http.StatusOK)
	models, _ := result["models"].([]any)
	if len(models) != 2 || models[0] != "gpt-x" || models[1] != "gpt-y" {
		t.Fatalf("models 响应 = %#v", result["models"])
	}
	if authHeader != "Bearer models-key" {
		t.Fatalf("上游 Authorization = %q, want Bearer models-key", authHeader)
	}
	readBack, ok := st.AIProvider(providerID)
	if !ok {
		t.Fatal("provider missing")
	}
	if len(readBack.Models) != 2 || readBack.Models[0] != "gpt-x" || readBack.Models[1] != "gpt-y" {
		t.Fatalf("models 未写回：%#v", readBack.Models)
	}
}

// TestAdminAIProviderTestFailureReturns502：上游失败时 /test 返回 502 且带原因。
func TestAdminAIProviderTestFailureReturns502(t *testing.T) {
	router, _ := newAgentPreferencesRouter(t)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(stub.Close)

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	created := doJSON(t, router, http.MethodPost, "/api/admin/ai-providers", adminToken, map[string]any{
		"name":     "故障供应商",
		"kind":     "text",
		"api_type": "chat_completions",
		"base_url": stub.URL,
		"api_key":  "broken-key",
		"enabled":  true,
	}, http.StatusCreated)
	providerID := created["provider"].(map[string]any)["id"].(string)

	failed := doJSON(t, router, http.MethodPost, "/api/admin/ai-providers/"+providerID+"/test", adminToken, nil, http.StatusBadGateway)
	if failed["error"] != "ai_provider_test_failed" {
		t.Fatalf("错误码 = %v", failed["error"])
	}
	message, _ := failed["message"].(string)
	if !strings.Contains(message, "upstream exploded") || !strings.Contains(message, "status=500") {
		t.Fatalf("失败原因未透出：%q", message)
	}
}

// TestAdminAgentPreferences：管理员可查看/重置指定客服偏好。
func TestAdminAgentPreferences(t *testing.T) {
	router, st := newAgentPreferencesRouter(t)
	agent, err := st.CreateAgent(domain.Agent{
		Account: "admin_pref_agent",
		Name:    "偏好被管理客服",
		Group:   "默认组",
		Status:  domain.AgentOffline,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	initial := doJSON(t, router, http.MethodGet, "/api/admin/agents/"+agent.ID+"/preferences", adminToken, nil, http.StatusOK)
	preferences := initial["preferences"].(map[string]any)
	if preferences["auto_translate_incoming"] != true || preferences["translation_dialect_mode"] != domain.DialectModeStandard {
		t.Fatalf("默认偏好不符合预期：%#v", preferences)
	}

	patched := doJSON(t, router, http.MethodPatch, "/api/admin/agents/"+agent.ID+"/preferences", adminToken, map[string]any{
		"auto_translate_incoming":  false,
		"translation_dialect_mode": "dialect",
	}, http.StatusOK)
	updated := patched["preferences"].(map[string]any)
	if updated["auto_translate_incoming"] != false || updated["translation_dialect_mode"] != domain.DialectModeDialect {
		t.Fatalf("更新偏好不符合预期：%#v", updated)
	}
	readBack, err := st.AgentPreferences(agent.ID)
	if err != nil {
		t.Fatalf("read preferences: %v", err)
	}
	readBack = readBack.WithDefaults()
	if readBack.AutoTranslateIncoming == nil || *readBack.AutoTranslateIncoming || readBack.DialectMode != domain.DialectModeDialect {
		t.Fatalf("存储层偏好与接口不一致：%+v", readBack)
	}

	// 不存在的客服返回 404；客服身份访问管理员接口返回 403。
	doJSON(t, router, http.MethodGet, "/api/admin/agents/missing/preferences", adminToken, nil, http.StatusNotFound)
	agentToken := loginForToken(t, router, "/api/agent/login", agent.Account, "AgentPass-123456")
	doJSON(t, router, http.MethodGet, "/api/admin/agents/"+agent.ID+"/preferences", agentToken, nil, http.StatusForbidden)
}
