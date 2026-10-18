package domain

import "strings"

// ===== 地理名称与国籍推测所需的静态表（零依赖）=====
//
// 控制台契约要求 country/region/city 是可直接展示的名称：
//   - GeoLite2 Locations CSV 提供 country_name / subdivision_1_name / city_name（zh-CN 文件为中文）；
//   - CDN 头与部分第三方库只给 ISO 码，这里用内置中文名表补齐 country 的可读名，
//     同时始终返回 country_code，前端两者都能展示。

// NormalizeCountryCode 归一化 ISO alpha-2 国家码：转大写、只保留两位字母；
// 占位值（XX/T1/ZZ）视为未知返回空串。
func NormalizeCountryCode(value string) string {
	code := strings.ToUpper(strings.TrimSpace(value))
	if len(code) != 2 {
		return ""
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	switch code {
	case "XX", "T1", "ZZ":
		return ""
	}
	return code
}

// countryNamesZh 是 ISO alpha-2 -> 中文名（覆盖主要市场，未知码回落为原码）。
var countryNamesZh = map[string]string{
	"AE": "阿联酋", "AF": "阿富汗", "AL": "阿尔巴尼亚", "AM": "亚美尼亚", "AO": "安哥拉",
	"AR": "阿根廷", "AT": "奥地利", "AU": "澳大利亚", "AZ": "阿塞拜疆", "BA": "波黑",
	"BD": "孟加拉国", "BE": "比利时", "BG": "保加利亚", "BH": "巴林", "BO": "玻利维亚",
	"BR": "巴西", "BY": "白俄罗斯", "CA": "加拿大", "CH": "瑞士", "CL": "智利",
	"CM": "喀麦隆", "CN": "中国", "CO": "哥伦比亚", "CR": "哥斯达黎加", "CU": "古巴",
	"CY": "塞浦路斯", "CZ": "捷克", "DE": "德国", "DK": "丹麦", "DO": "多米尼加",
	"DZ": "阿尔及利亚", "EC": "厄瓜多尔", "EE": "爱沙尼亚", "EG": "埃及", "ES": "西班牙",
	"ET": "埃塞俄比亚", "FI": "芬兰", "FR": "法国", "GB": "英国", "GE": "格鲁吉亚",
	"GH": "加纳", "GR": "希腊", "GT": "危地马拉", "HK": "中国香港", "HN": "洪都拉斯",
	"HR": "克罗地亚", "HU": "匈牙利", "ID": "印度尼西亚", "IE": "爱尔兰", "IL": "以色列",
	"IN": "印度", "IQ": "伊拉克", "IR": "伊朗", "IS": "冰岛", "IT": "意大利",
	"JM": "牙买加", "JO": "约旦", "JP": "日本", "KE": "肯尼亚", "KG": "吉尔吉斯斯坦",
	"KH": "柬埔寨", "KR": "韩国", "KW": "科威特", "KZ": "哈萨克斯坦", "LA": "老挝",
	"LB": "黎巴嫩", "LK": "斯里兰卡", "LT": "立陶宛", "LU": "卢森堡", "LV": "拉脱维亚",
	"LY": "利比亚", "MA": "摩洛哥", "MD": "摩尔多瓦", "MM": "缅甸", "MN": "蒙古",
	"MO": "中国澳门", "MX": "墨西哥", "MY": "马来西亚", "MZ": "莫桑比克", "NG": "尼日利亚",
	"NI": "尼加拉瓜", "NL": "荷兰", "NO": "挪威", "NP": "尼泊尔", "NZ": "新西兰",
	"OM": "阿曼", "PA": "巴拿马", "PE": "秘鲁", "PH": "菲律宾", "PK": "巴基斯坦",
	"PL": "波兰", "PR": "波多黎各", "PT": "葡萄牙", "PY": "巴拉圭", "QA": "卡塔尔",
	"RO": "罗马尼亚", "RS": "塞尔维亚", "RU": "俄罗斯", "SA": "沙特阿拉伯", "SD": "苏丹",
	"SE": "瑞典", "SG": "新加坡", "SI": "斯洛文尼亚", "SK": "斯洛伐克", "SN": "塞内加尔",
	"SV": "萨尔瓦多", "SY": "叙利亚", "TH": "泰国", "TN": "突尼斯", "TR": "土耳其",
	"TW": "中国台湾", "TZ": "坦桑尼亚", "UA": "乌克兰", "UG": "乌干达", "US": "美国",
	"UY": "乌拉圭", "UZ": "乌兹别克斯坦", "VE": "委内瑞拉", "VN": "越南", "YE": "也门",
	"ZA": "南非", "ZM": "赞比亚", "ZW": "津巴布韦",
}

// CountryName 返回国家码对应的可读名称；未知码原样返回（保证 country 始终可直接展示）。
func CountryName(code string) string {
	normalized := NormalizeCountryCode(code)
	if normalized == "" {
		return ""
	}
	if name, ok := countryNamesZh[normalized]; ok {
		return name
	}
	return normalized
}

// countryLanguages 是"国家 -> 官方/主要语言"表，用于判断语言证据是否与该国一致。
var countryLanguages = map[string][]string{
	"AE": {"ar"}, "AR": {"es"}, "AT": {"de"}, "AU": {"en"}, "BE": {"nl", "fr"},
	"BH": {"ar"}, "BO": {"es"}, "BR": {"pt"}, "CA": {"en", "fr"}, "CH": {"de", "fr", "it"},
	"CL": {"es"}, "CN": {"zh"}, "CO": {"es"}, "CR": {"es"}, "CU": {"es"},
	"DE": {"de"}, "DK": {"da"}, "DO": {"es"}, "DZ": {"ar"}, "EC": {"es"},
	"EG": {"ar"}, "ES": {"es"}, "FI": {"fi"}, "FR": {"fr"}, "GB": {"en"},
	"GR": {"el"}, "GT": {"es"}, "HK": {"zh"}, "HN": {"es"}, "ID": {"id"},
	"IE": {"en"}, "IL": {"he"}, "IN": {"hi", "en"}, "IQ": {"ar"}, "IR": {"fa"},
	"IT": {"it"}, "JO": {"ar"}, "JP": {"ja"}, "KE": {"en"}, "KR": {"ko"},
	"KW": {"ar"}, "LB": {"ar"}, "LY": {"ar"}, "MA": {"ar"}, "MX": {"es"},
	"MY": {"ms", "en"}, "NG": {"en"}, "NI": {"es"}, "NL": {"nl"}, "NO": {"no"},
	"NZ": {"en"}, "OM": {"ar"}, "PA": {"es"}, "PE": {"es"}, "PH": {"en"},
	"PK": {"ur", "en"}, "PL": {"pl"}, "PT": {"pt"}, "PY": {"es"}, "QA": {"ar"},
	"RO": {"ro"}, "RU": {"ru"}, "SA": {"ar"}, "SE": {"sv"}, "SG": {"en", "zh"},
	"SV": {"es"}, "SY": {"ar"}, "TH": {"th"}, "TN": {"ar"}, "TR": {"tr"},
	"TW": {"zh"}, "UA": {"uk", "ru"}, "US": {"en"}, "UY": {"es"}, "VE": {"es"},
	"VN": {"vi"}, "YE": {"ar"}, "ZA": {"en"},
}

// languagePrimaryCountry 是"语言 -> 最可能国家"表，仅在完全没有地理信息时
// 用于国籍推测（低置信度 0.3），绝不用于填充地理字段。
var languagePrimaryCountry = map[string]string{
	"zh": "CN", "en": "US", "ar": "SA", "es": "ES", "fr": "FR", "de": "DE",
	"pt": "PT", "it": "IT", "nl": "NL", "id": "ID", "vi": "VN", "tr": "TR",
	"pl": "PL", "ro": "RO", "ru": "RU", "ja": "JP", "ko": "KR", "th": "TH",
	"he": "IL", "hi": "IN", "el": "GR",
}

// timezoneCountry 是常见 IANA 时区 -> 国家码，用于判断时区证据是否与地理一致。
var timezoneCountry = map[string]string{
	"Africa/Cairo": "EG", "Africa/Casablanca": "MA", "Africa/Johannesburg": "ZA",
	"Africa/Lagos": "NG", "Africa/Nairobi": "KE", "Africa/Tunis": "TN",
	"America/Argentina/Buenos_Aires": "AR", "America/Bogota": "CO", "America/Caracas": "VE",
	"America/Chicago": "US", "America/Denver": "US", "America/Lima": "PE",
	"America/Los_Angeles": "US", "America/Mexico_City": "MX", "America/New_York": "US",
	"America/Panama": "PA", "America/Phoenix": "US", "America/Santiago": "CL",
	"America/Sao_Paulo": "BR", "America/Toronto": "CA", "Asia/Almaty": "KZ",
	"Asia/Amman": "JO", "Asia/Baghdad": "IQ", "Asia/Bahrain": "BH", "Asia/Baku": "AZ",
	"Asia/Bangkok": "TH", "Asia/Beirut": "LB", "Asia/Colombo": "LK", "Asia/Dhaka": "BD",
	"Asia/Dubai": "AE", "Asia/Ho_Chi_Minh": "VN", "Asia/Hong_Kong": "HK",
	"Asia/Hovd": "MN", "Asia/Istanbul": "TR", "Asia/Jakarta": "ID",
	"Asia/Jerusalem": "IL", "Asia/Karachi": "PK", "Asia/Kathmandu": "NP",
	"Asia/Kolkata": "IN", "Asia/Kuala_Lumpur": "MY", "Asia/Kuwait": "KW",
	"Asia/Macau": "MO", "Asia/Manila": "PH", "Asia/Muscat": "OM", "Asia/Qatar": "QA",
	"Asia/Riyadh": "SA", "Asia/Seoul": "KR", "Asia/Shanghai": "CN", "Asia/Singapore": "SG",
	"Asia/Taipei": "TW", "Asia/Tashkent": "UZ", "Asia/Tbilisi": "GE",
	"Asia/Tehran": "IR", "Asia/Tokyo": "JP", "Asia/Ulaanbaatar": "MN",
	"Asia/Yangon": "MM", "Asia/Yekaterinburg": "RU",
	"Atlantic/Reykjavik": "IS", "Australia/Perth": "AU", "Australia/Sydney": "AU",
	"Europe/Amsterdam": "NL", "Europe/Athens": "GR", "Europe/Belgrade": "RS",
	"Europe/Berlin": "DE", "Europe/Brussels": "BE", "Europe/Bucharest": "RO",
	"Europe/Budapest": "HU", "Europe/Copenhagen": "DK", "Europe/Dublin": "IE",
	"Europe/Helsinki": "FI", "Europe/Istanbul": "TR", "Europe/Kyiv": "UA",
	"Europe/Lisbon": "PT", "Europe/Ljubljana": "SI", "Europe/London": "GB",
	"Europe/Madrid": "ES", "Europe/Minsk": "BY", "Europe/Moscow": "RU",
	"Europe/Oslo": "NO", "Europe/Paris": "FR", "Europe/Prague": "CZ",
	"Europe/Riga": "LV", "Europe/Rome": "IT", "Europe/Sofia": "BG",
	"Europe/Stockholm": "SE", "Europe/Tallinn": "EE", "Europe/Vienna": "AT",
	"Europe/Vilnius": "LT", "Europe/Warsaw": "PL", "Europe/Zurich": "CH",
	"Pacific/Auckland": "NZ", "Pacific/Honolulu": "US", "UTC": "",
}

// GuessNationality 依据地理来源与语言/时区画像加权推测国籍。
// 返回值：ISO alpha-2 大写码、置信度（0..0.95）、依据列表
// （元素只会是 ip_cdn / geoip / third_party / language / timezone）。
//
// 权重规则（依据数量加权，上限 0.95）：
//   - IP 国别 0.6（依据为具体地理来源层级）；
//   - 主要语言是该国官方语言 +0.2（依据 language）；
//   - 时区与该国一致 +0.2（依据 timezone）；
//   - 完全没有地理信息时：语言 0.3、时区 0.2 单独成立。
func GuessNationality(geo GeoRecord, languages map[string]int, timezone string) (string, float64, []string) {
	basis := make([]string, 0, 3)
	country := NormalizeCountryCode(geo.CountryCode)
	if country == "" {
		country = NormalizeCountryCode(geo.Country)
	}
	confidence := 0.0
	if country != "" {
		confidence = 0.6
		basis = append(basis, normalizeGeoBasis(geo.Basis))
	}
	dominant := dominantLanguage(languages)
	if dominant != "" {
		switch {
		case country == "":
			if inferred, ok := languagePrimaryCountry[dominant]; ok && inferred != "" {
				country = inferred
				confidence = 0.3
				basis = append(basis, GeoBasisLanguage)
			}
		case languageMatchesCountry(dominant, country):
			confidence += 0.2
			basis = append(basis, GeoBasisLanguage)
		}
	}
	zone := strings.TrimSpace(timezone)
	if zone == "" {
		zone = strings.TrimSpace(geo.Timezone)
	}
	if zone != "" {
		switch inferred := timezoneCountry[zone]; {
		case country == "" && inferred != "":
			country = inferred
			confidence = 0.2
			basis = append(basis, GeoBasisTimezone)
		case country != "" && inferred == country && !containsString(basis, GeoBasisTimezone):
			confidence += 0.2
			basis = append(basis, GeoBasisTimezone)
		}
	}
	if country == "" {
		return "", 0, nil
	}
	if confidence > MaxNationalityConfidence {
		confidence = MaxNationalityConfidence
	}
	return country, roundConfidence(confidence), cleanStringList(basis)
}

// normalizeGeoBasis 把地理来源归一化为契约允许的键；未知来源按 CDN 处理。
func normalizeGeoBasis(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GeoBasisLocal:
		return GeoBasisLocal
	case GeoBasisThirdParty:
		return GeoBasisThirdParty
	case GeoBasisCDN:
		return GeoBasisCDN
	default:
		return GeoBasisCDN
	}
}

func dominantLanguage(languages map[string]int) string {
	best := ""
	bestCount := 0
	for lang, count := range languages {
		lang = strings.TrimSpace(lang)
		if lang == "" || count <= 0 {
			continue
		}
		if count > bestCount || (count == bestCount && lang < best) {
			best = lang
			bestCount = count
		}
	}
	return best
}

func languageMatchesCountry(language, country string) bool {
	for _, lang := range countryLanguages[country] {
		if lang == language {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func roundConfidence(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}
