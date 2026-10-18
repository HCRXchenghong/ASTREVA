package knowledge

import (
	"strings"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestTokenizeChineseBigramAndEnglishWords 校验中英混排的分词规则。
func TestTokenizeChineseBigramAndEnglishWords(t *testing.T) {
	got := Tokenize("不锈钢排水沟槽 Stainless Steel 304")
	want := []string{"不锈", "锈钢", "钢排", "排水", "水沟", "沟槽", "stainless", "steel", "304"}
	if len(got) != len(want) {
		t.Fatalf("分词数量 = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("分词[%d] = %q, want %q（完整结果 %v）", i, got[i], want[i], got)
		}
	}

	// 单字中文退化为单字，避免 bigram 为空导致检索不到。
	if single := Tokenize("水"); len(single) != 1 || single[0] != "水" {
		t.Fatalf("单字中文分词 = %v, want [水]", single)
	}
	// 标点与空白只作为分隔符。
	if tokens := Tokenize("你好，world!"); len(tokens) != 2 || tokens[0] != "你好" || tokens[1] != "world" {
		t.Fatalf("含标点分词 = %v", tokens)
	}
}

// TestRankKnowledgeEntriesChineseBigram：中文按 bigram 命中，无关条目不得分。
func TestRankKnowledgeEntriesChineseBigram(t *testing.T) {
	entries := []domain.KnowledgeEntry{
		{ID: "ke_drain", BaseID: "kb_1", Title: "排水沟槽", Content: "不锈钢排水沟槽规格与价格说明"},
		{ID: "ke_warranty", BaseID: "kb_1", Title: "售后政策", Content: "产品保修期限与退换货流程"},
	}
	hits := RankKnowledgeEntries("排水沟槽", entries, 3)
	if len(hits) != 1 || hits[0].EntryID != "ke_drain" {
		t.Fatalf("中文检索命中 = %+v, want 仅 ke_drain", hits)
	}
	if hits[0].Score <= 0 {
		t.Fatalf("命中分数应为正数：%v", hits[0].Score)
	}
	if len(hits[0].Matched) == 0 {
		t.Fatal("命中词不应为空")
	}

	// 只问"不锈钢"也应命中同一条目。
	if hits := RankKnowledgeEntries("不锈钢", entries, 3); len(hits) != 1 || hits[0].EntryID != "ke_drain" {
		t.Fatalf("不锈钢检索命中 = %+v", hits)
	}
}

// TestRankKnowledgeEntriesEnglishWords：英文按词命中。
func TestRankKnowledgeEntriesEnglishWords(t *testing.T) {
	entries := []domain.KnowledgeEntry{
		{ID: "ke_channel", BaseID: "kb_1", Content: "Stainless Steel Channel Drain with grate"},
		{ID: "ke_policy", BaseID: "kb_1", Content: "Warranty policy and return process"},
	}
	hits := RankKnowledgeEntries("stainless steel drain", entries, 3)
	if len(hits) != 1 || hits[0].EntryID != "ke_channel" {
		t.Fatalf("英文检索命中 = %+v, want 仅 ke_channel", hits)
	}
	if hits := RankKnowledgeEntries("warranty", entries, 3); len(hits) != 1 || hits[0].EntryID != "ke_policy" {
		t.Fatalf("warranty 检索命中 = %+v", hits)
	}
}

// TestRankTitleAndKeywordWeightsBeatContent：标题（×2）与关键词（×1.5）应高于正文（×1）。
func TestRankTitleAndKeywordWeightsBeatContent(t *testing.T) {
	entries := []domain.KnowledgeEntry{
		{ID: "ke_content", BaseID: "kb_1", Title: "普通标题", Content: "排水沟槽"},
		{ID: "ke_title", BaseID: "kb_1", Title: "排水沟槽", Content: "普通说明文字"},
	}
	hits := RankKnowledgeEntries("排水沟槽", entries, 3)
	if len(hits) != 2 {
		t.Fatalf("应命中两条，实际 %+v", hits)
	}
	if hits[0].EntryID != "ke_title" {
		t.Fatalf("标题加权应排第一，实际顺序 %+v", hits)
	}

	keywordEntries := []domain.KnowledgeEntry{
		{ID: "ke_kw_content", BaseID: "kb_1", Content: "排水沟槽"},
		{ID: "ke_kw", BaseID: "kb_1", Keywords: []string{"排水沟槽"}},
	}
	keywordHits := RankKnowledgeEntries("排水沟槽", keywordEntries, 3)
	if len(keywordHits) != 2 || keywordHits[0].EntryID != "ke_kw" {
		t.Fatalf("关键词加权应排第一，实际 %+v", keywordHits)
	}
}

// TestRankPriorityBoost：相同内容时 priority 高的排前面。
func TestRankPriorityBoost(t *testing.T) {
	entries := []domain.KnowledgeEntry{
		{ID: "ke_low", BaseID: "kb_1", Content: "排水沟槽", Priority: 0},
		{ID: "ke_high", BaseID: "kb_1", Content: "排水沟槽", Priority: 100},
	}
	hits := RankKnowledgeEntries("排水沟槽", entries, 3)
	if len(hits) != 2 || hits[0].EntryID != "ke_high" {
		t.Fatalf("优先级加成未生效：%+v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("高优先级分数应更大：%v <= %v", hits[0].Score, hits[1].Score)
	}
}

// TestRankLimitAndEmptyCorpus：limit 生效；空库/无命中返回空。
func TestRankLimitAndEmptyCorpus(t *testing.T) {
	entries := []domain.KnowledgeEntry{
		{ID: "ke_1", BaseID: "kb_1", Content: "排水沟槽 一"},
		{ID: "ke_2", BaseID: "kb_1", Content: "排水沟槽 二"},
		{ID: "ke_3", BaseID: "kb_1", Content: "排水沟槽 三"},
	}
	if hits := RankKnowledgeEntries("排水沟槽", entries, 2); len(hits) != 2 {
		t.Fatalf("limit=2 应返回 2 条，实际 %d", len(hits))
	}
	// limit<=0 时使用默认值 3。
	if hits := RankKnowledgeEntries("排水沟槽", entries, 0); len(hits) != 3 {
		t.Fatalf("默认 limit 应为 3，实际 %d", len(hits))
	}
	if hits := RankKnowledgeEntries("排水沟槽", nil, 3); len(hits) != 0 {
		t.Fatalf("空库应返回空结果：%+v", hits)
	}
	if hits := RankKnowledgeEntries("完全不相关的词", entries, 3); len(hits) != 0 {
		t.Fatalf("无命中应返回空结果：%+v", hits)
	}
	if hits := RankKnowledgeEntries("", entries, 3); len(hits) != 0 {
		t.Fatalf("空查询应返回空结果：%+v", hits)
	}
}

// TestFilterKnowledgeHitsThreshold：低于阈值的命中视为未命中。
func TestFilterKnowledgeHitsThreshold(t *testing.T) {
	hits := []domain.KnowledgeHit{
		{EntryID: "ke_high", Score: 0.9},
		{EntryID: "ke_mid", Score: 0.35},
		{EntryID: "ke_low", Score: 0.1},
	}
	filtered := FilterKnowledgeHits(hits, 0.35)
	if len(filtered) != 2 || filtered[0].EntryID != "ke_high" || filtered[1].EntryID != "ke_mid" {
		t.Fatalf("阈值过滤结果 = %+v", filtered)
	}
	if got := FilterKnowledgeHits(hits, 0.95); len(got) != 0 {
		t.Fatalf("阈值高于全部命中时应返回空：%+v", got)
	}
	if got := FilterKnowledgeHits(nil, 0.35); len(got) != 0 {
		t.Fatalf("空输入应返回空：%+v", got)
	}
	if got := FilterKnowledgeHits(hits, 0); len(got) != 3 {
		t.Fatalf("阈值<=0 时不过滤：%+v", got)
	}
}

// TestBuildKnowledgePrompt：资料被写入提示词并要求只依据资料回答。
func TestBuildKnowledgePrompt(t *testing.T) {
	entries := map[string]domain.KnowledgeEntry{
		"ke_1": {ID: "ke_1", Title: "排水沟槽", Content: "不锈钢排水沟槽支持定制长度。"},
	}
	hits := []domain.KnowledgeHit{{EntryID: "ke_1", Score: 1.2}}
	prompt := BuildKnowledgePrompt("你是客服助手。", hits, entries)
	for _, fragment := range []string{"你是客服助手。", "只依据这些资料", "排水沟槽", "不锈钢排水沟槽支持定制长度。"} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("提示词缺少 %q：%s", fragment, prompt)
		}
	}
	if got := BuildKnowledgePrompt("基础提示", nil, entries); got != "基础提示" {
		t.Fatalf("无命中时应原样返回基础提示，实际 %q", got)
	}
}

// 归一化保证后台阈值（0–1）真正起作用：分数必须有上界，且"只沾一点边"的内容会被挡掉。
func TestScoresNormalizedSoThresholdWorks(t *testing.T) {
	entries := []domain.KnowledgeEntry{
		{
			ID:       "ke_material",
			Title:    "不锈钢排水沟槽材质",
			Content:  "重型模块化排水沟采用 AISI 304 或 316L 不锈钢，符合 EN1433 承载标准。",
			Keywords: []string{"不锈钢", "材质", "EN1433"},
		},
		{
			ID:       "ke_about",
			Title:    "公司简介",
			Content:  "我们是一家专注于排水系统的公司，服务全球客户。",
			Keywords: []string{"公司"},
			Priority: 0,
		},
	}

	strong := RankKnowledgeEntries("你们用什么不锈钢材质？", entries, 5)
	if len(strong) == 0 {
		t.Fatal("明确命中材质的问题应有结果")
	}
	if strong[0].EntryID != "ke_material" {
		t.Fatalf("最相关条目应为材质条目，实际 %s", strong[0].EntryID)
	}
	for _, hit := range strong {
		if hit.Score <= 0 || hit.Score > 1 {
			t.Fatalf("归一化分数必须落在 (0,1]，实际 %s=%.4f", hit.EntryID, hit.Score)
		}
	}
	if strong[0].Score < domain.DefaultKnowledgeScoreThreshold {
		t.Fatalf("强命中分数 %.4f 应高于默认阈值 %.2f", strong[0].Score, domain.DefaultKnowledgeScoreThreshold)
	}

	// 只沾一点边（"客户" 只出现在无关条目的正文里）→ 归一化分数应低于默认阈值，并被过滤掉。
	weak := RankKnowledgeEntries("客户", entries, 5)
	for _, hit := range weak {
		if hit.EntryID == "ke_about" && hit.Score >= domain.DefaultKnowledgeScoreThreshold {
			t.Fatalf("弱命中分数 %.4f 不应达到阈值 %.2f", hit.Score, domain.DefaultKnowledgeScoreThreshold)
		}
	}
	for _, hit := range FilterKnowledgeHits(weak, domain.DefaultKnowledgeScoreThreshold) {
		if hit.EntryID == "ke_about" {
			t.Fatalf("弱命中不应通过阈值过滤（分数 %.4f）", hit.Score)
		}
	}
}
