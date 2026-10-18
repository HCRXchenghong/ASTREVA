// 本文件实现一个零第三方依赖的最小 MMDB（MaxMind DB）只读读取器，
// 让 geoip_db_path 既能指向 GeoLite2 CSV 目录，也能指向 .mmdb 文件。
//
// 实现范围（"够用即可"，不覆盖全部 MMDB 特性）：
//   - 元数据：文件尾部 \xab\xcd\xefMaxMind.com 标记定位（取最后一次出现），
//     解析元数据 map，取 node_count / record_size / ip_version / database_type；
//   - 搜索树：24/28/32 位记录，按 IP 位逐位遍历；ip_version=6 的库里
//     IPv4 位于 0::/96 子树，前 96 位走 0 分支；
//   - 数据区：控制字节（类型 + size 位）解码 pointer(0x20/0x28/0x30)、
//     UTF-8 string、double、bytes、uint16/uint32/uint64、map、array、
//     boolean、float、end marker。
//
// 规范依据：MaxMind DB File Format Specification v2.0
// （https://maxmind.github.io/MaxMind-DB/）。
//
// 缓存与并发：文件只在首次使用时惰性读取并解析一次；加载成功与否都会缓存
// （含错误状态），由 sync.Mutex 保护。加载完成后 buf/meta 只读，可被多
// goroutine 并发查询。
package geo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"customer-service/backend/internal/domain"
)

var (
	// ErrMMDBInvalid 表示 .mmdb 文件缺失、被截断、元数据非法或指针越界。
	ErrMMDBInvalid = errors.New("mmdb 文件无效或无法解析")
)

const (
	// mmdbMetadataMarker 是元数据区起始标记。
	mmdbMetadataMarker = "\xab\xcd\xefMaxMind.com"
	// mmdbMetadataMaxSize 是元数据区（含标记）的大小上限：128KiB。
	mmdbMetadataMaxSize = 128 << 10
	// mmdbDataSeparatorSize 是搜索树与数据区之间的 16 字节 0 分隔符。
	mmdbDataSeparatorSize = 16
	// mmdbMaxPayloadSize 是单个字段 payload 的理论上限（规范给出的 16,843,036）。
	mmdbMaxPayloadSize = 65821 + 0xFFFFFF
	// mmdbMaxDecodeDepth 是单次解码的最大嵌套层数（规范建议 512）。
	mmdbMaxDecodeDepth = 512
	// mmdbMaxDecodeValues 是单次解码的最大值个数，避免指针展开爆量。
	mmdbMaxDecodeValues = 65536
)

// MMDB 数据类型编号（规范 Output Data Section）。
const (
	mmdbTypePointer            = 1
	mmdbTypeString             = 2
	mmdbTypeDouble             = 3
	mmdbTypeBytes              = 4
	mmdbTypeUint16             = 5
	mmdbTypeUint32             = 6
	mmdbTypeMap                = 7
	mmdbTypeInt32              = 8
	mmdbTypeUint64             = 9
	mmdbTypeUint128            = 10
	mmdbTypeArray              = 11
	mmdbTypeDataCacheContainer = 12
	mmdbTypeEndMarker          = 13
	mmdbTypeBoolean            = 14
	mmdbTypeFloat              = 15
)

// mmdbPreferredLocales 是本地化名称的取值优先级（与 CSV 路径的 zh-CN → en 一致）。
var mmdbPreferredLocales = []string{"zh-CN", "zh", "en"}

// MMDBMetadata 是 mmdb 文件中对本读取器有意义的元数据。
type MMDBMetadata struct {
	// NodeCount 是搜索树节点数。
	NodeCount uint32
	// RecordSize 是每条记录的位数（24/28/32）。
	RecordSize int
	// IPVersion 是库内的 IP 版本（4 或 6）。
	IPVersion int
	// DatabaseType 是库类型（如 GeoLite2-City）；缺失时留空。
	DatabaseType string
}

// mmdbMetadata 是内部元数据，附带由元数据推导出的各段边界。
type mmdbMetadata struct {
	nodeCount        uint32
	recordSize       int
	ipVersion        int
	databaseType     string
	searchTreeSize   int
	dataSectionStart int
	// dataEnd 是数据区可读上界（元数据标记起点）。
	dataEnd int
}

// MMDBReader 是 .mmdb 文件的只读句柄：惰性加载、进程内缓存、并发安全。
type MMDBReader struct {
	path string

	mu       sync.Mutex
	loaded   bool
	loadErr  error
	loadedAt time.Time

	buf  []byte
	meta mmdbMetadata
}

// NewMMDBReader 创建 mmdb 读取器句柄（不立即读取文件）。
func NewMMDBReader(path string) *MMDBReader {
	return &MMDBReader{path: strings.TrimSpace(path)}
}

// Path 返回配置的 mmdb 文件路径。
func (r *MMDBReader) Path() string { return r.path }

// isMMDBPath 判断配置路径是否指向 .mmdb 文件（大小写不敏感）。
func isMMDBPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".mmdb")
}

// LoadedAt 返回最近一次成功加载的时间（未加载时为零值）。
func (r *MMDBReader) LoadedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadedAt
}

// LoadError 返回加载/解析错误（未加载时为 nil）。
func (r *MMDBReader) LoadError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadErr
}

// EnsureLoaded 首次调用时读取并解析 mmdb；失败只记录状态，不重试、不 panic。
func (r *MMDBReader) EnsureLoaded() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return r.loadErr
	}
	r.loaded = true
	buf, meta, err := loadMMDBFile(r.path)
	if err != nil {
		r.loadErr = err
		return err
	}
	r.buf = buf
	r.meta = meta
	r.loadedAt = time.Now().UTC()
	return nil
}

// Metadata 返回解析后的元数据；加载失败时返回错误。
func (r *MMDBReader) Metadata() (MMDBMetadata, error) {
	if err := r.EnsureLoaded(); err != nil {
		return MMDBMetadata{}, err
	}
	return MMDBMetadata{
		NodeCount:    r.meta.nodeCount,
		RecordSize:   r.meta.recordSize,
		IPVersion:    r.meta.ipVersion,
		DatabaseType: r.meta.databaseType,
	}, nil
}

// Lookup 查询 IP 对应的地理记录；未命中或文件不可用时返回 false（不暴露错误，
// 供三级解析器静默降级使用）。
func (r *MMDBReader) Lookup(ip string) (domain.GeoRecord, bool) {
	record, found, _ := r.LookupIP(ip)
	return record, found
}

// LookupIP 查询 IP 对应的地理记录，并把解析错误一并返回（便于测试与排障）。
// 未覆盖的 IP 返回 found=false 且 err=nil；文件损坏/指针越界返回 err。
func (r *MMDBReader) LookupIP(ip string) (domain.GeoRecord, bool, error) {
	if err := r.EnsureLoaded(); err != nil {
		return domain.GeoRecord{}, false, err
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return domain.GeoRecord{}, false, nil
	}
	// 加载成功后 buf/meta 只读（写入发生在 EnsureLoaded 的互斥区内），可并发读取。
	offset, found, err := r.findDataOffset(parsed)
	if err != nil || !found {
		return domain.GeoRecord{}, false, err
	}
	decoder := mmdbDecoder{buf: r.buf}
	value, _, err := decoder.decode(offset, r.meta.dataSectionStart, r.meta.dataEnd, 0)
	if err != nil {
		return domain.GeoRecord{}, false, fmt.Errorf("%w：数据记录解码失败（%v）", ErrMMDBInvalid, err)
	}
	record, ok := value.(map[string]any)
	if !ok {
		return domain.GeoRecord{}, false, fmt.Errorf("%w：数据记录不是 map", ErrMMDBInvalid)
	}
	geo := mmdbRecordToGeo(record)
	if geo.IsEmpty() {
		// 记录里没有本任务需要的字段（例如误配了 GeoLite2-ASN 这类库，
		// 或只有非地理字段）：视为未命中，让三级解析继续回落到第三方库。
		return domain.GeoRecord{}, false, nil
	}
	return geo, true, nil
}

// findDataOffset 从搜索树根节点逐位遍历，返回数据区内的记录起点。
func (r *MMDBReader) findDataOffset(ip net.IP) (int, bool, error) {
	bits := r.lookupBits(ip)
	if bits == nil {
		// 例如 IPv4 库查询 IPv6 地址：直接视为未覆盖。
		return 0, false, nil
	}
	node := uint32(0)
	for _, bit := range bits {
		value, err := r.readRecord(node, bit == 1)
		if err != nil {
			return 0, false, err
		}
		switch {
		case value < r.meta.nodeCount:
			node = value
		case value == r.meta.nodeCount:
			// 等于节点数：该网段没有数据。
			return 0, false, nil
		default:
			// 大于节点数：数据区指针。规范：$offset_in_file = ($record_value - $node_count) + $search_tree_size。
			offset := int(value-r.meta.nodeCount) + r.meta.searchTreeSize
			if offset < r.meta.dataSectionStart || offset >= r.meta.dataEnd {
				return 0, false, fmt.Errorf("%w：数据指针 %d 越界", ErrMMDBInvalid, value)
			}
			return offset, true, nil
		}
	}
	// 走完全部位仍未落到数据节点：未覆盖。
	return 0, false, nil
}

// lookupBits 把 IP 展开成搜索树上的位序列。
// ip_version=6 的库里 IPv4 位于 0::/96 子树，前面补 96 个 0 位。
func (r *MMDBReader) lookupBits(ip net.IP) []byte {
	if r.meta.ipVersion == 6 {
		if v4 := ip.To4(); v4 != nil {
			bits := make([]byte, 0, 128)
			bits = append(bits, make([]byte, 96)...)
			return append(bits, mmdbIPBits(v4)...)
		}
		if v16 := ip.To16(); v16 != nil {
			return mmdbIPBits(v16)
		}
		return nil
	}
	// IPv4 库只覆盖 IPv4 地址。
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	return mmdbIPBits(v4)
}

// readRecord 读取节点的一条记录（right=false 取左记录，true 取右记录）。
func (r *MMDBReader) readRecord(node uint32, right bool) (uint32, error) {
	nodeBytes := r.meta.recordSize * 2 / 8
	offset := int(node) * nodeBytes
	if offset < 0 || offset+nodeBytes > r.meta.searchTreeSize || offset+nodeBytes > len(r.buf) {
		return 0, fmt.Errorf("%w：节点 %d 越界", ErrMMDBInvalid, node)
	}
	body := r.buf[offset : offset+nodeBytes]
	switch r.meta.recordSize {
	case 24:
		base := 0
		if right {
			base = 3
		}
		return uint32(body[base])<<16 | uint32(body[base+1])<<8 | uint32(body[base+2]), nil
	case 28:
		// | 23..0 | 27..24 | 27..24 | 23..0 |：中间字节高 4 位是左记录的 27..24，
		// 低 4 位是右记录的 27..24；两条记录的 27..24 都是各自 28 位值的最高 4 位
		// （规范原文：4 位被"前置"，位于最高位），因此左记录 = b3高半字节<<24 | b0b1b2。
		if !right {
			return uint32(body[3]>>4)<<24 | uint32(body[0])<<16 | uint32(body[1])<<8 | uint32(body[2]), nil
		}
		return uint32(body[3]&0x0F)<<24 | uint32(body[4])<<16 | uint32(body[5])<<8 | uint32(body[6]), nil
	default: // 32 位
		base := 0
		if right {
			base = 4
		}
		return binary.BigEndian.Uint32(body[base : base+4]), nil
	}
}

// mmdbIPBits 把 IP 字节展开成 0/1 位序列。
func mmdbIPBits(ip []byte) []byte {
	bits := make([]byte, 0, len(ip)*8)
	for _, value := range ip {
		for index := 7; index >= 0; index-- {
			bits = append(bits, (value>>uint(index))&1)
		}
	}
	return bits
}

// loadMMDBFile 读取文件并解析出元数据。
func loadMMDBFile(path string) ([]byte, mmdbMetadata, error) {
	var empty mmdbMetadata
	if path == "" {
		return nil, empty, fmt.Errorf("%w：未配置 mmdb 路径", ErrMMDBInvalid)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, empty, fmt.Errorf("%w：文件 %s 不可访问（%v）", ErrMMDBInvalid, path, err)
	}
	if info.IsDir() {
		return nil, empty, fmt.Errorf("%w：%s 是目录，不是 .mmdb 文件", ErrMMDBInvalid, path)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, empty, fmt.Errorf("%w：读取 %s 失败（%v）", ErrMMDBInvalid, path, err)
	}
	meta, err := parseMMDB(buf)
	if err != nil {
		return nil, empty, err
	}
	return buf, meta, nil
}

// parseMMDB 定位元数据标记、解析元数据 map 并校验各段边界。
func parseMMDB(buf []byte) (mmdbMetadata, error) {
	var meta mmdbMetadata
	marker := []byte(mmdbMetadataMarker)
	// 数据区可能包含任意二进制，必须取标记的最后一次出现。
	index := bytes.LastIndex(buf, marker)
	if index < 0 {
		return meta, fmt.Errorf("%w：缺少 MaxMind 元数据标记", ErrMMDBInvalid)
	}
	if len(buf)-index > mmdbMetadataMaxSize {
		return meta, fmt.Errorf("%w：元数据区超过 128KiB 上限", ErrMMDBInvalid)
	}
	metadataStart := index + len(marker)
	// 元数据里的指针相对元数据区起点。
	decoder := mmdbDecoder{buf: buf}
	value, _, err := decoder.decode(metadataStart, metadataStart, len(buf), 0)
	if err != nil {
		return meta, fmt.Errorf("%w：元数据解码失败（%v）", ErrMMDBInvalid, err)
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return meta, fmt.Errorf("%w：元数据不是 map", ErrMMDBInvalid)
	}

	nodeCount, ok := mmdbUintValue(fields["node_count"])
	if !ok || nodeCount == 0 || nodeCount > uint64(^uint32(0)) {
		return meta, fmt.Errorf("%w：node_count 非法（%v）", ErrMMDBInvalid, fields["node_count"])
	}
	recordSizeValue, ok := mmdbUintValue(fields["record_size"])
	if !ok {
		return meta, fmt.Errorf("%w：record_size 缺失或非法（%v）", ErrMMDBInvalid, fields["record_size"])
	}
	recordSize := int(recordSizeValue)
	if recordSize != 24 && recordSize != 28 && recordSize != 32 {
		return meta, fmt.Errorf("%w：不支持的 record_size %d（仅支持 24/28/32）", ErrMMDBInvalid, recordSize)
	}
	ipVersionValue, ok := mmdbUintValue(fields["ip_version"])
	if !ok || (ipVersionValue != 4 && ipVersionValue != 6) {
		return meta, fmt.Errorf("%w：ip_version 非法（%v）", ErrMMDBInvalid, fields["ip_version"])
	}
	databaseType, _ := fields["database_type"].(string)

	nodeBytes := recordSize * 2 / 8
	searchTreeSize := uint64(nodeBytes) * nodeCount
	// 搜索树 + 16 字节分隔符必须落在元数据标记之前。
	if searchTreeSize+mmdbDataSeparatorSize > uint64(index) {
		return meta, fmt.Errorf("%w：搜索树大小 %d 超出文件范围", ErrMMDBInvalid, searchTreeSize)
	}
	meta = mmdbMetadata{
		nodeCount:        uint32(nodeCount),
		recordSize:       recordSize,
		ipVersion:        int(ipVersionValue),
		databaseType:     strings.TrimSpace(databaseType),
		searchTreeSize:   int(searchTreeSize),
		dataSectionStart: int(searchTreeSize) + mmdbDataSeparatorSize,
		dataEnd:          index,
	}
	return meta, nil
}

// mmdbUintValue 把解码出的整数转成 uint64（int32 为规范里的有符号类型）。
func mmdbUintValue(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	case float64:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	default:
		return 0, false
	}
}

// mmdbRecordToGeo 从数据记录的 map 中取值，缺失字段留空。
// 复用 location.record，保证与 CSV 来源的 GeoRecord 形状一致（含 Basis=geoip）。
func mmdbRecordToGeo(record map[string]any) domain.GeoRecord {
	country := mmdbChildMap(record, "country")
	loc := location{
		country:     mmdbLocalizedName(country, "names"),
		countryCode: mmdbStringValue(country["iso_code"]),
		region:      mmdbSubdivisionName(record),
		city:        mmdbLocalizedName(mmdbChildMap(record, "city"), "names"),
		timezone:    mmdbStringValue(mmdbChildMap(record, "location")["time_zone"]),
	}
	return loc.record(domain.GeoBasisLocal)
}

// mmdbSubdivisionName 取 subdivisions[0] 的可读名，缺失时回落 iso_code。
func mmdbSubdivisionName(record map[string]any) string {
	list, _ := record["subdivisions"].([]any)
	if len(list) == 0 {
		return ""
	}
	first, _ := list[0].(map[string]any)
	if name := mmdbLocalizedName(first, "names"); name != "" {
		return name
	}
	return mmdbStringValue(first["iso_code"])
}

// mmdbChildMap 取出子 map（类型不符或缺失时返回 nil）。
func mmdbChildMap(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	child, _ := parent[key].(map[string]any)
	return child
}

// mmdbStringValue 取出字符串值（类型不符时留空）。
func mmdbStringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

// mmdbLocalizedName 从 names 子 map 按 zh-CN → zh → en 的优先级取可读名。
func mmdbLocalizedName(container map[string]any, key string) string {
	if container == nil {
		return ""
	}
	names, _ := container[key].(map[string]any)
	for _, locale := range mmdbPreferredLocales {
		if name := mmdbStringValue(names[locale]); name != "" {
			return name
		}
	}
	return ""
}

// mmdbDecoder 是数据区/元数据区的解码器。
// pointerBase 是 pointer 类型所指偏移的基准：数据区为数据区起点，元数据区为元数据区起点。
type mmdbDecoder struct {
	buf    []byte
	values int
}

// decode 从 offset 解码一个值，返回解码结果与该值之后的偏移。
func (d *mmdbDecoder) decode(offset, pointerBase, limit, depth int) (any, int, error) {
	if depth > mmdbMaxDecodeDepth {
		return nil, offset, fmt.Errorf("%w：嵌套超过 %d 层", ErrMMDBInvalid, mmdbMaxDecodeDepth)
	}
	d.values++
	if d.values > mmdbMaxDecodeValues {
		return nil, offset, fmt.Errorf("%w：单条记录解码值超过 %d 个", ErrMMDBInvalid, mmdbMaxDecodeValues)
	}
	if offset < 0 || offset >= limit || offset >= len(d.buf) {
		return nil, offset, fmt.Errorf("%w：偏移 %d 越界", ErrMMDBInvalid, offset)
	}

	control := d.buf[offset]
	offset++
	valueType := int(control >> 5)
	if valueType == 0 {
		// 扩展类型：下一个字节存放实际类型（类型号 - 7）。
		if offset >= limit {
			return nil, offset, fmt.Errorf("%w：扩展类型字节越界", ErrMMDBInvalid)
		}
		valueType = int(d.buf[offset]) + 7
		offset++
	}

	if valueType == mmdbTypePointer {
		pointer, next, err := d.decodePointer(control, offset, limit)
		if err != nil {
			return nil, offset, err
		}
		target := pointerBase + int(pointer)
		if target < 0 || target >= limit {
			return nil, next, fmt.Errorf("%w：数据指针 %d 越界", ErrMMDBInvalid, pointer)
		}
		// 规范禁止"指针指向指针"；即使文件违规，值计数与深度上限也能兜底终止。
		value, _, err := d.decode(target, pointerBase, limit, depth+1)
		return value, next, err
	}

	size, offset, err := d.readSize(control, offset, limit)
	if err != nil {
		return nil, offset, err
	}

	switch valueType {
	case mmdbTypeString:
		payload, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		if !utf8.Valid(payload) {
			return nil, next, fmt.Errorf("%w：字符串不是合法 UTF-8", ErrMMDBInvalid)
		}
		return string(payload), next, nil

	case mmdbTypeBytes:
		payload, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		return append([]byte(nil), payload...), next, nil

	case mmdbTypeDouble:
		payload, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		if len(payload) != 8 {
			return nil, next, fmt.Errorf("%w：double 长度 %d 非法", ErrMMDBInvalid, len(payload))
		}
		return math.Float64frombits(binary.BigEndian.Uint64(payload)), next, nil

	case mmdbTypeFloat:
		payload, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		if len(payload) != 4 {
			return nil, next, fmt.Errorf("%w：float 长度 %d 非法", ErrMMDBInvalid, len(payload))
		}
		return math.Float32frombits(binary.BigEndian.Uint32(payload)), next, nil

	case mmdbTypeUint16, mmdbTypeUint32, mmdbTypeUint64:
		payload, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		if len(payload) > 8 {
			return nil, next, fmt.Errorf("%w：无符号整数长度 %d 非法", ErrMMDBInvalid, len(payload))
		}
		return mmdbBytesToUint(payload), next, nil

	case mmdbTypeInt32:
		payload, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		if len(payload) > 4 {
			return nil, next, fmt.Errorf("%w：int32 长度 %d 非法", ErrMMDBInvalid, len(payload))
		}
		return mmdbBytesToInt32(payload), next, nil

	case mmdbTypeUint128:
		// 128 位整数（通常用于存 IPv6 地址）不参与本任务的取值（只需
		// node_count/record_size/数据库类型与地理字段），直接跳过 payload，
		// 避免为此引入 math/big。解码结果为 nil。
		_, next, err := d.readPayload(offset, size, limit)
		if err != nil {
			return nil, next, err
		}
		return nil, next, nil

	case mmdbTypeMap:
		if size > mmdbMaxDecodeValues {
			return nil, offset, fmt.Errorf("%w：map 条目数 %d 过大", ErrMMDBInvalid, size)
		}
		out := make(map[string]any, size)
		for index := uint64(0); index < size; index++ {
			keyValue, next, err := d.decode(offset, pointerBase, limit, depth+1)
			if err != nil {
				return nil, next, err
			}
			key, ok := keyValue.(string)
			if !ok {
				return nil, next, fmt.Errorf("%w：map 键不是字符串", ErrMMDBInvalid)
			}
			value, next, err := d.decode(next, pointerBase, limit, depth+1)
			if err != nil {
				return nil, next, err
			}
			offset = next
			out[key] = value
		}
		return out, offset, nil

	case mmdbTypeArray:
		if size > mmdbMaxDecodeValues {
			return nil, offset, fmt.Errorf("%w：array 条目数 %d 过大", ErrMMDBInvalid, size)
		}
		out := make([]any, 0, size)
		for index := uint64(0); index < size; index++ {
			value, next, err := d.decode(offset, pointerBase, limit, depth+1)
			if err != nil {
				return nil, next, err
			}
			out = append(out, value)
			offset = next
		}
		return out, offset, nil

	case mmdbTypeBoolean:
		// boolean 的值直接放在 size 位里，没有 payload。
		if size > 1 {
			return nil, offset, fmt.Errorf("%w：boolean 长度 %d 非法", ErrMMDBInvalid, size)
		}
		return size == 1, offset, nil

	case mmdbTypeEndMarker:
		// 已废弃的结束标记：无 payload，遇到即为空值。
		return nil, offset, nil

	case mmdbTypeDataCacheContainer:
		return nil, offset, fmt.Errorf("%w：不支持的数据类型 data cache container", ErrMMDBInvalid)

	default:
		return nil, offset, fmt.Errorf("%w：未知数据类型 %d", ErrMMDBInvalid, valueType)
	}
}

// decodePointer 解析 pointer 类型的值。控制字节为 001SSVVV。
func (d *mmdbDecoder) decodePointer(control byte, offset, limit int) (uint64, int, error) {
	size := (control >> 3) & 0x3
	switch size {
	case 0:
		if offset+1 > limit {
			return 0, offset, fmt.Errorf("%w：指针字节越界", ErrMMDBInvalid)
		}
		return uint64(control&0x7)<<8 | uint64(d.buf[offset]), offset + 1, nil
	case 1:
		if offset+2 > limit {
			return 0, offset, fmt.Errorf("%w：指针字节越界", ErrMMDBInvalid)
		}
		value := uint64(control&0x7)<<16 | uint64(d.buf[offset])<<8 | uint64(d.buf[offset+1])
		return value + 2048, offset + 2, nil
	case 2:
		if offset+3 > limit {
			return 0, offset, fmt.Errorf("%w：指针字节越界", ErrMMDBInvalid)
		}
		value := uint64(control&0x7)<<24 | uint64(d.buf[offset])<<16 |
			uint64(d.buf[offset+1])<<8 | uint64(d.buf[offset+2])
		return value + 526336, offset + 3, nil
	default:
		if offset+4 > limit {
			return 0, offset, fmt.Errorf("%w：指针字节越界", ErrMMDBInvalid)
		}
		return uint64(binary.BigEndian.Uint32(d.buf[offset : offset+4])), offset + 4, nil
	}
}

// readSize 解析控制字节里的 payload 长度（29/30/31 为扩展长度）。
func (d *mmdbDecoder) readSize(control byte, offset, limit int) (uint64, int, error) {
	size := uint64(control & 0x1F)
	switch size {
	case 29:
		if offset+1 > limit {
			return 0, offset, fmt.Errorf("%w：长度字节越界", ErrMMDBInvalid)
		}
		return 29 + uint64(d.buf[offset]), offset + 1, nil
	case 30:
		if offset+2 > limit {
			return 0, offset, fmt.Errorf("%w：长度字节越界", ErrMMDBInvalid)
		}
		return 285 + uint64(binary.BigEndian.Uint16(d.buf[offset:offset+2])), offset + 2, nil
	case 31:
		if offset+3 > limit {
			return 0, offset, fmt.Errorf("%w：长度字节越界", ErrMMDBInvalid)
		}
		value := uint64(d.buf[offset])<<16 | uint64(d.buf[offset+1])<<8 | uint64(d.buf[offset+2])
		return 65821 + value, offset + 3, nil
	default:
		return size, offset, nil
	}
}

// readPayload 返回 [offset, offset+size) 的 payload 并做越界检查。
func (d *mmdbDecoder) readPayload(offset int, size uint64, limit int) ([]byte, int, error) {
	if size > mmdbMaxPayloadSize {
		return nil, offset, fmt.Errorf("%w：payload 长度 %d 过大", ErrMMDBInvalid, size)
	}
	end := offset + int(size)
	if end < offset || end > limit || end > len(d.buf) {
		return nil, offset, fmt.Errorf("%w：payload 越界（%d..%d）", ErrMMDBInvalid, offset, end)
	}
	return d.buf[offset:end], end, nil
}

// mmdbBytesToUint 按大端把 0-8 字节转成无符号整数（0 字节表示 0）。
func mmdbBytesToUint(payload []byte) uint64 {
	var value uint64
	for _, item := range payload {
		value = value<<8 | uint64(item)
	}
	return value
}

// mmdbBytesToInt32 按大端解析有符号 32 位整数（不足 4 字节时恒为正）。
func mmdbBytesToInt32(payload []byte) int64 {
	var value int64
	for _, item := range payload {
		value = value<<8 | int64(item)
	}
	if len(payload) == 4 && payload[0]&0x80 != 0 {
		value -= 1 << 32
	}
	return value
}
