package app

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

const (
	// visitorMinuteWindow 是"每分钟访客消息数"的滑动窗口。
	visitorMinuteWindow = time.Minute
	// limitDayWindow 是"每天"维度使用的滑动窗口：按最近 24 小时滚动计算。
	// 与自然日统计相比，滚动窗口不会在零点瞬间放量，也不依赖时区配置。
	limitDayWindow = 24 * time.Hour
	// limitCounterCleanupInterval 是内存计数器清理过期 Key 的间隔，避免长期运行内存膨胀。
	limitCounterCleanupInterval = 5 * time.Minute
	// limitTextCacheTTL 是超限提示译文的缓存时间：访客反复触发限额时不再反复调用翻译上游。
	limitTextCacheTTL = 10 * time.Minute
	// limitTextCacheMaxEntries 是译文缓存的最大条目数，超过后清理过期项。
	limitTextCacheMaxEntries = 128

	// limitActionExceeded 是超限审计事件的动作名。
	limitActionExceeded = "ai.limit_exceeded"
)

// windowCounter 是一个进程内的滑动窗口计数器。
//
// 说明：限额计数只保存在当前进程内存中，服务重启后窗口会重建（最多放行一个窗口的量）。
// 这是可接受的取舍：限额用于保护上游与访客体验，不承担计费/审计的强一致职责；
// 审计事件已独立落库，重启不会丢失"曾经超限"的记录。
type windowCounter struct {
	mu  sync.Mutex
	now func() time.Time
	// window 是计数保留时长；recent 是额外统计的近期窗口（0 表示不统计）。
	window time.Duration
	recent time.Duration
	events map[string][]time.Time
	// lastCleanup 记录上次清理时间；首次写入时立即清理一次。
	lastCleanup time.Time
}

// newWindowCounter 创建计数器；now 可在测试中替换以模拟时间流逝。
func newWindowCounter(now func() time.Time, window, recent time.Duration) *windowCounter {
	return &windowCounter{
		now:    now,
		window: window,
		recent: recent,
		events: map[string][]time.Time{},
	}
}

// record 记录一次尝试，返回近期窗口计数与总窗口计数（均包含本次尝试）。
func (c *windowCounter) record(key string) (recentCount, totalCount int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	events := append(c.events[key], now)
	// events 按时间升序追加，从头裁掉超出总窗口的事件。
	cutoff := now.Add(-c.window)
	start := 0
	for start < len(events) && !events[start].After(cutoff) {
		start++
	}
	if start > 0 {
		// 复制一份，避免底层数组被旧切片长期引用。
		events = append([]time.Time(nil), events[start:]...)
	}
	c.events[key] = events
	totalCount = len(events)

	if c.recent > 0 {
		recentCutoff := now.Add(-c.recent)
		for index := len(events) - 1; index >= 0; index-- {
			if !events[index].After(recentCutoff) {
				break
			}
			recentCount++
		}
	} else {
		recentCount = totalCount
	}

	c.cleanupLocked(now)
	return recentCount, totalCount
}

// cleanupLocked 按固定间隔清理已过期的 Key。
func (c *windowCounter) cleanupLocked(now time.Time) {
	if !c.lastCleanup.IsZero() && now.Sub(c.lastCleanup) < limitCounterCleanupInterval {
		return
	}
	c.lastCleanup = now
	cutoff := now.Add(-c.window)
	for key, events := range c.events {
		if len(events) == 0 || !events[len(events)-1].After(cutoff) {
			delete(c.events, key)
		}
	}
}

// limitTracker 汇总两类独立计数：
//   - visitor：按"会话 + 访客 ID + IP"统计访客消息（1 分钟 / 24 小时）；
//   - aiCalls：按会话统计 AI 上游调用次数（24 小时）。
type limitTracker struct {
	visitor *windowCounter
	aiCalls *windowCounter
}

func newLimitTracker() *limitTracker {
	now := time.Now
	return &limitTracker{
		// 两个计数器共享系统时钟；测试可分别替换 now 以模拟时间流逝。
		visitor: newWindowCounter(now, limitDayWindow, visitorMinuteWindow),
		aiCalls: newWindowCounter(now, limitDayWindow, 0),
	}
}

// limitTextTranslation 是超限提示译文缓存条目。
type limitTextTranslation struct {
	text      string
	expiresAt time.Time
}

// limitTextTranslator 缓存"中文超限提示 -> 访客语言"的翻译结果。
// 限额触发后如果每次都同步调用翻译上游，刷屏式请求会反过来放大上游成本，
// 因此对同一段文案按语言缓存一小段时间；翻译失败不缓存，下一次会重试。
type limitTextTranslator struct {
	mu    sync.Mutex
	now   func() time.Time
	cache map[string]limitTextTranslation
}

func newLimitTextTranslator() *limitTextTranslator {
	return &limitTextTranslator{
		now:   time.Now,
		cache: map[string]limitTextTranslation{},
	}
}

func limitTextCacheKey(lang, text string) string {
	return lang + "\x00" + text
}

func (t *limitTextTranslator) get(lang, text string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := limitTextCacheKey(lang, text)
	entry, ok := t.cache[key]
	if !ok {
		return ""
	}
	if !entry.expiresAt.After(t.now()) {
		delete(t.cache, key)
		return ""
	}
	return entry.text
}

func (t *limitTextTranslator) put(lang, text, translated string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	if len(t.cache) >= limitTextCacheMaxEntries {
		for key, entry := range t.cache {
			if !entry.expiresAt.After(now) {
				delete(t.cache, key)
			}
		}
	}
	t.cache[limitTextCacheKey(lang, text)] = limitTextTranslation{
		text:      translated,
		expiresAt: now.Add(limitTextCacheTTL),
	}
}

// visitorLimitKey 构造访客消息的计数维度：会话 + 访客 ID + IP。
// 三者共同作为 Key，保证不同会话、不同访客（即使共用 IP）互不影响。
func visitorLimitKey(conversation domain.Conversation) string {
	return strings.Join([]string{conversation.ID, conversation.VisitorID, conversation.VisitorIP}, "\x00")
}

// checkVisitorMessageLimit 在访客消息落库、翻译与 AI 触发之前执行频率限额检查。
// 超限时返回 *store.RateLimitError（errors.Is(err, store.ErrRateLimited) 为 true），
// 调用方不得继续写入任何消息。
func (s *Service) checkVisitorMessageLimit(conversation domain.Conversation) error {
	limits := s.store.AILimits()
	if !limits.Enabled {
		return nil
	}
	if limits.VisitorMessagesPerMinute <= 0 && limits.VisitorMessagesPerDay <= 0 {
		return nil
	}

	// 本次尝试也计入窗口：被拒绝的请求同样会消耗资源，计入后可避免"被拒后立刻恢复"的抖动。
	minuteCount, dayCount := s.limitTracker.visitor.record(visitorLimitKey(conversation))
	var dimension string
	var limit, count int
	switch {
	case limits.VisitorMessagesPerMinute > 0 && minuteCount > limits.VisitorMessagesPerMinute:
		dimension, limit, count = domain.AILimitDimensionVisitorMinute, limits.VisitorMessagesPerMinute, minuteCount
	case limits.VisitorMessagesPerDay > 0 && dayCount > limits.VisitorMessagesPerDay:
		dimension, limit, count = domain.AILimitDimensionVisitorDay, limits.VisitorMessagesPerDay, dayCount
	default:
		return nil
	}

	// 文案按访客当前回复语言下发；翻译失败回退中文原文。
	text := s.limitExceedText(conversationReplyLang(conversation), limits.ExceedMessage)
	s.recordLimitExceeded(conversation, dimension, count, limit)
	return &store.RateLimitError{
		Dimension: dimension,
		Limit:     limit,
		Count:     count,
		Message:   text,
	}
}

// recordLimitExceeded 写入超限审计事件。
// 只记录维度、计数、限额与必要的会话标识；不记录访客消息内容或任何密钥。
func (s *Service) recordLimitExceeded(conversation domain.Conversation, dimension string, count, limit int) {
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorKind:  domain.AccountVisitor,
		ActorID:    conversation.VisitorID,
		Action:     limitActionExceeded,
		Resource:   "conversation",
		ResourceID: conversation.ID,
		IPAddress:  conversation.VisitorIP,
		Description: fmt.Sprintf("触发 AI 限额：dimension=%s count=%d limit=%d",
			dimension, count, limit),
		CreatedAt: time.Now().UTC(),
	})
}

// limitExceedText 返回按访客语言下发的超限提示。
// 配置为空时使用内置中文默认文案；访客为中文时直接返回，其它语言复用既有翻译链路，
// 翻译失败回退中文原文（保证访客至少能看到明确提示）。
func (s *Service) limitExceedText(visitorLang, configured string) string {
	text := domain.LimitExceedMessageText(configured)
	lang := ai.NormalizeLang(visitorLang, ai.LangZh)
	if lang == ai.LangZh {
		return text
	}
	if cached := s.limitTexts.get(lang, text); cached != "" {
		return cached
	}
	translations := s.translateMessage(ai.LangZh, lang, domain.MessageText, text)
	if translated := strings.TrimSpace(translations[lang]); translated != "" {
		s.limitTexts.put(lang, text, translated)
		return translated
	}
	return text
}

// aiReplyCharLimit 读取当前生效的单次回复字数上限；总开关关闭或配置为 0 时返回 0（不限制）。
func (s *Service) aiReplyCharLimit() int {
	limits := s.store.AILimits()
	if !limits.Enabled {
		return 0
	}
	return limits.AIReplyMaxChars
}

// truncateAIReply 把 AI 回复限制在 maxChars（按 rune 计）以内：
//   - 优先截断到窗口内最后一个句末标点（。！？.!?；;\n），保留完整句子；
//   - 找不到句末标点时硬截断，并在末尾保留一个省略号；
//   - maxChars<=0 表示不限制，原样返回。
//
// 返回值长度保证不超过 maxChars。
func truncateAIReply(text string, maxChars int) string {
	if maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}

	cut := -1
	for index := 0; index < maxChars; index++ {
		switch runes[index] {
		case '。', '！', '？', '.', '!', '?', '；', ';', '\n':
			cut = index
		}
	}
	if cut >= 0 {
		// 去掉标点后的空白/换行，避免出现"句号 + 空行"的截断痕迹。
		result := strings.TrimRight(string(runes[:cut+1]), " \t\r\n")
		if result != "" {
			return result
		}
	}
	if maxChars == 1 {
		return string(runes[:1])
	}
	return string(runes[:maxChars-1]) + "…"
}
