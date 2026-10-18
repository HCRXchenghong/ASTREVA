package ai

import (
	"regexp"
	"strings"
	"sync"

	"unicode"
)

// NormalizeLang 把各种语言标记归一化为基础语言码（zh / en / ar / es ...），
// 无法识别时回退为 fallback（fallback 为空则回退 zh）。
// 方言标签（如 es-419、ar-Gulf）只保留语言部分，译文键始终使用基础码。
func NormalizeLang(value, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, "_", "-")
	if v != "" {
		base := v
		if index := strings.IndexAny(base, "-"); index > 0 {
			base = base[:index]
		}
		if code, ok := languageNames[base]; ok {
			return code
		}
		switch base {
		case LangZh, LangEn, LangAr, LangEs, LangFr, LangDe, LangPt, LangIt, LangNl,
			LangID, LangVi, LangTr, LangPl, LangRo, LangRu, LangJa, LangKo, LangTh, LangHe, LangHi, LangEl:
			return base
		case LangYue:
			return LangZh
		}
		// 兼容完整语言名（如 "spanish" / "英语"）
		if code, ok := lookupLanguageName(base); ok {
			return code
		}
	}
	if fallback != "" {
		return fallback
	}
	return LangZh
}

// LangName 返回用于提示词的语言名称（英文名，便于模型理解）。
func LangName(lang string) string {
	switch NormalizeLang(lang, LangZh) {
	case LangEn:
		return "English"
	case LangAr:
		return "Arabic"
	case LangEs:
		return "Spanish"
	case LangFr:
		return "French"
	case LangDe:
		return "German"
	case LangPt:
		return "Portuguese"
	case LangIt:
		return "Italian"
	case LangNl:
		return "Dutch"
	case LangID:
		return "Indonesian"
	case LangVi:
		return "Vietnamese"
	case LangTr:
		return "Turkish"
	case LangPl:
		return "Polish"
	case LangRo:
		return "Romanian"
	case LangRu:
		return "Russian"
	case LangJa:
		return "Japanese"
	case LangKo:
		return "Korean"
	case LangTh:
		return "Thai"
	case LangHe:
		return "Hebrew"
	case LangHi:
		return "Hindi"
	case LangEl:
		return "Greek"
	default:
		return "Simplified Chinese"
	}
}

func countRunesMatching(text string, match func(r rune) bool) int {
	count := 0
	for _, r := range text {
		if match(r) {
			count++
		}
	}
	return count
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

func isArabic(r rune) bool {
	return unicode.Is(unicode.Arabic, r)
}

func isLatinLetter(r rune) bool {
	return unicode.IsLetter(r) && !isCJK(r) && !isArabic(r)
}

// DetectLang 兼容旧调用点：返回基础语言码。完整结果请用 DetectLanguage。
func DetectLang(text string) string {
	return DetectLanguage(text).Code
}

var (
	regexCacheMu sync.Mutex
	regexCache   = map[string]*regexp.Regexp{}
)

// mustCompile 带缓存的 MustCompile；表达式非法时返回 nil 而不是 panic。
func mustCompile(pattern string) *regexp.Regexp {
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()
	if re, ok := regexCache[pattern]; ok {
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		regexCache[pattern] = nil
		return nil
	}
	regexCache[pattern] = re
	return re
}
