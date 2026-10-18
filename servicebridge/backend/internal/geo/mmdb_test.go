package geo

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// ===== 最小 MMDB 构造器（仅测试用）=====
//
// 因为环境拿不到真实 .mmdb 文件，这里按 MaxMind DB 规范 v2.0 用 Go 直接拼出
// 合法文件：搜索树 + 16 字节分隔符 + 数据区 + 元数据标记 + 元数据 map。
// 只支持互不重叠的前缀（本测试的数据满足），足以覆盖读取器需要的全部结构。

// mmdbTestRecord 是一条测试用的地理记录。
type mmdbTestRecord struct {
	countryCode string
	countryZh   string
	countryEn   string
	regionCode  string
	regionZh    string
	regionEn    string
	cityZh      string
	cityEn      string
	timezone    string
}

// mmdbTestEntry 是"网段 → 记录"的一条映射。
type mmdbTestEntry struct {
	network string
	record  mmdbTestRecord
}

// mmdbTestPair 是 map 的一个键值对（值已编码成字节）。
type mmdbTestPair struct {
	key   string
	value []byte
}

// mmdbTestFile 是构造出来的文件及其关键边界，便于断言与人为损坏。
type mmdbTestFile struct {
	bytes      []byte
	nodeCount  int
	recordSize int
	treeSize   int
	dataStart  int
	dataEnd    int
}

// testMMDBEntries 返回固定测试数据：2 个 IPv4 网段 + 1 个 IPv6 网段。
func testMMDBEntries() []mmdbTestEntry {
	riyadh := mmdbTestRecord{
		countryCode: "SA", countryZh: "沙特阿拉伯", countryEn: "Saudi Arabia",
		regionCode: "01", regionZh: "利雅得省", regionEn: "Riyadh",
		cityZh: "利雅得", cityEn: "Riyadh", timezone: "Asia/Riyadh",
	}
	mountainView := mmdbTestRecord{
		countryCode: "US", countryZh: "美国", countryEn: "United States",
		regionCode: "CA", regionZh: "加利福尼亚州", regionEn: "California",
		cityZh: "山景城", cityEn: "Mountain View", timezone: "America/Los_Angeles",
	}
	return []mmdbTestEntry{
		{network: "1.2.3.0/24", record: riyadh},
		{network: "8.8.8.0/24", record: mountainView},
		{network: "2001:db8::/32", record: riyadh},
	}
}

// mmdbTestNode 是构造过程中的搜索树节点。
type mmdbTestNode struct {
	child     [2]*mmdbTestNode
	dataIndex int // -1：无数据；>=0：指向该节点的记录应指向数据区第 N 条记录
}

func newMMDBTestNode() *mmdbTestNode { return &mmdbTestNode{dataIndex: -1} }

// insert 插入一个前缀；只支持互不重叠的前缀。
func (n *mmdbTestNode) insert(t *testing.T, bits []byte, dataIndex int) {
	t.Helper()
	node := n
	for _, bit := range bits {
		if node.dataIndex >= 0 {
			t.Fatal("测试构造器不支持前缀重叠")
		}
		next := node.child[bit]
		if next == nil {
			next = newMMDBTestNode()
			node.child[bit] = next
		}
		node = next
	}
	if node.dataIndex >= 0 || node.child[0] != nil || node.child[1] != nil {
		t.Fatal("测试构造器不支持前缀重叠")
	}
	node.dataIndex = dataIndex
}

// buildTestMMDB 构造一个合法的 mmdb 文件（ip_version=6）。
// recordSize 可取 24/28/32；IPv4 网段写入 0::/96 子树（与真实库一致）。
func buildTestMMDB(t *testing.T, recordSize int, entries []mmdbTestEntry) mmdbTestFile {
	t.Helper()
	return buildTestMMDBVersion(t, recordSize, 6, entries)
}

// buildTestMMDBVersion 构造指定 ip_version 的 mmdb 文件。
func buildTestMMDBVersion(t *testing.T, recordSize, ipVersion int, entries []mmdbTestEntry) mmdbTestFile {
	t.Helper()
	root := newMMDBTestNode()
	for index, entry := range entries {
		_, network, err := net.ParseCIDR(entry.network)
		if err != nil {
			t.Fatalf("测试网段 %q 非法：%v", entry.network, err)
		}
		root.insert(t, mmdbTestBits(t, network, ipVersion), index)
	}

	// 深度优先编号（根节点为 0）。
	numbers := map[*mmdbTestNode]int{}
	var order []*mmdbTestNode
	var visit func(*mmdbTestNode)
	visit = func(node *mmdbTestNode) {
		numbers[node] = len(order)
		order = append(order, node)
		for _, child := range node.child {
			if child != nil {
				visit(child)
			}
		}
	}
	visit(root)
	nodeCount := len(order)

	// 数据区：每条记录编码成一个 map。
	data := make([]byte, 0, 512)
	dataOffsets := make([]int, len(entries))
	for index, entry := range entries {
		dataOffsets[index] = len(data)
		data = append(data, mmdbTestEncodeRecord(entry.record)...)
	}

	// 搜索树：node_count × (record_size*2/8) 字节。
	nodeBytes := recordSize * 2 / 8
	tree := make([]byte, nodeCount*nodeBytes)
	for _, node := range order {
		offset := numbers[node] * nodeBytes
		for direction := 0; direction < 2; direction++ {
			child := node.child[direction]
			var value uint32
			switch {
			case child == nil:
				// 等于节点数：该网段没有数据。
				value = uint32(nodeCount)
			case child.dataIndex >= 0:
				// 大于节点数：数据指针（record = node_count + 16 + 数据区偏移）。
				value = uint32(nodeCount + mmdbDataSeparatorSize + dataOffsets[child.dataIndex])
			default:
				value = uint32(numbers[child])
			}
			mmdbTestWriteRecord(tree, offset, recordSize, direction == 1, value)
		}
	}

	out := make([]byte, 0, len(tree)+mmdbDataSeparatorSize+len(data)+256)
	out = append(out, tree...)
	out = append(out, make([]byte, mmdbDataSeparatorSize)...)
	out = append(out, data...)
	out = append(out, mmdbMetadataMarker...)
	out = append(out, mmdbTestEncodeMetadata(nodeCount, recordSize, ipVersion)...)
	return mmdbTestFile{
		bytes:      out,
		nodeCount:  nodeCount,
		recordSize: recordSize,
		treeSize:   len(tree),
		dataStart:  len(tree) + mmdbDataSeparatorSize,
		dataEnd:    len(tree) + mmdbDataSeparatorSize + len(data),
	}
}

// mmdbTestBits 返回网段在搜索树中的位序列。
// ip_version=6 时 IPv4 前补 96 个 0 位（0::/96 子树）；ip_version=4 时不支持 IPv6 网段。
func mmdbTestBits(t *testing.T, network *net.IPNet, ipVersion int) []byte {
	t.Helper()
	ones, bits := network.Mask.Size()
	if bits == 32 {
		if ipVersion == 4 {
			return mmdbIPBits(network.IP.To4())[:ones]
		}
		prefix := make([]byte, 0, 128)
		prefix = append(prefix, make([]byte, 96)...)
		prefix = append(prefix, mmdbIPBits(network.IP.To4())...)
		return prefix[:96+ones]
	}
	if ipVersion == 4 {
		t.Fatalf("ip_version=4 的库不支持 IPv6 网段 %s", network)
	}
	return mmdbIPBits(network.IP.To16())[:ones]
}

// mmdbTestEncodeRecord 把一条地理记录编码成 MMDB map。
func mmdbTestEncodeRecord(record mmdbTestRecord) []byte {
	names := func(zh, en string) []byte {
		return mmdbTestMap(
			mmdbTestPair{"en", mmdbTestString(en)},
			mmdbTestPair{"zh-CN", mmdbTestString(zh)},
		)
	}
	return mmdbTestMap(
		mmdbTestPair{"city", mmdbTestMap(
			mmdbTestPair{"names", names(record.cityZh, record.cityEn)},
		)},
		mmdbTestPair{"country", mmdbTestMap(
			mmdbTestPair{"iso_code", mmdbTestString(record.countryCode)},
			mmdbTestPair{"names", names(record.countryZh, record.countryEn)},
		)},
		mmdbTestPair{"location", mmdbTestMap(
			mmdbTestPair{"time_zone", mmdbTestString(record.timezone)},
		)},
		mmdbTestPair{"subdivisions", mmdbTestArray(
			mmdbTestMap(
				mmdbTestPair{"iso_code", mmdbTestString(record.regionCode)},
				mmdbTestPair{"names", names(record.regionZh, record.regionEn)},
			),
		)},
	)
}

// mmdbTestEncodeMetadata 编码元数据 map。
func mmdbTestEncodeMetadata(nodeCount, recordSize, ipVersion int) []byte {
	return mmdbTestMap(
		mmdbTestPair{"binary_format_major_version", mmdbTestUint16(2)},
		mmdbTestPair{"binary_format_minor_version", mmdbTestUint16(0)},
		mmdbTestPair{"build_epoch", mmdbTestUint64(1700000000)},
		mmdbTestPair{"database_type", mmdbTestString("GeoLite2-City")},
		mmdbTestPair{"ip_version", mmdbTestUint16(uint16(ipVersion))},
		mmdbTestPair{"node_count", mmdbTestUint32(uint32(nodeCount))},
		mmdbTestPair{"record_size", mmdbTestUint16(uint16(recordSize))},
	)
}

// mmdbTestControl 生成控制字节（含扩展类型与长度扩展字节）。
func mmdbTestControl(valueType, size int) []byte {
	control := byte(0)
	var extended []byte
	if valueType < 8 {
		control = byte(valueType << 5)
	} else {
		extended = []byte{byte(valueType - 7)}
	}
	switch {
	case size < 29:
		control |= byte(size)
		return append([]byte{control}, extended...)
	case size < 29+256:
		control |= 29
		return append(append([]byte{control}, extended...), byte(size-29))
	case size < 285+65536:
		control |= 30
		value := size - 285
		return append(append([]byte{control}, extended...), byte(value>>8), byte(value))
	default:
		control |= 31
		value := size - 65821
		return append(append([]byte{control}, extended...), byte(value>>16), byte(value>>8), byte(value))
	}
}

func mmdbTestString(value string) []byte {
	return append(mmdbTestControl(mmdbTypeString, len(value)), value...)
}

func mmdbTestMap(pairs ...mmdbTestPair) []byte {
	out := mmdbTestControl(mmdbTypeMap, len(pairs))
	for _, pair := range pairs {
		out = append(out, mmdbTestString(pair.key)...)
		out = append(out, pair.value...)
	}
	return out
}

func mmdbTestArray(items ...[]byte) []byte {
	out := mmdbTestControl(mmdbTypeArray, len(items))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

func mmdbTestUint16(value uint16) []byte {
	payload := []byte{byte(value >> 8), byte(value)}
	return append(mmdbTestControl(mmdbTypeUint16, len(payload)), payload...)
}

func mmdbTestUint32(value uint32) []byte {
	payload := []byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
	return append(mmdbTestControl(mmdbTypeUint32, len(payload)), payload...)
}

func mmdbTestUint64(value uint64) []byte {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, value)
	return append(mmdbTestControl(mmdbTypeUint64, len(payload)), payload...)
}

func mmdbTestDouble(value float64) []byte {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, math.Float64bits(value))
	return append(mmdbTestControl(mmdbTypeDouble, len(payload)), payload...)
}

func mmdbTestBoolean(value bool) []byte {
	size := 0
	if value {
		size = 1
	}
	return mmdbTestControl(mmdbTypeBoolean, size)
}

// mmdbTestPointer 生成 size=0（11 位）的 pointer 字段，指向数据区内的偏移。
func mmdbTestPointer(offset int) []byte {
	return []byte{byte(mmdbTypePointer<<5) | byte((offset>>8)&0x07), byte(offset)}
}

// mmdbTestWriteRecord 按规范写入一条搜索树记录。
// 与读取器实现相互独立，用作交叉校验。
func mmdbTestWriteRecord(buf []byte, offset, recordSize int, right bool, value uint32) {
	switch recordSize {
	case 24:
		base := offset
		if right {
			base += 3
		}
		buf[base] = byte(value >> 16)
		buf[base+1] = byte(value >> 8)
		buf[base+2] = byte(value)
	case 28:
		if !right {
			// 低 24 位放在前 3 字节，最高 4 位放在中间字节的高半字节。
			buf[offset] = byte(value >> 16)
			buf[offset+1] = byte(value >> 8)
			buf[offset+2] = byte(value)
			buf[offset+3] = byte(value>>24)<<4 | (buf[offset+3] & 0x0F)
			return
		}
		buf[offset+3] = (buf[offset+3] & 0xF0) | (byte(value>>24) & 0x0F)
		buf[offset+4] = byte(value >> 16)
		buf[offset+5] = byte(value >> 8)
		buf[offset+6] = byte(value)
	default: // 32 位
		base := offset
		if right {
			base += 4
		}
		binary.BigEndian.PutUint32(buf[base:base+4], value)
	}
}

// mmdbTestReadRecord 按规范读取一条搜索树记录（与 mmdb.go 中的实现独立）。
func mmdbTestReadRecord(buf []byte, offset, recordSize int, right bool) uint32 {
	switch recordSize {
	case 24:
		base := offset
		if right {
			base += 3
		}
		return uint32(buf[base])<<16 | uint32(buf[base+1])<<8 | uint32(buf[base+2])
	case 28:
		if !right {
			return uint32(buf[offset+3]>>4)<<24 | uint32(buf[offset])<<16 |
				uint32(buf[offset+1])<<8 | uint32(buf[offset+2])
		}
		return uint32(buf[offset+3]&0x0F)<<24 | uint32(buf[offset+4])<<16 |
			uint32(buf[offset+5])<<8 | uint32(buf[offset+6])
	default:
		base := offset
		if right {
			base += 4
		}
		return binary.BigEndian.Uint32(buf[base : base+4])
	}
}

// writeTestMMDB 把构造出的 mmdb 字节写入临时目录并返回文件路径。
func writeTestMMDB(t *testing.T, recordSize int, entries []mmdbTestEntry) string {
	t.Helper()
	file := buildTestMMDB(t, recordSize, entries)
	return writeMMDBBytes(t, file.bytes, "GeoLite2-City.mmdb")
}

// writeMMDBBytes 把任意字节写入临时目录下的 .mmdb 文件。
func writeMMDBBytes(t *testing.T, content []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("写入 mmdb 测试文件失败：%v", err)
	}
	return path
}

// assertGeoRecord 断言地理记录的可读字段与 Basis。
func assertGeoRecord(t *testing.T, record domain.GeoRecord, want mmdbTestRecord) {
	t.Helper()
	if record.Country != want.countryZh || record.CountryCode != want.countryCode {
		t.Fatalf("国家 = %q/%q，want %q/%q", record.Country, record.CountryCode, want.countryZh, want.countryCode)
	}
	if record.Region != want.regionZh || record.City != want.cityZh || record.Timezone != want.timezone {
		t.Fatalf("地区/城市/时区 = %q/%q/%q，want %q/%q/%q",
			record.Region, record.City, record.Timezone, want.regionZh, want.cityZh, want.timezone)
	}
	if record.Basis != domain.GeoBasisLocal {
		t.Fatalf("Basis = %q，want %q", record.Basis, domain.GeoBasisLocal)
	}
}

// TestMMDBRecordLayout 用规范里的字节布局手算校验 24/28 位记录的拆分，
// 并确认读取器的 readRecord 得到同样的值（防止"构造器与读取器一起错"）。
func TestMMDBRecordLayout(t *testing.T) {
	t.Run("24", func(t *testing.T) {
		buf := make([]byte, 6)
		mmdbTestWriteRecord(buf, 0, 24, false, 0x0ABCDEF)
		mmdbTestWriteRecord(buf, 0, 24, true, 0x123456)
		want := []byte{0xAB, 0xCD, 0xEF, 0x12, 0x34, 0x56}
		if !bytes.Equal(buf, want) {
			t.Fatalf("24 位记录字节 = % X，want % X", buf, want)
		}
		reader := &MMDBReader{buf: buf, meta: mmdbMetadata{nodeCount: 4, recordSize: 24, ipVersion: 4, searchTreeSize: 6}}
		assertMMDBRecordValue(t, reader, 0, 0x0ABCDEF, 0x123456)
	})

	t.Run("28", func(t *testing.T) {
		buf := make([]byte, 7)
		mmdbTestWriteRecord(buf, 0, 28, false, 0x0ABCDEF)
		mmdbTestWriteRecord(buf, 0, 28, true, 0x1234567)
		// 中间字节 0x01：高半字节是左记录的 27..24（0），低半字节是右记录的 27..24（1）。
		want := []byte{0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67}
		if !bytes.Equal(buf, want) {
			t.Fatalf("28 位记录字节 = % X，want % X", buf, want)
		}
		if got := mmdbTestReadRecord(buf, 0, 28, false); got != 0x0ABCDEF {
			t.Fatalf("测试读取器左记录 = %#x", got)
		}
		if got := mmdbTestReadRecord(buf, 0, 28, true); got != 0x1234567 {
			t.Fatalf("测试读取器右记录 = %#x", got)
		}
		reader := &MMDBReader{buf: buf, meta: mmdbMetadata{nodeCount: 4, recordSize: 28, ipVersion: 4, searchTreeSize: 7}}
		assertMMDBRecordValue(t, reader, 0, 0x0ABCDEF, 0x1234567)
	})

	t.Run("32", func(t *testing.T) {
		buf := make([]byte, 8)
		mmdbTestWriteRecord(buf, 0, 32, false, 0x0ABCDEF0)
		mmdbTestWriteRecord(buf, 0, 32, true, 0x12345678)
		want := []byte{0x0A, 0xBC, 0xDE, 0xF0, 0x12, 0x34, 0x56, 0x78}
		if !bytes.Equal(buf, want) {
			t.Fatalf("32 位记录字节 = % X，want % X", buf, want)
		}
		reader := &MMDBReader{buf: buf, meta: mmdbMetadata{nodeCount: 4, recordSize: 32, ipVersion: 4, searchTreeSize: 8}}
		assertMMDBRecordValue(t, reader, 0, 0x0ABCDEF0, 0x12345678)
	})
}

// assertMMDBRecordValue 用读取器的 readRecord 校验左右记录值。
func assertMMDBRecordValue(t *testing.T, reader *MMDBReader, node uint32, left, right uint32) {
	t.Helper()
	gotLeft, err := reader.readRecord(node, false)
	if err != nil {
		t.Fatalf("读取左记录失败：%v", err)
	}
	if gotLeft != left {
		t.Fatalf("左记录 = %#x，want %#x", gotLeft, left)
	}
	gotRight, err := reader.readRecord(node, true)
	if err != nil {
		t.Fatalf("读取右记录失败：%v", err)
	}
	if gotRight != right {
		t.Fatalf("右记录 = %#x，want %#x", gotRight, right)
	}
}

// ===== (a) 两国/城市/时区解析正确、未覆盖 IP 返回空 =====

func TestMMDBLookupIPv4IPv6(t *testing.T) {
	entries := testMMDBEntries()
	reader := NewMMDBReader(writeTestMMDB(t, 28, entries))

	metadata, err := reader.Metadata()
	if err != nil {
		t.Fatalf("元数据解析失败：%v", err)
	}
	if metadata.RecordSize != 28 || metadata.IPVersion != 6 || metadata.DatabaseType != "GeoLite2-City" {
		t.Fatalf("元数据 = %+v", metadata)
	}
	if metadata.NodeCount == 0 {
		t.Fatal("node_count 不应为 0")
	}
	if reader.LoadedAt().IsZero() || reader.LoadError() != nil {
		t.Fatalf("加载状态异常：loadedAt=%v err=%v", reader.LoadedAt(), reader.LoadError())
	}

	record, ok, err := reader.LookupIP("1.2.3.55")
	if err != nil || !ok {
		t.Fatalf("1.2.3.55 应命中：%+v/%v/%v", record, ok, err)
	}
	assertGeoRecord(t, record, entries[0].record)

	// 网段边界地址同样应命中。
	if _, ok, _ := reader.LookupIP("1.2.3.0"); !ok {
		t.Fatal("网段首地址 1.2.3.0 应命中")
	}
	if _, ok, _ := reader.LookupIP("1.2.3.255"); !ok {
		t.Fatal("网段末地址 1.2.3.255 应命中")
	}

	record, ok, err = reader.LookupIP("8.8.8.8")
	if err != nil || !ok {
		t.Fatalf("8.8.8.8 应命中：%+v/%v/%v", record, ok, err)
	}
	assertGeoRecord(t, record, entries[1].record)

	// IPv6 网段（同一个 v6 搜索树里）。
	record, ok, err = reader.LookupIP("2001:db8:1::5")
	if err != nil || !ok {
		t.Fatalf("2001:db8:1::5 应命中：%+v/%v/%v", record, ok, err)
	}
	assertGeoRecord(t, record, entries[2].record)

	// 未覆盖的 IP（含非法输入）返回空记录、found=false、无错误。
	for _, ip := range []string{"1.2.4.1", "4.4.4.4", "9.9.9.9", "2001:dead::1", "not-an-ip", ""} {
		record, ok, err = reader.LookupIP(ip)
		if err != nil {
			t.Fatalf("未覆盖 IP %q 不应返回错误：%v", ip, err)
		}
		if ok || !record.IsEmpty() {
			t.Fatalf("未覆盖 IP %q 应返回空记录，实际 %+v/%v", ip, record, ok)
		}
	}
}

// ===== (b) record_size 24 / 28（并覆盖 32）都能解析 =====

func TestMMDBRecordSizes(t *testing.T) {
	entries := testMMDBEntries()
	for _, recordSize := range []int{24, 28, 32} {
		t.Run(fmt.Sprintf("record_size_%d", recordSize), func(t *testing.T) {
			file := buildTestMMDB(t, recordSize, entries)
			// 构造器自身的布局自检：数据区起点 = 搜索树 + 16 字节分隔符，且数据区非空。
			if file.dataStart != file.treeSize+mmdbDataSeparatorSize || file.dataEnd <= file.dataStart {
				t.Fatalf("构造器布局异常：treeSize=%d dataStart=%d dataEnd=%d",
					file.treeSize, file.dataStart, file.dataEnd)
			}
			reader := NewMMDBReader(writeMMDBBytes(t, file.bytes, "GeoLite2-City.mmdb"))
			metadata, err := reader.Metadata()
			if err != nil {
				t.Fatalf("元数据解析失败：%v", err)
			}
			if metadata.RecordSize != recordSize {
				t.Fatalf("record_size = %d，want %d", metadata.RecordSize, recordSize)
			}
			record, ok, err := reader.LookupIP("1.2.3.55")
			if err != nil || !ok {
				t.Fatalf("record_size=%d 时 1.2.3.55 应命中：%+v/%v/%v", recordSize, record, ok, err)
			}
			assertGeoRecord(t, record, entries[0].record)
			if _, ok, _ := reader.LookupIP("8.8.8.8"); !ok {
				t.Fatalf("record_size=%d 时 8.8.8.8 应命中", recordSize)
			}
			if _, ok, _ := reader.LookupIP("2001:db8:1::5"); !ok {
				t.Fatalf("record_size=%d 时 2001:db8:1::5 应命中", recordSize)
			}
			if _, ok, _ := reader.LookupIP("1.2.4.1"); ok {
				t.Fatalf("record_size=%d 时未覆盖 IP 不应命中", recordSize)
			}
		})
	}
}

// TestMMDBIPv4OnlyDatabase 覆盖 ip_version=4 的库：IPv4 直接按 32 位遍历，
// 查询 IPv6 地址视为未覆盖（而不是报错或 panic）。
func TestMMDBIPv4OnlyDatabase(t *testing.T) {
	entries := testMMDBEntries()
	// 只放两个 IPv4 网段（v4 库不支持 IPv6 网段）。
	file := buildTestMMDBVersion(t, 28, 4, entries[:2])
	reader := NewMMDBReader(writeMMDBBytes(t, file.bytes, "GeoLite2-City-v4.mmdb"))
	metadata, err := reader.Metadata()
	if err != nil {
		t.Fatalf("元数据解析失败：%v", err)
	}
	if metadata.IPVersion != 4 {
		t.Fatalf("ip_version = %d，want 4", metadata.IPVersion)
	}
	record, ok, err := reader.LookupIP("1.2.3.55")
	if err != nil || !ok {
		t.Fatalf("v4 库 1.2.3.55 应命中：%+v/%v/%v", record, ok, err)
	}
	assertGeoRecord(t, record, entries[0].record)
	if _, ok, _ := reader.LookupIP("8.8.8.8"); !ok {
		t.Fatal("v4 库 8.8.8.8 应命中")
	}
	if record, ok, err := reader.LookupIP("2001:db8:1::5"); err != nil || ok || !record.IsEmpty() {
		t.Fatalf("v4 库查询 IPv6 应为空且无错误：%+v/%v/%v", record, ok, err)
	}
	if _, ok, _ := reader.LookupIP("1.2.4.1"); ok {
		t.Fatal("v4 库未覆盖 IP 不应命中")
	}
}

// ===== (c) 损坏文件：截断 / 标记丢失 / 越界指针 / 元数据非法 =====

func TestMMDBCorruptedFiles(t *testing.T) {
	valid := buildTestMMDB(t, 28, testMMDBEntries())

	t.Run("missing_file", func(t *testing.T) {
		reader := NewMMDBReader(filepath.Join(t.TempDir(), "not-exists.mmdb"))
		if err := reader.EnsureLoaded(); err == nil {
			t.Fatal("文件不存在应返回错误")
		}
		if _, ok := reader.Lookup("1.2.3.55"); ok {
			t.Fatal("文件不存在时不应命中")
		}
	})

	t.Run("truncated", func(t *testing.T) {
		path := writeMMDBBytes(t, valid.bytes[:len(valid.bytes)/3], "truncated.mmdb")
		reader := NewMMDBReader(path)
		err := reader.EnsureLoaded()
		if err == nil {
			t.Fatal("截断文件应返回错误")
		}
		if !errors.Is(err, ErrMMDBInvalid) {
			t.Fatalf("错误应包装 ErrMMDBInvalid：%v", err)
		}
		// 加载错误同样被缓存：第二次仍返回错误且不 panic。
		if second := reader.EnsureLoaded(); second == nil {
			t.Fatal("加载错误应被缓存")
		}
		if _, _, err := reader.LookupIP("1.2.3.55"); err == nil {
			t.Fatal("截断文件的查询应返回错误")
		}
		if record, ok := reader.Lookup("1.2.3.55"); ok || !record.IsEmpty() {
			t.Fatalf("Lookup 应静默降级：%+v/%v", record, ok)
		}
	})

	t.Run("missing_marker", func(t *testing.T) {
		broken := bytes.ReplaceAll(valid.bytes, []byte(mmdbMetadataMarker), []byte("\xab\xcd\xefMaxMind.coX"))
		path := writeMMDBBytes(t, broken, "no-marker.mmdb")
		reader := NewMMDBReader(path)
		err := reader.EnsureLoaded()
		if err == nil {
			t.Fatal("标记丢失应返回错误")
		}
		if !errors.Is(err, ErrMMDBInvalid) || !strings.Contains(err.Error(), "元数据标记") {
			t.Fatalf("标记丢失错误文案不符：%v", err)
		}
		if _, ok := reader.Lookup("1.2.3.55"); ok {
			t.Fatal("标记丢失时不应命中")
		}
	})

	t.Run("invalid_record_size", func(t *testing.T) {
		broken := append([]byte(nil), valid.bytes...)
		needle := append(mmdbTestString("record_size"), mmdbTestUint16(28)...)
		index := bytes.Index(broken, needle)
		if index < 0 {
			t.Fatal("未在元数据中找到 record_size")
		}
		broken[index+len(needle)-1] = 33 // 33 不是合法的 record_size
		path := writeMMDBBytes(t, broken, "bad-record-size.mmdb")
		reader := NewMMDBReader(path)
		err := reader.EnsureLoaded()
		if err == nil {
			t.Fatal("非法 record_size 应返回错误")
		}
		if !errors.Is(err, ErrMMDBInvalid) || !strings.Contains(err.Error(), "record_size") {
			t.Fatalf("非法 record_size 错误文案不符：%v", err)
		}
	})

	t.Run("out_of_range_pointer", func(t *testing.T) {
		broken := append([]byte(nil), valid.bytes...)
		nodeBytes := valid.recordSize * 2 / 8
		badValue := uint32(valid.nodeCount + mmdbDataSeparatorSize + len(broken)) // 远超数据区末尾
		patched := 0
		for node := 0; node < valid.nodeCount; node++ {
			for direction := 0; direction < 2; direction++ {
				offset := node * nodeBytes
				if mmdbTestReadRecord(broken, offset, valid.recordSize, direction == 1) > uint32(valid.nodeCount) {
					mmdbTestWriteRecord(broken, offset, valid.recordSize, direction == 1, badValue)
					patched++
				}
			}
		}
		if patched == 0 {
			t.Fatal("测试构造器没有生成任何数据指针")
		}
		path := writeMMDBBytes(t, broken, "bad-pointer.mmdb")
		reader := NewMMDBReader(path)
		// 搜索树与元数据本身仍然合法，可以加载；错误发生在查询解析数据指针时。
		if err := reader.EnsureLoaded(); err != nil {
			t.Fatalf("搜索树本身可加载，不应在这里报错：%v", err)
		}
		if _, _, err := reader.LookupIP("1.2.3.55"); err == nil {
			t.Fatal("越界数据指针应返回错误")
		} else if !errors.Is(err, ErrMMDBInvalid) {
			t.Fatalf("越界指针错误应包装 ErrMMDBInvalid：%v", err)
		}
		if record, ok := reader.Lookup("1.2.3.55"); ok || !record.IsEmpty() {
			t.Fatalf("Lookup 应静默降级：%+v/%v", record, ok)
		}
	})

	t.Run("lookup_before_load_of_empty_file", func(t *testing.T) {
		path := writeMMDBBytes(t, nil, "empty.mmdb")
		reader := NewMMDBReader(path)
		if _, _, err := reader.LookupIP("1.2.3.55"); err == nil {
			t.Fatal("空文件应返回错误")
		}
	})
}

// ===== (d) .mmdb 与 CSV 两种来源的 GeoRecord 形状一致 =====

func TestMMDBMatchesCSVRecordShape(t *testing.T) {
	csvStore := NewLocalStore(writeTestGeoDB(t))
	mmdbStore := NewLocalStore(writeTestMMDB(t, 28, testMMDBEntries()))
	if err := csvStore.EnsureLoaded(); err != nil {
		t.Fatalf("加载 CSV 库失败：%v", err)
	}
	if err := mmdbStore.EnsureLoaded(); err != nil {
		t.Fatalf("加载 mmdb 库失败：%v", err)
	}

	for _, ip := range []string{"1.2.3.55", "8.8.8.8", "2001:db8:1::5"} {
		csvRecord, csvOK := csvStore.Lookup(ip)
		mmdbRecord, mmdbOK := mmdbStore.Lookup(ip)
		if !csvOK || !mmdbOK {
			t.Fatalf("IP %s 应同时命中两种来源：csv=%v mmdb=%v", ip, csvOK, mmdbOK)
		}
		csvJSON, err := json.Marshal(csvRecord)
		if err != nil {
			t.Fatalf("序列化 CSV 记录失败：%v", err)
		}
		mmdbJSON, err := json.Marshal(mmdbRecord)
		if err != nil {
			t.Fatalf("序列化 mmdb 记录失败：%v", err)
		}
		// JSON 相同即字段名、字段值与 Basis 全部一致。
		if string(csvJSON) != string(mmdbJSON) {
			t.Fatalf("IP %s 两种来源的 GeoRecord 不一致：\nCSV  = %s\nMMDB = %s", ip, csvJSON, mmdbJSON)
		}
		if !strings.Contains(string(mmdbJSON), `"basis":"`+domain.GeoBasisLocal+`"`) {
			t.Fatalf("IP %s 的 Basis 应为 %q：%s", ip, domain.GeoBasisLocal, mmdbJSON)
		}
		for _, key := range []string{`"country"`, `"country_code"`, `"region"`, `"city"`, `"timezone"`, `"basis"`} {
			if !strings.Contains(string(mmdbJSON), key) {
				t.Fatalf("IP %s 的记录缺少字段 %s：%s", ip, key, mmdbJSON)
			}
		}
	}

	// 两者都未覆盖的 IP 都返回空。
	if record, ok := mmdbStore.Lookup("1.2.4.1"); ok || !record.IsEmpty() {
		t.Fatalf("mmdb 未覆盖 IP 应为空：%+v/%v", record, ok)
	}
}

// TestResolverWithMMDBLocalDB 覆盖 .mmdb 接入三级解析：本地命中不触发第三方，
// 未覆盖时才回落，且解析失败的 mmdb 只降级、不再返回旧的"未包含解析库"文案。
func TestResolverWithMMDBLocalDB(t *testing.T) {
	path := writeTestMMDB(t, 28, testMMDBEntries())
	resolver := NewResolver()
	if err := resolver.LocalError(path); err != nil {
		t.Fatalf("合法 mmdb 不应报加载错误：%v", err)
	}
	server, calls := newThirdPartyStub(t, 0)
	cfg := Config{
		GeoIPDir:            path,
		ThirdPartyURL:       server.URL + "/ip/{ip}",
		ThirdPartyTimeoutMS: 1000,
		CacheTTL:            time.Hour,
	}
	ctx := context.Background()

	record, ok := resolver.Resolve(ctx, cfg, "1.2.3.55", http.Header{})
	if !ok || record.Basis != domain.GeoBasisLocal {
		t.Fatalf("本地 mmdb 应命中：%+v/%v", record, ok)
	}
	assertGeoRecord(t, record, testMMDBEntries()[0].record)
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("本地 mmdb 命中时不应调用第三方，实际 %d 次", got)
	}

	// 本地未覆盖 → 回落第三方。
	record, ok = resolver.Resolve(ctx, cfg, "203.0.113.9", http.Header{})
	if !ok || record.Basis != domain.GeoBasisThirdParty {
		t.Fatalf("本地未覆盖时应回落第三方：%+v/%v", record, ok)
	}

	// 损坏 mmdb：只降级，错误里不再出现旧的"未包含 mmdb 解析库"。
	broken := writeMMDBBytes(t, []byte("not-a-real-mmdb"), "broken.mmdb")
	err := resolver.LocalError(broken)
	if err == nil {
		t.Fatal("损坏 mmdb 应返回加载错误")
	}
	if strings.Contains(err.Error(), "未包含 mmdb 解析库") {
		t.Fatalf("不应再返回旧提示：%v", err)
	}
	if !errors.Is(err, ErrLocalDBUnavailable) || !errors.Is(err, ErrMMDBInvalid) {
		t.Fatalf("错误应同时包装 ErrLocalDBUnavailable 与 ErrMMDBInvalid：%v", err)
	}
}

// TestMMDBDecoderTypes 直接覆盖数据区解码的类型分支：
// string / uint16 / uint32 / uint64 / double / bytes / boolean / uint128 / pointer / end marker。
func TestMMDBDecoderTypes(t *testing.T) {
	shared := mmdbTestString("shared")
	data := append([]byte(nil), shared...)
	data = append(data, mmdbTestMap(
		mmdbTestPair{"text", mmdbTestString("hello")},
		mmdbTestPair{"small", mmdbTestUint16(7)},
		mmdbTestPair{"number", mmdbTestUint32(70000)},
		mmdbTestPair{"large", mmdbTestUint64(1 << 40)},
		mmdbTestPair{"double", mmdbTestDouble(1.5)},
		mmdbTestPair{"bytes", append(mmdbTestControl(mmdbTypeBytes, 2), 0x01, 0x02)},
		mmdbTestPair{"flag", mmdbTestBoolean(true)},
		// uint128（通常存 IPv6 地址）在本读取器里被跳过，解码为 nil。
		mmdbTestPair{"big", append(mmdbTestControl(mmdbTypeUint128, 16), make([]byte, 16)...)},
		// 指针指向数据区偏移 0 处的 shared 字符串。
		mmdbTestPair{"alias", mmdbTestPointer(0)},
		// 已废弃的 end marker：无 payload。
		mmdbTestPair{"end", []byte{0x00, 0x06}},
	)...)

	decoder := mmdbDecoder{buf: data}
	value, next, err := decoder.decode(len(shared), 0, len(data), 0)
	if err != nil {
		t.Fatalf("解码失败：%v", err)
	}
	if next != len(data) {
		t.Fatalf("解码未消费完整数据区：%d != %d", next, len(data))
	}
	fields, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("解码结果不是 map：%T", value)
	}
	if got := fields["text"]; got != "hello" {
		t.Fatalf("text = %#v", got)
	}
	if got := fields["small"]; got != uint64(7) {
		t.Fatalf("small = %#v", got)
	}
	if got := fields["number"]; got != uint64(70000) {
		t.Fatalf("number = %#v", got)
	}
	if got := fields["large"]; got != uint64(1<<40) {
		t.Fatalf("large = %#v", got)
	}
	if got := fields["double"]; got != 1.5 {
		t.Fatalf("double = %#v", got)
	}
	if got, ok := fields["bytes"].([]byte); !ok || len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("bytes = %#v", fields["bytes"])
	}
	if got := fields["flag"]; got != true {
		t.Fatalf("flag = %#v", got)
	}
	if got, exists := fields["big"]; !exists || got != nil {
		t.Fatalf("uint128 应被跳过为 nil：%#v", got)
	}
	if got := fields["alias"]; got != "shared" {
		t.Fatalf("pointer = %#v，want shared", got)
	}
	if got, exists := fields["end"]; !exists || got != nil {
		t.Fatalf("end marker 应为 nil：%#v", got)
	}

	// 越界偏移必须报错而不是 panic。
	if _, _, err := decoder.decode(len(data), 0, len(data), 0); err == nil {
		t.Fatal("越界偏移应返回错误")
	}
}

// TestMMDBConcurrentLazyLookup 覆盖并发懒加载与查询（配合 -race 运行）。
func TestMMDBConcurrentLazyLookup(t *testing.T) {
	reader := NewMMDBReader(writeTestMMDB(t, 24, testMMDBEntries()))
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for index := 0; index < 16; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 25; round++ {
				record, ok := reader.Lookup("1.2.3.55")
				if !ok || record.CountryCode != "SA" {
					failures <- fmt.Errorf("并发查询结果异常：%+v/%v", record, ok)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if reader.LoadedAt().IsZero() {
		t.Fatal("并发懒加载应成功")
	}
	if _, ok := reader.Lookup("9.9.9.9"); ok {
		t.Fatal("未覆盖 IP 不应命中")
	}
}
