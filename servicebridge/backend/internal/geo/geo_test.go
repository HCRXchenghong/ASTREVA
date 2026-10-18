package geo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// writeGeoCSV 在测试目录写入 GeoLite2 CSV 文件。
func writeGeoCSV(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败：%v", name, err)
	}
}

// writeTestGeoDB 写一份包含 IPv4 与一个 IPv6 前缀的最小 GeoLite2 CSV 目录。
func writeTestGeoDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeGeoCSV(t, dir, "GeoLite2-City-Blocks-IPv4.csv",
		"network,geoname_id,registered_country_geoname_id,represented_country_geoname_id,is_anonymous_proxy,is_satellite_provider\n"+
			"1.2.3.0/24,1001,1001,,0,0\n"+
			"8.8.8.0/24,1002,1002,,0,0\n")
	writeGeoCSV(t, dir, "GeoLite2-City-Blocks-IPv6.csv",
		"network,geoname_id,registered_country_geoname_id,represented_country_geoname_id,is_anonymous_proxy,is_satellite_provider\n"+
			"2001:db8::/32,1001,1001,,0,0\n")
	// 同时放 en 与 zh-CN 两个语言文件，断言 zh-CN 优先。
	writeGeoCSV(t, dir, "GeoLite2-City-Locations-en.csv",
		"geoname_id,locale_code,continent_code,continent_name,country_iso_code,country_name,is_in_european_union,subdivision_1_iso_code,subdivision_1_name,subdivision_2_iso_code,subdivision_2_name,city_name,metro_code,time_zone\n"+
			"1001,en,AS,Asia,SA,Saudi Arabia,0,01,Riyadh,,,Riyadh,,Asia/Riyadh\n"+
			"1002,en,NA,North America,US,United States,0,CA,California,,,Mountain View,807,America/Los_Angeles\n")
	writeGeoCSV(t, dir, "GeoLite2-City-Locations-zh-CN.csv",
		"\ufeffgeoname_id,locale_code,continent_code,continent_name,country_iso_code,country_name,is_in_european_union,subdivision_1_iso_code,subdivision_1_name,subdivision_2_iso_code,subdivision_2_name,city_name,metro_code,time_zone\n"+
			"1001,zh-CN,AS,亚洲,SA,沙特阿拉伯,0,01,利雅得省,,,利雅得,,Asia/Riyadh\n"+
			"1002,zh-CN,NA,北美洲,US,美国,0,CA,加利福尼亚州,,,山景城,807,America/Los_Angeles\n")
	return dir
}

// TestCountryFromCDNHeaders 覆盖 CDN 头的优先级、大小写不敏感与占位值过滤。
func TestCountryFromCDNHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("CF-IPCountry", "SA")
	if code, ok := CountryFromCDNHeaders(header); !ok || code != "SA" {
		t.Fatalf("CF-IPCountry 解析 = %q/%v，want SA/true", code, ok)
	}

	header = http.Header{}
	header.Set("cf-ipcountry", "cn")
	if code, ok := CountryFromCDNHeaders(header); !ok || code != "CN" {
		t.Fatalf("小写头名解析 = %q/%v，want CN/true", code, ok)
	}

	// 优先级：CF-IPCountry > X-Vercel-IP-Country > X-Country-Code
	header = http.Header{}
	header.Set("X-Country-Code", "DE")
	header.Set("X-Vercel-IP-Country", "US")
	if code, _ := CountryFromCDNHeaders(header); code != "US" {
		t.Fatalf("Vercel 头应优先于 X-Country-Code，实际 %q", code)
	}

	header = http.Header{}
	header.Set("X-Country-Code", "jp")
	if code, ok := CountryFromCDNHeaders(header); !ok || code != "JP" {
		t.Fatalf("X-Country-Code 解析 = %q/%v，want JP/true", code, ok)
	}

	// 占位值与非法长度视为未命中。
	for _, value := range []string{"XX", "T1", "ZZ", "USA", "1", ""} {
		header = http.Header{}
		header.Set("CF-IPCountry", value)
		if code, ok := CountryFromCDNHeaders(header); ok {
			t.Fatalf("占位/非法值 %q 不应命中，实际 %q", value, code)
		}
	}
	if _, ok := CountryFromCDNHeaders(nil); ok {
		t.Fatal("nil header 不应命中")
	}

	record, ok := RecordFromCDN(http.Header{"Cf-Ipcountry": []string{"SA"}})
	if !ok || record.Basis != domain.GeoBasisCDN || record.CountryCode != "SA" {
		t.Fatalf("RecordFromCDN = %+v/%v", record, ok)
	}
	// country 必须是可读名，而不是 ISO 码。
	if record.Country != "沙特阿拉伯" {
		t.Fatalf("CDN 国家可读名 = %q，want 沙特阿拉伯", record.Country)
	}
}

// TestLocalStoreIPv4IPv6BinarySearch 覆盖本地 CSV 的加载、二分查找与未命中。
func TestLocalStoreIPv4IPv6BinarySearch(t *testing.T) {
	dir := writeTestGeoDB(t)
	store := NewLocalStore(dir)
	if err := store.EnsureLoaded(); err != nil {
		t.Fatalf("加载本地库失败：%v", err)
	}
	if store.LoadedAt().IsZero() {
		t.Fatal("加载时间未记录")
	}

	record, ok := store.Lookup("1.2.3.55")
	if !ok {
		t.Fatal("IPv4 1.2.3.55 应命中")
	}
	if record.Country != "沙特阿拉伯" || record.CountryCode != "SA" {
		t.Fatalf("IPv4 国家 = %q/%q，want 沙特阿拉伯/SA", record.Country, record.CountryCode)
	}
	if record.Region != "利雅得省" || record.City != "利雅得" || record.Timezone != "Asia/Riyadh" {
		t.Fatalf("IPv4 地区/城市/时区 = %q/%q/%q", record.Region, record.City, record.Timezone)
	}
	if record.Basis != domain.GeoBasisLocal {
		t.Fatalf("本地库 basis = %q，want %q", record.Basis, domain.GeoBasisLocal)
	}

	if _, ok := store.Lookup("8.8.8.8"); !ok {
		t.Fatal("IPv4 8.8.8.8 应命中")
	}
	if _, ok := store.Lookup("1.2.4.1"); ok {
		t.Fatal("IPv4 1.2.4.1 不应命中")
	}
	if _, ok := store.Lookup("9.9.9.9"); ok {
		t.Fatal("未知 IPv4 不应命中")
	}
	if _, ok := store.Lookup("not-an-ip"); ok {
		t.Fatal("非法 IP 不应命中")
	}

	record, ok = store.Lookup("2001:db8:1::5")
	if !ok {
		t.Fatal("IPv6 2001:db8:1::5 应命中")
	}
	if record.CountryCode != "SA" || record.Timezone != "Asia/Riyadh" {
		t.Fatalf("IPv6 记录 = %+v", record)
	}
	if _, ok := store.Lookup("2001:dead::1"); ok {
		t.Fatal("IPv6 2001:dead::1 不应命中")
	}
}

// TestLocalStoreUnavailableAndMMDB 覆盖文件缺失与 mmdb 配置：只降级、不 panic。
func TestLocalStoreUnavailableAndMMDB(t *testing.T) {
	empty := t.TempDir()
	store := NewLocalStore(empty)
	if err := store.EnsureLoaded(); err == nil {
		t.Fatal("空目录应返回不可用错误")
	}
	if _, ok := store.Lookup("1.2.3.4"); ok {
		t.Fatal("库不可用时不应命中")
	}

	// 目录不存在。
	missing := NewLocalStore(filepath.Join(empty, "not-exists"))
	if err := missing.EnsureLoaded(); err == nil {
		t.Fatal("目录不存在应返回错误")
	}

	// 配置为 .mmdb 但内容非法：必须返回解析错误而不是 panic，
	// 且不能再出现旧的"未包含 mmdb 解析库"提示（该构建已内置最小 MMDB 读取器）。
	mmdbPath := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	if err := os.WriteFile(mmdbPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入 mmdb 桩失败：%v", err)
	}
	mmdbStore := NewLocalStore(mmdbPath)
	err := mmdbStore.EnsureLoaded()
	if err == nil {
		t.Fatal("非法 mmdb 内容应返回解析错误")
	}
	if strings.Contains(err.Error(), "未包含 mmdb 解析库") {
		t.Fatalf("不应再返回旧的\"未包含 mmdb 解析库\"提示：%v", err)
	}
	if !errors.Is(err, ErrLocalDBUnavailable) || !errors.Is(err, ErrMMDBInvalid) {
		t.Fatalf("mmdb 错误应同时包装 ErrLocalDBUnavailable 与 ErrMMDBInvalid：%v", err)
	}
	if _, ok := mmdbStore.Lookup("1.2.3.4"); ok {
		t.Fatal("mmdb 配置不应命中")
	}

	// .mmdb 文件不存在：同样只降级。
	missingMMDB := NewLocalStore(filepath.Join(empty, "not-exists.mmdb"))
	if err := missingMMDB.EnsureLoaded(); err == nil {
		t.Fatal("mmdb 文件不存在应返回错误")
	}
}

// newThirdPartyStub 起一个第三方 IP 库桩，返回 ip-api 风格响应并统计调用次数。
func newThirdPartyStub(t *testing.T, delay time.Duration) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if delay > 0 {
			time.Sleep(delay)
		}
		ip := strings.TrimPrefix(r.URL.Path, "/ip/")
		w.Header().Set("Content-Type", "application/json")
		if ip == "203.0.113.9" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "countryCode": "SA", "country": "Saudi Arabia",
				"regionName": "Riyadh", "city": "Riyadh", "timezone": "Asia/Riyadh",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "fail", "message": "reserved range"})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestThirdPartyLookupCacheAndNegativeCache 覆盖第三方查询、正/负缓存。
func TestThirdPartyLookupCacheAndNegativeCache(t *testing.T) {
	server, calls := newThirdPartyStub(t, 0)
	client := NewThirdPartyClient(ThirdPartyConfig{
		URLTemplate: server.URL + "/ip/{ip}",
		APIKey:      "secret-key",
		TimeoutMS:   1000,
	})
	record, ok := client.Lookup(context.Background(), "203.0.113.9")
	if !ok {
		t.Fatal("第三方命中失败")
	}
	if record.CountryCode != "SA" || record.Country != "Saudi Arabia" || record.Basis != domain.GeoBasisThirdParty {
		t.Fatalf("第三方记录 = %+v", record)
	}
	if _, ok := client.Lookup(context.Background(), "203.0.113.9"); !ok {
		t.Fatal("第二次查询应命中缓存")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("缓存未生效：调用次数 = %d，want 1", got)
	}

	// 负缓存：未命中的 IP 也不重复请求。
	if _, ok := client.Lookup(context.Background(), "198.51.100.7"); ok {
		t.Fatal("保留地址不应命中")
	}
	if _, ok := client.Lookup(context.Background(), "198.51.100.7"); ok {
		t.Fatal("保留地址不应命中")
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("负缓存未生效：调用次数 = %d，want 2", got)
	}
}

// TestThirdPartyTimeoutDegradesSilently 覆盖第三方超时：静默降级且不阻塞太久。
func TestThirdPartyTimeoutDegradesSilently(t *testing.T) {
	server, _ := newThirdPartyStub(t, 400*time.Millisecond)
	client := NewThirdPartyClient(ThirdPartyConfig{
		URLTemplate: server.URL + "/ip/{ip}",
		TimeoutMS:   60,
	})
	start := time.Now()
	record, ok := client.Lookup(context.Background(), "203.0.113.9")
	elapsed := time.Since(start)
	if ok {
		t.Fatalf("超时不应命中，实际 %+v", record)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("超时降级耗时过长：%v", elapsed)
	}
}

// TestResolverPriority 覆盖三级优先级：CDN 头 → 本地 CSV → 第三方。
func TestResolverPriority(t *testing.T) {
	dir := writeTestGeoDB(t)
	server, calls := newThirdPartyStub(t, 0)
	cfg := Config{
		GeoIPDir:            dir,
		ThirdPartyURL:       server.URL + "/ip/{ip}",
		ThirdPartyKey:       "k",
		ThirdPartyTimeoutMS: 1000,
		CacheTTL:            time.Hour,
	}
	resolver := NewResolver()
	ctx := context.Background()

	// 1) CDN 头优先：即使本地库与第三方都可用，也只用 CDN 头，且不发起第三方请求。
	header := http.Header{}
	header.Set("CF-IPCountry", "US")
	record, ok := resolver.Resolve(ctx, cfg, "1.2.3.55", header)
	if !ok || record.Basis != domain.GeoBasisCDN || record.CountryCode != "US" {
		t.Fatalf("CDN 优先级结果 = %+v/%v", record, ok)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("CDN 命中时不应调用第三方，实际 %d 次", got)
	}

	// 2) 无 CDN 头：本地 CSV 命中，不调用第三方。
	record, ok = resolver.Resolve(ctx, cfg, "1.2.3.55", http.Header{})
	if !ok || record.Basis != domain.GeoBasisLocal || record.City != "利雅得" {
		t.Fatalf("本地库结果 = %+v/%v", record, ok)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("本地库命中时不应调用第三方，实际 %d 次", got)
	}

	// 3) 本地未命中：回落第三方。
	record, ok = resolver.Resolve(ctx, cfg, "203.0.113.9", http.Header{})
	if !ok || record.Basis != domain.GeoBasisThirdParty || record.CountryCode != "SA" {
		t.Fatalf("第三方结果 = %+v/%v", record, ok)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("第三方调用次数 = %d，want 1", got)
	}

	// 4) 三级都未命中：返回 false（调用方留空地理字段）。
	if record, ok := resolver.Resolve(ctx, cfg, "198.51.100.7", http.Header{}); ok {
		t.Fatalf("三级都未命中时应返回 false，实际 %+v", record)
	}

	// 5) IP 结果缓存：同一 IP 再次解析走缓存。
	_, _ = resolver.Resolve(ctx, cfg, "203.0.113.9", http.Header{})
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("IP 缓存未生效：第三方调用次数 = %d，want 2", got)
	}
}

// TestResolverThirdPartyTimeoutDoesNotBlock 覆盖第三方超时时整体解析静默降级。
func TestResolverThirdPartyTimeoutDoesNotBlock(t *testing.T) {
	server, _ := newThirdPartyStub(t, 500*time.Millisecond)
	resolver := NewResolver()
	start := time.Now()
	record, ok := resolver.Resolve(context.Background(), Config{
		ThirdPartyURL:       server.URL + "/ip/{ip}",
		ThirdPartyTimeoutMS: 60,
	}, "203.0.113.9", http.Header{})
	if ok {
		t.Fatalf("超时应降级为未命中，实际 %+v", record)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时降级耗时过长：%v", elapsed)
	}
}

// TestResolverLocalMMDBFallsBackToThirdParty 覆盖本地库配置错误（mmdb）时仍能回落第三方。
func TestResolverLocalMMDBFallsBackToThirdParty(t *testing.T) {
	mmdbPath := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	if err := os.WriteFile(mmdbPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入 mmdb 桩失败：%v", err)
	}
	server, calls := newThirdPartyStub(t, 0)
	resolver := NewResolver()
	cfg := Config{
		GeoIPDir:            mmdbPath,
		ThirdPartyURL:       server.URL + "/ip/{ip}",
		ThirdPartyTimeoutMS: 1000,
	}
	if err := resolver.LocalError(cfg.GeoIPDir); err == nil {
		t.Fatal("mmdb 配置应返回错误")
	}
	record, ok := resolver.Resolve(context.Background(), cfg, "203.0.113.9", http.Header{})
	if !ok || record.Basis != domain.GeoBasisThirdParty {
		t.Fatalf("mmdb 配置下应回落第三方，实际 %+v/%v", record, ok)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("第三方调用次数 = %d，want 1", got)
	}
}
