package app

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/knowledge"
	"customer-service/backend/internal/store"
)

// ===== 知识库倒排索引的进程内缓存（每个知识库一份）=====
//
// 设计要点：
//   - 懒加载：某知识库第一次参与检索时才从存储读取并建索引，之后命中缓存；
//   - TTL：默认 60s（KNOWLEDGE_INDEX_TTL_SECONDS 可覆盖），过期后下次检索重建；
//   - 写失效：知识库/条目的增删改（含批量导入）都会让对应库的索引失效，
//     通过 knowledgeIndexStore 包装 Store 实现，因此所有经由 service.Store() 的写路径
//     都会刷新索引，无需在每个 HTTP handler 里手动调用；
//   - 内存上限：KNOWLEDGE_INDEX_MAX_ENTRIES（默认 50000）限制缓存条目总数，
//     超出后按最近使用时间（LRU）淘汰旧库；单个库自身就超过上限时不做索引缓存，
//     调用方回退为"全表 BM25"（结果不变，只是放弃加速）；
//   - 并发安全：sync.RWMutex。读缓存走 RLock，重建/失效走 Lock。**读取底层存储时
//     绝不持有索引锁**，避免与存储自身锁形成锁序问题。
const (
	// defaultKnowledgeIndexTTLSeconds 是索引条目的默认有效期（秒）。
	defaultKnowledgeIndexTTLSeconds = 60
	// defaultKnowledgeIndexMaxEntries 是进程内索引缓存的条目总数上限。
	defaultKnowledgeIndexMaxEntries = 50000
	// knowledgeIndexTTLEnvKey / knowledgeIndexMaxEntriesEnvKey 是覆盖上述默认值的环境变量。
	knowledgeIndexTTLEnvKey        = "KNOWLEDGE_INDEX_TTL_SECONDS"
	knowledgeIndexMaxEntriesEnvKey = "KNOWLEDGE_INDEX_MAX_ENTRIES"
)

// knowledgeIndex 是"知识库 ID -> 倒排索引"的进程内缓存。
type knowledgeIndex struct {
	ttl        time.Duration
	maxEntries int

	mu    sync.RWMutex
	bases map[string]*indexedKnowledgeBase
}

// indexedKnowledgeBase 是缓存中的单个知识库索引及其生命周期信息。
type indexedKnowledgeBase struct {
	index    *knowledge.BaseIndex
	loadedAt time.Time
	lastUsed time.Time
}

// knowledgeEntryLister 是索引构建所需的最小存储能力（Postgres 用
// WHERE enabled AND base_id = ANY($1) 缩小到目标库）。
type knowledgeEntryLister interface {
	KnowledgeEntriesForBases(baseIDs []string) []domain.KnowledgeEntry
}

// newKnowledgeIndex 按环境变量构造索引缓存。
func newKnowledgeIndex() *knowledgeIndex {
	return &knowledgeIndex{
		ttl:        knowledgeIndexTTL(),
		maxEntries: knowledgeIndexMaxEntries(),
		bases:      map[string]*indexedKnowledgeBase{},
	}
}

// knowledgeIndexTTL 解析索引有效期：<=0 表示禁用缓存（每次都重建，正确性优先）。
func knowledgeIndexTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv(knowledgeIndexTTLEnvKey))
	if raw == "" {
		return defaultKnowledgeIndexTTLSeconds * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return defaultKnowledgeIndexTTLSeconds * time.Second
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// knowledgeIndexMaxEntries 解析缓存条目上限：<=0 表示不限。
func knowledgeIndexMaxEntries() int {
	raw := strings.TrimSpace(os.Getenv(knowledgeIndexMaxEntriesEnvKey))
	if raw == "" {
		return defaultKnowledgeIndexMaxEntries
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultKnowledgeIndexMaxEntries
	}
	return value
}

// base 返回某个知识库的倒排索引，未缓存或已过期时懒加载。
// 返回 nil 表示该库不应被索引缓存（条目数超过单库上限），调用方需回退全表检索。
func (x *knowledgeIndex) base(lister knowledgeEntryLister, baseID string, now time.Time) *knowledge.BaseIndex {
	if x == nil {
		return nil
	}
	x.mu.RLock()
	cached, ok := x.bases[baseID]
	if ok && !x.expired(cached, now) {
		x.mu.RUnlock()
		x.touch(baseID, now)
		return cached.index
	}
	x.mu.RUnlock()

	// 读存储不持锁：存储层有自己的锁，避免锁序耦合。
	entries := lister.KnowledgeEntriesForBases([]string{baseID})
	if x.maxEntries > 0 && len(entries) > x.maxEntries {
		// 单库超上限：不缓存，交由调用方回退（结果仍然正确）。
		return nil
	}
	built := knowledge.NewBaseIndex(entries)

	x.mu.Lock()
	x.bases[baseID] = &indexedKnowledgeBase{index: built, loadedAt: now, lastUsed: now}
	x.evictLocked()
	x.mu.Unlock()
	return built
}

// expired 判断缓存项是否已过期；ttl<=0 表示每次都视为过期（禁用缓存）。
func (x *knowledgeIndex) expired(entry *indexedKnowledgeBase, now time.Time) bool {
	if x.ttl <= 0 {
		return true
	}
	return now.Sub(entry.loadedAt) >= x.ttl
}

// touch 更新最近使用时间，供 LRU 淘汰使用。
func (x *knowledgeIndex) touch(baseID string, now time.Time) {
	x.mu.Lock()
	if entry, ok := x.bases[baseID]; ok {
		entry.lastUsed = now
	}
	x.mu.Unlock()
}

// evictLocked 在上限内保留最近使用的库；调用方必须已持有写锁。
func (x *knowledgeIndex) evictLocked() {
	if x.maxEntries <= 0 {
		return
	}
	total := 0
	for _, entry := range x.bases {
		total += entry.index.DocumentCount()
	}
	for total > x.maxEntries && len(x.bases) > 1 {
		oldestID := ""
		var oldest time.Time
		for baseID, entry := range x.bases {
			if oldestID == "" || entry.lastUsed.Before(oldest) {
				oldestID, oldest = baseID, entry.lastUsed
			}
		}
		if oldestID == "" {
			return
		}
		total -= x.bases[oldestID].index.DocumentCount()
		delete(x.bases, oldestID)
	}
}

// invalidate 失效单个知识库的索引（条目新增/库本身变更时使用）。
func (x *knowledgeIndex) invalidate(baseID string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	delete(x.bases, strings.TrimSpace(baseID))
	x.mu.Unlock()
}

// invalidateAll 失效全部缓存（知识库增删、条目更新/删除等无法确定影响范围的操作）。
func (x *knowledgeIndex) invalidateAll() {
	if x == nil {
		return
	}
	x.mu.Lock()
	x.bases = map[string]*indexedKnowledgeBase{}
	x.mu.Unlock()
}

// search 用倒排索引取候选并汇总语料统计。
//
// 返回值 ok=false 表示范围中存在无法索引缓存的库，调用方应回退到全表 BM25。
func (x *knowledgeIndex) search(lister knowledgeEntryLister, baseIDs, queryTokens []string, now time.Time) ([]domain.KnowledgeEntry, knowledge.CorpusStats, bool) {
	stats := knowledge.CorpusStats{DocumentFrequency: map[string]float64{}}
	if len(baseIDs) == 0 || len(queryTokens) == 0 {
		return nil, stats, true
	}
	indexes := make([]*knowledge.BaseIndex, 0, len(baseIDs))
	for _, baseID := range baseIDs {
		index := x.base(lister, baseID, now)
		if index == nil {
			return nil, stats, false
		}
		indexes = append(indexes, index)
	}

	documentCount := 0
	totalLength := 0
	for _, index := range indexes {
		documentCount += index.DocumentCount()
		totalLength += index.TotalLength()
	}
	stats.DocumentCount = float64(documentCount)
	stats.TotalLength = float64(totalLength)
	if documentCount == 0 {
		return nil, stats, true
	}
	for _, token := range queryTokens {
		frequency := 0
		for _, index := range indexes {
			frequency += index.DocumentFrequency(token)
		}
		if frequency > 0 {
			stats.DocumentFrequency[token] = float64(frequency)
		}
	}

	candidates := make([]domain.KnowledgeEntry, 0, 16)
	seen := make(map[string]bool, 16)
	for _, index := range indexes {
		for _, entryID := range index.Candidates(queryTokens) {
			if seen[entryID] {
				continue
			}
			seen[entryID] = true
			if entry, ok := index.Entry(entryID); ok {
				candidates = append(candidates, entry)
			}
		}
	}
	return candidates, stats, true
}

// ===== 写失效：包装 Store =====

// knowledgeIndexStore 在知识库/条目的写操作成功后让倒排索引失效。
// 通过嵌入 Store 接口，其余方法原样透传，因此可以整体替换 Service.store。
type knowledgeIndexStore struct {
	store.Store
	index *knowledgeIndex
}

func (s *knowledgeIndexStore) CreateKnowledgeBase(next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	out, err := s.Store.CreateKnowledgeBase(next)
	if err == nil {
		// 新库会影响"全部启用库"这一检索范围，整体失效最稳妥。
		s.index.invalidateAll()
	}
	return out, err
}

func (s *knowledgeIndexStore) UpdateKnowledgeBase(id string, next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	out, err := s.Store.UpdateKnowledgeBase(id, next)
	if err == nil {
		// enabled 翻转会改变检索范围（是否收录该库），整体失效。
		s.index.invalidateAll()
	}
	return out, err
}

func (s *knowledgeIndexStore) DeleteKnowledgeBase(id string) (domain.KnowledgeBase, error) {
	out, err := s.Store.DeleteKnowledgeBase(id)
	if err == nil {
		s.index.invalidateAll()
	}
	return out, err
}

func (s *knowledgeIndexStore) CreateKnowledgeEntry(next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	out, err := s.Store.CreateKnowledgeEntry(next)
	if err == nil {
		// 新增只影响目标库；批量导入走的就是这条路径，避免每次导入都整体失效。
		s.index.invalidate(out.BaseID)
	}
	return out, err
}

func (s *knowledgeIndexStore) UpdateKnowledgeEntry(id string, next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	out, err := s.Store.UpdateKnowledgeEntry(id, next)
	if err == nil {
		// 条目可能被移动到别的库，无法从出参确定旧库，整体失效。
		s.index.invalidateAll()
	}
	return out, err
}

func (s *knowledgeIndexStore) DeleteKnowledgeEntry(id string) (domain.KnowledgeEntry, error) {
	out, err := s.Store.DeleteKnowledgeEntry(id)
	if err == nil {
		s.index.invalidateAll()
	}
	return out, err
}
