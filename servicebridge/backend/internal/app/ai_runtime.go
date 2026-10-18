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

// ErrAIUpstreamUnavailable 表示能力链上的候选供应商全部调用失败（或全部处于熔断中）。
// 错误信息只包含能力、尝试/跳过数量与脱敏后的最后一次上游错误，绝不包含任何密钥。
var ErrAIUpstreamUnavailable = errors.New("ai_upstream_unavailable")

// providerModelsTimeout 是一键获取模型/连通性测试的外层超时上限。
const providerModelsTimeout = 60 * time.Second

// AIProviderTestResult 是供应商连通性测试结果。
type AIProviderTestResult struct {
	OK          bool   `json:"ok"`
	LatencyMS   int64  `json:"latency_ms"`
	Message     string `json:"message"`
	ModelsCount int    `json:"models_count"`
}

// aiCandidate 是能力链解析过程中的一条候选：blocked 非空表示该候选被白名单拒绝。
type aiCandidate struct {
	settings domain.AISettings
	blocked  error
}

// aiSettingsFor 返回某能力应使用的上游配置（宽容版）：
// 优先能力链首（targets 按 priority 升序，其次旧 binding 兜底），
// 解析异常（含白名单拒绝）时回退既有 ai_settings 并记录日志（严格调用路径见 aiSettingsForCapability）。
func (s *Service) aiSettingsFor(capability string) domain.AISettings {
	settings, err := s.aiSettingsForCapability(capability)
	if err != nil {
		s.logger.Warn("AI 能力绑定不可用，回退到全局 AI 配置",
			"capability", capability, "error", err)
		return s.settingsWithCacheMode()
	}
	return settings
}

// aiSettingsForCapability 是能力解析的严格版本：返回链首候选；
// 链首候选存在但目标主机不在白名单内时返回错误，调用方必须放弃本次请求。
func (s *Service) aiSettingsForCapability(capability string) (domain.AISettings, error) {
	settings, err, _ := s.resolveAISettings(capability)
	return settings, err
}

// resolveAISettings 解析链首配置并同时返回候选列表，供需要"解析一次、尝试多次"的调用方复用。
func (s *Service) resolveAISettings(capability string) (domain.AISettings, error, []aiCandidate) {
	candidates := s.aiCandidates(capability)
	if len(candidates) == 0 {
		return s.settingsWithCacheMode(), nil, candidates
	}
	if candidates[0].blocked != nil {
		return domain.AISettings{}, candidates[0].blocked, candidates
	}
	return candidates[0].settings, nil, candidates
}

// aiCandidates 按优先级返回某能力的候选配置（不含全局 ai_settings 兜底）：
// 先是 ai_binding_targets 中该能力 enabled 的记录（priority 升序、provider_id 升序），
// 再追加旧 ai_bindings 的单条记录作为兜底（等价于 priority=0 的追加项，已有部署行为不变）。
// 供应商不存在/被禁用/无 BaseURL 的候选直接跳过（等价于未绑定，由调用方回退全局配置）。
//
// 未配置任何候选（targets 与 bindings 都为空，绝大多数部署）时直接返回 nil：
// 这是访客消息主链路上的热路径，必须避免多余的内存分配与全局配置解析。
func (s *Service) aiCandidates(capability string) []aiCandidate {
	capability = strings.ToLower(strings.TrimSpace(capability))
	targets := s.store.AIBindingTargets()
	bindings := s.store.AIBindings()
	if len(targets) == 0 && len(bindings) == 0 {
		return nil
	}
	base := s.settingsWithCacheMode()
	out := make([]aiCandidate, 0, 4)
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.Capability != capability || !target.Enabled {
			continue
		}
		providerID := strings.TrimSpace(target.ProviderID)
		if providerID == "" || seen[providerID] {
			continue
		}
		settings, usable, err := s.resolveAICandidate(base, providerID, target.Model)
		if err != nil {
			seen[providerID] = true
			out = append(out, aiCandidate{blocked: err})
			continue
		}
		if !usable {
			continue
		}
		seen[providerID] = true
		out = append(out, aiCandidate{settings: settings})
	}
	if binding, ok := bindings[capability]; ok {
		providerID := strings.TrimSpace(binding.ProviderID)
		// 与 targets 指向同一供应商时不重复追加，避免链上出现完全相同的候选。
		if providerID != "" && !seen[providerID] {
			settings, usable, err := s.resolveAICandidate(base, providerID, binding.Model)
			switch {
			case err != nil:
				out = append(out, aiCandidate{blocked: err})
			case usable:
				out = append(out, aiCandidate{settings: settings})
			}
		}
	}
	return out
}

// resolveAICandidate 把一条候选（供应商 + 模型）解析为调用配置：
//   - 供应商不存在/被禁用/无 BaseURL → usable=false（不算错误，等价未绑定）；
//   - 目标主机不在 allowed_hosts 白名单内 → 硬错误（防 SSRF，绝不静默改投其它供应商）。
func (s *Service) resolveAICandidate(base domain.AISettings, providerID, model string) (domain.AISettings, bool, error) {
	provider, ok := s.store.AIProvider(providerID)
	if !ok || !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
		return domain.AISettings{}, false, nil
	}
	if !domain.HostAllowed(provider.BaseURL, provider.AllowedHosts) {
		return domain.AISettings{}, false, fmt.Errorf(
			"%w：供应商 %q 的目标主机不在 allowed_hosts 白名单内", ErrAIProviderHostNotAllowed, provider.ID)
	}
	return s.settingsForCandidate(base, provider, model), true, nil
}

// settingsForCandidate 把供应商配置叠加到全局 ai_settings 之上（保留 enabled/mode/temperature 等），
// 并写入运行时字段 ProviderID/ProviderName 供熔断器与健康接口使用。
func (s *Service) settingsForCandidate(base domain.AISettings, provider domain.AIProvider, model string) domain.AISettings {
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
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(provider.DefaultModel)
	}
	if model == "" && len(provider.Models) > 0 {
		model = strings.TrimSpace(provider.Models[0])
	}
	if model != "" {
		out.Model = model
	}
	out.ProviderID = provider.ID
	out.ProviderName = provider.Name
	// 缓存模式按最终模型解析；未探测到则保持原有请求格式。
	out.CacheMode = ""
	switch mode := strings.TrimSpace(s.store.AICacheProbe().ModelResults[out.Model]); mode {
	case ai.CacheModeAuto, ai.CacheModeExplicit:
		out.CacheMode = mode
	}
	return out
}

// aiSettingsChain 返回某能力的完整候选链（按尝试顺序）：
// targets（priority 升序）→ 旧 binding 兜底 → 全局 ai_settings（BaseURL/APIKey 可用时）。
// 白名单拒绝的候选会被跳过并记日志：严格调用路径（doWithFailover / aiSettingsForCapability）
// 会先对链首候选硬失败，因此这里跳过只影响故障转移的后续候选，不会把请求静默发往未授权主机。
func (s *Service) aiSettingsChain(capability string) []domain.AISettings {
	return s.chainFromCandidates(capability, s.aiCandidates(capability))
}

// chainFromCandidates 把候选列表展开为可尝试的配置链（跳过白名单拒绝项并追加全局兜底）。
func (s *Service) chainFromCandidates(capability string, candidates []aiCandidate) []domain.AISettings {
	chain := make([]domain.AISettings, 0, len(candidates)+1)
	for _, candidate := range candidates {
		if candidate.blocked != nil {
			s.logger.Warn("AI 候选供应商白名单不通过，已跳过",
				"capability", capability, "error", candidate.blocked)
			continue
		}
		chain = append(chain, candidate.settings)
	}
	// 全局兜底只在"确实可调用"时加入：只有 base_url 没有 Key 的配置调用时不会真正请求上游，
	// 而是返回本地兜底话术——把它当成候选会掩盖前面供应商的真实失败（且兜底话术语言可能不对）。
	base := s.settingsWithCacheMode()
	if strings.TrimSpace(base.BaseURL) != "" && strings.TrimSpace(base.APIKey) != "" {
		chain = append(chain, base)
	}
	return chain
}

// doWithFailover 依次尝试能力链上的每个配置，实现"多供应商 + 失效熔断 + 自动切下一个"：
//   - 链首候选被白名单拒绝时直接失败（严格语义：绝不静默改投到其它供应商）；
//   - 熔断打开（open）的供应商被完全跳过，不产生任何上游调用；
//   - 每次真实调用成功/失败都会更新熔断器（连续失败达阈值 → 打开，冷却后 half_open 放行一次）；
//   - 上游失败且可重试时切到下一个候选；全部失败返回最后一次错误（含尝试/跳过数量，密钥已脱敏）。
//
// 链只有一项时与改造前的单供应商行为完全等价。
func (s *Service) doWithFailover(ctx context.Context, capability string, fn func(domain.AISettings) (string, error)) (string, error) {
	// 只解析一次候选：链首被白名单拒绝时硬失败，其余拒绝项跳过（防 SSRF 且不阻断故障转移）。
	candidates := s.aiCandidates(capability)
	if len(candidates) > 0 && candidates[0].blocked != nil {
		return "", candidates[0].blocked
	}
	return s.runFailoverChain(ctx, capability, s.chainFromCandidates(capability, candidates), fn)
}

// doWithFailoverLenient 与 doWithFailover 相同，但白名单拒绝的候选只跳过、不阻断，
// 供欢迎语/系统消息等后台翻译使用（保证这类非访客阻塞链路不中断）。
func (s *Service) doWithFailoverLenient(ctx context.Context, capability string, fn func(domain.AISettings) (string, error)) (string, error) {
	return s.runFailoverChain(ctx, capability, s.aiSettingsChain(capability), fn)
}

// runFailoverChain 是故障转移的实际执行循环，见 doWithFailover 的语义说明。
// 链上只有一个候选（绝大多数部署：仅全局 ai_settings 或仅一个绑定供应商）时走快速路径，
// 不做错误包装与日志限流，错误原样返回，行为与改造前的单供应商实现完全一致。
func (s *Service) runFailoverChain(ctx context.Context, capability string, chain []domain.AISettings, fn func(domain.AISettings) (string, error)) (string, error) {
	if len(chain) == 0 {
		// 与改造前一致：没有任何可用配置时仍按全局配置尝试一次
		//（未配置密钥时由 AI 客户端返回本地兜底文案/明确错误）。
		chain = []domain.AISettings{s.settingsWithCacheMode()}
	}
	if len(chain) == 1 {
		settings := chain[0]
		providerID := strings.TrimSpace(settings.ProviderID)
		if providerID != "" && !s.circuit.allow(providerID) {
			return "", fmt.Errorf("%w：能力 %s 的供应商 %s 正在熔断冷却中",
				ErrAIUpstreamUnavailable, capability, providerID)
		}
		out, err := fn(settings)
		if err != nil {
			// 单候选路径：错误原样返回（文案与改造前一致），但先做密钥脱敏，
			// 防止上游把请求头原样回显到响应体里而进入日志/接口。
			redacted := redactedAIError{err: err, text: redactAISecrets(err.Error(), settings)}
			if providerID != "" {
				if opened := s.circuit.fail(providerID, redacted); opened {
					s.logger.Warn("AI 供应商熔断打开",
						"capability", capability, "provider_id", providerID, "provider", settings.ProviderName)
				}
			}
			return "", redacted
		}
		if providerID != "" {
			s.circuit.succeed(providerID)
		}
		return out, nil
	}
	attempted := 0
	skipped := 0
	var lastErr error
	for _, settings := range chain {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		providerID := strings.TrimSpace(settings.ProviderID)
		if providerID != "" && !s.circuit.allow(providerID) {
			// 熔断打开：完全跳过该供应商，不计入尝试次数。
			skipped++
			continue
		}
		attempted++
		out, err := fn(settings)
		if err == nil {
			if providerID != "" {
				s.circuit.succeed(providerID)
			}
			if attempted > 1 || skipped > 0 {
				s.logger.Info("AI 上游故障转移成功",
					"capability", capability, "provider_id", providerID,
					"provider", settings.ProviderName, "attempted", attempted, "skipped", skipped)
			}
			return out, nil
		}
		redacted := redactedAIError{err: err, text: redactAISecrets(err.Error(), settings)}
		if providerID != "" {
			// 熔断状态跃迁是低频事件，不受日志限流约束。
			if opened := s.circuit.fail(providerID, redacted); opened {
				s.logger.Warn("AI 供应商熔断打开",
					"capability", capability, "provider_id", providerID, "provider", settings.ProviderName)
			}
		}
		lastErr = err
		if !isRetryableAIUpstreamError(err) {
			break
		}
		// 日志限流：同一 (能力, 供应商) 每分钟最多一条，避免上游持续故障时刷屏。
		if s.failoverLogs.allow(capability + "/" + providerID) {
			s.logger.Warn("AI 上游调用失败，尝试下一个候选",
				"capability", capability, "provider_id", providerID,
				"provider", settings.ProviderName, "attempted", attempted, "error", redacted.Error())
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no ai candidate available")
	}
	redactedLast := redactedAIError{err: lastErr, text: redactAISecrets(lastErr.Error(), chain...)}
	return "", fmt.Errorf("%w：能力 %s 已尝试 %d 个候选（跳过 %d 个熔断中）全部失败：%w",
		ErrAIUpstreamUnavailable, capability, attempted, skipped, redactedLast)
}

// AIProviderHealth 返回全部供应商的熔断健康快照（管理员可读，绝不包含密钥）。
func (s *Service) AIProviderHealth() []domain.AIProviderHealth {
	providers := s.store.ListAIProviders()
	out := make([]domain.AIProviderHealth, 0, len(providers))
	for _, provider := range providers {
		state, failures, openedUntil, lastError := s.circuit.snapshot(provider.ID)
		// 健康接口的最后错误按该供应商的明文密钥二次脱敏（列表接口本身不含明文）。
		secret := ""
		if internal, ok := s.store.AIProvider(provider.ID); ok {
			secret = internal.APIKey
		}
		out = append(out, domain.AIProviderHealth{
			ID:                  provider.ID,
			Name:                provider.Name,
			Enabled:             provider.Enabled,
			State:               state,
			ConsecutiveFailures: failures,
			OpenedUntil:         openedUntil,
			LastError:           truncateAIErrorText(redactAISecrets(lastError, domain.AISettings{APIKey: secret})),
		})
	}
	return out
}

// settingsForProvider 把供应商配置转换为一次调用的 AISettings，
// 保留全局的 enabled/mode/temperature/max_output_tokens/system_prompt。
func (s *Service) settingsForProvider(provider domain.AIProvider) domain.AISettings {
	return s.settingsForCandidate(s.settingsWithCacheMode(), provider, "")
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
