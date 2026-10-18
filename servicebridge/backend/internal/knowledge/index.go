package knowledge

import (
	"sort"

	"customer-service/backend/internal/domain"
)

// ===== 进程内倒排索引（每个知识库一份）=====
//
// 背景：全表 BM25 在数万条条目时每次检索都要对**全部**条目分词并算分，
// 单次查询成本与语料规模线性相关。倒排索引把"分词"这一步从查询时移到加载时：
//
//	token -> posting list(entryID -> 词频)
//
// 检索时先用查询词取候选（并集），只对候选条目算 BM25。为了让结果与"全表 BM25"
// 完全一致（顺序与分数），语料级统计（文档数、平均长度、文档频次）由索引在加载时
// 顺带算出，通过 CorpusStats 传给 RankKnowledgeCandidates——候选子集只影响
// "哪些文档参与打分"，不影响 idf / 平均长度等全局量。
//
// 并发安全由上层缓存（app 层的 knowledgeIndex）用 sync.RWMutex 保证；本结构在构建
// 完成后是只读的，可以被多个 goroutine 并发查询。

// BaseIndex 是单个知识库的只读倒排索引。
type BaseIndex struct {
	entries []domain.KnowledgeEntry
	byID    map[string]domain.KnowledgeEntry
	// postings: token -> entryID -> 该条目所有字段（title/keywords/content）的词频之和。
	postings    map[string]map[string]int
	totalLength int
}

// NewBaseIndex 为一个知识库的全部启用条目建立倒排索引。
//
// 条目顺序不影响结果：BM25 的语料统计与排序键都与输入顺序无关。
func NewBaseIndex(entries []domain.KnowledgeEntry) *BaseIndex {
	index := &BaseIndex{
		entries:  append([]domain.KnowledgeEntry(nil), entries...),
		byID:     make(map[string]domain.KnowledgeEntry, len(entries)),
		postings: make(map[string]map[string]int),
	}
	for _, entry := range entries {
		doc := newEntryDoc(entry)
		index.byID[entry.ID] = entry
		index.totalLength += doc.length
		index.addField(entry.ID, doc.title)
		index.addField(entry.ID, doc.keywords)
		index.addField(entry.ID, doc.content)
	}
	return index
}

// addField 把单个字段的词频累加进 posting list。
func (b *BaseIndex) addField(entryID string, counts map[string]int) {
	for token, frequency := range counts {
		list, ok := b.postings[token]
		if !ok {
			list = make(map[string]int)
			b.postings[token] = list
		}
		list[entryID] += frequency
	}
}

// DocumentCount 返回该库的文档数（与全量 BM25 的 documentCount 相同）。
func (b *BaseIndex) DocumentCount() int {
	if b == nil {
		return 0
	}
	return len(b.entries)
}

// TotalLength 返回该库全部文档的长度之和（与全量 BM25 的 totalLength 相同）。
func (b *BaseIndex) TotalLength() int {
	if b == nil {
		return 0
	}
	return b.totalLength
}

// DocumentFrequency 返回包含该词的文档数：只要词出现在 title/keywords/content
// 任一字段即计数，与 entryDoc.contains 的语义完全一致。
func (b *BaseIndex) DocumentFrequency(token string) int {
	if b == nil {
		return 0
	}
	return len(b.postings[token])
}

// Candidates 返回任一查询词命中过的条目 ID（并集，按 ID 升序保证确定性）。
func (b *BaseIndex) Candidates(tokens []string) []string {
	if b == nil || len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, 16)
	for _, token := range tokens {
		for entryID := range b.postings[token] {
			if seen[entryID] {
				continue
			}
			seen[entryID] = true
			out = append(out, entryID)
		}
	}
	sort.Strings(out)
	return out
}

// Entry 按 ID 取回原始条目。
func (b *BaseIndex) Entry(entryID string) (domain.KnowledgeEntry, bool) {
	if b == nil {
		return domain.KnowledgeEntry{}, false
	}
	entry, ok := b.byID[entryID]
	return entry, ok
}

// TokenFrequency 返回某个词在某条目上的词频（仅用于调试/测试）。
func (b *BaseIndex) TokenFrequency(entryID, token string) int {
	if b == nil {
		return 0
	}
	return b.postings[token][entryID]
}
