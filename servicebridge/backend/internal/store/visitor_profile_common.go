package store

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"customer-service/backend/internal/domain"
)

// ===== 访客画像（P5）公共校验与序列化 =====
// 内存与 PostgreSQL 两个实现共用，保证对外表现一致。

const (
	maxGeoIPDirRunes       = 500
	maxThirdPartyURLRunes  = 1000
	maxThirdPartyKeyRunes  = 500
	maxVisitorIDRunes      = 128
	maxProfilePathRunes    = 500
	maxProfileSummaryRunes = 4000
	maxProfileListLimit    = 200
)

// normalizeVisitorProfileSettings 归一化并校验画像配置。
func normalizeVisitorProfileSettings(next domain.VisitorProfileSettings) (domain.VisitorProfileSettings, error) {
	out := domain.NormalizeVisitorProfileSettings(next)
	if _, err := cleanOptionalText(out.GeoIPDir, maxGeoIPDirRunes); err != nil {
		return domain.VisitorProfileSettings{}, err
	}
	if _, err := cleanOptionalText(out.ThirdPartyURL, maxThirdPartyURLRunes); err != nil {
		return domain.VisitorProfileSettings{}, err
	}
	if _, err := cleanOptionalText(out.ThirdPartyKey, maxThirdPartyKeyRunes); err != nil {
		return domain.VisitorProfileSettings{}, err
	}
	if out.ThirdPartyURL != "" {
		parsed, err := url.Parse(out.ThirdPartyURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return domain.VisitorProfileSettings{}, fmt.Errorf("%w: third_party_url 必须是 http/https 地址", ErrInvalidInput)
		}
		if !strings.Contains(out.ThirdPartyURL, "{ip}") {
			return domain.VisitorProfileSettings{}, fmt.Errorf("%w: third_party_url 必须包含 {ip} 占位符", ErrInvalidInput)
		}
	}
	return out, nil
}

// applyVisitorProfileSettingsPatch 把局部更新合并到当前配置上（nil 字段保持不变）。
func applyVisitorProfileSettingsPatch(current domain.VisitorProfileSettings, patch domain.VisitorProfileSettingsPatch) domain.VisitorProfileSettings {
	next := current
	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.GeoIPDir != nil {
		next.GeoIPDir = strings.TrimSpace(*patch.GeoIPDir)
	}
	if patch.ThirdPartyURL != nil {
		next.ThirdPartyURL = strings.TrimSpace(*patch.ThirdPartyURL)
	}
	if patch.ThirdPartyKey != nil {
		// nil=不改；空串=清除；其它=覆盖。
		next.ThirdPartyKey = strings.TrimSpace(*patch.ThirdPartyKey)
	}
	if patch.ThirdPartyTimeoutMS != nil {
		next.ThirdPartyTimeoutMS = *patch.ThirdPartyTimeoutMS
	}
	if patch.CacheTTLHours != nil {
		next.CacheTTLHours = *patch.CacheTTLHours
	}
	if patch.AISummaryEnabled != nil {
		next.AISummaryEnabled = *patch.AISummaryEnabled
	}
	if patch.AISummaryEveryMessages != nil {
		next.AISummaryEveryMessages = *patch.AISummaryEveryMessages
	}
	if patch.ShowFullIP != nil {
		next.ShowFullIP = *patch.ShowFullIP
	}
	return next
}

// normalizeVisitorProfileInput 归一化画像写入内容并校验访客 ID。
func normalizeVisitorProfileInput(profile domain.VisitorProfile) (domain.VisitorProfile, error) {
	out := domain.NormalizeVisitorProfile(profile)
	if out.VisitorID == "" {
		return domain.VisitorProfile{}, fmt.Errorf("%w: visitor_id 不能为空", ErrInvalidInput)
	}
	if runeLen(out.VisitorID) > maxVisitorIDRunes {
		return domain.VisitorProfile{}, fmt.Errorf("%w: visitor_id 过长", ErrInvalidInput)
	}
	if _, err := cleanOptionalText(out.LastPath, maxProfilePathRunes); err != nil {
		return domain.VisitorProfile{}, err
	}
	if _, err := cleanOptionalText(out.Summary, maxProfileSummaryRunes); err != nil {
		return domain.VisitorProfile{}, err
	}
	return out, nil
}

// normalizeVisitorProfileQuery 归一化列表查询条件（限制分页上限，避免全表拉取）。
func normalizeVisitorProfileQuery(query domain.VisitorProfileQuery) domain.VisitorProfileQuery {
	out := query
	out.Query = strings.TrimSpace(out.Query)
	out.Country = strings.ToUpper(strings.TrimSpace(out.Country))
	out.Tag = strings.TrimSpace(out.Tag)
	if out.Limit <= 0 {
		out.Limit = 50
	}
	if out.Limit > maxProfileListLimit {
		out.Limit = maxProfileListLimit
	}
	if out.Offset < 0 {
		out.Offset = 0
	}
	return out
}

// visitorProfileMatchesQuery 判断画像是否命中筛选条件（内存实现与 PG 语义保持一致）。
func visitorProfileMatchesQuery(profile domain.VisitorProfile, query domain.VisitorProfileQuery) bool {
	if query.Country != "" {
		code := domain.NormalizeCountryCode(query.Country)
		if code != "" {
			if profile.CountryCode != code {
				return false
			}
		} else if !strings.Contains(strings.ToUpper(profile.Country), query.Country) {
			return false
		}
	}
	if query.Tag != "" && !containsFold(profile.Tags, query.Tag) {
		return false
	}
	if query.Query != "" {
		haystack := strings.ToLower(strings.Join([]string{
			profile.VisitorID, profile.IPFirst, profile.IPLast, profile.Summary,
			profile.Country, profile.CountryCode, profile.City, profile.Region,
		}, "\n"))
		if !strings.Contains(haystack, strings.ToLower(query.Query)) {
			return false
		}
	}
	return true
}

func containsFold(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

// cloneVisitorProfile 深拷贝画像，避免调用方修改内部 map/slice。
func cloneVisitorProfile(profile domain.VisitorProfile) domain.VisitorProfile {
	out := profile
	out.NationalityBasis = append([]string(nil), profile.NationalityBasis...)
	out.Tags = append([]string(nil), profile.Tags...)
	out.Interests = append([]string(nil), profile.Interests...)
	out.Evidence = append([]domain.VisitorProfileEvidence(nil), profile.Evidence...)
	out.Languages = map[string]int{}
	for lang, count := range profile.Languages {
		out.Languages[lang] = count
	}
	if out.NationalityBasis == nil {
		out.NationalityBasis = []string{}
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if out.Interests == nil {
		out.Interests = []string{}
	}
	if out.Evidence == nil {
		out.Evidence = []domain.VisitorProfileEvidence{}
	}
	return out
}

// ===== JSONB 序列化/反序列化（两个实现共用）=====

func marshalProfileListJSON(values []string) string {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseProfileListJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	cleaned := make([]string, 0, len(out))
	seen := map[string]bool{}
	for _, value := range out {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func marshalLanguageCountsJSON(counts map[string]int) string {
	if counts == nil {
		counts = map[string]int{}
	}
	raw, err := json.Marshal(counts)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func parseLanguageCountsJSON(raw string) map[string]int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]int{}
	}
	var out map[string]int
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]int{}
	}
	cleaned := map[string]int{}
	for lang, count := range out {
		lang = strings.TrimSpace(lang)
		if lang == "" || count <= 0 {
			continue
		}
		cleaned[lang] = count
	}
	return cleaned
}

func marshalEvidenceJSON(entries []domain.VisitorProfileEvidence) string {
	if entries == nil {
		entries = []domain.VisitorProfileEvidence{}
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseEvidenceJSON(raw string) []domain.VisitorProfileEvidence {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []domain.VisitorProfileEvidence{}
	}
	var out []domain.VisitorProfileEvidence
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []domain.VisitorProfileEvidence{}
	}
	cleaned := make([]domain.VisitorProfileEvidence, 0, len(out))
	seen := map[string]bool{}
	for _, entry := range out {
		entry.MessageID = strings.TrimSpace(entry.MessageID)
		if entry.MessageID == "" || seen[entry.MessageID] {
			continue
		}
		seen[entry.MessageID] = true
		cleaned = append(cleaned, entry)
	}
	return cleaned
}

// sortProfilesByUpdated 按最近更新倒序排列（同时间按访客 ID 升序，保证稳定）。
func sortProfilesByUpdated(profiles []domain.VisitorProfile) {
	sort.Slice(profiles, func(i, j int) bool {
		if !profiles[i].UpdatedAt.Equal(profiles[j].UpdatedAt) {
			return profiles[i].UpdatedAt.After(profiles[j].UpdatedAt)
		}
		return profiles[i].VisitorID < profiles[j].VisitorID
	})
}
