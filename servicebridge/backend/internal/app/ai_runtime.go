package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

// ErrAIProviderHostNotAllowed 表示绑定供应商的目标主机不在 allowed_hosts 白名单内。
// 这是防 SSRF 的硬性拦截：请求绝不会发往未授权主机，也不会静默改投到其它供应商。
var ErrAIProviderHostNotAllowed = errors.New("ai_provider_host_not_allowed")

// providerModelsTimeout 是一键获取模型/连通性测试的外层超时上限。
const providerModelsTimeout = 60 * time.Second

// AIProviderTestResult 是供应商连通性测试结果。
type AIProviderTestResult struct {
	OK          bool   `json:"ok"`
	LatencyMS   int64  `json:"latency_ms"`
	Message     string `json:"message"`
	ModelsCount int    `json:"models_count"`
}

// aiSettingsFor 返回某能力应使用的上游配置：
// 优先能力绑定的供应商，未绑定/被禁用/配置无效时回退既有 ai_settings。
// 白名单校验不通过时同样回退并记录日志（严格调用路径见 aiSettingsForCapability）。
func (s *Service) aiSettingsFor(capability string) domain.AISettings {
	settings, err := s.aiSettingsForCapability(capability)
	if err != nil {
		s.logger.Warn("AI 能力绑定不可用，回退到全局 AI 配置",
			"capability", capability, "error", err)
		return s.settingsWithCacheMode()
	}
	return settings
}

// aiSettingsForCapability 是能力解析的严格版本：
// 绑定供应商存在但目标主机不在白名单内时返回错误，调用方必须放弃本次请求。
func (s *Service) aiSettingsForCapability(capability string) (domain.AISettings, error) {
	base := s.settingsWithCacheMode()
	capability = strings.ToLower(strings.TrimSpace(capability))
	binding, ok := s.store.AIBindings()[capability]
	if !ok || strings.TrimSpace(binding.ProviderID) == "" {
		return base, nil
	}
	provider, ok := s.store.AIProvider(binding.ProviderID)
	if !ok || !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
		// 供应商被删除或禁用：回退到既有 ai_settings，保证服务不中断。
		return base, nil
	}
	if !domain.HostAllowed(provider.BaseURL, provider.AllowedHosts) {
		return domain.AISettings{}, fmt.Errorf(
			"%w：供应商 %q 的目标主机不在 allowed_hosts 白名单内", ErrAIProviderHostNotAllowed, provider.ID)
	}
	out := base
	out.BaseURL = strings.TrimSpace(provider.BaseURL)
	out.APIKey = provider.APIKey
	out.APIType = provider.APIType
	if provider.TimeoutMS > 0 {
		seconds := provider.TimeoutMS / 1000
		if seconds < 1 {
			seconds = 1
		}
		out.TimeoutSeconds = seconds
	}
	model := strings.TrimSpace(binding.Model)
	if model == "" {
		model = strings.TrimSpace(provider.DefaultModel)
	}
	if model == "" && len(provider.Models) > 0 {
		model = strings.TrimSpace(provider.Models[0])
	}
	if model != "" {
		out.Model = model
	}
	// 缓存模式按最终模型解析；未探测到则保持原有请求格式。
	out.CacheMode = ""
	switch mode := strings.TrimSpace(s.store.AICacheProbe().ModelResults[out.Model]); mode {
	case ai.CacheModeAuto, ai.CacheModeExplicit:
		out.CacheMode = mode
	}
	return out, nil
}

// translationSettingsOrDefault 解析 translation 能力配置；绑定不可用（含白名单拒绝）时
// 回退到既有 ai_settings，保证欢迎语/系统消息等后台翻译不中断。
func (s *Service) translationSettingsOrDefault(conversationID string) domain.AISettings {
	settings, err := s.aiSettingsForCapability(domain.AICapabilityTranslation)
	if err != nil {
		s.logger.Warn("翻译能力绑定不可用，回退全局配置",
			"conversation_id", conversationID, "error", err)
		return s.settingsWithCacheMode()
	}
	return settings
}

// settingsForProvider 把供应商配置转换为一次调用的 AISettings，
// 保留全局的 enabled/mode/temperature/max_output_tokens/system_prompt。
func (s *Service) settingsForProvider(provider domain.AIProvider) domain.AISettings {
	settings := s.settingsWithCacheMode()
	settings.BaseURL = strings.TrimSpace(provider.BaseURL)
	settings.APIKey = provider.APIKey
	settings.APIType = provider.APIType
	if provider.TimeoutMS > 0 {
		seconds := provider.TimeoutMS / 1000
		if seconds < 1 {
			seconds = 1
		}
		settings.TimeoutSeconds = seconds
	}
	model := strings.TrimSpace(provider.DefaultModel)
	if model == "" && len(provider.Models) > 0 {
		model = strings.TrimSpace(provider.Models[0])
	}
	if model != "" {
		settings.Model = model
	}
	settings.CacheMode = ""
	switch mode := strings.TrimSpace(s.store.AICacheProbe().ModelResults[settings.Model]); mode {
	case ai.CacheModeAuto, ai.CacheModeExplicit:
		settings.CacheMode = mode
	}
	return settings
}

// providerSettings 读取供应商并返回其调用配置，同时执行主机白名单校验。
func (s *Service) providerSettings(providerID string) (domain.AIProvider, domain.AISettings, error) {
	provider, ok := s.store.AIProvider(strings.TrimSpace(providerID))
	if !ok {
		return domain.AIProvider{}, domain.AISettings{}, store.ErrNotFound
	}
	if !domain.HostAllowed(provider.BaseURL, provider.AllowedHosts) {
		return provider, domain.AISettings{}, fmt.Errorf(
			"%w：供应商 %q 的目标主机不在 allowed_hosts 白名单内", ErrAIProviderHostNotAllowed, provider.ID)
	}
	return provider, s.settingsForProvider(provider), nil
}

// FetchProviderModels 一键获取供应商模型列表并写回 models。
// anthropic_messages 上游通常不提供 /models，失败时返回 ErrInvalidInput 提示手动填写。
func (s *Service) FetchProviderModels(ctx context.Context, providerID string) (domain.AIProvider, []string, error) {
	provider, settings, err := s.providerSettings(providerID)
	if err != nil {
		return provider, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, providerModelsTimeout)
	defer cancel()
	models, err := s.ai.ListModels(ctx, settings)
	if err != nil {
		if provider.APIType == domain.AIAPITypeAnthropicMessages {
			return provider, nil, fmt.Errorf("%w：Anthropic 协议上游通常不支持 /models，请手动填写模型列表（%v）", store.ErrInvalidInput, err)
		}
		return provider, nil, fmt.Errorf("获取模型列表失败：%w", err)
	}
	provider.Models = models
	updated, err := s.store.UpdateAIProvider(provider.ID, provider, nil)
	if err != nil {
		return provider, models, err
	}
	return updated, models, nil
}

// TestProvider 执行最小连通性测试：优先 GET /models，失败时用一次极小的对话请求兜底。
// 返回的 error 供 HTTP 层映射为 502 并带上原因。
func (s *Service) TestProvider(ctx context.Context, providerID string) (AIProviderTestResult, error) {
	_, settings, err := s.providerSettings(providerID)
	if err != nil {
		return AIProviderTestResult{OK: false, Message: err.Error()}, err
	}
	ctx, cancel := context.WithTimeout(ctx, providerModelsTimeout)
	defer cancel()
	start := time.Now()
	models, modelsErr := s.ai.ListModels(ctx, settings)
	latency := time.Since(start).Milliseconds()
	if modelsErr == nil {
		return AIProviderTestResult{
			OK:          true,
			LatencyMS:   latency,
			Message:     fmt.Sprintf("连接成功，获取到 %d 个模型", len(models)),
			ModelsCount: len(models),
		}, nil
	}
	// 部分上游不提供 /models；有密钥时用一次极小对话验证连通性。
	// 没有密钥时不能走 Reply（它会直接返回本地兜底文案，造成假成功）。
	if strings.TrimSpace(settings.APIKey) != "" {
		if reply, chatErr := s.ai.Reply(ctx, settings, s.store.ContactSettings(), nil, "ping", ai.LangEn, ""); chatErr == nil && strings.TrimSpace(reply) != "" {
			return AIProviderTestResult{
				OK:        true,
				LatencyMS: time.Since(start).Milliseconds(),
				Message:   "连接成功（上游未提供 /models，已用最小对话请求验证）",
			}, nil
		}
	}
	result := AIProviderTestResult{
		OK:        false,
		LatencyMS: latency,
		Message:   modelsErr.Error(),
	}
	return result, modelsErr
}
