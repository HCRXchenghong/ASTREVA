package store

import (
	"errors"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestMemoryAIProviderCRUDAndMasking 覆盖供应商增删改查、密钥掩码与明文边界。
func TestMemoryAIProviderCRUDAndMasking(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	created, err := s.CreateAIProvider(domain.AIProvider{
		Name:         "OpenAI 主账号",
		Kind:         domain.AIProviderKindText,
		APIType:      domain.AIAPITypeChatCompletions,
		BaseURL:      "https://api.openai.com/v1",
		Models:       []string{"gpt-x", "gpt-x", " gpt-y "},
		AllowedHosts: "api.openai.com",
		Enabled:      true,
	}, "sk-test-1234567890")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if created.ID == "" {
		t.Fatal("provider id should be generated")
	}
	if created.TimeoutMS != 30000 {
		t.Fatalf("timeout_ms 默认值 = %d, want 30000", created.TimeoutMS)
	}
	if len(created.Models) != 2 || created.Models[0] != "gpt-x" || created.Models[1] != "gpt-y" {
		t.Fatalf("models 应去重去空格，实际 %#v", created.Models)
	}
	if created.APIKeyMasked != "sk-t****7890" {
		t.Fatalf("api_key_masked = %q, want sk-t****7890", created.APIKeyMasked)
	}

	// 列表只返回掩码，绝不返回明文。
	providers := s.ListAIProviders()
	if len(providers) != 1 {
		t.Fatalf("providers 数量 = %d, want 1", len(providers))
	}
	if providers[0].APIKey != "" {
		t.Fatalf("列表不应返回明文密钥：%q", providers[0].APIKey)
	}
	if providers[0].APIKeyMasked != "sk-t****7890" {
		t.Fatalf("列表掩码 = %q", providers[0].APIKeyMasked)
	}

	// 内部读取（/secret 与运行时）可以拿到明文。
	internal, ok := s.AIProvider(created.ID)
	if !ok {
		t.Fatal("AIProvider should find created provider")
	}
	if internal.APIKey != "sk-test-1234567890" {
		t.Fatalf("内部读取密钥 = %q", internal.APIKey)
	}

	// apiKey==nil 表示不改密钥。
	renamed := internal
	renamed.Name = "OpenAI 备用账号"
	updated, err := s.UpdateAIProvider(created.ID, renamed, nil)
	if err != nil {
		t.Fatalf("update provider: %v", err)
	}
	if updated.Name != "OpenAI 备用账号" {
		t.Fatalf("更新后名称 = %q", updated.Name)
	}
	if updated.APIKeyMasked != "sk-t****7890" {
		t.Fatalf("未传 api_key 时密钥应保持不变，mask=%q", updated.APIKeyMasked)
	}

	// 传空串表示清除密钥。
	empty := ""
	cleared, err := s.UpdateAIProvider(created.ID, renamed, &empty)
	if err != nil {
		t.Fatalf("clear provider key: %v", err)
	}
	if cleared.APIKeyMasked != "" {
		t.Fatalf("清除后 mask = %q, want empty", cleared.APIKeyMasked)
	}
	if read, _ := s.AIProvider(created.ID); read.APIKey != "" {
		t.Fatalf("清除后明文 = %q, want empty", read.APIKey)
	}

	// 传入新密钥后掩码随之更新。
	newKey := "sk-new-key-abcdefgh"
	refreshed, err := s.UpdateAIProvider(created.ID, renamed, &newKey)
	if err != nil {
		t.Fatalf("update provider key: %v", err)
	}
	if refreshed.APIKeyMasked != "sk-n****efgh" {
		t.Fatalf("新密钥掩码 = %q, want sk-n****efgh", refreshed.APIKeyMasked)
	}

	if err := s.DeleteAIProvider(created.ID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if _, ok := s.AIProvider(created.ID); ok {
		t.Fatal("删除后仍能读到 provider")
	}
	if providers := s.ListAIProviders(); len(providers) != 0 {
		t.Fatalf("删除后列表应为空：%#v", providers)
	}
	if err := s.DeleteAIProvider(created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

// TestMemoryAIProviderValidationAndHostWhitelist 覆盖类型/协议匹配与白名单格式校验。
func TestMemoryAIProviderValidationAndHostWhitelist(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	if _, err := s.CreateAIProvider(domain.AIProvider{
		Name:    "非法白名单",
		BaseURL: "https://api.example.com/v1",
		// 白名单条目不允许带协议/路径。
		AllowedHosts: "https://api.example.com/v1",
		Enabled:      true,
	}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法 allowed_hosts 应返回 ErrInvalidInput，实际 %v", err)
	}

	if _, err := s.CreateAIProvider(domain.AIProvider{
		Name:    "类型不匹配",
		Kind:    domain.AIProviderKindImage,
		APIType: domain.AIAPITypeChatCompletions,
		BaseURL: "https://api.example.com/v1",
		Enabled: true,
	}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("image 供应商使用 chat 协议应返回 ErrInvalidInput，实际 %v", err)
	}

	image, err := s.CreateAIProvider(domain.AIProvider{
		Name:    "图像供应商",
		Kind:    domain.AIProviderKindImage,
		BaseURL: "https://image.example.com/v1",
		Enabled: true,
	}, "")
	if err != nil {
		t.Fatalf("create image provider: %v", err)
	}
	if image.APIType != domain.AIAPITypeImages {
		t.Fatalf("image 供应商默认协议 = %q, want images", image.APIType)
	}

	cases := []struct {
		name    string
		rawURL  string
		allowed string
		want    bool
	}{
		{"空白名单不限制", "http://127.0.0.1:8080/v1", "", true},
		{"精确主机匹配", "https://api.openai.com/v1", "api.openai.com", true},
		{"主机不在白名单", "http://127.0.0.1:8080/v1", "example.com", false},
		{"子域通配", "https://api.example.com/v1", "*.example.com", true},
		{"端口匹配", "https://api.example.com:8443/v1", "api.example.com:8443", true},
		{"端口不匹配", "https://api.example.com:443/v1", "api.example.com:8443", false},
		{"IP 白名单", "http://10.0.0.8:9000/v1", "10.0.0.8", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.HostAllowed(tc.rawURL, tc.allowed); got != tc.want {
				t.Fatalf("HostAllowed(%q, %q) = %v, want %v", tc.rawURL, tc.allowed, got, tc.want)
			}
		})
	}
}

// TestMemoryAIBindingsAndDeleteResidual 覆盖绑定读写、能力校验与删除供应商后的残留清理。
func TestMemoryAIBindingsAndDeleteResidual(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	provider, err := s.CreateAIProvider(domain.AIProvider{
		Name:    "翻译供应商",
		Kind:    domain.AIProviderKindText,
		BaseURL: "https://api.example.com/v1",
		Models:  []string{"translate-model"},
		Enabled: true,
	}, "secret-key-1234")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	binding, err := s.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "translate-model",
		Params:     map[string]any{"temperature": 0.2},
	})
	if err != nil {
		t.Fatalf("upsert binding: %v", err)
	}
	if binding.UpdatedAt.IsZero() {
		t.Fatal("binding updated_at 未写入")
	}
	bindings := s.AIBindings()
	if got := bindings[domain.AICapabilityTranslation]; got.ProviderID != provider.ID || got.Model != "translate-model" {
		t.Fatalf("binding 读回不一致：%+v", got)
	}
	if _, err := s.UpsertAIBinding(domain.AIBinding{Capability: "unknown", ProviderID: provider.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法能力应返回 ErrInvalidInput，实际 %v", err)
	}
	// 再次写入同一能力为覆盖更新。
	if _, err := s.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "translate-model-v2",
	}); err != nil {
		t.Fatalf("overwrite binding: %v", err)
	}
	if got := s.AIBindings()[domain.AICapabilityTranslation]; got.Model != "translate-model-v2" {
		t.Fatalf("覆盖更新失败：%+v", got)
	}

	// 删除供应商后，绑定被一并删除（无残留），运行时自动回退。
	if err := s.DeleteAIProvider(provider.ID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if _, ok := s.AIBindings()[domain.AICapabilityTranslation]; ok {
		t.Fatal("删除供应商后不应残留能力绑定")
	}
}

// TestMemoryAIBindingTargetsCRUDAndCascade 覆盖候选供应商的增改查、主键覆盖、
// 优先级排序、参数校验与删除供应商后的级联清理。
func TestMemoryAIBindingTargetsCRUDAndCascade(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	// 显式指定 ID，便于断言同优先级下的稳定排序（provider_id 升序）。
	primary, err := s.CreateAIProvider(domain.AIProvider{
		ID:      "aip_primary",
		Name:    "主供应商",
		Kind:    domain.AIProviderKindText,
		BaseURL: "https://api.example.com/v1",
		Models:  []string{"model-a"},
		Enabled: true,
	}, "key-primary")
	if err != nil {
		t.Fatalf("create primary provider: %v", err)
	}
	backup, err := s.CreateAIProvider(domain.AIProvider{
		ID:      "aip_backup",
		Name:    "备供应商",
		Kind:    domain.AIProviderKindText,
		BaseURL: "https://api2.example.com/v1",
		Models:  []string{"model-b"},
		Enabled: true,
	}, "key-backup")
	if err != nil {
		t.Fatalf("create backup provider: %v", err)
	}

	first, err := s.UpsertAIBindingTarget(domain.AIBindingTarget{
		Capability: domain.AICapabilityTranslation,
		ProviderID: primary.ID,
		Model:      "model-a",
		Priority:   1,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("upsert primary target: %v", err)
	}
	if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatal("候选 created_at/updated_at 未写入")
	}
	if _, err := s.UpsertAIBindingTarget(domain.AIBindingTarget{
		Capability: domain.AICapabilityTranslation,
		ProviderID: backup.ID,
		Model:      "model-b",
		Priority:   0,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("upsert backup target: %v", err)
	}

	// 排序：priority 升序。
	targets := s.AIBindingTargets()
	if len(targets) != 2 || targets[0].ProviderID != backup.ID || targets[1].ProviderID != primary.ID {
		t.Fatalf("候选排序不符合 priority 升序：%#v", targets)
	}

	// 覆盖更新：主键相同，created_at 保留、model/priority/enabled 覆盖。
	updated, err := s.UpsertAIBindingTarget(domain.AIBindingTarget{
		Capability: domain.AICapabilityTranslation,
		ProviderID: primary.ID,
		Model:      "model-a-v2",
		Priority:   0,
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("overwrite primary target: %v", err)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("覆盖更新应保留 created_at：%v -> %v", first.CreatedAt, updated.CreatedAt)
	}
	if updated.Model != "model-a-v2" || updated.Priority != 0 || updated.Enabled {
		t.Fatalf("覆盖更新未生效：%+v", updated)
	}
	// 同优先级（0）下按 provider_id 升序：aip_backup < aip_primary。
	targets = s.AIBindingTargets()
	if len(targets) != 2 || targets[0].ProviderID != backup.ID || targets[1].ProviderID != primary.ID {
		t.Fatalf("同优先级排序应为 provider_id 升序：%#v", targets)
	}

	// 非法能力 / 空 provider_id / 负数 priority 归一。
	if _, err := s.UpsertAIBindingTarget(domain.AIBindingTarget{Capability: "unknown", ProviderID: primary.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法能力应返回 ErrInvalidInput，实际 %v", err)
	}
	if _, err := s.UpsertAIBindingTarget(domain.AIBindingTarget{Capability: domain.AICapabilityTranslation, ProviderID: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空 provider_id 应返回 ErrInvalidInput，实际 %v", err)
	}
	negative, err := s.UpsertAIBindingTarget(domain.AIBindingTarget{
		Capability: domain.AICapabilityTranslation,
		ProviderID: backup.ID,
		Priority:   -5,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("负数 priority 应被归一而非报错：%v", err)
	}
	if negative.Priority != 0 {
		t.Fatalf("负数 priority 应归零，实际 %d", negative.Priority)
	}

	// 删除：不存在返回 ErrNotFound，存在则移除。
	if err := s.DeleteAIBindingTarget(domain.AICapabilityTranslation, "aip_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除不存在的候选应返回 ErrNotFound，实际 %v", err)
	}
	if err := s.DeleteAIBindingTarget(domain.AICapabilityTranslation, backup.ID); err != nil {
		t.Fatalf("删除候选失败：%v", err)
	}
	if got := s.AIBindingTargets(); len(got) != 1 || got[0].ProviderID != primary.ID {
		t.Fatalf("删除后剩余候选不符合预期：%#v", got)
	}

	// 删除供应商：其候选被级联清理。
	if err := s.DeleteAIProvider(primary.ID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if got := s.AIBindingTargets(); len(got) != 0 {
		t.Fatalf("删除供应商后不应残留候选：%#v", got)
	}
}
