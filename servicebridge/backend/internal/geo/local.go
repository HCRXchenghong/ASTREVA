package geo

import (
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/domain"
)

// LocalStore 是本地 GeoLite2 CSV 库（零依赖、惰性加载、进程内缓存）。
//
// 期望目录下存在：
//
//	GeoLite2-City-Blocks-IPv4.csv
//	GeoLite2-City-Blocks-IPv6.csv
//	GeoLite2-City-Locations-<lang>.csv   （语言文件择优：zh-CN → en → 任意一个）
//
// Blocks 至少使用 network / geoname_id 两列；Locations 使用 geoname_id /
// country_iso_code / country_name / subdivision_1_name / city_name / time_zone
// （官方列名为 time_zone，同时兼容 timezone 写法）。
type LocalStore struct {
	dir string

	mu       sync.Mutex
	loaded   bool
	loadErr  error
	loadedAt time.Time

	v4 []v4Range
	v6 []v6Range
}

type location struct {
	country     string
	countryCode string
	region      string
	city        string
	timezone    string
}

type v4Range struct {
	start uint32
	end   uint32
	loc   location
}

type v6Range struct {
	startHi uint64
	startLo uint64
	endHi   uint64
	endLo   uint64
	loc     location
}

// NewLocalStore 创建本地 CSV 库句柄（不立即加载）。
func NewLocalStore(dir string) *LocalStore {
	return &LocalStore{dir: strings.TrimSpace(dir)}
}

// Dir 返回配置的目录。
func (s *LocalStore) Dir() string { return s.dir }

// LoadedAt 返回最近一次成功加载的时间。
func (s *LocalStore) LoadedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadedAt
}

// LoadError 返回加载错误（未加载时为 nil）。
func (s *LocalStore) LoadError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// EnsureLoaded 首次调用时加载 CSV；失败只记录状态，不会重试、不会 panic。
func (s *LocalStore) EnsureLoaded() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.loadErr
	}
	s.loaded = true
	v4, v6, err := loadCSVDir(s.dir)
	if err != nil {
		s.loadErr = err
		return err
	}
	s.v4 = v4
	s.v6 = v6
	s.loadedAt = time.Now().UTC()
	return nil
}

// Lookup 按 IP 二分查找本地库；未命中或库不可用时返回 false。
func (s *LocalStore) Lookup(ip string) (domain.GeoRecord, bool) {
	if err := s.EnsureLoaded(); err != nil {
		return domain.GeoRecord{}, false
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return domain.GeoRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v4 := parsed.To4(); v4 != nil {
		value := binary.BigEndian.Uint32(v4)
		index := sort.Search(len(s.v4), func(i int) bool { return s.v4[i].start > value })
		if index == 0 {
			return domain.GeoRecord{}, false
		}
		entry := s.v4[index-1]
		if value > entry.end {
			return domain.GeoRecord{}, false
		}
		return entry.loc.record(domain.GeoBasisLocal), true
	}
	hi, lo := ipv6ToUint128(parsed.To16())
	index := sort.Search(len(s.v6), func(i int) bool {
		entry := s.v6[i]
		return entry.startHi > hi || (entry.startHi == hi && entry.startLo > lo)
	})
	if index == 0 {
		return domain.GeoRecord{}, false
	}
	entry := s.v6[index-1]
	if compareUint128(hi, lo, entry.endHi, entry.endLo) > 0 {
		return domain.GeoRecord{}, false
	}
	return entry.loc.record(domain.GeoBasisLocal), true
}

// record 把内部定位记录转换为对外地理记录：country 保证是可读名（ISO 码时用内置中文名）。
func (l location) record(basis string) domain.GeoRecord {
	country := strings.TrimSpace(l.country)
	code := domain.NormalizeCountryCode(l.countryCode)
	// CSV 里的 country_name 是名称；若缺列或本身就是 ISO 码，则用内置名称表补齐。
	if country == "" || domain.NormalizeCountryCode(country) != "" {
		country = domain.CountryName(code)
	}
	return domain.GeoRecord{
		Country:     country,
		CountryCode: code,
		Region:      strings.TrimSpace(l.region),
		City:        strings.TrimSpace(l.city),
		Timezone:    strings.TrimSpace(l.timezone),
		Basis:       basis,
	}
}

// loadCSVDir 加载目录下的 Blocks + Locations 并构建排序后的区间表。
func loadCSVDir(dir string) ([]v4Range, []v6Range, error) {
	if dir == "" {
		return nil, nil, fmt.Errorf("%w：未配置 geoip_dir", ErrLocalDBUnavailable)
	}
	// 配置成 .mmdb 文件时给出明确错误，绝不 panic。
	if strings.HasSuffix(strings.ToLower(dir), ".mmdb") {
		return nil, nil, ErrMMDBNotSupported
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("%w：目录 %s 不可访问（%v）", ErrLocalDBUnavailable, dir, err)
	}
	if !info.IsDir() {
		if strings.HasSuffix(strings.ToLower(dir), ".mmdb") {
			return nil, nil, ErrMMDBNotSupported
		}
		return nil, nil, fmt.Errorf("%w：%s 不是目录", ErrLocalDBUnavailable, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("%w：读取目录 %s 失败（%v）", ErrLocalDBUnavailable, dir, err)
	}
	locationsFile := pickLocationsFile(entries)
	if locationsFile == "" {
		return nil, nil, fmt.Errorf("%w：缺少 GeoLite2-City-Locations-<lang>.csv", ErrLocalDBUnavailable)
	}
	locations, err := loadLocations(filepath.Join(dir, locationsFile))
	if err != nil {
		return nil, nil, err
	}
	v4File := pickBlocksFile(entries, "Blocks-IPv4")
	v6File := pickBlocksFile(entries, "Blocks-IPv6")
	if v4File == "" && v6File == "" {
		return nil, nil, fmt.Errorf("%w：缺少 GeoLite2-City-Blocks-IPv4.csv / GeoLite2-City-Blocks-IPv6.csv", ErrLocalDBUnavailable)
	}
	var v4 []v4Range
	var v6 []v6Range
	if v4File != "" {
		v4, err = loadBlocksV4(filepath.Join(dir, v4File), locations)
		if err != nil {
			return nil, nil, err
		}
	}
	if v6File != "" {
		v6, err = loadBlocksV6(filepath.Join(dir, v6File), locations)
		if err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(v4, func(i, j int) bool { return v4[i].start < v4[j].start })
	sort.Slice(v6, func(i, j int) bool {
		if v6[i].startHi != v6[j].startHi {
			return v6[i].startHi < v6[j].startHi
		}
		return v6[i].startLo < v6[j].startLo
	})
	return v4, v6, nil
}

// pickLocationsFile 按 zh-CN → en → 任意一个 的优先级选择语言文件。
func pickLocationsFile(entries []os.DirEntry) string {
	candidates := make([]string, 0, 4)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "GeoLite2-City-Locations-") || !strings.HasSuffix(strings.ToLower(name), ".csv") {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	for _, want := range []string{"zh-CN", "en"} {
		for _, name := range candidates {
			if strings.Contains(name, "-Locations-"+want+".") {
				return name
			}
		}
	}
	return candidates[0]
}

// pickBlocksFile 选择 Blocks 文件（先精确名，再按关键字匹配）。
func pickBlocksFile(entries []os.DirEntry, keyword string) string {
	exact := "GeoLite2-City-" + keyword + ".csv"
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() == exact {
			return exact
		}
	}
	candidates := make([]string, 0, 2)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, keyword) && strings.HasSuffix(strings.ToLower(name), ".csv") {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	return candidates[0]
}

// loadLocations 读取 Locations CSV，返回 geoname_id -> 定位信息。
func loadLocations(path string) (map[string]location, error) {
	rows, closer, err := openCSV(path)
	if err != nil {
		return nil, fmt.Errorf("%w：打开 %s 失败（%v）", ErrLocalDBUnavailable, path, err)
	}
	defer closer()
	header, err := rows.Read()
	if err != nil {
		return nil, fmt.Errorf("%w：%s 表头读取失败（%v）", ErrLocalDBUnavailable, path, err)
	}
	columns := indexColumns(header)
	geonameIndex, ok := columns["geoname_id"]
	if !ok {
		return nil, fmt.Errorf("%w：%s 缺少 geoname_id 列", ErrLocalDBUnavailable, path)
	}
	out := map[string]location{}
	for {
		record, err := rows.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w：%s 解析失败（%v）", ErrLocalDBUnavailable, path, err)
		}
		geonameID := strings.TrimSpace(field(record, geonameIndex))
		if geonameID == "" {
			continue
		}
		out[geonameID] = location{
			country:     field(record, columns["country_name"]),
			countryCode: field(record, columns["country_iso_code"]),
			region:      field(record, columns["subdivision_1_name"]),
			city:        field(record, columns["city_name"]),
			timezone:    firstNonEmpty(field(record, columns["time_zone"]), field(record, columns["timezone"])),
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w：%s 没有可用数据行", ErrLocalDBUnavailable, path)
	}
	return out, nil
}

// loadBlocksV4 读取 IPv4 Blocks 并展开为 start/end（uint32）区间。
func loadBlocksV4(path string, locations map[string]location) ([]v4Range, error) {
	rows, closer, err := openCSV(path)
	if err != nil {
		return nil, fmt.Errorf("%w：打开 %s 失败（%v）", ErrLocalDBUnavailable, path, err)
	}
	defer closer()
	header, err := rows.Read()
	if err != nil {
		return nil, fmt.Errorf("%w：%s 表头读取失败（%v）", ErrLocalDBUnavailable, path, err)
	}
	columns := indexColumns(header)
	networkIndex, okNetwork := columns["network"]
	geonameIndex, okGeoname := columns["geoname_id"]
	if !okNetwork || !okGeoname {
		return nil, fmt.Errorf("%w：%s 缺少 network 或 geoname_id 列", ErrLocalDBUnavailable, path)
	}
	out := make([]v4Range, 0, 1024)
	for {
		record, err := rows.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w：%s 解析失败（%v）", ErrLocalDBUnavailable, path, err)
		}
		ip, network, parseErr := net.ParseCIDR(strings.TrimSpace(field(record, networkIndex)))
		if parseErr != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		ones, bits := network.Mask.Size()
		if bits != 32 {
			continue
		}
		start := binary.BigEndian.Uint32(ip4)
		mask := uint32(0)
		if ones > 0 {
			mask = ^uint32(0) << (32 - ones)
		}
		start &= mask
		out = append(out, v4Range{
			start: start,
			end:   start | ^mask,
			loc:   locations[strings.TrimSpace(field(record, geonameIndex))],
		})
	}
	return out, nil
}

// loadBlocksV6 读取 IPv6 Blocks 并展开为 128 位 start/end（hi/lo uint64）区间。
func loadBlocksV6(path string, locations map[string]location) ([]v6Range, error) {
	rows, closer, err := openCSV(path)
	if err != nil {
		return nil, fmt.Errorf("%w：打开 %s 失败（%v）", ErrLocalDBUnavailable, path, err)
	}
	defer closer()
	header, err := rows.Read()
	if err != nil {
		return nil, fmt.Errorf("%w：%s 表头读取失败（%v）", ErrLocalDBUnavailable, path, err)
	}
	columns := indexColumns(header)
	networkIndex, okNetwork := columns["network"]
	geonameIndex, okGeoname := columns["geoname_id"]
	if !okNetwork || !okGeoname {
		return nil, fmt.Errorf("%w：%s 缺少 network 或 geoname_id 列", ErrLocalDBUnavailable, path)
	}
	out := make([]v6Range, 0, 256)
	for {
		record, err := rows.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w：%s 解析失败（%v）", ErrLocalDBUnavailable, path, err)
		}
		ip, network, parseErr := net.ParseCIDR(strings.TrimSpace(field(record, networkIndex)))
		if parseErr != nil || ip.To4() != nil {
			continue
		}
		ones, bits := network.Mask.Size()
		if bits != 128 {
			continue
		}
		hi, lo := ipv6ToUint128(ip.To16())
		startHi, startLo, endHi, endLo := ipv6MaskRange(hi, lo, ones)
		out = append(out, v6Range{
			startHi: startHi, startLo: startLo, endHi: endHi, endLo: endLo,
			loc: locations[strings.TrimSpace(field(record, geonameIndex))],
		})
	}
	return out, nil
}

func openCSV(path string) (*csv.Reader, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	return reader, func() { _ = file.Close() }, nil
}

// indexColumns 建立列名 -> 下标映射，兼容 UTF-8 BOM 与大小写差异。
func indexColumns(header []string) map[string]int {
	out := make(map[string]int, len(header))
	for index, name := range header {
		clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
		if clean == "" {
			continue
		}
		if _, exists := out[clean]; !exists {
			out[clean] = index
		}
	}
	return out
}

func field(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return record[index]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ipv6ToUint128(ip net.IP) (uint64, uint64) {
	if len(ip) != net.IPv6len {
		return 0, 0
	}
	return binary.BigEndian.Uint64(ip[:8]), binary.BigEndian.Uint64(ip[8:])
}

// ipv6MaskRange 按前缀长度计算 start/end（128 位）。
func ipv6MaskRange(hi, lo uint64, ones int) (startHi, startLo, endHi, endLo uint64) {
	switch {
	case ones <= 0:
		return 0, 0, ^uint64(0), ^uint64(0)
	case ones < 64:
		mask := ^uint64(0) << (64 - ones)
		startHi, startLo = hi&mask, uint64(0)
	case ones == 64:
		startHi, startLo = hi, uint64(0)
	case ones < 128:
		mask := ^uint64(0) << (128 - ones)
		startHi, startLo = hi, lo&mask
	default:
		startHi, startLo = hi, lo
	}
	endHi, endLo = startHi, startLo
	if ones < 64 {
		endHi |= ^uint64(0) >> ones
		endLo = ^uint64(0)
	} else if ones < 128 {
		endLo |= ^uint64(0) >> (ones - 64)
	}
	return startHi, startLo, endHi, endLo
}

// compareUint128 比较两个 128 位无符号数。
func compareUint128(aHi, aLo, bHi, bLo uint64) int {
	switch {
	case aHi != bHi:
		if aHi < bHi {
			return -1
		}
		return 1
	case aLo != bLo:
		if aLo < bLo {
			return -1
		}
		return 1
	default:
		return 0
	}
}
