// Package knowledge 实现 AI 知识库的本地检索打分。
//
// 第一版不引入任何外部依赖（无向量库、无全文索引服务）：
//   - 中文按字符 bigram 切分（"排水沟槽" -> 排水 / 水沟 / 沟槽），
//   - 其它语言按 Unicode 字母/数字连续段切词并小写归一；
//   - 打分使用 BM25（k1=1.2, b=0.75）并归一化到 (0,1]，在 title（×2）、keywords（×1.5）、
//     content（×1）三个字段上分别计算后加权求和，再乘 (1 + priority/100)。
//
// 所有函数都是纯函数，便于单元测试与复用。
package knowledge

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"customer-service/backend/internal/domain"
)

// BM25 参数与字段权重：与产品需求一致，调整时同步更新测试。
const (
	bm25K1 = 1.2
	bm25B  = 0.75

	titleWeight    = 2.0
	keywordsWeight = 1.5
	contentWeight  = 1.0
)

// Tokenize 把一段文本切分为检索词：
//   - 中文（Unicode Han）连续段按相邻两字组成 bigram，单字段落退化为单字；
//   - 其它语言的字母/数字连续段整体作为词，统一小写；
//   - 标点、空白与符号一律作为分隔符丢弃。
func Tokenize(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	tokens := make([]string, 0, len([]rune(text)))
	han := make([]rune, 0, 8)
	word := make([]rune, 0, 8)

	flushHan := func() {
		if len(han) == 0 {
			return
		}
		if len(han) == 1 {
			tokens = append(tokens, string(han[0]))
		} else {
			for i := 0; i+1 < len(han); i++ {
				tokens = append(tokens, string(han[i:i+2]))
			}
		}
		han = han[:0]
	}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(string(word)))
		word = word[:0]
	}

	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			word = append(word, r)
		default:
			flushHan()
			flushWord()
		}
	}
	flushHan()
	flushWord()
	return tokens
}

// tokenFrequency 统计一组词的出现次数。
func tokenFrequency(tokens []string) map[string]int {
	if len(tokens) == 0 {
		return nil
	}
	out := make(map[string]int, len(tokens))
	for _, token := range tokens {
		out[token]++
	}
	return out
}

// uniqueTokens 按出现顺序去重，避免同一查询词重复计分。
func uniqueTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

// entryDoc 是一条知识条目的分词结果。
type entryDoc struct {
	entry    domain.KnowledgeEntry
	title    map[string]int
	keywords map[string]int
	content  map[string]int
	length   int
}

func newEntryDoc(entry domain.KnowledgeEntry) entryDoc {
	titleTokens := Tokenize(entry.Title)
	keywordTokens := Tokenize(strings.Join(entry.Keywords, " "))
	contentTokens := Tokenize(entry.Content)
	return entryDoc{
		entry:    entry,
		title:    tokenFrequency(titleTokens),
		keywords: tokenFrequency(keywordTokens),
		content:  tokenFrequency(contentTokens),
		length:   len(titleTokens) + len(keywordTokens) + len(contentTokens),
	}
}

// contains 判断某个词是否出现在该条目的任一字段中。
func (d entryDoc) contains(token string) bool {
	return d.title[token] > 0 || d.keywords[token] > 0 || d.content[token] > 0
}

// bm25Field 计算单个字段上的 BM25 贡献值。
func bm25Field(termFrequency, fieldLength int, averageLength, idf float64) float64 {
	if termFrequency <= 0 || fieldLength <= 0 || averageLength <= 0 {
		return 0
	}
	normalization := 1 - bm25B + bm25B*float64(fieldLength)/averageLength
	return idf * (float64(termFrequency) * (bm25K1 + 1)) / (float64(termFrequency) + bm25K1*normalization)
}

// CorpusStats 是候选条目所在**全量语料**的全局统计。
//
// 倒排索引先取候选，只对候选算 BM25；但 idf 与平均长度必须仍按全量语料计算，
// 否则候选子集上的分数/排序会和"全表 BM25"不一致。因此这些量在索引加载时算好，
// 通过 CorpusStats 传入 RankKnowledgeCandidates。
type CorpusStats struct {
	// DocumentCount 是语料中的文档数（float64 与 BM25 公式保持一致）。
	DocumentCount float64
	// TotalLength 是语料中所有文档长度之和。
	TotalLength float64
	// DocumentFrequency 是查询词在全量语料中的文档频次（只存在于语料的词才有键）。
	DocumentFrequency map[string]float64
}

// RankKnowledgeEntries 对候选知识条目按查询词做 BM25 排序，返回分数最高的前 limit 条。
//
// 该函数是纯函数：不读数据库、不改入参、不做阈值过滤（阈值由调用方用
// FilterKnowledgeHits 处理）。limit<=0 时使用 domain.DefaultKnowledgeMaxEntries。
// 返回结果按 分数降序 → priority 降序 → ID 升序 排列，保证同一输入结果稳定。
//
// entries 是**全量语料**：语料统计与打分都基于它。只对候选条目打分的场景请用
// RankKnowledgeCandidates，并传入由倒排索引预计算的 CorpusStats。
func RankKnowledgeEntries(query string, entries []domain.KnowledgeEntry, limit int) []domain.KnowledgeHit {
	if limit <= 0 {
		limit = domain.DefaultKnowledgeMaxEntries
	}
	queryTokens := uniqueTokens(Tokenize(query))
	if len(queryTokens) == 0 || len(entries) == 0 {
		return nil
	}

	docs := make([]entryDoc, 0, len(entries))
	totalLength := 0
	for _, entry := range entries {
		doc := newEntryDoc(entry)
		docs = append(docs, doc)
		totalLength += doc.length
	}
	if totalLength == 0 {
		return nil
	}
	stats := CorpusStats{
		DocumentCount:     float64(len(docs)),
		TotalLength:       float64(totalLength),
		DocumentFrequency: make(map[string]float64, len(queryTokens)),
	}
	for _, token := range queryTokens {
		documentFrequency := 0.0
		for _, other := range docs {
			if other.contains(token) {
				documentFrequency++
			}
		}
		if documentFrequency > 0 {
			stats.DocumentFrequency[token] = documentFrequency
		}
	}
	return rankEntryDocs(queryTokens, docs, stats, limit)
}

// RankKnowledgeCandidates 只对倒排索引取出的候选条目做 BM25 打分，语料统计由调用方
// 预计算（见 CorpusStats）。在"候选 = 语料中所有含任一查询词的条目"这一前提下，
// 结果与 RankKnowledgeEntries(query, 全量语料, limit) 的顺序与分数完全一致。
func RankKnowledgeCandidates(query string, candidates []domain.KnowledgeEntry, stats CorpusStats, limit int) []domain.KnowledgeHit {
	if limit <= 0 {
		limit = domain.DefaultKnowledgeMaxEntries
	}
	queryTokens := uniqueTokens(Tokenize(query))
	if len(queryTokens) == 0 || len(candidates) == 0 {
		// 语料非空但无候选时，与全表 BM25 一样返回空切片（而非 nil），保持结果形状一致。
		if len(queryTokens) == 0 || stats.DocumentCount <= 0 || stats.TotalLength <= 0 {
			return nil
		}
		return []domain.KnowledgeHit{}
	}
	if stats.DocumentCount <= 0 || stats.TotalLength <= 0 {
		return nil
	}
	docs := make([]entryDoc, 0, len(candidates))
	for _, entry := range candidates {
		docs = append(docs, newEntryDoc(entry))
	}
	return rankEntryDocs(queryTokens, docs, stats, limit)
}

// rankEntryDocs 是 BM25 打分的公共实现：按预计算的语料统计对 docs 打分并排序。
func rankEntryDocs(queryTokens []string, docs []entryDoc, stats CorpusStats, limit int) []domain.KnowledgeHit {
	if len(queryTokens) == 0 || len(docs) == 0 || stats.DocumentCount <= 0 || stats.TotalLength <= 0 {
		return nil
	}
	averageLength := stats.TotalLength / stats.DocumentCount
	documentCount := stats.DocumentCount

	// 先算一次每个 query token 的 idf，并据此得到"理论最高分"：
	// 单字段上 BM25 的极限是 idf*(k1+1)，因此
	// maxPossible = Σ idf(t) * (k1+1) * (titleWeight+keywordsWeight+contentWeight)。
	// 用它把原始 BM25 分数归一化到 (0,1]，阈值才能跨查询、跨文档长度比较——
	// 否则原始分是无上界的（实测同一问题能到 19 分），后台配的 0.35 阈值形同虚设，
	// 只要有一个词片段重合就会被当成"命中知识"，AI 会据此乱回答。
	idfByToken := make(map[string]float64, len(queryTokens))
	maxPossible := 0.0
	for _, token := range queryTokens {
		documentFrequency := stats.DocumentFrequency[token]
		if documentFrequency == 0 {
			continue
		}
		// BM25 的 idf 使用 +1 平滑，保证在所有文档都含该词时仍为正数。
		idf := math.Log(1 + (documentCount-documentFrequency+0.5)/(documentFrequency+0.5))
		idfByToken[token] = idf
		maxPossible += idf * (bm25K1 + 1) * (titleWeight + keywordsWeight + contentWeight)
	}

	// 同分时的稳定排序键：priority 降序 → ID 升序。优先级从待打分的 docs 直接取，
	// 候选集就是参与打分的全部文档，因此与全量 BM25 的 priorityOf 结果一致。
	priorityByID := make(map[string]int, len(docs))
	for _, doc := range docs {
		priorityByID[doc.entry.ID] = doc.entry.Priority
	}

	hits := make([]domain.KnowledgeHit, 0, len(docs))
	for _, doc := range docs {
		score := 0.0
		matched := make([]string, 0, len(queryTokens))
		for _, token := range queryTokens {
			idf, ok := idfByToken[token]
			if !ok {
				continue
			}
			fieldScore := titleWeight*bm25Field(doc.title[token], len(doc.title), averageLength, idf) +
				keywordsWeight*bm25Field(doc.keywords[token], len(doc.keywords), averageLength, idf) +
				contentWeight*bm25Field(doc.content[token], len(doc.content), averageLength, idf)
			if fieldScore > 0 {
				score += fieldScore
				matched = append(matched, token)
			}
		}
		if score <= 0 || len(matched) == 0 {
			continue
		}
		if maxPossible > 0 {
			score /= maxPossible
		}
		// 优先级加成：priority=100 时分数翻倍，便于人工把重点条目顶到前面；
		// 归一化后的分数收敛到 [0,1]，保证与后台阈值（0–1）语义一致。
		score *= 1 + float64(doc.entry.Priority)/100
		if score > 1 {
			score = 1
		}
		hits = append(hits, domain.KnowledgeHit{
			EntryID: doc.entry.ID,
			BaseID:  doc.entry.BaseID,
			Title:   doc.entry.Title,
			Score:   score,
			Matched: matched,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		priorityI, priorityJ := priorityByID[hits[i].EntryID], priorityByID[hits[j].EntryID]
		if priorityI != priorityJ {
			return priorityI > priorityJ
		}
		return hits[i].EntryID < hits[j].EntryID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// FilterKnowledgeHits 过滤掉低于阈值的命中；threshold<=0 时返回原结果。
func FilterKnowledgeHits(hits []domain.KnowledgeHit, threshold float64) []domain.KnowledgeHit {
	if len(hits) == 0 {
		return nil
	}
	if threshold <= 0 {
		return hits
	}
	out := make([]domain.KnowledgeHit, 0, len(hits))
	for _, hit := range hits {
		if hit.Score >= threshold {
			out = append(out, hit)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildKnowledgePrompt 把命中的知识条目拼进 system prompt，并要求模型只依据资料回答。
// byID 为条目 ID 到条目的映射（由调用方从候选集构造）。没有可用资料时原样返回 basePrompt。
func BuildKnowledgePrompt(basePrompt string, hits []domain.KnowledgeHit, byID map[string]domain.KnowledgeEntry) string {
	if len(hits) == 0 || len(byID) == 0 {
		return basePrompt
	}
	var builder strings.Builder
	if base := strings.TrimSpace(basePrompt); base != "" {
		builder.WriteString(base)
		builder.WriteString("\n\n")
	}
	builder.WriteString("以下是知识库中与访客问题相关的资料。请只依据这些资料回答访客，不要编造资料中不存在的信息，也不要依赖你自己的常识补充细节。")
	builder.WriteString("如果资料不足以回答，请如实说明并建议转人工。\n")
	index := 0
	for _, hit := range hits {
		entry, ok := byID[hit.EntryID]
		if !ok {
			continue
		}
		index++
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			title = "未命名资料"
		}
		builder.WriteString(fmt.Sprintf("【资料%d】%s\n%s\n", index, title, strings.TrimSpace(entry.Content)))
	}
	if index == 0 {
		return basePrompt
	}
	return strings.TrimSpace(builder.String())
}
