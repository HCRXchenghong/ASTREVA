package app

import (
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/knowledge"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// countingKnowledgeStore 统计 KnowledgeEntriesForBases（全量条目读取）的调用次数，
// 用于断言"索引命中路径不再读全表"。
type countingKnowledgeStore struct {
	store.Store
	reads int32
}

func (c *countingKnowledgeStore) KnowledgeEntriesForBases(baseIDs []string) []domain.KnowledgeEntry {
	atomic.AddInt32(&c.reads, 1)
	return c.Store.KnowledgeEntriesForBases(baseIDs)
}

func (c *countingKnowledgeStore) readCount() int {
	return int(atomic.LoadInt32(&c.reads))
}

// newIndexedKnowledgeService 构造使用确定性索引配置（TTL 60s、上限默认）的服务。
func newIndexedKnowledgeService(t *testing.T, st store.Store) *Service {
	t.Helper()
	service := NewService(st, realtime.NewHub(), slog.Default())
	// 不依赖环境变量：固定 TTL 与上限，保证测试可重复。
	service.knowledgeIndex.ttl = time.Minute
	service.knowledgeIndex.maxEntries = defaultKnowledgeIndexMaxEntries
	return service
}

// seedIndexedCorpus 构造两条知识库、若干条含中英文的条目。
func seedIndexedCorpus(t *testing.T, st store.Store) (domain.KnowledgeBase, domain.KnowledgeBase) {
	t.Helper()
	baseA, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "产品库", Enabled: true, SortOrder: 1})
	if err != nil {
		t.Fatalf("create base A: %v", err)
	}
	baseB, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "售后库", Enabled: true, SortOrder: 2})
	if err != nil {
		t.Fatalf("create base B: %v", err)
	}
	entries := []domain.KnowledgeEntry{
		{BaseID: baseA.ID, Title: "不锈钢排水沟槽", Content: "不锈钢排水沟槽支持定制长度与颜色，可按图纸生产。", Keywords: []string{"排水沟槽", "不锈钢"}, Priority: 10, Enabled: true},
		{BaseID: baseA.ID, Title: "树脂排水沟槽", Content: "树脂排水沟槽适用于小区与园林排水工程。", Keywords: []string{"排水沟槽", "树脂"}, Priority: 0, Enabled: true},
		{BaseID: baseA.ID, Title: "排水沟槽盖板", Content: "沟槽盖板有球墨铸铁与不锈钢两种材质。", Keywords: []string{"盖板"}, Priority: 5, Enabled: true},
		{BaseID: baseB.ID, Title: "Refund policy", Content: "退款申请需在收货后 7 天内提交，支持原路退回。", Keywords: []string{"refund", "退款"}, Priority: 3, Enabled: true},
		{BaseID: baseB.ID, Title: "发票说明", Content: "发票可开具增值税专用发票或普通发票。", Keywords: []string{"发票"}, Priority: 1, Enabled: true},
		{BaseID: baseB.ID, Title: "已停用条目", Content: "排水沟槽内部测试资料，不应参与检索。", Keywords: []string{"排水沟槽"}, Enabled: false},
	}
	for _, entry := range entries {
		if _, err := st.CreateKnowledgeEntry(entry); err != nil {
			t.Fatalf("seed entry %q: %v", entry.Title, err)
		}
	}
	return baseA, baseB
}

// TestKnowledgeIndexMatchesFullTableBM25：
// 倒排索引取候选 + 候选打分，结果必须与"取全量语料再做 BM25"在顺序与分数上完全一致
// （含限定单个知识库、以及应用阈值后的检索结果）。
func TestKnowledgeIndexMatchesFullTableBM25(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	baseA, baseB := seedIndexedCorpus(t, st)
	service := newIndexedKnowledgeService(t, st)

	queries := []string{
		"排水沟槽怎么卖",
		"不锈钢 盖板",
		"退款 refund",
		"发票怎么开",
		"完全不相关的关键词",
		"排水沟槽",
	}
	for _, query := range queries {
		for _, scope := range [][]string{nil, {baseA.ID}, {baseB.ID}, {baseA.ID, baseB.ID}} {
			limit := 10
			got := service.PreviewKnowledgeSearch(query, scope, limit)
			want := knowledge.RankKnowledgeEntries(query, st.KnowledgeEntriesForBases(scope), limit)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("索引结果与全表 BM25 不一致：query=%q scope=%v\n got=%+v\nwant=%+v", query, scope, got, want)
			}
		}
	}

	// 应用阈值后的正式检索路径也必须一致。
	if _, err := st.UpdateKnowledgeSettings(domain.KnowledgeSettings{
		UnknownAction:  domain.KnowledgeUnknownRefuse,
		RefuseReply:    "抱歉",
		ScoreThreshold: 0.3,
		MaxEntries:     2,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("update knowledge settings: %v", err)
	}
	search := service.searchKnowledge("排水沟槽怎么卖", nil)
	wantHits := knowledge.FilterKnowledgeHits(
		knowledge.RankKnowledgeEntries("排水沟槽怎么卖", st.KnowledgeEntriesForBases(nil), 2), 0.3)
	if !reflect.DeepEqual(search.Hits, wantHits) {
		t.Fatalf("阈值过滤后的检索结果不一致：\n got=%+v\nwant=%+v", search.Hits, wantHits)
	}
	if !search.Active {
		t.Fatal("存在启用知识库时 Active 应为 true")
	}
	for _, hit := range search.Hits {
		if _, ok := search.ByID[hit.EntryID]; !ok {
			t.Fatalf("ByID 缺少命中条目 %s", hit.EntryID)
		}
	}
}

// TestKnowledgeIndexAvoidsFullEntryReads：
// 索引预热后，再次检索不应再调用 KnowledgeEntriesForBases（全量条目读取）；
// 查询词无候选时同样不读存储，直接返回空。
func TestKnowledgeIndexAvoidsFullEntryReads(t *testing.T) {
	counting := &countingKnowledgeStore{Store: store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})}
	base, _ := seedIndexedCorpus(t, counting)
	service := newIndexedKnowledgeService(t, counting)

	// 预热：第一次检索需要懒加载建索引，允许读一次全量。
	if hits := service.PreviewKnowledgeSearch("排水沟槽", nil, 10); len(hits) == 0 {
		t.Fatal("预热检索应命中")
	}
	if counting.readCount() == 0 {
		t.Fatal("首次检索应触发一次懒加载")
	}

	atomic.StoreInt32(&counting.reads, 0)
	hits := service.PreviewKnowledgeSearch("排水沟槽", nil, 10)
	if len(hits) == 0 {
		t.Fatal("缓存命中时仍应返回结果")
	}
	if got := counting.readCount(); got != 0 {
		t.Fatalf("索引命中路径不应读取全量条目，实际 %d 次", got)
	}

	// 无候选（查询词不在任何条目中）：直接返回空，同样不读存储。
	if hits := service.PreviewKnowledgeSearch("量子纠缠证明", nil, 10); len(hits) != 0 {
		t.Fatalf("无候选时应返回空：%+v", hits)
	}
	if got := counting.readCount(); got != 0 {
		t.Fatalf("无候选时不应读取全量条目，实际 %d 次", got)
	}

	// 限定到某个库同样走缓存。
	if hits := service.PreviewKnowledgeSearch("排水沟槽", []string{base.ID}, 10); len(hits) == 0 {
		t.Fatal("限定知识库检索应命中")
	}
	if got := counting.readCount(); got != 0 {
		t.Fatalf("限定知识库检索不应读取全量条目，实际 %d 次", got)
	}
}

// TestKnowledgeIndexRefreshesAfterWrites：
// 条目/知识库增删改（含导入所用的创建路径）后，索引必须刷新并对检索可见。
func TestKnowledgeIndexRefreshesAfterWrites(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	base, _ := seedIndexedCorpus(t, st)
	service := newIndexedKnowledgeService(t, st)

	// 预热索引。
	service.PreviewKnowledgeSearch("玄武岩", nil, 10)

	// 新增：走 service.Store()（等价于后台创建/导入路径），索引应立即失效。
	created, err := service.Store().CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID:  base.ID,
		Title:   "玄武岩纤维沟槽",
		Content: "玄武岩纤维沟槽适用于强腐蚀环境。",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	hits := service.PreviewKnowledgeSearch("玄武岩", nil, 10)
	if len(hits) != 1 || hits[0].EntryID != created.ID {
		t.Fatalf("新增条目后索引未刷新：%+v", hits)
	}

	// 修改：改成完全不同的主题，旧查询不应再命中，新查询应命中。
	updated := created
	updated.Title = "碳纤维沟槽"
	updated.Content = "碳纤维沟槽用于轻量化场景。"
	if _, err := service.Store().UpdateKnowledgeEntry(created.ID, updated); err != nil {
		t.Fatalf("update entry: %v", err)
	}
	if hits := service.PreviewKnowledgeSearch("玄武岩", nil, 10); len(hits) != 0 {
		t.Fatalf("修改后旧查询不应命中：%+v", hits)
	}
	if hits := service.PreviewKnowledgeSearch("碳纤维", nil, 10); len(hits) != 1 {
		t.Fatalf("修改后新查询应命中：%+v", hits)
	}

	// 删除：不再命中。
	if _, err := service.Store().DeleteKnowledgeEntry(created.ID); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	if hits := service.PreviewKnowledgeSearch("碳纤维", nil, 10); len(hits) != 0 {
		t.Fatalf("删除后不应命中：%+v", hits)
	}

	// 新增知识库：新库的条目应对"全部启用库"检索立即可见。
	newBase, err := service.Store().CreateKnowledgeBase(domain.KnowledgeBase{Name: "新增库", Enabled: true})
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	if _, err := service.Store().CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID:  newBase.ID,
		Title:   "玄武岩纤维管",
		Content: "玄武岩纤维管耐高温。",
		Enabled: true,
	}); err != nil {
		t.Fatalf("create entry in new base: %v", err)
	}
	if hits := service.PreviewKnowledgeSearch("玄武岩", nil, 10); len(hits) != 1 {
		t.Fatalf("新知识库的条目应立即可见：%+v", hits)
	}

	// 停用知识库：其条目不再参与检索（UpdateKnowledgeBase 同样失效索引）。
	disabled := newBase
	disabled.Enabled = false
	if _, err := service.Store().UpdateKnowledgeBase(newBase.ID, disabled); err != nil {
		t.Fatalf("disable base: %v", err)
	}
	if hits := service.PreviewKnowledgeSearch("玄武岩", nil, 10); len(hits) != 0 {
		t.Fatalf("停用知识库后其条目不应参与检索：%+v", hits)
	}
}

// TestKnowledgeIndexTenThousandEntriesLatency：
// 1 万条条目的语料上，索引预热后的选择性检索应在 200ms 内返回（放宽阈值避免 CI 抖动）。
func TestKnowledgeIndexTenThousandEntriesLatency(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	base, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "大库", Enabled: true})
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	const total = 10000
	// 绝大多数条目共享常见词，只有 4 条含稀有词，用来验证"倒排取候选"确实把候选压得很小。
	for i := 0; i < total; i++ {
		content := fmt.Sprintf("这是第 %d 条关于市政工程、法兰配件与管道安装的资料。", i)
		title := fmt.Sprintf("产品资料 %d", i)
		if i%3000 == 0 {
			content += " 玄武岩纤维增强版本。"
			title = "玄武岩纤维排水沟槽"
		}
		if _, err := st.CreateKnowledgeEntry(domain.KnowledgeEntry{
			BaseID:  base.ID,
			Title:   title,
			Content: content,
			Enabled: true,
		}); err != nil {
			t.Fatalf("seed entry %d: %v", i, err)
		}
	}
	service := newIndexedKnowledgeService(t, st)

	// 预热（含首次建索引）。
	if hits := service.PreviewKnowledgeSearch("玄武岩", nil, 10); len(hits) == 0 {
		t.Fatal("预热检索应命中稀有词条目")
	}

	var worst time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		hits := service.PreviewKnowledgeSearch("玄武岩纤维排水沟槽", nil, 10)
		elapsed := time.Since(start)
		if len(hits) == 0 {
			t.Fatal("检索应命中")
		}
		if elapsed > worst {
			worst = elapsed
		}
	}
	if worst > 200*time.Millisecond {
		t.Fatalf("1 万条语料检索过慢：最慢 %v（阈值 200ms）", worst)
	}
	t.Logf("1 万条语料索引检索最慢耗时：%v", worst)
}

// TestKnowledgeIndexFallsBackWhenBaseExceedsCap：
// 单库条目数超过索引内存上限时不做缓存，回退为全表 BM25，结果仍与暴力检索一致。
func TestKnowledgeIndexFallsBackWhenBaseExceedsCap(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	seedIndexedCorpus(t, st)
	service := newIndexedKnowledgeService(t, st)
	service.knowledgeIndex.maxEntries = 2 // 语料多于 2 条：无法缓存

	for _, query := range []string{"排水沟槽怎么卖", "退款 refund", "完全不相关"} {
		got := service.PreviewKnowledgeSearch(query, nil, 10)
		want := knowledge.RankKnowledgeEntries(query, st.KnowledgeEntriesForBases(nil), 10)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("超上限回退结果与暴力检索不一致：query=%q\n got=%+v\nwant=%+v", query, got, want)
		}
	}
	service.knowledgeIndex.mu.RLock()
	cached := len(service.knowledgeIndex.bases)
	service.knowledgeIndex.mu.RUnlock()
	if cached != 0 {
		t.Fatalf("超上限的知识库不应进入缓存，实际缓存 %d 个", cached)
	}
}

// TestKnowledgeIndexConcurrentSearchAndInvalidate：
// 并发检索 + 并发增删条目（触发索引失效）不应死锁/竞态，且最终写入对检索可见。
func TestKnowledgeIndexConcurrentSearchAndInvalidate(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	base, _ := seedIndexedCorpus(t, st)
	service := newIndexedKnowledgeService(t, st)

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				service.PreviewKnowledgeSearch("排水沟槽 不锈钢", nil, 10)
			}
		}()
	}

	// 并发写入：创建后立即删除，持续触发 invalidate。
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; i < 60; i++ {
			entry, err := service.Store().CreateKnowledgeEntry(domain.KnowledgeEntry{
				BaseID:  base.ID,
				Title:   fmt.Sprintf("并发条目 %d", i),
				Content: "并发写入的排水沟槽资料",
				Enabled: true,
			})
			if err != nil {
				t.Errorf("并发创建条目失败：%v", err)
				return
			}
			if _, err := service.Store().DeleteKnowledgeEntry(entry.ID); err != nil {
				t.Errorf("并发删除条目失败：%v", err)
				return
			}
		}
	}()
	writer.Wait()
	close(stop)
	readers.Wait()

	// 最终状态一致：新建条目对检索立即可见。
	created, err := service.Store().CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID:  base.ID,
		Title:   "并发收尾条目",
		Content: "玄武岩纤维收尾资料",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("收尾创建条目失败：%v", err)
	}
	hits := service.PreviewKnowledgeSearch("玄武岩纤维", nil, 10)
	if len(hits) != 1 || hits[0].EntryID != created.ID {
		t.Fatalf("并发写入后索引应刷新可见：%+v", hits)
	}
}

// TestKnowledgeIndexEnvOverrides：索引 TTL 与内存上限可由环境变量覆盖，
// 默认 60s；TTL<=0 表示禁用缓存；非法值回落默认。
func TestKnowledgeIndexEnvOverrides(t *testing.T) {
	t.Setenv(knowledgeIndexTTLEnvKey, "5")
	t.Setenv(knowledgeIndexMaxEntriesEnvKey, "123")
	if got := knowledgeIndexTTL(); got != 5*time.Second {
		t.Fatalf("TTL 环境变量未生效：%v", got)
	}
	if got := knowledgeIndexMaxEntries(); got != 123 {
		t.Fatalf("上限环境变量未生效：%d", got)
	}

	t.Setenv(knowledgeIndexTTLEnvKey, "")
	if got := knowledgeIndexTTL(); got != defaultKnowledgeIndexTTLSeconds*time.Second {
		t.Fatalf("未配置时 TTL 应为默认值：%v", got)
	}

	t.Setenv(knowledgeIndexTTLEnvKey, "0")
	if got := knowledgeIndexTTL(); got != 0 {
		t.Fatalf("TTL=0 应表示禁用缓存：%v", got)
	}

	t.Setenv(knowledgeIndexTTLEnvKey, "not-a-number")
	if got := knowledgeIndexTTL(); got != defaultKnowledgeIndexTTLSeconds*time.Second {
		t.Fatalf("非法 TTL 应回落默认值：%v", got)
	}
	t.Setenv(knowledgeIndexMaxEntriesEnvKey, "-1")
	if got := knowledgeIndexMaxEntries(); got != defaultKnowledgeIndexMaxEntries {
		t.Fatalf("非法上限应回落默认值：%d", got)
	}
}
