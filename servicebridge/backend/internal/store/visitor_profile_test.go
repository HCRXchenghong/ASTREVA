package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// TestMemoryVisitorProfileSettings 覆盖画像配置的默认值、局部更新与密钥掩码语义。
func TestMemoryVisitorProfileSettings(t *testing.T) {
	st := NewMemoryStore(Options{})

	defaults := st.VisitorProfileSettings()
	if !defaults.Enabled || !defaults.ShowFullIP || !defaults.AISummaryEnabled {
		t.Fatalf("默认开关不符：%+v", defaults)
	}
	if defaults.ThirdPartyTimeoutMS != domain.DefaultThirdPartyTimeoutMS ||
		defaults.CacheTTLHours != domain.DefaultThirdPartyCacheTTLHour ||
		defaults.AISummaryEveryMessages != domain.DefaultAISummaryEveryMessages {
		t.Fatalf("默认数值不符：%+v", defaults)
	}
	if defaults.ThirdPartyKeyMasked != "" {
		t.Fatalf("默认不应有密钥掩码：%q", defaults.ThirdPartyKeyMasked)
	}

	// 写入密钥与目录：返回掩码，不返回明文。
	dir := "/data/geolite2"
	url := "https://ip.example.com/{ip}"
	key := "super-secret-key-1234"
	updated, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{
		GeoIPDir:      &dir,
		ThirdPartyURL: &url,
		ThirdPartyKey: &key,
	})
	if err != nil {
		t.Fatalf("更新配置失败：%v", err)
	}
	if updated.GeoIPDir != dir || updated.ThirdPartyURL != url {
		t.Fatalf("目录/URL 未保存：%+v", updated)
	}
	if updated.ThirdPartyKey != key {
		t.Fatalf("服务端内部应可读明文密钥：%q", updated.ThirdPartyKey)
	}
	if updated.ThirdPartyKeyMasked != "****1234" {
		t.Fatalf("密钥掩码 = %q，want ****1234", updated.ThirdPartyKeyMasked)
	}

	// 不传密钥 = 不改。
	enabled := false
	updated, err = st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{Enabled: &enabled})
	if err != nil {
		t.Fatalf("更新开关失败：%v", err)
	}
	if updated.Enabled || updated.ThirdPartyKey != key {
		t.Fatalf("不传密钥不应清除：%+v", updated)
	}

	// 传空串 = 清除。
	empty := ""
	updated, err = st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyKey: &empty})
	if err != nil {
		t.Fatalf("清除密钥失败：%v", err)
	}
	if updated.ThirdPartyKey != "" || updated.ThirdPartyKeyMasked != "" {
		t.Fatalf("密钥应已清除：%+v", updated)
	}

	// 非法 URL：缺少 {ip} 占位符或非 http(s) 必须报 ErrInvalidInput。
	badURL := "https://ip.example.com/lookup"
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyURL: &badURL}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("缺少 {ip} 应返回 ErrInvalidInput，实际 %v", err)
	}
	badScheme := "ftp://ip.example.com/{ip}"
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyURL: &badScheme}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非 http(s) 应返回 ErrInvalidInput，实际 %v", err)
	}

	// 越界数值收敛到合法区间。
	tooSmall := 1
	tooBig := 999999
	updated, err = st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{
		ThirdPartyTimeoutMS:    &tooSmall,
		CacheTTLHours:          &tooBig,
		AISummaryEveryMessages: &tooBig,
	})
	if err != nil {
		t.Fatalf("收敛更新失败：%v", err)
	}
	if updated.ThirdPartyTimeoutMS != domain.MinThirdPartyTimeoutMS {
		t.Fatalf("超时下限收敛 = %d", updated.ThirdPartyTimeoutMS)
	}
	if updated.CacheTTLHours != domain.MaxCacheTTLHours {
		t.Fatalf("缓存上限收敛 = %d", updated.CacheTTLHours)
	}
	if updated.AISummaryEveryMessages != domain.MaxAISummaryEveryMessages {
		t.Fatalf("摘要间隔上限收敛 = %d", updated.AISummaryEveryMessages)
	}
}

// TestMemoryVisitorProfileRoundTrip 覆盖画像创建/更新/读取与 JSONB 字段往返。
func TestMemoryVisitorProfileRoundTrip(t *testing.T) {
	st := NewMemoryStore(Options{})
	visitorID := "vis_round_trip"

	if _, ok := st.VisitorProfile(visitorID); ok {
		t.Fatal("未写入时不应读到画像")
	}

	profile, err := st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		profile.Country = "沙特阿拉伯"
		profile.CountryCode = "SA"
		profile.Region = "利雅得省"
		profile.City = "利雅得"
		profile.Timezone = "Asia/Riyadh"
		profile.NationalityGuess = "SA"
		profile.NationalityConfidence = 0.8
		profile.NationalityBasis = []string{domain.GeoBasisLocal, domain.GeoBasisTimezone}
		profile.IPFirst = "1.2.3.4"
		profile.IPLast = "1.2.3.9"
		profile.Languages = map[string]int{"ar": 5, "en": 2}
		profile.Dialect = "ar-Gulf"
		profile.CodeSwitching = true
		profile.Summary = "关注报价与工期"
		profile.Tags = []string{"高意向", "询价"}
		profile.Interests = []string{"太阳能"}
		profile.Evidence = []domain.VisitorProfileEvidence{{
			MessageID: "msg_1", Text: "كم السعر؟", CreatedAt: time.Now().UTC(),
		}}
		profile.Visits = 3
		profile.LastPath = "/ar/pricing"
		return nil
	})
	if err != nil {
		t.Fatalf("写入画像失败：%v", err)
	}
	if profile.VisitorID != visitorID || profile.CreatedAt.IsZero() || profile.UpdatedAt.IsZero() {
		t.Fatalf("画像元数据不符：%+v", profile)
	}

	read, ok := st.VisitorProfile(visitorID)
	if !ok {
		t.Fatal("画像应可读回")
	}
	if read.Country != "沙特阿拉伯" || read.CountryCode != "SA" || read.City != "利雅得" {
		t.Fatalf("地理字段往返失败：%+v", read)
	}
	if len(read.NationalityBasis) != 2 || read.NationalityBasis[0] != domain.GeoBasisLocal {
		t.Fatalf("nationality_basis 往返失败：%+v", read.NationalityBasis)
	}
	if read.Languages["ar"] != 5 || read.Languages["en"] != 2 {
		t.Fatalf("languages 往返失败：%+v", read.Languages)
	}
	if len(read.Tags) != 2 || len(read.Interests) != 1 || len(read.Evidence) != 1 {
		t.Fatalf("JSONB 集合往返失败：%+v", read)
	}
	if read.Evidence[0].MessageID != "msg_1" {
		t.Fatalf("evidence 往返失败：%+v", read.Evidence)
	}
	if read.NationalityConfidence != 0.8 {
		t.Fatalf("置信度往返失败：%v", read.NationalityConfidence)
	}

	// 读取结果是深拷贝：外部修改不影响存储。
	read.Languages["ar"] = 999
	again, _ := st.VisitorProfile(visitorID)
	if again.Languages["ar"] != 5 {
		t.Fatalf("画像未做深拷贝：%+v", again.Languages)
	}

	// mutate 返回错误时不写入。
	sentinel := errors.New("boom")
	if _, err := st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		profile.Summary = "不应写入"
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("mutate 错误应原样返回，实际 %v", err)
	}
	if current, _ := st.VisitorProfile(visitorID); current.Summary != "关注报价与工期" {
		t.Fatalf("mutate 失败后不应写入：%q", current.Summary)
	}
}

// TestMemoryVisitorProfileConcurrentMutate 覆盖同访客并发更新不丢计数（存储层串行化）。
func TestMemoryVisitorProfileConcurrentMutate(t *testing.T) {
	st := NewMemoryStore(Options{})
	visitorID := "vis_concurrent"
	const workers = 20
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
				if profile.Languages == nil {
					profile.Languages = map[string]int{}
				}
				profile.Languages["en"]++
				profile.Visits++
				return nil
			})
		}()
	}
	wait.Wait()
	profile, ok := st.VisitorProfile(visitorID)
	if !ok {
		t.Fatal("并发写入后应存在画像")
	}
	if profile.Visits != workers || profile.Languages["en"] != workers {
		t.Fatalf("并发计数丢失：visits=%d languages=%+v", profile.Visits, profile.Languages)
	}
}

// TestMemoryVisitorProfilesFilterAndPaging 覆盖列表筛选与分页。
func TestMemoryVisitorProfilesFilterAndPaging(t *testing.T) {
	st := NewMemoryStore(Options{})
	seed := []struct {
		id        string
		country   string
		code      string
		city      string
		summary   string
		tags      []string
		ip        string
		updatedAt time.Time
	}{
		{"vis_a", "沙特阿拉伯", "SA", "利雅得", "询价太阳能项目", []string{"高意向"}, "1.2.3.4", time.Now().UTC().Add(-3 * time.Hour)},
		{"vis_b", "美国", "US", "山景城", "咨询 API 文档", []string{"技术"}, "5.6.7.8", time.Now().UTC().Add(-2 * time.Hour)},
		{"vis_c", "中国", "CN", "上海", "售后问题", []string{"售后"}, "9.9.9.9", time.Now().UTC().Add(-1 * time.Hour)},
	}
	for _, item := range seed {
		item := item
		if _, err := st.UpdateVisitorProfile(item.id, func(profile *domain.VisitorProfile) error {
			profile.Country = item.country
			profile.CountryCode = item.code
			profile.City = item.city
			profile.Summary = item.summary
			profile.Tags = item.tags
			profile.IPLast = item.ip
			profile.UpdatedAt = item.updatedAt
			return nil
		}); err != nil {
			t.Fatalf("写入 %s 失败：%v", item.id, err)
		}
		// UpdateVisitorProfile 会覆盖 updated_at，这里直接改内部时间以固定排序。
		st.mu.Lock()
		if profile, ok := st.visitorProfiles[item.id]; ok {
			profile.UpdatedAt = item.updatedAt
		}
		st.mu.Unlock()
	}

	all, total := st.VisitorProfiles(domain.VisitorProfileQuery{})
	if total != 3 || len(all) != 3 {
		t.Fatalf("列表 = %d/%d，want 3/3", len(all), total)
	}
	if all[0].VisitorID != "vis_c" {
		t.Fatalf("应按最近更新倒序，首条 = %s", all[0].VisitorID)
	}

	byCountry, total := st.VisitorProfiles(domain.VisitorProfileQuery{Country: "SA"})
	if total != 1 || len(byCountry) != 1 || byCountry[0].VisitorID != "vis_a" {
		t.Fatalf("国家码筛选失败：%+v (total=%d)", byCountry, total)
	}
	// 中文名筛选也要能命中。
	if byName, total := st.VisitorProfiles(domain.VisitorProfileQuery{Country: "美国"}); total != 1 || byName[0].VisitorID != "vis_b" {
		t.Fatalf("国家名筛选失败：%+v (total=%d)", byName, total)
	}

	byTag, total := st.VisitorProfiles(domain.VisitorProfileQuery{Tag: "高意向"})
	if total != 1 || byTag[0].VisitorID != "vis_a" {
		t.Fatalf("标签筛选失败：%+v (total=%d)", byTag, total)
	}

	byQuery, total := st.VisitorProfiles(domain.VisitorProfileQuery{Query: "9.9.9.9"})
	if total != 1 || byQuery[0].VisitorID != "vis_c" {
		t.Fatalf("关键词筛选失败：%+v (total=%d)", byQuery, total)
	}
	if bySummary, total := st.VisitorProfiles(domain.VisitorProfileQuery{Query: "太阳能"}); total != 1 || bySummary[0].VisitorID != "vis_a" {
		t.Fatalf("摘要关键词筛选失败：%+v (total=%d)", bySummary, total)
	}

	page, total := st.VisitorProfiles(domain.VisitorProfileQuery{Limit: 2})
	if total != 3 || len(page) != 2 {
		t.Fatalf("分页第一页 = %d/%d", len(page), total)
	}
	page2, total := st.VisitorProfiles(domain.VisitorProfileQuery{Limit: 2, Offset: 2})
	if total != 3 || len(page2) != 1 || page2[0].VisitorID != "vis_a" {
		t.Fatalf("分页第二页 = %+v (total=%d)", page2, total)
	}
	if beyond, _ := st.VisitorProfiles(domain.VisitorProfileQuery{Limit: 2, Offset: 99}); len(beyond) != 0 {
		t.Fatalf("越界分页应为空：%+v", beyond)
	}
}

// TestMemoryVisitorProfileCleanup 覆盖显式删除与"会话清空后随访客数据清理"。
func TestMemoryVisitorProfileCleanup(t *testing.T) {
	st := NewMemoryStore(Options{})
	session, err := st.CreateVisitorConversation("1.2.3.4", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	visitorID := session.Conversation.VisitorID
	if _, err := st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		profile.Visits = 1
		return nil
	}); err != nil {
		t.Fatalf("写入画像失败：%v", err)
	}

	// 访客维度会话列表。
	conversations := st.ConversationsForVisitor(visitorID)
	if len(conversations) != 1 || conversations[0].ID != session.Conversation.ID {
		t.Fatalf("ConversationsForVisitor = %+v", conversations)
	}

	// 显式删除。
	if err := st.DeleteVisitorProfile(visitorID); err != nil {
		t.Fatalf("删除画像失败：%v", err)
	}
	if _, ok := st.VisitorProfile(visitorID); ok {
		t.Fatal("画像应已删除")
	}
	if err := st.DeleteVisitorProfile(visitorID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}

	// 会话删除后随访客数据清理。
	if _, err := st.UpdateVisitorProfile(visitorID, nil); err != nil {
		t.Fatalf("重建画像失败：%v", err)
	}
	admin := domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_test"}
	if _, _, err := st.CloseConversation(admin, session.Conversation.ID); err != nil {
		t.Fatalf("关闭会话失败：%v", err)
	}
	if err := st.DeleteConversation(admin, session.Conversation.ID); err != nil {
		t.Fatalf("删除会话失败：%v", err)
	}
	if _, ok := st.VisitorProfile(visitorID); ok {
		t.Fatal("该访客已无会话，画像应随数据保留策略清理")
	}
}

// TestMemoryVisitorProfileRetentionSettings 覆盖「访客画像保留天数」的默认值、校验与区间。
func TestMemoryVisitorProfileRetentionSettings(t *testing.T) {
	st := NewMemoryStore(Options{})

	// 默认 180 天（后台给的可选项之一）。
	if got := st.VisitorProfileSettings().RetentionDays; got != domain.DefaultVisitorProfileRetentionDays {
		t.Fatalf("默认保留天数 = %d，期望 %d", got, domain.DefaultVisitorProfileRetentionDays)
	}

	// 30/90/180 与 0（永久）都必须被接受。
	for _, days := range []int{30, 90, 180, 0} {
		value := days
		updated, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &value})
		if err != nil {
			t.Fatalf("retention_days=%d 应被接受，实际 %v", days, err)
		}
		if updated.RetentionDays != days {
			t.Fatalf("retention_days 写入后 = %d，期望 %d", updated.RetentionDays, days)
		}
	}

	// 1..6 天毫无意义，必须明确拒绝（ErrInvalidInput），而不是悄悄改成别的值。
	for _, days := range []int{1, 6} {
		value := days
		if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &value}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("retention_days=%d 应返回 ErrInvalidInput，实际 %v", days, err)
		}
	}
	// 存储层对越界值做防御性收敛（与 timeout/cache 字段一致），负数按"未设置"回落默认 180：
	// 控制台走的是 HTTP 接口，那里对 3651 与 -5 都会直接 400（见 httpx 校验），不会静默改写用户意图。
	over := 3651
	clamped, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &over})
	if err != nil {
		t.Fatalf("越界值存储层应收敛而不是报错，实际 %v", err)
	}
	if clamped.RetentionDays != domain.MaxVisitorProfileRetentionDays {
		t.Fatalf("越界值应收敛到 %d，实际 %d", domain.MaxVisitorProfileRetentionDays, clamped.RetentionDays)
	}
	negative := -5
	fallback, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &negative})
	if err != nil {
		t.Fatalf("负值存储层应回落默认，实际 %v", err)
	}
	if fallback.RetentionDays != domain.DefaultVisitorProfileRetentionDays {
		t.Fatalf("负值应回落到默认 %d，实际 %d", domain.DefaultVisitorProfileRetentionDays, fallback.RetentionDays)
	}
	// 复原为永久保留，供下面的断言使用。
	zero := 0
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &zero}); err != nil {
		t.Fatalf("复原为永久保留失败：%v", err)
	}

	// 非法值不应污染已保存的设置。
	if got := st.VisitorProfileSettings().RetentionDays; got != 0 {
		t.Fatalf("非法写入后保留天数应保持为上一次合法值 0，实际 %d", got)
	}
}

// TestMemoryPurgeExpiredVisitorProfiles 验证按保留天数清理：只删超期的，未超期与空时间不动。
func TestMemoryPurgeExpiredVisitorProfiles(t *testing.T) {
	st := NewMemoryStore(Options{})
	now := time.Now().UTC()

	stale, err := st.CreateVisitorConversation("1.1.1.1", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	fresh, err := st.CreateVisitorConversation("2.2.2.2", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}

	old := now.AddDate(0, 0, -200)
	if _, err := st.UpdateVisitorProfile(stale.Conversation.VisitorID, func(profile *domain.VisitorProfile) error {
		profile.LastSeenAt = &old
		profile.UpdatedAt = old
		return nil
	}); err != nil {
		t.Fatalf("写入旧画像失败：%v", err)
	}
	if _, err := st.UpdateVisitorProfile(fresh.Conversation.VisitorID, func(profile *domain.VisitorProfile) error {
		profile.LastSeenAt = &now
		profile.UpdatedAt = now
		return nil
	}); err != nil {
		t.Fatalf("写入新画像失败：%v", err)
	}

	// 180 天保留：200 天前的画像该被清掉，今天的必须留下。
	removed, err := st.PurgeVisitorProfilesBefore(now.AddDate(0, 0, -180))
	if err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if removed != 1 {
		t.Fatalf("应清理 1 条超期画像，实际 %d", removed)
	}
	if _, ok := st.VisitorProfile(stale.Conversation.VisitorID); ok {
		t.Fatal("200 天前的画像应被清理")
	}
	if _, ok := st.VisitorProfile(fresh.Conversation.VisitorID); !ok {
		t.Fatal("未超期的画像不应被清理")
	}

	// 边界：cutoff 正好等于 last_seen_at 时不算超期（严格早于才删）。
	removed, err = st.PurgeVisitorProfilesBefore(now.Add(time.Second))
	if err != nil {
		t.Fatalf("边界清理失败：%v", err)
	}
	if removed != 1 {
		t.Fatalf("cutoff 之后应清理掉最后一条，实际 %d", removed)
	}
}
