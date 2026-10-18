package ai

import (
	"strings"
	"unicode"
)

// 支持的语言代码。访客端、客服端与翻译引擎统一使用这三个代码。
const (
	LangZh = "zh"
	LangEn = "en"
	LangAr = "ar"
)

// NormalizeLang 把各种语言标记归一化为 zh / en / ar，无法识别时回退为 fallback。
func NormalizeLang(value, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, "_", "-")
	switch {
	case strings.HasPrefix(v, "zh"):
		return LangZh
	case strings.HasPrefix(v, "ar"):
		return LangAr
	case strings.HasPrefix(v, "en"):
		return LangEn
	}
	if fallback != "" {
		return fallback
	}
	return LangZh
}

// LangName 返回用于提示词的语言名称。
func LangName(lang string) string {
	switch lang {
	case LangEn:
		return "English"
	case LangAr:
		return "Arabic"
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

// DetectLang 基于字符构成粗略判断一段文本的语言，用于决定是否需要翻译。
// 它只影响是否跳过翻译，不影响消息最终展示的语言。
func DetectLang(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return LangZh
	}
	cjk := countRunesMatching(text, isCJK)
	arabic := countRunesMatching(text, isArabic)
	latin := countRunesMatching(text, isLatinLetter)
	total := cjk + arabic + latin
	if total == 0 {
		return LangZh
	}
	if cjk > 0 && float64(cjk)/float64(total) >= 0.3 {
		return LangZh
	}
	if arabic > latin && arabic > cjk {
		return LangAr
	}
	if latin >= arabic && latin > cjk {
		return LangEn
	}
	return LangZh
}
