package ai

import (
	"math"
	"strings"
	"unicode"
)

// LanguageResult 是语种识别的完整结果。
// Code 是基础语言码（用于 Translations 键），Tag 是 BCP-47 风格标签（含地区/方言），
// Dialect 只在证据充分时给出，否则为空（回落标准语）。
type LanguageResult struct {
	Code       string
	Tag        string
	Dialect    string
	Confidence float64
	Method     string // script | lexicon | script+lexicon | instruction | fallback
	Candidates []LanguageCandidate
}

// LanguageCandidate 是候选语种与得分，供"会话主语言"统计与调试使用。
type LanguageCandidate struct {
	Code  string
	Score float64
}

// 扩展支持的语言码。译文键只使用基础码，方言通过 Tag/Dialect 表达。
const (
	LangZh  = "zh"
	LangEn  = "en"
	LangAr  = "ar"
	LangEs  = "es"
	LangFr  = "fr"
	LangDe  = "de"
	LangPt  = "pt"
	LangIt  = "it"
	LangNl  = "nl"
	LangID  = "id"
	LangVi  = "vi"
	LangTr  = "tr"
	LangPl  = "pl"
	LangRo  = "ro"
	LangRu  = "ru"
	LangJa  = "ja"
	LangKo  = "ko"
	LangTh  = "th"
	LangHe  = "he"
	LangHi  = "hi"
	LangEl  = "el"
	LangYue = "yue"
)

type scriptCounter struct {
	han, kana, hangul, arabic, cyrillic, thai, hebrew, devanagari, greek, latin, other int
}

func classifyScripts(text string) scriptCounter {
	var c scriptCounter
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			c.han++
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			c.kana++
		case unicode.Is(unicode.Hangul, r):
			c.hangul++
		case unicode.Is(unicode.Arabic, r):
			c.arabic++
		case unicode.Is(unicode.Cyrillic, r):
			c.cyrillic++
		case unicode.Is(unicode.Thai, r):
			c.thai++
		case unicode.Is(unicode.Hebrew, r):
			c.hebrew++
		case unicode.Is(unicode.Devanagari, r):
			c.devanagari++
		case unicode.Is(unicode.Greek, r):
			c.greek++
		case unicode.Is(unicode.Latin, r):
			c.latin++
		default:
			if unicode.IsLetter(r) {
				c.other++
			}
		}
	}
	return c
}

var traditionalOnly = []rune("這們嗎說產場點麼開關與後裡發現應該總體臺灣電腦網絡東西時間問題話語")
var cantoneseMarkers = []string{"係", "唔", "咗", "嘅", "喺", "冇", "睇", "佢", "咁", "啲", "畀", "攞"}

func hasAnyRune(text string, runes []rune) bool {
	for _, r := range runes {
		if strings.ContainsRune(text, r) {
			return true
		}
	}
	return false
}

func containsAnyFold(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// 停用词表：选取区分度高、几乎不会出现在其他语言里的高频词。
var latinStopwords = map[string][]string{
	LangEn: {"the", "and", "you", "please", "with", "this", "that", "have", "would", "your", "about", "what", "there", "from"},
	LangEs: {"el", "la", "los", "las", "que", "para", "con", "una", "por", "como", "pero", "muy", "gracias", "precio", "puede", "necesito", "cuánto", "dónde"},
	LangFr: {"le", "la", "les", "des", "une", "que", "pour", "avec", "vous", "nous", "est", "dans", "sur", "merci", "bonjour", "prix", "combien", "voudrais"},
	LangDe: {"der", "die", "das", "und", "nicht", "mit", "für", "ist", "wir", "sie", "haben", "bitte", "danke", "preis", "wie", "können", "möchte", "guten"},
	LangPt: {"não", "uma", "para", "com", "que", "você", "obrigado", "preço", "como", "está", "por", "mais", "isso", "quanto", "olá"},
	LangIt: {"che", "per", "una", "con", "non", "sono", "grazie", "prezzo", "come", "questo", "anche", "molto", "vorrei", "ciao"},
	LangNl: {"de", "het", "een", "niet", "met", "voor", "van", "ik", "wij", "dank", "prijs", "hoeveel", "graag", "alstublieft"},
	LangID: {"yang", "dan", "tidak", "untuk", "dengan", "saya", "kami", "harga", "bisa", "terima", "kasih", "berapa", "apakah", "mau"},
	LangVi: {"không", "của", "và", "cho", "với", "tôi", "chúng", "giá", "cảm", "ơn", "bao", "nhiêu", "như", "thế", "nào"},
	LangTr: {"ve", "bir", "için", "ile", "değil", "ben", "biz", "fiyat", "teşekkür", "nasıl", "ne", "kadar", "istiyorum", "merhaba"},
	LangPl: {"nie", "się", "jest", "dla", "oraz", "dziękuję", "cena", "jak", "proszę", "chcę", "czy", "można", "witam"},
	LangRo: {"nu", "este", "pentru", "cu", "care", "mulțumesc", "preț", "cum", "doar", "vreau", "bună", "vă", "rog"},
}

var latinDistinctive = map[string][]string{
	LangEs: {"¿", "¡", "ñ", "ía", "ción", "qué"},
	LangFr: {"ç", "œ", "à", "ê", "î", "votre"},
	LangDe: {"ß", "ä", "ö", "ü", "sch", "ich"},
	LangPt: {"ã", "õ", "ç", "ção", "não"},
	LangIt: {"gli", "zione", "che"},
	LangVi: {"ơ", "ư", "đ", "ạ", "ế", "ộ"},
	LangTr: {"ı", "ğ", "ş", "ç"},
	LangPl: {"ą", "ę", "ł", "ż", "ś"},
	LangRo: {"ă", "â", "î", "ș", "ț"},
	LangNl: {"ij", "aa", "oe"},
	LangID: {"ng", "nya", "kah"},
}

func latinTokens(text string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || r == '\'' {
			current.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func scoreLatin(text string) []LanguageCandidate {
	tokens := latinTokens(text)
	if len(tokens) == 0 {
		return nil
	}
	tokenSet := make(map[string]int, len(tokens))
	for _, token := range tokens {
		tokenSet[token]++
	}
	scores := make(map[string]float64)
	for code, words := range latinStopwords {
		for _, word := range words {
			if count, ok := tokenSet[word]; ok {
				scores[code] += float64(count) * 1.0
			}
		}
	}
	lower := strings.ToLower(text)
	for code, markers := range latinDistinctive {
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				scores[code] += 1.2
			}
		}
	}
	var candidates []LanguageCandidate
	for code, score := range scores {
		if score > 0 {
			candidates = append(candidates, LanguageCandidate{Code: code, Score: score})
		}
	}
	// 按分数降序（同分时按语言码稳定排序）
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Score > candidates[i].Score ||
				(candidates[j].Score == candidates[i].Score && candidates[j].Code < candidates[i].Code) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	return candidates
}

func detectChineseVariant(text string) (string, string) {
	if containsAnyFold(text, cantoneseMarkers) {
		return LangYue, "yue-Hant"
	}
	if hasAnyRune(text, traditionalOnly) {
		return LangZh, "zh-Hant"
	}
	return LangZh, "zh-Hans"
}

// 阿拉伯语方言标记按"整词"匹配，避免 "زوين" 命中 "وين" 这类子串误判。
var arabicDialectWords = []struct {
	dialect string
	words   []string
	letters string
}{
	{"ar-Gulf", []string{"شلون", "وين", "ابغى", "أبغى", "مره", "وايد", "شنو", "هسه"}, "گچپ"},
	{"ar-EG", []string{"ازيك", "عايز", "عاوز", "خالص", "جامد", "كده", "ايه", "إيه", "دلوقتي"}, ""},
	{"ar-LEV", []string{"كيفك", "هلق", "منيح", "شو", "هيك", "كتير", "شو"}, ""},
	{"ar-MA", []string{"بزاف", "واش", "دابا", "كيفاش", "مزيان", "غير", "بزاف"}, ""},
}

func arabicWords(text string) map[string]bool {
	words := map[string]bool{}
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words[string(current)] = true
			current = nil
		}
	}
	for _, r := range text {
		if unicode.Is(unicode.Arabic, r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return words
}

func detectArabicDialect(text string) string {
	words := arabicWords(text)
	for _, rule := range arabicDialectWords {
		for _, word := range rule.words {
			if words[word] {
				return rule.dialect
			}
		}
		if rule.letters != "" && strings.ContainsAny(text, rule.letters) {
			return rule.dialect
		}
	}
	return ""
}

func detectSpanishDialect(text string) string {
	switch {
	case containsAnyFold(text, []string{"vosotros", "vale", "coche", "ordenador", "móvil", "genial"}):
		return "es-ES"
	case containsAnyFold(text, []string{"vos ", "che ", "boludo", "laburo", "plata", "querés"}):
		return "es-AR"
	case containsAnyFold(text, []string{"carro", "platicar", "ahorita", "computadora", "celular", "acá"}):
		return "es-419"
	}
	return ""
}

func detectPortugueseDialect(text string) string {
	switch {
	case containsAnyFold(text, []string{"você", "celular", "ônibus", "time", "a gente", "bacana"}):
		return "pt-BR"
	case containsAnyFold(text, []string{"telemóvel", "autocarro", "equipa", "pequeno-almoço"}):
		return "pt-PT"
	}
	return ""
}

func detectEnglishDialect(text string) string {
	if containsAnyFold(text, []string{"colour", "favourite", "centre", "organise", "whilst"}) {
		return "en-GB"
	}
	if containsAnyFold(text, []string{"color", "favorite", "center", "organize", "gotten"}) {
		return "en-US"
	}
	return ""
}

/**
 * DetectLanguage 识别一段文本的语言：先按书写脚本定位语族，拉丁语系再做词表打分，
 * 有足够证据时给出方言标签，否则回落标准语。纯本地、无网络调用。
 */
func DetectLanguage(text string) LanguageResult {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return LanguageResult{Code: LangZh, Tag: LangZh, Confidence: 0, Method: "fallback"}
	}
	c := classifyScripts(trimmed)
	totalLetters := c.han + c.kana + c.hangul + c.arabic + c.cyrillic + c.thai + c.hebrew + c.devanagari + c.greek + c.latin + c.other
	if totalLetters == 0 {
		return LanguageResult{Code: LangZh, Tag: LangZh, Confidence: 0, Method: "fallback"}
	}

	type scriptRule struct {
		count int
		code  string
		tag   string
	}
	// 假名/谚文优先于汉字：日文里含大量汉字，但假名决定它是日语。
	scripts := []scriptRule{
		{c.kana, LangJa, "ja"},
		{c.hangul, LangKo, "ko"},
		{c.thai, LangTh, "th"},
		{c.hebrew, LangHe, "he"},
		{c.devanagari, LangHi, "hi"},
		{c.greek, LangEl, "el"},
		{c.cyrillic, LangRu, "ru"},
		{c.arabic, LangAr, "ar"},
	}
	for _, rule := range scripts {
		if float64(rule.count)/float64(totalLetters) >= 0.15 {
			confidence := math.Min(1, float64(rule.count)/float64(totalLetters)+0.2)
			result := LanguageResult{Code: rule.code, Tag: rule.tag, Confidence: round2(confidence), Method: "script"}
			if rule.code == LangAr {
				if dialect := detectArabicDialect(trimmed); dialect != "" {
					result.Dialect = dialect
					result.Tag = dialect
				}
			}
			return result
		}
	}
	if c.han > 0 && float64(c.han+c.kana)/float64(totalLetters) >= 0.2 {
		code, tag := detectChineseVariant(trimmed)
		return LanguageResult{
			Code:       code,
			Tag:        tag,
			Dialect:    tag,
			Confidence: round2(math.Min(1, float64(c.han)/float64(totalLetters)+0.2)),
			Method:     "script",
		}
	}

	// 拉丁语系：词表打分
	candidates := scoreLatin(trimmed)
	if len(candidates) > 0 {
		total := 0.0
		for _, item := range candidates {
			total += item.Score
		}
		top := candidates[0]
		confidence := top.Score / total
		result := LanguageResult{
			Code:       top.Code,
			Tag:        top.Code,
			Confidence: round2(confidence),
			Method:     "lexicon",
			Candidates: candidates,
		}
		switch top.Code {
		case LangEs:
			if dialect := detectSpanishDialect(trimmed); dialect != "" {
				result.Dialect = dialect
				result.Tag = dialect
			}
		case LangPt:
			if dialect := detectPortugueseDialect(trimmed); dialect != "" {
				result.Dialect = dialect
				result.Tag = dialect
			}
		case LangEn:
			if dialect := detectEnglishDialect(trimmed); dialect != "" {
				result.Dialect = dialect
				result.Tag = dialect
			}
		}
		if confidence >= 0.55 {
			return result
		}
		result.Method = "lexicon"
		return result
	}

	// 只有拉丁字母但没有任何词表命中：默认按英语处理（最常见的商务语言）
	return LanguageResult{Code: LangEn, Tag: LangEn, Confidence: 0.3, Method: "fallback"}
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

// 语言名 → 语言码，覆盖客户可能用来表达"请用 X 回答"的说法。
var languageNames = map[string]string{
	"english": LangEn, "英语": LangEn, "英文": LangEn, "الإنجليزية": LangEn, "انجليزي": LangEn,
	"inglés": LangEn, "ingles": LangEn, "anglais": LangEn, "englisch": LangEn, "inglês": LangEn,
	"inglese": LangEn, "英語": LangEn, "영어": LangEn, "английский": LangEn,
	"chinese": LangZh, "mandarin": LangZh, "中文": LangZh, "汉语": LangZh, "简体中文": LangZh,
	"普通话": LangZh, "中文简体": LangZh, "الصينية": LangZh, "chino": LangZh, "chinois": LangZh,
	"chinesisch": LangZh, "chinês": LangZh, "cinese": LangZh, "中国語": LangZh, "중국어": LangZh, "китайский": LangZh,
	"arabic": LangAr, "阿拉伯语": LangAr, "阿语": LangAr, "العربية": LangAr, "عربي": LangAr,
	"árabe": LangAr, "arabe": LangAr, "arabisch": LangAr, "arabo": LangAr, "アラビア語": LangAr, "아랍어": LangAr,
	"spanish": LangEs, "español": LangEs, "espanol": LangEs, "西班牙语": LangEs, "西语": LangEs,
	"espagnol": LangEs, "spanisch": LangEs, "espanhol": LangEs, "spagnolo": LangEs, "スペイン語": LangEs, "스페인어": LangEs,
	"french": LangFr, "français": LangFr, "francais": LangFr, "法语": LangFr, "法文": LangFr,
	"französisch": LangFr, "frances": LangFr, "francese": LangFr, "フランス語": LangFr, "프랑스어": LangFr,
	"german": LangDe, "deutsch": LangDe, "德语": LangDe, "德文": LangDe, "alemán": LangDe,
	"allemand": LangDe, "alemão": LangDe, "tedesco": LangDe, "ドイツ語": LangDe, "독일어": LangDe,
	"portuguese": LangPt, "português": LangPt, "portugues": LangPt, "葡萄牙语": LangPt, "葡语": LangPt,
	"portugués": LangPt, "portugais": LangPt, "portugiesisch": LangPt, "portoghese": LangPt,
	"russian": LangRu, "русский": LangRu, "русском": LangRu, "по-русски": LangRu, "russisch": LangRu, "俄语": LangRu, "russo": LangRu, "russe": LangRu, "ruso": LangRu,
	"japanese": LangJa, "日本語": LangJa, "日语": LangJa, "japonés": LangJa, "japonais": LangJa,
	"korean": LangKo, "한국어": LangKo, "韩语": LangKo, "coreano": LangKo, "coréen": LangKo,
	"italian": LangIt, "italiano": LangIt, "意大利语": LangIt, "italien": LangIt,
	"thai": LangTh, "ไทย": LangTh, "泰语": LangTh,
	"vietnamese": LangVi, "tiếng việt": LangVi, "越南语": LangVi,
	"indonesian": LangID, "bahasa": LangID, "印尼语": LangID,
	"turkish": LangTr, "türkçe": LangTr, "土耳其语": LangTr,
	"dutch": LangNl, "nederlands": LangNl, "荷兰语": LangNl,
	"polish": LangPl, "polski": LangPl, "波兰语": LangPl,
	"romanian": LangRo, "română": LangRo, "罗马尼亚语": LangRo,
}

func lookupLanguageName(value string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(value))
	key = strings.Trim(key, " .,!?。！？、:：;；\"'“”「」")
	if code, ok := languageNames[key]; ok {
		return code, true
	}
	// 允许 "in English" / "用英语" 这类前后缀
	for name, code := range languageNames {
		if strings.Contains(key, name) {
			return code, true
		}
	}
	return "", false
}

// instructionPatterns 按语言列出"请用 X 回答我"的句式，捕获组 1 是语言名。
var instructionPatterns = []string{
	`请(?:你)?用\s*([^\s，,。.！!？?]{1,20})\s*(?:来)?(?:回复|回答|答复|说|讲|沟通|交流|发消息)`,
	`用\s*([^\s，,。.！!？?]{1,20})\s*(?:来)?(?:回复|回答|答复)(?:我)?`,
	`(?:请)?以\s*([^\s，,。.！!？?]{1,20})\s*(?:回复|回答)(?:我)?`,
	`(?i)(?:please\s+)?(?:reply|respond|answer|write|talk|speak|continue|chat)\s+(?:to\s+me\s+)?in\s+([A-Za-zÀ-ɏ\- ]{2,20})`,
	`(?i)in\s+([A-Za-zÀ-ɏ\- ]{2,20})\s+please`,
	`(?i)responde(?:r|me)?\s+en\s+([A-Za-zÀ-ɏ]{2,20})`,
	`(?i)contéstame\s+en\s+([A-Za-zÀ-ɏ]{2,20})`,
	`(?i)répond(?:ez|s|re)?\s+en\s+([A-Za-zÀ-ɏ]{2,20})`,
	`(?i)antworte\s+(?:mir\s+)?auf\s+([A-Za-zÀ-ɏäöüß]{2,20})`,
	`(?i)responde(?:r)?\s+em\s+([A-Za-zÀ-ɏ]{2,20})`,
	`(?i)rispondi\s+in\s+([A-Za-zÀ-ɏ]{2,20})`,
	`(?i)отвечай\s+на\s+([А-Яа-яЁё]{2,20})`,
	`بالعربية`,
	`بالإنجليزية`,
	`بالفرنسية`,
	`بالصينية`,
	`(?:日本語|にほんご)で`,
	`한국어로`,
}

var scriptedInstructions = map[string]string{
	"بالعربية":    LangAr,
	"بالإنجليزية": LangEn,
	"بالفرنسية":   LangFr,
	"بالصينية":    LangZh,
	"日本語で":        LangJa,
	"にほんごで":       LangJa,
	"한국어로":        LangKo,
}

// ParseLanguageInstruction 从客户消息里解析"请用 X 语言回答我"这类显式指令。
// 返回语言码与是否命中；命中后应在整个会话内持续生效。
func ParseLanguageInstruction(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len([]rune(trimmed)) > 400 {
		return "", false
	}
	for marker, code := range scriptedInstructions {
		if strings.Contains(trimmed, marker) {
			return code, true
		}
	}
	for _, pattern := range instructionPatterns {
		re := mustCompile(pattern)
		if re == nil {
			continue
		}
		matches := re.FindStringSubmatch(trimmed)
		if len(matches) < 2 {
			continue
		}
		if code, ok := lookupLanguageName(matches[1]); ok {
			return code, true
		}
	}
	return "", false
}

// DetectDominantLanguage 按"用得多的语言"选择回复语言：
// 统计占比最高者；平局取最近一条的语种；最高占比低于阈值时回落 fallback。
func DetectDominantLanguage(recent []string, fallback string, minRatio float64) string {
	if len(recent) == 0 {
		return NormalizeLang(fallback, LangZh)
	}
	if minRatio <= 0 {
		minRatio = 0.4
	}
	counts := map[string]int{}
	order := make([]string, 0, len(recent))
	for _, item := range recent {
		code := NormalizeLang(item, "")
		if code == "" {
			continue
		}
		counts[code]++
		order = append(order, code)
	}
	if len(counts) == 0 {
		return NormalizeLang(fallback, LangZh)
	}
	best := ""
	bestCount := 0
	for code, count := range counts {
		if count > bestCount || (count == bestCount && code == order[len(order)-1]) {
			best = code
			bestCount = count
		}
	}
	if float64(bestCount)/float64(len(order)) < minRatio {
		// 平局或占比过低：以最近一条为准，其次初始语言
		last := order[len(order)-1]
		if float64(counts[last])/float64(len(order)) >= minRatio {
			return last
		}
		return NormalizeLang(fallback, LangZh)
	}
	return best
}
