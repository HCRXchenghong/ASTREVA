package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/geo"
	"customer-service/backend/internal/store"
)

// ===== 访客画像（P5）：生成与更新 =====
//
// 采集时机：
//   - 建会话：记录 ip_first/ip_last、visits、初始语言、地理与国籍推测；
//   - 每次访客消息：语言计数、方言标记、code-switching、地理（走 IP 缓存）；
//   - AI 摘要：每 ai_summary_every_messages 条访客消息异步触发一次。
//
// 隐私：enabled=false 时不采集、不返回，也不会发起任何第三方请求。

const (
	// profileCodeSwitchingWindow 是 code-switching 判定的观察窗口（最近 N 条访客消息）。
	profileCodeSwitchingWindow = 10
	// profileSummaryHistoryLimit 是喂给 AI 的最近对话条数上限。
	profileSummaryHistoryLimit = 20
	// profileSummaryMaxEvidence 是单次摘要保留的证据条数上限。
	profileSummaryMaxEvidence = 20
	// profileSummaryTimeout 是摘要任务的整体超时上限。
	profileSummaryTimeout = 30 * time.Second
)

// visitorProfileRuntime 是画像的进程内运行时（地理解析器 + 本地库告警去重）。
type visitorProfileRuntime struct {
	geo *geo.Resolver

	mu          sync.Mutex
	loggedError string
}

func newVisitorProfileRuntime() *visitorProfileRuntime {
	return &visitorProfileRuntime{geo: geo.NewResolver()}
}

// VisitorProfileSettings 返回画像配置（第三方密钥只回末四位掩码）。
func (s *Service) VisitorProfileSettings() domain.VisitorProfileSettings {
	return s.store.VisitorProfileSettings()
}

// UpdateVisitorProfileSettings 局部更新画像配置（nil=不改，third_party_key 空串=清除）。
func (s *Service) UpdateVisitorProfileSettings(patch domain.VisitorProfileSettingsPatch) (domain.VisitorProfileSettings, error) {
	return s.store.UpdateVisitorProfileSettings(patch)
}

// VisitorProfiles 返回画像列表（含总数）；画像关闭时返回 ErrNotFound。
func (s *Service) VisitorProfiles(query domain.VisitorProfileQuery) ([]domain.VisitorProfileView, int, error) {
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled {
		return nil, 0, store.ErrNotFound
	}
	profiles, total := s.store.VisitorProfiles(query)
	views := make([]domain.VisitorProfileView, 0, len(profiles))
	for _, profile := range profiles {
		views = append(views, s.visitorProfileView(profile, settings, false))
	}
	return views, total, nil
}

// VisitorProfileDetail 返回单个访客画像详情（含证据消息与会话列表）。
func (s *Service) VisitorProfileDetail(visitorID string) (domain.VisitorProfileView, error) {
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled {
		return domain.VisitorProfileView{}, store.ErrNotFound
	}
	profile, ok := s.store.VisitorProfile(visitorID)
	if !ok {
		return domain.VisitorProfileView{}, store.ErrNotFound
	}
	return s.visitorProfileView(profile, settings, true), nil
}

// VisitorProfileForConversation 返回客服会话侧的画像卡数据。
// 画像关闭或该访客暂无画像时返回 ErrNotFound（HTTP 404），由调用方负责越权校验。
func (s *Service) VisitorProfileForConversation(conversationID string) (domain.VisitorProfileView, error) {
	conversation, ok := s.store.Conversation(strings.TrimSpace(conversationID))
	if !ok {
		return domain.VisitorProfileView{}, store.ErrNotFound
	}
	return s.VisitorProfileDetail(conversation.VisitorID)
}

// RefreshVisitorProfile 立即重算画像（地理 + 国籍 + code-switching + AI 摘要）。
// AI 摘要失败只记日志、保留旧值，接口仍返回当前画像。
func (s *Service) RefreshVisitorProfile(ctx context.Context, visitorID string) (domain.VisitorProfileView, error) {
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled {
		return domain.VisitorProfileView{}, store.ErrNotFound
	}
	profile, ok := s.store.VisitorProfile(visitorID)
	if !ok {
		return domain.VisitorProfileView{}, store.ErrNotFound
	}
	// 地理重算：优先走进程内 IP 缓存，缓存失效才重新请求第三方。
	geoRecord, geoOK := s.resolveVisitorGeo(ctx, settings, profile.IPLast, nil)
	recent := s.visitorRecentLanguageCodes(visitorID, profileCodeSwitchingWindow, "", "")
	geoBasis := ""
	if geoOK {
		geoBasis = geoRecord.Basis
	}
	if _, err := s.store.UpdateVisitorProfile(visitorID, func(current *domain.VisitorProfile) error {
		if geoOK {
			applyVisitorGeo(current, geoRecord)
		}
		if len(recent) > 0 {
			current.CodeSwitching = hasCodeSwitching(recent)
		}
		refreshVisitorNationality(current, geoBasis)
		return nil
	}); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Warn("刷新访客画像失败", "visitor_id", visitorID, "error", err)
	}
	if err := s.runVisitorProfileSummary(ctx, visitorID); err != nil {
		s.logger.Warn("重算访客画像摘要失败", "visitor_id", visitorID, "error", err)
	}
	updated, ok := s.store.VisitorProfile(visitorID)
	if !ok {
		return domain.VisitorProfileView{}, store.ErrNotFound
	}
	return s.visitorProfileView(updated, settings, true), nil
}

// visitorProfileView 把画像转换为接口视图：按 show_full_ip 决定是否脱敏 IP。
func (s *Service) visitorProfileView(profile domain.VisitorProfile, settings domain.VisitorProfileSettings, withDetail bool) domain.VisitorProfileView {
	lastSeen := profile.UpdatedAt
	view := domain.VisitorProfileView{
		VisitorID:             profile.VisitorID,
		Country:               profile.Country,
		CountryCode:           profile.CountryCode,
		Region:                profile.Region,
		City:                  profile.City,
		Timezone:              profile.Timezone,
		NationalityGuess:      profile.NationalityGuess,
		NationalityConfidence: profile.NationalityConfidence,
		NationalityBasis:      emptyIfNil(profile.NationalityBasis),
		Languages:             emptyLanguageCounts(profile.Languages),
		Dialect:               profile.Dialect,
		CodeSwitching:         profile.CodeSwitching,
		Summary:               profile.Summary,
		Tags:                  emptyIfNil(profile.Tags),
		Interests:             emptyIfNil(profile.Interests),
		Visits:                profile.Visits,
		LastPath:              profile.LastPath,
		LastSeenAt:            &lastSeen,
		IPLast:                profile.IPLast,
	}
	if !settings.ShowFullIP {
		view.IPLast = domain.MaskIP(profile.IPLast)
	}
	if withDetail {
		evidence := emptyEvidence(profile.Evidence)
		conversations := s.visitorProfileConversations(profile.VisitorID)
		view.Evidence = &evidence
		view.Conversations = &conversations
	}
	return view
}

// visitorProfileConversations 返回画像详情里的会话摘要（创建时间倒序）。
func (s *Service) visitorProfileConversations(visitorID string) []domain.VisitorProfileConversation {
	conversations := s.store.ConversationsForVisitor(visitorID)
	out := make([]domain.VisitorProfileConversation, 0, len(conversations))
	for _, conversation := range conversations {
		out = append(out, domain.VisitorProfileConversation{
			ConversationID: conversation.ID,
			Status:         string(conversation.Status),
			CreatedAt:      conversation.CreatedAt,
			LastMessageAt:  conversation.LastMessageAt,
		})
	}
	return out
}

// ===== 采集 =====

// CreateVisitorConversationWithGeo 与 CreateVisitorConversation 相同，
// 额外带上访客请求头：CDN 注入的国家码（CF-IPCountry 等）是最优先的地理来源。
func (s *Service) CreateVisitorConversationWithGeo(ip, source, lang, ipSource, remark string, header http.Header) (domain.VisitorSession, error) {
	session, err := s.CreateVisitorConversation(ip, source, lang, ipSource, remark)
	if err != nil {
		return domain.VisitorSession{}, err
	}
	s.trackVisitorVisit(session, header)
	return session, nil
}

// trackVisitorVisit 异步记录一次访客到访（建会话）：绝不阻塞建会话主流程。
func (s *Service) trackVisitorVisit(session domain.VisitorSession, header http.Header) {
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled {
		// 画像关闭：不采集、不发起第三方请求。
		return
	}
	conversation := session.Conversation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), profileSummaryTimeout)
		defer cancel()
		geoRecord, geoOK := s.resolveVisitorGeo(ctx, settings, conversation.VisitorIP, header)
		visitorLang := ai.NormalizeOptionalLang(conversation.VisitorLang)
		geoBasis := ""
		if geoOK {
			geoBasis = geoRecord.Basis
		}
		if _, err := s.store.UpdateVisitorProfile(conversation.VisitorID, func(profile *domain.VisitorProfile) error {
			if profile.Languages == nil {
				profile.Languages = map[string]int{}
			}
			if visitorLang != "" {
				profile.Languages[visitorLang]++
			}
			if geoOK {
				applyVisitorGeo(profile, geoRecord)
			}
			if profile.IPFirst == "" {
				profile.IPFirst = conversation.VisitorIP
			}
			profile.IPLast = conversation.VisitorIP
			profile.Visits++
			refreshVisitorNationality(profile, geoBasis)
			return nil
		}); err != nil {
			s.logger.Warn("记录访客到访失败", "visitor_id", conversation.VisitorID, "error", err)
		}
	}()
}

// trackVisitorMessage 异步更新访客画像；达到摘要阈值时再异步触发一次 AI 摘要。
// 所有网络与存储调用都在存储锁之外完成，避免阻塞消息主流程。
func (s *Service) trackVisitorMessage(conversation domain.Conversation, message domain.Message, detectedTag string) {
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled {
		// 画像关闭：不采集、不发起第三方请求。
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), profileSummaryTimeout)
		defer cancel()
		_, due, err := s.applyVisitorMessage(ctx, settings, conversation, message, detectedTag)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				s.logger.Warn("更新访客画像失败", "visitor_id", conversation.VisitorID, "error", err)
			}
			return
		}
		if due {
			if err := s.runVisitorProfileSummary(ctx, conversation.VisitorID); err != nil {
				s.logger.Warn("生成访客画像摘要失败", "visitor_id", conversation.VisitorID, "error", err)
			}
		}
	}()
}

// applyVisitorMessage 把一条访客消息合并进画像，并返回是否到达 AI 摘要触发阈值。
// 该方法是同步的（便于测试与 refresh 复用），生产路径由 trackVisitorMessage 异步调用。
func (s *Service) applyVisitorMessage(ctx context.Context, settings domain.VisitorProfileSettings, conversation domain.Conversation, message domain.Message, detectedTag string) (domain.VisitorProfile, bool, error) {
	if !settings.Enabled {
		return domain.VisitorProfile{}, false, store.ErrNotFound
	}
	if strings.TrimSpace(conversation.VisitorID) == "" {
		return domain.VisitorProfile{}, false, store.ErrInvalidInput
	}
	// 网络调用（第三方 IP 库）在存储锁之外完成，命中缓存时零成本。
	geoRecord, geoOK := s.resolveVisitorGeo(ctx, settings, conversation.VisitorIP, nil)
	// code-switching 窗口：跨会话取该访客最近 N 条访客消息的语种。
	recent := s.visitorRecentLanguageCodes(conversation.VisitorID, profileCodeSwitchingWindow, message.ServerMsgID, detectedTag)
	languageCode := ai.NormalizeOptionalLang(detectedTag)
	dialect := profileDialectTag(detectedTag)
	geoBasis := ""
	if geoOK {
		geoBasis = geoRecord.Basis
	}

	updated, err := s.store.UpdateVisitorProfile(conversation.VisitorID, func(profile *domain.VisitorProfile) error {
		if profile.Languages == nil {
			profile.Languages = map[string]int{}
		}
		if languageCode != "" {
			profile.Languages[languageCode]++
		}
		if dialect != "" {
			profile.Dialect = dialect
		}
		if len(recent) > 0 {
			profile.CodeSwitching = hasCodeSwitching(recent)
		}
		if geoOK {
			applyVisitorGeo(profile, geoRecord)
		}
		if profile.IPFirst == "" {
			profile.IPFirst = conversation.VisitorIP
		}
		if strings.TrimSpace(conversation.VisitorIP) != "" {
			profile.IPLast = conversation.VisitorIP
		}
		refreshVisitorNationality(profile, geoBasis)
		return nil
	})
	if err != nil {
		return domain.VisitorProfile{}, false, err
	}
	total := 0
	for _, count := range updated.Languages {
		if count > 0 {
			total += count
		}
	}
	due := settings.AISummaryEnabled && total > 0 &&
		settings.AISummaryEveryMessages > 0 && total%settings.AISummaryEveryMessages == 0
	return updated, due, nil
}

// resolveVisitorGeo 按三级顺序解析地理：CDN 头 → 本地 CSV → 第三方。
// 本地库加载失败只记一次 warning（不影响会话），mmdb 配置返回明确错误。
func (s *Service) resolveVisitorGeo(ctx context.Context, settings domain.VisitorProfileSettings, ip string, header http.Header) (domain.GeoRecord, bool) {
	if s.profile == nil || s.profile.geo == nil {
		return domain.GeoRecord{}, false
	}
	if err := s.profile.geo.LocalError(settings.GeoIPDir); err != nil && settings.GeoIPDir != "" {
		s.logProfileGeoError(err)
	}
	return s.profile.geo.Resolve(ctx, geo.Config{
		GeoIPDir:            settings.GeoIPDir,
		ThirdPartyURL:       settings.ThirdPartyURL,
		ThirdPartyKey:       settings.ThirdPartyKey,
		ThirdPartyTimeoutMS: settings.ThirdPartyTimeoutMS,
		CacheTTL:            time.Duration(settings.CacheTTLHours) * time.Hour,
	}, ip, header)
}

// logProfileGeoError 对同一错误只记一次 warning，避免刷屏。
func (s *Service) logProfileGeoError(err error) {
	message := err.Error()
	s.profile.mu.Lock()
	alreadyLogged := s.profile.loggedError == message
	if !alreadyLogged {
		s.profile.loggedError = message
	}
	s.profile.mu.Unlock()
	if !alreadyLogged {
		s.logger.Warn("本地 GeoLite2 库不可用，已降级", "error", err)
	}
}

// applyVisitorGeo 把解析结果写入画像：只覆盖非空字段，绝不清空已有地理信息。
func applyVisitorGeo(profile *domain.VisitorProfile, record domain.GeoRecord) {
	if strings.TrimSpace(record.Country) != "" {
		profile.Country = record.Country
	}
	if code := domain.NormalizeCountryCode(record.CountryCode); code != "" {
		profile.CountryCode = code
	}
	if strings.TrimSpace(record.Region) != "" {
		profile.Region = record.Region
	}
	if strings.TrimSpace(record.City) != "" {
		profile.City = record.City
	}
	if strings.TrimSpace(record.Timezone) != "" {
		profile.Timezone = record.Timezone
	}
}

// refreshVisitorNationality 重算国籍推测（只写"推测"字段，不覆盖地理字段）。
// geoBasis 是本次地理解析的来源层级；为空时沿用画像里已记录的依据。
func refreshVisitorNationality(profile *domain.VisitorProfile, geoBasis string) {
	basis := strings.TrimSpace(geoBasis)
	if basis == "" {
		basis = visitorGeoBasis(profile.NationalityBasis)
	}
	record := domain.GeoRecord{
		Country:     profile.Country,
		CountryCode: profile.CountryCode,
		Timezone:    profile.Timezone,
		Basis:       basis,
	}
	guess, confidence, nationalBasis := domain.GuessNationality(record, profile.Languages, profile.Timezone)
	profile.NationalityGuess = guess
	profile.NationalityConfidence = confidence
	profile.NationalityBasis = nationalBasis
}

// visitorGeoBasis 从既有依据里取出地理来源层级（用于重算时保持来源稳定）。
func visitorGeoBasis(basis []string) string {
	for _, value := range basis {
		switch value {
		case domain.GeoBasisCDN, domain.GeoBasisLocal, domain.GeoBasisThirdParty:
			return value
		}
	}
	return domain.GeoBasisCDN
}

// profileDialectTag 从消息检测标签里提取方言标记：只有带地区/方言后缀
// （如 es-419、ar-Gulf、pt-BR、en-GB、zh-Hant）才记录，zh-Hans 回落标准语。
func profileDialectTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" || !strings.Contains(tag, "-") || strings.EqualFold(tag, "zh-Hans") {
		return ""
	}
	return tag
}

// hasCodeSwitching 判断最近若干条访客消息里是否出现 ≥2 种语言。
func hasCodeSwitching(languages []string) bool {
	seen := map[string]bool{}
	for _, lang := range languages {
		code := ai.NormalizeOptionalLang(lang)
		if code == "" {
			continue
		}
		seen[code] = true
		if len(seen) >= 2 {
			return true
		}
	}
	return false
}

// visitorRecentLanguageCodes 读取该访客最近 limit 条访客文本消息的语种。
// includeMessageID 用于确认"当前这条消息"是否已入库；未入库时把 includeTag 追加到窗口末尾。
func (s *Service) visitorRecentLanguageCodes(visitorID string, limit int, includeMessageID, includeTag string) []string {
	if limit <= 0 {
		limit = profileCodeSwitchingWindow
	}
	conversations := s.store.ConversationsForVisitor(visitorID)
	// 按创建时间正序拼接各会话消息，保证窗口是真实的时间顺序。
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].CreatedAt.Before(conversations[j].CreatedAt)
	})
	codes := make([]string, 0, limit+1)
	sawInclude := false
	for _, conversation := range conversations {
		for _, message := range s.store.Messages(conversation.ID) {
			if message.SenderType != domain.SenderVisitor || message.MessageType != domain.MessageText {
				continue
			}
			if includeMessageID != "" && message.ServerMsgID == includeMessageID {
				sawInclude = true
			}
			if code := ai.NormalizeOptionalLang(message.DetectedLang); code != "" {
				codes = append(codes, code)
			}
		}
	}
	if !sawInclude && strings.TrimSpace(includeTag) != "" {
		codes = append(codes, includeTag)
	}
	if len(codes) > limit {
		codes = codes[len(codes)-limit:]
	}
	return codes
}

// ===== AI 摘要 =====

// visitorProfileSummarySystemPrompt 要求上游输出严格 JSON（解析失败不覆盖旧值）。
const visitorProfileSummarySystemPrompt = `你是客户画像分析助手。请根据客服与访客的最近对话以及已有画像，输出严格 JSON。

输出格式（只输出 JSON 本体，不要 markdown 代码块、不要任何解释文字）：
{"summary":"一段简洁的中文画像摘要","tags":["标签"],"interests":["关注点"],"evidence":["引用的消息 server_msg_id"]}

要求：
1. summary 不超过 200 字，聚焦行业、需求、关注产品、预算与工期线索、情绪、是否决策人；
2. tags 与 interests 各不超过 8 项，使用简短中文词；
3. evidence 只允许引用输入里出现过的 server_msg_id，不要编造；
4. 信息不足时给出保守结论，不要臆测。`

// visitorProfileSummaryPayload 是 AI 摘要的严格 JSON 结构。
type visitorProfileSummaryPayload struct {
	Summary   string          `json:"summary"`
	Tags      []string        `json:"tags"`
	Interests []string        `json:"interests"`
	Evidence  json.RawMessage `json:"evidence"`
}

// runVisitorProfileSummary 调用 reception 能力绑定的上游生成画像摘要。
// 解析失败/上游失败一律保留旧值（只记日志），返回错误供调用方记录。
func (s *Service) runVisitorProfileSummary(ctx context.Context, visitorID string) error {
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled || !settings.AISummaryEnabled {
		return nil
	}
	profile, ok := s.store.VisitorProfile(visitorID)
	if !ok {
		return store.ErrNotFound
	}
	transcript, known := s.visitorProfileTranscript(visitorID, profileSummaryHistoryLimit)
	if strings.TrimSpace(transcript) == "" {
		return nil
	}
	aiSettings := s.aiSettingsFor(domain.AICapabilityReception)
	if strings.TrimSpace(aiSettings.APIKey) == "" {
		// 未配置上游：静默跳过，不影响会话与接口。
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, profileSummaryTimeout)
	defer cancel()
	raw, err := s.ai.CompleteJSON(ctx, aiSettings, visitorProfileSummarySystemPrompt, buildVisitorProfileSummaryInput(profile, transcript))
	if err != nil {
		return err
	}
	payload, err := parseVisitorProfileSummaryJSON(raw)
	if err != nil {
		// 解析失败：不覆盖旧摘要（只记日志）。
		s.logger.Warn("访客画像摘要 JSON 解析失败，保留旧值", "visitor_id", visitorID, "error", err)
		return nil
	}
	evidence := make([]domain.VisitorProfileEvidence, 0, len(payload.EvidenceIDs))
	for _, messageID := range payload.EvidenceIDs {
		if message, ok := known[messageID]; ok {
			evidence = append(evidence, domain.VisitorProfileEvidence{
				MessageID: message.ServerMsgID,
				Text:      truncateProfileEvidence(message.Content),
				CreatedAt: message.CreatedAt,
			})
		}
	}
	if len(evidence) > profileSummaryMaxEvidence {
		evidence = evidence[:profileSummaryMaxEvidence]
	}
	now := time.Now().UTC()
	if _, err := s.store.UpdateVisitorProfile(visitorID, func(current *domain.VisitorProfile) error {
		current.Summary = payload.Summary
		current.Tags = payload.Tags
		current.Interests = payload.Interests
		current.Evidence = evidence
		current.AIUpdatedAt = &now
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// visitorProfileTranscript 拼接该访客最近 limit 条对话（含 server_msg_id 便于引用证据），
// 同时返回 server_msg_id -> 消息 的映射，用于把证据 ID 还原成真实消息。
func (s *Service) visitorProfileTranscript(visitorID string, limit int) (string, map[string]domain.Message) {
	conversations := s.store.ConversationsForVisitor(visitorID)
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].CreatedAt.Before(conversations[j].CreatedAt)
	})
	type entry struct {
		message domain.Message
	}
	entries := make([]entry, 0, limit)
	for _, conversation := range conversations {
		for _, message := range s.store.Messages(conversation.ID) {
			switch {
			case message.SenderType == domain.SenderVisitor && message.MessageType == domain.MessageText:
			case message.SenderType == domain.SenderAgent && message.MessageType == domain.MessageText:
			case message.SenderType == domain.SenderAI && message.MessageType == domain.MessageAIText:
			default:
				continue
			}
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			entries = append(entries, entry{message: message})
		}
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	known := map[string]domain.Message{}
	var builder strings.Builder
	for _, item := range entries {
		role := "访客"
		switch item.message.SenderType {
		case domain.SenderAgent:
			role = "客服"
		case domain.SenderAI:
			role = "AI"
		}
		known[item.message.ServerMsgID] = item.message
		builder.WriteString("[" + item.message.ServerMsgID + "] " + role + "：" + item.message.Content + "\n")
	}
	return strings.TrimSpace(builder.String()), known
}

// buildVisitorProfileSummaryInput 组装摘要输入：最近对话 + 已有画像。
func buildVisitorProfileSummaryInput(profile domain.VisitorProfile, transcript string) string {
	var builder strings.Builder
	builder.WriteString("【已有画像】\n")
	builder.WriteString("国家/地区：" + firstNonEmpty(profile.Country, "未知") + " " + profile.CountryCode + "\n")
	builder.WriteString("时区：" + firstNonEmpty(profile.Timezone, "未知") + "\n")
	builder.WriteString("语言计数：" + formatLanguageCounts(profile.Languages) + "\n")
	builder.WriteString("方言：" + firstNonEmpty(profile.Dialect, "无") + "；代码切换：" + boolText(profile.CodeSwitching) + "\n")
	builder.WriteString("已有摘要：" + firstNonEmpty(profile.Summary, "无") + "\n")
	builder.WriteString("已有标签：" + strings.Join(profile.Tags, "、") + "\n")
	builder.WriteString("\n【最近对话】\n")
	builder.WriteString(transcript)
	return builder.String()
}

// parseVisitorProfileSummaryJSON 严格解析摘要 JSON：容忍 markdown 代码块包裹，
// 但字段缺失/类型错误/空摘要一律视为解析失败（调用方保留旧值）。
func parseVisitorProfileSummaryJSON(raw string) (visitorProfileSummaryResult, error) {
	text := strings.TrimSpace(raw)
	if index := strings.Index(text, "{"); index >= 0 {
		if end := strings.LastIndex(text, "}"); end > index {
			text = text[index : end+1]
		}
	}
	var payload visitorProfileSummaryPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return visitorProfileSummaryResult{}, err
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		return visitorProfileSummaryResult{}, errors.New("summary 为空")
	}
	result := visitorProfileSummaryResult{
		Summary:     summary,
		Tags:        cleanProfileStrings(payload.Tags, 8),
		Interests:   cleanProfileStrings(payload.Interests, 8),
		EvidenceIDs: parseProfileEvidenceIDs(payload.Evidence),
	}
	return result, nil
}

type visitorProfileSummaryResult struct {
	Summary     string
	Tags        []string
	Interests   []string
	EvidenceIDs []string
}

// parseProfileEvidenceIDs 兼容 ["msg_1"] 与 [{"message_id":"msg_1"}] 两种证据写法。
func parseProfileEvidenceIDs(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return cleanProfileStrings(asStrings, profileSummaryMaxEvidence)
	}
	var asObjects []struct {
		MessageID string `json:"message_id"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		ids := make([]string, 0, len(asObjects))
		for _, item := range asObjects {
			ids = append(ids, firstNonEmpty(item.MessageID, item.ID))
		}
		return cleanProfileStrings(ids, profileSummaryMaxEvidence)
	}
	return nil
}

// cleanProfileStrings 去空、去重并限制条数。
func cleanProfileStrings(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func truncateProfileEvidence(text string) string {
	const maxRunes = 200
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "…"
}

func formatLanguageCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "无"
	}
	langs := domain.SortedLanguages(counts)
	parts := make([]string, 0, len(langs))
	for _, lang := range langs {
		parts = append(parts, lang+"="+itoaProfile(counts[lang]))
	}
	return strings.Join(parts, "、")
}

func boolText(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func itoaProfile(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buf[index] = '-'
	}
	return string(buf[index:])
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func emptyEvidence(values []domain.VisitorProfileEvidence) []domain.VisitorProfileEvidence {
	if values == nil {
		return []domain.VisitorProfileEvidence{}
	}
	return values
}

func emptyLanguageCounts(counts map[string]int) map[string]int {
	if counts == nil {
		return map[string]int{}
	}
	return counts
}
